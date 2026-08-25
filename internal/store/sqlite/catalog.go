package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/11DingKing/lushan-youstay-allocation/internal/domain"
)

type ResourceFilter struct {
	PropertyID string
	Kind       domain.ResourceKind
	Status     domain.ResourceStatus
	Limit      int
	Offset     int
}

func (s *Store) CreateProperty(ctx context.Context, p domain.Property) error {
	if err := p.Validate(); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO properties(id,name,zone,timezone,status,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, p.ID, p.Name, p.Zone, p.Timezone, p.Status, p.Version, encodeTime(p.CreatedAt), encodeTime(p.UpdatedAt))
	if err != nil {
		return mapError("create property", err)
	}
	return nil
}

func (s *Store) GetProperty(ctx context.Context, id string) (domain.Property, error) {
	return scanProperty(s.db.QueryRowContext(ctx, `SELECT id,name,zone,timezone,status,version,created_at,updated_at FROM properties WHERE id=?`, id))
}

func scanProperty(row *sql.Row) (domain.Property, error) {
	var p domain.Property
	var created, updated string
	if err := row.Scan(&p.ID, &p.Name, &p.Zone, &p.Timezone, &p.Status, &p.Version, &created, &updated); err != nil {
		return domain.Property{}, mapError("scan property", err)
	}
	var err error
	p.CreatedAt, err = parseTime(created)
	if err != nil {
		return domain.Property{}, err
	}
	p.UpdatedAt, err = parseTime(updated)
	return p, err
}

func (s *Store) UpdatePropertyStatus(ctx context.Context, id string, from, to domain.PropertyStatus, version int64, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE properties
		SET status=?, version=version+1, updated_at=?
		WHERE id=?`,
		to, encodeTime(now), id)
	if err != nil {
		return fmt.Errorf("update property status: %w", err)
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return domain.ErrVersionConflict
	}
	return nil
}

func (s *Store) CreateResource(ctx context.Context, r domain.Resource) error {
	if err := r.Validate(); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO resources(id,property_id,code,name,kind,capacity,base_price_cents,status,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, r.ID, r.PropertyID, r.Code, r.Name, r.Kind, r.Capacity, r.BasePriceCents, r.Status, r.Version, encodeTime(r.CreatedAt), encodeTime(r.UpdatedAt))
	if err != nil {
		return mapError("create resource", err)
	}
	return nil
}

func (s *Store) GetResource(ctx context.Context, id string) (domain.Resource, error) {
	return scanResource(s.db.QueryRowContext(ctx, `SELECT id,property_id,code,name,kind,capacity,base_price_cents,status,version,created_at,updated_at FROM resources WHERE id=?`, id))
}

func scanResource(row interface{ Scan(...any) error }) (domain.Resource, error) {
	var r domain.Resource
	var created, updated string
	if err := row.Scan(&r.ID, &r.PropertyID, &r.Code, &r.Name, &r.Kind, &r.Capacity, &r.BasePriceCents, &r.Status, &r.Version, &created, &updated); err != nil {
		return domain.Resource{}, mapError("scan resource", err)
	}
	var err error
	r.CreatedAt, err = parseTime(created)
	if err != nil {
		return domain.Resource{}, err
	}
	r.UpdatedAt, err = parseTime(updated)
	return r, err
}

func (s *Store) ListResources(ctx context.Context, f ResourceFilter) ([]domain.Resource, int, error) {
	where := []string{"1=1"}
	args := []any{}
	if f.PropertyID != "" {
		where = append(where, "property_id=?")
		args = append(args, f.PropertyID)
	}
	if f.Kind != "" {
		where = append(where, "kind=?")
		args = append(args, f.Kind)
	}
	if f.Status != "" {
		where = append(where, "status=?")
		args = append(args, f.Status)
	}
	clause := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM resources WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count resources: %w", err)
	}
	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,property_id,code,name,kind,capacity,base_price_cents,status,version,created_at,updated_at FROM resources WHERE `+clause+` ORDER BY code LIMIT ? OFFSET ?`, append(args, limit, f.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list resources: %w", err)
	}
	defer rows.Close()
	items := make([]domain.Resource, 0)
	for rows.Next() {
		r, e := scanResource(rows)
		if e != nil {
			return nil, 0, e
		}
		items = append(items, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate resources: %w", err)
	}
	return items, total, nil
}

func (s *Store) UpdateResourceStatus(ctx context.Context, id string, from, to domain.ResourceStatus, version int64, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE resources SET status=?,version=version+1,updated_at=? WHERE id=? AND status=? AND version=?`, to, encodeTime(now), id, from, version)
	if err != nil {
		return fmt.Errorf("update resource status: %w", err)
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return domain.ErrVersionConflict
	}
	return nil
}
