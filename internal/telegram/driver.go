package telegram

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"shengen/internal/clock"
	"shengen/internal/status"
	"shengen/internal/store"
)

func (b *Bot) handleDriverMessage(ctx context.Context, client *Client, t *store.Tenant, chatID int64, driver *store.Driver, text string) {
	if isHelpRequest(text) {
		client.SendMessage(ctx, chatID, driverHelpText, DriverKeyboard())
		return
	}
	btn, legacy := driverButton(text)
	if p, ok := b.getPending(chatID); ok && p.Kind == "return_entry" {
		if !isDriverButton(btn) {
			b.handleReturnEntryText(ctx, client, t, chatID, driver, p.Data.(time.Time), text)
			return
		}
		b.clearPending(chatID) // another button: the question is dropped
	}
	if legacy {
		// The confirmation that follows may carry an inline "Poništi"
		// button instead of a keyboard, so the new keyboard goes out first.
		client.SendMessage(ctx, chatID, keyboardRenamedMsg, DriverKeyboard())
	}
	switch btn {
	case BtnEnteredSchengen:
		b.driverExited(ctx, client, t, chatID, driver)
	case BtnExitedSchengen:
		b.driverReturned(ctx, client, t, chatID, driver)
	case BtnMyDays:
		b.driverMyDays(ctx, client, t, chatID, driver)
	default:
		client.SendMessage(ctx, chatID, "Koristi dugmad ispod. Za uputstvo pritisni „"+BtnHelp+"“.", DriverKeyboard())
	}
}

// keyboardRenamedMsg goes to a driver who pressed a button from the old
// keyboard, together with the new one.
const keyboardRenamedMsg = "Dugmad su preimenovana: „" + legacyBtnExitedSerbia + "“ je sada „" + BtnEnteredSchengen +
	"“, a „" + legacyBtnReturnedSerbia + "“ je sada „" + BtnExitedSchengen + "“. Pritiskaj ih na granici Šengena, ne Srbije."

// driverErrorMsg is the reply when recording a button press fails.
const driverErrorMsg = "Greška pri evidentiranju. Pokušaj ponovo ili se javi dispečeru."

func (b *Bot) driverExited(ctx context.Context, client *Client, t *store.Tenant, chatID int64, driver *store.Driver) {
	open, err := b.Store.OpenStayForDriver(ctx, t.ID, driver.ID)
	if err == nil {
		// Already has an open stay — ask for confirmation instead of silently
		// creating a second one (the DB would reject it anyway).
		b.setPending(chatID, "reopen_confirm", nil)
		client.SendMessage(ctx, chatID,
			fmt.Sprintf("Već imaš evidentiran ulazak u Šengen od %s. Ako je taj datum greška, da pomerimo ulazak na danas?", open.Entry.Format("02.01.2006")),
			InlineKeyboard(InlineRow(InlineButton("Da, ispravi", "reopen_confirm"), InlineButton("Otkaži", "reopen_cancel"))))
		return
	}
	if clock.NearMidnight() {
		b.askCrossingDate(ctx, client, chatID, "exit", "Kog datuma si ušao u Šengen (datum na pečatu)?")
		return
	}
	b.driverExitedOn(ctx, client, t, chatID, driver, clock.Today())
}

// driverExitedOn records entering Schengen on date: today, or the date the
// driver picked around midnight.
func (b *Bot) driverExitedOn(ctx context.Context, client *Client, t *store.Tenant, chatID int64, driver *store.Driver, date time.Time) {
	stays, err := b.Store.ListStaysByDriver(ctx, t.ID, driver.ID)
	if err != nil {
		client.SendMessage(ctx, chatID, driverErrorMsg, DriverKeyboard())
		return
	}
	// Back into Schengen on the same date he left it: that date is already a
	// Schengen day, so continue that stay instead of opening a new one that
	// would overlap it on that date.
	if n := len(stays); n > 0 && stays[n-1].Exit != nil && stays[n-1].Exit.Equal(date) {
		last := stays[n-1]
		if err := b.Store.UpdateStay(ctx, t.ID, last.ID, last.Entry, nil, last.Type, last.Note); err != nil {
			client.SendMessage(ctx, chatID, driverErrorMsg, DriverKeyboard())
			return
		}
		b.Store.RecordAudit(ctx, t.ID, "telegram_driver", &driver.ID, "stay", last.ID, "reopen", nil, nil)
		b.replyExited(ctx, client, t, chatID, driver, date,
			fmt.Sprintf("Zabeleženo: ponovo u Šengenu od %s, nastavak boravka od %s.", date.Format("02.01.2006"), last.Entry.Format("02.01.2006")),
			undoCallback(last.ID, "reopen"))
		return
	}

	st, err := b.Store.CreateStay(ctx, t.ID, driver.ID, date, nil, "work", "telegram_button", "", nil)
	if err != nil {
		client.SendMessage(ctx, chatID, driverErrorMsg, DriverKeyboard())
		return
	}
	b.Store.RecordAudit(ctx, t.ID, "telegram_driver", &driver.ID, "stay", st.ID, "create", nil, nil)
	b.replyExited(ctx, client, t, chatID, driver, date,
		fmt.Sprintf("Zabeleženo: ulazak u Šengen %s.", date.Format("02.01.2006")),
		undoCallback(st.ID, "create"))
}

// askCrossingDate offers the two dates a crossing around midnight can carry:
// before midnight Belgrade time an EET border already shows tomorrow; after
// midnight the driver may have crossed just before it.
func (b *Bot) askCrossingDate(ctx context.Context, client *Client, chatID int64, action, question string) {
	today := clock.Today()
	first, second := today.AddDate(0, 0, -1), today
	firstLabel, secondLabel := "juče", "danas"
	if clock.Now().Hour() >= 23 {
		first, second = today, today.AddDate(0, 0, 1)
		firstLabel, secondLabel = "danas", "sutra"
	}
	client.SendMessage(ctx, chatID, question, InlineKeyboard(InlineRow(
		InlineButton(firstLabel+" "+first.Format("02.01."), crossingDateCallback(action, first)),
		InlineButton(secondLabel+" "+second.Format("02.01."), crossingDateCallback(action, second)),
	)))
}

func crossingDateCallback(action string, date time.Time) string {
	return "date:" + action + ":" + date.Format("2006-01-02")
}

// handleCrossingDate records the crossing on the date the driver picked. The
// date comes back from Telegram, so it is checked again: only yesterday,
// today or the EET date are possible answers to the question above.
func (b *Bot) handleCrossingDate(ctx context.Context, client *Client, t *store.Tenant, chatID int64, driver *store.Driver, callbackID, data string) {
	parts := strings.Split(data, ":")
	if len(parts) != 3 {
		client.AnswerCallbackQuery(ctx, callbackID, "")
		return
	}
	date, err := time.Parse("2006-01-02", parts[2])
	if err != nil || date.Before(clock.Today().AddDate(0, 0, -1)) || date.After(clock.LatestToday()) {
		client.AnswerCallbackQuery(ctx, callbackID, "Izbor je istekao, pritisni dugme ponovo.")
		return
	}
	client.AnswerCallbackQuery(ctx, callbackID, "")
	switch parts[1] {
	case "exit":
		if _, err := b.Store.OpenStayForDriver(ctx, t.ID, driver.ID); err == nil {
			client.SendMessage(ctx, chatID, "Ulazak u Šengen je već evidentiran.", DriverKeyboard())
			return
		}
		b.driverExitedOn(ctx, client, t, chatID, driver, date)
	case "return":
		b.driverReturnedOn(ctx, client, t, chatID, driver, date)
	}
}

// replyExited confirms an entry into Schengen with how long the driver may
// stay, or a warning when he has no days left.
func (b *Bot) replyExited(ctx context.Context, client *Client, t *store.Tenant, chatID int64, driver *store.Driver, today time.Time, msg, undo string) {

	stays, _ := b.Store.ListStaysByDriver(ctx, t.ID, driver.ID)
	ds := status.Compute(*driver, stays, today)
	switch {
	case ds.OverBy > 0:
		// Entering with the allowance already used up: never phrase this
		// as a date he may stay until.
		msg += fmt.Sprintf(" ⛔ PAŽNJA: već si %d dana preko limita od 90 — ne smeš da ostaneš u Šengenu. Odmah se javi dispečeru.", ds.OverBy)
	case ds.MaxContinuousUntil != nil:
		msg += fmt.Sprintf(" Možeš ostati najduže do %s.", ds.MaxContinuousUntil.Format("02.01.2006"))
	}
	client.SendMessage(ctx, chatID, msg, InlineKeyboard(InlineRow(InlineButton("Poništi", undo))))
}

func (b *Bot) driverReturned(ctx context.Context, client *Client, t *store.Tenant, chatID int64, driver *store.Driver) {
	if clock.NearMidnight() {
		// Asked also without an open stay: the exit date is needed either
		// way, and driverReturnedOn then asks for the entry date.
		b.askCrossingDate(ctx, client, chatID, "return", "Kog datuma si izašao iz Šengena (datum na pečatu)?")
		return
	}
	b.driverReturnedOn(ctx, client, t, chatID, driver, clock.Today())
}

// driverReturnedOn closes the open stay on date: today, or the date the
// driver picked around midnight.
func (b *Bot) driverReturnedOn(ctx context.Context, client *Client, t *store.Tenant, chatID int64, driver *store.Driver, date time.Time) {
	closed, err := b.Store.CloseOpenStay(ctx, t.ID, driver.ID, date)
	if errors.Is(err, store.ErrNotFound) {
		b.askReturnEntry(ctx, client, t, chatID, driver, date)
		return
	}
	var verr *store.ValidationError
	if errors.As(err, &verr) {
		client.SendMessage(ctx, chatID, "Nije zabeleženo: "+verr.Msg+". Javi se dispečeru.", DriverKeyboard())
		return
	}
	if err != nil {
		client.SendMessage(ctx, chatID, driverErrorMsg, DriverKeyboard())
		return
	}
	b.Store.RecordAudit(ctx, t.ID, "telegram_driver", &driver.ID, "stay", closed.ID, "close", nil, nil)

	stays, _ := b.Store.ListStaysByDriver(ctx, t.ID, driver.ID)
	ds := status.Compute(*driver, stays, date)
	msg := fmt.Sprintf("Zabeleženo: izlazak iz Šengena %s. Iskorišćeno %d/90, preostalo %d dana.", date.Format("02.01.2006"), ds.UsedDays, ds.Remaining)
	client.SendMessage(ctx, chatID, msg, InlineKeyboard(InlineRow(InlineButton("Poništi", undoCallback(closed.ID, "close")))))
}

func (b *Bot) driverMyDays(ctx context.Context, client *Client, t *store.Tenant, chatID int64, driver *store.Driver) {
	stays, err := b.Store.ListStaysByDriver(ctx, t.ID, driver.ID)
	if err != nil {
		client.SendMessage(ctx, chatID, "Greška. Pokušaj ponovo za minut.", DriverKeyboard())
		return
	}
	ds := status.Compute(*driver, stays, clock.Today())
	client.SendMessage(ctx, chatID, formatMyDays(ds), DriverKeyboard())
}

// formatMyDays is the driver's "Moji dani" reply, addressed to him.
func formatMyDays(ds status.DriverStatus) string {
	return formatDays(ds, true)
}

// formatDriverDays is the same summary for the dispatcher's /vozac,
// talking about the driver.
func formatDriverDays(ds status.DriverStatus) string {
	return formatDays(ds, false)
}

func formatDays(ds status.DriverStatus, toDriver bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Iskorišćeno: %d/90\nPreostalo danas: %d\n", ds.UsedDays, ds.Remaining)
	if ds.InSchengen {
		fmt.Fprintf(&b, "Trenutno: u Šengenu od %s\n", ds.CurrentSince.Format("02.01.2006"))
		switch {
		case ds.OverBy > 0 && toDriver:
			fmt.Fprintf(&b, "⛔ PREKORAČENO za %d dana — rok je bio %s. Odmah napusti Šengen i javi se dispečeru.\n",
				ds.OverBy, ds.MaxContinuousUntil.Format("02.01.2006"))
		case ds.OverBy > 0:
			fmt.Fprintf(&b, "⛔ PREKORAČENO za %d dana — rok je bio %s. Mora odmah da napusti Šengen.\n",
				ds.OverBy, ds.MaxContinuousUntil.Format("02.01.2006"))
		case ds.MaxContinuousUntil != nil && toDriver:
			fmt.Fprintf(&b, "Možeš da ostaneš do: %s\n", ds.MaxContinuousUntil.Format("02.01.2006"))
		case ds.MaxContinuousUntil != nil:
			fmt.Fprintf(&b, "Može da ostane do: %s\n", ds.MaxContinuousUntil.Format("02.01.2006"))
		}
	} else {
		fmt.Fprintf(&b, "Trenutno: van Šengena\nNajduža tura sada: %d dana\n", ds.MaxTripLengthToday)
	}
	if ds.NextFreeUp != nil {
		fmt.Fprintf(&b, "Sledeće oslobađanje: %s (+%d)\n", ds.NextFreeUp.Date.Format("02.01.2006"), ds.NextFreeUp.DaysFreed)
	}
	return b.String()
}

// undoCallback / parseUndoCallback encode the 10-minute "Poništi" action
// directly in callback_data ("undo:<stayID>:<kind>") so it survives a
// process restart unlike the in-memory pending map — undo correctness
// matters more than the other multi-step flows.
func undoCallback(stayID int64, kind string) string {
	return fmt.Sprintf("undo:%d:%s", stayID, kind)
}

// entryUndoKind is the undo kind for "Da, ispravi": it carries the entry
// date to restore, since the stay itself no longer remembers it.
func entryUndoKind(oldEntry time.Time) string {
	return "entry-" + oldEntry.Format("2006-01-02")
}

func parseUndoCallback(data string) (stayID int64, kind string, ok bool) {
	parts := strings.Split(data, ":")
	if len(parts) != 3 || parts[0] != "undo" {
		return 0, "", false
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, "", false
	}
	return id, parts[2], true
}

const undoWindow = 10 * time.Minute

func (b *Bot) handleDriverCallback(ctx context.Context, client *Client, t *store.Tenant, chatID int64, driver *store.Driver, cq *CallbackQuery) {
	data := cq.Data

	if stayID, kind, ok := parseUndoCallback(data); ok {
		b.handleUndo(ctx, client, t, chatID, driver, cq.ID, stayID, kind)
		return
	}

	if strings.HasPrefix(data, "date:") {
		b.handleCrossingDate(ctx, client, t, chatID, driver, cq.ID, data)
		return
	}

	if strings.HasPrefix(data, "retentry:") {
		b.handleReturnEntryCallback(ctx, client, t, chatID, driver, cq.ID, data)
		return
	}

	switch data {
	case "reopen_confirm":
		b.handleReopenConfirm(ctx, client, t, chatID, driver, cq.ID)
	case "reopen_cancel":
		b.clearPending(chatID)
		client.AnswerCallbackQuery(ctx, cq.ID, "Otkazano.")
	case "reminder_still_in":
		client.AnswerCallbackQuery(ctx, cq.ID, "U redu, podsetićemo ponovo.")
	case "reminder_returned":
		// The reminder may be answered after the exit was already
		// recorded; that is not an exit without an entry.
		if _, err := b.Store.OpenStayForDriver(ctx, t.ID, driver.ID); errors.Is(err, store.ErrNotFound) {
			client.AnswerCallbackQuery(ctx, cq.ID, "Izlazak je već evidentiran.")
			return
		}
		client.AnswerCallbackQuery(ctx, cq.ID, "")
		b.driverReturned(ctx, client, t, chatID, driver)
	case "retentry_cancel":
		b.clearPending(chatID)
		client.AnswerCallbackQuery(ctx, cq.ID, "Otkazano.")
		client.SendMessage(ctx, chatID, "Ništa nije upisano. Javi dispečeru kad si bio u Šengenu.", DriverKeyboard())
	default:
		client.AnswerCallbackQuery(ctx, cq.ID, "")
	}
}

func (b *Bot) handleUndo(ctx context.Context, client *Client, t *store.Tenant, chatID int64, driver *store.Driver, callbackID string, stayID int64, kind string) {
	st, err := b.Store.GetStay(ctx, t.ID, stayID)
	if err != nil || st.DriverID != driver.ID {
		client.AnswerCallbackQuery(ctx, callbackID, "Unos više ne postoji.")
		return
	}

	var elapsed time.Duration
	switch kind {
	case "create":
		elapsed = time.Since(st.CreatedAt)
	default: // close, reopen, entry-…: the stay was changed, not created
		elapsed = time.Since(st.UpdatedAt)
	}
	if elapsed > undoWindow {
		client.AnswerCallbackQuery(ctx, callbackID, "Vreme za poništavanje je isteklo.")
		return
	}

	msg, err := b.performUndo(ctx, t.ID, stayID, kind, "telegram_driver", &driver.ID)
	if err != nil {
		client.AnswerCallbackQuery(ctx, callbackID, "Greška pri poništavanju.")
		return
	}
	client.AnswerCallbackQuery(ctx, callbackID, "Poništeno.")
	client.SendMessage(ctx, chatID, msg, DriverKeyboard())
}

func (b *Bot) handleReopenConfirm(ctx context.Context, client *Client, t *store.Tenant, chatID int64, driver *store.Driver, callbackID string) {
	if p, ok := b.getPending(chatID); !ok || p.Kind != "reopen_confirm" {
		client.AnswerCallbackQuery(ctx, callbackID, "Zahtev je istekao.")
		return
	}
	b.clearPending(chatID)

	open, err := b.Store.OpenStayForDriver(ctx, t.ID, driver.ID)
	if err != nil {
		client.AnswerCallbackQuery(ctx, callbackID, "Nema otvorenog unosa.")
		return
	}
	// The earlier entry date was wrong: move it to today. Never close the
	// old stay and open a new one — the new one would touch the old one on
	// today's date and be refused, leaving the driver with no open stay.
	today := clock.Today()
	err = b.Store.WithTx(ctx, func(tx *store.Store) error {
		if err := tx.UpdateStay(ctx, t.ID, open.ID, today, nil, open.Type, open.Note); err != nil {
			return err
		}
		return tx.RecordAudit(ctx, t.ID, "telegram_driver", &driver.ID, "stay", open.ID, "update", nil, nil)
	})
	if err != nil {
		var verr *store.ValidationError
		if errors.As(err, &verr) {
			client.AnswerCallbackQuery(ctx, callbackID, "Nije ispravljeno.")
			client.SendMessage(ctx, chatID, "Nije ispravljeno: "+verr.Msg+". Javi se dispečeru.", DriverKeyboard())
			return
		}
		client.AnswerCallbackQuery(ctx, callbackID, "Greška.")
		return
	}
	client.AnswerCallbackQuery(ctx, callbackID, "Ispravljeno.")
	client.SendMessage(ctx, chatID,
		fmt.Sprintf("Ispravljeno: ulazak u Šengen je sada %s (bio je %s).", today.Format("02.01.2006"), open.Entry.Format("02.01.2006")),
		InlineKeyboard(InlineRow(InlineButton("Poništi", undoCallback(open.ID, entryUndoKind(open.Entry))))))
}
