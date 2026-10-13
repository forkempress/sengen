package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

func (s *Store) CreateUser(ctx context.Context, tenantID int64, email, passwordHash, role, name string) (*User, error) {
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO users (tenant_id, email, password_hash, role, name) VALUES (?, ?, ?, ?, ?)`,
		tenantID, email, passwordHash, role, name)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	id, _ := res.LastInsertId()
	return &User{ID: id, TenantID: tenantID, Email: email, PasswordHash: passwordHash, Role: role, Name: name}, nil
}
