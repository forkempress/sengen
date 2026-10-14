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
		if _, err := st.GetUserByEmail(ctx, typed); err != nil {
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

// Everyone signs in at the same address and the email alone picks the firm,
// so an address may belong to only one account across all firms.
func TestCreateUser_EmailIsUniqueAcrossFirms(t *testing.T) {
	st, tenantA, _ := newTestStore(t)
	ctx := context.Background()

	other, err := st.CreateTenant(ctx, "Druga Firma", "druga-firma")
	if err != nil {
		t.Fatalf("create second tenant: %v", err)
	}
	if _, err := st.CreateUser(ctx, tenantA, "ana@firma.rs", "hash", "owner", "Ana A"); err != nil {
		t.Fatal(err)
	}
	_, err = st.CreateUser(ctx, other.ID, "Ana@Firma.rs", "hash", "owner", "Ana B")
	var verr *store.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("same email in another firm = %v, want a ValidationError", err)
	}
}

// Login has no firm to scope by: the lookup must find the account in
// whichever firm it lives, and report that firm.
func TestGetUserByEmail_FindsTheAccountsFirm(t *testing.T) {
	st, tenantA, _ := newTestStore(t)
	ctx := context.Background()
	other, err := st.CreateTenant(ctx, "Druga Firma", "druga-firma")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser(ctx, tenantA, "ana@firma.rs", "hash", "owner", "Ana"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser(ctx, other.ID, "petar@druga.rs", "hash", "owner", "Petar"); err != nil {
		t.Fatal(err)
	}

	for email, wantTenant := range map[string]int64{"ana@firma.rs": tenantA, "PETAR@druga.rs": other.ID} {
		u, err := st.GetUserByEmail(ctx, email)
		if err != nil {
			t.Fatalf("GetUserByEmail(%s): %v", email, err)
		}
		if u.TenantID != wantTenant {
			t.Errorf("%s belongs to firm %d, want %d", email, u.TenantID, wantTenant)
		}
	}
	if _, err := st.GetUserByEmail(ctx, "nema@nikoga.rs"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown email = %v, want ErrNotFound", err)
	}
}

func TestListUsers_IsTenantScoped(t *testing.T) {
	st, tenantA, _ := newTestStore(t)
	ctx := context.Background()

	other, err := st.CreateTenant(ctx, "Druga Firma", "druga-firma")
	if err != nil {
		t.Fatalf("create second tenant: %v", err)
	}
	if _, err := st.CreateUser(ctx, tenantA, "ana@firma.rs", "hash", "owner", "Ana A"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser(ctx, other.ID, "marija@druga.rs", "hash", "owner", "Marija B"); err != nil {
		t.Fatal(err)
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
}
