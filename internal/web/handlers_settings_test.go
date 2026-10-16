package web

import (
	"context"
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
