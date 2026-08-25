package operations

import (
	"context"
	"fmt"
	"time"

	"github.com/11DingKing/lushan-youstay-allocation/internal/clock"
	"github.com/11DingKing/lushan-youstay-allocation/internal/domain"
	"github.com/11DingKing/lushan-youstay-allocation/internal/identity"
	storesqlite "github.com/11DingKing/lushan-youstay-allocation/internal/store/sqlite"
)

type Store interface {
	GetStay(context.Context, string) (domain.Stay, error)
	GetTask(context.Context, string) (domain.OperationalTask, error)
	CreateTask(context.Context, domain.OperationalTask) error
	SaveTask(context.Context, domain.OperationalTask) error
	CheckoutAndCreateCleaning(context.Context, domain.Stay, domain.OperationalTask, storesqlite.AuditEvent) error
	CreateDamageClaim(context.Context, domain.DamageClaim) error
}

type Service struct {
	store Store
	clock clock.Clock
}

func NewService(store Store, c clock.Clock) *Service { return &Service{store: store, clock: c} }

func (s *Service) Checkout(ctx context.Context, stayID, actorID, requestID string) (domain.OperationalTask, error) {
	stay, err := s.store.GetStay(ctx, stayID)
	if err != nil {
		return domain.OperationalTask{}, err
	}
	updated, err := stay.Transition(domain.StayCheckedOut, s.clock.Now())
	if err != nil {
		return domain.OperationalTask{}, err
	}
	now := s.clock.Now()
	task := domain.OperationalTask{ID: identity.MustNew("task"), StayID: &stay.ID, ResourceID: stay.ResourceID, Kind: domain.TaskCleaning, Status: domain.TaskPending, AssignedRole: "cleaner", AvailableAt: now, CreatedAt: now, UpdatedAt: now}
	actor := actorID
	event := storesqlite.AuditEvent{ActorID: &actor, RequestID: requestID, Action: "stay.checkout", ObjectType: "stay", ObjectID: stay.ID, Result: "succeeded", DetailJSON: "{}", OccurredAt: now}
	if err := s.store.CheckoutAndCreateCleaning(ctx, updated, task, event); err != nil {
		return domain.OperationalTask{}, fmt.Errorf("checkout and schedule cleaning: %w", err)
	}
	return task, nil
}

func (s *Service) CompleteTask(ctx context.Context, taskID string) (domain.OperationalTask, error) {
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return domain.OperationalTask{}, err
	}
	updated, err := task.Complete(s.clock.Now())
	if err != nil {
		return domain.OperationalTask{}, err
	}
	if err := s.store.SaveTask(ctx, updated); err != nil {
		return domain.OperationalTask{}, err
	}
	return updated, nil
}

func (s *Service) FailTask(ctx context.Context, taskID string, cause error, maxAttempts int, backoff time.Duration) (domain.OperationalTask, error) {
	if cause == nil {
		return domain.OperationalTask{}, domain.ErrInvalid
	}
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return domain.OperationalTask{}, err
	}
	if task.Status != domain.TaskClaimed {
		return domain.OperationalTask{}, domain.ErrInvalidTransition
	}
	updated := task.Retry(s.clock.Now(), cause, maxAttempts, backoff)
	if err := s.store.SaveTask(ctx, updated); err != nil {
		return domain.OperationalTask{}, err
	}
	return updated, nil
}

func (s *Service) AssessDamage(ctx context.Context, stayID, taskID, actorID, description string, amount int64) (domain.DamageClaim, error) {
	if description == "" || amount < 0 {
		return domain.DamageClaim{}, domain.ErrInvalid
	}
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return domain.DamageClaim{}, err
	}
	if task.StayID == nil || *task.StayID != stayID || task.Kind != domain.TaskInspection {
		return domain.DamageClaim{}, domain.ErrConflict
	}
	now := s.clock.Now()
	claim := domain.DamageClaim{ID: identity.MustNew("dmg"), StayID: stayID, TaskID: taskID, AssessedBy: actorID, Description: description, AmountCents: amount, Status: domain.DamageDraft, CreatedAt: now, UpdatedAt: now}
	if err := s.store.CreateDamageClaim(ctx, claim); err != nil {
		return domain.DamageClaim{}, err
	}
	return claim, nil
}
