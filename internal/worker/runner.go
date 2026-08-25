package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/11DingKing/lushan-youstay-allocation/internal/clock"
	"github.com/11DingKing/lushan-youstay-allocation/internal/domain"
)

type HoldStore interface {
	ExpireDueHolds(context.Context, time.Time, int) (int, error)
}
type SessionCleaner interface {
	RemoveExpired(context.Context) (int64, error)
}
type TaskStore interface {
	ClaimDueTasks(context.Context, string, time.Time, int) ([]domain.OperationalTask, error)
	SaveTask(context.Context, domain.OperationalTask) error
}
type TaskHandler interface {
	Handle(context.Context, domain.OperationalTask) error
}

type Runner struct {
	clock       clock.Clock
	holds       HoldStore
	sessions    SessionCleaner
	tasks       TaskStore
	handler     TaskHandler
	interval    time.Duration
	logger      *slog.Logger
	maxAttempts int
	backoff     time.Duration
	roles       []string
}

func NewRunner(c clock.Clock, holds HoldStore, sessions SessionCleaner, tasks TaskStore, handler TaskHandler, interval time.Duration, logger *slog.Logger) *Runner {
	return &Runner{clock: c, holds: holds, sessions: sessions, tasks: tasks, handler: handler, interval: interval, logger: logger, maxAttempts: 4, backoff: time.Second, roles: []string{"cleaner", "maintenance", "camp_guard"}}
}

func (r *Runner) Run(ctx context.Context) error {
	if err := r.Tick(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		return fmt.Errorf("worker initial tick: %w", err)
	}
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := r.Tick(ctx); err != nil {
				r.logger.Error("worker tick failed", "error", err)
			}
		}
	}
}

func (r *Runner) Tick(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var failures []error
	if count, err := r.holds.ExpireDueHolds(ctx, r.clock.Now(), 100); err != nil {
		failures = append(failures, fmt.Errorf("expire holds: %w", err))
	} else if count > 0 {
		r.logger.Info("expired stale holds", "count", count)
	}
	if count, err := r.sessions.RemoveExpired(ctx); err != nil {
		failures = append(failures, fmt.Errorf("clean sessions: %w", err))
	} else if count > 0 {
		r.logger.Info("removed expired sessions", "count", count)
	}
	for _, role := range r.roles {
		if err := r.processRole(ctx, role); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (r *Runner) processRole(ctx context.Context, role string) error {
	tasks, err := r.tasks.ClaimDueTasks(ctx, role, r.clock.Now(), 20)
	if err != nil {
		return fmt.Errorf("claim %s tasks: %w", role, err)
	}
	var failures []error
	for _, task := range tasks {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := r.handler.Handle(ctx, task); err != nil {
			message := err.Error()
			task.LastError = &message
			task.UpdatedAt = r.clock.Now()
			if task.Attempts >= r.maxAttempts {
				task.Status = domain.TaskFailed
			} else {
				task.Status = domain.TaskPending
				task.AvailableAt = r.clock.Now().Add(r.backoff * time.Duration(task.Attempts))
			}
			if saveErr := r.tasks.SaveTask(ctx, task); saveErr != nil {
				failures = append(failures, fmt.Errorf("save failed task %s: %w", task.ID, saveErr))
			}
			continue
		}
		task.Status = domain.TaskCompleted
		task.LastError = nil
		task.UpdatedAt = r.clock.Now()
		if err := r.tasks.SaveTask(ctx, task); err != nil {
			failures = append(failures, fmt.Errorf("complete task %s: %w", task.ID, err))
		}
	}
	return errors.Join(failures...)
}

type DispatchHandler struct {
	mu       sync.RWMutex
	handlers map[domain.TaskKind]func(context.Context, domain.OperationalTask) error
}

func NewDispatchHandler() *DispatchHandler {
	return &DispatchHandler{handlers: make(map[domain.TaskKind]func(context.Context, domain.OperationalTask) error)}
}
func (d *DispatchHandler) Register(kind domain.TaskKind, handler func(context.Context, domain.OperationalTask) error) {
	d.mu.Lock()
	d.handlers[kind] = handler
	d.mu.Unlock()
}
func (d *DispatchHandler) Handle(ctx context.Context, task domain.OperationalTask) error {
	d.mu.RLock()
	handler := d.handlers[task.Kind]
	d.mu.RUnlock()
	if handler == nil {
		return fmt.Errorf("no handler for task kind %s", task.Kind)
	}
	return handler(ctx, task)
}
