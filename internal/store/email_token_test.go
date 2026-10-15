package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"shengen/internal/store"
)

func resetToken(tenantID, userID int64, email string) store.EmailToken {
	return store.EmailToken{Purpose: store.EmailTokenReset, Email: email, TenantID: &tenantID, UserID: &userID}
}

func TestEmailToken_ConsumeOnce(t *testing.T) {
	st, tenantID, _ := newTestStore(t)
	ctx := context.Background()
	u, err := st.CreateUser(ctx, tenantID, "ana@firma.rs", "x", "owner", "Ana")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateEmailToken(ctx, "tok-1", resetToken(tenantID, u.ID, " Ana@Firma.rs "), time.Hour); err != nil {
		t.Fatal(err)
	}

	peeked, err := st.PeekEmailToken(ctx, "tok-1", store.EmailTokenReset)
	if err != nil {
		t.Fatalf("peek: %v", err)
	}
	if peeked.Email != "ana@firma.rs" || *peeked.UserID != u.ID || *peeked.TenantID != tenantID {
		t.Errorf("peek = %+v", peeked)
	}
	// Peeking does not use the token up.
	got, err := st.ConsumeEmailToken(ctx, "tok-1", store.EmailTokenReset)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if *got.UserID != u.ID {
		t.Errorf("consumed user = %d", *got.UserID)
	}
	if _, err := st.ConsumeEmailToken(ctx, "tok-1", store.EmailTokenReset); !errors.Is(err, store.ErrTokenUsed) {
		t.Errorf("second consume: %v, want ErrTokenUsed", err)
	}
	if _, err := st.PeekEmailToken(ctx, "tok-1", store.EmailTokenReset); !errors.Is(err, store.ErrTokenUsed) {
		t.Errorf("peek after use: %v, want ErrTokenUsed", err)
	}
}

func TestEmailToken_ExpiredAndUnknownAndWrongPurpose(t *testing.T) {
	st, tenantID, _ := newTestStore(t)
	ctx := context.Background()
	u, _ := st.CreateUser(ctx, tenantID, "ana@firma.rs", "x", "owner", "Ana")
	if err := st.CreateEmailToken(ctx, "old", resetToken(tenantID, u.ID, "ana@firma.rs"), -time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ConsumeEmailToken(ctx, "old", store.EmailTokenReset); !errors.Is(err, store.ErrTokenExpired) {
		t.Errorf("expired: %v, want ErrTokenExpired", err)
	}
	if _, err := st.ConsumeEmailToken(ctx, "nema", store.EmailTokenReset); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown: %v, want ErrNotFound", err)
	}

	if err := st.CreateEmailToken(ctx, "r", resetToken(tenantID, u.ID, "ana@firma.rs"), time.Hour); err != nil {
		t.Fatal(err)
	}
	// A reset link cannot be redeemed as an invitation or a sign-up.
	if _, err := st.ConsumeEmailToken(ctx, "r", store.EmailTokenInvite); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("wrong purpose: %v, want ErrNotFound", err)
	}
	if _, err := st.ConsumeEmailToken(ctx, "r", store.EmailTokenReset); err != nil {
		t.Errorf("right purpose after a wrong try: %v", err)
	}
}

// Only the link in the latest email works.
func TestEmailToken_NewTokenVoidsOlder(t *testing.T) {
	st, tenantID, _ := newTestStore(t)
	ctx := context.Background()
	u, _ := st.CreateUser(ctx, tenantID, "ana@firma.rs", "x", "owner", "Ana")
	st.CreateEmailToken(ctx, "first", resetToken(tenantID, u.ID, "ana@firma.rs"), time.Hour)
	st.CreateEmailToken(ctx, "second", resetToken(tenantID, u.ID, "ana@firma.rs"), time.Hour)
	if _, err := st.ConsumeEmailToken(ctx, "first", store.EmailTokenReset); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("older link: %v, want ErrNotFound", err)
	}
	if _, err := st.ConsumeEmailToken(ctx, "second", store.EmailTokenReset); err != nil {
		t.Errorf("latest link: %v", err)
	}
}

func TestEmailToken_ConcurrentConsumeHasOneWinner(t *testing.T) {
	st, tenantID, _ := newTestStore(t)
	ctx := context.Background()
	u, _ := st.CreateUser(ctx, tenantID, "ana@firma.rs", "x", "owner", "Ana")
	st.CreateEmailToken(ctx, "race", resetToken(tenantID, u.ID, "ana@firma.rs"), time.Hour)

	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := st.ConsumeEmailToken(ctx, "race", store.EmailTokenReset); err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Errorf("%d requests redeemed the same token, want 1", wins)
	}
}

func TestEmailToken_CleanupAndLog(t *testing.T) {
	st, tenantID, _ := newTestStore(t)
	ctx := context.Background()
	u, _ := st.CreateUser(ctx, tenantID, "ana@firma.rs", "x", "owner", "Ana")
	st.CreateEmailToken(ctx, "old", resetToken(tenantID, u.ID, "ana@firma.rs"), -time.Hour)
	other := resetToken(tenantID, u.ID, "drugi@firma.rs")
	st.CreateEmailToken(ctx, "live", other, time.Hour)
	n, err := st.DeleteExpiredEmailTokens(ctx, time.Now())
	if err != nil || n != 1 {
		t.Errorf("deleted %d (%v), want 1", n, err)
	}
	if _, err := st.PeekEmailToken(ctx, "live", store.EmailTokenReset); err != nil {
		t.Errorf("live token was removed: %v", err)
	}

	for _, status := range []string{store.EmailSent, store.EmailSent, store.EmailFailed, store.EmailOverLimit} {
		if err := st.RecordEmail(ctx, "reset", &tenantID, "Ana@Firma.rs", status, ""); err != nil {
			t.Fatal(err)
		}
	}
	sent, err := st.CountEmailsSentSince(ctx, time.Now().Add(-time.Hour))
	if err != nil || sent != 2 {
		t.Errorf("sent = %d (%v), want 2: failed and refused messages do not count", sent, err)
	}
	if n, _ := st.DeleteEmailLogBefore(ctx, time.Now().Add(time.Minute)); n != 4 {
		t.Errorf("log cleanup removed %d, want 4", n)
	}
}
