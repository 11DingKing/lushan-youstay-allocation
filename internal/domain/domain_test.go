package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/11DingKing/lushan-youstay-allocation/internal/domain"
)

func TestDateRangeUsesPropertyTimezoneAndHalfOpenNights(t *testing.T) {
	t.Parallel()
	rangeValue, err := domain.ParseDateRange("2026-10-01", "2026-10-04", "Asia/Shanghai")
	if err != nil {
		t.Fatalf("ParseDateRange() error = %v", err)
	}
	if got, want := rangeValue.NightCount(), 3; got != want {
		t.Fatalf("NightCount() = %d, want %d", got, want)
	}
	want := []string{"2026-10-01", "2026-10-02", "2026-10-03"}
	got := rangeValue.NightStrings()
	if len(got) != len(want) {
		t.Fatalf("NightStrings() length = %d, want %d", len(got), len(want))
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("NightStrings()[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}

func TestDateRangeRejectsInvalidBoundaries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		checkIn  string
		checkOut string
		timezone string
	}{
		{name: "same day", checkIn: "2026-10-01", checkOut: "2026-10-01", timezone: "Asia/Shanghai"},
		{name: "reverse", checkIn: "2026-10-02", checkOut: "2026-10-01", timezone: "Asia/Shanghai"},
		{name: "bad check in", checkIn: "01-10-2026", checkOut: "2026-10-02", timezone: "Asia/Shanghai"},
		{name: "bad check out", checkIn: "2026-10-01", checkOut: "tomorrow", timezone: "Asia/Shanghai"},
		{name: "unknown timezone", checkIn: "2026-10-01", checkOut: "2026-10-02", timezone: "Mars/Base"},
		{name: "too long", checkIn: "2026-01-01", checkOut: "2026-06-01", timezone: "Asia/Shanghai"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := domain.ParseDateRange(test.checkIn, test.checkOut, test.timezone)
			if err == nil {
				t.Fatal("ParseDateRange() error = nil, want validation error")
			}
		})
	}
}

func TestDateRangeOverlapUsesCheckoutAsExclusiveBoundary(t *testing.T) {
	t.Parallel()
	first, _ := domain.ParseDateRange("2026-09-01", "2026-09-03", "Asia/Shanghai")
	adjacent, _ := domain.ParseDateRange("2026-09-03", "2026-09-05", "Asia/Shanghai")
	overlap, _ := domain.ParseDateRange("2026-09-02", "2026-09-04", "Asia/Shanghai")
	if first.Overlaps(adjacent) {
		t.Error("adjacent stays must not overlap")
	}
	if !first.Overlaps(overlap) {
		t.Error("shared night must overlap")
	}
}

func TestResourceValidation(t *testing.T) {
	t.Parallel()
	base := domain.Resource{ID: "r1", PropertyID: "p1", Code: "V1", Name: "Villa", Kind: domain.ResourceVillaRoom, Capacity: 2, BasePriceCents: 100, Status: domain.ResourceOpen, Version: 1}
	tests := []struct {
		name    string
		mutate  func(*domain.Resource)
		wantErr bool
	}{
		{name: "valid", mutate: func(*domain.Resource) {}},
		{name: "missing id", mutate: func(r *domain.Resource) { r.ID = "" }, wantErr: true},
		{name: "missing property", mutate: func(r *domain.Resource) { r.PropertyID = "" }, wantErr: true},
		{name: "missing code", mutate: func(r *domain.Resource) { r.Code = "" }, wantErr: true},
		{name: "zero capacity", mutate: func(r *domain.Resource) { r.Capacity = 0 }, wantErr: true},
		{name: "negative price", mutate: func(r *domain.Resource) { r.BasePriceCents = -1 }, wantErr: true},
		{name: "unknown kind", mutate: func(r *domain.Resource) { r.Kind = "tree_house" }, wantErr: true},
		{name: "unknown status", mutate: func(r *domain.Resource) { r.Status = "missing" }, wantErr: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			r := base
			test.mutate(&r)
			err := r.Validate()
			if (err != nil) != test.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestResourceStateMachine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		from    domain.ResourceStatus
		to      domain.ResourceStatus
		allowed bool
	}{
		{name: "open to held", from: domain.ResourceOpen, to: domain.ResourceHeld, allowed: true},
		{name: "held to occupied", from: domain.ResourceHeld, to: domain.ResourceOccupied, allowed: true},
		{name: "occupied to cleaning", from: domain.ResourceOccupied, to: domain.ResourceCleaning, allowed: true},
		{name: "cleaning to open", from: domain.ResourceCleaning, to: domain.ResourceOpen, allowed: true},
		{name: "fault to cleaning", from: domain.ResourceFaulted, to: domain.ResourceCleaning, allowed: true},
		{name: "closed to open", from: domain.ResourceClosed, to: domain.ResourceOpen, allowed: true},
		{name: "open to cleaning", from: domain.ResourceOpen, to: domain.ResourceCleaning},
		{name: "occupied to open", from: domain.ResourceOccupied, to: domain.ResourceOpen},
		{name: "closed to occupied", from: domain.ResourceClosed, to: domain.ResourceOccupied},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			resource := domain.Resource{Status: test.from, Version: 7}
			updated, err := resource.Transition(test.to)
			if test.allowed {
				if err != nil {
					t.Fatalf("Transition() error = %v", err)
				}
				if updated.Status != test.to || updated.Version != 8 {
					t.Fatalf("Transition() = status %s version %d", updated.Status, updated.Version)
				}
				return
			}
			if !errors.Is(err, domain.ErrInvalidTransition) {
				t.Fatalf("Transition() error = %v, want ErrInvalidTransition", err)
			}
		})
	}
}

func TestStayStateMachineHappyPath(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)
	expires := now.Add(time.Hour)
	stay := domain.Stay{Status: domain.StayInquiry, Version: 1, HoldExpiresAt: &expires}
	path := []domain.StayStatus{domain.StayHeld, domain.StayGuaranteed, domain.StayCheckedIn, domain.StayCheckedOut, domain.StaySettling, domain.StaySettled}
	for _, target := range path {
		updated, err := stay.Transition(target, now)
		if err != nil {
			t.Fatalf("Transition(%s) from %s: %v", target, stay.Status, err)
		}
		stay = updated
	}
	if stay.Version != int64(len(path)+1) {
		t.Fatalf("final version = %d, want %d", stay.Version, len(path)+1)
	}
}

func TestStayStateMachineRejectsSkippedSteps(t *testing.T) {
	t.Parallel()
	tests := []struct {
		from domain.StayStatus
		to   domain.StayStatus
	}{
		{from: domain.StayInquiry, to: domain.StayGuaranteed},
		{from: domain.StayHeld, to: domain.StayCheckedIn},
		{from: domain.StayGuaranteed, to: domain.StaySettled},
		{from: domain.StayCheckedIn, to: domain.StaySettling},
		{from: domain.StayCheckedOut, to: domain.StayCheckedIn},
		{from: domain.StaySettled, to: domain.StayCancelled},
	}
	for _, test := range tests {
		stay := domain.Stay{Status: test.from, Version: 2}
		_, err := stay.Transition(test.to, time.Now())
		if !errors.Is(err, domain.ErrInvalidTransition) {
			t.Errorf("Transition(%s -> %s) error = %v", test.from, test.to, err)
		}
	}
}

func TestHoldActiveRequiresHeldStatusAndFutureExpiry(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC)
	future := now.Add(time.Minute)
	past := now.Add(-time.Minute)
	tests := []struct {
		name string
		stay domain.Stay
		want bool
	}{
		{name: "active", stay: domain.Stay{Status: domain.StayHeld, HoldExpiresAt: &future}, want: true},
		{name: "expired", stay: domain.Stay{Status: domain.StayHeld, HoldExpiresAt: &past}},
		{name: "missing expiry", stay: domain.Stay{Status: domain.StayHeld}},
		{name: "guaranteed", stay: domain.Stay{Status: domain.StayGuaranteed, HoldExpiresAt: &future}},
	}
	for _, test := range tests {
		if got := test.stay.HoldActive(now); got != test.want {
			t.Errorf("%s HoldActive() = %v, want %v", test.name, got, test.want)
		}
	}
}

func TestOperationalTaskRetryAndCompletion(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	task := domain.OperationalTask{Status: domain.TaskPending, Attempts: 0, AvailableAt: now}
	claimed, err := task.Claim(now)
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if claimed.Status != domain.TaskClaimed || claimed.Attempts != 1 {
		t.Fatalf("claimed = %#v", claimed)
	}
	retried := claimed.Retry(now, errors.New("temporary radio outage"), 3, time.Minute)
	if retried.Status != domain.TaskPending {
		t.Fatalf("Retry() status = %s", retried.Status)
	}
	if want := now.Add(time.Minute); !retried.AvailableAt.Equal(want) {
		t.Fatalf("Retry() available = %s, want %s", retried.AvailableAt, want)
	}
	retried.Status = domain.TaskClaimed
	retried.Attempts = 3
	failed := retried.Retry(now, errors.New("permanent"), 3, time.Minute)
	if failed.Status != domain.TaskFailed {
		t.Fatalf("final Retry() status = %s", failed.Status)
	}
	completed, err := claimed.Complete(now)
	if err != nil || completed.Status != domain.TaskCompleted {
		t.Fatalf("Complete() = %#v, %v", completed, err)
	}
}

func TestSettlementNeverProducesNegativeRefund(t *testing.T) {
	t.Parallel()
	now := time.Now()
	tests := []struct {
		name   string
		paid   int64
		damage int64
		refund int64
	}{
		{name: "full refund", paid: 50_000, damage: 0, refund: 50_000},
		{name: "partial refund", paid: 50_000, damage: 12_000, refund: 38_000},
		{name: "damage exceeds paid", paid: 20_000, damage: 25_000, refund: 0},
	}
	for _, test := range tests {
		settlement, err := domain.NewSettlement("s1", "stay1", test.paid, test.damage, now)
		if err != nil {
			t.Fatalf("%s NewSettlement() error = %v", test.name, err)
		}
		if settlement.RefundCents != test.refund {
			t.Errorf("%s refund = %d, want %d", test.name, settlement.RefundCents, test.refund)
		}
	}
	if _, err := domain.NewSettlement("s", "stay", -1, 0, now); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("negative paid error = %v", err)
	}
}
