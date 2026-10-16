package web

import (
	"net/http"
	"net/url"
	"strconv"
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

	page := "/vozaci/" + strconv.FormatInt(a.driver.ID, 10)
	w := sa.post(page+"/boravci", stayForm())
	if loc := w.Header().Get("Location"); loc != page {
		t.Fatalf("redirect = %q, want %s with no message in the URL", loc, page)
	}
	if body := sa.get(page).Body.String(); !strings.Contains(body, "Boravak dodat.") {
		t.Fatal("first page after the form does not show the message")
	}
	if body := sa.get(page).Body.String(); strings.Contains(body, "Boravak dodat.") {
		t.Error("message shown again on the second page")
	}
}

// A message belongs to the session that caused it, not to another browser
// of the same firm.
func TestFlash_StaysWithItsSession(t *testing.T) {
	app, a, _ := twoFirmApp(t)
	s1 := app.mustLogin(a)
	s2 := app.mustLogin(a)

	page := "/vozaci/" + strconv.FormatInt(a.driver.ID, 10)
	s1.post(page+"/boravci", stayForm())
	if body := s2.get(page).Body.String(); strings.Contains(body, "Boravak dodat.") {
		t.Error("another session sees the message")
	}
	if body := s1.get(page).Body.String(); !strings.Contains(body, "Boravak dodat.") {
		t.Error("own session does not see the message")
	}
}

func stayForm() url.Values {
	return url.Values{"entry_date": {"2026-01-05"}, "exit_date": {"2026-01-10"}, "type": {"work"}}
}

// Adding a driver with Telegram in use answers with the page itself: the
// one-time link and its QR code are shown at once, and the token never goes
// into a URL.
func TestDriverCreate_ShowsLinkAndQRAtOnce(t *testing.T) {
	app, a, _ := twoFirmApp(t)
	sa := app.mustLogin(a)

	w := sa.post("/podesavanja/vozaci", url.Values{"name": {"Novi Vozač"}, "telegram": {"1"}})
	if w.Code != http.StatusOK || w.Header().Get("Location") != "" {
		t.Fatalf("create = %d %q, want 200 with the page", w.Code, w.Header().Get("Location"))
	}
	body := w.Body.String()
	for _, want := range []string{"Vozač dodat.", "Link za povezivanje — Novi Vozač", "/start "} {
		if !strings.Contains(body, want) {
			t.Errorf("page after adding a driver is missing %q", want)
		}
	}
}

// A firm with no driver on the bot gets no link: just the message, after a
// redirect.
func TestDriverCreate_WithoutTelegramRedirects(t *testing.T) {
	app, a, _ := twoFirmApp(t)
	sa := app.mustLogin(a)

	w := sa.post("/podesavanja/vozaci", url.Values{"name": {"Novi Vozač"}})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/podesavanja/vozaci" {
		t.Fatalf("create = %d %q, want 303 to the drivers page", w.Code, w.Header().Get("Location"))
	}
	body := sa.get("/podesavanja/vozaci").Body.String()
	if !strings.Contains(body, "Vozač dodat.") || !strings.Contains(body, "Novi Vozač") {
		t.Error("drivers page does not show the new driver and the message")
	}
	if strings.Contains(body, "Link za povezivanje") {
		t.Error("a link was issued for a firm that does not use the bot")
	}
}
