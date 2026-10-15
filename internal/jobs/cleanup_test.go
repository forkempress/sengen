package jobs

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"shengen/internal/db"
	"shengen/internal/store"
)

// SEC-17: the daily job deletes expired sessions and link tokens (Telegram
// and email) and old email log rows, also when no bot is configured.
func TestTick_CleansUpExpiredSessionsAndTokens(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	st := store.New(conn)
	ctx := context.Background()
	tn, _ := st.CreateTenant(ctx, "Firma", "firma")
	u, _ := st.CreateUser(ctx, tn.ID, "v@firma.rs", "h", "owner", "V")
	d, _ := st.CreateDriver(ctx, tn.ID, "Ana", "")

	st.CreateSession(ctx, "istekla", tn.ID, u.ID, "c", time.Now().Add(-time.Minute))
	st.CreateSession(ctx, "ziva", tn.ID, u.ID, "c", time.Now().Add(time.Hour))
	st.CreateDriverLinkToken(ctx, "stari", tn.ID, d.ID, -time.Minute)
	st.CreateEmailToken(ctx, "stari-mejl", store.EmailToken{Purpose: store.EmailTokenReset, Email: u.Email, TenantID: &tn.ID, UserID: &u.ID}, -time.Minute)
	st.RecordEmail(ctx, "reset", &tn.ID, u.Email, store.EmailSent, "")
	conn.Exec(`UPDATE email_log SET created_at = '2026-01-01T00:00:00.000Z'`)

	r := New(st, nil)
	r.tick(ctx)

	if _, err := st.GetSession(ctx, "istekla"); err != store.ErrNotFound {
		t.Errorf("expired session after the job: %v, want ErrNotFound", err)
	}
	if _, err := st.GetSession(ctx, "ziva"); err != nil {
		t.Errorf("live session after the job: %v", err)
	}
	if n, _ := st.DeleteExpiredLinkTokens(ctx, time.Now()); n != 0 {
		t.Errorf("job left %d expired link tokens", n)
	}
	if n, _ := st.DeleteExpiredEmailTokens(ctx, time.Now()); n != 0 {
		t.Errorf("job left %d expired email tokens", n)
	}
	if n, _ := st.DeleteEmailLogBefore(ctx, time.Now().Add(-emailLogRetention)); n != 0 {
		t.Errorf("job left %d email log rows older than %v", n, emailLogRetention)
	}

	// The next tick the same day does not run the cleanup again.
	st.CreateSession(ctx, "istekla2", tn.ID, u.ID, "c", time.Now().Add(-time.Minute))
	r.tick(ctx)
	if _, err := st.GetSession(ctx, "istekla2"); err != nil {
		t.Errorf("cleanup ran twice within a day: %v", err)
	}
}
