package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/11DingKing/lushan-youstay-allocation/internal/auth"
	"github.com/11DingKing/lushan-youstay-allocation/internal/domain"
)

func (s *Store) CreateUser(ctx context.Context, user auth.User) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO users(id,username,password_hash,display_name,role,active,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`,
		user.ID, user.Username, user.PasswordHash, user.DisplayName, user.Role, user.Active, encodeTime(user.CreatedAt), encodeTime(user.UpdatedAt))
	if err != nil {
		return mapError("create user", err)
	}
	return nil
}

func (s *Store) FindUserByUsername(ctx context.Context, username string) (auth.User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT id,username,password_hash,display_name,role,active,created_at,updated_at FROM users WHERE username=?`, username))
}

func (s *Store) FindUserByID(ctx context.Context, id string) (auth.User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT id,username,password_hash,display_name,role,active,created_at,updated_at FROM users WHERE id=?`, id))
}

func scanUser(row *sql.Row) (auth.User, error) {
	var user auth.User
	var created, updated string
	if err := row.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.DisplayName, &user.Role, &user.Active, &created, &updated); err != nil {
		return auth.User{}, mapError("scan user", err)
	}
	var err error
	if user.CreatedAt, err = parseTime(created); err != nil {
		return auth.User{}, fmt.Errorf("parse user created_at: %w", err)
	}
	if user.UpdatedAt, err = parseTime(updated); err != nil {
		return auth.User{}, fmt.Errorf("parse user updated_at: %w", err)
	}
	return user, nil
}

func (s *Store) CreateSession(ctx context.Context, session auth.Session) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions(id,user_id,token_hash,expires_at,revoked_at,created_at,last_seen_at) VALUES(?,?,?,?,?,?,?)`,
		session.ID, session.UserID, session.TokenHash, encodeTime(session.ExpiresAt), nullableTime(session.RevokedAt), encodeTime(session.CreatedAt), encodeTime(session.LastSeenAt))
	if err != nil {
		return mapError("create session", err)
	}
	return nil
}

func (s *Store) FindSessionByTokenHash(ctx context.Context, digest string) (auth.Session, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,user_id,token_hash,expires_at,revoked_at,created_at,last_seen_at FROM sessions WHERE token_hash=?`, digest)
	var session auth.Session
	var expires, created, seen string
	var revoked sql.NullString
	if err := row.Scan(&session.ID, &session.UserID, &session.TokenHash, &expires, &revoked, &created, &seen); err != nil {
		return auth.Session{}, mapError("scan session", err)
	}
	var err error
	if session.ExpiresAt, err = parseTime(expires); err != nil {
		return auth.Session{}, fmt.Errorf("parse session expiry: %w", err)
	}
	if session.CreatedAt, err = parseTime(created); err != nil {
		return auth.Session{}, fmt.Errorf("parse session created_at: %w", err)
	}
	if session.LastSeenAt, err = parseTime(seen); err != nil {
		return auth.Session{}, fmt.Errorf("parse session last_seen_at: %w", err)
	}
	if revoked.Valid {
		value, e := parseTime(revoked.String)
		if e != nil {
			return auth.Session{}, fmt.Errorf("parse revoked_at: %w", e)
		}
		session.RevokedAt = &value
	}
	return session, nil
}

func (s *Store) TouchSession(ctx context.Context, id string, seen time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE sessions SET last_seen_at=? WHERE id=? AND revoked_at IS NULL`, encodeTime(seen), id)
	if err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return domain.ErrUnauthorized
	}
	return nil
}

func (s *Store) RevokeSession(ctx context.Context, id string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, encodeTime(now), id)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at<=? OR (revoked_at IS NOT NULL AND revoked_at<=?)`, encodeTime(now), encodeTime(now.Add(-24*time.Hour)))
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	count, err := result.RowsAffected()
	return count, err
}

func mapError(operation string, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s: %w", operation, domain.ErrNotFound)
	}
	message := err.Error()
	if errors.Is(translate(err), domainConflict) || containsConstraint(message) {
		return fmt.Errorf("%s: %w", operation, domain.ErrConflict)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func containsConstraint(message string) bool {
	for _, needle := range []string{"UNIQUE constraint", "constraint failed", "database is locked"} {
		if strings.Contains(message, needle) {
			return true
		}
	}
	return false
}
