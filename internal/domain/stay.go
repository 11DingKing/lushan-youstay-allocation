package domain

import "time"

type StayStatus string

const (
	StayInquiry    StayStatus = "inquiry"
	StayHeld       StayStatus = "held"
	StayGuaranteed StayStatus = "guaranteed"
	StayCheckedIn  StayStatus = "checked_in"
	StayCheckedOut StayStatus = "checked_out"
	StaySettling   StayStatus = "settling"
	StaySettled    StayStatus = "settled"
	StayCancelled  StayStatus = "cancelled"
	StayExpired    StayStatus = "expired"
	StayRelocated  StayStatus = "relocated"
)

type Stay struct {
	ID             string
	GuestID        string
	ResourceID     string
	Status         StayStatus
	CheckInDate    string
	CheckOutDate   string
	PartySize      int
	QuotedCents    int64
	GuaranteeCents int64
	HoldExpiresAt  *time.Time
	IdempotencyKey string
	Version        int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (s Stay) Validate() error {
	if err := Require(s.ID != "", "id", "is required"); err != nil {
		return err
	}
	if err := Require(s.GuestID != "", "guest_id", "is required"); err != nil {
		return err
	}
	if err := Require(s.ResourceID != "", "resource_id", "is required"); err != nil {
		return err
	}
	if err := Require(s.PartySize > 0, "party_size", "must be positive"); err != nil {
		return err
	}
	if err := Require(s.QuotedCents >= 0, "quoted_cents", "cannot be negative"); err != nil {
		return err
	}
	if err := Require(s.IdempotencyKey != "", "idempotency_key", "is required"); err != nil {
		return err
	}
	switch s.Status {
	case StayInquiry, StayHeld, StayGuaranteed, StayCheckedIn, StayCheckedOut,
		StaySettling, StaySettled, StayCancelled, StayExpired, StayRelocated:
		return nil
	default:
		return &FieldError{Field: "status", Message: "is not recognized"}
	}
}

func (s Stay) Range(timezone string) (DateRange, error) {
	return ParseDateRange(s.CheckInDate, s.CheckOutDate, timezone)
}

func (s Stay) HoldActive(now time.Time) bool {
	return s.Status == StayHeld && s.HoldExpiresAt != nil && now.Before(*s.HoldExpiresAt)
}

func (s Stay) Transition(to StayStatus, now time.Time) (Stay, error) {
	allowed := map[StayStatus]map[StayStatus]bool{
		StayInquiry:    {StayHeld: true, StayCancelled: true},
		StayHeld:       {StayGuaranteed: true, StayCancelled: true, StayExpired: true},
		StayGuaranteed: {StayCheckedIn: true, StayCancelled: true, StayRelocated: true},
		StayCheckedIn:  {StayCheckedOut: true, StayRelocated: true},
		StayCheckedOut: {StaySettling: true},
		StaySettling:   {StaySettled: true},
		StayRelocated:  {StayCheckedIn: true, StayCheckedOut: true},
	}
	if s.Status == to {
		return s, nil
	}
	if !allowed[s.Status][to] {
		return Stay{}, &StateError{Entity: "stay", From: string(s.Status), To: string(to)}
	}
	s.Status = to
	s.Version++
	s.UpdatedAt = now.UTC()
	if to == StayGuaranteed || to == StayCancelled || to == StayExpired {
		s.HoldExpiresAt = nil
	}
	return s, nil
}

type IdentityStatus string

const (
	IdentityPending  IdentityStatus = "pending"
	IdentityVerified IdentityStatus = "verified"
	IdentityRejected IdentityStatus = "rejected"
)

type IdentityCheck struct {
	ID             string
	StayID         string
	GuestID        string
	DocumentDigest string
	Status         IdentityStatus
	VerifiedBy     *string
	VerifiedAt     *time.Time
	CreatedAt      time.Time
}

type PaymentKind string
type PaymentStatus string

const (
	PaymentGuarantee PaymentKind   = "guarantee"
	PaymentCharge    PaymentKind   = "charge"
	PaymentRefund    PaymentKind   = "refund"
	PaymentPending   PaymentStatus = "pending"
	PaymentSucceeded PaymentStatus = "succeeded"
	PaymentFailed    PaymentStatus = "failed"
)

type Payment struct {
	ID              string
	StayID          string
	Provider        string
	ProviderEventID string
	AmountCents     int64
	Kind            PaymentKind
	Status          PaymentStatus
	OccurredAt      time.Time
	CreatedAt       time.Time
}
