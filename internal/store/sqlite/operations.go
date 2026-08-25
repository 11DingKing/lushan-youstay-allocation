package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/11DingKing/lushan-youstay-allocation/internal/domain"
)

func (s *Store) CreateTask(ctx context.Context, t domain.OperationalTask) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO operational_tasks(id,stay_id,resource_id,kind,status,assigned_role,attempts,available_at,last_error,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, t.ID, t.StayID, t.ResourceID, t.Kind, t.Status, t.AssignedRole, t.Attempts, encodeTime(t.AvailableAt), t.LastError, encodeTime(t.CreatedAt), encodeTime(t.UpdatedAt))
	if err != nil {
		return mapError("create operational task", err)
	}
	return nil
}

func (s *Store) GetTask(ctx context.Context, id string) (domain.OperationalTask, error) {
	return scanTask(s.db.QueryRowContext(ctx, `SELECT id,stay_id,resource_id,kind,status,assigned_role,attempts,available_at,last_error,created_at,updated_at FROM operational_tasks WHERE id=?`, id))
}

func scanTask(row interface{ Scan(...any) error }) (domain.OperationalTask, error) {
	var t domain.OperationalTask
	var stay, last sql.NullString
	var available, created, updated string
	if err := row.Scan(&t.ID, &stay, &t.ResourceID, &t.Kind, &t.Status, &t.AssignedRole, &t.Attempts, &available, &last, &created, &updated); err != nil {
		return domain.OperationalTask{}, mapError("scan operational task", err)
	}
	if stay.Valid {
		t.StayID = &stay.String
	}
	if last.Valid {
		t.LastError = &last.String
	}
	var err error
	t.AvailableAt, err = parseTime(available)
	if err != nil {
		return t, err
	}
	t.CreatedAt, err = parseTime(created)
	if err != nil {
		return t, err
	}
	t.UpdatedAt, err = parseTime(updated)
	return t, err
}

func (s *Store) ClaimDueTasks(ctx context.Context, role string, now time.Time, limit int) ([]domain.OperationalTask, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	claimed := make([]domain.OperationalTask, 0)
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,stay_id,resource_id,kind,status,assigned_role,attempts,available_at,last_error,created_at,updated_at FROM operational_tasks WHERE status='pending' AND assigned_role=? AND available_at<=? ORDER BY available_at,id LIMIT ?`, role, encodeTime(now), limit)
		if err != nil {
			return fmt.Errorf("list due tasks: %w", err)
		}
		var candidates []domain.OperationalTask
		for rows.Next() {
			t, e := scanTask(rows)
			if e != nil {
				rows.Close()
				return e
			}
			candidates = append(candidates, t)
		}
		rows.Close()
		for _, t := range candidates {
			result, err := tx.ExecContext(ctx, `UPDATE operational_tasks SET status='claimed',attempts=attempts+1,updated_at=? WHERE id=? AND status='pending' AND attempts=?`, encodeTime(now), t.ID, t.Attempts)
			if err != nil {
				return err
			}
			count, _ := result.RowsAffected()
			if count == 0 {
				continue
			}
			t.Status = domain.TaskClaimed
			t.Attempts++
			t.UpdatedAt = now
			claimed = append(claimed, t)
		}
		return nil
	})
	return claimed, err
}

func (s *Store) SaveTask(ctx context.Context, t domain.OperationalTask) error {
	result, err := s.db.ExecContext(ctx, `UPDATE operational_tasks SET status=?,attempts=?,available_at=?,last_error=?,updated_at=? WHERE id=?`, t.Status, t.Attempts, encodeTime(t.AvailableAt), t.LastError, encodeTime(t.UpdatedAt), t.ID)
	if err != nil {
		return fmt.Errorf("save operational task: %w", err)
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) CheckoutAndCreateCleaning(ctx context.Context, stay domain.Stay, task domain.OperationalTask, event AuditEvent) error {
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE stays SET status=?,version=?,updated_at=? WHERE id=? AND version=? AND status='checked_in'`, stay.Status, stay.Version, encodeTime(stay.UpdatedAt), stay.ID, stay.Version-1)
		if err != nil {
			return err
		}
		count, _ := result.RowsAffected()
		if count == 0 {
			return domain.ErrVersionConflict
		}
		if _, err = tx.ExecContext(ctx, `UPDATE stay_nights SET status='released' WHERE stay_id=?`, stay.ID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE resources SET status='cleaning',version=version+1,updated_at=? WHERE id=? AND status='occupied'`, encodeTime(stay.UpdatedAt), stay.ResourceID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO operational_tasks(id,stay_id,resource_id,kind,status,assigned_role,attempts,available_at,last_error,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, task.ID, task.StayID, task.ResourceID, task.Kind, task.Status, task.AssignedRole, task.Attempts, encodeTime(task.AvailableAt), task.LastError, encodeTime(task.CreatedAt), encodeTime(task.UpdatedAt)); err != nil {
			return err
		}
		return insertAudit(ctx, tx, event)
	})
}

func (s *Store) CreateDamageClaim(ctx context.Context, c domain.DamageClaim) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO damage_claims(id,stay_id,task_id,assessed_by,description,amount_cents,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, c.ID, c.StayID, c.TaskID, c.AssessedBy, c.Description, c.AmountCents, c.Status, encodeTime(c.CreatedAt), encodeTime(c.UpdatedAt))
	if err != nil {
		return mapError("create damage claim", err)
	}
	return nil
}

func (s *Store) SumAcceptedDamage(ctx context.Context, stayID string) (int64, error) {
	var amount sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT SUM(amount_cents) FROM damage_claims WHERE stay_id=? AND status='accepted'`, stayID).Scan(&amount); err != nil {
		return 0, fmt.Errorf("sum damage: %w", err)
	}
	return amount.Int64, nil
}

func (s *Store) SumSucceededPayments(ctx context.Context, stayID string) (int64, error) {
	var amount sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT SUM(CASE WHEN kind='refund' THEN -amount_cents ELSE amount_cents END) FROM payments WHERE stay_id=? AND status='succeeded'`, stayID).Scan(&amount); err != nil {
		return 0, fmt.Errorf("sum payments: %w", err)
	}
	return amount.Int64, nil
}

func (s *Store) CreateSettlement(ctx context.Context, settlement domain.RefundSettlement, event AuditEvent) error {
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO refund_settlements(id,stay_id,paid_cents,damage_cents,refund_cents,status,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, settlement.ID, settlement.StayID, settlement.PaidCents, settlement.DamageCents, settlement.RefundCents, settlement.Status, settlement.Version, encodeTime(settlement.CreatedAt), encodeTime(settlement.UpdatedAt)); err != nil {
			return mapError("create settlement", err)
		}
		result, err := tx.ExecContext(ctx, `UPDATE stays SET status='settling',version=version+1,updated_at=? WHERE id=? AND status='checked_out'`, encodeTime(settlement.UpdatedAt), settlement.StayID)
		if err != nil {
			return err
		}
		count, _ := result.RowsAffected()
		if count == 0 {
			return domain.ErrInvalidTransition
		}
		return insertAudit(ctx, tx, event)
	})
}

func (s *Store) CountTable(ctx context.Context, table string) (int, error) {
	allowed := map[string]bool{"users": true, "sessions": true, "properties": true, "resources": true, "stays": true, "stay_nights": true, "identity_checks": true, "payments": true, "operational_tasks": true, "damage_claims": true, "refund_settlements": true, "audit_events": true, "outbox_events": true}
	if !allowed[table] {
		return 0, fmt.Errorf("table is not countable")
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}
