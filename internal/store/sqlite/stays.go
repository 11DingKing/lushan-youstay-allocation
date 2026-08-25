package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/11DingKing/lushan-youstay-allocation/internal/domain"
)

type AuditEvent struct {
	ActorID                                                     *string
	RequestID, Action, ObjectType, ObjectID, Result, DetailJSON string
	OccurredAt                                                  time.Time
}

func (s *Store) CreateHeldStay(ctx context.Context, stay domain.Stay, nights []string, event AuditEvent) error {
	if err := stay.Validate(); err != nil {
		return err
	}
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO stays(id,guest_id,resource_id,status,check_in_date,check_out_date,party_size,quoted_cents,guarantee_cents,hold_expires_at,idempotency_key,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, stay.ID, stay.GuestID, stay.ResourceID, stay.Status, stay.CheckInDate, stay.CheckOutDate, stay.PartySize, stay.QuotedCents, stay.GuaranteeCents, nullableTime(stay.HoldExpiresAt), stay.IdempotencyKey, stay.Version, encodeTime(stay.CreatedAt), encodeTime(stay.UpdatedAt))
		if err != nil {
			return mapError("create held stay", err)
		}
		price := int64(0)
		if len(nights) > 0 {
			price = stay.QuotedCents / int64(len(nights))
		}
		for _, night := range nights {
			if _, err := tx.ExecContext(ctx, `INSERT INTO stay_nights(stay_id,resource_id,night,price_cents,status) VALUES(?,?,?,?,?)`, stay.ID, stay.ResourceID, night, price, "held"); err != nil {
				return mapError("reserve stay night", err)
			}
		}
		return insertAudit(ctx, tx, event)
	})
}

func (s *Store) GetStay(ctx context.Context, id string) (domain.Stay, error) {
	return scanStay(s.db.QueryRowContext(ctx, `SELECT id,guest_id,resource_id,status,check_in_date,check_out_date,party_size,quoted_cents,guarantee_cents,hold_expires_at,idempotency_key,version,created_at,updated_at FROM stays WHERE id=?`, id))
}

func (s *Store) GetStayByIdempotencyKey(ctx context.Context, key string) (domain.Stay, error) {
	return scanStay(s.db.QueryRowContext(ctx, `SELECT id,guest_id,resource_id,status,check_in_date,check_out_date,party_size,quoted_cents,guarantee_cents,hold_expires_at,idempotency_key,version,created_at,updated_at FROM stays WHERE idempotency_key=?`, key))
}

func scanStay(row interface{ Scan(...any) error }) (domain.Stay, error) {
	var stay domain.Stay
	var expiry sql.NullString
	var created, updated string
	if err := row.Scan(&stay.ID, &stay.GuestID, &stay.ResourceID, &stay.Status, &stay.CheckInDate, &stay.CheckOutDate, &stay.PartySize, &stay.QuotedCents, &stay.GuaranteeCents, &expiry, &stay.IdempotencyKey, &stay.Version, &created, &updated); err != nil {
		return domain.Stay{}, mapError("scan stay", err)
	}
	var err error
	stay.CreatedAt, err = parseTime(created)
	if err != nil {
		return domain.Stay{}, err
	}
	stay.UpdatedAt, err = parseTime(updated)
	if err != nil {
		return domain.Stay{}, err
	}
	if expiry.Valid {
		v, e := parseTime(expiry.String)
		if e != nil {
			return domain.Stay{}, e
		}
		stay.HoldExpiresAt = &v
	}
	return stay, nil
}

func (s *Store) GuaranteeStay(ctx context.Context, stay domain.Stay, check domain.IdentityCheck, payment domain.Payment, event AuditEvent) error {
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE stays SET status=?,guarantee_cents=?,hold_expires_at=NULL,version=version+1,updated_at=? WHERE id=? AND status='held' AND version=?`, stay.Status, stay.GuaranteeCents, encodeTime(stay.UpdatedAt), stay.ID, stay.Version-1)
		if err != nil {
			return fmt.Errorf("guarantee stay: %w", err)
		}
		count, _ := result.RowsAffected()
		if count == 0 {
			return domain.ErrVersionConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO identity_checks(id,stay_id,guest_id,document_digest,status,verified_by,verified_at,created_at) VALUES(?,?,?,?,?,?,?,?)`, check.ID, check.StayID, check.GuestID, check.DocumentDigest, check.Status, check.VerifiedBy, nullableTime(check.VerifiedAt), encodeTime(check.CreatedAt))
		if err != nil {
			return mapError("save identity check", err)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO payments(id,stay_id,provider,provider_event_id,amount_cents,kind,status,occurred_at,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, payment.ID, payment.StayID, payment.Provider, payment.ProviderEventID, payment.AmountCents, payment.Kind, payment.Status, encodeTime(payment.OccurredAt), encodeTime(payment.CreatedAt))
		if err != nil {
			return mapError("save guarantee payment", err)
		}
		if _, err = tx.ExecContext(ctx, `UPDATE stay_nights SET status='reserved' WHERE stay_id=? AND status='held'`, stay.ID); err != nil {
			return fmt.Errorf("reserve guaranteed nights: %w", err)
		}
		return insertAudit(ctx, tx, event)
	})
}

func (s *Store) RecordPayment(ctx context.Context, payment domain.Payment) (bool, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO payments(id,stay_id,provider,provider_event_id,amount_cents,kind,status,occurred_at,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, payment.ID, payment.StayID, payment.Provider, payment.ProviderEventID, payment.AmountCents, payment.Kind, payment.Status, encodeTime(payment.OccurredAt), encodeTime(payment.CreatedAt))
	if err == nil {
		return true, nil
	}
	if errors.Is(mapError("payment", err), domain.ErrConflict) {
		var existingID string
		if lookupErr := s.db.QueryRowContext(ctx, `SELECT id FROM payments WHERE provider=? AND provider_event_id=?`, payment.Provider, payment.ProviderEventID).Scan(&existingID); lookupErr != nil {
			return false, fmt.Errorf("confirm duplicate payment: %w", lookupErr)
		}
		if existingID == "" {
			return false, domain.ErrNotFound
		}
		return true, nil
	}
	return false, fmt.Errorf("record payment: %w", err)
}

func (s *Store) TransitionStay(ctx context.Context, stay domain.Stay, event AuditEvent) error {
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE stays SET status=?,hold_expires_at=?,version=?,updated_at=? WHERE id=? AND version=?`, stay.Status, nullableTime(stay.HoldExpiresAt), stay.Version, encodeTime(stay.UpdatedAt), stay.ID, stay.Version-1)
		if err != nil {
			return fmt.Errorf("transition stay: %w", err)
		}
		count, _ := result.RowsAffected()
		if count == 0 {
			return domain.ErrVersionConflict
		}
		switch stay.Status {
		case domain.StayExpired, domain.StayCancelled:
			_, err = tx.ExecContext(ctx, `UPDATE stay_nights SET status='released' WHERE stay_id=?`, stay.ID)
		case domain.StayCheckedIn:
			_, err = tx.ExecContext(ctx, `UPDATE stay_nights SET status='occupied' WHERE stay_id=?`, stay.ID)
		}
		if err != nil {
			return fmt.Errorf("transition stay nights: %w", err)
		}
		return insertAudit(ctx, tx, event)
	})
}

func (s *Store) ExpireDueHolds(ctx context.Context, now time.Time, limit int) (int, error) {
	if limit <= 0 {
		limit = 50
	}
	count := 0
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,version FROM stays WHERE status='held' AND hold_expires_at<=? ORDER BY hold_expires_at LIMIT ?`, encodeTime(now), limit)
		if err != nil {
			return fmt.Errorf("query expired holds: %w", err)
		}
		type candidate struct {
			id      string
			version int64
		}
		var candidates []candidate
		for rows.Next() {
			var c candidate
			if err := rows.Scan(&c.id, &c.version); err != nil {
				rows.Close()
				return err
			}
			candidates = append(candidates, c)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, c := range candidates {
			result, err := tx.ExecContext(ctx, `UPDATE stays SET status='expired',hold_expires_at=NULL,version=version+1,updated_at=? WHERE id=? AND status='held' AND version=?`, encodeTime(now), c.id, c.version)
			if err != nil {
				return err
			}
			affected, _ := result.RowsAffected()
			if affected == 0 {
				continue
			}
			if _, err := tx.ExecContext(ctx, `UPDATE stay_nights SET status='released' WHERE stay_id=? AND status='held'`, c.id); err != nil {
				return err
			}
			count++
		}
		return nil
	})
	return count, err
}

func insertAudit(ctx context.Context, tx *sql.Tx, e AuditEvent) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO audit_events(actor_id,request_id,action,object_type,object_id,result,detail_json,occurred_at) VALUES(?,?,?,?,?,?,?,?)`, e.ActorID, e.RequestID, e.Action, e.ObjectType, e.ObjectID, e.Result, e.DetailJSON, encodeTime(e.OccurredAt))
	if err != nil {
		return fmt.Errorf("write audit event: %w", err)
	}
	return nil
}
