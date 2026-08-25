package worker_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/11DingKing/lushan-youstay-allocation/internal/clock"
	"github.com/11DingKing/lushan-youstay-allocation/internal/domain"
	"github.com/11DingKing/lushan-youstay-allocation/internal/worker"
)

type startupFailureHoldStore struct {
	cancel context.CancelFunc
	err    error
}

func (s startupFailureHoldStore) ExpireDueHolds(context.Context, time.Time, int) (int, error) {
	s.cancel()
	return 0, s.err
}

func TestRunPropagatesInitialRecoveryFailure(t *testing.T) {
	sentinel := errors.New("recovery database unavailable")
	ctx, cancel := context.WithCancel(context.Background())
	tasks := &taskStore{byRole: map[string][]domain.OperationalTask{}, claimErr: map[string]error{}}
	runner := worker.NewRunner(clock.NewManual(time.Now()), startupFailureHoldStore{cancel: cancel, err: sentinel}, &cleaner{}, tasks, worker.NewDispatchHandler(), time.Hour, testLogger())
	err := runner.Run(ctx)
	if !errors.Is(err, sentinel) {
		t.Fatalf("initial recovery error = %v, want %v", err, sentinel)
	}
}
