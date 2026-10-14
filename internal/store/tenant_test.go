package store_test

import (
	"context"
	"errors"
	"testing"

	"shengen/internal/store"
)

func TestSlugFromName(t *testing.T) {
	cases := map[string]string{
		"Prevoz Đurić d.o.o.":       "prevoz-djuric-d-o-o",
		"  Test Prevoz  ":           "test-prevoz",
		"ČAČAK Šped & Žica":         "cacak-sped-zica",
		"---":                       "",
		"Firma 2026":                "firma-2026",
		"Transport Ćosić / Beograd": "transport-cosic-beograd",
	}
	for name, want := range cases {
		if got := store.SlugFromName(name); got != want {
			t.Errorf("SlugFromName(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestCreateTenant_DuplicateSlugIsAValidationError(t *testing.T) {
	st, _, _ := newTestStore(t)
	_, err := st.CreateTenant(context.Background(), "Opet ista", "test.local")
	var verr *store.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("duplicate slug = %v, want a ValidationError", err)
	}
}

func TestGetTenant(t *testing.T) {
	st, tenantID, _ := newTestStore(t)
	ctx := context.Background()

	got, err := st.GetTenant(ctx, tenantID)
	if err != nil || got.Name != "Test Prevoz" {
		t.Fatalf("GetTenant = %+v, %v", got, err)
	}
	if _, err := st.GetTenant(ctx, tenantID+999); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown tenant = %v, want ErrNotFound", err)
	}
}

// One shared bot: a chat id must resolve to the right driver in whichever
// firm it is linked, and a Telegram account can't be split across drivers.
func TestTelegramChat_ResolvesAcrossFirmsAndIsExclusive(t *testing.T) {
	st, tenantA, driverA := newTestStore(t)
	ctx := context.Background()

	other, err := st.CreateTenant(ctx, "Druga Firma", "druga-firma")
	if err != nil {
		t.Fatal(err)
	}
	driverB, err := st.CreateDriver(ctx, other.ID, "Petar", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := st.SetDriverTelegramChatID(ctx, tenantA, driverA, 111); err != nil {
		t.Fatal(err)
	}
	if err := st.SetDriverTelegramChatID(ctx, other.ID, driverB.ID, 222); err != nil {
		t.Fatal(err)
	}

	for chat, want := range map[int64]struct {
		driver, tenant int64
	}{111: {driverA, tenantA}, 222: {driverB.ID, other.ID}} {
		d, err := st.GetDriverByTelegramChat(ctx, chat)
		if err != nil {
			t.Fatalf("chat %d: %v", chat, err)
		}
		if d.ID != want.driver || d.TenantID != want.tenant {
			t.Errorf("chat %d -> driver %d of firm %d, want driver %d of firm %d", chat, d.ID, d.TenantID, want.driver, want.tenant)
		}
	}

	// The same Telegram account can't also be linked to a driver in another firm.
	err = st.SetDriverTelegramChatID(ctx, other.ID, driverB.ID, 111)
	var verr *store.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("linking an already-linked chat = %v, want a ValidationError", err)
	}

	if _, err := st.GetDriverByTelegramChat(ctx, 999); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown chat = %v, want ErrNotFound", err)
	}
}
