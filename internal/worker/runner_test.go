package worker_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/11DingKing/lushan-youstay-allocation/internal/clock"
	"github.com/11DingKing/lushan-youstay-allocation/internal/domain"
	"github.com/11DingKing/lushan-youstay-allocation/internal/worker"
)

type holdStore struct {
	mu    sync.Mutex
	calls int
	count int
	err   error
	at    time.Time
}

func (s *holdStore) ExpireDueHolds(ctx context.Context, at time.Time, limit int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.at = at
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return s.count, s.err
}

type cleaner struct {
	mu    sync.Mutex
	calls int
	count int64
	err   error
}

func (s *cleaner) RemoveExpired(ctx context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return s.count, s.err
}

type taskStore struct {
	mu       sync.Mutex
	byRole   map[string][]domain.OperationalTask
	saved    []domain.OperationalTask
	claimErr map[string]error
	saveErr  error
}

func (s *taskStore) ClaimDueTasks(ctx context.Context, role string, now time.Time, limit int) ([]domain.OperationalTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.claimErr[role]; err != nil {
		return nil, err
	}
	items := append([]domain.OperationalTask(nil), s.byRole[role]...)
	delete(s.byRole, role)
	return items, nil
}
func (s *taskStore) SaveTask(ctx context.Context, task domain.OperationalTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saved = append(s.saved, task)
	return nil
}

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestTickRestoresAllPersistedWorkClasses(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC)
	holds := &holdStore{count: 3}
	sessions := &cleaner{count: 2}
	tasks := &taskStore{byRole: map[string][]domain.OperationalTask{}, claimErr: map[string]error{}}
	dispatch := worker.NewDispatchHandler()
	for _, kind := range []domain.TaskKind{domain.TaskCleaning, domain.TaskInspection, domain.TaskRepair, domain.TaskRelocation} {
		kind := kind
		dispatch.Register(kind, func(ctx context.Context, task domain.OperationalTask) error { return nil })
	}
	tasks.byRole["cleaner"] = []domain.OperationalTask{{ID: "clean", Kind: domain.TaskCleaning, Status: domain.TaskClaimed, Attempts: 1}}
	tasks.byRole["maintenance"] = []domain.OperationalTask{{ID: "repair", Kind: domain.TaskRepair, Status: domain.TaskClaimed, Attempts: 1}}
	tasks.byRole["camp_guard"] = []domain.OperationalTask{{ID: "relocate", Kind: domain.TaskRelocation, Status: domain.TaskClaimed, Attempts: 1}}
	runner := worker.NewRunner(clock.NewManual(now), holds, sessions, tasks, dispatch, time.Hour, testLogger())
	if err := runner.Tick(context.Background()); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if holds.calls != 1 || !holds.at.Equal(now) {
		t.Fatalf("holds calls=%d at=%s", holds.calls, holds.at)
	}
	if sessions.calls != 1 {
		t.Fatalf("session calls=%d", sessions.calls)
	}
	tasks.mu.Lock()
	defer tasks.mu.Unlock()
	if len(tasks.saved) != 3 {
		t.Fatalf("saved tasks=%d", len(tasks.saved))
	}
	for _, task := range tasks.saved {
		if task.Status != domain.TaskCompleted {
			t.Errorf("task %s status=%s", task.ID, task.Status)
		}
	}
}

func TestTickRetriesTemporaryTaskFailureWithBackoff(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC)
	tasks := &taskStore{byRole: map[string][]domain.OperationalTask{"cleaner": {{ID: "clean", Kind: domain.TaskCleaning, Status: domain.TaskClaimed, Attempts: 2}}}, claimErr: map[string]error{}}
	dispatch := worker.NewDispatchHandler()
	sentinel := errors.New("cleaning device offline")
	dispatch.Register(domain.TaskCleaning, func(context.Context, domain.OperationalTask) error { return sentinel })
	runner := worker.NewRunner(clock.NewManual(now), &holdStore{}, &cleaner{}, tasks, dispatch, time.Hour, testLogger())
	if err := runner.Tick(context.Background()); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	tasks.mu.Lock()
	defer tasks.mu.Unlock()
	if len(tasks.saved) != 1 {
		t.Fatalf("saved=%d", len(tasks.saved))
	}
	saved := tasks.saved[0]
	if saved.Status != domain.TaskPending {
		t.Fatalf("status=%s", saved.Status)
	}
	if saved.LastError == nil || *saved.LastError != sentinel.Error() {
		t.Fatalf("last error=%v", saved.LastError)
	}
	if !saved.AvailableAt.After(now) {
		t.Fatalf("available_at=%s", saved.AvailableAt)
	}
}

func TestTickMarksRepeatedFailurePermanent(t *testing.T) {
	t.Parallel()
	tasks := &taskStore{byRole: map[string][]domain.OperationalTask{"maintenance": {{ID: "repair", Kind: domain.TaskRepair, Status: domain.TaskClaimed, Attempts: 4}}}, claimErr: map[string]error{}}
	dispatch := worker.NewDispatchHandler()
	dispatch.Register(domain.TaskRepair, func(context.Context, domain.OperationalTask) error { return errors.New("part unavailable") })
	runner := worker.NewRunner(clock.NewManual(time.Now()), &holdStore{}, &cleaner{}, tasks, dispatch, time.Hour, testLogger())
	if err := runner.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	tasks.mu.Lock()
	defer tasks.mu.Unlock()
	if got := tasks.saved[0].Status; got != domain.TaskFailed {
		t.Fatalf("status=%s", got)
	}
}

func TestTickJoinsIndependentSubsystemErrors(t *testing.T) {
	t.Parallel()
	holdErr := errors.New("hold database busy")
	sessionErr := errors.New("session database busy")
	claimErr := errors.New("task database busy")
	tasks := &taskStore{byRole: map[string][]domain.OperationalTask{}, claimErr: map[string]error{"cleaner": claimErr}}
	runner := worker.NewRunner(clock.NewManual(time.Now()), &holdStore{err: holdErr}, &cleaner{err: sessionErr}, tasks, worker.NewDispatchHandler(), time.Hour, testLogger())
	err := runner.Tick(context.Background())
	for _, want := range []error{holdErr, sessionErr, claimErr} {
		if !errors.Is(err, want) {
			t.Errorf("Tick() error %v does not wrap %v", err, want)
		}
	}
}

func TestTickStopsBeforeWorkWhenContextCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	holds := &holdStore{}
	sessions := &cleaner{}
	runner := worker.NewRunner(clock.NewManual(time.Now()), holds, sessions, &taskStore{byRole: map[string][]domain.OperationalTask{}, claimErr: map[string]error{}}, worker.NewDispatchHandler(), time.Hour, testLogger())
	err := runner.Tick(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Tick() error=%v", err)
	}
	if holds.calls != 0 || sessions.calls != 0 {
		t.Fatalf("cancelled tick performed work")
	}
}

func TestRunExecutesInitialTickAndStopsGracefully(t *testing.T) {
	holds := &holdStore{}
	sessions := &cleaner{}
	tasks := &taskStore{byRole: map[string][]domain.OperationalTask{}, claimErr: map[string]error{}}
	runner := worker.NewRunner(clock.NewManual(time.Now()), holds, sessions, tasks, worker.NewDispatchHandler(), time.Hour, testLogger())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	deadline := time.After(time.Second)
	for {
		holds.mu.Lock()
		calls := holds.calls
		holds.mu.Unlock()
		if calls > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("initial tick did not run")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop")
	}
}

func TestDispatchHandlerRoutesByTaskKind(t *testing.T) {
	t.Parallel()
	dispatch := worker.NewDispatchHandler()
	called := false
	dispatch.Register(domain.TaskInspection, func(ctx context.Context, task domain.OperationalTask) error {
		called = true
		if task.ID != "inspect" {
			t.Errorf("task ID=%s", task.ID)
		}
		return nil
	})
	if err := dispatch.Handle(context.Background(), domain.OperationalTask{ID: "inspect", Kind: domain.TaskInspection}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("registered handler not called")
	}
	if err := dispatch.Handle(context.Background(), domain.OperationalTask{Kind: domain.TaskRepair}); err == nil {
		t.Fatal("missing handler error=nil")
	}
}
