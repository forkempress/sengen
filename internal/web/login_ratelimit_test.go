package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"shengen/internal/auth"
	"shengen/internal/telegram"
)

// loginFrom posts a login form as if it came through the given peer and
// headers.
func loginFrom(h http.Handler, remoteAddr string, headers map[string]string, email, password string) int {
	form := url.Values{"email": {email}, "password": {password}}
	r := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.RemoteAddr = remoteAddr
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code
}

func rateLimitApp(t *testing.T, proxy auth.TrustedProxy) (*testApp, http.Handler) {
	app := newTestApp(t)
	srv := New(app.st, auth.New(app.st, true), telegram.New(app.st, telegram.Config{}))
	srv.TrustedProxy = proxy
	return app, srv.Routes()
}

// SEC-05: one address trying one password against many accounts used to get
// 5 tries per account (key was "ip|email"), i.e. unlimited spraying.
func TestLoginRateLimit_PasswordSprayingFromOneIP(t *testing.T) {
	_, h := rateLimitApp(t, auth.ProxyNone)
	blocked := false
	for i := 0; i < 50; i++ {
		code := loginFrom(h, "203.0.113.7:4000", nil, fmt.Sprintf("zrtva%d@firma.rs", i), "Leto2026!")
		if code == http.StatusTooManyRequests {
			blocked = true
			if i < loginLimitPerIP {
				t.Errorf("blocked after %d attempts, want the IP limit of %d", i, loginLimitPerIP)
			}
			break
		}
	}
	if !blocked {
		t.Error("50 accounts sprayed from one IP without being limited")
	}
}

// SEC-05: many addresses guessing one account are limited per account.
func TestLoginRateLimit_DistributedGuessingOfOneAccount(t *testing.T) {
	app, h := rateLimitApp(t, auth.ProxyCaddy)
	a := app.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana", 30)
	for i := 0; i < loginLimitPerAccount; i++ {
		xff := map[string]string{"X-Forwarded-For": fmt.Sprintf("198.51.100.%d", i+1)}
		if code := loginFrom(h, "127.0.0.1:1", xff, a.email, "pogresna"); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status %d, want 401", i+1, code)
		}
	}
	xff := map[string]string{"X-Forwarded-For": "198.51.100.200"}
	if code := loginFrom(h, "127.0.0.1:1", xff, a.email, a.password); code != http.StatusTooManyRequests {
		t.Errorf("after %d failures from different IPs status = %d, want 429 even with the right password", loginLimitPerAccount, code)
	}
}

// SEC-06: behind Caddy a forged CF-Connecting-IP used to give every request a
// fresh bucket.
func TestLoginRateLimit_ForgedCFConnectingIPBehindCaddy(t *testing.T) {
	_, h := rateLimitApp(t, auth.ProxyCaddy)
	for i := 0; i < loginLimitPerIP+5; i++ {
		headers := map[string]string{
			"X-Forwarded-For":  "203.0.113.7",
			"CF-Connecting-IP": fmt.Sprintf("10.0.%d.%d", i/250, i%250+1),
		}
		code := loginFrom(h, "127.0.0.1:1", headers, fmt.Sprintf("u%d@firma.rs", i), "x")
		if code == http.StatusTooManyRequests {
			return
		}
	}
	t.Error("forged CF-Connecting-IP behind Caddy escaped the IP limit")
}

// A successful login clears the account's failures, so the owner is not
// locked out by their own typos once they get it right.
func TestLoginRateLimit_SuccessResetsAccount(t *testing.T) {
	app, h := rateLimitApp(t, auth.ProxyNone)
	a := app.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana", 30)
	for i := 0; i < loginLimitPerAccount-1; i++ {
		loginFrom(h, "203.0.113.7:1", nil, a.email, "pogresna")
	}
	if code := loginFrom(h, "203.0.113.7:1", nil, a.email, a.password); code != http.StatusSeeOther {
		t.Fatalf("correct password: status %d, want 303", code)
	}
	for i := 0; i < loginLimitPerAccount-1; i++ {
		if code := loginFrom(h, "203.0.113.7:1", nil, a.email, "pogresna"); code != http.StatusUnauthorized {
			t.Fatalf("after reset attempt %d: status %d, want 401", i+1, code)
		}
	}
}
