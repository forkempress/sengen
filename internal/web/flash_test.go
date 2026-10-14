package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// SEC-16: text in ?ok= or ?err= is never shown as an app message, so a link
// cannot put a phishing notice on a real app page.
func TestFlash_URLTextIsNotShown(t *testing.T) {
	app, a, _ := twoFirmApp(t)
	sa := app.mustLogin(a)

	const bait = "Sesija istekla, pozovite +381 60 000 0000"
	for _, path := range []string{
		"/pregled", "/vozaci/" + id(a.driver.ID), "/podesavanja/vozaci",
		"/podesavanja/korisnici", "/podesavanja/telegram", "/import",
	} {
		q := "?ok=" + url.QueryEscape(bait) + "&err=" + url.QueryEscape(bait)
		w := sa.get(path + q)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", path, w.Code)
		}
		if strings.Contains(w.Body.String(), "pozovite +381") {
			t.Errorf("GET %s shows text from the URL", path)
		}
	}
}

// A message set by a form is shown on the next page, once.
func TestFlash_ShownOnceAfterRedirect(t *testing.T) {
	app, a, _ := twoFirmApp(t)
	sa := app.mustLogin(a)

	w := sa.post("/podesavanja/vozaci", url.Values{"name": {"Novi Vozač"}})
	if loc := w.Header().Get("Location"); loc != "/podesavanja/vozaci" {
		t.Fatalf("redirect = %q, want /podesavanja/vozaci with no message in the URL", loc)
	}
	if body := sa.get("/podesavanja/vozaci").Body.String(); !strings.Contains(body, "Vozač dodat.") {
		t.Fatal("first page after the form does not show the message")
	}
	if body := sa.get("/podesavanja/vozaci").Body.String(); strings.Contains(body, "Vozač dodat.") {
		t.Error("message shown again on the second page")
	}
}

// A message belongs to the session that caused it, not to another browser
// of the same firm.
func TestFlash_StaysWithItsSession(t *testing.T) {
	app, a, _ := twoFirmApp(t)
	s1 := app.mustLogin(a)
	s2 := app.mustLogin(a)

	s1.post("/podesavanja/vozaci", url.Values{"name": {"Novi Vozač"}})
	if body := s2.get("/podesavanja/vozaci").Body.String(); strings.Contains(body, "Vozač dodat.") {
		t.Error("another session sees the message")
	}
	if body := s1.get("/podesavanja/vozaci").Body.String(); !strings.Contains(body, "Vozač dodat.") {
		t.Error("own session does not see the message")
	}
}
