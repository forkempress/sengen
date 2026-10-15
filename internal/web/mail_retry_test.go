package web

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"shengen/internal/mail"
	"shengen/internal/store"
)

// flakyMailer fails the first failures sends, then records the rest.
type flakyMailer struct {
	mail.Recorder
	mu       sync.Mutex
	failures int
	attempts int
}

func (m *flakyMailer) Send(ctx context.Context, msg mail.Message) error {
	m.mu.Lock()
	m.attempts++
	fail := m.attempts <= m.failures
	m.mu.Unlock()
	if fail {
		return errors.New("dial tcp: i/o timeout")
	}
	return m.Recorder.Send(ctx, msg)
}

func mailStatuses(t *testing.T, a *resetApp) []string {
	t.Helper()
	rows, err := a.st.DB.QueryContext(context.Background(), `SELECT status FROM email_log ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func TestMail_RetriedAfterFailure(t *testing.T) {
	a := newResetApp(t)
	a.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana", 30)
	m := &flakyMailer{failures: 2}
	a.srv.Mailer, a.srv.MailRetryDelays = m, []time.Duration{time.Millisecond, time.Millisecond}

	a.forgot("vlasnik@prva.rs")
	a.sent()
	if got := m.Sent(); len(got) != 1 || got[0].To != "vlasnik@prva.rs" {
		t.Fatalf("sent %+v, want the reset mail on the third attempt", got)
	}
	// Attempts that were retried are not failures.
	if s := mailStatuses(t, a); len(s) != 1 || s[0] != store.EmailSent {
		t.Errorf("email_log = %v, want [sent]", s)
	}
}

func TestMail_FailedAfterEveryAttempt(t *testing.T) {
	a := newResetApp(t)
	a.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana", 30)
	m := &flakyMailer{failures: 100}
	a.srv.Mailer, a.srv.MailRetryDelays = m, []time.Duration{time.Millisecond, time.Millisecond}

	a.forgot("vlasnik@prva.rs")
	a.sent()
	if m.attempts != 3 {
		t.Errorf("attempts = %d, want 3", m.attempts)
	}
	if s := mailStatuses(t, a); len(s) != 1 || s[0] != store.EmailFailed {
		t.Errorf("email_log = %v, want one failed row", s)
	}
}

func TestMail_ShutdownEndsRetryWait(t *testing.T) {
	a := newResetApp(t)
	a.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana", 30)
	a.srv.Mailer, a.srv.MailRetryDelays = &flakyMailer{failures: 100}, []time.Duration{time.Hour}

	a.forgot("vlasnik@prva.rs")
	a.srv.StopMailRetries()
	start := time.Now()
	a.sent()
	if d := time.Since(start); d > 4*time.Second {
		t.Fatalf("shutdown waited %s for a retry", d)
	}
	if s := mailStatuses(t, a); len(s) != 1 || s[0] != store.EmailFailed {
		t.Errorf("email_log = %v, want the lost message logged as failed", s)
	}
}
