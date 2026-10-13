package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Session struct {
	Token      string
	TenantID   int64
	UserID     int64
	CSRFSecret string
	ExpiresAt  time.Time
}

func (s *Store) CreateSession(ctx context.Context, token string, tenantID, userID int64, csrfSecret string, expiresAt time.Time) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO sessions (token, tenant_id, user_id, csrf_secret, expires_at) VALUES (?, ?, ?, ?, ?)`,
		token, tenantID, userID, csrfSecret, expiresAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (s *Store) GetSession(ctx context.Context, token string) (*Session, error) {
	var sess Session
	var expiresAt string
	err := s.DB.QueryRowContext(ctx,
		`SELECT token, tenant_id, user_id, csrf_secret, expires_at FROM sessions WHERE token = ?`, token,
	).Scan(&sess.Token, &sess.TenantID, &sess.UserID, &sess.CSRFSecret, &expiresAt)
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
	return &sess, nil
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, token)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (s *Store) DeleteExpiredSessions(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("delete expired sessions: %w", err)
	}
	return nil
}
