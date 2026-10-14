package web

import (
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
)

// SEC-03: every response carries the security headers, signed in or not,
// pages and static files alike.
func TestSecurityHeaders_OnEveryResponse(t *testing.T) {
	app, a, _ := twoFirmApp(t)
	sa := app.mustLogin(a)

	responses := map[string]http.Header{
		"GET /login":             app.request(http.MethodGet, "/login", nil, nil).Header(),
		"GET /pregled (anon)":    app.request(http.MethodGet, "/pregled", nil, nil).Header(),
		"GET /pregled":           sa.get("/pregled").Header(),
		"GET /static/style.css":  app.request(http.MethodGet, "/static/style.css", nil, nil).Header(),
		"POST /telegram/webhook": app.request(http.MethodPost, "/telegram/webhook", nil, nil).Header(),
	}
	want := map[string]string{
		"Content-Security-Policy":   "frame-ancestors 'none'",
		"X-Frame-Options":           "DENY",
		"Strict-Transport-Security": "max-age=31536000",
		"X-Content-Type-Options":    "nosniff",
		"Referrer-Policy":           "same-origin",
	}
	for name, h := range responses {
		for header, sub := range want {
			if !strings.Contains(h.Get(header), sub) {
				t.Errorf("%s: %s = %q, want it to contain %q", name, header, h.Get(header), sub)
			}
		}
	}
}

var externalRefRe = regexp.MustCompile(`(?i)<(?:link|script|img)[^>]+(?:href|src)="(https?:)?//`)

// SEC-19, LEG-07: loading the app must not reach Google Fonts, unpkg or any
// other third party — fonts and htmx are served from /static.
func TestPages_LoadNothingFromThirdParties(t *testing.T) {
	app, a, _ := twoFirmApp(t)
	sa := app.mustLogin(a)

	pages := map[string]string{
		"/login":   app.request(http.MethodGet, "/login", nil, nil).Body.String(),
		"/pregled": sa.get("/pregled").Body.String(),
	}
	for path, body := range pages {
		if m := externalRefRe.FindString(body); m != "" {
			t.Errorf("%s loads from another domain: %s", path, m)
		}
		for _, local := range []string{`/static/fonts.css`, `/static/htmx-1.9.12.min.js`} {
			if !strings.Contains(body, local) {
				t.Errorf("%s does not load %s", path, local)
			}
		}
	}

	// Everything the layout and fonts.css point at must actually be served.
	css := app.request(http.MethodGet, "/static/fonts.css", nil, nil).Body.String()
	urls := regexp.MustCompile(`url\('([^']+)'\)`).FindAllStringSubmatch(css, -1)
	if len(urls) == 0 {
		t.Fatal("fonts.css declares no font files")
	}
	assets := []string{"/static/htmx-1.9.12.min.js", "/static/app.js", "/static/style.css"}
	for _, u := range urls {
		assets = append(assets, u[1])
	}
	for _, path := range assets {
		if w := app.request(http.MethodGet, path, nil, nil); w.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, w.Code)
		}
	}
}

// The policy forbids inline handlers, so templates must not use them.
func TestTemplates_NoInlineScript(t *testing.T) {
	inline := regexp.MustCompile(`(?i)\son[a-z]+="|<script>|<style`)
	entries, err := templateFS.ReadDir("templates")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, _ := templateFS.ReadFile("templates/" + e.Name())
		if m := inline.FindString(string(b)); m != "" {
			t.Errorf("%s uses inline script/style %q, which the CSP blocks", e.Name(), m)
		}
	}
}

// SEC-20: the marketing site sends HSTS too.
func TestSiteHeaders_HaveHSTS(t *testing.T) {
	b, err := os.ReadFile("../../site/_headers")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "Strict-Transport-Security: max-age=31536000") {
		t.Error("site/_headers has no Strict-Transport-Security")
	}
}
