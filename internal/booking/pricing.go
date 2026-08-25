package booking

import (
	"fmt"
	"time"

	"github.com/11DingKing/lushan-youstay-allocation/internal/domain"
)

type PricePolicy struct {
	WeekendBasisPoints    int64
	HolidayBasisPoints    int64
	LongStayBasisPoints   int64
	LongStayMinimumNights int
	MinimumGuaranteeCents int64
	GuaranteeBasisPoints  int64
}

func DefaultPricePolicy() PricePolicy {
	return PricePolicy{WeekendBasisPoints: 12000, HolidayBasisPoints: 13500, LongStayBasisPoints: 9200,
		LongStayMinimumNights: 7, MinimumGuaranteeCents: 10000, GuaranteeBasisPoints: 3000}
}

func (p PricePolicy) Quote(resource domain.Resource, dateRange domain.DateRange, holidays map[string]bool) (int64, error) {
	if err := resource.Validate(); err != nil {
		return 0, err
	}
	if dateRange.NightCount() <= 0 {
		return 0, domain.ErrInvalid
	}
	total := int64(0)
	for _, night := range dateRange.Nights() {
		factor := int64(10000)
		if night.Weekday() == time.Friday || night.Weekday() == time.Saturday {
			factor = p.WeekendBasisPoints
		}
		if holidays[night.Format(domain.DateLayout)] {
			factor = p.HolidayBasisPoints
		}
		total += resource.BasePriceCents * factor / 10000
	}
	if dateRange.NightCount() >= p.LongStayMinimumNights {
		total = total * p.LongStayBasisPoints / 10000
	}
	if total < 0 {
		return 0, fmt.Errorf("quote overflow: %w", domain.ErrInvalid)
	}
	return total, nil
}

func (p PricePolicy) Guarantee(quote int64) int64 {
	value := quote * p.GuaranteeBasisPoints / 10000
	if value < p.MinimumGuaranteeCents {
		return p.MinimumGuaranteeCents
	}
	return value
}
