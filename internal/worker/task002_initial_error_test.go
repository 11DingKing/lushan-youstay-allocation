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

func TestRunKeepsRecoveringAfterInitialFailure(t *testing.T) {
	sentinel := errors.New("recovery database unavailable")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	tasks := &taskStore{byRole: map[string][]domain.OperationalTask{}, claimErr: map[string]error{}}
	runner := worker.NewRunner(clock.NewManual(time.Now()), &holdStore{err: sentinel}, &cleaner{}, tasks, worker.NewDispatchHandler(), time.Hour, testLogger())
	err := runner.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("worker stopped after transient recovery error = %v, want %v", err, context.Canceled)
	}
}
