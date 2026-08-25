package settlement

import (
	"context"
	"fmt"

	"github.com/11DingKing/lushan-youstay-allocation/internal/clock"
	"github.com/11DingKing/lushan-youstay-allocation/internal/domain"
	"github.com/11DingKing/lushan-youstay-allocation/internal/identity"
	storesqlite "github.com/11DingKing/lushan-youstay-allocation/internal/store/sqlite"
)

type Store interface {
	GetStay(context.Context, string) (domain.Stay, error)
	SumAcceptedDamage(context.Context, string) (int64, error)
	SumSucceededPayments(context.Context, string) (int64, error)
	CreateSettlement(context.Context, domain.RefundSettlement, storesqlite.AuditEvent) error
}
type Service struct {
	store Store
	clock clock.Clock
}

func NewService(store Store, c clock.Clock) *Service { return &Service{store: store, clock: c} }

func (s *Service) Start(ctx context.Context, stayID, actorID, requestID string) (domain.RefundSettlement, error) {
	stay, err := s.store.GetStay(ctx, stayID)
	if err != nil {
		return domain.RefundSettlement{}, err
	}
	if stay.Status != domain.StayCheckedOut {
		return domain.RefundSettlement{}, domain.ErrInvalidTransition
	}
	paid, err := s.store.SumSucceededPayments(ctx, stayID)
	if err != nil {
		return domain.RefundSettlement{}, fmt.Errorf("calculate paid amount: %w", err)
	}
	damage, err := s.store.SumAcceptedDamage(ctx, stayID)
	if err != nil {
		return domain.RefundSettlement{}, fmt.Errorf("calculate damage amount: %w", err)
	}
	now := s.clock.Now()
	result, err := domain.NewSettlement(identity.MustNew("set"), stayID, paid, damage, now)
	if err != nil {
		return domain.RefundSettlement{}, err
	}
	actor := actorID
	event := storesqlite.AuditEvent{ActorID: &actor, RequestID: requestID, Action: "settlement.start", ObjectType: "stay", ObjectID: stayID, Result: "succeeded", DetailJSON: "{}", OccurredAt: now}
	if err := s.store.CreateSettlement(ctx, result, event); err != nil {
		return domain.RefundSettlement{}, err
	}
	return result, nil
}
