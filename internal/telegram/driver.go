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
	switch text {
	case BtnExitedSerbia:
		b.driverExited(ctx, client, t, chatID, driver)
	case BtnReturnedSerbia:
		b.driverReturned(ctx, client, t, chatID, driver)
	case BtnMyDays:
		b.driverMyDays(ctx, client, t, chatID, driver)
	default:
		client.SendMessage(ctx, chatID, "Koristite dugmad ispod. Za uputstvo pritisnite „"+BtnHelp+"“.", DriverKeyboard())
	}
}

func (b *Bot) driverExited(ctx context.Context, client *Client, t *store.Tenant, chatID int64, driver *store.Driver) {
	open, err := b.Store.OpenStayForDriver(ctx, t.ID, driver.ID)
	if err == nil {
		// Already has an open stay — ask for confirmation instead of silently
		// creating a second one (the DB would reject it anyway).
		b.setPending(chatID, "reopen_confirm", nil)
		client.SendMessage(ctx, chatID,
			fmt.Sprintf("Već imaš evidentiran izlazak iz Srbije od %s. Ako je taj datum greška, da pomerimo izlazak na danas?", open.Entry.Format("02.01.2006")),
			InlineKeyboard(InlineRow(InlineButton("Da, ispravi", "reopen_confirm"), InlineButton("Otkaži", "reopen_cancel"))))
		return
	}

	today := clock.Today()
	stays, err := b.Store.ListStaysByDriver(ctx, t.ID, driver.ID)
	if err != nil {
		client.SendMessage(ctx, chatID, "Greška pri evidentiranju, pokušajte ponovo ili se javite dispečeru.", nil)
		return
	}
	// Back into Schengen on the same date he left it: today is already a
	// Schengen day, so continue that stay instead of opening a new one that
	// would overlap it on today's date.
	if n := len(stays); n > 0 && stays[n-1].Exit != nil && stays[n-1].Exit.Equal(today) {
		last := stays[n-1]
		if err := b.Store.UpdateStay(ctx, t.ID, last.ID, last.Entry, nil, last.Type, last.Note); err != nil {
			client.SendMessage(ctx, chatID, "Greška pri evidentiranju, pokušajte ponovo ili se javite dispečeru.", nil)
			return
		}
		b.Store.RecordAudit(ctx, t.ID, "telegram_driver", &driver.ID, "stay", last.ID, "reopen", nil, nil)
		b.replyExited(ctx, client, t, chatID, driver, today,
			fmt.Sprintf("Zabeleženo: ponovo u Šengenu od danas, nastavak boravka od %s.", last.Entry.Format("02.01.2006")),
			undoCallback(last.ID, "reopen"))
		return
	}

	st, err := b.Store.CreateStay(ctx, t.ID, driver.ID, today, nil, "work", "telegram_button", "", nil)
	if err != nil {
		client.SendMessage(ctx, chatID, "Greška pri evidentiranju, pokušajte ponovo ili se javite dispečeru.", nil)
		return
	}
	b.Store.RecordAudit(ctx, t.ID, "telegram_driver", &driver.ID, "stay", st.ID, "create", nil, nil)
	b.replyExited(ctx, client, t, chatID, driver, today,
		fmt.Sprintf("Zabeleženo: izlazak iz Srbije %s.", today.Format("02.01.2006")),
		undoCallback(st.ID, "create"))
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
	today := clock.Today()
	closed, err := b.Store.CloseOpenStay(ctx, t.ID, driver.ID, today)
	if errors.Is(err, store.ErrNotFound) {
		client.SendMessage(ctx, chatID, "Nemaš evidentiran izlazak iz Srbije trenutno.", nil)
		return
	}
	if err != nil {
		client.SendMessage(ctx, chatID, "Greška pri evidentiranju, pokušajte ponovo ili se javite dispečeru.", nil)
		return
	}
	b.Store.RecordAudit(ctx, t.ID, "telegram_driver", &driver.ID, "stay", closed.ID, "close", nil, nil)

	stays, _ := b.Store.ListStaysByDriver(ctx, t.ID, driver.ID)
	ds := status.Compute(*driver, stays, today)
	msg := fmt.Sprintf("Zabeleženo: povratak u Srbiju %s. Iskorišćeno %d/90, preostalo %d dana.", today.Format("02.01.2006"), ds.UsedDays, ds.Remaining)
	client.SendMessage(ctx, chatID, msg, InlineKeyboard(InlineRow(InlineButton("Poništi", undoCallback(closed.ID, "close")))))
}

func (b *Bot) driverMyDays(ctx context.Context, client *Client, t *store.Tenant, chatID int64, driver *store.Driver) {
	stays, err := b.Store.ListStaysByDriver(ctx, t.ID, driver.ID)
	if err != nil {
		client.SendMessage(ctx, chatID, "Greška, pokušajte ponovo.", nil)
		return
	}
	ds := status.Compute(*driver, stays, clock.Today())
	client.SendMessage(ctx, chatID, formatMyDays(ds), DriverKeyboard())
}

func formatMyDays(ds status.DriverStatus) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Iskorišćeno: %d/90\nPreostalo danas: %d\n", ds.UsedDays, ds.Remaining)
	if ds.InSchengen {
		fmt.Fprintf(&b, "Trenutno: van Srbije od %s\n", ds.CurrentSince.Format("02.01.2006"))
		if ds.OverBy > 0 {
			fmt.Fprintf(&b, "⛔ PREKORAČENO za %d dana — rok je bio %s. Odmah napusti Šengen i javi se dispečeru.\n",
				ds.OverBy, ds.MaxContinuousUntil.Format("02.01.2006"))
		} else if ds.MaxContinuousUntil != nil {
			fmt.Fprintf(&b, "Može da ostane do: %s\n", ds.MaxContinuousUntil.Format("02.01.2006"))
		}
	} else {
		fmt.Fprintf(&b, "Trenutno: u Srbiji\nNajduža tura sada: %d dana\n", ds.MaxTripLengthToday)
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

	switch data {
	case "reopen_confirm":
		b.handleReopenConfirm(ctx, client, t, chatID, driver, cq.ID)
	case "reopen_cancel":
		b.clearPending(chatID)
		client.AnswerCallbackQuery(ctx, cq.ID, "Otkazano.")
	case "reminder_still_in":
		client.AnswerCallbackQuery(ctx, cq.ID, "U redu, podsetićemo ponovo.")
	case "reminder_returned":
		client.AnswerCallbackQuery(ctx, cq.ID, "")
		b.driverReturned(ctx, client, t, chatID, driver)
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
			client.SendMessage(ctx, chatID, "Nije ispravljeno: "+verr.Msg+". Javite se dispečeru.", nil)
			return
		}
		client.AnswerCallbackQuery(ctx, callbackID, "Greška.")
		return
	}
	client.AnswerCallbackQuery(ctx, callbackID, "Ispravljeno.")
	client.SendMessage(ctx, chatID,
		fmt.Sprintf("Ispravljeno: izlazak iz Srbije je sada %s (bio je %s).", today.Format("02.01.2006"), open.Entry.Format("02.01.2006")),
		InlineKeyboard(InlineRow(InlineButton("Poništi", undoCallback(open.ID, entryUndoKind(open.Entry))))))
}
