package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type User struct {
	ID           int64
	TenantID     int64
	Email        string
	PasswordHash string
	Role         string
	Name         string
	// TelegramChatID is set once the user has linked their Telegram account
	// (admin commands, and optionally the firm's alerts).
	TelegramChatID *int64
	// Active is false once the owner has deactivated the account: it can no
	// longer sign in or use the bot.
	Active bool
}

// userColumns and scanUser keep every users query reading the same fields.
const userColumns = `id, tenant_id, email, password_hash, role, name, telegram_chat_id, active`

type rowScanner interface{ Scan(dest ...any) error }

func scanUser(row rowScanner) (*User, error) {
	var u User
	var active int
	if err := row.Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Role, &u.Name, &u.TelegramChatID, &active); err != nil {
		return nil, err
	}
	u.Active = active == 1
	return &u, nil
}

// GetUserByEmail finds the account across all firms: the email alone decides
// which firm a person signs in to (it is unique system-wide, migration 0013).
func (s *Store) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	email = NormalizeEmail(email)
	u, err := scanUser(s.DB.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE email = ?`, email))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user by email: %w", err)
	}
	return u, nil
}

func (s *Store) GetUser(ctx context.Context, tenantID, id int64) (*User, error) {
	u, err := scanUser(s.DB.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE tenant_id = ? AND id = ?`, tenantID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return u, nil
}

// NormalizeEmail is the single rule for comparing email addresses: login,
// seeding and the users page all go through it, so an address typed with
// capitals or stray spaces can never become an account nobody can log into.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func (s *Store) CreateUser(ctx context.Context, tenantID int64, email, passwordHash, role, name string) (*User, error) {
	email = NormalizeEmail(email)
	if err := checkLen("ime", name, MaxNameLen); err != nil {
		return nil, err
	}
	if err := checkLen("email", email, MaxEmailLen); err != nil {
		return nil, err
	}
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO users (tenant_id, email, password_hash, role, name) VALUES (?, ?, ?, ?, ?)`,
		tenantID, email, passwordHash, role, name)
	if isUniqueConstraintErr(err) {
		return nil, &ValidationError{Msg: "korisnik sa tim emailom već postoji"}
	}
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	id, _ := res.LastInsertId()
	return &User{ID: id, TenantID: tenantID, Email: email, PasswordHash: passwordHash, Role: role, Name: name, Active: true}, nil
}

// ListUsers returns the tenant's web users: active first, then owners
// first, then by name.
func (s *Store) ListUsers(ctx context.Context, tenantID int64) ([]User, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+userColumns+` FROM users
		 WHERE tenant_id = ? ORDER BY active DESC, role = 'owner' DESC, name`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

// ErrLastOwner refuses a change that would leave the firm without an active
// owner: nobody could manage its users or Telegram settings any more.
var ErrLastOwner = &ValidationError{Msg: "Firma mora imati bar jednog aktivnog vlasnika."}

// SetUserActive deactivates or reactivates a user of the firm and returns
// the user as it was before. Deactivation also ends every session of the
// user and unlinks his Telegram, so a person who left loses web and bot
// access at once rather than when a session would have expired.
func (s *Store) SetUserActive(ctx context.Context, tenantID, userID int64, active bool) (*User, error) {
	var before *User
	err := s.WithTx(ctx, func(tx *Store) error {
		u, err := tx.GetUser(ctx, tenantID, userID)
		if err != nil {
			return err
		}
		before = u
		if !active && u.Active && u.Role == "owner" {
			if err := tx.requireAnotherActiveOwner(ctx, tenantID, userID); err != nil {
				return err
			}
		}
		if _, err := tx.DB.ExecContext(ctx,
			`UPDATE users SET active = ? WHERE tenant_id = ? AND id = ?`, boolToInt(active), tenantID, userID); err != nil {
			return fmt.Errorf("set user active: %w", err)
		}
		if !active {
			return tx.revokeUserAccess(ctx, tenantID, u)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return before, nil
}

// SetUserRole changes a user's role and returns the user as it was before.
// An owner can be made a dispatcher only while another active owner remains.
func (s *Store) SetUserRole(ctx context.Context, tenantID, userID int64, role string) (*User, error) {
	var before *User
	err := s.WithTx(ctx, func(tx *Store) error {
		u, err := tx.GetUser(ctx, tenantID, userID)
		if err != nil {
			return err
		}
		before = u
		if u.Role == "owner" && role != "owner" && u.Active {
			if err := tx.requireAnotherActiveOwner(ctx, tenantID, userID); err != nil {
				return err
			}
		}
		if _, err := tx.DB.ExecContext(ctx,
			`UPDATE users SET role = ? WHERE tenant_id = ? AND id = ?`, role, tenantID, userID); err != nil {
			return fmt.Errorf("set user role: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return before, nil
}

// SetUserPassword stores a new password hash and, like deactivation, ends
// every session and unlinks Telegram: whoever knew the old password loses
// what they did with it. The caller signs the current browser in again.
func (s *Store) SetUserPassword(ctx context.Context, tenantID, userID int64, passwordHash string) error {
	return s.WithTx(ctx, func(tx *Store) error {
		u, err := tx.GetUser(ctx, tenantID, userID)
		if err != nil {
			return err
		}
		if _, err := tx.DB.ExecContext(ctx,
			`UPDATE users SET password_hash = ? WHERE tenant_id = ? AND id = ?`, passwordHash, tenantID, userID); err != nil {
			return fmt.Errorf("set user password: %w", err)
		}
		return tx.revokeUserAccess(ctx, tenantID, u)
	})
}

func (s *Store) requireAnotherActiveOwner(ctx context.Context, tenantID, exceptUserID int64) error {
	var n int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE tenant_id = ? AND role = 'owner' AND active = 1 AND id != ?`,
		tenantID, exceptUserID).Scan(&n); err != nil {
		return fmt.Errorf("count owners: %w", err)
	}
	if n == 0 {
		return ErrLastOwner
	}
	return nil
}

// revokeUserAccess deletes the user's sessions and unused Telegram link
// tokens and unlinks his Telegram chat. If the firm's alerts went to that
// chat they stop, instead of reaching someone who no longer has access.
func (s *Store) revokeUserAccess(ctx context.Context, tenantID int64, u *User) error {
	if _, err := s.DB.ExecContext(ctx,
		`DELETE FROM sessions WHERE tenant_id = ? AND user_id = ?`, tenantID, u.ID); err != nil {
		return fmt.Errorf("delete user sessions: %w", err)
	}
	if _, err := s.DB.ExecContext(ctx,
		`DELETE FROM telegram_link_tokens WHERE tenant_id = ? AND user_id = ? AND used_at IS NULL`, tenantID, u.ID); err != nil {
		return fmt.Errorf("delete user link tokens: %w", err)
	}
	if u.TelegramChatID == nil {
		return nil
	}
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE users SET telegram_chat_id = NULL WHERE tenant_id = ? AND id = ?`, tenantID, u.ID); err != nil {
		return fmt.Errorf("unlink user telegram: %w", err)
	}
	t, err := s.GetTenant(ctx, tenantID)
	if err != nil {
		return err
	}
	settings, err := t.ParsedSettings()
	if err != nil {
		return err
	}
	if settings.TelegramAlertChatID == *u.TelegramChatID {
		settings.TelegramAlertChatID = 0
		return s.UpdateTenantSettings(ctx, tenantID, settings)
	}
	return nil
}
