package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"shengen/internal/auth"
)

// SEC-21: another site cannot submit the login form, so it cannot sign a
// visitor in to the attacker's account.
func TestLogin_CrossSiteFormIsRefused(t *testing.T) {
	app, a, _ := twoFirmApp(t)

	post := func(headers map[string]string) *httptest.ResponseRecorder {
		form := url.Values{"email": {a.email}, "password": {a.password}}
		r := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
		r.Host = "app.dani90.rs"
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		app.handler.ServeHTTP(w, r)
		return w
	}
	hasSession := func(w *httptest.ResponseRecorder) bool {
		for _, c := range w.Result().Cookies() {
			if c.Name == auth.SessionCookieName && c.Value != "" {
				return true
			}
		}
		return false
	}

	for name, h := range map[string]map[string]string{
		"Sec-Fetch-Site cross-site": {"Sec-Fetch-Site": "cross-site"},
		"Sec-Fetch-Site same-site":  {"Sec-Fetch-Site": "same-site"},
		"Origin of another site":    {"Origin": "https://napadac.example"},
	} {
		if w := post(h); w.Code != http.StatusForbidden || hasSession(w) {
			t.Errorf("%s: status %d, session %v; want 403 and no session", name, w.Code, hasSession(w))
		}
	}

	for name, h := range map[string]map[string]string{
		"Sec-Fetch-Site same-origin": {"Sec-Fetch-Site": "same-origin"},
		"Origin of the app":          {"Origin": "https://app.dani90.rs"},
		"no browser headers":         {},
	} {
		if w := post(h); w.Code != http.StatusSeeOther || !hasSession(w) {
			t.Errorf("%s: status %d, session %v; want a signed-in redirect", name, w.Code, hasSession(w))
		}
	}
}
