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
	"sync/atomic"
	"testing"
	"time"

	"shengen/internal/clock"
	"shengen/internal/db"
	"shengen/internal/status"
	"shengen/internal/store"
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
	bot, tn := newTestBot(t)

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
	bot, tn := newTestBot(t)

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

// --- shared bot test setup ---

const testSecret = "pravi-webhook-secret-vrednost"

// newTestDB opens a fresh database with migrations and one firm.
func newTestDB(t *testing.T) (*store.Store, *store.Tenant) {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	st := store.New(conn)
	tn, err := st.CreateTenant(context.Background(), "Test Prevoz", "test-prevoz")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	return st, tn
}

// newTestBot builds the shared bot over a fresh database with one firm.
// Call newFakeTelegram first: the bot's client reads the API address when it
// is constructed.
//
// The clock is pinned to noon of the current Belgrade date, so button tests
// don't hit the midnight date question when the suite runs late at night.
func newTestBot(t *testing.T) (*Bot, *store.Tenant) {
	t.Helper()
	d := clock.Today()
	noon := time.Date(d.Year(), d.Month(), d.Day(), 12, 0, 0, 0, clock.Now().Location())
	t.Cleanup(clock.Set(func() time.Time { return noon }))
	st, tn := newTestDB(t)
	return New(st, Config{Token: "TEST-TOKEN", WebhookSecret: testSecret}), tn
}

func postWebhook(t *testing.T, bot *Bot, headerSecret, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/telegram/webhook", strings.NewReader(body))
	if headerSecret != "" {
		r.Header.Set("X-Telegram-Bot-Api-Secret-Token", headerSecret)
	}
	w := httptest.NewRecorder()
	bot.HandleWebhook(w, r)
	if err := bot.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	return w
}

// nextUpdateID gives each test update its own update_id, as Telegram does;
// a repeated id is a redelivery and is skipped.
var lastUpdateID atomic.Int64

func nextUpdateID() int64 { return lastUpdateID.Add(1) }

func textUpdate(chatID int64, text string) string {
	b, _ := json.Marshal(Update{UpdateID: nextUpdateID(), Message: &Message{MessageID: 1, From: &TgUser{ID: chatID}, Chat: Chat{ID: chatID, Type: "private"}, Text: text}})
	return string(b)
}

// --- webhook secret ---

func TestHandleWebhook_SecretValidation(t *testing.T) {
	body := textUpdate(999, "zdravo")

	t.Run("correct secret is accepted", func(t *testing.T) {
		newFakeTelegram(t)
		bot, _ := newTestBot(t)
		if got := postWebhook(t, bot, testSecret, body).Code; got != http.StatusOK {
			t.Errorf("status = %d, want 200", got)
		}
	})

	t.Run("wrong secret is rejected", func(t *testing.T) {
		newFakeTelegram(t)
		bot, _ := newTestBot(t)
		for _, wrong := range []string{
			"pogresan-secret",
			testSecret + "x",               // correct prefix, longer
			testSecret[:len(testSecret)-1], // correct prefix, shorter
			strings.ToUpper(testSecret),    // same length, different bytes
		} {
			if got := postWebhook(t, bot, wrong, body).Code; got != http.StatusForbidden {
				t.Errorf("status for %q = %d, want 403", wrong, got)
			}
		}
	})

	t.Run("missing secret header is rejected", func(t *testing.T) {
		newFakeTelegram(t)
		bot, _ := newTestBot(t)
		if got := postWebhook(t, bot, "", body).Code; got != http.StatusForbidden {
			t.Errorf("status = %d, want 403", got)
		}
	})

	// Without a configured bot the endpoint must not exist as far as the
	// outside world can tell, and must never accept an empty secret.
	t.Run("unconfigured bot returns 404", func(t *testing.T) {
		newFakeTelegram(t)
		st, _ := newTestDB(t)
		for _, cfg := range []Config{{}, {Token: "TEST-TOKEN"}} {
			bot := New(st, cfg)
			for _, secret := range []string{"", "bilo-sta"} {
				if got := postWebhook(t, bot, secret, body).Code; got != http.StatusNotFound {
					t.Errorf("config %+v, secret %q: status = %d, want 404", cfg, secret, got)
				}
			}
		}
	})
}

// --- one shared bot, many firms ---

// twoFirms sets up firms A and B, each with a driver linked to Telegram and
// a different history, so a reply can be traced back to the firm it came from.
func twoFirms(t *testing.T) (bot *Bot, a, b *store.Tenant, driverA, driverB *store.Driver) {
	t.Helper()
	bot, a = newTestBot(t)
	ctx := context.Background()
	var err error
	if b, err = bot.Store.CreateTenant(ctx, "Druga Firma", "druga-firma"); err != nil {
		t.Fatal(err)
	}
	if driverA, err = bot.Store.CreateDriver(ctx, a.ID, "Ana Prva", ""); err != nil {
		t.Fatal(err)
	}
	if driverB, err = bot.Store.CreateDriver(ctx, b.ID, "Boris Drugi", ""); err != nil {
		t.Fatal(err)
	}
	if err := bot.Store.SetDriverTelegramChatID(ctx, a.ID, driverA.ID, 111); err != nil {
		t.Fatal(err)
	}
	if err := bot.Store.SetDriverTelegramChatID(ctx, b.ID, driverB.ID, 222); err != nil {
		t.Fatal(err)
	}
	// A: 5 days used. B: 20 days used.
	today := clock.Today()
	stay := func(tenantID, driverID int64, from, to int) {
		entry, exit := today.AddDate(0, 0, from), today.AddDate(0, 0, to)
		if _, err := bot.Store.CreateStay(ctx, tenantID, driverID, entry, &exit, "work", "manual", "", nil); err != nil {
			t.Fatal(err)
		}
	}
	stay(a.ID, driverA.ID, -30, -26)
	stay(b.ID, driverB.ID, -40, -21)
	return bot, a, b, driverA, driverB
}

// The chat that sends the message decides the firm: each driver must get
// his own firm's numbers, through the real webhook path.
func TestWebhook_DriverGetsHisOwnFirmsNumbers(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, _, _, _, _ := twoFirms(t)

	postWebhook(t, bot, testSecret, textUpdate(111, BtnMyDays))
	postWebhook(t, bot, testSecret, textUpdate(222, BtnMyDays))

	texts := fake.texts()
	if len(texts) != 2 {
		t.Fatalf("expected two replies, got %d: %v", len(texts), texts)
	}
	if !strings.Contains(texts[0], "Iskorišćeno: 5/90") {
		t.Errorf("driver of firm A got %q, want 5/90", texts[0])
	}
	if !strings.Contains(texts[1], "Iskorišćeno: 20/90") {
		t.Errorf("driver of firm B got %q, want 20/90", texts[1])
	}
}

// An owner's /pregled must list only his own firm's drivers.
func TestWebhook_AdminSeesOnlyHisFirm(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, a, _, _, _ := twoFirms(t)
	ctx := context.Background()

	owner, err := bot.Store.CreateUser(ctx, a.ID, "vlasnik@a.rs", "hash", "owner", "Vlasnik A")
	if err != nil {
		t.Fatal(err)
	}
	if err := bot.Store.SetUserTelegramChatID(ctx, a.ID, owner.ID, 333); err != nil {
		t.Fatal(err)
	}

	postWebhook(t, bot, testSecret, textUpdate(333, "/pregled"))

	texts := fake.texts()
	if len(texts) != 1 {
		t.Fatalf("expected one reply, got %v", texts)
	}
	if !strings.Contains(texts[0], "Ana Prva") {
		t.Errorf("owner of A doesn't see his own driver: %q", texts[0])
	}
	if strings.Contains(texts[0], "Boris Drugi") {
		t.Errorf("owner of A sees a driver of firm B: %q", texts[0])
	}
}

// For a chat nobody has linked yet, the one-time token is the only thing that
// names the firm — it was issued inside that firm.
func TestStart_LinkTokenDecidesTheFirm(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, _, b, _, _ := twoFirms(t)
	ctx := context.Background()

	newDriver, err := bot.Store.CreateDriver(ctx, b.ID, "Novi Vozač", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := bot.Store.CreateDriverLinkToken(ctx, "tokB", b.ID, newDriver.ID, time.Hour); err != nil {
		t.Fatal(err)
	}

	postWebhook(t, bot, testSecret, textUpdate(444, "/start tokB"))

	linked, err := bot.Store.GetDriverByTelegramChat(ctx, 444)
	if err != nil {
		t.Fatalf("chat 444 not linked: %v (replies: %v)", err, fake.texts())
	}
	if linked.ID != newDriver.ID || linked.TenantID != b.ID {
		t.Errorf("chat 444 linked to driver %d of firm %d, want driver %d of firm %d", linked.ID, linked.TenantID, newDriver.ID, b.ID)
	}
}

// A Telegram account already linked to one driver can't be quietly moved to
// another (possibly in another firm); the driver is told why.
func TestStart_AlreadyLinkedChatIsRefusedWithAReason(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, _, b, _, driverB := twoFirms(t)
	ctx := context.Background()

	other, err := bot.Store.CreateDriver(ctx, b.ID, "Treći", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := bot.Store.CreateDriverLinkToken(ctx, "tok3", b.ID, other.ID, time.Hour); err != nil {
		t.Fatal(err)
	}

	postWebhook(t, bot, testSecret, textUpdate(111, "/start tok3")) // 111 belongs to firm A's driver

	if texts := fake.texts(); len(texts) != 1 || !strings.Contains(texts[0], "već povezan") {
		t.Errorf("reply = %v, want an explanation that the account is already linked", texts)
	}
	if d, _ := bot.Store.GetDriverByTelegramChat(ctx, 111); d == nil || d.ID == other.ID || d.ID == driverB.ID {
		t.Errorf("chat 111 was moved to another driver: %+v", d)
	}
}

func TestWebhook_UnknownChatIsToldToContactDispatcher(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, _, _, _, _ := twoFirms(t)

	postWebhook(t, bot, testSecret, textUpdate(999, BtnMyDays))

	if texts := fake.texts(); len(texts) != 1 || texts[0] != unknownChatMsg {
		t.Errorf("reply to unknown chat = %v, want %q", texts, unknownChatMsg)
	}
}

func TestStartLink(t *testing.T) {
	st, _ := newTestDB(t)
	if got := New(st, Config{Username: "@Dani90Bot"}).StartLink("abc"); got != "https://t.me/Dani90Bot?start=abc" {
		t.Errorf("StartLink = %q", got)
	}
	if got := New(st, Config{}).StartLink("abc"); got != "" {
		t.Errorf("StartLink without username = %q, want empty", got)
	}
}

// --- overstay messaging ---

// A driver who has used all 90 days and presses "Ušao u Šengen" must be
// told he may not stay — never handed a date that reads like permission.
func TestDriverExited_WithNoDaysLeftWarnsInsteadOfGivingADate(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, tn := newTestBot(t)
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
	ds := status.DriverStatus{
		UsedDays: 93, Remaining: 0, OverBy: 3, InSchengen: true,
		CurrentSince: &since, MaxContinuousUntil: &deadline,
	}
	for _, text := range []string{formatMyDays(ds), formatDriverDays(ds)} {
		if strings.Contains(text, "ostaneš do") || strings.Contains(text, "ostane do") {
			t.Errorf("over-limit summary offers a 'may stay until' date:\n%s", text)
		}
		if !strings.Contains(text, "PREKORAČENO za 3 dana") {
			t.Errorf("over-limit summary does not state the overstay:\n%s", text)
		}
	}
}

// --- help ---

func TestDriverHelp_ReachableByButtonAndCommand(t *testing.T) {
	for _, input := range []string{BtnHelp, "/pomoc", "/pomoć", "/help"} {
		t.Run(input, func(t *testing.T) {
			fake := newFakeTelegram(t)
			bot, tn := newTestBot(t)
			driver := &store.Driver{ID: 1, TenantID: tn.ID, Name: "Marko"}

			bot.handleDriverMessage(context.Background(), fake.client(), tn, 555, driver, input)

			texts := fake.texts()
			if len(texts) != 1 || texts[0] != driverHelpText {
				t.Fatalf("reply to %q = %v, want the driver help text", input, texts)
			}
		})
	}
}

// The help must steer drivers on transit routes to the Schengen border, not
// the Serbian one — pressing at the wrong border is the main way the count
// goes wrong.
func TestDriverHelp_ExplainsTheSchengenBorderAndRecovery(t *testing.T) {
	for _, must := range []string{
		"granica Šengena",
		"BiH",
		"privatna putovanja",
		"zaboravio",
		"Poništi",
		"EES",
	} {
		if !strings.Contains(driverHelpText, must) {
			t.Errorf("driver help is missing %q", must)
		}
	}
}

func TestDriverKeyboard_HasHelpButton(t *testing.T) {
	var found bool
	for _, row := range DriverKeyboard().Keyboard {
		for _, b := range row {
			if b.Text == BtnHelp {
				found = true
			}
		}
	}
	if !found {
		t.Error("driver keyboard has no help button — drivers don't type commands")
	}
}

func TestAdminHelp_ListsEveryCommand(t *testing.T) {
	for _, cmd := range []string{"/pregled", "/vozac", "/planer", "/dodaj", "/link", "/ispravi", "/sablon", "/pomoc"} {
		if !strings.Contains(adminHelpText, cmd) {
			t.Errorf("admin help does not mention %s", cmd)
		}
	}
}

func TestAdminHelp_ReachableByCommand(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, tn := newTestBot(t)
	owner := &store.User{ID: 1, TenantID: tn.ID, Role: "owner"}

	bot.handleAdminMessage(context.Background(), fake.client(), tn, store.TenantSettings{}, 777, owner, &Message{Text: "/pomoc"})

	if texts := fake.texts(); len(texts) != 1 || texts[0] != adminHelpText {
		t.Fatalf("reply to /pomoc = %v, want the admin help text", texts)
	}
}

// Linking a driver is the moment he is most likely to read instructions, so
// the welcome message carries the full help, not just "povezani ste".
func TestStart_DriverWelcomeIncludesHelp(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, tn := newTestBot(t)
	ctx := context.Background()

	driver, err := bot.Store.CreateDriver(ctx, tn.ID, "Marko", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := bot.Store.CreateDriverLinkToken(ctx, "tok123", tn.ID, driver.ID, time.Hour); err != nil {
		t.Fatal(err)
	}

	bot.handleStart(ctx, fake.client(), 555, "/start tok123")

	texts := fake.texts()
	if len(texts) != 1 || !strings.Contains(texts[0], driverHelpText) {
		t.Fatalf("welcome = %v, want it to contain the driver help", texts)
	}
}

// A driver who leaves Schengen and comes back on the same calendar date
// (Neum corridor, a quick turnaround at the border) presses "Izašao iz Šengena" and
// then "Ušao u Šengen" again on the same day. The second press must be recorded,
// not rejected as an overlap with the stay that just closed today.
func TestDriverExited_AgainOnTheDayHeReturned(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, tn := newTestBot(t)
	ctx := context.Background()

	driver, err := bot.Store.CreateDriver(ctx, tn.ID, "Marko", "")
	if err != nil {
		t.Fatalf("create driver: %v", err)
	}
	today := clock.Today()
	if _, err := bot.Store.CreateStay(ctx, tn.ID, driver.ID, today.AddDate(0, 0, -5), nil, "work", "telegram_button", "", nil); err != nil {
		t.Fatalf("seed open stay: %v", err)
	}

	bot.driverReturned(ctx, fake.client(), tn, 555, driver)
	bot.driverExited(ctx, fake.client(), tn, 555, driver)

	if _, err := bot.Store.OpenStayForDriver(ctx, tn.ID, driver.ID); err != nil {
		texts := fake.texts()
		t.Fatalf("no open stay after re-entering the same day; last reply: %q", texts[len(texts)-1])
	}
	stays, _ := bot.Store.ListStaysByDriver(ctx, tn.ID, driver.ID)
	if got := status.Compute(*driver, stays, today).UsedDays; got != 6 {
		t.Errorf("UsedDays = %d, want 6 (today counted once)", got)
	}
}

// QA-01: a second "Izašao" answered with "Da, ispravi" means the earlier
// entry date was a mistake. The driver must stay in Schengen, now from
// today, and "Poništi" must bring the old entry date back.
func TestReopenConfirm_DriverStaysInSchengen(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, tn := newTestBot(t)
	ctx := context.Background()

	driver, err := bot.Store.CreateDriver(ctx, tn.ID, "Marko", "")
	if err != nil {
		t.Fatalf("create driver: %v", err)
	}
	today := clock.Today()
	oldEntry := today.AddDate(0, 0, -5)
	st, err := bot.Store.CreateStay(ctx, tn.ID, driver.ID, oldEntry, nil, "work", "telegram_button", "", nil)
	if err != nil {
		t.Fatalf("seed open stay: %v", err)
	}

	bot.driverExited(ctx, fake.client(), tn, 555, driver)
	bot.handleReopenConfirm(ctx, fake.client(), tn, 555, driver, "cb")

	open, err := bot.Store.OpenStayForDriver(ctx, tn.ID, driver.ID)
	if err != nil {
		t.Fatalf("no open stay after 'Da, ispravi': %v (replies %q)", err, fake.texts())
	}
	if !open.Entry.Equal(today) {
		t.Errorf("open stay entry = %s, want today %s", open.Entry.Format("02.01.2006"), today.Format("02.01.2006"))
	}
	stays, _ := bot.Store.ListStaysByDriver(ctx, tn.ID, driver.ID)
	if len(stays) != 1 {
		t.Errorf("driver has %d stays, want 1 (the corrected one)", len(stays))
	}
	if !status.Compute(*driver, stays, today).InSchengen {
		t.Error("InSchengen = false after 'Da, ispravi'")
	}

	bot.handleUndo(ctx, fake.client(), tn, 555, driver, "cb2", st.ID, entryUndoKind(oldEntry))
	open, err = bot.Store.OpenStayForDriver(ctx, tn.ID, driver.ID)
	if err != nil || !open.Entry.Equal(oldEntry) {
		t.Errorf("after Poništi open stay = %+v, %v; want entry %s", open, err, oldEntry.Format("02.01.2006"))
	}
}

// --- crossing date around midnight (QA-02) ---

// pinClock sets the Belgrade wall clock for the rest of the test.
func pinClock(t *testing.T, belgradeTime string) {
	t.Helper()
	loc := clock.Now().Location()
	v, err := time.ParseInLocation("2006-01-02 15:04", belgradeTime, loc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(clock.Set(func() time.Time { return v }))
}

func (f *fakeTelegram) lastCallbackData() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for i := len(f.calls) - 1; i >= 0; i-- {
		rm, ok := f.calls[i].Body["reply_markup"].(map[string]any)
		if !ok {
			continue
		}
		rows, _ := rm["inline_keyboard"].([]any)
		for _, row := range rows {
			for _, btn := range row.([]any) {
				out = append(out, btn.(map[string]any)["callback_data"].(string))
			}
		}
		return out
	}
	return nil
}

// 00:30 in Sofia is 23:30 in Belgrade. The stamp says the 15th; the bot must
// be able to record the 15th, not silently write the 14th.
func TestDriverExited_NearMidnightAsksForStampDate(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, tn := newTestBot(t)
	pinClock(t, "2026-10-14 23:30")
	ctx := context.Background()
	driver, _ := bot.Store.CreateDriver(ctx, tn.ID, "Marko", "")

	bot.driverExited(ctx, fake.client(), tn, 555, driver)
	if _, err := bot.Store.OpenStayForDriver(ctx, tn.ID, driver.ID); err == nil {
		t.Fatal("stay recorded without asking for the date")
	}
	choices := fake.lastCallbackData()
	want := []string{"date:exit:2026-10-14", "date:exit:2026-10-15"}
	if strings.Join(choices, ",") != strings.Join(want, ",") {
		t.Fatalf("choices = %v, want %v", choices, want)
	}

	bot.handleDriverCallback(ctx, fake.client(), tn, 555, driver, &CallbackQuery{ID: "cb", Data: choices[1]})
	open, err := bot.Store.OpenStayForDriver(ctx, tn.ID, driver.ID)
	if err != nil {
		t.Fatalf("no open stay after picking the date: %v (replies %q)", err, fake.texts())
	}
	if got := open.Entry.Format("2006-01-02"); got != "2026-10-15" {
		t.Errorf("entry = %s, want 2026-10-15 (stamp date)", got)
	}
}

func TestDriverReturned_AfterMidnightOffersYesterday(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, tn := newTestBot(t)
	pinClock(t, "2026-10-15 00:40")
	ctx := context.Background()
	driver, _ := bot.Store.CreateDriver(ctx, tn.ID, "Marko", "")
	if _, err := bot.Store.CreateStay(ctx, tn.ID, driver.ID, clock.Today().AddDate(0, 0, -5), nil, "work", "telegram_button", "", nil); err != nil {
		t.Fatal(err)
	}

	bot.driverReturned(ctx, fake.client(), tn, 555, driver)
	choices := fake.lastCallbackData()
	want := []string{"date:return:2026-10-14", "date:return:2026-10-15"}
	if strings.Join(choices, ",") != strings.Join(want, ",") {
		t.Fatalf("choices = %v, want %v", choices, want)
	}
	bot.handleDriverCallback(ctx, fake.client(), tn, 555, driver, &CallbackQuery{ID: "cb", Data: choices[0]})
	stays, _ := bot.Store.ListStaysByDriver(ctx, tn.ID, driver.ID)
	if len(stays) != 1 || stays[0].Exit == nil || stays[0].Exit.Format("2006-01-02") != "2026-10-14" {
		t.Errorf("stays = %+v, want one closed on 2026-10-14", stays)
	}
}

// Outside the window the button records today without a question, and a
// forged or stale date choice is refused.
func TestDriverExited_DaytimeRecordsToday(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, tn := newTestBot(t)
	pinClock(t, "2026-10-14 22:59")
	ctx := context.Background()
	driver, _ := bot.Store.CreateDriver(ctx, tn.ID, "Marko", "")

	bot.handleDriverCallback(ctx, fake.client(), tn, 555, driver, &CallbackQuery{ID: "cb", Data: "date:exit:2026-10-16"})
	if _, err := bot.Store.OpenStayForDriver(ctx, tn.ID, driver.ID); err == nil {
		t.Fatal("a date two days ahead was accepted")
	}

	bot.driverExited(ctx, fake.client(), tn, 555, driver)
	open, err := bot.Store.OpenStayForDriver(ctx, tn.ID, driver.ID)
	if err != nil || open.Entry.Format("2006-01-02") != "2026-10-14" {
		t.Errorf("open stay = %+v, %v; want entry 2026-10-14", open, err)
	}
}

// --- Telegram identity (SEC-09, SEC-12) ---

func groupUpdate(chatID, fromID int64, text string) string {
	b, _ := json.Marshal(Update{UpdateID: nextUpdateID(), Message: &Message{MessageID: 1, From: &TgUser{ID: fromID}, Chat: Chat{ID: chatID, Type: "group"}, Text: text}})
	return string(b)
}

func callbackUpdate(chatID, fromID int64, data string) string {
	b, _ := json.Marshal(Update{UpdateID: nextUpdateID(), CallbackQuery: &CallbackQuery{
		ID: "cb", From: TgUser{ID: fromID}, Data: data,
		Message: &Message{MessageID: 1, Chat: Chat{ID: chatID, Type: "private"}},
	}})
	return string(b)
}

// /start in a group would make every member of the group the driver (or the
// admin). The link token must survive untouched and nothing gets linked.
func TestWebhook_StartInGroupIsRefused(t *testing.T) {
	newFakeTelegram(t)
	bot, tn := newTestBot(t)
	ctx := context.Background()
	driver, _ := bot.Store.CreateDriver(ctx, tn.ID, "Marko", "")
	if err := bot.Store.CreateDriverLinkToken(ctx, "tokG", tn.ID, driver.ID, time.Hour); err != nil {
		t.Fatal(err)
	}

	postWebhook(t, bot, testSecret, groupUpdate(-100500, 777, "/start tokG"))

	for _, chat := range []int64{-100500, 777} {
		if d, err := bot.Store.GetDriverByTelegramChat(ctx, chat); err == nil {
			t.Errorf("chat %d linked to driver %d from a group", chat, d.ID)
		}
	}
	if _, err := bot.Store.ConsumeLinkToken(ctx, "tokG"); err != nil {
		t.Errorf("the token was spent by the group: %v", err)
	}
}

// A group chat linked before the fix must not act as the driver either.
func TestWebhook_ButtonsInGroupAreIgnored(t *testing.T) {
	newFakeTelegram(t)
	bot, tn := newTestBot(t)
	ctx := context.Background()
	driver, _ := bot.Store.CreateDriver(ctx, tn.ID, "Marko", "")
	if err := bot.Store.SetDriverTelegramChatID(ctx, tn.ID, driver.ID, -100500); err != nil {
		t.Fatal(err)
	}

	postWebhook(t, bot, testSecret, groupUpdate(-100500, 777, BtnEnteredSchengen))

	if _, err := bot.Store.OpenStayForDriver(ctx, tn.ID, driver.ID); err == nil {
		t.Error("a group member recorded an exit for the driver")
	}
}

// A button press is the driver's only if the person who pressed it is the
// linked account.
func TestWebhook_CallbackFromSomeoneElseIsRefused(t *testing.T) {
	newFakeTelegram(t)
	bot, tn := newTestBot(t)
	ctx := context.Background()
	driver, _ := bot.Store.CreateDriver(ctx, tn.ID, "Marko", "")
	if err := bot.Store.SetDriverTelegramChatID(ctx, tn.ID, driver.ID, 555); err != nil {
		t.Fatal(err)
	}
	if _, err := bot.Store.CreateStay(ctx, tn.ID, driver.ID, clock.Today().AddDate(0, 0, -3), nil, "work", "telegram_button", "", nil); err != nil {
		t.Fatal(err)
	}

	postWebhook(t, bot, testSecret, callbackUpdate(555, 777, "reminder_returned"))
	if _, err := bot.Store.OpenStayForDriver(ctx, tn.ID, driver.ID); err != nil {
		t.Error("a callback from another account closed the driver's stay")
	}

	postWebhook(t, bot, testSecret, callbackUpdate(555, 555, "reminder_returned"))
	if _, err := bot.Store.OpenStayForDriver(ctx, tn.ID, driver.ID); err == nil {
		t.Error("the driver's own callback did not close the stay")
	}
}

// A deactivated driver no longer writes through the bot, and his Telegram
// account is free to be linked again (by another firm, say).
func TestDeactivatedDriverLosesBotAccess(t *testing.T) {
	newFakeTelegram(t)
	bot, tn := newTestBot(t)
	ctx := context.Background()
	driver, _ := bot.Store.CreateDriver(ctx, tn.ID, "Marko", "")
	if err := bot.Store.SetDriverTelegramChatID(ctx, tn.ID, driver.ID, 555); err != nil {
		t.Fatal(err)
	}

	if err := bot.Store.SetDriverActive(ctx, tn.ID, driver.ID, false); err != nil {
		t.Fatal(err)
	}
	postWebhook(t, bot, testSecret, textUpdate(555, BtnEnteredSchengen))

	if _, err := bot.Store.OpenStayForDriver(ctx, tn.ID, driver.ID); err == nil {
		t.Error("deactivated driver recorded an exit")
	}
	d, _ := bot.Store.GetDriver(ctx, tn.ID, driver.ID)
	if d.TelegramChatID != nil {
		t.Errorf("deactivated driver keeps telegram_chat_id %d", *d.TelegramChatID)
	}
	other, _ := bot.Store.CreateDriver(ctx, tn.ID, "Novi", "")
	if err := bot.Store.SetDriverTelegramChatID(ctx, tn.ID, other.ID, 555); err != nil {
		t.Errorf("the account can't be linked again: %v", err)
	}
}

// SEC-02: a deactivated user loses the bot at once. Deactivation unlinks the
// chat; the lookup also skips inactive users, in case a chat is left linked.
func TestWebhook_DeactivatedUserIsRefused(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, a, _, _, _ := twoFirms(t)
	ctx := context.Background()

	if _, err := bot.Store.CreateUser(ctx, a.ID, "vlasnik@a.rs", "hash", "owner", "Vlasnik A"); err != nil {
		t.Fatal(err)
	}
	disp, err := bot.Store.CreateUser(ctx, a.ID, "dispecer@a.rs", "hash", "dispatcher", "Dispečer A")
	if err != nil {
		t.Fatal(err)
	}
	if err := bot.Store.SetUserTelegramChatID(ctx, a.ID, disp.ID, 444); err != nil {
		t.Fatal(err)
	}

	postWebhook(t, bot, testSecret, textUpdate(444, "/pregled"))
	if texts := fake.texts(); len(texts) != 1 || !strings.Contains(texts[0], "Ana Prva") {
		t.Fatalf("active dispatcher: got %v, want /pregled", texts)
	}

	if _, err := bot.Store.SetUserActive(ctx, a.ID, disp.ID, false); err != nil {
		t.Fatal(err)
	}
	postWebhook(t, bot, testSecret, textUpdate(444, "/pregled"))
	if texts := fake.texts(); len(texts) != 2 || texts[1] != unknownChatMsg {
		t.Errorf("deactivated dispatcher: got %v, want %q", texts, unknownChatMsg)
	}

	// Second lock: inactive with the chat still linked.
	if _, err := bot.Store.SetUserActive(ctx, a.ID, disp.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := bot.Store.SetUserTelegramChatID(ctx, a.ID, disp.ID, 444); err != nil {
		t.Fatal(err)
	}
	if _, err := bot.Store.DB.ExecContext(ctx, `UPDATE users SET active = 0 WHERE id = ?`, disp.ID); err != nil {
		t.Fatal(err)
	}
	postWebhook(t, bot, testSecret, textUpdate(444, "/pregled"))
	if texts := fake.texts(); len(texts) != 3 || texts[2] != unknownChatMsg {
		t.Errorf("inactive user with a linked chat: got %v, want %q", texts, unknownChatMsg)
	}
}
