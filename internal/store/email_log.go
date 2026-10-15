package store

import (
	"context"
	"fmt"
	"time"
)

// Outcomes recorded in email_log.status.
const (
	EmailSent      = "sent"
	EmailFailed    = "failed"
	EmailOverLimit = "over_limit"
)

// maxEmailErrLen caps the provider error kept per message.
const maxEmailErrLen = 300

// RecordEmail logs one attempt to send a message. tenantID is nil for mail
// that belongs to no firm yet (a sign-up being confirmed).
func (s *Store) RecordEmail(ctx context.Context, tag string, tenantID *int64, recipient, status, errText string) error {
	if r := []rune(errText); len(r) > maxEmailErrLen {
		errText = string(r[:maxEmailErrLen])
	}
	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO email_log (tag, tenant_id, recipient, status, error, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		tag, tenantID, NormalizeEmail(recipient), status, errText, formatTimestamp(time.Now())); err != nil {
		return fmt.Errorf("record email: %w", err)
	}
	return nil
}

// CountEmailsSentSince counts messages handed to the provider since t —
// what the daily sending cap is checked against.
func (s *Store) CountEmailsSentSince(ctx context.Context, t time.Time) (int, error) {
	var n int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM email_log WHERE status = ? AND created_at >= ?`, EmailSent, formatTimestamp(t),
	).Scan(&n); err != nil {
		return 0, fmt.Errorf("count emails: %w", err)
	}
	return n, nil
}

// DeleteEmailLogBefore drops log rows older than cutoff.
func (s *Store) DeleteEmailLogBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM email_log WHERE created_at < ?`, formatTimestamp(cutoff))
	if err != nil {
		return 0, fmt.Errorf("delete email log: %w", err)
	}
	return res.RowsAffected()
}
