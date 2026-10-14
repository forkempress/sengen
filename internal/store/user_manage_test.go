package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"shengen/internal/store"
)

func newUserTestStore(t *testing.T) (*store.Store, *store.Tenant, *store.User, *store.User) {
	t.Helper()
	st, tenantID, _ := newTestStore(t)
	ctx := context.Background()
	tn, err := st.GetTenant(ctx, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := st.CreateUser(ctx, tn.ID, "vlasnik@firma.rs", "hash", "owner", "Vlasnik")
	if err != nil {
		t.Fatal(err)
	}
	disp, err := st.CreateUser(ctx, tn.ID, "dispecer@firma.rs", "hash", "dispatcher", "Dispečer")
	if err != nil {
		t.Fatal(err)
	}
	return st, tn, owner, disp
}

// The firm always keeps an active owner, whether the last one would be
// deactivated or made a dispatcher.
func TestUser_LastActiveOwnerIsKept(t *testing.T) {
	st, tn, owner, disp := newUserTestStore(t)
	ctx := context.Background()

	if _, err := st.SetUserActive(ctx, tn.ID, owner.ID, false); !errors.Is(err, store.ErrLastOwner) {
		t.Errorf("deactivating the only owner: err = %v, want ErrLastOwner", err)
	}
	if _, err := st.SetUserRole(ctx, tn.ID, owner.ID, "dispatcher"); !errors.Is(err, store.ErrLastOwner) {
		t.Errorf("demoting the only owner: err = %v, want ErrLastOwner", err)
	}

	// With a second active owner both are allowed.
	if _, err := st.SetUserRole(ctx, tn.ID, disp.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetUserRole(ctx, tn.ID, owner.ID, "dispatcher"); err != nil {
		t.Errorf("demoting one of two owners: %v", err)
	}
	// disp is now the only owner again.
	if _, err := st.SetUserActive(ctx, tn.ID, disp.ID, false); !errors.Is(err, store.ErrLastOwner) {
		t.Errorf("deactivating the remaining owner: err = %v, want ErrLastOwner", err)
	}

	// An inactive owner does not count.
	if _, err := st.SetUserRole(ctx, tn.ID, owner.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetUserActive(ctx, tn.ID, owner.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetUserRole(ctx, tn.ID, disp.ID, "dispatcher"); !errors.Is(err, store.ErrLastOwner) {
		t.Errorf("demoting the only active owner: err = %v, want ErrLastOwner", err)
	}
}

// Deactivation and a password change end every session, unlink Telegram,
// drop unused link tokens and stop alerts going to that chat.
func TestUser_RevokeAccess(t *testing.T) {
	for _, tc := range []struct {
		name   string
		revoke func(st *store.Store, tenantID, userID int64) error
	}{
		{"deactivate", func(st *store.Store, tenantID, userID int64) error {
			_, err := st.SetUserActive(context.Background(), tenantID, userID, false)
			return err
		}},
		{"password change", func(st *store.Store, tenantID, userID int64) error {
			return st.SetUserPassword(context.Background(), tenantID, userID, "novi-hash")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, tn, owner, disp := newUserTestStore(t)
			ctx := context.Background()
			exp := time.Now().Add(time.Hour)
			for _, tok := range []string{"s1", "s2"} {
				if err := st.CreateSession(ctx, tok, tn.ID, disp.ID, "c", exp); err != nil {
					t.Fatal(err)
				}
			}
			if err := st.CreateSession(ctx, "owner-s", tn.ID, owner.ID, "c", exp); err != nil {
				t.Fatal(err)
			}
			if err := st.SetUserTelegramChatID(ctx, tn.ID, disp.ID, 5005); err != nil {
				t.Fatal(err)
			}
			if err := st.CreateUserLinkToken(ctx, "link", tn.ID, disp.ID, time.Hour); err != nil {
				t.Fatal(err)
			}
			if err := st.UpdateTenantSettings(ctx, tn.ID, store.TenantSettings{TelegramAlertChatID: 5005}); err != nil {
				t.Fatal(err)
			}

			if err := tc.revoke(st, tn.ID, disp.ID); err != nil {
				t.Fatal(err)
			}

			for _, tok := range []string{"s1", "s2"} {
				if _, err := st.GetSession(ctx, tok); !errors.Is(err, store.ErrNotFound) {
					t.Errorf("session %s survived: %v", tok, err)
				}
			}
			if _, err := st.GetSession(ctx, "owner-s"); err != nil {
				t.Errorf("another user's session was deleted: %v", err)
			}
			u, err := st.GetUser(ctx, tn.ID, disp.ID)
			if err != nil {
				t.Fatal(err)
			}
			if u.TelegramChatID != nil {
				t.Errorf("telegram_chat_id = %d, want NULL", *u.TelegramChatID)
			}
			if _, err := st.ConsumeLinkToken(ctx, "link"); !errors.Is(err, store.ErrNotFound) {
				t.Errorf("unused link token still redeemable: %v", err)
			}
			got, _ := st.GetTenant(ctx, tn.ID)
			if s, _ := got.ParsedSettings(); s.TelegramAlertChatID != 0 {
				t.Errorf("alert chat = %d, want 0", s.TelegramAlertChatID)
			}
		})
	}
}

// Every change is scoped by firm: another firm's user id is "not found".
func TestUser_ChangesAreScopedByFirm(t *testing.T) {
	st, tn, _, disp := newUserTestStore(t)
	ctx := context.Background()
	other, err := st.CreateTenant(ctx, "Druga", "druga")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetUserActive(ctx, other.ID, disp.ID, false); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("SetUserActive from another firm: %v, want ErrNotFound", err)
	}
	if _, err := st.SetUserRole(ctx, other.ID, disp.ID, "owner"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("SetUserRole from another firm: %v, want ErrNotFound", err)
	}
	if err := st.SetUserPassword(ctx, other.ID, disp.ID, "x"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("SetUserPassword from another firm: %v, want ErrNotFound", err)
	}
	u, _ := st.GetUser(ctx, tn.ID, disp.ID)
	if !u.Active || u.Role != "dispatcher" || u.PasswordHash != "hash" {
		t.Errorf("user changed: %+v", u)
	}
}
