package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"shengen/internal/clock"
)

// PlannedStay is a trip the firm intends to make: From and To are the first
// and last day in Schengen. It never counts as a real stay.
type PlannedStay struct {
	ID        int64
	TenantID  int64
	DriverID  int64
	From      time.Time // date-only, UTC midnight
	To        time.Time
	Note      string
	CreatedBy *int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// MaxPlanDays is the longest plan: a longer trip can never fit in 90/180.
const MaxPlanDays = 90

const plannedColumns = `id, tenant_id, driver_id, start_date, end_date, note, created_by, created_at, updated_at`

func scanPlanned(sc interface{ Scan(...any) error }) (PlannedStay, error) {
	var p PlannedStay
	var from, to, createdAt, updatedAt string
	if err := sc.Scan(&p.ID, &p.TenantID, &p.DriverID, &from, &to, &p.Note, &p.CreatedBy, &createdAt, &updatedAt); err != nil {
		return p, err
	}
	var err error
	if p.From, err = time.Parse(dateLayout, from); err != nil {
		return p, fmt.Errorf("parse start_date: %w", err)
	}
	if p.To, err = time.Parse(dateLayout, to); err != nil {
		return p, fmt.Errorf("parse end_date: %w", err)
	}
	p.CreatedAt, _ = parseTimestamp(createdAt)
	p.UpdatedAt, _ = parseTimestamp(updatedAt)
	return p, nil
}

func (s *Store) listPlanned(ctx context.Context, where string, args ...any) ([]PlannedStay, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+plannedColumns+` FROM planned_stays WHERE `+where+` ORDER BY start_date, id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list planned stays: %w", err)
	}
	defer rows.Close()
	var out []PlannedStay
	for rows.Next() {
		p, err := scanPlanned(rows)
		if err != nil {
			return nil, fmt.Errorf("scan planned stay: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListPlannedStaysByDriver returns a driver's plans, earliest first.
func (s *Store) ListPlannedStaysByDriver(ctx context.Context, tenantID, driverID int64) ([]PlannedStay, error) {
	return s.listPlanned(ctx, `tenant_id = ? AND driver_id = ?`, tenantID, driverID)
}

// ListPlannedStaysForTenant returns every plan of the firm, for the overview.
func (s *Store) ListPlannedStaysForTenant(ctx context.Context, tenantID int64) ([]PlannedStay, error) {
	return s.listPlanned(ctx, `tenant_id = ?`, tenantID)
}

// ListPlannedStaysFrom returns the firm's plans that end on or after day.
func (s *Store) ListPlannedStaysFrom(ctx context.Context, tenantID int64, day time.Time) ([]PlannedStay, error) {
	return s.listPlanned(ctx, `tenant_id = ? AND end_date >= ?`, tenantID, day.Format(dateLayout))
}

func (s *Store) GetPlannedStay(ctx context.Context, tenantID, id int64) (*PlannedStay, error) {
	p, err := scanPlanned(s.DB.QueryRowContext(ctx,
		`SELECT `+plannedColumns+` FROM planned_stays WHERE tenant_id = ? AND id = ?`, tenantID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get planned stay: %w", err)
	}
	return &p, nil
}

// validatePlan checks the dates. A new plan, or one whose start is moved,
// must not start in the past; any plan must end today or later.
func validatePlan(from, to time.Time, startMoved bool) error {
	today := clock.Today()
	switch {
	case to.Before(from):
		return &ValidationError{Msg: "povratak ne može biti pre polaska"}
	case int(to.Sub(from).Hours()/24)+1 > MaxPlanDays:
		return &ValidationError{Msg: fmt.Sprintf("plan može trajati najviše %d dana", MaxPlanDays)}
	case startMoved && from.Before(today):
		return &ValidationError{Msg: "polazak ne može biti u prošlosti"}
	case to.Before(today):
		return &ValidationError{Msg: "povratak ne može biti u prošlosti"}
	}
	return nil
}

func (s *Store) CreatePlannedStay(ctx context.Context, tenantID, driverID int64, from, to time.Time, note string, createdBy *int64) (*PlannedStay, error) {
	if err := checkLen("napomena", note, MaxNoteLen); err != nil {
		return nil, err
	}
	if _, err := s.GetDriver(ctx, tenantID, driverID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, &ValidationError{Msg: "nepoznat vozač"}
		}
		return nil, err
	}
	if err := validatePlan(from, to, true); err != nil {
		return nil, err
	}
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO planned_stays (tenant_id, driver_id, start_date, end_date, note, created_by) VALUES (?, ?, ?, ?, ?, ?)`,
		tenantID, driverID, from.Format(dateLayout), to.Format(dateLayout), note, createdBy)
	if err != nil {
		return nil, fmt.Errorf("create planned stay: %w", err)
	}
	id, _ := res.LastInsertId()
	return &PlannedStay{ID: id, TenantID: tenantID, DriverID: driverID, From: from, To: to, Note: note, CreatedBy: createdBy}, nil
}

// UpdatePlannedStay changes a plan's dates and note. A plan already under
// way may keep its start in the past, so a dispatcher can move the expected
// return of a trip that has begun.
func (s *Store) UpdatePlannedStay(ctx context.Context, tenantID, id int64, from, to time.Time, note string) error {
	if err := checkLen("napomena", note, MaxNoteLen); err != nil {
		return err
	}
	p, err := s.GetPlannedStay(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if err := validatePlan(from, to, !from.Equal(p.From)); err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx,
		`UPDATE planned_stays SET start_date = ?, end_date = ?, note = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
		 WHERE tenant_id = ? AND id = ?`,
		from.Format(dateLayout), to.Format(dateLayout), note, tenantID, id)
	if err != nil {
		return fmt.Errorf("update planned stay: %w", err)
	}
	return nil
}

func (s *Store) DeletePlannedStay(ctx context.Context, tenantID, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM planned_stays WHERE tenant_id = ? AND id = ?`, tenantID, id)
	if err != nil {
		return fmt.Errorf("delete planned stay: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
