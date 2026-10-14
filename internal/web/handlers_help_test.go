package web

import (
	"bytes"
	"strings"
	"testing"

	"shengen/internal/status"
	"shengen/internal/store"
)

func renderHelp(t *testing.T, data helpData) string {
	t.Helper()
	var buf bytes.Buffer
	if err := loadTemplates()["pomoc.html"].ExecuteTemplate(&buf, "layout.html", data); err != nil {
		t.Fatalf("render pomoc.html: %v", err)
	}
	return buf.String()
}

// The colour bands must be explained with the tenant's own thresholds, or the
// help would contradict the dashboard the dispatcher is looking at.
func TestHelpPage_UsesTenantThresholds(t *testing.T) {
	html := renderHelp(t, helpData{
		baseData:   baseData{User: &store.User{Name: "Ana", Role: "dispatcher"}},
		Thresholds: status.Thresholds{Red: 12, Orange: 25},
	})

	for _, want := range []string{"manje od 12 dana", "od 12 do 25 dana", "više od 25 dana", "12–25"} {
		if !strings.Contains(html, want) {
			t.Errorf("help page does not contain %q", want)
		}
	}
	if strings.Contains(html, "manje od 15 dana") {
		t.Error("help page shows the default threshold instead of the tenant's")
	}
}

func TestHelpPage_SettingsLinkOnlyForOwner(t *testing.T) {
	owner := renderHelp(t, helpData{
		baseData: baseData{User: &store.User{Name: "V", Role: "owner"}}, Thresholds: status.DefaultThresholds, IsOwner: true,
	})
	dispatcher := renderHelp(t, helpData{
		baseData: baseData{User: &store.User{Name: "D", Role: "dispatcher"}}, Thresholds: status.DefaultThresholds,
	})

	const link = `href="/podesavanja/telegram">Podešavanja → Telegram`
	if !strings.Contains(owner, link) {
		t.Error("owner should get a link to change the thresholds")
	}
	if strings.Contains(dispatcher, link) {
		t.Error("dispatcher is linked to a page that returns 403 for them")
	}
}

func TestHelpPage_CoversTheEssentials(t *testing.T) {
	html := renderHelp(t, helpData{
		baseData: baseData{User: &store.User{Name: "D", Role: "dispatcher"}}, Thresholds: status.DefaultThresholds,
	})
	for _, want := range []string{
		"90 dana u bilo kom periodu od 180 dana",
		"Šengen, a ne EU",
		"privatna putovanja",
		"Stanje na dan",
		"Ništa se ne upisuje dok ne potvrdite",
		"Telegram link",
		"EES sistem na granici",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("help page is missing %q", want)
		}
	}
}
