package web

import (
	"context"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"

	"shengen/internal/auth"
	"shengen/internal/store"
	"shengen/internal/telegram"
)

// fakeBotAPI records sendMessage calls; when fail is set it answers like
// Telegram does for a user who blocked the bot.
type fakeBotAPI struct {
	mu   sync.Mutex
	sent []map[string]any
	fail bool
}

func newAppWithBot(t *testing.T, api *fakeBotAPI) *testApp {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		json.Unmarshal(raw, &body)
		api.mu.Lock()
		fail := api.fail
		if strings.HasSuffix(r.URL.Path, "/sendMessage") && !fail {
			api.sent = append(api.sent, body)
		}
		api.mu.Unlock()
		if fail {
			io.WriteString(w, `{"ok":false,"description":"Forbidden: bot was blocked by the user"}`)
			return
		}
		io.WriteString(w, `{"ok":true,"result":{}}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("TELEGRAM_API_BASE_URL", srv.URL)

	app := newTestApp(t)
	bot := telegram.New(app.st, telegram.Config{Token: "TEST-TOKEN", WebhookSecret: "s"})
	app.handler = New(app.st, auth.New(app.st, true), bot).Routes()
	return app
}

func (a *testApp) linkUserTelegram(f firm, chatID int64) {
	a.t.Helper()
	u, err := a.st.GetUserByEmail(context.Background(), f.email)
	if err != nil {
		a.t.Fatal(err)
	}
	if err := a.st.SetUserTelegramChatID(context.Background(), f.tenant.ID, u.ID, chatID); err != nil {
		a.t.Fatal(err)
	}
}

func (a *testApp) alertChat(tenantID int64) int64 {
	a.t.Helper()
	tn, err := a.st.GetTenant(context.Background(), tenantID)
	if err != nil {
		a.t.Fatal(err)
	}
	s, err := tn.ParsedSettings()
	if err != nil {
		a.t.Fatal(err)
	}
	return s.TelegramAlertChatID
}

// flashFrom follows the redirect in w and returns the message the next page
// shows: "ok" for the green one, "err" for the red one. Messages live on the
// session, not in the URL (SEC-16).
func flashFrom(s *session, w *httptest.ResponseRecorder, kind string) string {
	s.app.t.Helper()
	loc := w.Header().Get("Location")
	if w.Code != http.StatusSeeOther || loc == "" {
		s.app.t.Fatalf("want a redirect, got %d", w.Code)
	}
	re := regexp.MustCompile(`<div class="flash flash-` + kind + `">([^<]*)</div>`)
	loc, _, _ = strings.Cut(loc, "#") // a browser never sends the fragment
	m := re.FindStringSubmatch(s.get(loc).Body.String())
	if m == nil {
		return ""
	}
	return html.UnescapeString(m[1])
}

func TestAlertsToMe_SendsAlertsToTheOwnersTelegram(t *testing.T) {
	api := &fakeBotAPI{}
	app := newAppWithBot(t, api)
	a := app.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana", 30)
	app.linkUserTelegram(a, 7001)
	sa := app.mustLogin(a)

	if body := sa.get("/podesavanja/telegram").Body.String(); !strings.Contains(body, "ne šalju nikome") ||
		!strings.Contains(body, "Šalji upozorenja na moj Telegram") {
		t.Fatal("before: page should say alerts go nowhere and offer the button")
	}

	w := sa.post("/podesavanja/telegram/upozorenja-na-moj", nil)

	if got := app.alertChat(a.tenant.ID); got != 7001 {
		t.Errorf("alert chat = %d, want the owner's chat 7001", got)
	}
	if ok := flashFrom(sa, w, "ok"); !strings.Contains(ok, "probnu poruku") {
		t.Errorf("confirmation = %q, want it to mention the probe message", ok)
	}
	api.mu.Lock()
	sent := api.sent
	api.mu.Unlock()
	if len(sent) != 1 || sent[0]["chat_id"] != float64(7001) || !strings.Contains(sent[0]["text"].(string), "Prva Firma") {
		t.Errorf("probe message = %v, want one message to chat 7001 naming the firm", sent)
	}

	body := sa.get("/podesavanja/telegram").Body.String()
	if !strings.Contains(body, "Upozorenja stižu na vaš Telegram") {
		t.Error("after: page should say alerts go to your Telegram")
	}
	if strings.Contains(body, "Šalji upozorenja na moj Telegram") {
		t.Error("after: button should disappear once alerts already go to you")
	}
}

// Before linking there is no chat to send to: nothing changes, and the owner
// is told what to do first.
func TestAlertsToMe_RequiresALinkedTelegram(t *testing.T) {
	app := newAppWithBot(t, &fakeBotAPI{})
	a := app.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana", 30)
	sa := app.mustLogin(a)

	if body := sa.get("/podesavanja/telegram").Body.String(); strings.Contains(body, `action="/podesavanja/telegram/upozorenja-na-moj"`) {
		t.Error("button shown although the owner hasn't linked Telegram")
	}

	w := sa.post("/podesavanja/telegram/upozorenja-na-moj", nil)

	if got := app.alertChat(a.tenant.ID); got != 0 {
		t.Errorf("alert chat = %d, want unchanged 0", got)
	}
	if err := flashFrom(sa, w, "err"); !strings.Contains(err, "Prvo povežite") {
		t.Errorf("error = %q, want instructions to link Telegram first", err)
	}
}

// The setting is saved even if Telegram refuses the probe (e.g. the owner
// blocked the bot), and the owner is told — without Telegram's raw error.
func TestAlertsToMe_ProbeFailureIsReportedNotHidden(t *testing.T) {
	api := &fakeBotAPI{fail: true}
	app := newAppWithBot(t, api)
	a := app.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana", 30)
	app.linkUserTelegram(a, 7001)

	sa := app.mustLogin(a)
	w := sa.post("/podesavanja/telegram/upozorenja-na-moj", nil)

	if got := app.alertChat(a.tenant.ID); got != 7001 {
		t.Errorf("alert chat = %d, want 7001 saved despite the failed probe", got)
	}
	ok := flashFrom(sa, w, "ok")
	if !strings.Contains(ok, "nije stigla") {
		t.Errorf("message = %q, want it to say the probe didn't arrive", ok)
	}
	if strings.Contains(ok, "Forbidden") || strings.Contains(ok, "TEST-TOKEN") {
		t.Errorf("message shows a raw Telegram error: %q", ok)
	}
}

func TestAlertsToMe_DispatcherIsRefused(t *testing.T) {
	app := newAppWithBot(t, &fakeBotAPI{})
	a := app.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana", 30)

	hash, _ := auth.HashPassword("lozinka-za-test")
	disp, err := app.st.CreateUser(context.Background(), a.tenant.ID, "dispecer@prva.rs", hash, "dispatcher", "Dispečer")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.st.SetUserTelegramChatID(context.Background(), a.tenant.ID, disp.ID, 7002); err != nil {
		t.Fatal(err)
	}
	sd := app.mustLogin(firm{email: "dispecer@prva.rs", password: "lozinka-za-test"})

	if w := sd.post("/podesavanja/telegram/upozorenja-na-moj", nil); w.Code != http.StatusForbidden {
		t.Errorf("dispatcher = %d, want 403", w.Code)
	}
	if got := app.alertChat(a.tenant.ID); got != 0 {
		t.Errorf("alert chat = %d after a dispatcher's attempt, want 0", got)
	}
}

// Pressing the button changes only the presser's own firm.
func TestAlertsToMe_TouchesOnlyOwnFirm(t *testing.T) {
	app := newAppWithBot(t, &fakeBotAPI{})
	a := app.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana", 30)
	b := app.addFirm("Druga Firma", "druga", "vlasnik@druga.rs", "Boris", 50)
	if err := app.st.UpdateTenantSettings(context.Background(), b.tenant.ID, store.TenantSettings{TelegramAlertChatID: -100555}); err != nil {
		t.Fatal(err)
	}
	app.linkUserTelegram(a, 7001)

	app.mustLogin(a).post("/podesavanja/telegram/upozorenja-na-moj", nil)

	if got := app.alertChat(b.tenant.ID); got != -100555 {
		t.Errorf("firm B's alert chat = %d, want its own -100555 untouched", got)
	}
}

// Thresholds and the alert destination are separate forms: saving one never
// changes the other.
func TestThresholds_DoNotTouchAlertChat(t *testing.T) {
	app := newAppWithBot(t, &fakeBotAPI{})
	a := app.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana", 30)
	if err := app.st.UpdateTenantSettings(context.Background(), a.tenant.ID, store.TenantSettings{TelegramAlertChatID: -100777}); err != nil {
		t.Fatal(err)
	}
	sa := app.mustLogin(a)

	sa.post("/podesavanja/telegram/pragovi", url.Values{"threshold_red": {"10"}, "threshold_orange": {"20"}})

	if got := app.alertChat(a.tenant.ID); got != -100777 {
		t.Errorf("alert chat = %d after saving thresholds, want -100777 kept", got)
	}
}

func TestAlertGroup_EmptyTurnsAlertsOff(t *testing.T) {
	app := newAppWithBot(t, &fakeBotAPI{})
	a := app.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana", 30)
	sa := app.mustLogin(a)

	sa.post("/podesavanja/telegram/grupa", url.Values{"alert_chat_id": {"-100123"}})
	if got := app.alertChat(a.tenant.ID); got != -100123 {
		t.Fatalf("alert chat = %d, want -100123", got)
	}
	sa.post("/podesavanja/telegram/grupa", url.Values{"alert_chat_id": {""}})
	if got := app.alertChat(a.tenant.ID); got != 0 {
		t.Errorf("alert chat = %d, want 0 after clearing", got)
	}
}

// While alerts go nowhere, the dashboard says so.
func TestPregled_BannerWhileAlertsHaveNoRecipient(t *testing.T) {
	app := newAppWithBot(t, &fakeBotAPI{})
	a := app.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana", 30)
	sa := app.mustLogin(a)
	if _, err := app.st.CreateDriver(context.Background(), a.tenant.ID, "Vozač", ""); err != nil {
		t.Fatal(err)
	}

	if body := sa.get("/pregled").Body.String(); !strings.Contains(body, "Upozorenja se ne šalju nikome") {
		t.Error("no banner while alerts go nowhere")
	}
	sa.post("/podesavanja/telegram/grupa", url.Values{"alert_chat_id": {"-100123"}})
	if body := sa.get("/pregled").Body.String(); strings.Contains(body, "Upozorenja se ne šalju nikome") {
		t.Error("banner still shown after alerts got a recipient")
	}
}
