package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"shengen/internal/clock"
)

type Stay struct {
	ID        int64
	TenantID  int64
	DriverID  int64
	Entry     time.Time // date-only, UTC midnight
	Exit      *time.Time
	Type      string // "work" | "private"
	Source    string // "manual" | "import" | "telegram_button" | "telegram_location"
	Note      string
	CreatedBy *int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ValidationError is returned for domain-rule violations the UI should show
// inline next to the offending field, as opposed to unexpected DB errors.
type ValidationError struct {
	Msg string
}

func (e *ValidationError) Error() string { return e.Msg }

const stayColumns = `id, tenant_id, driver_id, entry_date, exit_date, type, source, note, created_by, created_at, updated_at`

func (s *Store) ListStaysByDriver(ctx context.Context, tenantID, driverID int64) ([]Stay, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+stayColumns+` FROM stays WHERE tenant_id = ? AND driver_id = ? ORDER BY entry_date`,
		tenantID, driverID)
	if err != nil {
		return nil, fmt.Errorf("list stays: %w", err)
	}
	defer rows.Close()
	return scanStays(rows)
}

// ListStaysForTenant returns every stay for every driver in the tenant, used
// by the dashboard to compute each driver's status in bulk.
func (s *Store) ListStaysForTenant(ctx context.Context, tenantID int64) ([]Stay, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+stayColumns+` FROM stays WHERE tenant_id = ? ORDER BY driver_id, entry_date`,
		tenantID)
	if err != nil {
		return nil, fmt.Errorf("list stays for tenant: %w", err)
	}
	defer rows.Close()
	return scanStays(rows)
}

// parseTimestamp parses the TEXT timestamps written by SQLite's
// strftime('%Y-%m-%dT%H:%M:%fZ','now') default, tolerating the handful of
// fractional-second widths SQLite may emit.
func parseTimestamp(s string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02T15:04:05.000Z", time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized timestamp %q", s)
}

func scanStays(rows *sql.Rows) ([]Stay, error) {
	var out []Stay
	for rows.Next() {
		var st Stay
		var entryStr string
		var exitStr sql.NullString
		var createdAtStr, updatedAtStr string
		if err := rows.Scan(&st.ID, &st.TenantID, &st.DriverID, &entryStr, &exitStr, &st.Type, &st.Source, &st.Note, &st.CreatedBy, &createdAtStr, &updatedAtStr); err != nil {
			return nil, fmt.Errorf("scan stay: %w", err)
		}
		entry, err := time.Parse(dateLayout, entryStr)
		if err != nil {
			return nil, fmt.Errorf("parse entry_date: %w", err)
		}
		st.Entry = entry
		if exitStr.Valid {
			exit, err := time.Parse(dateLayout, exitStr.String)
			if err != nil {
				return nil, fmt.Errorf("parse exit_date: %w", err)
			}
			st.Exit = &exit
		}
		st.CreatedAt, _ = parseTimestamp(createdAtStr)
		st.UpdatedAt, _ = parseTimestamp(updatedAtStr)
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *Store) GetStay(ctx context.Context, tenantID, id int64) (*Stay, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+stayColumns+` FROM stays WHERE tenant_id = ? AND id = ?`, tenantID, id)
	var st Stay
	var entryStr string
	var exitStr sql.NullString
	var createdAtStr, updatedAtStr string
	err := row.Scan(&st.ID, &st.TenantID, &st.DriverID, &entryStr, &exitStr, &st.Type, &st.Source, &st.Note, &st.CreatedBy, &createdAtStr, &updatedAtStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get stay: %w", err)
	}
	st.Entry, _ = time.Parse(dateLayout, entryStr)
	if exitStr.Valid {
		exit, _ := time.Parse(dateLayout, exitStr.String)
		st.Exit = &exit
	}
	st.CreatedAt, _ = parseTimestamp(createdAtStr)
	st.UpdatedAt, _ = parseTimestamp(updatedAtStr)
	return &st, nil
}

// validateStay enforces: exit >= entry, no future dates, and no overlap with
// the driver's other stays. excludeID excludes a stay from the overlap check
// when editing it (pass 0 when creating).
func (s *Store) validateStay(ctx context.Context, tenantID, driverID, excludeID int64, entry time.Time, exit *time.Time) error {
	// A crossing at an EET border after 23:00 Belgrade time is stamped with
	// tomorrow's Belgrade date; that is not the future.
	latest := clock.LatestToday()
	if entry.After(latest) {
		return &ValidationError{Msg: "datum ulaska ne može biti u budućnosti"}
	}
	if exit != nil {
		if exit.After(latest) {
			return &ValidationError{Msg: "datum izlaska ne može biti u budućnosti"}
		}
		if exit.Before(entry) {
			return &ValidationError{Msg: "datum izlaska ne može biti pre datuma ulaska"}
		}
	}

	existing, err := s.ListStaysByDriver(ctx, tenantID, driverID)
	if err != nil {
		return err
	}
	newExitForCompare := latest // open stay treated as extending through today for overlap purposes
	if exit != nil {
		newExitForCompare = *exit
	}
	for _, ex := range existing {
		if ex.ID == excludeID {
			continue
		}
		exExitForCompare := latest
		if ex.Exit != nil {
			exExitForCompare = *ex.Exit
		}
		overlap := !entry.After(exExitForCompare) && !ex.Entry.After(newExitForCompare)
		if overlap {
			return &ValidationError{Msg: fmt.Sprintf("preklapa se sa postojećim boravkom %s–%s",
				ex.Entry.Format("02.01.2006"), formatExitForMessage(ex.Exit))}
		}
	}
	return nil
}

func formatExitForMessage(exit *time.Time) string {
	if exit == nil {
		return "otvoren"
	}
	return exit.Format("02.01.2006")
}

func (s *Store) CreateStay(ctx context.Context, tenantID, driverID int64, entry time.Time, exit *time.Time, typ, source, note string, createdBy *int64) (*Stay, error) {
	if err := checkLen("napomena", note, MaxNoteLen); err != nil {
		return nil, err
	}
	// The driver id may come from a form; never attach a stay to another
	// firm's driver.
	if _, err := s.GetDriver(ctx, tenantID, driverID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, &ValidationError{Msg: "nepoznat vozač"}
		}
		return nil, err
	}
	if err := s.validateStay(ctx, tenantID, driverID, 0, entry, exit); err != nil {
		return nil, err
	}
	var exitStr sql.NullString
	if exit != nil {
		exitStr = sql.NullString{String: exit.Format(dateLayout), Valid: true}
	}
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO stays (tenant_id, driver_id, entry_date, exit_date, type, source, note, created_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		tenantID, driverID, entry.Format(dateLayout), exitStr, typ, source, note, createdBy)
	if err != nil {
		return nil, fmt.Errorf("create stay: %w", err)
	}
	id, _ := res.LastInsertId()
	return &Stay{ID: id, TenantID: tenantID, DriverID: driverID, Entry: entry, Exit: exit, Type: typ, Source: source, Note: note, CreatedBy: createdBy}, nil
}

func (s *Store) UpdateStay(ctx context.Context, tenantID, id int64, entry time.Time, exit *time.Time, typ, note string) error {
	if err := checkLen("napomena", note, MaxNoteLen); err != nil {
		return err
	}
	st, err := s.GetStay(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if err := s.validateStay(ctx, tenantID, st.DriverID, id, entry, exit); err != nil {
		return err
	}
	var exitStr sql.NullString
	if exit != nil {
		exitStr = sql.NullString{String: exit.Format(dateLayout), Valid: true}
	}
	_, err = s.DB.ExecContext(ctx,
		`UPDATE stays SET entry_date = ?, exit_date = ?, type = ?, note = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
		 WHERE tenant_id = ? AND id = ?`,
		entry.Format(dateLayout), exitStr, typ, note, tenantID, id)
	if err != nil {
		return fmt.Errorf("update stay: %w", err)
	}
	return nil
}

func (s *Store) DeleteStay(ctx context.Context, tenantID, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM stays WHERE tenant_id = ? AND id = ?`, tenantID, id)
	if err != nil {
		return fmt.Errorf("delete stay: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// CloseOpenStay closes a driver's currently open stay (if any) on exitDate —
// used by the "Vratio se danas" button. Returns ErrNotFound if there is no
// open stay.
func (s *Store) CloseOpenStay(ctx context.Context, tenantID, driverID int64, exitDate time.Time) (*Stay, error) {
	stays, err := s.ListStaysByDriver(ctx, tenantID, driverID)
	if err != nil {
		return nil, err
	}
	var open *Stay
	for i := range stays {
		if stays[i].Exit == nil {
			open = &stays[i]
			break
		}
	}
	if open == nil {
		return nil, ErrNotFound
	}
	if exitDate.Before(open.Entry) {
		return nil, &ValidationError{Msg: "datum povratka ne može biti pre datuma ulaska"}
	}
	if err := s.UpdateStay(ctx, tenantID, open.ID, open.Entry, &exitDate, open.Type, open.Note); err != nil {
		return nil, err
	}
	open.Exit = &exitDate
	return open, nil
}

// OpenStayForDriver returns the driver's currently open stay, if any.
func (s *Store) OpenStayForDriver(ctx context.Context, tenantID, driverID int64) (*Stay, error) {
	stays, err := s.ListStaysByDriver(ctx, tenantID, driverID)
	if err != nil {
		return nil, err
	}
	for i := range stays {
		if stays[i].Exit == nil {
			return &stays[i], nil
		}
	}
	return nil, ErrNotFound
}
