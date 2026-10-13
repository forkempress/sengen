package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// isUniqueConstraintErr matches modernc.org/sqlite's error string for a
// UNIQUE/PRIMARY KEY constraint violation. The driver doesn't expose a typed
// sentinel for this, so we match on SQLite's own message text.
func isUniqueConstraintErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// LinkToken is a one-time, expiring token used to bind a Telegram chat_id to
// either a driver or an admin user (exactly one of DriverID/UserID is set —
// enforced by the DB CHECK constraint).
type LinkToken struct {
	Token     string
	TenantID  int64
	DriverID  *int64
	UserID    *int64
	ExpiresAt time.Time
	UsedAt    *time.Time
}

func (s *Store) CreateDriverLinkToken(ctx context.Context, token string, tenantID, driverID int64, ttl time.Duration) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO telegram_link_tokens (token, tenant_id, driver_id, expires_at) VALUES (?, ?, ?, ?)`,
		token, tenantID, driverID, time.Now().Add(ttl).UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("create driver link token: %w", err)
	}
	return nil
}

func (s *Store) CreateUserLinkToken(ctx context.Context, token string, tenantID, userID int64, ttl time.Duration) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO telegram_link_tokens (token, tenant_id, user_id, expires_at) VALUES (?, ?, ?, ?)`,
		token, tenantID, userID, time.Now().Add(ttl).UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("create user link token: %w", err)
	}
	return nil
}

// ConsumeLinkToken looks up a token, verifies it is unused and unexpired,
// and marks it used, all in one call so a token can never be redeemed twice
// even under concurrent webhook deliveries (SQLite's single-writer
// serialization makes this read-then-update safe).
func (s *Store) ConsumeLinkToken(ctx context.Context, token string) (*LinkToken, error) {
	var lt LinkToken
	var expiresAt string
	var usedAt sql.NullString
	err := s.DB.QueryRowContext(ctx,
		`SELECT token, tenant_id, driver_id, user_id, expires_at, used_at FROM telegram_link_tokens WHERE token = ?`,
		token,
	).Scan(&lt.Token, &lt.TenantID, &lt.DriverID, &lt.UserID, &expiresAt, &usedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lookup link token: %w", err)
	}
	lt.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)
	if usedAt.Valid {
		return nil, &ValidationError{Msg: "link je već iskorišćen"}
	}
	if time.Now().After(lt.ExpiresAt) {
		return nil, &ValidationError{Msg: "link je istekao"}
	}
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE telegram_link_tokens SET used_at = ? WHERE token = ?`,
		time.Now().UTC().Format(time.RFC3339), token,
	); err != nil {
		return nil, fmt.Errorf("mark link token used: %w", err)
	}
	return &lt, nil
}

func (s *Store) SetDriverTelegramChatID(ctx context.Context, tenantID, driverID, chatID int64) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE drivers SET telegram_chat_id = ? WHERE tenant_id = ? AND id = ?`, chatID, tenantID, driverID)
	if err != nil {
		return fmt.Errorf("set driver telegram chat id: %w", err)
	}
	return nil
}

func (s *Store) SetUserTelegramChatID(ctx context.Context, tenantID, userID, chatID int64) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE users SET telegram_chat_id = ? WHERE tenant_id = ? AND id = ?`, chatID, tenantID, userID)
	if err != nil {
		return fmt.Errorf("set user telegram chat id: %w", err)
	}
	return nil
}

// GetDriverByTelegramChat resolves a driver by (tenant_id, chat_id) — used by
// the webhook to recognize a known driver chat. tenant_id is already known
// from the Host header, chat_id alone isn't tenant-scoped on Telegram's side.
func (s *Store) GetDriverByTelegramChat(ctx context.Context, tenantID, chatID int64) (*Driver, error) {
	var d Driver
	var active int
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, tenant_id, name, vehicle_label, telegram_chat_id, active FROM drivers WHERE tenant_id = ? AND telegram_chat_id = ?`,
		tenantID, chatID,
	).Scan(&d.ID, &d.TenantID, &d.Name, &d.VehicleLabel, &d.TelegramChatID, &active)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get driver by telegram chat: %w", err)
	}
	d.Active = active == 1
	return &d, nil
}

func (s *Store) GetUserByTelegramChat(ctx context.Context, tenantID, chatID int64) (*User, error) {
	var u User
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, tenant_id, email, password_hash, role, name FROM users WHERE tenant_id = ? AND telegram_chat_id = ?`,
		tenantID, chatID,
	).Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Role, &u.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user by telegram chat: %w", err)
	}
	return &u, nil
}

// TryRecordAlert attempts to claim the dedup slot for (tenant, driver,
// alertType, date). It returns sent=true if this call claimed it (the
// caller should proceed to send the notification) or sent=false if another
// call already claimed it today (skip — already sent). driverID 0 is the
// sentinel for tenant-wide alerts (e.g. the daily digest).
func (s *Store) TryRecordAlert(ctx context.Context, tenantID, driverID int64, alertType, date string) (bool, error) {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO telegram_alert_log (tenant_id, driver_id, alert_type, alert_date) VALUES (?, ?, ?, ?)`,
		tenantID, driverID, alertType, date)
	if err == nil {
		return true, nil
	}
	if isUniqueConstraintErr(err) {
		return false, nil
	}
	return false, fmt.Errorf("record alert dedup: %w", err)
}
