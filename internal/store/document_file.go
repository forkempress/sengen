package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"shengen/internal/clock"
)

// DocumentFileRetention is how long an uploaded scan is kept for testing
// other models on it.
const DocumentFileRetention = 30 * 24 * time.Hour

// DocumentFile is the scan behind a document import.
type DocumentFile struct {
	ImportID       int64
	TenantID       int64
	MediaType      string
	Data           []byte
	DriverNames    []string
	SingleDriverID *int64
}

// SaveDocumentFile keeps the scan of a read for DocumentFileRetention.
func (s *Store) SaveDocumentFile(ctx context.Context, f DocumentFile) error {
	names, err := json.Marshal(f.DriverNames)
	if err != nil {
		return err
	}
	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO document_files (document_import_id, tenant_id, media_type, data, driver_names, single_driver_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		f.ImportID, f.TenantID, f.MediaType, f.Data, string(names), f.SingleDriverID, formatTimestamp(clock.Now())); err != nil {
		return fmt.Errorf("save document file: %w", err)
	}
	return nil
}

// RecordDocumentWritten stores which drivers a confirmed import wrote to and
// the period its stays cover, so the stays now in the app for that period
// can serve as the correct reading of the scan. Like ConfirmDocumentImport,
// a foreign or repeated id is ignored.
func (s *Store) RecordDocumentWritten(ctx context.Context, tenantID, id int64, driverIDs []int64, from, to time.Time) error {
	var ids strings.Builder
	ids.WriteString(",")
	for _, d := range driverIDs {
		ids.WriteString(strconv.FormatInt(d, 10) + ",")
	}
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE document_imports SET written_driver_ids = ?, period_from = ?, period_to = ?
		 WHERE id = ? AND tenant_id = ? AND written_driver_ids = ''`,
		ids.String(), from.Format("2006-01-02"), to.Format("2006-01-02"), id, tenantID); err != nil {
		return fmt.Errorf("record document written: %w", err)
	}
	return nil
}

// DeleteDocumentFilesBefore removes scans uploaded before cutoff.
func (s *Store) DeleteDocumentFilesBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM document_files WHERE created_at < ?`, formatTimestamp(cutoff))
	if err != nil {
		return 0, fmt.Errorf("delete document files: %w", err)
	}
	return res.RowsAffected()
}

// deleteDriverDocumentFiles removes every kept scan that may show the
// driver: one read on his page, one whose import wrote stays to him, and
// any of the firm's scans not confirmed, whose drivers are unknown.
func (s *Store) deleteDriverDocumentFiles(ctx context.Context, tenantID, driverID int64) error {
	_, err := s.DB.ExecContext(ctx,
		`DELETE FROM document_files WHERE tenant_id = ? AND (single_driver_id = ? OR document_import_id IN (
		   SELECT id FROM document_imports WHERE tenant_id = ? AND (written_driver_ids = '' OR written_driver_ids LIKE ?)))`,
		tenantID, driverID, tenantID, "%,"+strconv.FormatInt(driverID, 10)+",%")
	return err
}

// EvalDocument is a kept scan of a confirmed import with the stays now in
// the app for the drivers it wrote to, over the period it covered.
type EvalDocument struct {
	DocumentFile
	TenantName string
	Model      string
	CreatedAt  time.Time
	// Drivers are the names of the drivers the import wrote to, by id.
	Drivers map[int64]string
	Stays   []Stay
}

// ListEvalDocuments returns every kept scan whose import was confirmed.
func (s *Store) ListEvalDocuments(ctx context.Context) ([]EvalDocument, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT f.document_import_id, f.tenant_id, t.name, f.media_type, f.data, f.driver_names, f.single_driver_id,
		        d.model, d.created_at, d.written_driver_ids, d.period_from, d.period_to
		 FROM document_files f
		 JOIN document_imports d ON d.id = f.document_import_id
		 JOIN tenants t ON t.id = f.tenant_id
		 WHERE d.written_driver_ids != '' ORDER BY f.document_import_id`)
	if err != nil {
		return nil, fmt.Errorf("list eval documents: %w", err)
	}
	type pending struct {
		doc      EvalDocument
		ids      string
		from, to string
	}
	var docs []pending
	for rows.Next() {
		var p pending
		var names, created string
		var single sql.NullInt64
		if err := rows.Scan(&p.doc.ImportID, &p.doc.TenantID, &p.doc.TenantName, &p.doc.MediaType, &p.doc.Data, &names, &single,
			&p.doc.Model, &created, &p.ids, &p.from, &p.to); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan eval document: %w", err)
		}
		if err := json.Unmarshal([]byte(names), &p.doc.DriverNames); err != nil {
			rows.Close()
			return nil, fmt.Errorf("document %d driver names: %w", p.doc.ImportID, err)
		}
		if single.Valid {
			p.doc.SingleDriverID = &single.Int64
		}
		p.doc.CreatedAt, _ = parseTimestamp(created)
		docs = append(docs, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]EvalDocument, 0, len(docs))
	for _, p := range docs {
		p.doc.Drivers = map[int64]string{}
		for _, field := range strings.Split(strings.Trim(p.ids, ","), ",") {
			id, err := strconv.ParseInt(field, 10, 64)
			if err != nil {
				continue
			}
			d, err := s.GetDriver(ctx, p.doc.TenantID, id)
			if err == ErrNotFound {
				continue // deleted since; its scan went with it unless it shared one
			}
			if err != nil {
				return nil, err
			}
			p.doc.Drivers[id] = d.Name
			stays, err := s.ListStaysByDriver(ctx, p.doc.TenantID, id)
			if err != nil {
				return nil, err
			}
			for _, st := range stays {
				// Stays overlapping the period the import covered.
				if st.Entry.Format("2006-01-02") <= p.to && (st.Exit == nil || st.Exit.Format("2006-01-02") >= p.from) {
					p.doc.Stays = append(p.doc.Stays, st)
				}
			}
		}
		out = append(out, p.doc)
	}
	return out, nil
}
