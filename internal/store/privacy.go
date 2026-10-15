package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// --- terms of use and data processing agreement ---

// TermsStatus is the version of the terms a firm accepted ("" if none) and
// when.
type TermsStatus struct {
	Version    string
	AcceptedAt *time.Time
	AcceptedBy *int64
}

func (s *Store) GetTermsStatus(ctx context.Context, tenantID int64) (TermsStatus, error) {
	var ts TermsStatus
	var at sql.NullString
	err := s.DB.QueryRowContext(ctx,
		`SELECT terms_version, terms_accepted_at, terms_accepted_by FROM tenants WHERE id = ?`, tenantID,
	).Scan(&ts.Version, &at, &ts.AcceptedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return ts, ErrNotFound
	}
	if err != nil {
		return ts, fmt.Errorf("get terms status: %w", err)
	}
	if at.Valid {
		if t, err := parseTimestamp(at.String); err == nil {
			ts.AcceptedAt = &t
		}
	}
	return ts, nil
}

// AcceptTerms records that userID accepted version of the terms for the firm.
func (s *Store) AcceptTerms(ctx context.Context, tenantID, userID int64, version string) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE tenants SET terms_version = ?, terms_accepted_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), terms_accepted_by = ?
		 WHERE id = ?`, version, userID, tenantID)
	if err != nil {
		return fmt.Errorf("accept terms: %w", err)
	}
	return nil
}

// --- the driver's privacy notice in the bot ---

// DriverPrivacyAck returns when the driver confirmed the privacy notice, or
// nil if he has not.
func (s *Store) DriverPrivacyAck(ctx context.Context, tenantID, driverID int64) (*time.Time, error) {
	var at sql.NullString
	err := s.DB.QueryRowContext(ctx,
		`SELECT privacy_ack_at FROM drivers WHERE tenant_id = ? AND id = ?`, tenantID, driverID).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("driver privacy ack: %w", err)
	}
	if !at.Valid {
		return nil, nil
	}
	t, err := parseTimestamp(at.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// DriversAwaitingPrivacyAck lists the firm's active drivers linked to
// Telegram who have not confirmed the privacy notice yet.
func (s *Store) DriversAwaitingPrivacyAck(ctx context.Context, tenantID int64) ([]Driver, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, tenant_id, name, vehicle_label, telegram_chat_id, active FROM drivers
		 WHERE tenant_id = ? AND active = 1 AND telegram_chat_id IS NOT NULL AND privacy_ack_at IS NULL ORDER BY id`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("drivers awaiting privacy ack: %w", err)
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

func (s *Store) SetDriverPrivacyAck(ctx context.Context, tenantID, driverID int64) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE drivers SET privacy_ack_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE tenant_id = ? AND id = ?`,
		tenantID, driverID)
	if err != nil {
		return fmt.Errorf("set driver privacy ack: %w", err)
	}
	return nil
}

// UnlinkDriverTelegram ends the link between a driver and his Telegram
// account (the driver's /obrisi_me). His records stay: they belong to the
// firm, which decides whether to delete them.
func (s *Store) UnlinkDriverTelegram(ctx context.Context, tenantID, driverID int64) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE drivers SET telegram_chat_id = NULL, privacy_ack_at = NULL WHERE tenant_id = ? AND id = ?`,
		tenantID, driverID)
	if err != nil {
		return fmt.Errorf("unlink driver telegram: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- permanent deletion of a driver ---

// DeleteDriverPermanently removes a driver and everything recorded about
// him: stays, plans, pending link tokens, alert history, the location
// events table (unused so far) and stays another firm once forged onto him. Audit entries about his stays and plans keep
// only who did what and when: their before/after copies, which hold dates,
// are cleared. It runs in one transaction; call it on the top-level Store.
func (s *Store) DeleteDriverPermanently(ctx context.Context, tenantID, driverID int64) error {
	return s.WithTx(ctx, func(tx *Store) error {
		if _, err := tx.GetDriver(ctx, tenantID, driverID); err != nil {
			return err
		}
		scrub := func(entity, table string) error {
			_, err := tx.DB.ExecContext(ctx,
				`UPDATE audit_log SET before_json = NULL, after_json = NULL
				 WHERE tenant_id = ? AND entity = ? AND entity_id IN (SELECT id FROM `+table+` WHERE tenant_id = ? AND driver_id = ?)`,
				tenantID, entity, tenantID, driverID)
			return err
		}
		if err := scrub("stay", "stays"); err != nil {
			return fmt.Errorf("scrub stay audit: %w", err)
		}
		if err := scrub("planned_stay", "planned_stays"); err != nil {
			return fmt.Errorf("scrub plan audit: %w", err)
		}
		if _, err := tx.DB.ExecContext(ctx,
			`UPDATE audit_log SET before_json = NULL, after_json = NULL WHERE tenant_id = ? AND entity = 'driver' AND entity_id = ?`,
			tenantID, driverID); err != nil {
			return fmt.Errorf("scrub driver audit: %w", err)
		}
		for _, q := range []string{
			`DELETE FROM planned_stays WHERE tenant_id = ? AND driver_id = ?`,
			`DELETE FROM stays WHERE tenant_id = ? AND driver_id = ?`,
			`DELETE FROM events WHERE tenant_id = ? AND driver_id = ?`,
			`DELETE FROM telegram_link_tokens WHERE tenant_id = ? AND driver_id = ?`,
			`DELETE FROM telegram_alert_log WHERE tenant_id = ? AND driver_id = ?`,
			`DELETE FROM drivers WHERE tenant_id = ? AND id = ?`,
		} {
			if _, err := tx.DB.ExecContext(ctx, q, tenantID, driverID); err != nil {
				return fmt.Errorf("delete driver (%s): %w", strings.Fields(q)[2], err)
			}
		}
		// Rows another firm once forged onto this driver (migration 0014),
		// kept aside for review: they carry the other firm's tenant_id, but
		// they are about him. The driver was checked above to be this firm's.
		if _, err := tx.DB.ExecContext(ctx, `DELETE FROM stays_cross_tenant WHERE driver_id = ?`, driverID); err != nil {
			return fmt.Errorf("delete driver (stays_cross_tenant): %w", err)
		}
		return nil
	})
}

// --- export ---

// DriverExport is everything stored about one driver, for the firm's CSV
// export and the driver's /moji_podaci.
type DriverExport struct {
	Driver       Driver
	CreatedAt    time.Time
	PrivacyAckAt *time.Time
	Stays        []Stay
	Plans        []PlannedStay
}

func (s *Store) ExportDriver(ctx context.Context, tenantID, driverID int64) (*DriverExport, error) {
	d, err := s.GetDriver(ctx, tenantID, driverID)
	if err != nil {
		return nil, err
	}
	out := &DriverExport{Driver: *d}
	var created string
	if err := s.DB.QueryRowContext(ctx,
		`SELECT created_at FROM drivers WHERE tenant_id = ? AND id = ?`, tenantID, driverID).Scan(&created); err != nil {
		return nil, fmt.Errorf("driver created_at: %w", err)
	}
	out.CreatedAt, _ = parseTimestamp(created)
	if out.PrivacyAckAt, err = s.DriverPrivacyAck(ctx, tenantID, driverID); err != nil {
		return nil, err
	}
	if out.Stays, err = s.ListStaysByDriver(ctx, tenantID, driverID); err != nil {
		return nil, err
	}
	if out.Plans, err = s.ListPlannedStaysByDriver(ctx, tenantID, driverID); err != nil {
		return nil, err
	}
	return out, nil
}
