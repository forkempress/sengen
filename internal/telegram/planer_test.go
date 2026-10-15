package telegram

import (
	"context"
	"strings"
	"testing"
	"time"

	"shengen/internal/clock"
	"shengen/internal/store"
)

// planerSetup gives an owner linked on chat 444 and one driver whose history
// is set up by the caller.
func planerSetup(t *testing.T) (*Bot, *store.Tenant, *store.Driver) {
	t.Helper()
	bot, tn := newTestBot(t)
	ctx := context.Background()
	owner, err := bot.Store.CreateUser(ctx, tn.ID, "vlasnik@test.rs", "hash", "owner", "Vlasnik")
	if err != nil {
		t.Fatal(err)
	}
	if err := bot.Store.SetUserTelegramChatID(ctx, tn.ID, owner.ID, 444); err != nil {
		t.Fatal(err)
	}
	d, err := bot.Store.CreateDriver(ctx, tn.ID, "Marko Planić", "")
	if err != nil {
		t.Fatal(err)
	}
	return bot, tn, d
}

func addStay(t *testing.T, bot *Bot, tn *store.Tenant, d *store.Driver, entryAgo int, exitAgo *int) {
	t.Helper()
	today := clock.Today()
	var exit *time.Time
	if exitAgo != nil {
		e := today.AddDate(0, 0, -*exitAgo)
		exit = &e
	}
	if _, err := bot.Store.CreateStay(context.Background(), tn.ID, d.ID, today.AddDate(0, 0, -entryAgo), exit, "work", "manual", "", nil); err != nil {
		t.Fatal(err)
	}
}

func intp(n int) *int { return &n }

// A driver in Schengen right now, with almost no other history, must be able
// to go on a 20-day trip in six weeks. Before the fix his open stay ran on
// through the planned trip, every day was counted twice and the planner said
// he could not go.
func TestPlaner_DriverInSchengenIsNotCountedTwice(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, tn, d := planerSetup(t)
	addStay(t, bot, tn, d, 70, intp(61)) // 10 days
	addStay(t, bot, tn, d, 4, nil)       // in Schengen since 4 days ago

	start := clock.Today().AddDate(0, 0, 42).Format("02.01.2006")
	postWebhook(t, bot, testSecret, textUpdate(444, "/planer Marko "+start+" 20"))

	texts := fake.texts()
	if len(texts) != 1 {
		t.Fatalf("expected one reply, got %v", texts)
	}
	if !strings.Contains(texts[0], "MOŽE na turu") || strings.Contains(texts[0], "NE MOŽE") {
		t.Errorf("driver with 15 used days should be able to go: %q", texts[0])
	}
	if !strings.Contains(texts[0], "vraća danas") {
		t.Errorf("reply should say the plan assumes he returns today: %q", texts[0])
	}
}

func TestPlaner_DriverInSchengenCannotStartToday(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, tn, d := planerSetup(t)
	addStay(t, bot, tn, d, 4, nil)

	postWebhook(t, bot, testSecret, textUpdate(444, "/planer Marko "+clock.Today().Format("02.01.2006")+" 5"))

	texts := fake.texts()
	if len(texts) != 1 || !strings.Contains(texts[0], "trenutno u Šengenu") {
		t.Errorf("expected an explanation that he is still away, got %v", texts)
	}
}

func TestPlaner_ImpossibleTripSuggestsEarliestStart(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, tn, d := planerSetup(t)
	addStay(t, bot, tn, d, 85, intp(1)) // 85 days, just back

	postWebhook(t, bot, testSecret, textUpdate(444, "/planer Marko "+clock.Today().AddDate(0, 0, 1).Format("02.01.2006")+" 20"))

	texts := fake.texts()
	if len(texts) != 1 || !strings.Contains(texts[0], "NE MOŽE") || !strings.Contains(texts[0], "Najraniji polazak") {
		t.Errorf("expected a refusal with the earliest start date, got %v", texts)
	}
}

// Malformed input must get an answer, not crash the handler and leave the
// admin waiting in silence.
func TestPlaner_BadInputIsAnswered(t *testing.T) {
	for _, in := range []string{"/planer Marko 25 20", "/planer Marko 31.04 5", "/planer Marko 25.10 120"} {
		t.Run(in, func(t *testing.T) {
			fake := newFakeTelegram(t)
			bot, _, _ := planerSetup(t)
			postWebhook(t, bot, testSecret, textUpdate(444, in))
			texts := fake.texts()
			if len(texts) != 1 || strings.Contains(texts[0], "MOŽE") {
				t.Errorf("expected one error reply, got %v", texts)
			}
		})
	}
}

func TestDateParsing(t *testing.T) {
	today := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	day := func(s string) time.Time { t, _ := time.Parse("02.01.2006", s); return t }

	future := map[string]time.Time{
		"25.10":      day("25.10.2026"),
		"25.10.":     day("25.10.2026"), // trailing dot
		"01.03":      day("01.03.2027"), // already passed this year
		"14.10":      day("14.10.2026"), // today
		"05.01.27":   day("05.01.2027"),
		"05.01.2027": day("05.01.2027"),
	}
	for in, want := range future {
		got, err := resolveFutureDate(in, today)
		if err != nil || !got.Equal(want) {
			t.Errorf("resolveFutureDate(%q) = %v, %v; want %v", in, got, err, want)
		}
	}

	past := map[string]time.Time{
		"10.10":  day("10.10.2026"),
		"10.10.": day("10.10.2026"),
		"20.12":  day("20.12.2025"), // not yet reached this year
	}
	for in, want := range past {
		got, err := parseQuickDate(in, today)
		if err != nil || !got.Equal(want) {
			t.Errorf("parseQuickDate(%q) = %v, %v; want %v", in, got, err, want)
		}
	}

	for _, bad := range []string{"25", "", "31.04", "30.02.2027", "32.01", "10.13", "1.2.3.4", "aa.bb"} {
		if _, err := resolveFutureDate(bad, today); err == nil {
			t.Errorf("resolveFutureDate(%q) accepted an invalid date", bad)
		}
		if _, err := parseQuickDate(bad, today); err == nil {
			t.Errorf("parseQuickDate(%q) accepted an invalid date", bad)
		}
	}
}

func addPlan(t *testing.T, bot *Bot, tn *store.Tenant, d *store.Driver, fromIn, toIn int, note string) {
	t.Helper()
	today := clock.Today()
	if _, err := bot.Store.CreatePlannedStay(context.Background(), tn.ID, d.ID, today.AddDate(0, 0, fromIn), today.AddDate(0, 0, toIn), note, nil); err != nil {
		t.Fatal(err)
	}
}

func TestPlaner_DateRange(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, _, _ := planerSetup(t)
	today := clock.Today()
	from, to := today.AddDate(0, 0, 10), today.AddDate(0, 0, 29)

	postWebhook(t, bot, testSecret, textUpdate(444, "/planer Marko Planić "+from.Format("02.01.2006")+"-"+to.Format("02.01.2006")))
	texts := fake.texts()
	if len(texts) != 1 {
		t.Fatalf("expected one reply, got %v", texts)
	}
	for _, want := range []string{"MOŽE na turu", "(20 dana)", "iskorišćeno 20/90", "ostaje 70 dana", "sme da ostane najkasnije do"} {
		if !strings.Contains(texts[0], want) {
			t.Errorf("reply %q does not contain %q", texts[0], want)
		}
	}
}

// The trip is checked together with the saved plans, and the reply says
// when it collides with one.
func TestPlaner_CountsSavedPlans(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, tn, d := planerSetup(t)
	addPlan(t, bot, tn, d, 5, 60, "Minhen") // 56 days

	from := clock.Today().AddDate(0, 0, 61)
	postWebhook(t, bot, testSecret, textUpdate(444, "/planer Marko "+from.Format("02.01.2006")+" 40"))
	texts := fake.texts()
	if len(texts) != 1 || !strings.Contains(texts[0], "NE MOŽE") || !strings.Contains(texts[0], "sačuvanim planovima (1)") {
		t.Errorf("40 days after a 56-day plan must not fit: %v", texts)
	}

	fake.reset()
	start := clock.Today().AddDate(0, 0, 50)
	postWebhook(t, bot, testSecret, textUpdate(444, "/planer Marko "+start.Format("02.01.")+" "+start.AddDate(0, 0, 3).Format("02.01.")))
	if texts := fake.texts(); len(texts) != 1 || !strings.Contains(texts[0], "Preklapa se sa sačuvanim planom") {
		t.Errorf("expected an overlap warning, got %v", texts)
	}
}

func TestPlaner_ListsSavedPlans(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, tn, d := planerSetup(t)

	postWebhook(t, bot, testSecret, textUpdate(444, "/planer Marko"))
	if texts := fake.texts(); len(texts) != 1 || !strings.Contains(texts[0], "nema sačuvanih planova") {
		t.Errorf("driver without plans: %v", texts)
	}

	addPlan(t, bot, tn, d, 5, 14, "Lion")
	addPlan(t, bot, tn, d, 20, 105, "")
	fake.reset()
	postWebhook(t, bot, testSecret, textUpdate(444, "/planer Marko"))
	texts := fake.texts()
	if len(texts) != 1 {
		t.Fatalf("expected one reply, got %v", texts)
	}
	for _, want := range []string{"Planovi — Marko Planić", "planiran", "posle ostaje 80 dana", "Lion", "prelazi limit"} {
		if !strings.Contains(texts[0], want) {
			t.Errorf("reply %q does not contain %q", texts[0], want)
		}
	}
}

func TestPlaner_ArgumentForms(t *testing.T) {
	today := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	day := func(s string) time.Time { t, _ := time.Parse("02.01.2006", s); return t }
	for _, tc := range []struct {
		in, name string
		from, to time.Time
		trip     bool
	}{
		{"Marko 20.12-05.01", "Marko", day("20.12.2026"), day("05.01.2027"), true},
		{"Marko Planić 01.11 10.11", "Marko Planić", day("01.11.2026"), day("10.11.2026"), true},
		{"Marko 01.11 10", "Marko", day("01.11.2026"), day("10.11.2026"), true},
		{"Marko Planić", "Marko Planić", time.Time{}, time.Time{}, false},
	} {
		a := parsePlanerArgs(tc.in)
		if a.name != tc.name || a.hasTrip != tc.trip {
			t.Errorf("%q: name %q trip %v", tc.in, a.name, a.hasTrip)
			continue
		}
		if !tc.trip {
			continue
		}
		from, to, msg := a.resolveTrip(today)
		if msg != "" || !from.Equal(tc.from) || !to.Equal(tc.to) {
			t.Errorf("%q: %v–%v %q, want %v–%v", tc.in, from, to, msg, tc.from, tc.to)
		}
	}
	for _, bad := range []string{"Marko 01.11 0", "Marko 01.11 91", "Marko 31.11-05.12", "Marko 01.11-31.11"} {
		if _, _, msg := parsePlanerArgs(bad).resolveTrip(today); msg == "" {
			t.Errorf("%q accepted", bad)
		}
	}
}

// /ispravi offers undo only for stays: an undo button on a plan or a user
// would treat its id as a stay's.
func TestIspravi_IgnoresPlans(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, tn, _ := planerSetup(t)
	bot.Store.RecordAudit(context.Background(), tn.ID, "user", nil, "planned_stay", 1, "create", nil, nil)

	postWebhook(t, bot, testSecret, textUpdate(444, "/ispravi"))
	if texts := fake.texts(); len(texts) != 1 || !strings.Contains(texts[0], "Nema skorašnjih izmena") {
		t.Errorf("plan audit entry shown by /ispravi: %v", texts)
	}
}

func (f *fakeTelegram) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = nil
}
