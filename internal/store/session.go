package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Session is a signed-in browser. Token is the cookie value; the database
// holds only its hash.
type Session struct {
	Token      string
	TenantID   int64
	UserID     int64
	CSRFSecret string
	ExpiresAt  time.Time
	LastSeenAt time.Time
}

func (s *Store) CreateSession(ctx context.Context, token string, tenantID, userID int64, csrfSecret string, expiresAt time.Time) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, tenant_id, user_id, csrf_secret, expires_at, last_seen_at) VALUES (?, ?, ?, ?, ?, ?)`,
		hashToken(token), tenantID, userID, csrfSecret, expiresAt.UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (s *Store) GetSession(ctx context.Context, token string) (*Session, error) {
	sess := Session{Token: token}
	var expiresAt, lastSeen string
	err := s.DB.QueryRowContext(ctx,
		`SELECT tenant_id, user_id, csrf_secret, expires_at, last_seen_at FROM sessions WHERE token_hash = ?`, hashToken(token),
	).Scan(&sess.TenantID, &sess.UserID, &sess.CSRFSecret, &expiresAt, &lastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	sess.ExpiresAt, err = time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("parse session expiry: %w", err)
	}
	sess.LastSeenAt, err = time.Parse(time.RFC3339, lastSeen)
	if err != nil {
		return nil, fmt.Errorf("parse session last seen: %w", err)
	}
	return &sess, nil
}

// TouchSession records that the session was just used, for the idle expiry.
func (s *Store) TouchSession(ctx context.Context, token string, at time.Time) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE sessions SET last_seen_at = ? WHERE token_hash = ?`, at.UTC().Format(time.RFC3339), hashToken(token))
	if err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	return nil
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteExpiredSessions removes sessions past their absolute expiry or unused
// since idleCutoff, and reports how many went.
func (s *Store) DeleteExpiredSessions(ctx context.Context, now, idleCutoff time.Time) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ? OR last_seen_at < ?`,
		now.UTC().Format(time.RFC3339), idleCutoff.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	return res.RowsAffected()
}

// SetSessionFlash stores a one-time message for the next page this session
// opens. ok and errMsg replace whatever was waiting there.
func (s *Store) SetSessionFlash(ctx context.Context, token, ok, errMsg string) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE sessions SET flash_ok = ?, flash_err = ? WHERE token_hash = ?`, ok, errMsg, hashToken(token))
	if err != nil {
		return fmt.Errorf("set session flash: %w", err)
	}
	return nil
}

// TakeSessionFlash returns the waiting message, if any, and clears it so it
// is shown once.
func (s *Store) TakeSessionFlash(ctx context.Context, token string) (ok, errMsg string, err error) {
	err = s.DB.QueryRowContext(ctx,
		`SELECT flash_ok, flash_err FROM sessions WHERE token_hash = ?`, hashToken(token)).Scan(&ok, &errMsg)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("get session flash: %w", err)
	}
	if ok == "" && errMsg == "" {
		return "", "", nil
	}
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE sessions SET flash_ok = '', flash_err = '' WHERE token_hash = ?`, hashToken(token)); err != nil {
		return "", "", fmt.Errorf("clear session flash: %w", err)
	}
	return ok, errMsg, nil
}
