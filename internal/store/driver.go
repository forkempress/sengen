package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type Driver struct {
	ID             int64
	TenantID       int64
	Name           string
	VehicleLabel   string
	TelegramChatID *int64
	Active         bool
}

func (s *Store) ListDrivers(ctx context.Context, tenantID int64, onlyActive bool) ([]Driver, error) {
	query := `SELECT id, tenant_id, name, vehicle_label, telegram_chat_id, active FROM drivers WHERE tenant_id = ?`
	if onlyActive {
		query += ` AND active = 1`
	}
	query += ` ORDER BY name`
	rows, err := s.DB.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list drivers: %w", err)
	}
	defer rows.Close()

	var out []Driver
	for rows.Next() {
		var d Driver
		var active int
		if err := rows.Scan(&d.ID, &d.TenantID, &d.Name, &d.VehicleLabel, &d.TelegramChatID, &active); err != nil {
			return nil, fmt.Errorf("scan driver: %w", err)
		}
		d.Active = active == 1
		out = append(out, d)
	}
	return out, rows.Err()
}

// FindDriverByName resolves a driver by name for the Telegram admin flow:
// case-insensitive exact match first, falling back to a unique
// case-insensitive substring match. Returns ErrNotFound if there's no
// match, and a ValidationError if more than one driver matches ambiguously.
func (s *Store) FindDriverByName(ctx context.Context, tenantID int64, name string) (*Driver, error) {
	drivers, err := s.ListDrivers(ctx, tenantID, true)
	if err != nil {
		return nil, err
	}
	needle := strings.ToLower(strings.TrimSpace(name))
	for i := range drivers {
		if strings.ToLower(drivers[i].Name) == needle {
			return &drivers[i], nil
		}
	}
	var matches []*Driver
	for i := range drivers {
		if strings.Contains(strings.ToLower(drivers[i].Name), needle) {
			matches = append(matches, &drivers[i])
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return nil, &ValidationError{Msg: fmt.Sprintf("ime %q odgovara više vozača, budite precizniji", name)}
	}
	return nil, ErrNotFound
}

func (s *Store) GetDriver(ctx context.Context, tenantID, id int64) (*Driver, error) {
	var d Driver
	var active int
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, tenant_id, name, vehicle_label, telegram_chat_id, active FROM drivers WHERE tenant_id = ? AND id = ?`,
		tenantID, id,
	).Scan(&d.ID, &d.TenantID, &d.Name, &d.VehicleLabel, &d.TelegramChatID, &active)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get driver: %w", err)
	}
	d.Active = active == 1
	return &d, nil
}

func (s *Store) CreateDriver(ctx context.Context, tenantID int64, name, vehicleLabel string) (*Driver, error) {
	if err := checkLen("naziv vozača", name, MaxNameLen); err != nil {
		return nil, err
	}
	if err := checkLen("oznaka vozila", vehicleLabel, MaxNameLen); err != nil {
		return nil, err
	}
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO drivers (tenant_id, name, vehicle_label, active) VALUES (?, ?, ?, 1)`,
		tenantID, name, vehicleLabel)
	if err != nil {
		return nil, fmt.Errorf("create driver: %w", err)
	}
	id, _ := res.LastInsertId()
	return &Driver{ID: id, TenantID: tenantID, Name: name, VehicleLabel: vehicleLabel, Active: true}, nil
}

// SetDriverActive turns a driver on or off. Deactivating also unlinks his
// Telegram account, so he can't keep writing through the bot and the account
// can be linked again elsewhere; a reactivated driver needs a new link.
func (s *Store) SetDriverActive(ctx context.Context, tenantID, id int64, active bool) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE drivers SET active = ?, telegram_chat_id = CASE WHEN ? THEN telegram_chat_id END
		 WHERE tenant_id = ? AND id = ?`, boolToInt(active), boolToInt(active), tenantID, id)
	if err != nil {
		return fmt.Errorf("set driver active: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
