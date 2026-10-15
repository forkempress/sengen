package telegram

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"shengen/internal/legal"
	"shengen/internal/store"
)

// The driver's privacy in the bot: the notice he confirms after linking,
// /moji_podaci and /obrisi_me. The firm is the controller of his data; the
// bot speaks on its behalf and sends him to it for corrections and
// deletion.

const (
	cbPrivacyAck    = "privacy_ack"
	cbUnlinkConfirm = "unlink_confirm"
	cbUnlinkCancel  = "unlink_cancel"
)

// PrivacyNotice is the short notice a driver confirms after linking (and
// that the daily job re-sends, at most weekly, until he does).
func PrivacyNotice(firm string) string {
	return "ℹ️ Tvoji podaci\n\n" +
		firm + " vodi evidenciju tvojih dana u Šengenu (pravilo 90/180), da ne prekoračiš dozvoljeni boravak.\n\n" +
		"Čuva se: ime, vozilo, datumi ulaska i izlaska iz Šengena (i privatna putovanja, jer se i ona broje), planirane ture i tvoj Telegram nalog. Lokacija se ne prati.\n" +
		"Vide: vlasnik i dispečeri firme " + firm + ". Sistem tehnički održava Dani90, po nalogu firme; poruke idu preko Telegrama.\n\n" +
		"Za uvid, ispravku ili brisanje obrati se firmi " + firm + ". U botu: /moji_podaci — šta se čuva o tebi, /obrisi_me — prekid veze sa botom.\n" +
		"Više: " + legal.PublicBaseURL + "/privatnost"
}

func PrivacyAckKeyboard() InlineKeyboardMarkup {
	return InlineKeyboard(InlineRow(InlineButton("✅ Razumem, nastavi", cbPrivacyAck)))
}

// sendPrivacyNotice asks the driver to confirm the notice.
func (b *Bot) sendPrivacyNotice(ctx context.Context, client *Client, t *store.Tenant, chatID int64) {
	client.SendMessage(ctx, chatID, PrivacyNotice(t.Name), PrivacyAckKeyboard())
}

func (b *Bot) handlePrivacyAck(ctx context.Context, client *Client, t *store.Tenant, chatID int64, driver *store.Driver, callbackID string) {
	if err := b.Store.SetDriverPrivacyAck(ctx, t.ID, driver.ID); err != nil {
		log.Printf("telegram: privacy ack for driver %d: %v", driver.ID, err)
		client.AnswerCallbackQuery(ctx, callbackID, tryAgainMsg)
		return
	}
	client.AnswerCallbackQuery(ctx, callbackID, "Hvala.")
	client.SendMessage(ctx, chatID, "Zdravo, "+driver.Name+"! Povezan si sa botom.\n\n"+driverHelpText, DriverKeyboard())
}

// cmdMyData answers /moji_podaci: everything stored about the driver.
func (b *Bot) cmdMyData(ctx context.Context, client *Client, t *store.Tenant, chatID int64, driver *store.Driver) {
	ex, err := b.Store.ExportDriver(ctx, t.ID, driver.ID)
	if err != nil {
		client.SendMessage(ctx, chatID, tryAgainMsg, nil)
		return
	}
	client.SendMessage(ctx, chatID, formatMyData(t.Name, ex), nil)
}

// maxStaysListed keeps /moji_podaci under Telegram's 4096-character limit.
const maxStaysListed = 40

func formatMyData(firm string, ex *store.DriverExport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "📋 Šta se čuva o tebi\n\nFirma (rukovalac podacima): %s\nIme: %s\n", firm, ex.Driver.Name)
	if ex.Driver.VehicleLabel != "" {
		fmt.Fprintf(&b, "Vozilo: %s\n", ex.Driver.VehicleLabel)
	}
	b.WriteString("Telegram: povezan (ovaj nalog)\n")
	if ex.PrivacyAckAt != nil {
		fmt.Fprintf(&b, "Obaveštenje o podacima potvrđeno: %s\n", ex.PrivacyAckAt.In(time.Local).Format("02.01.2006"))
	}
	fmt.Fprintf(&b, "Unet u evidenciju: %s\n", ex.CreatedAt.Format("02.01.2006"))

	fmt.Fprintf(&b, "\nBoravci u Šengenu (%d):\n", len(ex.Stays))
	stays := ex.Stays
	if len(stays) > maxStaysListed {
		fmt.Fprintf(&b, "(prikazano poslednjih %d)\n", maxStaysListed)
		stays = stays[len(stays)-maxStaysListed:]
	}
	for _, st := range stays {
		exit := "u toku"
		if st.Exit != nil {
			exit = st.Exit.Format("02.01.2006")
		}
		typ := "posao"
		if st.Type == "private" {
			typ = "privatno"
		}
		fmt.Fprintf(&b, "• %s – %s, %s", st.Entry.Format("02.01.2006"), exit, typ)
		if st.Note != "" {
			b.WriteString(", " + st.Note)
		}
		b.WriteString("\n")
	}
	if len(ex.Plans) > 0 {
		fmt.Fprintf(&b, "\nPlanirane ture (%d):\n", len(ex.Plans))
		for _, p := range ex.Plans {
			fmt.Fprintf(&b, "• %s – %s", p.From.Format("02.01.2006"), p.To.Format("02.01.2006"))
			if p.Note != "" {
				b.WriteString(", " + p.Note)
			}
			b.WriteString("\n")
		}
	}
	fmt.Fprintf(&b, "\nZa ispravku, izvoz ili brisanje obrati se firmi %s. Prekid veze sa botom: /obrisi_me.", firm)
	return b.String()
}

// cmdForgetMe answers /obrisi_me with a confirmation.
func (b *Bot) cmdForgetMe(ctx context.Context, client *Client, t *store.Tenant, chatID int64) {
	client.SendMessage(ctx, chatID,
		"Da prekinemo vezu ovog Telegram naloga sa botom firme "+t.Name+"?\n\n"+
			"Bot ti više neće slati poruke niti primati dugmad. Firma će biti obaveštena da tražiš brisanje svojih podataka; "+
			"evidenciju vodi i briše firma, kao rukovalac podacima.",
		InlineKeyboard(InlineRow(InlineButton("Da, prekini vezu", cbUnlinkConfirm), InlineButton("Otkaži", cbUnlinkCancel))))
}

func (b *Bot) handleUnlinkConfirm(ctx context.Context, client *Client, t *store.Tenant, chatID int64, driver *store.Driver, callbackID string) {
	if err := b.Store.UnlinkDriverTelegram(ctx, t.ID, driver.ID); err != nil {
		log.Printf("telegram: unlink driver %d: %v", driver.ID, err)
		client.AnswerCallbackQuery(ctx, callbackID, tryAgainMsg)
		return
	}
	b.clearPending(chatID)
	if err := b.Store.RecordAudit(ctx, t.ID, "telegram_driver", &driver.ID, "driver", driver.ID, "telegram_unlink_self", nil, nil); err != nil {
		log.Printf("telegram: audit unlink driver %d: %v", driver.ID, err)
	}
	client.AnswerCallbackQuery(ctx, callbackID, "Veza je prekinuta.")
	client.SendMessage(ctx, chatID,
		"Veza sa botom je prekinuta. Firma "+t.Name+" je obaveštena. Za brisanje ili uvid u podatke obrati se firmi; pitanja o obradi: "+legal.PublicBaseURL+"/privatnost",
		ReplyKeyboardRemove{RemoveKeyboard: true})
	b.notifyFirm(ctx, client, t, fmt.Sprintf(
		"ℹ️ Vozač %s je u botu prekinuo vezu svog Telegram naloga (/obrisi_me) i traži brisanje svojih podataka. "+
			"Kao rukovalac, odlučite o tome: u aplikaciji Vozači → %s → „Trajno obriši“, ili ga samo deaktivirajte ako podatke morate da čuvate.",
		driver.Name, driver.Name))
}

// notifyFirm tells the firm something it must act on: in its alert chat, or,
// when there is none, to every owner with a linked Telegram account.
func (b *Bot) notifyFirm(ctx context.Context, client *Client, t *store.Tenant, text string) {
	if settings, err := t.ParsedSettings(); err == nil && settings.TelegramAlertChatID != 0 {
		b.notifyDispatcher(ctx, client, t, text)
		return
	}
	users, err := b.Store.ListUsers(ctx, t.ID)
	if err != nil {
		log.Printf("telegram: firm notice for tenant %d: %v", t.ID, err)
		return
	}
	for _, u := range users {
		if u.Active && u.Role == "owner" && u.TelegramChatID != nil {
			if err := client.SendMessage(ctx, *u.TelegramChatID, text, nil); err != nil {
				log.Printf("telegram: firm notice to user %d: %v", u.ID, err)
			}
		}
	}
}
