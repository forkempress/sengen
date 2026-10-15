package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"shengen/internal/clock"
)

// Outcomes recorded in document_imports.status.
const (
	DocImportOK       = "ok"
	DocImportEmpty    = "empty"
	DocImportTooLarge = "too_large"
	DocImportError    = "error"
)

// DocumentImport is one scan sent to Claude and what came of it.
type DocumentImport struct {
	ID           int64
	TenantID     int64
	TenantName   string
	UserID       *int64
	Status       string
	Model        string
	InputTokens  int64
	OutputTokens int64
	CostMicroUSD int64
	StaysRead    int
	DriversRead  int
	RowsValid    int
	CreatedAt    time.Time

	ConfirmedAt    *time.Time
	StaysWritten   int
	DriversWritten int
}

// RecordDocumentImport stores a read and returns its id.
func (s *Store) RecordDocumentImport(ctx context.Context, d DocumentImport) (int64, error) {
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO document_imports (tenant_id, user_id, status, model, input_tokens, output_tokens,
		   cost_microusd, stays_read, drivers_read, rows_valid, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.TenantID, d.UserID, d.Status, d.Model, d.InputTokens, d.OutputTokens,
		d.CostMicroUSD, d.StaysRead, d.DriversRead, d.RowsValid, formatTimestamp(clock.Now()))
	if err != nil {
		return 0, fmt.Errorf("record document import: %w", err)
	}
	return res.LastInsertId()
}

// ConfirmDocumentImport records what the user wrote from a read. The id
// comes back from the browser, so it must belong to tenantID and count only
// once; anything else is ignored.
func (s *Store) ConfirmDocumentImport(ctx context.Context, tenantID, id int64, stays, drivers int) error {
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE document_imports SET confirmed_at = ?, stays_written = ?, drivers_written = ?
		 WHERE id = ? AND tenant_id = ? AND status = ? AND confirmed_at IS NULL`,
		formatTimestamp(clock.Now()), stays, drivers, id, tenantID, DocImportOK); err != nil {
		return fmt.Errorf("confirm document import: %w", err)
	}
	return nil
}

const docImportColumns = `d.id, d.tenant_id, t.name, d.user_id, d.status, d.model, d.input_tokens, d.output_tokens,
	d.cost_microusd, d.stays_read, d.drivers_read, d.rows_valid, d.created_at,
	d.confirmed_at, d.stays_written, d.drivers_written`

// ListDocumentImportsSince returns reads made at or after since, oldest first.
func (s *Store) ListDocumentImportsSince(ctx context.Context, since time.Time) ([]DocumentImport, error) {
	return s.listDocumentImports(ctx, `d.created_at >= ? ORDER BY d.created_at, d.id`, formatTimestamp(since))
}

// ListDocumentImportsConfirmedSince returns reads confirmed at or after
// since, oldest confirmation first.
func (s *Store) ListDocumentImportsConfirmedSince(ctx context.Context, since time.Time) ([]DocumentImport, error) {
	return s.listDocumentImports(ctx, `d.confirmed_at >= ? ORDER BY d.confirmed_at, d.id`, formatTimestamp(since))
}

func (s *Store) listDocumentImports(ctx context.Context, where string, args ...any) ([]DocumentImport, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+docImportColumns+` FROM document_imports d JOIN tenants t ON t.id = d.tenant_id WHERE `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("list document imports: %w", err)
	}
	defer rows.Close()
	var out []DocumentImport
	for rows.Next() {
		var d DocumentImport
		var userID sql.NullInt64
		var created string
		var confirmed sql.NullString
		if err := rows.Scan(&d.ID, &d.TenantID, &d.TenantName, &userID, &d.Status, &d.Model, &d.InputTokens, &d.OutputTokens,
			&d.CostMicroUSD, &d.StaysRead, &d.DriversRead, &d.RowsValid, &created,
			&confirmed, &d.StaysWritten, &d.DriversWritten); err != nil {
			return nil, fmt.Errorf("scan document import: %w", err)
		}
		if userID.Valid {
			d.UserID = &userID.Int64
		}
		d.CreatedAt, _ = parseTimestamp(created)
		if confirmed.Valid {
			t, _ := parseTimestamp(confirmed.String)
			d.ConfirmedAt = &t
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DocumentImportSpend is how many reads were made and what they cost.
type DocumentImportSpend struct {
	Count        int
	CostMicroUSD int64
}

// DocumentImportSpendSince sums reads made at or after since, for one firm
// (tenantID > 0) or for all of them (0).
func (s *Store) DocumentImportSpendSince(ctx context.Context, tenantID int64, since time.Time) (DocumentImportSpend, error) {
	var sp DocumentImportSpend
	if err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(cost_microusd), 0) FROM document_imports
		 WHERE created_at >= ? AND (? = 0 OR tenant_id = ?)`,
		formatTimestamp(since), tenantID, tenantID,
	).Scan(&sp.Count, &sp.CostMicroUSD); err != nil {
		return sp, fmt.Errorf("document import spend: %w", err)
	}
	return sp, nil
}
