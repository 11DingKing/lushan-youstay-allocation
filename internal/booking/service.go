package booking

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/11DingKing/lushan-youstay-allocation/internal/clock"
	"github.com/11DingKing/lushan-youstay-allocation/internal/domain"
	"github.com/11DingKing/lushan-youstay-allocation/internal/identity"
	storesqlite "github.com/11DingKing/lushan-youstay-allocation/internal/store/sqlite"
)

type Store interface {
	GetProperty(context.Context, string) (domain.Property, error)
	GetResource(context.Context, string) (domain.Resource, error)
	GetStay(context.Context, string) (domain.Stay, error)
	GetStayByIdempotencyKey(context.Context, string) (domain.Stay, error)
	CreateHeldStay(context.Context, domain.Stay, []string, storesqlite.AuditEvent) error
	GuaranteeStay(context.Context, domain.Stay, domain.IdentityCheck, domain.Payment, storesqlite.AuditEvent) error
	TransitionStay(context.Context, domain.Stay, storesqlite.AuditEvent) error
	RecordPayment(context.Context, domain.Payment) (bool, error)
}

type Service struct {
	store   Store
	clock   clock.Clock
	prices  PricePolicy
	holdTTL time.Duration
}

func NewService(store Store, c clock.Clock, prices PricePolicy, holdTTL time.Duration) *Service {
	return &Service{store: store, clock: c, prices: prices, holdTTL: holdTTL}
}

type HoldRequest struct {
	GuestID, ResourceID, CheckInDate, CheckOutDate, IdempotencyKey, RequestID string
	PartySize                                                                 int
	Holidays                                                                  map[string]bool
}

func (s *Service) Hold(ctx context.Context, request HoldRequest) (domain.Stay, error) {
	if request.IdempotencyKey == "" {
		return domain.Stay{}, &domain.FieldError{Field: "idempotency_key", Message: "is required"}
	}
	existing, err := s.store.GetStayByIdempotencyKey(ctx, request.IdempotencyKey)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return domain.Stay{}, fmt.Errorf("check idempotency key: %w", err)
	}
	resource, err := s.store.GetResource(ctx, request.ResourceID)
	if err != nil {
		return domain.Stay{}, fmt.Errorf("get requested resource: %w", err)
	}
	property, err := s.store.GetProperty(ctx, resource.PropertyID)
	if err != nil {
		return domain.Stay{}, fmt.Errorf("get resource property: %w", err)
	}
	if !property.CanAcceptArrivals() {
		return domain.Stay{}, domain.ErrUnavailable
	}
	if !resource.CanHost(request.PartySize) {
		return domain.Stay{}, domain.ErrUnavailable
	}
	dateRange, err := domain.ParseDateRange(request.CheckInDate, request.CheckOutDate, property.Timezone)
	if err != nil {
		return domain.Stay{}, err
	}
	quote, err := s.prices.Quote(resource, dateRange, request.Holidays)
	if err != nil {
		return domain.Stay{}, err
	}
	now := s.clock.Now()
	expiry := now.Add(s.holdTTL)
	stay := domain.Stay{ID: identity.MustNew("stay"), GuestID: request.GuestID, ResourceID: resource.ID, Status: domain.StayHeld,
		CheckInDate: request.CheckInDate, CheckOutDate: request.CheckOutDate, PartySize: request.PartySize, QuotedCents: quote,
		HoldExpiresAt: &expiry, IdempotencyKey: request.IdempotencyKey, Version: 1, CreatedAt: now, UpdatedAt: now}
	event := audit(request.GuestID, request.RequestID, "stay.hold", "stay", stay.ID, "succeeded", now)
	if err := s.store.CreateHeldStay(ctx, stay, dateRange.NightStrings(), event); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return domain.Stay{}, &domain.ConflictError{Resource: resource.Code, Reason: "one or more nights are already allocated"}
		}
		return domain.Stay{}, fmt.Errorf("create held stay: %w", err)
	}
	return stay, nil
}

type GuaranteeRequest struct{ StayID, GuestID, DocumentDigest, Provider, ProviderEventID, RequestID, VerifiedBy string }

func (s *Service) Guarantee(ctx context.Context, request GuaranteeRequest) (domain.Stay, error) {
	stay, err := s.store.GetStay(ctx, request.StayID)
	if err != nil {
		return domain.Stay{}, err
	}
	now := s.clock.Now()
	if stay.GuestID != request.GuestID {
		return domain.Stay{}, domain.ErrForbidden
	}
	if !stay.HoldActive(now) {
		if stay.Status == domain.StayHeld {
			return domain.Stay{}, domain.ErrExpired
		}
		return domain.Stay{}, domain.ErrInvalidTransition
	}
	if len(request.DocumentDigest) < 16 {
		return domain.Stay{}, &domain.FieldError{Field: "document_digest", Message: "must be a one-way digest"}
	}
	guaranteed, err := stay.Transition(domain.StayGuaranteed, now)
	if err != nil {
		return domain.Stay{}, err
	}
	guaranteed.GuaranteeCents = s.prices.Guarantee(stay.QuotedCents)
	verifiedAt := now
	check := domain.IdentityCheck{ID: identity.MustNew("idc"), StayID: stay.ID, GuestID: stay.GuestID, DocumentDigest: request.DocumentDigest, Status: domain.IdentityVerified, VerifiedBy: &request.VerifiedBy, VerifiedAt: &verifiedAt, CreatedAt: now}
	payment := domain.Payment{ID: identity.MustNew("pay"), StayID: stay.ID, Provider: request.Provider, ProviderEventID: request.ProviderEventID, AmountCents: guaranteed.GuaranteeCents, Kind: domain.PaymentGuarantee, Status: domain.PaymentSucceeded, OccurredAt: now, CreatedAt: now}
	event := audit(request.VerifiedBy, request.RequestID, "stay.guarantee", "stay", stay.ID, "succeeded", now)
	if err := s.store.GuaranteeStay(ctx, guaranteed, check, payment, event); err != nil {
		return domain.Stay{}, fmt.Errorf("guarantee stay: %w", err)
	}
	return guaranteed, nil
}

func (s *Service) CheckIn(ctx context.Context, stayID, actorID, requestID string) (domain.Stay, error) {
	stay, err := s.store.GetStay(ctx, stayID)
	if err != nil {
		return domain.Stay{}, err
	}
	updated, err := stay.Transition(domain.StayCheckedIn, s.clock.Now())
	if err != nil {
		return domain.Stay{}, err
	}
	if err := s.store.TransitionStay(ctx, updated, audit(actorID, requestID, "stay.check_in", "stay", stay.ID, "succeeded", s.clock.Now())); err != nil {
		return domain.Stay{}, fmt.Errorf("check in: %w", err)
	}
	return updated, nil
}

func (s *Service) Cancel(ctx context.Context, stayID, actorID, requestID string) (domain.Stay, error) {
	stay, err := s.store.GetStay(ctx, stayID)
	if err != nil {
		return domain.Stay{}, err
	}
	updated, err := stay.Transition(domain.StayCancelled, s.clock.Now())
	if err != nil {
		return domain.Stay{}, err
	}
	if err := s.store.TransitionStay(ctx, updated, audit(actorID, requestID, "stay.cancel", "stay", stay.ID, "succeeded", s.clock.Now())); err != nil {
		return domain.Stay{}, err
	}
	return updated, nil
}

func (s *Service) ReceivePayment(ctx context.Context, p domain.Payment) (bool, error) {
	if p.Provider == "" || p.ProviderEventID == "" {
		return false, domain.ErrInvalid
	}
	if p.ID == "" {
		p.ID = identity.MustNew("pay")
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = s.clock.Now()
	}
	if p.OccurredAt.IsZero() {
		p.OccurredAt = p.CreatedAt
	}
	created, err := s.store.RecordPayment(ctx, p)
	if err != nil {
		return false, fmt.Errorf("record provider payment: %w", err)
	}
	return created, nil
}

func audit(actor, requestID, action, objectType, objectID, result string, now time.Time) storesqlite.AuditEvent {
	var actorID *string
	if actor != "" {
		actorID = &actor
	}
	if requestID == "" {
		requestID = "system"
	}
	return storesqlite.AuditEvent{ActorID: actorID, RequestID: requestID, Action: action, ObjectType: objectType, ObjectID: objectID, Result: result, DetailJSON: "{}", OccurredAt: now}
}
