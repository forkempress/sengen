package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var ErrNotFound = errors.New("not found")

// Tenant is one transport company. Firms are not told apart by web address
// — everyone signs in at the same address and the account decides the firm —
// so Slug is only a short unique handle for operators (cmd/seed uses it to
// find an existing firm).
type Tenant struct {
	ID       int64
	Name     string
	Slug     string
	Settings string
}

const tenantColumns = `id, name, slug, settings`

func scanTenant(row *sql.Row) (*Tenant, error) {
	var t Tenant
	err := row.Scan(&t.ID, &t.Name, &t.Slug, &t.Settings)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get tenant: %w", err)
	}
	return &t, nil
}

// GetTenant loads a firm by id — the id comes from the signed-in session or
// from the Telegram account that sent a message, never from the request URL.
func (s *Store) GetTenant(ctx context.Context, id int64) (*Tenant, error) {
	return scanTenant(s.DB.QueryRowContext(ctx, `SELECT `+tenantColumns+` FROM tenants WHERE id = ?`, id))
}

func (s *Store) GetTenantBySlug(ctx context.Context, slug string) (*Tenant, error) {
	return scanTenant(s.DB.QueryRowContext(ctx, `SELECT `+tenantColumns+` FROM tenants WHERE slug = ?`, slug))
}

// ListAllTenants returns every tenant — used by background jobs, which scan
// across all firms rather than serving one signed-in user.
func (s *Store) ListAllTenants(ctx context.Context) ([]Tenant, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+tenantColumns+` FROM tenants ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list all tenants: %w", err)
	}
	defer rows.Close()
	var out []Tenant
	for rows.Next() {
		var t Tenant
		if err := rows.Scan(&t.ID, &t.Name, &t.Slug, &t.Settings); err != nil {
			return nil, fmt.Errorf("scan tenant: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) CreateTenant(ctx context.Context, name, slug string) (*Tenant, error) {
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO tenants (name, slug, settings, trial_until) VALUES (?, ?, '{}', date('now', '+30 days'))`, name, slug)
	if isUniqueConstraintErr(err) {
		return nil, &ValidationError{Msg: fmt.Sprintf("firma sa oznakom %q već postoji", slug)}
	}
	if err != nil {
		return nil, fmt.Errorf("create tenant: %w", err)
	}
	id, _ := res.LastInsertId()
	return &Tenant{ID: id, Name: name, Slug: slug, Settings: "{}"}, nil
}

var slugFold = strings.NewReplacer("č", "c", "ć", "c", "š", "s", "ž", "z", "đ", "dj")
var slugJunk = regexp.MustCompile(`[^a-z0-9]+`)

// SlugFromName derives a firm's handle from its name: "Prevoz Đurić d.o.o."
// becomes "prevoz-djuric-d-o-o". Used when the operator doesn't pass one.
func SlugFromName(name string) string {
	s := slugFold.Replace(strings.ToLower(strings.TrimSpace(name)))
	return strings.Trim(slugJunk.ReplaceAllString(s, "-"), "-")
}
