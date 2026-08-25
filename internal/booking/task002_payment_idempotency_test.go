package booking_test

import (
	"context"
	"testing"

	"github.com/11DingKing/lushan-youstay-allocation/internal/domain"
)

func TestDuplicateProviderNotificationReportsAlreadyProcessed(t *testing.T) {
	f := newBookingFixture(t)
	stay, err := f.service.Hold(context.Background(), defaultHold(f, "payment-hold"))
	if err != nil {
		t.Fatal(err)
	}
	now := f.clock.Now()
	payment := domain.Payment{ID: "payment-first", StayID: stay.ID, Provider: "wechat", ProviderEventID: "provider-event-7", AmountCents: 30000, Kind: domain.PaymentGuarantee, Status: domain.PaymentSucceeded, OccurredAt: now, CreatedAt: now}
	first, err := f.service.ReceivePayment(context.Background(), payment)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.service.ReceivePayment(context.Background(), payment)
	if err != nil {
		t.Fatal(err)
	}
	if !first || second {
		t.Fatalf("provider notification results first=%v second=%v, want true then false", first, second)
	}
	count, err := f.store.CountTable(context.Background(), "payments")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("payments persisted=%d, duplicate notification must not create another payment", count)
	}
}
