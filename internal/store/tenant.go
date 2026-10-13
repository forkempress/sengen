package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrNotFound = errors.New("not found")

type Tenant struct {
	ID       int64
	Name     string
	Host     string
	Settings string
}

func (s *Store) GetTenantByHost(ctx context.Context, host string) (*Tenant, error) {
	var t Tenant
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, name, host, settings FROM tenants WHERE host = ?`, host,
	).Scan(&t.ID, &t.Name, &t.Host, &t.Settings)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get tenant by host: %w", err)
	}
	return &t, nil
}

// ListAllTenants returns every tenant — used by background jobs, which scan
// across all tenants rather than being scoped to one request's Host header.
func (s *Store) ListAllTenants(ctx context.Context) ([]Tenant, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, name, host, settings FROM tenants ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list all tenants: %w", err)
	}
	defer rows.Close()
	var out []Tenant
	for rows.Next() {
		var t Tenant
		if err := rows.Scan(&t.ID, &t.Name, &t.Host, &t.Settings); err != nil {
			return nil, fmt.Errorf("scan tenant: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) CreateTenant(ctx context.Context, name, host string) (*Tenant, error) {
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO tenants (name, host, settings) VALUES (?, ?, '{}')`, name, host)
	if err != nil {
		return nil, fmt.Errorf("create tenant: %w", err)
	}
	id, _ := res.LastInsertId()
	return &Tenant{ID: id, Name: name, Host: host, Settings: "{}"}, nil
}
