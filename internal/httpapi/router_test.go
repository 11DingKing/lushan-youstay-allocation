package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/11DingKing/lushan-youstay-allocation/internal/auth"
	"github.com/11DingKing/lushan-youstay-allocation/internal/booking"
	"github.com/11DingKing/lushan-youstay-allocation/internal/clock"
	"github.com/11DingKing/lushan-youstay-allocation/internal/domain"
	"github.com/11DingKing/lushan-youstay-allocation/internal/httpapi"
	"github.com/11DingKing/lushan-youstay-allocation/internal/operations"
	"github.com/11DingKing/lushan-youstay-allocation/internal/settlement"
	storesqlite "github.com/11DingKing/lushan-youstay-allocation/internal/store/sqlite"
)

type apiFixture struct {
	handler http.Handler
	store   *storesqlite.Store
}

func newAPIFixture(t *testing.T) *apiFixture {
	t.Helper()
	ctx := context.Background()
	store, err := storesqlite.Open(ctx, filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)
	users := []auth.User{{ID: "operator-test", Username: "api-operator", PasswordHash: auth.HashPassword("secret"), DisplayName: "Operator", Role: auth.RoleOperator, Active: true, CreatedAt: now, UpdatedAt: now}, {ID: "cleaner-test", Username: "api-cleaner", PasswordHash: auth.HashPassword("secret"), DisplayName: "Cleaner", Role: auth.RoleCleaner, Active: true, CreatedAt: now, UpdatedAt: now}}
	for _, user := range users {
		if err := store.CreateUser(ctx, user); err != nil {
			t.Fatal(err)
		}
	}
	property := domain.Property{ID: "p1", Name: "YouStay", Zone: "Lushan", Timezone: "Asia/Shanghai", Status: domain.PropertyOpen, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateProperty(ctx, property); err != nil {
		t.Fatal(err)
	}
	resource := domain.Resource{ID: "r1", PropertyID: property.ID, Code: "M201", Name: "Mountain", Kind: domain.ResourceMountainRoom, Capacity: 2, BasePriceCents: 50_000, Status: domain.ResourceOpen, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateResource(ctx, resource); err != nil {
		t.Fatal(err)
	}
	manual := clock.NewManual(now)
	authService := auth.NewService(store, manual, time.Hour)
	handler := httpapi.New(httpapi.Dependencies{Auth: authService, Booking: booking.NewService(store, manual, booking.DefaultPricePolicy(), 15*time.Minute), Operations: operations.NewService(store, manual), Settlement: settlement.NewService(store, manual), Store: store, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	fixture := &apiFixture{handler: handler, store: store}
	t.Cleanup(func() { _ = store.Close() })
	return fixture
}

func (f *apiFixture) request(t *testing.T, method, path, token string, body any) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, "http://youstay.local"+path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)
	return recorder.Result()
}

func decode(t *testing.T, response *http.Response) map[string]any {
	t.Helper()
	defer response.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode status %d: %v", response.StatusCode, err)
	}
	return body
}

func (f *apiFixture) login(t *testing.T, username string) string {
	t.Helper()
	response := f.request(t, http.MethodPost, "/v1/auth/login", "", map[string]any{"username": username, "password": "secret"})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login status=%d body=%v", response.StatusCode, decode(t, response))
	}
	body := decode(t, response)
	token, ok := body["token"].(string)
	if !ok || token == "" {
		t.Fatalf("login body=%v", body)
	}
	return token
}

func TestHealthEndpointsExposeLivenessAndDependencyReadiness(t *testing.T) {
	f := newAPIFixture(t)
	for _, path := range []string{"/health/live", "/health/ready"} {
		response := f.request(t, http.MethodGet, path, "", nil)
		if response.StatusCode != http.StatusOK {
			t.Errorf("GET %s status=%d", path, response.StatusCode)
		}
		body := decode(t, response)
		if body["status"] == nil {
			t.Errorf("GET %s body=%v", path, body)
		}
		if response.Header.Get("X-Request-ID") == "" {
			t.Errorf("GET %s missing request ID", path)
		}
	}
}

func TestLoginReturnsTokenAndRoleWithoutPasswordMaterial(t *testing.T) {
	f := newAPIFixture(t)
	response := f.request(t, http.MethodPost, "/v1/auth/login", "", map[string]any{"username": "api-operator", "password": "secret"})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", response.StatusCode)
	}
	body := decode(t, response)
	if body["token"] == "" {
		t.Fatalf("body=%v", body)
	}
	encoded, _ := json.Marshal(body)
	if bytes.Contains(encoded, []byte("password")) {
		t.Fatalf("password material leaked: %s", encoded)
	}
	user := body["user"].(map[string]any)
	if user["role"] != "operator" {
		t.Fatalf("user=%v", user)
	}
}

func TestLoginValidationAndCredentialFailuresUseStableErrors(t *testing.T) {
	f := newAPIFixture(t)
	tests := []struct {
		name string
		body any
		want int
		code string
	}{{name: "unknown field", body: map[string]any{"username": "api-operator", "password": "secret", "extra": true}, want: http.StatusBadRequest, code: "invalid_request"}, {name: "wrong password", body: map[string]any{"username": "api-operator", "password": "wrong"}, want: http.StatusUnauthorized, code: "unauthorized"}}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			response := f.request(t, http.MethodPost, "/v1/auth/login", "", test.body)
			if response.StatusCode != test.want {
				t.Fatalf("status=%d", response.StatusCode)
			}
			body := decode(t, response)
			apiError := body["error"].(map[string]any)
			if apiError["code"] != test.code || apiError["request_id"] == "" {
				t.Fatalf("error=%v", apiError)
			}
		})
	}
}

func TestProtectedRoutesRequireBearerSession(t *testing.T) {
	f := newAPIFixture(t)
	for _, token := range []string{"", "not-a-session"} {
		response := f.request(t, http.MethodGet, "/v1/resources", token, nil)
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("token %q status=%d", token, response.StatusCode)
		}
		body := decode(t, response)
		if body["error"].(map[string]any)["code"] != "unauthorized" {
			t.Fatalf("body=%v", body)
		}
	}
}

func TestResourceListingSupportsTypedFiltersAndPagination(t *testing.T) {
	f := newAPIFixture(t)
	token := f.login(t, "api-operator")
	response := f.request(t, http.MethodGet, "/v1/resources?property_id=p1&kind=mountain_room&status=open&limit=10", token, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%v", response.StatusCode, decode(t, response))
	}
	body := decode(t, response)
	if body["total"].(float64) != 1 {
		t.Fatalf("body=%v", body)
	}
	items := body["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["Code"] != "M201" {
		t.Fatalf("items=%v", items)
	}
}

func TestRoleMiddlewareRejectsCleanerBookingMutation(t *testing.T) {
	f := newAPIFixture(t)
	token := f.login(t, "api-cleaner")
	response := f.request(t, http.MethodPost, "/v1/stays/hold", token, map[string]any{"guest_id": "operator-test", "resource_id": "r1", "check_in": "2026-09-01", "check_out": "2026-09-02", "party_size": 1, "idempotency_key": "role-test"})
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d body=%v", response.StatusCode, decode(t, response))
	}
	body := decode(t, response)
	if body["error"].(map[string]any)["code"] != "forbidden" {
		t.Fatalf("body=%v", body)
	}
	count, _ := f.store.CountTable(context.Background(), "stays")
	if count != 0 {
		t.Fatalf("forbidden request created %d stays", count)
	}
}

func TestLogoutRevokesTokenForSubsequentRequests(t *testing.T) {
	f := newAPIFixture(t)
	token := f.login(t, "api-operator")
	response := f.request(t, http.MethodPost, "/v1/auth/logout", token, nil)
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status=%d", response.StatusCode)
	}
	response = f.request(t, http.MethodGet, "/v1/resources", token, nil)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("post-logout status=%d", response.StatusCode)
	}
	response.Body.Close()
}

func TestRequestIDIsAcceptedFromCallerAndReturnedOnErrors(t *testing.T) {
	f := newAPIFixture(t)
	request := httptest.NewRequest(http.MethodGet, "http://youstay.local/v1/resources", nil)
	request.Header.Set("X-Request-ID", "external-request-42")
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)
	response := recorder.Result()
	if response.Header.Get("X-Request-ID") != "external-request-42" {
		t.Fatalf("header=%q", response.Header.Get("X-Request-ID"))
	}
	body := decode(t, response)
	if body["error"].(map[string]any)["request_id"] != "external-request-42" {
		t.Fatalf("body=%v", body)
	}
}
