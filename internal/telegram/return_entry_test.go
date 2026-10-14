package telegram

import (
	"context"
	"slices"
	"strings"
	"testing"

	"shengen/internal/clock"
	"shengen/internal/store"
)

const testAlertChat int64 = 9000

// withAlertChat sets the firm's dispatcher alert chat and returns the
// reloaded firm.
func withAlertChat(t *testing.T, bot *Bot, tn *store.Tenant) *store.Tenant {
	t.Helper()
	ctx := context.Background()
	settings, _ := tn.ParsedSettings()
	settings.TelegramAlertChatID = testAlertChat
	if err := bot.Store.UpdateTenantSettings(ctx, tn.ID, settings); err != nil {
		t.Fatal(err)
	}
	tn, err := bot.Store.GetTenant(ctx, tn.ID)
	if err != nil {
		t.Fatal(err)
	}
	return tn
}

// textsTo returns the texts sent to chatID.
func (f *fakeTelegram) textsTo(chatID int64) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		id, _ := c.Body["chat_id"].(float64)
		if s, ok := c.Body["text"].(string); ok && int64(id) == chatID {
			out = append(out, s)
		}
	}
	return out
}

// QA-03: "Izašao iz Šengena" without a recorded entry used to answer only
// "nothing to close": the trip was lost and the dispatcher never knew. The
// bot now asks for the entry date, records the closed stay and tells the
// dispatcher.
func TestQA_ReturnWithoutExit(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, tn := newTestBot(t)
	tn = withAlertChat(t, bot, tn)
	ctx := context.Background()
	driver, _ := bot.Store.CreateDriver(ctx, tn.ID, "Marko", "")
	today := clock.Today()

	bot.handleDriverMessage(ctx, fake.client(), tn, 555, driver, BtnExitedSchengen)

	if stays, _ := bot.Store.ListStaysByDriver(ctx, tn.ID, driver.ID); len(stays) != 0 {
		t.Fatalf("stay recorded before the entry date was given: %+v", stays)
	}
	asked := fake.textsTo(555)
	if len(asked) != 1 || !strings.Contains(asked[0], "Kog datuma si ušao u Šengen?") {
		t.Fatalf("driver replies = %q, want the entry date question", asked)
	}
	choices := fake.lastCallbackData()
	pick := returnEntryCallback(today.AddDate(0, 0, -3), today)
	if !slices.Contains(choices, pick) || !slices.Contains(choices, returnEntryCallback(today, today)) {
		t.Fatalf("choices = %v, want the last days down to today", choices)
	}
	if got := fake.textsTo(testAlertChat); len(got) != 1 || !strings.Contains(got[0], "Marko") {
		t.Errorf("dispatcher got %q, want one notice about Marko", got)
	}

	bot.handleDriverCallback(ctx, fake.client(), tn, 555, driver, &CallbackQuery{ID: "cb", Data: pick})

	stays, _ := bot.Store.ListStaysByDriver(ctx, tn.ID, driver.ID)
	if len(stays) != 1 || !stays[0].Entry.Equal(today.AddDate(0, 0, -3)) || stays[0].Exit == nil || !stays[0].Exit.Equal(today) {
		t.Fatalf("stays = %+v, want one closed stay from 3 days ago to today", stays)
	}
	replies := fake.textsTo(555)
	if last := replies[len(replies)-1]; !strings.Contains(last, "Zabeleženo: boravak u Šengenu") || !strings.Contains(last, "4/90") {
		t.Errorf("confirmation = %q", last)
	}
	notices := fake.textsTo(testAlertChat)
	if len(notices) != 2 || !strings.Contains(notices[1], today.AddDate(0, 0, -3).Format("02.01.2006")) {
		t.Errorf("dispatcher notices = %q, want a second one with the recorded dates", notices)
	}

	// Pressing again: the trip is closed, so the question comes back, but
	// the dispatcher is not told twice on the same day.
	bot.handleDriverMessage(ctx, fake.client(), tn, 555, driver, BtnExitedSchengen)
	if got := fake.textsTo(testAlertChat); len(got) != 2 {
		t.Errorf("dispatcher notices after a second press = %d, want still 2", len(got))
	}
}

func TestReturnWithoutEntry_TypedDate(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, tn := newTestBot(t)
	ctx := context.Background()
	driver, _ := bot.Store.CreateDriver(ctx, tn.ID, "Marko", "")
	today := clock.Today()

	bot.handleDriverMessage(ctx, fake.client(), tn, 555, driver, BtnExitedSchengen)
	bot.handleDriverMessage(ctx, fake.client(), tn, 555, driver, "zaboravio sam")
	if replies := fake.texts(); !strings.Contains(replies[len(replies)-1], "Ne razumem datum") {
		t.Errorf("reply to a non-date = %q", replies[len(replies)-1])
	}
	bot.handleDriverMessage(ctx, fake.client(), tn, 555, driver, today.AddDate(0, 0, 2).Format("02.01.2006"))
	if replies := fake.texts(); !strings.Contains(replies[len(replies)-1], "posle izlaska") {
		t.Errorf("reply to a future date = %q", replies[len(replies)-1])
	}

	entry := today.AddDate(0, 0, -20)
	bot.handleDriverMessage(ctx, fake.client(), tn, 555, driver, entry.Format("02.01"))

	stays, _ := bot.Store.ListStaysByDriver(ctx, tn.ID, driver.ID)
	if len(stays) != 1 || !stays[0].Entry.Equal(entry) || stays[0].Exit == nil || !stays[0].Exit.Equal(today) {
		t.Fatalf("stays = %+v (replies %q), want %s–today", stays, fake.texts(), entry)
	}
	// The question is answered: a later message is not taken as a date.
	if _, ok := bot.getPending(555); ok {
		t.Error("the entry question is still pending after recording")
	}
}

// Another button while the question is open drops it, so "Moji dani" is
// not read as a date.
func TestReturnWithoutEntry_OtherButtonDropsTheQuestion(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, tn := newTestBot(t)
	ctx := context.Background()
	driver, _ := bot.Store.CreateDriver(ctx, tn.ID, "Marko", "")

	bot.handleDriverMessage(ctx, fake.client(), tn, 555, driver, BtnExitedSchengen)
	bot.handleDriverMessage(ctx, fake.client(), tn, 555, driver, BtnMyDays)
	if replies := fake.texts(); !strings.Contains(replies[len(replies)-1], "Iskorišćeno") {
		t.Errorf("reply to Moji dani = %q", replies[len(replies)-1])
	}
	if _, ok := bot.getPending(555); ok {
		t.Error("the entry question survived another button")
	}
}

// The dates come back from Telegram: an old exit date or an entry outside
// the 180-day window is refused.
func TestReturnWithoutEntry_ForgedCallbackIsRefused(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, tn := newTestBot(t)
	ctx := context.Background()
	driver, _ := bot.Store.CreateDriver(ctx, tn.ID, "Marko", "")
	today := clock.Today()

	for _, data := range []string{
		returnEntryCallback(today.AddDate(0, 0, -10), today.AddDate(0, 0, -5)),
		returnEntryCallback(today.AddDate(0, 0, -200), today),
		returnEntryCallback(today.AddDate(0, 0, 1), today),
	} {
		bot.handleDriverCallback(ctx, fake.client(), tn, 555, driver, &CallbackQuery{ID: "cb", Data: data})
	}
	if stays, _ := bot.Store.ListStaysByDriver(ctx, tn.ID, driver.ID); len(stays) != 0 {
		t.Errorf("forged choices recorded %+v", stays)
	}
}

// The week-old reminder answered after the exit was already recorded is
// not an exit without an entry.
func TestReminderReturned_AfterExitAlreadyRecorded(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, tn := newTestBot(t)
	ctx := context.Background()
	driver, _ := bot.Store.CreateDriver(ctx, tn.ID, "Marko", "")

	bot.handleDriverCallback(ctx, fake.client(), tn, 555, driver, &CallbackQuery{ID: "cb", Data: "reminder_returned"})
	if replies := fake.texts(); len(replies) != 1 || !strings.Contains(replies[0], "već evidentiran") {
		t.Errorf("replies = %q, want only the callback answer", replies)
	}
}
