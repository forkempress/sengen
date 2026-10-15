package web

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// SEC-17: a session unused for more than 24 hours no longer signs anyone in,
// although its 14-day expiry is still ahead.
func TestSession_IdleTimeout(t *testing.T) {
	app, a, _ := twoFirmApp(t)
	sa := app.mustLogin(a)

	if w := sa.get("/pregled"); w.Code != http.StatusOK {
		t.Fatalf("fresh session: status %d, want 200", w.Code)
	}

	// Last used 23 hours ago: still valid, and using it renews the clock.
	app.setLastSeen(sa, time.Now().Add(-23*time.Hour))
	if w := sa.get("/pregled"); w.Code != http.StatusOK {
		t.Fatalf("session idle 23h: status %d, want 200", w.Code)
	}

	app.setLastSeen(sa, time.Now().Add(-25*time.Hour))
	if w := sa.get("/pregled"); w.Code == http.StatusOK {
		t.Error("session idle 25h still opens /pregled")
	}
}

func (a *testApp) setLastSeen(s *session, at time.Time) {
	a.t.Helper()
	if err := a.st.TouchSession(context.Background(), s.cookie.Value, at); err != nil {
		a.t.Fatal(err)
	}
}

// "Prijava" on dani90.rs leads to /login: a browser still signed in goes
// straight to /pregled, one whose session has ended gets the form.
func TestLoginPage_RedirectsSignedIn(t *testing.T) {
	app, a, _ := twoFirmApp(t)

	if w := app.request(http.MethodGet, "/login", nil, nil); w.Code != http.StatusOK {
		t.Fatalf("anonymous /login: status %d, want 200", w.Code)
	}

	sa := app.mustLogin(a)
	w := sa.get("/login")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/pregled" {
		t.Fatalf("signed-in /login: status %d to %q, want 303 to /pregled", w.Code, w.Header().Get("Location"))
	}

	app.setLastSeen(sa, time.Now().Add(-25*time.Hour))
	if w := sa.get("/login"); w.Code != http.StatusOK {
		t.Errorf("idle session /login: status %d, want 200 (the form)", w.Code)
	}
}
