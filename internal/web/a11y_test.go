package web

import (
	"bytes"
	"encoding/csv"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"shengen/internal/importer"
)

// The example file must import cleanly with the firm's own drivers, or the
// template would teach the wrong format.
func TestSampleCSV_ImportsWithoutErrors(t *testing.T) {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	var buf bytes.Buffer
	csv.NewWriter(&buf).WriteAll(sampleCSV([]string{"Ana Anić", "Đorđe Ćosić"}, today))

	rows, err := importer.Parse(&buf, []importer.ExistingDriver{{ID: 1, Name: "Ana Anić"}, {ID: 2, Name: "Đorđe Ćosić"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(sampleRows) {
		t.Fatalf("parsed %d rows, want %d", len(rows), len(sampleRows))
	}
	for _, r := range rows {
		if len(r.Errors) > 0 {
			t.Errorf("sample row %d has errors: %v", r.LineNum, r.Errors)
		}
	}
}

func TestSampleCSV_Download(t *testing.T) {
	app, a, _ := twoFirmApp(t)
	w := app.mustLogin(a).get("/import/primer.csv")
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("download = %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	if !strings.Contains(w.Body.String(), "vozač,ulaz,izlaz,tip,napomena") {
		t.Error("sample has no header row")
	}
}

// Unknown pages answer in Serbian, inside the app's layout, with a way back.
func TestNotFound_IsBrandedAndInSerbian(t *testing.T) {
	app, a, b := twoFirmApp(t)
	sa := app.mustLogin(a)
	for _, path := range []string{"/nepostoji", "/vozaci/" + id(b.driver.ID)} {
		w := sa.get(path)
		body := w.Body.String()
		if w.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, w.Code)
		}
		if !strings.Contains(body, "Ova stranica ne postoji") || !strings.Contains(body, `href="/pregled"`) || strings.Contains(body, "page not found") {
			t.Errorf("GET %s is not the branded 404 page", path)
		}
	}
}

// Every label points at its field (UX-15).
func TestTemplates_EveryLabelHasFor(t *testing.T) {
	label := regexp.MustCompile(`<label(\s[^>]*)?>`)
	for name := range templateFS_files(t) {
		src, _ := templateFS.ReadFile("templates/" + name)
		for _, m := range label.FindAllString(string(src), -1) {
			if !strings.Contains(m, `for="`) {
				t.Errorf("%s: %s has no for", name, m)
			}
		}
	}
}

func templateFS_files(t *testing.T) map[string]bool {
	t.Helper()
	entries, err := templateFS.ReadDir("templates")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, e := range entries {
		out[e.Name()] = true
	}
	return out
}

// UX-25: password managers need the autocomplete hints, and the error sits
// next to the fields it is about (once, not also as a banner on top).
func TestLogin_AutocompleteAndErrorByTheField(t *testing.T) {
	app, _, _ := twoFirmApp(t)
	page := app.request(http.MethodGet, "/login", nil, nil).Body.String()
	for _, want := range []string{`autocomplete="email"`, `autocomplete="current-password"`, `href="/registracija"`, `href="/zaboravljena-lozinka"`, `aria-label="Prikaži lozinku"`, "Koliko dana vaši vozači još smeju u Šengenu."} {
		if !strings.Contains(page, want) {
			t.Errorf("login page is missing %q", want)
		}
	}

	_, w := app.login("nema@nikoga.rs", "pogresna-lozinka")
	body := w.Body.String()
	if !strings.Contains(body, `id="login-err"`) || !strings.Contains(body, `aria-describedby="login-err"`) {
		t.Error("login error is not tied to the fields")
	}
	if n := strings.Count(body, "Pogrešan email ili lozinka."); n != 1 {
		t.Errorf("error shown %d times, want once", n)
	}
}
