package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/11DingKing/lushan-youstay-allocation/internal/auth"
	"github.com/11DingKing/lushan-youstay-allocation/internal/clock"
)

func TestCancelledLogoutDoesNotRevokeSession(t *testing.T) {
	store := newAuthStore(activeUser())
	service := auth.NewService(store, clock.NewManual(time.Now()), time.Hour)
	token, _, err := service.Login(context.Background(), "operator", "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := service.Logout(ctx, token); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled logout error = %v, want context cancellation", err)
	}
	if _, err := service.Authenticate(context.Background(), token); err != nil {
		t.Fatalf("cancelled logout revoked usable session: %v", err)
	}
}
