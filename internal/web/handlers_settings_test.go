package web

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// The drivers page shows Telegram only to a firm that uses the bot, or
// asks for it.
func TestDriversPage_TelegramOnlyWhenInUse(t *testing.T) {
	app, a, _ := twoFirmApp(t)
	sa := app.mustLogin(a)
	tgOnly := []string{"<th>Telegram</th>", "Telegram link i QR", "Sa Telegram botom povezano"}

	body := sa.get("/podesavanja/vozaci").Body.String()
	for _, hidden := range tgOnly {
		if strings.Contains(body, hidden) {
			t.Errorf("firm without the bot sees %q", hidden)
		}
	}
	if !strings.Contains(body, `href="/podesavanja/vozaci?telegram=1"`) {
		t.Error("no way to show Telegram linking")
	}

	body = sa.get("/podesavanja/vozaci?telegram=1").Body.String()
	for _, want := range tgOnly[:2] {
		if !strings.Contains(body, want) {
			t.Errorf("?telegram=1 page is missing %q", want)
		}
	}

	if err := app.st.SetDriverTelegramChatID(context.Background(), a.tenant.ID, a.driver.ID, 9001); err != nil {
		t.Fatal(err)
	}
	body = sa.get("/podesavanja/vozaci").Body.String()
	for _, want := range []string{"<th>Telegram</th>", "Ponovo poveži Telegram", "Sa Telegram botom povezano"} {
		if !strings.Contains(body, want) {
			t.Errorf("firm with a linked driver is missing %q", want)
		}
	}
	if strings.Contains(body, "?telegram=1") {
		t.Error("firm with a linked driver still sees the 'show linking' hint")
	}
}

// Each active driver shows the days used and the same badge as Pregled;
// the rarer actions sit in the row's "⋯" menu.
func TestDriversPage_DaysAndRowMenu(t *testing.T) {
	app, a, _ := twoFirmApp(t)
	body := app.mustLogin(a).get("/podesavanja/vozaci").Body.String()
	for _, want := range []string{
		"<th>Dani</th>", "/90</span>", `class="badge `,
		`<details class="row-menu">`, `aria-label="Još akcija — ` + a.driver.Name + `"`,
		`href="/vozaci/` + id(a.driver.ID) + `/izvestaj"`, ">Arhiviraj</button>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("drivers page is missing %q", want)
		}
	}
}

// Archiving moves a driver to its own block at the bottom, and back.
func TestDriversPage_ArchiveAndRestore(t *testing.T) {
	app, a, _ := twoFirmApp(t)
	sa := app.mustLogin(a)
	base := "/podesavanja/vozaci/" + id(a.driver.ID)

	if w := sa.post(base+"/deaktiviraj", nil); w.Code != http.StatusSeeOther {
		t.Fatalf("archive = %d, want 303", w.Code)
	}
	body := sa.get("/podesavanja/vozaci").Body.String()
	for _, want := range []string{"Vozač je arhiviran.", `id="arhiva"`, "Vrati iz arhive", "Nema aktivnih vozača."} {
		if !strings.Contains(body, want) {
			t.Errorf("after archiving the page is missing %q", want)
		}
	}
	if strings.Contains(body, `aria-label="Još akcija — `+a.driver.Name+`"`) {
		t.Error("archived driver is still in the active table")
	}

	if w := sa.post(base+"/aktiviraj", nil); w.Code != http.StatusSeeOther {
		t.Fatalf("restore = %d, want 303", w.Code)
	}
	body = sa.get("/podesavanja/vozaci").Body.String()
	if !strings.Contains(body, "Vozač je vraćen iz arhive.") || strings.Contains(body, `id="arhiva"`) {
		t.Error("restored driver is not back in the active table")
	}
}
