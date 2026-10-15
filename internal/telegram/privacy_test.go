package telegram

import (
	"context"
	"strings"
	"testing"

	"shengen/internal/clock"
	"shengen/internal/store"
)

// linkedDriver gives a driver linked on chat 600 with one stay, and an owner
// linked on chat 444.
func linkedDriver(t *testing.T) (*Bot, *store.Tenant, *store.Driver) {
	t.Helper()
	bot, tn, d := planerSetup(t)
	ctx := context.Background()
	if err := bot.Store.SetDriverTelegramChatID(ctx, tn.ID, d.ID, 600); err != nil {
		t.Fatal(err)
	}
	addStay(t, bot, tn, d, 20, intp(15))
	addPlan(t, bot, tn, d, 5, 9, "Lion")
	return bot, tn, d
}

func TestMyData_ListsWhatIsStored(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, tn, _ := linkedDriver(t)

	postWebhook(t, bot, testSecret, textUpdate(600, "/moji_podaci"))
	texts := fake.texts()
	if len(texts) != 1 {
		t.Fatalf("replies = %v", texts)
	}
	entry := clock.Today().AddDate(0, 0, -20).Format("02.01.2006")
	for _, want := range []string{tn.Name, "Marko Planić", "Boravci u Šengenu (1)", entry, "Planirane ture (1)", "Lion", "obrati se firmi"} {
		if !strings.Contains(texts[0], want) {
			t.Errorf("/moji_podaci lacks %q:\n%s", want, texts[0])
		}
	}
}

func TestForgetMe_UnlinksAndTellsTheFirm(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, tn, d := linkedDriver(t)
	ctx := context.Background()

	postWebhook(t, bot, testSecret, textUpdate(600, "/obrisi_me"))
	if texts := fake.texts(); len(texts) != 1 || !strings.Contains(texts[0], "prekinemo vezu") {
		t.Fatalf("expected a confirmation question, got %v", texts)
	}
	if got, _ := bot.Store.GetDriver(ctx, tn.ID, d.ID); got.TelegramChatID == nil {
		t.Fatal("unlinked before confirming")
	}

	postWebhook(t, bot, testSecret, callbackUpdate(600, 600, cbUnlinkCancel))
	if got, _ := bot.Store.GetDriver(ctx, tn.ID, d.ID); got.TelegramChatID == nil {
		t.Fatal("unlinked after cancelling")
	}

	fake.reset()
	postWebhook(t, bot, testSecret, callbackUpdate(600, 600, cbUnlinkConfirm))
	got, _ := bot.Store.GetDriver(ctx, tn.ID, d.ID)
	if got.TelegramChatID != nil {
		t.Error("driver still linked after confirming")
	}
	if len(mustStays(t, bot, tn.ID, d.ID)) != 1 {
		t.Error("the driver's records were deleted; that is the firm's decision")
	}
	toDriver, toOwner := fake.textsTo(600), fake.textsTo(444)
	if len(toDriver) == 0 || !strings.Contains(toDriver[len(toDriver)-1], "Veza sa botom je prekinuta") {
		t.Errorf("driver got %v", toDriver)
	}
	if len(toOwner) != 1 || !strings.Contains(toOwner[0], "Marko Planić") || !strings.Contains(toOwner[0], "Trajno obriši") {
		t.Errorf("owner got %v, want a notice about the request", toOwner)
	}
	// The chat is no longer the driver's: his button presses are not
	// recorded any more.
	fake.reset()
	postWebhook(t, bot, testSecret, textUpdate(600, BtnEnteredSchengen))
	if len(mustStays(t, bot, tn.ID, d.ID)) != 1 {
		t.Error("an unlinked chat still records stays")
	}
}

func mustStays(t *testing.T, bot *Bot, tenantID, driverID int64) []store.Stay {
	t.Helper()
	stays, err := bot.Store.ListStaysByDriver(context.Background(), tenantID, driverID)
	if err != nil {
		t.Fatal(err)
	}
	return stays
}
