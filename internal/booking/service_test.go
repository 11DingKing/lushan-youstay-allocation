package booking_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/11DingKing/lushan-youstay-allocation/internal/auth"
	"github.com/11DingKing/lushan-youstay-allocation/internal/booking"
	"github.com/11DingKing/lushan-youstay-allocation/internal/clock"
	"github.com/11DingKing/lushan-youstay-allocation/internal/domain"
	storesqlite "github.com/11DingKing/lushan-youstay-allocation/internal/store/sqlite"
)

type bookingFixture struct {
	store    *storesqlite.Store
	clock    *clock.Manual
	service  *booking.Service
	property domain.Property
	resource domain.Resource
	guest    auth.User
}

func newBookingFixture(t *testing.T) *bookingFixture {
	t.Helper()
	ctx := context.Background()
	store, err := storesqlite.Open(ctx, filepath.Join(t.TempDir(), "booking.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)
	manual := clock.NewManual(now)
	guest := auth.User{ID: "guest1", Username: "guest1", PasswordHash: auth.HashPassword("secret"), DisplayName: "Guest", Role: auth.RoleGuest, Active: true, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateUser(ctx, guest); err != nil {
		t.Fatal(err)
	}
	property := domain.Property{ID: "p1", Name: "YouStay", Zone: "Lushan", Timezone: "Asia/Shanghai", Status: domain.PropertyOpen, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateProperty(ctx, property); err != nil {
		t.Fatal(err)
	}
	resource := domain.Resource{ID: "r1", PropertyID: property.ID, Code: "M201", Name: "Mountain", Kind: domain.ResourceMountainRoom, Capacity: 2, BasePriceCents: 50_000, Status: domain.ResourceOpen, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateResource(ctx, resource); err != nil {
		t.Fatal(err)
	}
	return &bookingFixture{store: store, clock: manual, service: booking.NewService(store, manual, booking.DefaultPricePolicy(), 15*time.Minute), property: property, resource: resource, guest: guest}
}

func defaultHold(f *bookingFixture, key string) booking.HoldRequest {
	return booking.HoldRequest{GuestID: f.guest.ID, ResourceID: f.resource.ID, CheckInDate: "2026-09-01", CheckOutDate: "2026-09-03", PartySize: 2, IdempotencyKey: key, RequestID: "req1"}
}

func TestHoldCreatesQuoteExpiryNightsAndAudit(t *testing.T) {
	f := newBookingFixture(t)
	stay, err := f.service.Hold(context.Background(), defaultHold(f, "hold-1"))
	if err != nil {
		t.Fatalf("Hold() error = %v", err)
	}
	if stay.Status != domain.StayHeld {
		t.Errorf("status = %s", stay.Status)
	}
	if stay.QuotedCents != 100_000 {
		t.Errorf("quote = %d", stay.QuotedCents)
	}
	if stay.HoldExpiresAt == nil || !stay.HoldExpiresAt.Equal(f.clock.Now().Add(15*time.Minute)) {
		t.Errorf("expiry = %v", stay.HoldExpiresAt)
	}
	var nights int
	if err := f.store.DB().QueryRow(`SELECT COUNT(*) FROM stay_nights WHERE stay_id=?`, stay.ID).Scan(&nights); err != nil {
		t.Fatal(err)
	}
	if nights != 2 {
		t.Fatalf("nights = %d", nights)
	}
	var events int
	if err := f.store.DB().QueryRow(`SELECT COUNT(*) FROM audit_events WHERE object_id=?`, stay.ID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("audit events = %d", events)
	}
}

func TestHoldReturnsOriginalResultForSameIdempotencyKey(t *testing.T) {
	f := newBookingFixture(t)
	request := defaultHold(f, "same-key")
	first, err := f.service.Hold(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.CheckOutDate = "2026-09-05"
	second, err := f.service.Hold(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.CheckOutDate != first.CheckOutDate {
		t.Fatalf("idempotent result changed: first=%#v second=%#v", first, second)
	}
	count, _ := f.store.CountTable(context.Background(), "stays")
	if count != 1 {
		t.Fatalf("stay count = %d", count)
	}
}

func TestHoldRejectsOverlappingNightsWithoutPartialState(t *testing.T) {
	f := newBookingFixture(t)
	if _, err := f.service.Hold(context.Background(), defaultHold(f, "first")); err != nil {
		t.Fatal(err)
	}
	second := defaultHold(f, "second")
	second.CheckInDate = "2026-09-02"
	second.CheckOutDate = "2026-09-04"
	_, err := f.service.Hold(context.Background(), second)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second Hold() error = %v", err)
	}
	count, _ := f.store.CountTable(context.Background(), "stays")
	if count != 1 {
		t.Fatalf("stay count = %d", count)
	}
	var nights int
	_ = f.store.DB().QueryRow(`SELECT COUNT(*) FROM stay_nights`).Scan(&nights)
	if nights != 2 {
		t.Fatalf("night count = %d", nights)
	}
}

func TestHoldRejectsClosedPropertyAndFaultedResource(t *testing.T) {
	tests := []struct {
		name           string
		closeProperty  bool
		resourceStatus domain.ResourceStatus
	}{{name: "weather closure", closeProperty: true}, {name: "faulted resource", resourceStatus: domain.ResourceFaulted}, {name: "closed resource", resourceStatus: domain.ResourceClosed}}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			f := newBookingFixture(t)
			if test.closeProperty {
				if err := f.store.UpdatePropertyStatus(context.Background(), f.property.ID, domain.PropertyOpen, domain.PropertyWeatherClosed, 1, f.clock.Now()); err != nil {
					t.Fatal(err)
				}
			}
			if test.resourceStatus != "" {
				if err := f.store.UpdateResourceStatus(context.Background(), f.resource.ID, domain.ResourceOpen, test.resourceStatus, 1, f.clock.Now()); err != nil {
					t.Fatal(err)
				}
			}
			_, err := f.service.Hold(context.Background(), defaultHold(f, "closed"))
			if !errors.Is(err, domain.ErrUnavailable) {
				t.Fatalf("Hold() error = %v", err)
			}
		})
	}
}

func TestHoldRejectsPartyOverCapacityAndBadDates(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*booking.HoldRequest)
	}{{name: "capacity", mutate: func(r *booking.HoldRequest) { r.PartySize = 3 }}, {name: "same date", mutate: func(r *booking.HoldRequest) { r.CheckOutDate = r.CheckInDate }}, {name: "reverse date", mutate: func(r *booking.HoldRequest) { r.CheckOutDate = "2026-08-31" }}, {name: "missing idempotency", mutate: func(r *booking.HoldRequest) { r.IdempotencyKey = "" }}}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			f := newBookingFixture(t)
			request := defaultHold(f, "bad")
			test.mutate(&request)
			_, err := f.service.Hold(context.Background(), request)
			if err == nil {
				t.Fatal("Hold() error = nil")
			}
			count, _ := f.store.CountTable(context.Background(), "stays")
			if count != 0 {
				t.Fatalf("invalid hold persisted %d stays", count)
			}
		})
	}
}

func TestGuaranteeRequiresActiveHoldOwnershipAndIdentityDigest(t *testing.T) {
	tests := []struct {
		name            string
		advance         time.Duration
		guestID, digest string
		want            error
	}{{name: "expired", advance: 16 * time.Minute, guestID: "guest1", digest: "0123456789abcdef", want: domain.ErrExpired}, {name: "other guest", guestID: "other", digest: "0123456789abcdef", want: domain.ErrForbidden}, {name: "short digest", guestID: "guest1", digest: "short", want: domain.ErrInvalid}}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			f := newBookingFixture(t)
			stay, err := f.service.Hold(context.Background(), defaultHold(f, "hold"))
			if err != nil {
				t.Fatal(err)
			}
			f.clock.Advance(test.advance)
			_, err = f.service.Guarantee(context.Background(), booking.GuaranteeRequest{StayID: stay.ID, GuestID: test.guestID, DocumentDigest: test.digest, Provider: "mock", ProviderEventID: "event", VerifiedBy: "desk", RequestID: "req"})
			if !errors.Is(err, test.want) {
				t.Fatalf("Guarantee() error = %v, want %v", err, test.want)
			}
			loaded, _ := f.store.GetStay(context.Background(), stay.ID)
			if loaded.Status != domain.StayHeld {
				t.Fatalf("failed guarantee changed status to %s", loaded.Status)
			}
		})
	}
}

func TestGuaranteeAtomicallyPersistsRelatedEntities(t *testing.T) {
	f := newBookingFixture(t)
	verifier := auth.User{ID: "desk", Username: "desk", PasswordHash: auth.HashPassword("secret"), DisplayName: "Desk", Role: auth.RoleFrontdesk, Active: true, CreatedAt: f.clock.Now(), UpdatedAt: f.clock.Now()}
	if err := f.store.CreateUser(context.Background(), verifier); err != nil {
		t.Fatal(err)
	}
	stay, err := f.service.Hold(context.Background(), defaultHold(f, "hold"))
	if err != nil {
		t.Fatal(err)
	}
	updated, err := f.service.Guarantee(context.Background(), booking.GuaranteeRequest{StayID: stay.ID, GuestID: f.guest.ID, DocumentDigest: "0123456789abcdef", Provider: "wechat", ProviderEventID: "evt-1", VerifiedBy: verifier.ID, RequestID: "req"})
	if err != nil {
		t.Fatalf("Guarantee() error = %v", err)
	}
	if updated.Status != domain.StayGuaranteed || updated.GuaranteeCents != 30_000 {
		t.Fatalf("updated = %#v", updated)
	}
	for _, table := range []string{"identity_checks", "payments"} {
		count, _ := f.store.CountTable(context.Background(), table)
		if count != 1 {
			t.Errorf("%s count = %d", table, count)
		}
	}
	var reserved int
	_ = f.store.DB().QueryRow(`SELECT COUNT(*) FROM stay_nights WHERE stay_id=? AND status='reserved'`, stay.ID).Scan(&reserved)
	if reserved != 2 {
		t.Fatalf("reserved = %d", reserved)
	}
}

func TestCheckInRequiresGuaranteedStayAndUsesOptimisticVersion(t *testing.T) {
	f := newBookingFixture(t)
	stay, err := f.service.Hold(context.Background(), defaultHold(f, "hold"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CheckIn(context.Background(), stay.ID, "desk", "req"); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("early CheckIn() error = %v", err)
	}
	verifier := auth.User{ID: "desk", Username: "desk", PasswordHash: auth.HashPassword("secret"), DisplayName: "Desk", Role: auth.RoleFrontdesk, Active: true, CreatedAt: f.clock.Now(), UpdatedAt: f.clock.Now()}
	_ = f.store.CreateUser(context.Background(), verifier)
	guaranteed, err := f.service.Guarantee(context.Background(), booking.GuaranteeRequest{StayID: stay.ID, GuestID: f.guest.ID, DocumentDigest: "0123456789abcdef", Provider: "mock", ProviderEventID: "evt", VerifiedBy: "desk", RequestID: "req"})
	if err != nil {
		t.Fatal(err)
	}
	checkedIn, err := f.service.CheckIn(context.Background(), guaranteed.ID, "desk", "req")
	if err != nil {
		t.Fatal(err)
	}
	if checkedIn.Status != domain.StayCheckedIn || checkedIn.Version != 3 {
		t.Fatalf("checkedIn = %#v", checkedIn)
	}
}

func TestCancelReleasesHeldNights(t *testing.T) {
	f := newBookingFixture(t)
	stay, err := f.service.Hold(context.Background(), defaultHold(f, "hold"))
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := f.service.Cancel(context.Background(), stay.ID, f.guest.ID, "req")
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != domain.StayCancelled {
		t.Fatalf("status = %s", cancelled.Status)
	}
	var released int
	_ = f.store.DB().QueryRow(`SELECT COUNT(*) FROM stay_nights WHERE stay_id=? AND status='released'`, stay.ID).Scan(&released)
	if released != 2 {
		t.Fatalf("released nights = %d", released)
	}
}

func TestReceivePaymentDeduplicatesProviderEvent(t *testing.T) {
	f := newBookingFixture(t)
	stay, err := f.service.Hold(context.Background(), defaultHold(f, "hold"))
	if err != nil {
		t.Fatal(err)
	}
	payment := domain.Payment{StayID: stay.ID, Provider: "alipay", ProviderEventID: "event-1", AmountCents: 5_000, Kind: domain.PaymentCharge, Status: domain.PaymentSucceeded}
	created, err := f.service.ReceivePayment(context.Background(), payment)
	if err != nil || !created {
		t.Fatalf("first ReceivePayment() = %v, %v", created, err)
	}
	created, err = f.service.ReceivePayment(context.Background(), payment)
	if err != nil || created {
		t.Fatalf("second ReceivePayment() = %v, %v", created, err)
	}
	count, _ := f.store.CountTable(context.Background(), "payments")
	if count != 1 {
		t.Fatalf("payments = %d", count)
	}
}

func TestHoldPropagatesCancelledContextWithoutPersistence(t *testing.T) {
	f := newBookingFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := f.service.Hold(ctx, defaultHold(f, "cancelled"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Hold() error = %v", err)
	}
	count, _ := f.store.CountTable(context.Background(), "stays")
	if count != 0 {
		t.Fatalf("cancelled request persisted %d stays", count)
	}
}
