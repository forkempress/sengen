package store_test

import (
	"context"
	"errors"
	"testing"

	"shengen/internal/store"
)

func TestCreateUser_NormalisesEmail(t *testing.T) {
	st, tenantID, _ := newTestStore(t)
	ctx := context.Background()

	u, err := st.CreateUser(ctx, tenantID, "  Dispecer@Firma.RS ", "hash", "dispatcher", "Dispečer")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.Email != "dispecer@firma.rs" {
		t.Errorf("stored email = %q, want dispecer@firma.rs", u.Email)
	}

	// Login lower-cases what is typed; any casing must find the account.
	for _, typed := range []string{"dispecer@firma.rs", "DISPECER@FIRMA.RS", " Dispecer@Firma.rs"} {
		if _, err := st.GetUserByEmail(ctx, tenantID, typed); err != nil {
			t.Errorf("GetUserByEmail(%q): %v", typed, err)
		}
	}
}

func TestCreateUser_DuplicateEmailIsAValidationError(t *testing.T) {
	st, tenantID, _ := newTestStore(t)
	ctx := context.Background()

	if _, err := st.CreateUser(ctx, tenantID, "ana@firma.rs", "hash", "dispatcher", "Ana"); err != nil {
		t.Fatalf("first CreateUser: %v", err)
	}
	_, err := st.CreateUser(ctx, tenantID, "ANA@firma.rs", "hash", "owner", "Druga Ana")
	var verr *store.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("duplicate email (different case) = %v, want a ValidationError", err)
	}
}

// Two companies on the same installation may each have an "ana@…" and must
// never see each other's users.
func TestListUsers_IsTenantScoped(t *testing.T) {
	st, tenantA, _ := newTestStore(t)
	ctx := context.Background()

	other, err := st.CreateTenant(ctx, "Druga Firma", "druga.local")
	if err != nil {
		t.Fatalf("create second tenant: %v", err)
	}
	if _, err := st.CreateUser(ctx, tenantA, "ana@firma.rs", "hash", "owner", "Ana A"); err != nil {
		t.Fatal(err)
	}
	// Same address in another tenant is a different account, not a duplicate.
	if _, err := st.CreateUser(ctx, other.ID, "ana@firma.rs", "hash", "owner", "Ana B"); err != nil {
		t.Fatalf("same email in another tenant: %v", err)
	}
	if _, err := st.CreateUser(ctx, tenantA, "petar@firma.rs", "hash", "dispatcher", "Petar"); err != nil {
		t.Fatal(err)
	}

	users, err := st.ListUsers(ctx, tenantA)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("tenant A sees %d users, want 2: %+v", len(users), users)
	}
	for _, u := range users {
		if u.TenantID != tenantA {
			t.Errorf("tenant A sees user %q of tenant %d", u.Name, u.TenantID)
		}
	}
	if users[0].Role != "owner" {
		t.Errorf("owners should be listed first, got %q (%s) first", users[0].Name, users[0].Role)
	}

	u, err := st.GetUserByEmail(ctx, other.ID, "ana@firma.rs")
	if err != nil || u.Name != "Ana B" {
		t.Errorf("tenant B's ana = %+v, %v; want Ana B", u, err)
	}
}
