package store

import (
	"context"
	"fmt"
	"time"
)

func (s *Store) RecordAudit(ctx context.Context, tenantID int64, actorType string, actorID *int64, entity string, entityID int64, action string, beforeJSON, afterJSON *string) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO audit_log (tenant_id, actor_type, actor_id, entity, entity_id, action, before_json, after_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		tenantID, actorType, actorID, entity, entityID, action, beforeJSON, afterJSON)
	if err != nil {
		return fmt.Errorf("record audit: %w", err)
	}
	return nil
}

type AuditEntry struct {
	ID         int64
	ActorType  string
	ActorID    *int64
	Entity     string
	EntityID   int64
	Action     string
	OccurredAt time.Time
}

// ListRecentAudit returns the most recent audit entries for a tenant,
// newest first — backs the Telegram admin /ispravi command.
func (s *Store) ListRecentAudit(ctx context.Context, tenantID int64, limit int) ([]AuditEntry, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, actor_type, actor_id, entity, entity_id, action, occurred_at
		 FROM audit_log WHERE tenant_id = ? ORDER BY occurred_at DESC LIMIT ?`,
		tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent audit: %w", err)
	}
	defer rows.Close()

	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var occurredAt string
		if err := rows.Scan(&e.ID, &e.ActorType, &e.ActorID, &e.Entity, &e.EntityID, &e.Action, &occurredAt); err != nil {
			return nil, fmt.Errorf("scan audit entry: %w", err)
		}
		e.OccurredAt, _ = parseTimestamp(occurredAt)
		out = append(out, e)
	}
	return out, rows.Err()
}
