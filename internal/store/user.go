package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type User struct {
	ID           int64
	TenantID     int64
	Email        string
	PasswordHash string
	Role         string
	Name         string
}

func (s *Store) GetUserByEmail(ctx context.Context, tenantID int64, email string) (*User, error) {
	email = NormalizeEmail(email)
	var u User
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, tenant_id, email, password_hash, role, name FROM users WHERE tenant_id = ? AND email = ?`,
		tenantID, email,
	).Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Role, &u.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user by email: %w", err)
	}
	return &u, nil
}

func (s *Store) GetUser(ctx context.Context, tenantID, id int64) (*User, error) {
	var u User
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, tenant_id, email, password_hash, role, name FROM users WHERE tenant_id = ? AND id = ?`,
		tenantID, id,
	).Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Role, &u.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return &u, nil
}

// NormalizeEmail is the single rule for comparing email addresses: login,
// seeding and the users page all go through it, so an address typed with
// capitals or stray spaces can never become an account nobody can log into.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func (s *Store) CreateUser(ctx context.Context, tenantID int64, email, passwordHash, role, name string) (*User, error) {
	email = NormalizeEmail(email)
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO users (tenant_id, email, password_hash, role, name) VALUES (?, ?, ?, ?, ?)`,
		tenantID, email, passwordHash, role, name)
	if isUniqueConstraintErr(err) {
		return nil, &ValidationError{Msg: "korisnik sa tim emailom već postoji"}
	}
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	id, _ := res.LastInsertId()
	return &User{ID: id, TenantID: tenantID, Email: email, PasswordHash: passwordHash, Role: role, Name: name}, nil
}

// ListUsers returns the tenant's web users, owners first, then by name.
func (s *Store) ListUsers(ctx context.Context, tenantID int64) ([]User, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, tenant_id, email, password_hash, role, name FROM users
		 WHERE tenant_id = ? ORDER BY role = 'owner' DESC, name`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Role, &u.Name); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
