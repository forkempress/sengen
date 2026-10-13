// Package store is the data access layer: every query is scoped by tenant_id
// so that no handler can accidentally leak data across tenants.
package store

import (
	"context"
	"database/sql"
	"fmt"
)

// DBTX is satisfied by both *sql.DB and *sql.Tx, so every Store method works
// unmodified whether it's running against the pool or inside a transaction
// (see WithTx, used by the CSV import confirm step to commit all-or-nothing).
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type Store struct {
	DB DBTX
	db *sql.DB // only set on the top-level Store; needed to open transactions
}

func New(db *sql.DB) *Store {
	return &Store{DB: db, db: db}
}

// WithTx runs fn inside a database transaction, committing if fn returns nil
// and rolling back otherwise. It must be called on the top-level Store
// (not on a Store already wrapping a transaction).
func (s *Store) WithTx(ctx context.Context, fn func(txStore *Store) error) error {
	if s.db == nil {
		return fmt.Errorf("WithTx: not a top-level Store (already inside a transaction)")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if err := fn(&Store{DB: tx}); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

const dateLayout = "2006-01-02"
