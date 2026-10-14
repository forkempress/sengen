package store

import (
	"context"
	"fmt"
	"time"
)

// MarkTelegramUpdate records that an update arrived. It reports false when
// the update_id was already recorded, i.e. this delivery is Telegram's retry
// of one already being handled.
func (s *Store) MarkTelegramUpdate(ctx context.Context, updateID int64) (bool, error) {
	res, err := s.DB.ExecContext(ctx,
		`INSERT OR IGNORE INTO telegram_updates (update_id, received_at) VALUES (?, ?)`,
		updateID, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return false, fmt.Errorf("mark telegram update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("mark telegram update: %w", err)
	}
	return n == 1, nil
}

// DeleteTelegramUpdatesBefore forgets update_ids received before cutoff.
func (s *Store) DeleteTelegramUpdatesBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM telegram_updates WHERE received_at < ?`, cutoff.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, fmt.Errorf("delete old telegram updates: %w", err)
	}
	return res.RowsAffected()
}
