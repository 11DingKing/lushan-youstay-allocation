package auth

import (
	"context"
	"time"
)

type Role string

const (
	RoleGuest       Role = "guest"
	RoleFrontdesk   Role = "frontdesk"
	RoleCleaner     Role = "cleaner"
	RoleMaintenance Role = "maintenance"
	RoleCampGuard   Role = "camp_guard"
	RoleOperator    Role = "operator"
)

type User struct {
	ID           string
	Username     string
	PasswordHash string
	DisplayName  string
	Role         Role
	Active       bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Session struct {
	ID         string
	UserID     string
	TokenHash  string
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
	LastSeenAt time.Time
}

func (s Session) Active(now time.Time) bool {
	return s.RevokedAt == nil && now.Before(s.ExpiresAt)
}

type Principal struct {
	SessionID   string
	UserID      string
	DisplayName string
	Role        Role
}

type contextKey struct{}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, principal)
}

func PrincipalFrom(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(contextKey{}).(Principal)
	return principal, ok
}

func (p Principal) HasAny(roles ...Role) bool {
	for _, role := range roles {
		if p.Role == role {
			return true
		}
	}
	return false
}
