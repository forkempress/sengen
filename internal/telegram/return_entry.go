package telegram

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"shengen/internal/calc"
	"shengen/internal/clock"
	"shengen/internal/status"
	"shengen/internal/store"
)

// A driver who forgot "Ušao u Šengen" and presses "Izašao iz Šengena" has
// no open stay to close. Answering only "nothing to close" would lose the
// whole trip, so the bot asks when he entered, records the closed stay and
// tells the dispatcher, who can check the date against the stamps.

// returnEntryChoices is how many days, counting back from the exit date,
// are offered as buttons; older dates are typed.
const returnEntryChoices = 8

var weekdayShort = [...]string{"ned", "pon", "uto", "sre", "čet", "pet", "sub"}

// askReturnEntry asks for the entry date of a trip that ends on exit.
func (b *Bot) askReturnEntry(ctx context.Context, client *Client, t *store.Tenant, chatID int64, driver *store.Driver, exit time.Time) {
	b.setPending(chatID, "return_entry", exit)

	var rows [][]InlineKeyboardButton
	var row []InlineKeyboardButton
	for i := 0; i < returnEntryChoices; i++ {
		d := exit.AddDate(0, 0, -i)
		label := weekdayShort[d.Weekday()] + " " + d.Format("02.01.")
		if d.Equal(clock.Today()) {
			label = "danas " + d.Format("02.01.")
		}
		row = append(row, InlineButton(label, returnEntryCallback(d, exit)))
		if len(row) == 4 {
			rows = append(rows, row)
			row = nil
		}
	}
	rows = append(rows, InlineRow(InlineButton("Otkaži", "retentry_cancel")))

	client.SendMessage(ctx, chatID,
		fmt.Sprintf("Nemaš evidentiran ulazak u Šengen, a izlazak je %s. Kog datuma si ušao u Šengen? Izaberi dan ili napiši datum (npr. %s).",
			exit.Format("02.01.2006"), exit.AddDate(0, 0, -12).Format("02.01")),
		InlineKeyboard(rows...))

	// Once per driver and day: pressing the button twice must not repeat it.
	if fresh, err := b.Store.TryRecordAlert(ctx, t.ID, driver.ID, "return_without_entry", exit.Format("2006-01-02")); err == nil && fresh {
		b.notifyDispatcher(ctx, client, t, fmt.Sprintf(
			"ℹ️ %s je javio izlazak iz Šengena %s, a ulazak nije bio evidentiran. Bot ga pita za datum ulaska.",
			driver.Name, exit.Format("02.01.2006")))
	}
}

func returnEntryCallback(entry, exit time.Time) string {
	return "retentry:" + entry.Format("2006-01-02") + ":" + exit.Format("2006-01-02")
}

// handleReturnEntryCallback records the trip with the entry date the driver
// picked. Both dates come back from Telegram, so they are checked again.
func (b *Bot) handleReturnEntryCallback(ctx context.Context, client *Client, t *store.Tenant, chatID int64, driver *store.Driver, callbackID, data string) {
	parts := strings.Split(data, ":")
	if len(parts) != 3 {
		client.AnswerCallbackQuery(ctx, callbackID, "")
		return
	}
	entry, err1 := time.Parse("2006-01-02", parts[1])
	exit, err2 := time.Parse("2006-01-02", parts[2])
	if err1 != nil || err2 != nil || !validReturnExit(exit) {
		client.AnswerCallbackQuery(ctx, callbackID, "Izbor je istekao, pritisni dugme ponovo.")
		return
	}
	if msg := checkReturnEntry(entry, exit); msg != "" {
		client.AnswerCallbackQuery(ctx, callbackID, msg)
		return
	}
	client.AnswerCallbackQuery(ctx, callbackID, "")
	b.recordReturnWithoutEntry(ctx, client, t, chatID, driver, entry, exit)
}

// handleReturnEntryText takes a typed entry date (dd.mm or dd.mm.gggg)
// while the question is open.
func (b *Bot) handleReturnEntryText(ctx context.Context, client *Client, t *store.Tenant, chatID int64, driver *store.Driver, exit time.Time, text string) {
	entry, err := parseQuickDate(text, exit)
	if err != nil {
		client.SendMessage(ctx, chatID, "Ne razumem datum. Napiši ga kao dd.mm, npr. "+exit.AddDate(0, 0, -12).Format("02.01")+", ili izaberi dan iznad.", nil)
		return
	}
	if msg := checkReturnEntry(entry, exit); msg != "" {
		client.SendMessage(ctx, chatID, msg+" Napiši drugi datum.", nil)
		return
	}
	b.recordReturnWithoutEntry(ctx, client, t, chatID, driver, entry, exit)
}

// validReturnExit: the exit is today, or the date picked around midnight.
func validReturnExit(exit time.Time) bool {
	return !exit.Before(clock.Today().AddDate(0, 0, -1)) && !exit.After(clock.LatestToday())
}

// checkReturnEntry returns why entry cannot start a trip that ends on
// exit, or "" when it can. A trip longer than the 180-day window cannot
// be a forgotten button and needs the dispatcher.
func checkReturnEntry(entry, exit time.Time) string {
	if entry.After(exit) {
		return "Ulazak ne može biti posle izlaska (" + exit.Format("02.01.2006") + ")."
	}
	if entry.Before(exit.AddDate(0, 0, -(calc.WindowDays - 1))) {
		return "Datum je stariji od 180 dana. Javi se dispečeru."
	}
	return ""
}

func (b *Bot) recordReturnWithoutEntry(ctx context.Context, client *Client, t *store.Tenant, chatID int64, driver *store.Driver, entry, exit time.Time) {
	if _, err := b.Store.OpenStayForDriver(ctx, t.ID, driver.ID); err == nil {
		// "Ušao u Šengen" was pressed in the meantime; that stay is the
		// one to close.
		b.clearPending(chatID)
		client.SendMessage(ctx, chatID, "U međuvremenu je evidentiran ulazak u Šengen. Pritisni „"+BtnExitedSchengen+"“ ponovo.", DriverKeyboard())
		return
	}

	var st *store.Stay
	err := b.Store.WithTx(ctx, func(tx *store.Store) error {
		var err error
		st, err = tx.CreateStay(ctx, t.ID, driver.ID, entry, &exit, "work", "telegram_button", "ulazak naveden naknadno, uz izlazak", nil)
		if err != nil {
			return err
		}
		return tx.RecordAudit(ctx, t.ID, "telegram_driver", &driver.ID, "stay", st.ID, "create", nil, nil)
	})
	var verr *store.ValidationError
	if errors.As(err, &verr) {
		// Usually a clash with a stay already on record. The pending
		// question stays open so the driver can type another date.
		client.SendMessage(ctx, chatID, "Nije zabeleženo: "+verr.Msg+". Napiši drugi datum ili se javi dispečeru.", nil)
		return
	}
	if err != nil {
		log.Printf("telegram: stay without entry for driver %d: %v", driver.ID, err)
		client.SendMessage(ctx, chatID, driverErrorMsg, DriverKeyboard())
		return
	}
	b.clearPending(chatID)

	stays, _ := b.Store.ListStaysByDriver(ctx, t.ID, driver.ID)
	ds := status.Compute(*driver, stays, exit)
	client.SendMessage(ctx, chatID,
		fmt.Sprintf("Zabeleženo: boravak u Šengenu od %s do %s. Iskorišćeno %d/90, preostalo %d dana. Dispečer je obavešten.",
			entry.Format("02.01.2006"), exit.Format("02.01.2006"), ds.UsedDays, ds.Remaining),
		InlineKeyboard(InlineRow(InlineButton("Poništi", undoCallback(st.ID, "create")))))
	b.notifyDispatcher(ctx, client, t, fmt.Sprintf(
		"ℹ️ %s: upisan boravak u Šengenu %s–%s. Ulazak nije bio pritisnut, datum ulaska je vozač naveo naknadno — proverite ga.",
		driver.Name, entry.Format("02.01.2006"), exit.Format("02.01.2006")))
}

// notifyDispatcher posts to the firm's alert chat, when one is set.
func (b *Bot) notifyDispatcher(ctx context.Context, client *Client, t *store.Tenant, text string) {
	settings, err := t.ParsedSettings()
	if err != nil || settings.TelegramAlertChatID == 0 {
		return
	}
	if err := client.SendMessage(ctx, settings.TelegramAlertChatID, text, nil); err != nil {
		log.Printf("telegram: dispatcher notice for tenant %d: %v", t.ID, err)
	}
}
