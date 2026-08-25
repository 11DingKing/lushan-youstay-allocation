package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/11DingKing/lushan-youstay-allocation/internal/clock"
	"github.com/11DingKing/lushan-youstay-allocation/internal/domain"
	"github.com/11DingKing/lushan-youstay-allocation/internal/identity"
)

type Store interface {
	FindUserByUsername(context.Context, string) (User, error)
	FindUserByID(context.Context, string) (User, error)
	CreateSession(context.Context, Session) error
	FindSessionByTokenHash(context.Context, string) (Session, error)
	TouchSession(context.Context, string, time.Time) error
	RevokeSession(context.Context, string, time.Time) error
	DeleteExpiredSessions(context.Context, time.Time) (int64, error)
}

type Service struct {
	store Store
	clock clock.Clock
	ttl   time.Duration
}

func NewService(store Store, c clock.Clock, ttl time.Duration) *Service {
	return &Service{store: store, clock: c, ttl: ttl}
}

func (s *Service) Login(ctx context.Context, username, password string) (string, Principal, error) {
	if username == "" || password == "" {
		return "", Principal{}, domain.ErrUnauthorized
	}
	user, err := s.store.FindUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return "", Principal{}, domain.ErrUnauthorized
		}
		return "", Principal{}, fmt.Errorf("lookup login user: %w", err)
	}
	if !user.Active || !CheckPassword(user.PasswordHash, password) {
		return "", Principal{}, domain.ErrUnauthorized
	}
	raw, digest, err := newToken()
	if err != nil {
		return "", Principal{}, err
	}
	now := s.clock.Now()
	session := Session{ID: identity.MustNew("ses"), UserID: user.ID, TokenHash: digest,
		ExpiresAt: now.Add(s.ttl), CreatedAt: now, LastSeenAt: now}
	if err := s.store.CreateSession(ctx, session); err != nil {
		return "", Principal{}, fmt.Errorf("persist session: %w", err)
	}
	return raw, principal(user, session), nil
}

func (s *Service) Authenticate(ctx context.Context, raw string) (Principal, error) {
	if raw == "" {
		return Principal{}, domain.ErrUnauthorized
	}
	session, err := s.store.FindSessionByTokenHash(ctx, tokenDigest(raw))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return Principal{}, domain.ErrUnauthorized
		}
		return Principal{}, fmt.Errorf("lookup session: %w", err)
	}
	now := s.clock.Now()
	if !session.Active(now) {
		if !now.Before(session.ExpiresAt) {
			return Principal{}, domain.ErrExpired
		}
		return Principal{}, domain.ErrUnauthorized
	}
	user, err := s.store.FindUserByID(ctx, session.UserID)
	if err != nil {
		return Principal{}, fmt.Errorf("lookup session user: %w", err)
	}
	if !user.Active {
		return Principal{}, domain.ErrForbidden
	}
	if err := s.store.TouchSession(ctx, session.ID, now); err != nil {
		return Principal{}, fmt.Errorf("touch session: %w", err)
	}
	return principal(user, session), nil
}

func (s *Service) Logout(ctx context.Context, raw string) error {
	if raw == "" {
		return domain.ErrUnauthorized
	}
	lookupContext := ctx
	if ctx.Err() != nil {
		lookupContext = context.Background()
	}
	session, err := s.store.FindSessionByTokenHash(lookupContext, tokenDigest(raw))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("lookup logout session: %w", err)
	}
	if session.RevokedAt != nil {
		return nil
	}
	revokeContext := ctx
	if ctx.Err() != nil {
		revokeContext = context.Background()
	}
	if err := s.store.RevokeSession(revokeContext, session.ID, s.clock.Now()); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func (s *Service) RemoveExpired(ctx context.Context) (int64, error) {
	count, err := s.store.DeleteExpiredSessions(ctx, s.clock.Now())
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	return count, nil
}

func principal(user User, session Session) Principal {
	return Principal{SessionID: session.ID, UserID: user.ID, DisplayName: user.DisplayName, Role: user.Role}
}

func newToken() (string, string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", "", fmt.Errorf("generate session token: %w", err)
	}
	raw := base64.RawURLEncoding.EncodeToString(buffer)
	return raw, tokenDigest(raw), nil
}

func tokenDigest(raw string) string {
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:])
}
