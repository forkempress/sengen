package web

import (
	"io"
	"net/http"
	"regexp"
	"testing"
)

var versionedAssetRe = regexp.MustCompile(`(?:href|src)="(/static/style\.css\?v=[0-9a-f]{10})"`)

// Cloudflare kept serving an old style.css under the same URL after a deploy.
// Pages must link the CSS with a content hash, and that URL may be cached
// for good while the plain one is revalidated.
func TestStaticAssets_VersionedURLAndCaching(t *testing.T) {
	app, _, _ := twoFirmApp(t)

	body, _ := io.ReadAll(app.request(http.MethodGet, "/login", nil, nil).Body)
	m := versionedAssetRe.FindSubmatch(body)
	if m == nil {
		t.Fatalf("login page does not link style.css with ?v=<hash>")
	}

	versioned := app.request(http.MethodGet, string(m[1]), nil, nil)
	if versioned.Code != http.StatusOK {
		t.Fatalf("GET %s = %d", m[1], versioned.Code)
	}
	if got := versioned.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("versioned Cache-Control = %q", got)
	}
	plain := app.request(http.MethodGet, "/static/style.css", nil, nil)
	if got := plain.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("plain Cache-Control = %q", got)
	}
}

// /favicon.ico used to fall through to the login redirect, so browsers showed
// no icon. It must lead to the SVG, and pages must link it.
func TestFavicon(t *testing.T) {
	app, _, _ := twoFirmApp(t)

	ico := app.request(http.MethodGet, "/favicon.ico", nil, nil)
	if ico.Code != http.StatusMovedPermanently || ico.Header().Get("Location") != "/static/favicon.svg" {
		t.Fatalf("GET /favicon.ico = %d -> %q", ico.Code, ico.Header().Get("Location"))
	}
	svg := app.request(http.MethodGet, "/static/favicon.svg", nil, nil)
	if svg.Code != http.StatusOK || svg.Header().Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("GET /static/favicon.svg = %d %q", svg.Code, svg.Header().Get("Content-Type"))
	}
	body, _ := io.ReadAll(app.request(http.MethodGet, "/login", nil, nil).Body)
	if !regexp.MustCompile(`rel="icon" href="/static/favicon\.svg\?v=[0-9a-f]{10}"`).Match(body) {
		t.Errorf("login page does not link favicon.svg")
	}
}
