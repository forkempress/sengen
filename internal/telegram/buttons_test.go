package telegram

import (
	"context"
	"strings"
	"testing"

	"shengen/internal/clock"
)

// replyKeyboard returns the reply-keyboard button texts of the call at i,
// or nil when that message carried no reply keyboard.
func (f *fakeTelegram) replyKeyboard(i int) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	rm, ok := f.calls[i].Body["reply_markup"].(map[string]any)
	if !ok {
		return nil
	}
	rows, ok := rm["keyboard"].([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, row := range rows {
		for _, btn := range row.([]any) {
			out = append(out, btn.(map[string]any)["text"].(string))
		}
	}
	return out
}

func TestDriverKeyboard_NamesTheSchengenBorder(t *testing.T) {
	row := DriverKeyboard().Keyboard[0]
	if row[0].Text != "▶ Ušao u Šengen" || row[1].Text != "◀ Izašao iz Šengena" {
		t.Errorf("first row = %q, %q", row[0].Text, row[1].Text)
	}
}

// Drivers linked before the rename still have the old keyboard, and a
// button press arrives as its text. The old texts must keep recording, and
// the reply must replace the keyboard with the new one.
func TestDriverButtons_OldNamesStillWork(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, tn := newTestBot(t)
	ctx := context.Background()
	driver, _ := bot.Store.CreateDriver(ctx, tn.ID, "Marko", "")

	bot.handleDriverMessage(ctx, fake.client(), tn, 555, driver, "Izašao iz Srbije")
	open, err := bot.Store.OpenStayForDriver(ctx, tn.ID, driver.ID)
	if err != nil {
		t.Fatalf("old exit button recorded nothing: %v (replies %q)", err, fake.texts())
	}
	if !open.Entry.Equal(clock.Today()) {
		t.Errorf("entry = %s, want today", open.Entry)
	}
	if kb := fake.replyKeyboard(0); len(kb) == 0 || kb[0] != BtnEnteredSchengen {
		t.Errorf("first reply keyboard = %v, want the new keyboard", kb)
	}
	if !strings.Contains(fake.texts()[0], "preimenovana") {
		t.Errorf("first reply = %q, want the rename notice", fake.texts()[0])
	}

	bot.handleDriverMessage(ctx, fake.client(), tn, 555, driver, "Vratio se u Srbiju")
	stays, _ := bot.Store.ListStaysByDriver(ctx, tn.ID, driver.ID)
	if len(stays) != 1 || stays[0].Exit == nil {
		t.Errorf("old return button did not close the stay: %+v (replies %q)", stays, fake.texts())
	}
}

func TestDriverButtons_NewNamesNeedNoNotice(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, tn := newTestBot(t)
	ctx := context.Background()
	driver, _ := bot.Store.CreateDriver(ctx, tn.ID, "Marko", "")

	bot.handleDriverMessage(ctx, fake.client(), tn, 555, driver, BtnEnteredSchengen)
	texts := fake.texts()
	if len(texts) != 1 || !strings.Contains(texts[0], "Zabeleženo: ulazak u Šengen") {
		t.Errorf("replies = %q, want one confirmation of entering Schengen", texts)
	}
	bot.handleDriverMessage(ctx, fake.client(), tn, 555, driver, BtnExitedSchengen)
	texts = fake.texts()
	if len(texts) != 2 || !strings.Contains(texts[1], "Zabeleženo: izlazak iz Šengena") {
		t.Errorf("replies = %q, want a confirmation of leaving Schengen", texts)
	}
}

// Driver replies say "ti"; the dispatcher is addressed with "Vi".
func TestDriverTexts_UseTi(t *testing.T) {
	for _, text := range []string{driverHelpText, keyboardRenamedMsg, driverErrorMsg} {
		for _, vi := range []string{"pokušajte", "javite", "Javite", "pritisnite", "Koristite", "ste "} {
			if strings.Contains(text, vi) {
				t.Errorf("driver text uses %q: %q", vi, text)
			}
		}
	}
}
