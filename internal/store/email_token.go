package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Purposes of an email token (email_tokens.purpose).
const (
	EmailTokenSignup = "signup"
	EmailTokenReset  = "reset"
	EmailTokenInvite = "invite"
)

// EmailToken is a one-time link sent by email. Only its hash is stored; the
// token itself exists only in the message.
type EmailToken struct {
	Purpose  string
	Email    string
	TenantID *int64
	UserID   *int64
	Payload  string // JSON, "{}" when unused
}

// ErrTokenUsed and ErrTokenExpired say why a token was refused, so the page
// can tell "already used" from "too old". A token that never existed (or was
// voided by a newer one) is ErrNotFound.
var (
	ErrTokenUsed    = errors.New("token already used")
	ErrTokenExpired = errors.New("token expired")
)

// CreateEmailToken stores a new token valid for ttl. Earlier unused tokens of
// the same purpose for the same address (within the same firm, for firm
// tokens) are voided, so only the link in the latest email works.
func (s *Store) CreateEmailToken(ctx context.Context, token string, et EmailToken, ttl time.Duration) error {
	et.Email = NormalizeEmail(et.Email)
	if et.Payload == "" {
		et.Payload = "{}"
	}
	return s.WithTx(ctx, func(tx *Store) error {
		if _, err := tx.DB.ExecContext(ctx,
			`DELETE FROM email_tokens WHERE purpose = ? AND email = ? AND tenant_id IS ? AND used_at IS NULL`,
			et.Purpose, et.Email, et.TenantID); err != nil {
			return fmt.Errorf("void email tokens: %w", err)
		}
		if _, err := tx.DB.ExecContext(ctx,
			`INSERT INTO email_tokens (token_hash, purpose, email, tenant_id, user_id, payload, expires_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			hashToken(token), et.Purpose, et.Email, et.TenantID, et.UserID, et.Payload,
			formatTimestamp(time.Now().Add(ttl))); err != nil {
			return fmt.Errorf("create email token: %w", err)
		}
		return nil
	})
}

// PeekEmailToken returns a token that could still be redeemed, without
// redeeming it: the page behind a link checks the link before showing its
// form, and a mail scanner opening the link must not use it up.
func (s *Store) PeekEmailToken(ctx context.Context, token, purpose string) (*EmailToken, error) {
	var et EmailToken
	var expiresAt string
	var usedAt sql.NullString
	err := s.DB.QueryRowContext(ctx,
		`SELECT purpose, email, tenant_id, user_id, payload, expires_at, used_at
		 FROM email_tokens WHERE token_hash = ? AND purpose = ?`, hashToken(token), purpose,
	).Scan(&et.Purpose, &et.Email, &et.TenantID, &et.UserID, &et.Payload, &expiresAt, &usedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("peek email token: %w", err)
	}
	if usedAt.Valid {
		return nil, ErrTokenUsed
	}
	if expiresAt <= formatTimestamp(time.Now()) {
		return nil, ErrTokenExpired
	}
	return &et, nil
}

// ConsumeEmailToken redeems a token. Like ConsumeLinkToken, claiming it is
// one UPDATE guarded by used_at IS NULL, so of two requests racing with the
// same link exactly one gets it.
func (s *Store) ConsumeEmailToken(ctx context.Context, token, purpose string) (*EmailToken, error) {
	now := formatTimestamp(time.Now())
	var et EmailToken
	err := s.DB.QueryRowContext(ctx,
		`UPDATE email_tokens SET used_at = ?
		 WHERE token_hash = ? AND purpose = ? AND used_at IS NULL AND expires_at > ?
		 RETURNING purpose, email, tenant_id, user_id, payload`,
		now, hashToken(token), purpose, now,
	).Scan(&et.Purpose, &et.Email, &et.TenantID, &et.UserID, &et.Payload)
	if err == nil {
		return &et, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("consume email token: %w", err)
	}
	// Not claimed: say why.
	_, err = s.PeekEmailToken(ctx, token, purpose)
	if err == nil {
		// Became redeemable between the two statements; cannot happen with
		// one connection, but never report success for an unclaimed token.
		return nil, ErrTokenUsed
	}
	return nil, err
}

// DeleteExpiredEmailTokens removes tokens past their expiry, used or not.
func (s *Store) DeleteExpiredEmailTokens(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM email_tokens WHERE expires_at < ?`, formatTimestamp(now))
	if err != nil {
		return 0, fmt.Errorf("delete expired email tokens: %w", err)
	}
	return res.RowsAffected()
}
