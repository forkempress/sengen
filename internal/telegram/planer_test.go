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
