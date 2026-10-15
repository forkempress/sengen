package superadmin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"shengen/internal/clock"
	"shengen/internal/db"
	"shengen/internal/health"
	"shengen/internal/store"
	"shengen/internal/telegram"
)

const adminID int64 = 4242

// --- fake Telegram ---

type sent struct {
	ChatID int64
	Text   string
}

type fakeTelegram struct {
	mu    sync.Mutex
	msgs  []sent
	other []string // non-sendMessage methods called
}

func newFakeTelegram(t *testing.T) *fakeTelegram {
	t.Helper()
	f := &fakeTelegram{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			id, _ := body["chat_id"].(float64)
			text, _ := body["text"].(string)
			f.msgs = append(f.msgs, sent{ChatID: int64(id), Text: text})
			io.WriteString(w, `{"ok":true,"result":{}}`)
		case strings.HasSuffix(r.URL.Path, "/getWebhookInfo"):
			f.other = append(f.other, "getWebhookInfo")
			io.WriteString(w, `{"ok":true,"result":{"url":"https://app.dani90.rs/telegram/webhook","pending_update_count":0}}`)
		default:
			f.other = append(f.other, r.URL.Path)
			io.WriteString(w, `{"ok":true,"result":{}}`)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("TELEGRAM_API_BASE_URL", srv.URL)
	return f
}

func (f *fakeTelegram) take() []sent {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.msgs
	f.msgs = nil
	return out
}

// --- fixture ---

// The fixed "now": Wednesday 14.10.2026, 12:00 in Belgrade.
var fixedNow = time.Date(2026, 10, 14, 12, 0, 0, 0, clock.Now().Location())

func setNow(t *testing.T, at time.Time) {
	t.Helper()
	t.Cleanup(clock.Set(func() time.Time { return at }))
}

func ago(d time.Duration) string {
	return fixedNow.Add(-d).UTC().Format("2006-01-02T15:04:05.000Z")
}

const day = 24 * time.Hour

func dateIn(days int) *time.Time {
	d := clock.Today().AddDate(0, 0, days)
	return &d
}

// Every driver name in the fixture: none may ever appear in superadmin output.
var driverNames = []string{"Marko Tajni", "Jovan Skriveni", "Petar Anonimni", "Stari Neaktivni", "Ana Diskretna", "Mila Privatna", "Zoran Povuceni"}

type fixture struct {
	st                       *store.Store
	alfa, beta, cezar, delta *store.Tenant
	epsilon                  *store.Tenant
}

// newFixture builds five firms with known counts:
//
//	Alfa    paid   300 din 3 active drivers (2 on Telegram) + 1 inactive; owner+dispatcher on Telegram
//	Beta    trial  €3,50   2 active drivers, trial ends in 2 days, first stay 2h ago, created 5 days ago
//	Cezar   paused 300 din 1 active driver, no activity
//	Delta   trial  300 din no drivers, created 30 days ago, never active, trial ends in 20 days
//	Epsilon trial  300 din created 2 hours ago, trial ends in 30 days
func newFixture(t *testing.T) *fixture {
	t.Helper()
	setNow(t, fixedNow)
	health.Reset()
	t.Cleanup(health.Reset)

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	st := store.New(conn)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	exec := func(q string, args ...any) {
		t.Helper()
		_, err := conn.Exec(q, args...)
		must(err)
	}
	tenant := func(name, slug, plan string, trial *time.Time, price int, currency string, created time.Duration) *store.Tenant {
		tn, err := st.CreateTenant(ctx, name, slug)
		must(err)
		must(st.SetTenantBilling(ctx, tn.ID, plan, trial, price, currency))
		exec(`UPDATE tenants SET created_at = ? WHERE id = ?`, ago(created), tn.ID)
		return tn
	}
	driver := func(tn *store.Tenant, name string, created time.Duration) *store.Driver {
		d, err := st.CreateDriver(ctx, tn.ID, name, "")
		must(err)
		exec(`UPDATE drivers SET created_at = ? WHERE id = ?`, ago(created), d.ID)
		return d
	}
	stay := func(tn *store.Tenant, d *store.Driver, entryDays, exitDays int, open bool, created time.Duration) {
		entry := clock.Today().AddDate(0, 0, -entryDays)
		var exit *time.Time
		if !open {
			exit = dateIn(-exitDays)
		}
		s, err := st.CreateStay(ctx, tn.ID, d.ID, entry, exit, "work", "manual", "", nil)
		must(err)
		exec(`UPDATE stays SET created_at = ?, updated_at = ? WHERE id = ?`, ago(created), ago(created), s.ID)
	}

	f := &fixture{st: st}
	f.alfa = tenant("Alfa Prevoz", "alfa", store.PlanPaid, nil, 30000, store.CurrencyRSD, 60*day)
	owner, err := st.CreateUser(ctx, f.alfa.ID, "vlasnik@alfa.rs", "h", "owner", "Vlasnik Alfa")
	must(err)
	disp, err := st.CreateUser(ctx, f.alfa.ID, "dispecer@alfa.rs", "h", "dispatcher", "Dispečer Alfa")
	must(err)
	must(st.SetUserTelegramChatID(ctx, f.alfa.ID, owner.ID, 902))
	must(st.SetUserTelegramChatID(ctx, f.alfa.ID, disp.ID, 903))
	marko := driver(f.alfa, "Marko Tajni", 60*day)
	jovan := driver(f.alfa, "Jovan Skriveni", 60*day)
	petar := driver(f.alfa, "Petar Anonimni", 60*day)
	stari := driver(f.alfa, "Stari Neaktivni", 60*day)
	must(st.SetDriverTelegramChatID(ctx, f.alfa.ID, marko.ID, 901))
	must(st.SetDriverTelegramChatID(ctx, f.alfa.ID, jovan.ID, 905))
	stay(f.alfa, marko, 10, 0, true, 10*day)
	stay(f.alfa, jovan, 40, 30, false, 40*day)
	stay(f.alfa, petar, 100, 95, false, 100*day)
	stay(f.alfa, stari, 5, 0, true, 5*day)
	must(st.SetDriverActive(ctx, f.alfa.ID, stari.ID, false))

	f.beta = tenant("Beta Trans", "beta", store.PlanTrial, dateIn(2), 350, store.CurrencyEUR, 5*day)
	_, err = st.CreateUser(ctx, f.beta.ID, "vlasnik@beta.rs", "h", "owner", "Vlasnik Beta")
	must(err)
	ana := driver(f.beta, "Ana Diskretna", 5*day)
	driver(f.beta, "Mila Privatna", 5*day)
	stay(f.beta, ana, 3, 2, false, 2*time.Hour)

	f.cezar = tenant("Cezar Logistika", "cezar", store.PlanPaused, nil, 30000, store.CurrencyRSD, 100*day)
	_, err = st.CreateUser(ctx, f.cezar.ID, "vlasnik@cezar.rs", "h", "owner", "Vlasnik Cezar")
	must(err)
	driver(f.cezar, "Zoran Povuceni", 100*day)

	f.delta = tenant("Delta Kamioni", "delta", store.PlanTrial, dateIn(20), 30000, store.CurrencyRSD, 30*day)
	f.epsilon = tenant("Epsilon Cargo", "epsilon", store.PlanTrial, dateIn(30), 30000, store.CurrencyRSD, 2*time.Hour)
	return f
}

func assertNoDriverNames(t *testing.T, label, out string) {
	t.Helper()
	for _, n := range driverNames {
		if strings.Contains(out, n) || strings.Contains(out, strings.Fields(n)[0]) {
			t.Errorf("%s leaks driver name %q:\n%s", label, n, out)
		}
	}
}

func assertContains(t *testing.T, label, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("%s: missing %q in:\n%s", label, w, out)
		}
	}
}

// --- aggregates ---

func TestPlatformStats_ExactCounts(t *testing.T) {
	f := newFixture(t)
	p, err := f.st.PlatformStats(context.Background(), fixedNow.Add(-7*day))
	if err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		name      string
		got, want int
	}{
		{"tenants", p.Tenants(), 5},
		{"trial", p.TenantsByPlan[store.PlanTrial], 3},
		{"paid", p.TenantsByPlan[store.PlanPaid], 1},
		{"paused", p.TenantsByPlan[store.PlanPaused], 1},
		{"users", p.Users(), 4},
		{"owners", p.UsersByRole["owner"], 3},
		{"dispatchers", p.UsersByRole["dispatcher"], 1},
		{"users on telegram", p.UsersOnTelegram, 2},
		{"active drivers", p.DriversActive, 6},  // the deactivated driver is excluded
		{"linked drivers", p.DriversLinked, 2},  // deactivating also unlinked nobody counted
		{"in schengen", p.DriversInSchengen, 1}, // the inactive driver's open stay does not count
		{"stays", p.Stays, 5},
		{"stays last 7 days", p.StaysSince, 2},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
}

func TestMRR_ExactAmounts(t *testing.T) {
	f := newFixture(t)
	firms, err := f.st.ListTenantOverviews(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	m := ComputeMRR(firms)
	// Alfa: 3 active × 300 din = 900 din. Trial: Beta 2 × €3,50 = €7; Delta and Epsilon have no drivers.
	want := MRR{Paid: Amounts{"RSD": 90000}, PaidTenants: 1, PaidDrivers: 3,
		Trial: Amounts{"RSD": 0, "EUR": 700}, TrialTenants: 3, TrialDrivers: 2}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("MRR = %+v, want %+v", m, want)
	}

	out, err := f.svc().Run(context.Background(), nil, "/mrr", "")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, "/mrr", out, "MRR: 900 din", "Plaćene firme: 1, vozača 3", "prosek 900 din", "€7,00 iz 3 firmi, 2 vozača")
}

func (f *fixture) svc() *Service { return New(f.st, []int64{adminID}, "") }

func TestCommands_OutputAndPrivacy(t *testing.T) {
	f := newFixture(t)
	s := f.svc()
	ctx := context.Background()

	cases := []struct {
		cmd, args string
		want      []string
		notWant   []string
	}{
		{"/stats", "", []string{
			"Firme: 5 (trial 3, plaćena 1, pauzirana 1)",
			"Korisnici: 4 (vlasnici 3, dispečeri 1), na Telegramu 2",
			"Vozači: 6 aktivnih, 2 povezanih sa botom",
			"U Šengenu sada: 1 vozača",
			"Boravci: 5 ukupno, 2 unetih u poslednjih 7 dana",
			"Registracije (7 dana): 0 započeto, 0 potvrđeno",
		}, nil},
		{"/firme", "", []string{
			fmt.Sprintf("#%d Alfa Prevoz — plaćena, 3 vozača", f.alfa.ID),
			fmt.Sprintf("#%d Delta Kamioni — trial, 0 vozača, bez aktivnosti", f.delta.ID),
		}, nil},
		{"/firma", fmt.Sprint(f.alfa.ID), []string{
			"Alfa Prevoz (oznaka alfa)",
			"Plan: plaćena",
			"Cena: 300 din po vozaču, mesečno 900 din",
			"Korisnici: 2 (vlasnici 1, dispečeri 1)",
			"Vozači: 3 aktivnih, 2 povezanih sa botom",
			"Boravci: 4",
			"Danas: 1 u Šengenu",
		}, []string{"@alfa.rs", "Vlasnik Alfa"}},
		{"/firma", "99999", []string{"Firma #99999 ne postoji."}, nil},
		{"/firma", "abc", []string{"Upotreba: /firma <id>"}, nil},
		{"/trial", "", []string{
			"Trial (3)",
			fmt.Sprintf("#%d Beta Trans — trial do %s (još 2 dana), 2 vozača", f.beta.ID, dateIn(2).Format("02.01.2006")),
		}, []string{"Alfa", "Cezar"}},
		{"/neaktivne", "", []string{
			"(1)",
			fmt.Sprintf("#%d Delta Kamioni — trial, poslednja aktivnost nikad", f.delta.ID),
		}, []string{"Alfa", "Beta", "Cezar", "Epsilon"}},
		{"/zdravlje", "", []string{"Baza: OK", "Backup: nije podešen", "Greške (24h): nema"}, nil},
	}
	for _, c := range cases {
		out, err := s.Run(ctx, nil, c.cmd, c.args)
		if err != nil {
			t.Fatalf("%s %s: %v", c.cmd, c.args, err)
		}
		label := strings.TrimSpace(c.cmd + " " + c.args)
		assertContains(t, label, out, c.want...)
		for _, nw := range c.notWant {
			if strings.Contains(out, nw) {
				t.Errorf("%s: unexpected %q in:\n%s", label, nw, out)
			}
		}
		assertNoDriverNames(t, label, out)
		if len(out) > 4096 {
			t.Errorf("%s: reply is %d chars, over Telegram's limit", label, len(out))
		}
	}

	// Beta trial comes before Delta (sooner end).
	out, _ := s.Run(ctx, nil, "/trial", "")
	if strings.Index(out, "Beta") > strings.Index(out, "Delta") {
		t.Errorf("/trial not sorted by end date:\n%s", out)
	}
}

func TestFormatMoney(t *testing.T) {
	for _, c := range []struct {
		minor    int
		currency string
		want     string
	}{
		{0, "RSD", "0 din"}, {30000, "RSD", "300 din"}, {123400, "RSD", "1.234 din"}, {123450, "RSD", "1.234,50 din"},
		{-450000, "RSD", "-4.500 din"}, {0, "EUR", "€0,00"}, {5, "EUR", "€0,05"}, {123456, "EUR", "€1.234,56"},
	} {
		if got := formatMoney(c.minor, c.currency); got != c.want {
			t.Errorf("formatMoney(%d, %s) = %q, want %q", c.minor, c.currency, got, c.want)
		}
	}
	for a, want := range map[string]Amounts{
		"0 din": {}, "900 din": {"RSD": 90000, "EUR": 0}, "900 din + €7,00": {"EUR": 700, "RSD": 90000},
	} {
		if got := want.String(); got != a {
			t.Errorf("Amounts%v = %q, want %q", map[string]int(want), got, a)
		}
	}
}

// --- access control, end to end through the webhook ---

const webhookSecret = "test-secret"

var updateID atomic.Int64

func post(t *testing.T, bot *telegram.Bot, chatID int64, chatType string, fromID int64, text string) {
	t.Helper()
	body := fmt.Sprintf(`{"update_id":%d,"message":{"message_id":1,"from":{"id":%d,"first_name":"x"},"chat":{"id":%d,"type":%q},"date":0,"text":%q}}`,
		updateID.Add(1), fromID, chatID, chatType, text)
	r := httptest.NewRequest(http.MethodPost, "/telegram/webhook", strings.NewReader(body))
	r.Header.Set("X-Telegram-Bot-Api-Secret-Token", webhookSecret)
	w := httptest.NewRecorder()
	bot.HandleWebhook(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("webhook status %d", w.Code)
	}
	if err := bot.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func newBot(t *testing.T, f *fixture) (*telegram.Bot, *fakeTelegram) {
	t.Helper()
	fake := newFakeTelegram(t)
	bot := telegram.New(f.st, telegram.Config{Token: "TEST-TOKEN", WebhookSecret: webhookSecret})
	bot.SuperAdmin = f.svc()
	return bot, fake
}

func joinTexts(ms []sent) string {
	var parts []string
	for _, m := range ms {
		parts = append(parts, m.Text)
	}
	return strings.Join(parts, "\n---\n")
}

// A caller who is not an operator must not be able to run any superadmin
// command, and must not be able to tell the commands exist: the reply is
// exactly the reply to a made-up command.
func TestNonAdmin_CannotRunAnySuperadminCommand(t *testing.T) {
	f := newFixture(t)
	bot, fake := newBot(t, f)

	callers := []struct {
		name     string
		chatID   int64
		chatType string
		fromID   int64
	}{
		{"linked driver", 901, "private", 901},
		{"linked owner", 902, "private", 902},
		{"linked dispatcher", 903, "private", 903},
		{"unknown account", 904, "private", 904},
		{"operator id in a group chat", -100500, "group", adminID},
		{"operator id in a supergroup", -100501, "supergroup", adminID},
		{"someone else in the operator's chat id", adminID, "private", 777},
	}
	for _, c := range callers {
		post(t, bot, c.chatID, c.chatType, c.fromID, "/nepostojeca_komanda")
		baseline := joinTexts(fake.take())

		for _, cmd := range Commands {
			for _, text := range []string{cmd, cmd + " " + fmt.Sprint(f.alfa.ID)} {
				post(t, bot, c.chatID, c.chatType, c.fromID, text)
				got := joinTexts(fake.take())
				if got != baseline {
					t.Errorf("%s, %q: reply %q differs from the unknown-command reply %q", c.name, text, got, baseline)
				}
				for _, leak := range []string{"Statistika", "MRR", "Superadmin", "Alfa Prevoz", "Zdravlje", "Trial ("} {
					if strings.Contains(got, leak) {
						t.Errorf("%s, %q: reply leaks %q: %s", c.name, text, leak, got)
					}
				}
			}
		}
	}

	// Every private attempt was audited as refused; none as allowed.
	entries, err := f.st.ListSuperadminAudit(context.Background(), 1000)
	if err != nil {
		t.Fatal(err)
	}
	// The group chats and the foreign sender in a private chat are dropped by
	// the bot before any handler runs, so only the first four are audited.
	privateCallers := 4
	if want := privateCallers * len(Commands) * 2; len(entries) != want {
		t.Errorf("audit rows = %d, want %d", len(entries), want)
	}
	for _, e := range entries {
		if e.Allowed {
			t.Errorf("audit row allowed for non-operator: %+v", e)
		}
	}
}

func TestAdmin_RunsCommandsAndIsAudited(t *testing.T) {
	f := newFixture(t)
	bot, fake := newBot(t, f)

	post(t, bot, adminID, "private", adminID, "/stats")
	msgs := fake.take()
	if len(msgs) != 1 || msgs[0].ChatID != adminID || !strings.Contains(msgs[0].Text, "Firme: 5") {
		t.Fatalf("operator /stats reply = %+v", msgs)
	}

	post(t, bot, adminID, "private", adminID, fmt.Sprintf("/firma@dani90bot %d", f.beta.ID))
	msgs = fake.take()
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "Beta Trans") {
		t.Fatalf("operator /firma reply = %+v", msgs)
	}

	// An ordinary message from the operator is not a superadmin command and
	// gets the normal (unlinked account) handling.
	post(t, bot, adminID, "private", adminID, "/pregled")
	if msgs = fake.take(); len(msgs) != 1 || strings.Contains(msgs[0].Text, "Firme") {
		t.Errorf("operator /pregled = %+v, want the normal unknown-account reply", msgs)
	}

	entries, err := f.st.ListSuperadminAudit(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("audit rows = %d, want 2: %+v", len(entries), entries)
	}
	// Newest first.
	if e := entries[0]; e.TelegramUserID != adminID || e.Command != "/firma" || e.Args != fmt.Sprint(f.beta.ID) || !e.Allowed || e.OccurredAt.IsZero() {
		t.Errorf("audit row = %+v", e)
	}
	if e := entries[1]; e.Command != "/stats" || !e.Allowed {
		t.Errorf("audit row = %+v", e)
	}
}

// If the audit row can't be written, the command does not run.
func TestAdmin_AuditFailureBlocksCommand(t *testing.T) {
	f := newFixture(t)
	bot, fake := newBot(t, f)
	if _, err := f.st.DB.ExecContext(context.Background(), `DROP TABLE superadmin_audit_log`); err != nil {
		t.Fatal(err)
	}
	post(t, bot, adminID, "private", adminID, "/stats")
	msgs := fake.take()
	if len(msgs) != 1 || msgs[0].Text != auditFailedMsg {
		t.Errorf("reply = %+v, want the audit failure message", msgs)
	}
}

// --- push notifications ---

func newNotifier(t *testing.T, f *fixture, backupFile string) (*Notifier, *fakeTelegram) {
	t.Helper()
	fake := newFakeTelegram(t)
	s := New(f.st, []int64{adminID, 5151}, backupFile)
	return s.Notifier(telegram.NewClient("TEST-TOKEN")), fake
}

func freshBackup(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "backup-ok")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// File mtimes are real time; age it relative to the fake clock.
	if err := os.Chtimes(p, fixedNow.Add(-time.Hour), fixedNow.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNotifier_EventsGoOutOnceToEveryOperator(t *testing.T) {
	f := newFixture(t)
	n, fake := newNotifier(t, f, freshBackup(t))
	ctx := context.Background()

	n.Tick(ctx)
	msgs := fake.take()
	all := joinTexts(msgs)
	assertContains(t, "first tick", all,
		fmt.Sprintf("Nova firma: #%d Epsilon Cargo", f.epsilon.ID),
		fmt.Sprintf("#%d Beta Trans, 2 vozača", f.beta.ID), // trial ends in 2 days
		fmt.Sprintf("Prva aktivnost: #%d Beta Trans", f.beta.ID),
	)
	assertNoDriverNames(t, "notifications", all)
	for _, unwanted := range []string{"Delta", "Alfa", "Cezar", "Greške", "Backup", "Dnevni pregled", "ne prima"} {
		if strings.Contains(all, unwanted) {
			t.Errorf("unexpected %q in first tick:\n%s", unwanted, all)
		}
	}
	// Three events × two operators.
	if len(msgs) != 6 {
		t.Errorf("sent %d messages, want 6: %s", len(msgs), all)
	}
	perOperator := map[int64]int{}
	for _, m := range msgs {
		perOperator[m.ChatID]++
	}
	if perOperator[adminID] != 3 || perOperator[5151] != 3 {
		t.Errorf("messages per operator = %v", perOperator)
	}

	// Nothing repeats on the next tick, nor from a fresh notifier (restart).
	n.Tick(ctx)
	n2, _ := newNotifier(t, f, freshBackup(t))
	n2.Tick(ctx)
	if again := fake.take(); len(again) != 0 {
		t.Errorf("repeated notifications: %s", joinTexts(again))
	}
}

func TestNotifier_ErrorsAtMostHourly(t *testing.T) {
	f := newFixture(t)
	n, fake := newNotifier(t, f, freshBackup(t))
	ctx := context.Background()
	n.Tick(ctx)
	fake.take()

	health.ReportError(health.KindTelegramPanic)
	health.ReportError(health.KindSendError)
	n.Tick(ctx)
	got := joinTexts(fake.take())
	assertContains(t, "error alert", got, "pad obrade poruke 1", "neuspelo slanje 1")

	setNow(t, fixedNow.Add(30*time.Minute))
	health.ReportError(health.KindJobPanic)
	n.Tick(ctx)
	if got := fake.take(); len(got) != 0 {
		t.Errorf("second error alert within the hour: %s", joinTexts(got))
	}

	setNow(t, fixedNow.Add(61*time.Minute))
	n.Tick(ctx)
	got = joinTexts(fake.take())
	assertContains(t, "next hour", got, "pad pozadinskog posla 1")
	if strings.Contains(got, "pad obrade poruke") {
		t.Errorf("already reported errors repeated: %s", got)
	}
}

func TestNotifier_BackupMissingOrStale(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	stale := freshBackup(t)
	os.Chtimes(stale, fixedNow.Add(-30*time.Hour), fixedNow.Add(-30*time.Hour))
	n, fake := newNotifier(t, f, stale)
	n.Tick(ctx)
	assertContains(t, "stale backup", joinTexts(fake.take()), "Backup: poslednji uspešan pre 30h")

	f2 := newFixture(t)
	n, fake = newNotifier(t, f2, filepath.Join(t.TempDir(), "nema"))
	n.Tick(ctx)
	assertContains(t, "missing backup", joinTexts(fake.take()), "Backup: nema zapisa o uspešnom backupu")
}

func TestNotifier_DailyDigestAt8Belgrade(t *testing.T) {
	f := newFixture(t)
	n, fake := newNotifier(t, f, freshBackup(t))
	ctx := context.Background()

	// 07:59 the same day: no digest.
	setNow(t, time.Date(2026, 10, 14, 7, 59, 0, 0, fixedNow.Location()))
	n.Tick(ctx)
	if strings.Contains(joinTexts(fake.take()), "Dnevni pregled") {
		t.Fatal("digest before 08:00")
	}

	// 08:05 next day: digest, once.
	setNow(t, time.Date(2026, 10, 15, 8, 5, 0, 0, fixedNow.Location()))
	n.Tick(ctx)
	got := joinTexts(fake.take())
	assertContains(t, "digest", got,
		"Dnevni pregled — 15.10.2026",
		"Juče: 1 novih firmi, 1 unetih boravaka", // Epsilon and Beta's stay, both on 14.10.
		"MRR: 900 din (1 plaćenih firmi), trial potencijal €7,00",
		"Firme: 5",
	)
	assertNoDriverNames(t, "digest", got)
	if c := strings.Count(got, "Dnevni pregled"); c != 2 {
		t.Errorf("digest sent %d times, want once per operator (2)", c)
	}

	setNow(t, time.Date(2026, 10, 15, 8, 10, 0, 0, fixedNow.Location()))
	n.Tick(ctx)
	if strings.Contains(joinTexts(fake.take()), "Dnevni pregled") {
		t.Error("digest sent twice the same day")
	}
}

// addSignup starts a sign-up whose form was sent `age` before fixedNow.
func (f *fixture) addSignup(t *testing.T, token, email, firm, name string, age time.Duration) {
	t.Helper()
	ctx := context.Background()
	form := store.SignupForm{FirmName: firm, Name: name, PasswordHash: "TAJNI-HES-LOZINKE", TermsVersion: "1"}
	if err := f.st.CreateSignup(ctx, token, "123456", "browser-"+token, email, form, day); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.DB.ExecContext(ctx, `UPDATE email_tokens SET created_at = ? WHERE purpose = 'signup' AND email = ?`, ago(age), email); err != nil {
		t.Fatal(err)
	}
}

func TestNotifier_SignupStartedOnceWithContact(t *testing.T) {
	f := newFixture(t)
	f.addSignup(t, "tok-zeta", "pera@zeta.rs", "Zeta Prevoz", "Pera Perić", time.Hour)
	f.addSignup(t, "tok-omega", "staro@omega.rs", "Omega Stara", "Stari Pokušaj", 30*time.Hour)
	n, fake := newNotifier(t, f, freshBackup(t))
	ctx := context.Background()

	n.Tick(ctx)
	all := joinTexts(fake.take())
	assertContains(t, "signup push", all, "📝 Započeta registracija: Zeta Prevoz — Pera Perić <pera@zeta.rs>, 14.10.2026 11:00. Čeka potvrdu mejla.")
	if c := strings.Count(all, "Započeta registracija"); c != 2 {
		t.Errorf("signup pushes = %d, want one per operator (2):\n%s", c, all)
	}
	for _, unwanted := range []string{"Omega", "TAJNI-HES-LOZINKE", "123456"} {
		if strings.Contains(all, unwanted) {
			t.Errorf("unexpected %q in:\n%s", unwanted, all)
		}
	}

	n.Tick(ctx)
	if again := joinTexts(fake.take()); strings.Contains(again, "Započeta registracija") {
		t.Errorf("signup reported twice: %s", again)
	}
}

func TestSignupCounts_StatsAndDigest(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := f.st.DB.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	mail := func(tag, to string, age time.Duration) {
		t.Helper()
		if err := f.st.RecordEmail(ctx, tag, nil, to, store.EmailSent, ""); err != nil {
			t.Fatal(err)
		}
		exec(`UPDATE email_log SET created_at = ? WHERE id = (SELECT MAX(id) FROM email_log)`, ago(age))
	}
	confirmed := func(tn *store.Tenant, age time.Duration) {
		t.Helper()
		if err := f.st.RecordAudit(ctx, tn.ID, "system", nil, "tenant", tn.ID, "signup", nil, nil); err != nil {
			t.Fatal(err)
		}
		exec(`UPDATE audit_log SET occurred_at = ? WHERE id = (SELECT MAX(id) FROM audit_log)`, ago(age))
	}
	mail("signup", "pera@zeta.rs", 3*time.Hour)
	mail("signup", "pera@zeta.rs", 2*time.Hour) // the form sent again: one sign-up
	mail("signup", "vlasnik@epsilon.rs", 2*time.Hour)
	mail("signup_exists", "vlasnik@beta.rs", time.Hour) // not a new firm
	mail("reset", "vlasnik@alfa.rs", time.Hour)
	mail("signup", "staro@omega.rs", 10*day) // outside the week
	confirmed(f.epsilon, 2*time.Hour)
	confirmed(f.beta, 5*day)

	out, err := f.svc().Run(ctx, nil, "/stats", "")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, "/stats", out, "Registracije (7 dana): 2 započeto, 2 potvrđeno")

	n, fake := newNotifier(t, f, freshBackup(t))
	setNow(t, time.Date(2026, 10, 15, 8, 5, 0, 0, fixedNow.Location()))
	n.Tick(ctx)
	assertContains(t, "digest", joinTexts(fake.take()), "Registracije juče: 2 započeto, 1 potvrđeno")
}

func TestNotifier_NoOperatorsSendsNothing(t *testing.T) {
	f := newFixture(t)
	fake := newFakeTelegram(t)
	New(f.st, nil, "").Notifier(telegram.NewClient("TEST-TOKEN")).Tick(context.Background())
	if got := fake.take(); len(got) != 0 {
		t.Errorf("sent without operators: %s", joinTexts(got))
	}
}

func TestWebhookProblem(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	ok := telegram.WebhookInfo{URL: "https://x/telegram/webhook"}
	if p := webhookProblem(ok, now); p != "" {
		t.Errorf("healthy webhook reported %q", p)
	}
	old := ok
	old.LastErrorDate, old.LastErrorMessage = now.Add(-2*time.Hour).Unix(), "staro"
	if p := webhookProblem(old, now); p != "" {
		t.Errorf("old error reported %q", p)
	}
	bad := ok
	bad.LastErrorDate, bad.LastErrorMessage, bad.PendingUpdateCount = now.Add(-5*time.Minute).Unix(), "Wrong response: 502", 150
	p := webhookProblem(bad, now)
	assertContains(t, "webhook problem", p, "Wrong response: 502", "150 poruka čeka")
	if p := webhookProblem(telegram.WebhookInfo{}, now); !strings.Contains(p, "nije registrovan") {
		t.Errorf("missing webhook: %q", p)
	}
}

func TestNotifier_DocumentImportCostAndOutcome(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	record := func(tenant *store.Tenant, d store.DocumentImport) int64 {
		t.Helper()
		d.TenantID = tenant.ID
		id, err := f.st.RecordDocumentImport(ctx, d)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	// 18.200 input + 2.100 output tokens on Opus 5.5: 18.200×4 + 2.100×20 = 114.800 µ$.
	okID := record(f.alfa, store.DocumentImport{Status: store.DocImportOK, Model: "claude-opus-5-5",
		InputTokens: 18200, OutputTokens: 2100, CostMicroUSD: 114800, StaysRead: 14, DriversRead: 3, RowsValid: 12})
	record(f.beta, store.DocumentImport{Status: store.DocImportError})
	n, fake := newNotifier(t, f, freshBackup(t))

	n.Tick(ctx)
	all := joinTexts(fake.take())
	assertContains(t, "doc import push", all,
		fmt.Sprintf("📄 Uvoz dokumenta: #%d Alfa", f.alfa.ID),
		"pročitano 14 boravaka za 3 vozača, 12 ispravno. Čeka potvrdu.",
		"Trošak: $0,115 (claude-opus-5-5, 18.200 + 2.100 tokena). Firma ovog meseca: $0,115 (1 dokument).",
		fmt.Sprintf("❌ Uvoz dokumenta nije uspeo: #%d Beta Trans", f.beta.ID),
		"Trošak: $0,000. Firma ovog meseca: $0,000 (1 dokument).",
	)
	assertNoDriverNames(t, "doc import", all)

	// Another firm's id and a second confirmation are both ignored.
	if err := f.st.ConfirmDocumentImport(ctx, f.beta.ID, okID, 99, 9); err != nil {
		t.Fatal(err)
	}
	if err := f.st.ConfirmDocumentImport(ctx, f.alfa.ID, okID, 12, 3); err != nil {
		t.Fatal(err)
	}
	if err := f.st.ConfirmDocumentImport(ctx, f.alfa.ID, okID, 1, 1); err != nil {
		t.Fatal(err)
	}
	n.Tick(ctx)
	all = joinTexts(fake.take())
	assertContains(t, "confirm push", all, fmt.Sprintf("✅ Uvoz dokumenta upisan: #%d Alfa", f.alfa.ID), "— 12 boravaka za 3 vozača.")
	if strings.Contains(all, "📄") || strings.Count(all, "upisan") != 2 {
		t.Errorf("want only the confirmation, once per operator:\n%s", all)
	}

	n.Tick(ctx)
	if again := fake.take(); len(again) != 0 {
		t.Errorf("repeated: %s", joinTexts(again))
	}

	// The next morning's digest counts yesterday's documents.
	setNow(t, time.Date(2026, 10, 15, 8, 5, 0, 0, fixedNow.Location()))
	n.Tick(ctx)
	assertContains(t, "digest", joinTexts(fake.take()), "Uvoz dokumenata juče: 2 ($0,115), ovog meseca $0,115")
}

func TestPlural(t *testing.T) {
	for n, want := range map[int]string{1: "1 boravak", 2: "2 boravka", 5: "5 boravaka", 11: "11 boravaka", 12: "12 boravaka", 21: "21 boravak", 24: "24 boravka"} {
		if got := plural(n, "boravak", "boravka", "boravaka"); got != want {
			t.Errorf("plural(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestNotifier_AnthropicKeyReminderDailyFromFiveDays(t *testing.T) {
	f := newFixture(t)
	n, fake := newNotifier(t, f, freshBackup(t))
	ctx := context.Background()
	exp := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	n.AnthropicKeyExpires = &exp
	keyMsgs := func() string {
		var out []string
		for _, m := range fake.take() {
			if strings.Contains(m.Text, "Claude API ključ") {
				out = append(out, m.Text)
			}
		}
		return strings.Join(out, "\n")
	}

	setNow(t, time.Date(2026, 10, 14, 12, 0, 0, 0, fixedNow.Location())) // 6 days before
	n.Tick(ctx)
	if got := keyMsgs(); got != "" {
		t.Fatalf("reminder 6 days early: %s", got)
	}

	setNow(t, time.Date(2026, 10, 15, 7, 0, 0, 0, fixedNow.Location())) // 5 days, before 08:00
	n.Tick(ctx)
	if got := keyMsgs(); got != "" {
		t.Fatalf("reminder before 08:00: %s", got)
	}
	setNow(t, time.Date(2026, 10, 15, 8, 5, 0, 0, fixedNow.Location()))
	n.Tick(ctx)
	assertContains(t, "5 days", keyMsgs(), "🔑 Claude API ključ ističe za 5 dana (20.10.2026)", "ANTHROPIC_API_KEY_EXPIRES")
	setNow(t, time.Date(2026, 10, 15, 9, 0, 0, 0, fixedNow.Location()))
	n.Tick(ctx)
	if got := keyMsgs(); got != "" {
		t.Errorf("reminder twice the same day: %s", got)
	}

	setNow(t, time.Date(2026, 10, 19, 8, 5, 0, 0, fixedNow.Location()))
	n.Tick(ctx)
	assertContains(t, "1 day", keyMsgs(), "ističe sutra (20.10.2026)")

	setNow(t, time.Date(2026, 10, 22, 8, 5, 0, 0, fixedNow.Location()))
	n.Tick(ctx)
	assertContains(t, "expired", keyMsgs(), "je istekao 20.10.2026 — uvoz dokumenata ne radi")

	if got, _ := n.Run(ctx, nil, "/zdravlje", ""); !strings.Contains(got, "Claude ključ: je istekao") {
		t.Errorf("/zdravlje without key line:\n%s", got)
	}
}

func TestNotifier_UnsentMailReportedOnce(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	logMail := func(tag, to, status, errText string, age time.Duration) {
		t.Helper()
		if _, err := f.st.DB.ExecContext(ctx,
			`INSERT INTO email_log (tag, recipient, status, error, created_at) VALUES (?, ?, ?, ?, ?)`,
			tag, to, status, errText, ago(age)); err != nil {
			t.Fatal(err)
		}
	}
	logMail("signup", "pera@zeta.rs", store.EmailFailed, "dial tcp: i/o timeout", time.Hour)
	logMail("reset", "ana@prva.rs", store.EmailOverLimit, "", 2*time.Hour)
	logMail("signup", "ok@zeta.rs", store.EmailSent, "", time.Hour)
	logMail("signup", "staro@omega.rs", store.EmailFailed, "staro", 30*time.Hour)
	n, fake := newNotifier(t, f, freshBackup(t))

	n.Tick(ctx)
	all := joinTexts(fake.take())
	assertContains(t, "mail push", all,
		"✉️ Mejl nije poslat: potvrda registracije za pera@zeta.rs, 14.10.2026 11:00. greška: dial tcp: i/o timeout.",
		"✉️ Mejl nije poslat: reset lozinke za ana@prva.rs, 14.10.2026 10:00. dostignut je dnevni limit mejlova.")
	if c := strings.Count(all, "Mejl nije poslat"); c != 4 {
		t.Errorf("mail pushes = %d, want two per operator (4):\n%s", c, all)
	}
	for _, unwanted := range []string{"ok@zeta.rs", "staro@omega.rs"} {
		if strings.Contains(all, unwanted) {
			t.Errorf("unexpected %q in:\n%s", unwanted, all)
		}
	}

	n.Tick(ctx)
	if again := joinTexts(fake.take()); strings.Contains(again, "Mejl nije poslat") {
		t.Errorf("unsent mail reported twice: %s", again)
	}
}
