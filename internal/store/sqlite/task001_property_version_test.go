package sqlite_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/11DingKing/lushan-youstay-allocation/internal/domain"
)

func TestPropertyStatusConcurrentWritersRejectStaleVersion(t *testing.T) {
	store := openStore(t)
	now := time.Now().UTC()
	property := domain.Property{ID: "property-concurrent", Name: "Weather Gate", Zone: "Lushan", Timezone: "Asia/Shanghai", Status: domain.PropertyOpen, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateProperty(context.Background(), property); err != nil { t.Fatal(err) }
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, status := range []domain.PropertyStatus{domain.PropertyWeatherClosed, domain.PropertyMaintenanceClosed} {
		status := status
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- store.UpdatePropertyStatus(context.Background(), property.ID, domain.PropertyOpen, status, 1, now)
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	var success, stale int
	for err := range results {
		switch {
		case err == nil: success++
		case errors.Is(err, domain.ErrVersionConflict): stale++
		default: t.Fatalf("unexpected update error: %v", err)
		}
	}
	if success != 1 || stale != 1 { t.Fatalf("concurrent updates success=%d stale=%d, want one of each", success, stale) }
	loaded, err := store.GetProperty(context.Background(), property.ID)
	if err != nil { t.Fatal(err) }
	if loaded.Version != 2 || loaded.Status == domain.PropertyOpen { t.Fatalf("property after concurrent update = %#v", loaded) }
}
