package store_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"shengen/internal/store"
)

func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// dumpColumn returns every value of one text column, to look for a raw token.
func dumpColumn(t *testing.T, st *store.Store, query string) []string {
	t.Helper()
	rows, err := st.DB.QueryContext(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	return out
}

func newUser(t *testing.T, st *store.Store, tenantID int64) int64 {
	t.Helper()
	u, err := st.CreateUser(context.Background(), tenantID, "vlasnik@test.rs", "hash", "owner", "Vlasnik")
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

// SEC-17: a copy of the database must not contain usable session tokens.
func TestSession_StoredAsSHA256(t *testing.T) {
	st, tenantID, _ := newTestStore(t)
	ctx := context.Background()
	userID := newUser(t, st, tenantID)
	const token = "sirovi-token-sesije-iz-kolacica"

	if err := st.CreateSession(ctx, token, tenantID, userID, "csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	stored := dumpColumn(t, st, `SELECT token_hash FROM sessions`)
	if len(stored) != 1 || stored[0] != sha(token) {
		t.Errorf("sessions holds %v, want only the SHA-256 of the token", stored)
	}
	for _, v := range stored {
		if strings.Contains(v, token) {
			t.Error("raw session token is in the database")
		}
	}

	sess, err := st.GetSession(ctx, token)
	if err != nil || sess.UserID != userID || sess.Token != token {
		t.Fatalf("GetSession with the cookie value: %+v, %v", sess, err)
	}
	if _, err := st.GetSession(ctx, sha(token)); err == nil {
		t.Error("the stored hash itself must not work as a cookie")
	}
	if err := st.DeleteSession(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetSession(ctx, token); err != store.ErrNotFound {
		t.Errorf("after delete: %v, want ErrNotFound", err)
	}
}

// SEC-11 (remainder): link tokens are hashed the same way.
func TestLinkToken_StoredAsSHA256(t *testing.T) {
	st, tenantID, driverID := newTestStore(t)
	ctx := context.Background()
	const token = "sirovi-link-token"

	if err := st.CreateDriverLinkToken(ctx, token, tenantID, driverID, time.Hour); err != nil {
		t.Fatal(err)
	}
	stored := dumpColumn(t, st, `SELECT token_hash FROM telegram_link_tokens`)
	if len(stored) != 1 || stored[0] != sha(token) {
		t.Errorf("telegram_link_tokens holds %v, want only the SHA-256 of the token", stored)
	}
	if _, err := st.ConsumeLinkToken(ctx, sha(token)); err == nil {
		t.Error("the stored hash itself must not redeem the link")
	}
	lt, err := st.ConsumeLinkToken(ctx, token)
	if err != nil || lt.DriverID == nil || *lt.DriverID != driverID {
		t.Fatalf("ConsumeLinkToken: %+v, %v", lt, err)
	}
}

// SEC-17: the cleanup removes sessions past either expiry and expired link
// tokens, and keeps live ones.
func TestDeleteExpiredSessionsAndLinkTokens(t *testing.T) {
	st, tenantID, driverID := newTestStore(t)
	ctx := context.Background()
	userID := newUser(t, st, tenantID)
	now := time.Now()

	st.CreateSession(ctx, "ziva", tenantID, userID, "c", now.Add(time.Hour))
	st.CreateSession(ctx, "istekla", tenantID, userID, "c", now.Add(-time.Minute))
	st.CreateSession(ctx, "neaktivna", tenantID, userID, "c", now.Add(time.Hour))
	st.TouchSession(ctx, "neaktivna", now.Add(-25*time.Hour))

	n, err := st.DeleteExpiredSessions(ctx, now, now.Add(-24*time.Hour))
	if err != nil || n != 2 {
		t.Errorf("deleted %d sessions (err %v), want 2", n, err)
	}
	if _, err := st.GetSession(ctx, "ziva"); err != nil {
		t.Errorf("live session was deleted: %v", err)
	}

	st.CreateDriverLinkToken(ctx, "stari", tenantID, driverID, -time.Minute)
	if n, err := st.DeleteExpiredLinkTokens(ctx, now); err != nil || n != 1 {
		t.Errorf("deleted %d link tokens (err %v), want 1", n, err)
	}
	st.CreateDriverLinkToken(ctx, "novi", tenantID, driverID, time.Hour)
	if n, _ := st.DeleteExpiredLinkTokens(ctx, now); n != 0 {
		t.Errorf("a live link token was deleted")
	}
}
