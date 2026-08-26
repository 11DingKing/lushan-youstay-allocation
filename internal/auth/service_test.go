package auth_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/11DingKing/lushan-youstay-allocation/internal/auth"
	"github.com/11DingKing/lushan-youstay-allocation/internal/clock"
	"github.com/11DingKing/lushan-youstay-allocation/internal/domain"
)

type authStore struct {
	mu       sync.Mutex
	users    map[string]auth.User
	sessions map[string]auth.Session
	fail     error
}

func newAuthStore(users ...auth.User) *authStore {
	store := &authStore{users: make(map[string]auth.User), sessions: make(map[string]auth.Session)}
	for _, user := range users {
		store.users[user.ID] = user
	}
	return store
}

func (s *authStore) FindUserByUsername(ctx context.Context, username string) (auth.User, error) {
	if err := ctx.Err(); err != nil {
		return auth.User{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return auth.User{}, s.fail
	}
	for _, user := range s.users {
		if user.Username == username {
			return user, nil
		}
	}
	return auth.User{}, domain.ErrNotFound
}
func (s *authStore) FindUserByID(ctx context.Context, id string) (auth.User, error) {
	if err := ctx.Err(); err != nil {
		return auth.User{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.users[id]
	if !ok {
		return auth.User{}, domain.ErrNotFound
	}
	return user, nil
}
func (s *authStore) CreateSession(ctx context.Context, session auth.Session) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	s.sessions[session.TokenHash] = session
	return nil
}
func (s *authStore) FindSessionByTokenHash(ctx context.Context, digest string) (auth.Session, error) {
	if err := ctx.Err(); err != nil {
		return auth.Session{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[digest]
	if !ok {
		return auth.Session{}, domain.ErrNotFound
	}
	return session, nil
}
func (s *authStore) TouchSession(ctx context.Context, id string, seen time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, session := range s.sessions {
		if session.ID == id {
			session.LastSeenAt = seen
			s.sessions[key] = session
			return nil
		}
	}
	return domain.ErrNotFound
}
func (s *authStore) RevokeSession(ctx context.Context, id string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, session := range s.sessions {
		if session.ID == id {
			session.RevokedAt = &now
			s.sessions[key] = session
			return nil
		}
	}
	return domain.ErrNotFound
}
func (s *authStore) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var count int64
	for key, session := range s.sessions {
		if !now.Before(session.ExpiresAt) {
			delete(s.sessions, key)
			count++
		}
	}
	return count, nil
}

func activeUser() auth.User {
	return auth.User{ID: "u1", Username: "operator", PasswordHash: auth.HashPassword("correct horse"), DisplayName: "Operator", Role: auth.RoleOperator, Active: true}
}

func TestPasswordHashNeverStoresPlaintextAndVerifiesExactly(t *testing.T) {
	t.Parallel()
	encoded := auth.HashPassword("mountain-secret")
	if encoded == "mountain-secret" {
		t.Fatal("HashPassword returned plaintext")
	}
	if len(encoded) != 71 {
		t.Fatalf("encoded length = %d", len(encoded))
	}
	if !auth.CheckPassword(encoded, "mountain-secret") {
		t.Fatal("correct password rejected")
	}
	for _, candidate := range []string{"", "mountain-secre", "Mountain-secret", "mountain-secret ", encoded} {
		if auth.CheckPassword(encoded, candidate) {
			t.Errorf("incorrect password %q accepted", candidate)
		}
	}
	if auth.CheckPassword("bcrypt:not-supported", "mountain-secret") {
		t.Fatal("unknown hash format accepted")
	}
}

func TestLoginCreatesOpaqueExpiringSession(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	manual := clock.NewManual(now)
	store := newAuthStore(activeUser())
	service := auth.NewService(store, manual, 2*time.Hour)
	token, principal, err := service.Login(context.Background(), "operator", "correct horse")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if token == "" || token == activeUser().PasswordHash {
		t.Fatalf("token = %q", token)
	}
	if principal.UserID != "u1" || principal.Role != auth.RoleOperator {
		t.Fatalf("principal = %#v", principal)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.sessions) != 1 {
		t.Fatalf("sessions = %d", len(store.sessions))
	}
	for digest, session := range store.sessions {
		if digest == token {
			t.Error("raw token persisted")
		}
		if session.TokenHash != digest {
			t.Errorf("session digest mismatch")
		}
		if !session.ExpiresAt.Equal(now.Add(2 * time.Hour)) {
			t.Errorf("expiry = %s", session.ExpiresAt)
		}
	}
}

func TestLoginRejectsCredentialsWithoutRevealingWhichFieldFailed(t *testing.T) {
	t.Parallel()
	service := auth.NewService(newAuthStore(activeUser()), clock.NewManual(time.Now()), time.Hour)
	tests := []struct{ name, user, password string }{{name: "unknown user", user: "missing", password: "correct horse"}, {name: "wrong password", user: "operator", password: "wrong"}, {name: "empty username", password: "correct horse"}, {name: "empty password", user: "operator"}}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			_, _, err := service.Login(context.Background(), test.user, test.password)
			if !errors.Is(err, domain.ErrUnauthorized) {
				t.Fatalf("Login() error = %v", err)
			}
		})
	}
}

func TestLoginRejectsInactiveUser(t *testing.T) {
	t.Parallel()
	user := activeUser()
	user.Active = false
	service := auth.NewService(newAuthStore(user), clock.NewManual(time.Now()), time.Hour)
	_, _, err := service.Login(context.Background(), user.Username, "correct horse")
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("Login() error = %v", err)
	}
}

func TestAuthenticateRefreshesLastSeenAndReturnsRole(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	manual := clock.NewManual(now)
	store := newAuthStore(activeUser())
	service := auth.NewService(store, manual, time.Hour)
	token, _, err := service.Login(context.Background(), "operator", "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	manual.Advance(5 * time.Minute)
	principal, err := service.Authenticate(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	if principal.Role != auth.RoleOperator || principal.SessionID == "" {
		t.Fatalf("principal = %#v", principal)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, session := range store.sessions {
		if !session.LastSeenAt.Equal(manual.Now()) {
			t.Fatalf("last seen = %s", session.LastSeenAt)
		}
	}
}

func TestLogoutRevokesImmediatelyAndIsIdempotent(t *testing.T) {
	t.Parallel()
	manual := clock.NewManual(time.Now())
	store := newAuthStore(activeUser())
	service := auth.NewService(store, manual, time.Hour)
	token, _, err := service.Login(context.Background(), "operator", "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Logout(context.Background(), token); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if _, err := service.Authenticate(context.Background(), token); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("Authenticate after logout error = %v", err)
	}
	if err := service.Logout(context.Background(), token); err != nil {
		t.Fatalf("second Logout() error = %v", err)
	}
	if err := service.Logout(context.Background(), "unknown-token"); err != nil {
		t.Fatalf("unknown token Logout() error = %v", err)
	}
}

func TestAuthenticateDistinguishesExpiredSession(t *testing.T) {
	t.Parallel()
	manual := clock.NewManual(time.Now())
	store := newAuthStore(activeUser())
	service := auth.NewService(store, manual, time.Minute)
	token, _, err := service.Login(context.Background(), "operator", "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	manual.Advance(time.Minute)
	_, err = service.Authenticate(context.Background(), token)
	if !errors.Is(err, domain.ErrExpired) {
		t.Fatalf("Authenticate() error = %v", err)
	}
}

func TestAuthenticateRejectsUserDisabledAfterLogin(t *testing.T) {
	t.Parallel()
	manual := clock.NewManual(time.Now())
	store := newAuthStore(activeUser())
	service := auth.NewService(store, manual, time.Hour)
	token, _, err := service.Login(context.Background(), "operator", "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	user := store.users["u1"]
	user.Active = false
	store.users[user.ID] = user
	store.mu.Unlock()
	_, err = service.Authenticate(context.Background(), token)
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("Authenticate() error = %v", err)
	}
}

func TestAuthenticationHonorsContextCancellation(t *testing.T) {
	t.Parallel()
	manual := clock.NewManual(time.Now())
	store := newAuthStore(activeUser())
	service := auth.NewService(store, manual, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := service.Login(ctx, "operator", "correct horse")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Login() error = %v", err)
	}
}

func TestStoreFailureIsWrappedForDiagnosis(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("disk unavailable")
	store := newAuthStore(activeUser())
	store.fail = sentinel
	service := auth.NewService(store, clock.NewManual(time.Now()), time.Hour)
	_, _, err := service.Login(context.Background(), "operator", "correct horse")
	if !errors.Is(err, sentinel) {
		t.Fatalf("Login() error = %v, want wrapped sentinel", err)
	}
}

func TestRemoveExpiredDelegatesCurrentClock(t *testing.T) {
	t.Parallel()
	now := time.Now()
	manual := clock.NewManual(now)
	store := newAuthStore(activeUser())
	store.sessions["expired"] = auth.Session{ID: "s1", UserID: "u1", TokenHash: "expired", ExpiresAt: now.Add(-time.Second)}
	store.sessions["active"] = auth.Session{ID: "s2", UserID: "u1", TokenHash: "active", ExpiresAt: now.Add(time.Hour)}
	service := auth.NewService(store, manual, time.Hour)
	count, err := service.RemoveExpired(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("RemoveExpired() = %d, %v", count, err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, ok := store.sessions["active"]; !ok {
		t.Fatal("active session removed")
	}
}

func TestPrincipalRoleMatching(t *testing.T) {
	t.Parallel()
	principal := auth.Principal{Role: auth.RoleCleaner}
	if !principal.HasAny(auth.RoleFrontdesk, auth.RoleCleaner) {
		t.Fatal("matching role rejected")
	}
	if principal.HasAny(auth.RoleOperator, auth.RoleMaintenance) {
		t.Fatal("unrelated role accepted")
	}
	if principal.HasAny() {
		t.Fatal("empty role list accepted")
	}
}
