package booking_test

import (
	"testing"

	"github.com/11DingKing/lushan-youstay-allocation/internal/booking"
	"github.com/11DingKing/lushan-youstay-allocation/internal/domain"
)

func TestQuoteCombinesWeekendsHolidaysAndLongStayDiscount(t *testing.T) {
	t.Parallel()
	resource := domain.Resource{ID: "r", PropertyID: "p", Code: "M01", Name: "Mountain", Kind: domain.ResourceMountainRoom, Capacity: 2, BasePriceCents: 10_000, Status: domain.ResourceOpen}
	policy := booking.DefaultPricePolicy()
	tests := []struct {
		name, checkIn, checkOut string
		holidays                map[string]bool
		want                    int64
	}{
		{name: "weekday", checkIn: "2026-08-25", checkOut: "2026-08-26", want: 10_000},
		{name: "friday and saturday", checkIn: "2026-08-28", checkOut: "2026-08-30", want: 24_000},
		{name: "holiday overrides weekend", checkIn: "2026-08-28", checkOut: "2026-08-29", holidays: map[string]bool{"2026-08-28": true}, want: 13_500},
		{name: "seven night discount", checkIn: "2026-08-24", checkOut: "2026-08-31", want: 68_080},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			dateRange, err := domain.ParseDateRange(test.checkIn, test.checkOut, "Asia/Shanghai")
			if err != nil {
				t.Fatal(err)
			}
			got, err := policy.Quote(resource, dateRange, test.holidays)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("Quote() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestGuaranteeUsesMinimumAndPercentage(t *testing.T) {
	t.Parallel()
	policy := booking.DefaultPricePolicy()
	tests := []struct{ quote, want int64 }{{quote: 1, want: 10_000}, {quote: 30_000, want: 10_000}, {quote: 100_000, want: 30_000}, {quote: 200_000, want: 60_000}}
	for _, test := range tests {
		if got := policy.Guarantee(test.quote); got != test.want {
			t.Errorf("Guarantee(%d) = %d, want %d", test.quote, got, test.want)
		}
	}
}
