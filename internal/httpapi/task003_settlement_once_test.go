package httpapi_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/11DingKing/lushan-youstay-allocation/internal/auth"
)

func TestSettlementStartIsSingleUseAfterCheckout(t *testing.T) {
	f := newAPIFixture(t)
	now := time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)
	if err := f.store.CreateUser(context.Background(), auth.User{ID: "guest-settle", Username: "guest-settle", PasswordHash: auth.HashPassword("secret"), DisplayName: "Guest", Role: auth.RoleGuest, Active: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	operator := f.login(t, "api-operator")
	hold := f.request(t, http.MethodPost, "/v1/stays/hold", operator, map[string]any{"guest_id": "guest-settle", "resource_id": "r1", "check_in": "2026-09-01", "check_out": "2026-09-02", "party_size": 1, "idempotency_key": "settle-once"})
	if hold.StatusCode != http.StatusCreated {
		t.Fatalf("hold status=%d body=%v", hold.StatusCode, decode(t, hold))
	}
	stayID, ok := decode(t, hold)["ID"].(string)
	if !ok || stayID == "" {
		t.Fatalf("hold response missing ID")
	}
	guarantee := f.request(t, http.MethodPost, "/v1/stays/"+stayID+"/guarantee", operator, map[string]any{"guest_id": "guest-settle", "document_digest": "0123456789abcdef", "provider": "mock", "provider_event_id": "settle-payment"})
	if guarantee.StatusCode != http.StatusOK {
		t.Fatalf("guarantee status=%d body=%v", guarantee.StatusCode, decode(t, guarantee))
	}
	for _, path := range []string{"/v1/stays/" + stayID + "/check-in", "/v1/stays/" + stayID + "/checkout"} {
		response := f.request(t, http.MethodPost, path, operator, nil)
		if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusAccepted {
			t.Fatalf("%s status=%d body=%v", path, response.StatusCode, decode(t, response))
		}
		response.Body.Close()
	}
	first := f.request(t, http.MethodPost, "/v1/stays/"+stayID+"/settlements", operator, nil)
	if first.StatusCode != http.StatusCreated {
		t.Fatalf("first settlement status=%d body=%v", first.StatusCode, decode(t, first))
	}
	first.Body.Close()
	second := f.request(t, http.MethodPost, "/v1/stays/"+stayID+"/settlements", operator, nil)
	if second.StatusCode == http.StatusCreated {
		t.Fatalf("second settlement unexpectedly created")
	}
	second.Body.Close()
	count, err := f.store.CountTable(context.Background(), "refund_settlements")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("settlement rows=%d, want one durable settlement", count)
	}
}
