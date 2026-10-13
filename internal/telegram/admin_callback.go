package telegram

import (
	"context"
	"fmt"

	"shengen/internal/store"
)

func (b *Bot) handleAdminCallback(ctx context.Context, client *Client, t *store.Tenant, settings store.TenantSettings, chatID int64, user *store.User, cq *CallbackQuery) {
	data := cq.Data

	if stayID, kind, ok := parseUndoCallback(data); ok {
		msg, err := b.performUndo(ctx, t.ID, stayID, kind, "telegram_admin", &user.ID)
		if err != nil {
			client.AnswerCallbackQuery(ctx, cq.ID, "Greška pri poništavanju.")
			return
		}
		client.AnswerCallbackQuery(ctx, cq.ID, "Poništeno.")
		client.SendMessage(ctx, chatID, msg, nil)
		return
	}

	switch data {
	case "quickentry_confirm":
		b.confirmQuickEntryCommit(ctx, client, t, chatID, cq.ID)
	case "quickentry_cancel":
		b.clearPending(chatID)
		client.AnswerCallbackQuery(ctx, cq.ID, "Otkazano.")
	case "newdriver_confirm":
		b.confirmNewDriver(ctx, client, t, chatID, cq.ID)
	case "newdriver_cancel":
		b.clearPending(chatID)
		client.AnswerCallbackQuery(ctx, cq.ID, "Otkazano.")
	case "import_confirm":
		b.confirmImport(ctx, client, t, chatID, cq.ID)
	case "import_cancel":
		b.clearPending(chatID)
		client.AnswerCallbackQuery(ctx, cq.ID, "Otkazano.")
	default:
		client.AnswerCallbackQuery(ctx, cq.ID, "")
	}
}

func (b *Bot) confirmQuickEntryCommit(ctx context.Context, client *Client, t *store.Tenant, chatID int64, callbackID string) {
	p, ok := b.getPending(chatID)
	if !ok || p.Kind != "quickentry" {
		client.AnswerCallbackQuery(ctx, callbackID, "Zahtev je istekao.")
		return
	}
	data := p.Data.(quickEntryPending)
	b.clearPending(chatID)

	count := 0
	err := b.Store.WithTx(ctx, func(tx *store.Store) error {
		for _, r := range data.Ranges {
			typ := "work"
			if r.Private {
				typ = "private"
			}
			exit := r.Exit
			st, err := tx.CreateStay(ctx, t.ID, data.DriverID, r.Entry, &exit, typ, "telegram_admin", "", &data.UserID)
			if err != nil {
				return err
			}
			tx.RecordAudit(ctx, t.ID, "telegram_admin", &data.UserID, "stay", st.ID, "create", nil, nil)
			count++
		}
		return nil
	})
	if err != nil {
		client.AnswerCallbackQuery(ctx, callbackID, "Greška pri upisu.")
		client.SendMessage(ctx, chatID, "Uvoz neuspešan: "+err.Error(), nil)
		return
	}
	client.AnswerCallbackQuery(ctx, callbackID, "Sačuvano.")
	client.SendMessage(ctx, chatID, fmt.Sprintf("Sačuvano %d boravka za %s.", count, data.DriverName), nil)
}

func (b *Bot) confirmNewDriver(ctx context.Context, client *Client, t *store.Tenant, chatID int64, callbackID string) {
	p, ok := b.getPending(chatID)
	if !ok || p.Kind != "new_driver_quickentry" {
		client.AnswerCallbackQuery(ctx, callbackID, "Zahtev je istekao.")
		return
	}
	data := p.Data.(newDriverPending)
	b.clearPending(chatID)

	driver, err := b.Store.CreateDriver(ctx, t.ID, data.DriverName, "")
	if err != nil {
		client.AnswerCallbackQuery(ctx, callbackID, "Greška pri dodavanju vozača.")
		return
	}
	client.AnswerCallbackQuery(ctx, callbackID, "Vozač dodat.")

	user, err := b.Store.GetUser(ctx, t.ID, data.UserID)
	if err != nil {
		return
	}
	b.confirmQuickEntry(ctx, client, t, chatID, user, *driver, data.Ranges)
}

func (b *Bot) confirmImport(ctx context.Context, client *Client, t *store.Tenant, chatID int64, callbackID string) {
	p, ok := b.getPending(chatID)
	if !ok || p.Kind != "import" {
		client.AnswerCallbackQuery(ctx, callbackID, "Zahtev je istekao.")
		return
	}
	data := p.Data.(importPending)
	b.clearPending(chatID)

	count := 0
	err := b.Store.WithTx(ctx, func(tx *store.Store) error {
		for _, row := range data.Rows {
			st, err := tx.CreateStay(ctx, t.ID, row.DriverID, row.Entry, row.Exit, row.Type, "telegram_admin", row.Note, &data.UserID)
			if err != nil {
				return fmt.Errorf("red %d: %w", row.LineNum, err)
			}
			tx.RecordAudit(ctx, t.ID, "telegram_admin", &data.UserID, "stay", st.ID, "create", nil, nil)
			count++
		}
		return nil
	})
	if err != nil {
		client.AnswerCallbackQuery(ctx, callbackID, "Greška pri upisu.")
		client.SendMessage(ctx, chatID, "Uvoz neuspešan: "+err.Error(), nil)
		return
	}
	client.AnswerCallbackQuery(ctx, callbackID, "Uvezeno.")
	client.SendMessage(ctx, chatID, fmt.Sprintf("Uvezeno %d boravka.", count), nil)
}
