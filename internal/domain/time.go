package domain

import (
	"fmt"
	"time"
)

const DateLayout = "2006-01-02"

type DateRange struct {
	CheckIn  time.Time
	CheckOut time.Time
}

func NewDateRange(checkIn, checkOut time.Time, location *time.Location) (DateRange, error) {
	if location == nil {
		return DateRange{}, &FieldError{Field: "timezone", Message: "is required"}
	}
	in := midnight(checkIn.In(location), location)
	out := midnight(checkOut.In(location), location)
	if !out.After(in) {
		return DateRange{}, &FieldError{Field: "check_out", Message: "must be after check_in"}
	}
	if out.Sub(in) > 90*24*time.Hour {
		return DateRange{}, &FieldError{Field: "check_out", Message: "stay cannot exceed 90 nights"}
	}
	return DateRange{CheckIn: in, CheckOut: out}, nil
}

func ParseDateRange(checkIn, checkOut, timezone string) (DateRange, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return DateRange{}, fmt.Errorf("load property timezone: %w", err)
	}
	in, err := time.ParseInLocation(DateLayout, checkIn, loc)
	if err != nil {
		return DateRange{}, &FieldError{Field: "check_in", Message: "must use YYYY-MM-DD"}
	}
	out, err := time.ParseInLocation(DateLayout, checkOut, loc)
	if err != nil {
		return DateRange{}, &FieldError{Field: "check_out", Message: "must use YYYY-MM-DD"}
	}
	return NewDateRange(in, out, loc)
}

func (r DateRange) Nights() []time.Time {
	result := make([]time.Time, 0, r.NightCount())
	for day := r.CheckIn; day.Before(r.CheckOut); day = day.AddDate(0, 0, 1) {
		result = append(result, day)
	}
	return result
}

func (r DateRange) NightStrings() []string {
	nights := r.Nights()
	result := make([]string, len(nights))
	for i, night := range nights {
		result[i] = night.Format(DateLayout)
	}
	return result
}

func (r DateRange) NightCount() int {
	return int(r.CheckOut.Sub(r.CheckIn).Hours() / 24)
}

func (r DateRange) Overlaps(other DateRange) bool {
	return r.CheckIn.Before(other.CheckOut) && other.CheckIn.Before(r.CheckOut)
}

func (r DateRange) Contains(day time.Time) bool {
	return !day.Before(r.CheckIn) && day.Before(r.CheckOut)
}

func midnight(value time.Time, location *time.Location) time.Time {
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, location)
}
