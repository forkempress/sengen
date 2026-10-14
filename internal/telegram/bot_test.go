package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"shengen/internal/clock"
	"shengen/internal/db"
	"shengen/internal/status"
	"shengen/internal/store"
	"shengen/internal/tenant"
)

// fakeTelegram stands in for api.telegram.org and records what the bot tried
// to send, so tests can assert on the reply the driver/admin would see.
type fakeTelegram struct {
	server *httptest.Server

	mu    sync.Mutex
	calls []fakeCall
}

type fakeCall struct {
	Path string
	Body map[string]any
}

func newFakeTelegram(t *testing.T) *fakeTelegram {
	t.Helper()
	f := &fakeTelegram{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		json.Unmarshal(raw, &body)

		f.mu.Lock()
		f.calls = append(f.calls, fakeCall{Path: r.URL.Path, Body: body})
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true,"result":{}}`)
	}))
	t.Cleanup(f.server.Close)
	t.Setenv("TELEGRAM_API_BASE_URL", f.server.URL)
	return f
}

func (f *fakeTelegram) texts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if s, ok := c.Body["text"].(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func (f *fakeTelegram) client() *Client { return NewClient("TEST-TOKEN") }

// --- role enforcement ---

// A linked Telegram account proves who someone is, not what they may do.
// `viewer` is deliberately passed as a struct literal: the DB CHECK
// constraint doesn't allow that role yet, but the gate must already hold so
// the role can be introduced (parity work) without handing out write access.
func TestHandleAdminMessage_RejectsNonAdminRole(t *testing.T) {
	fake := newFakeTelegram(t)
	// A real (empty) store, not nil: if the gate ever regresses, the command
	// runs for real and the test reports a wrong reply instead of panicking
	// on a nil store — a clean failure that also lets later tests run.
	bot, tn := newWebhookTestBot(t, "")

	viewer := &store.User{ID: 7, TenantID: tn.ID, Role: "viewer", Name: "Pregledač"}
	msg := &Message{Chat: Chat{ID: 555}, Text: "/pregled"}

	bot.handleAdminMessage(context.Background(), fake.client(), tn, store.TenantSettings{}, 555, viewer, msg)

	texts := fake.texts()
	if len(texts) != 1 {
		t.Fatalf("expected exactly one reply, got %d: %v", len(texts), texts)
	}
	if texts[0] != noAdminAccessMsg {
		t.Errorf("reply = %q, want the refusal message", texts[0])
	}
	if strings.Contains(texts[0], "Pregled") || strings.Contains(texts[0], "90") {
		t.Errorf("refusal leaked driver data: %q", texts[0])
	}
}

func TestHandleAdminCallback_RejectsNonAdminRole(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, tn := newWebhookTestBot(t, "")

	viewer := &store.User{ID: 7, TenantID: tn.ID, Role: "viewer"}
	cq := &CallbackQuery{ID: "cb1", Data: "import_confirm", Message: &Message{Chat: Chat{ID: 555}}}

	bot.handleAdminCallback(context.Background(), fake.client(), tn, store.TenantSettings{}, 555, viewer, cq)

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.calls) != 1 {
		t.Fatalf("expected exactly one API call, got %d", len(fake.calls))
	}
	call := fake.calls[0]
	if !strings.Contains(call.Path, "answerCallbackQuery") {
		t.Errorf("call = %s, want answerCallbackQuery", call.Path)
	}
	// Asserting on the text, not just the call shape: without the role gate
	// the request still answers the callback (with "Zahtev je istekao"), so a
	// shape-only check would pass against unguarded code.
	if got, _ := call.Body["text"].(string); got != noAdminAccessMsg {
		t.Errorf("callback answer = %q, want the refusal message %q", got, noAdminAccessMsg)
	}
}

// --- webhook secret ---

func newWebhookTestBot(t *testing.T, secret string) (*Bot, *store.Tenant) {
	t.Helper()

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	st := store.New(conn)
	ctx := context.Background()
	tn, err := st.CreateTenant(ctx, "Test Prevoz", "test.local")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if secret != "" {
		settings := store.TenantSettings{TelegramBotToken: "TEST-TOKEN", TelegramWebhookSecret: secret}
		if err := st.UpdateTenantSettings(ctx, tn.ID, settings); err != nil {
			t.Fatalf("update settings: %v", err)
		}
		tn, err = st.GetTenantByHost(ctx, "test.local")
		if err != nil {
			t.Fatalf("reload tenant: %v", err)
		}
	}
	return New(st), tn
}

func postWebhook(t *testing.T, bot *Bot, tn *store.Tenant, headerSecret string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"update_id":1,"message":{"message_id":1,"chat":{"id":999,"type":"private"},"date":1,"text":"zdravo"}}`
	r := httptest.NewRequest(http.MethodPost, "/telegram/webhook", strings.NewReader(body))
	if headerSecret != "" {
		r.Header.Set("X-Telegram-Bot-Api-Secret-Token", headerSecret)
	}
	if tn != nil {
		r = r.WithContext(tenant.WithTenant(r.Context(), tn))
	}
	w := httptest.NewRecorder()
	bot.HandleWebhook(w, r)
	return w
}

func TestHandleWebhook_SecretValidation(t *testing.T) {
	const secret = "pravi-webhook-secret-vrednost"

	t.Run("correct secret is accepted", func(t *testing.T) {
		newFakeTelegram(t)
		bot, tn := newWebhookTestBot(t, secret)
		if got := postWebhook(t, bot, tn, secret).Code; got != http.StatusOK {
			t.Errorf("status = %d, want 200", got)
		}
	})

	t.Run("wrong secret is rejected", func(t *testing.T) {
		newFakeTelegram(t)
		bot, tn := newWebhookTestBot(t, secret)
		for _, wrong := range []string{
			"pogresan-secret",
			secret + "x",            // correct prefix, longer
			secret[:len(secret)-1],  // correct prefix, shorter
			strings.ToUpper(secret), // same length, different bytes
		} {
			if got := postWebhook(t, bot, tn, wrong).Code; got != http.StatusForbidden {
				t.Errorf("status for %q = %d, want 403", wrong, got)
			}
		}
	})

	t.Run("missing secret header is rejected", func(t *testing.T) {
		newFakeTelegram(t)
		bot, tn := newWebhookTestBot(t, secret)
		if got := postWebhook(t, bot, tn, "").Code; got != http.StatusForbidden {
			t.Errorf("status = %d, want 403", got)
		}
	})

	// Without a configured bot the endpoint must not exist as far as the
	// outside world can tell, and must never accept an empty secret.
	t.Run("unconfigured tenant returns 404", func(t *testing.T) {
		newFakeTelegram(t)
		bot, tn := newWebhookTestBot(t, "")
		if got := postWebhook(t, bot, tn, "").Code; got != http.StatusNotFound {
			t.Errorf("status = %d, want 404", got)
		}
		if got := postWebhook(t, bot, tn, "bilo-sta").Code; got != http.StatusNotFound {
			t.Errorf("status with a guessed secret = %d, want 404", got)
		}
	})

	t.Run("request without tenant context returns 404", func(t *testing.T) {
		newFakeTelegram(t)
		bot, _ := newWebhookTestBot(t, secret)
		if got := postWebhook(t, bot, nil, secret).Code; got != http.StatusNotFound {
			t.Errorf("status = %d, want 404", got)
		}
	})
}

// --- overstay messaging ---

// A driver who has used all 90 days and presses "Izašao iz Srbije" must be
// told he may not stay — never handed a date that reads like permission.
func TestDriverExited_WithNoDaysLeftWarnsInsteadOfGivingADate(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, tn := newWebhookTestBot(t, "")
	ctx := context.Background()

	driver, err := bot.Store.CreateDriver(ctx, tn.ID, "Marko", "")
	if err != nil {
		t.Fatalf("create driver: %v", err)
	}
	today := clock.Today()
	first, last := today.AddDate(0, 0, -90), today.AddDate(0, 0, -1) // 90 days used
	if _, err := bot.Store.CreateStay(ctx, tn.ID, driver.ID, first, &last, "work", "manual", "", nil); err != nil {
		t.Fatalf("seed 90-day stay: %v", err)
	}

	bot.driverExited(ctx, fake.client(), tn, 555, driver)

	texts := fake.texts()
	if len(texts) != 1 {
		t.Fatalf("expected one reply, got %d: %v", len(texts), texts)
	}
	reply := texts[0]
	if strings.Contains(reply, "Možeš ostati najduže do") {
		t.Errorf("driver over the limit was told he may stay until a date: %q", reply)
	}
	if !strings.Contains(reply, "preko limita") {
		t.Errorf("reply does not warn about the limit: %q", reply)
	}
}

func TestFormatMyDays_OverLimit(t *testing.T) {
	deadline := clock.Today().AddDate(0, 0, -3)
	since := clock.Today().AddDate(0, 0, -95)
	text := formatMyDays(status.DriverStatus{
		UsedDays: 93, Remaining: 0, OverBy: 3, InSchengen: true,
		CurrentSince: &since, MaxContinuousUntil: &deadline,
	})
	if strings.Contains(text, "Može da ostane do") {
		t.Errorf("over-limit summary offers a 'may stay until' date:\n%s", text)
	}
	if !strings.Contains(text, "PREKORAČENO za 3 dana") {
		t.Errorf("over-limit summary does not state the overstay:\n%s", text)
	}
}
