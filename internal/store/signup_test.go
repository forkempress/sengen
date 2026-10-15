package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"shengen/internal/store"
)

func signupForm(firm string) store.SignupForm {
	return store.SignupForm{FirmName: firm, Name: "Ana", PasswordHash: "hash", TermsVersion: "v1", TermsAcceptedAt: time.Now()}
}

func TestSignup_CodeWorksOnlyInItsBrowser(t *testing.T) {
	st, _, _ := newTestStore(t)
	ctx := context.Background()
	if err := st.CreateSignup(ctx, "link", "123456", "browser-a", "Ana@Nova.rs", signupForm("Nova"), time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ConsumeSignupCode(ctx, "browser-b", "123456"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("code from another browser: %v, want ErrNotFound", err)
	}
	p, err := st.ConsumeSignupCode(ctx, "browser-a", "123456")
	if err != nil {
		t.Fatal(err)
	}
	if p.Email != "ana@nova.rs" || p.Form.FirmName != "Nova" || !p.FromBrowser {
		t.Errorf("pending = %+v", p)
	}
	// Code and link are one sign-up: using one uses both.
	if _, err := st.ConsumeSignupLink(ctx, "link", ""); !errors.Is(err, store.ErrTokenUsed) {
		t.Errorf("link after code: %v, want ErrTokenUsed", err)
	}
}

func TestSignup_WrongCodesVoidTheSignup(t *testing.T) {
	st, _, _ := newTestStore(t)
	ctx := context.Background()
	st.CreateSignup(ctx, "link", "123456", "b", "ana@nova.rs", signupForm("Nova"), time.Hour)
	for i := 1; i < store.MaxSignupCodeTries; i++ {
		if _, err := st.ConsumeSignupCode(ctx, "b", "000000"); !errors.Is(err, store.ErrWrongCode) {
			t.Fatalf("wrong code %d: %v", i, err)
		}
	}
	if _, err := st.ConsumeSignupCode(ctx, "b", "000000"); !errors.Is(err, store.ErrTokenExpired) {
		t.Errorf("last wrong code: %v, want ErrTokenExpired", err)
	}
	if _, err := st.ConsumeSignupCode(ctx, "b", "123456"); !errors.Is(err, store.ErrTokenExpired) {
		t.Errorf("right code after too many wrong ones: %v", err)
	}
	if _, err := st.ConsumeSignupLink(ctx, "link", "b"); err == nil {
		t.Error("link still works after the code was guessed at too often")
	}
}

func TestSignup_LinkKnowsItsBrowser(t *testing.T) {
	st, _, _ := newTestStore(t)
	ctx := context.Background()
	st.CreateSignup(ctx, "l1", "111111", "b1", "a@nova.rs", signupForm("A"), time.Hour)
	st.CreateSignup(ctx, "l2", "222222", "b2", "b@nova.rs", signupForm("B"), time.Hour)
	if p, err := st.ConsumeSignupLink(ctx, "l1", "drugi"); err != nil || p.FromBrowser {
		t.Errorf("link opened elsewhere: %+v %v", p, err)
	}
	if p, err := st.ConsumeSignupLink(ctx, "l2", "b2"); err != nil || !p.FromBrowser {
		t.Errorf("link opened in its browser: %+v %v", p, err)
	}
}

func TestSignup_NewSignupVoidsOlderAndExpiry(t *testing.T) {
	st, _, _ := newTestStore(t)
	ctx := context.Background()
	st.CreateSignup(ctx, "old", "111111", "b", "ana@nova.rs", signupForm("Nova"), time.Hour)
	st.CreateSignup(ctx, "new", "222222", "b", "ana@nova.rs", signupForm("Nova"), time.Hour)
	if _, err := st.ConsumeSignupLink(ctx, "old", "b"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("older link: %v", err)
	}
	if _, err := st.ConsumeSignupCode(ctx, "b", "111111"); !errors.Is(err, store.ErrWrongCode) {
		t.Errorf("older code: %v", err)
	}
	st.CreateSignup(ctx, "stale", "333333", "c", "bora@nova.rs", signupForm("X"), -time.Minute)
	if _, err := st.ConsumeSignupLink(ctx, "stale", "c"); !errors.Is(err, store.ErrTokenExpired) {
		t.Errorf("expired link: %v", err)
	}
	if _, err := st.ConsumeSignupCode(ctx, "c", "333333"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expired code: %v", err)
	}
}

func TestCreateFirmWithOwner(t *testing.T) {
	st, _, _ := newTestStore(t)
	ctx := context.Background()
	until := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	accepted := time.Date(2026, 10, 15, 9, 30, 0, 0, time.UTC)
	form := store.SignupForm{FirmName: "Prevoz Đurić", Name: "Ana", PasswordHash: "h", TermsVersion: "2026-10-15", TermsAcceptedAt: accepted}

	t1, u1, err := st.CreateFirmWithOwner(ctx, "Ana@Djuric.rs", form, until)
	if err != nil {
		t.Fatal(err)
	}
	if t1.Slug != "prevoz-djuric" || u1.Role != "owner" || u1.Email != "ana@djuric.rs" || u1.TenantID != t1.ID {
		t.Errorf("firm %+v user %+v", t1, u1)
	}
	ov, err := st.GetTenantOverview(ctx, t1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ov.Plan != store.PlanTrial || ov.TrialUntil == nil || ov.TrialUntil.Format("2006-01-02") != "2026-11-14" {
		t.Errorf("plan %s trial %v", ov.Plan, ov.TrialUntil)
	}
	ts, err := st.GetTermsStatus(ctx, t1.ID)
	if err != nil || ts.Version != "2026-10-15" || ts.AcceptedBy == nil || *ts.AcceptedBy != u1.ID || ts.AcceptedAt == nil || !ts.AcceptedAt.Equal(accepted) {
		t.Errorf("terms = %+v (%v)", ts, err)
	}

	// Same firm name: a free slug. Same email: refused, and no firm is left behind.
	t2, _, err := st.CreateFirmWithOwner(ctx, "bora@djuric.rs", form, until)
	if err != nil || t2.Slug != "prevoz-djuric-2" {
		t.Errorf("second firm with the same name: %+v %v", t2, err)
	}
	before, _ := st.ListAllTenants(ctx)
	if _, _, err := st.CreateFirmWithOwner(ctx, "ana@djuric.rs", form, until); !errors.Is(err, store.ErrEmailTaken) {
		t.Errorf("taken email: %v", err)
	}
	after, _ := st.ListAllTenants(ctx)
	if len(after) != len(before) {
		t.Error("a refused sign-up left a firm behind")
	}
}
