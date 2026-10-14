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

// Issuing a new link voids the driver's (or user's) earlier unused ones, so
// only the last link sent out can be redeemed.
func (s *Store) CreateDriverLinkToken(ctx context.Context, token string, tenantID, driverID int64, ttl time.Duration) error {
	if _, err := s.DB.ExecContext(ctx,
		`DELETE FROM telegram_link_tokens WHERE tenant_id = ? AND driver_id = ? AND used_at IS NULL`,
		tenantID, driverID); err != nil {
		return fmt.Errorf("void driver link tokens: %w", err)
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO telegram_link_tokens (token_hash, tenant_id, driver_id, expires_at) VALUES (?, ?, ?, ?)`,
		hashToken(token), tenantID, driverID, time.Now().Add(ttl).UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("create driver link token: %w", err)
	}
	return nil
}

func (s *Store) CreateUserLinkToken(ctx context.Context, token string, tenantID, userID int64, ttl time.Duration) error {
	if _, err := s.DB.ExecContext(ctx,
		`DELETE FROM telegram_link_tokens WHERE tenant_id = ? AND user_id = ? AND used_at IS NULL`,
		tenantID, userID); err != nil {
		return fmt.Errorf("void user link tokens: %w", err)
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO telegram_link_tokens (token_hash, tenant_id, user_id, expires_at) VALUES (?, ?, ?, ?)`,
		hashToken(token), tenantID, userID, time.Now().Add(ttl).UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("create user link token: %w", err)
	}
	return nil
}

// ConsumeLinkToken redeems an unused, unexpired token. Claiming it is one
// UPDATE guarded by used_at IS NULL, so under concurrent webhook deliveries
// exactly one caller gets the token; the others see it as already used.
func (s *Store) ConsumeLinkToken(ctx context.Context, token string) (*LinkToken, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	lt := LinkToken{Token: token}
	var expiresAt string
	err := s.DB.QueryRowContext(ctx,
		`UPDATE telegram_link_tokens SET used_at = ?
		 WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?
		 RETURNING tenant_id, driver_id, user_id, expires_at`,
		now, hashToken(token), now,
	).Scan(&lt.TenantID, &lt.DriverID, &lt.UserID, &expiresAt)
	if err == nil {
		lt.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)
		return &lt, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("consume link token: %w", err)
	}

	// Not claimed: say why.
	var usedAt sql.NullString
	err = s.DB.QueryRowContext(ctx,
		`SELECT used_at FROM telegram_link_tokens WHERE token_hash = ?`, hashToken(token),
	).Scan(&usedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lookup link token: %w", err)
	}
	if usedAt.Valid {
		return nil, &ValidationError{Msg: "link je već iskorišćen"}
	}
	return nil, &ValidationError{Msg: "link je istekao"}
}

// DeleteExpiredLinkTokens removes link tokens past their expiry, used or
// not; a used one has done its job, an expired one can no longer be redeemed.
func (s *Store) DeleteExpiredLinkTokens(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM telegram_link_tokens WHERE expires_at < ?`, now.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, fmt.Errorf("delete expired link tokens: %w", err)
	}
	return res.RowsAffected()
}

func (s *Store) SetDriverTelegramChatID(ctx context.Context, tenantID, driverID, chatID int64) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE drivers SET telegram_chat_id = ? WHERE tenant_id = ? AND id = ?`, chatID, tenantID, driverID)
	if isUniqueConstraintErr(err) {
		// One Telegram account maps to one driver across all firms — the
		// shared bot relies on that to know whose message it is.
		return &ValidationError{Msg: "Ovaj Telegram nalog je već povezan sa drugim vozačem."}
	}
	if err != nil {
		return fmt.Errorf("set driver telegram chat id: %w", err)
	}
	return nil
}

func (s *Store) SetUserTelegramChatID(ctx context.Context, tenantID, userID, chatID int64) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE users SET telegram_chat_id = ? WHERE tenant_id = ? AND id = ?`, chatID, tenantID, userID)
	if isUniqueConstraintErr(err) {
		return &ValidationError{Msg: "Ovaj Telegram nalog je već povezan sa drugim korisnikom."}
	}
	if err != nil {
		return fmt.Errorf("set user telegram chat id: %w", err)
	}
	return nil
}

// GetDriverByTelegramChat finds the driver linked to a Telegram chat, in any
// firm. With one shared bot the chat is what tells us the firm: a Telegram
// account can be linked to at most one driver system-wide (unique index on
// drivers.telegram_chat_id), so the answer is never ambiguous. A deactivated
// driver is not found: he no longer writes through the bot.
func (s *Store) GetDriverByTelegramChat(ctx context.Context, chatID int64) (*Driver, error) {
	var d Driver
	var active int
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, tenant_id, name, vehicle_label, telegram_chat_id, active FROM drivers WHERE telegram_chat_id = ? AND active = 1`,
		chatID,
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

// GetUserByTelegramChat finds the web user (owner/dispatcher) linked to a
// Telegram chat, in any firm; unique system-wide like the driver link. A
// deactivated user is not found, like a deactivated driver (deactivation
// also unlinks the chat; this is the second lock).
func (s *Store) GetUserByTelegramChat(ctx context.Context, chatID int64) (*User, error) {
	u, err := scanUser(s.DB.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE telegram_chat_id = ? AND active = 1`, chatID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user by telegram chat: %w", err)
	}
	return u, nil
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

// ReleaseAlert gives back a slot claimed by TryRecordAlert, for a
// notification that could not be sent, so the next attempt sends it.
func (s *Store) ReleaseAlert(ctx context.Context, tenantID, driverID int64, alertType, date string) error {
	_, err := s.DB.ExecContext(ctx,
		`DELETE FROM telegram_alert_log WHERE tenant_id = ? AND driver_id = ? AND alert_type = ? AND alert_date = ?`,
		tenantID, driverID, alertType, date)
	if err != nil {
		return fmt.Errorf("release alert dedup: %w", err)
	}
	return nil
}
