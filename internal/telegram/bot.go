package telegram

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"shengen/internal/store"
	"shengen/internal/tenant"
)

// Bot wires the Telegram webhook to the application's store. Each tenant
// runs its own @BotFather bot (its token lives in tenant.Settings), so every
// request builds its own Client from that tenant's stored credentials —
// there is no single global bot.
type Bot struct {
	Store *store.Store

	mu      sync.Mutex
	pending map[int64]pendingAction // chat_id -> in-progress multi-step flow (import preview, quick-entry draft, reopen confirm)
}

func New(st *store.Store) *Bot {
	return &Bot{Store: st, pending: make(map[int64]pendingAction)}
}

type pendingAction struct {
	Kind    string
	Data    any
	Expires time.Time
}

const pendingTTL = 10 * time.Minute

func (b *Bot) setPending(chatID int64, kind string, data any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pending[chatID] = pendingAction{Kind: kind, Data: data, Expires: time.Now().Add(pendingTTL)}
}

func (b *Bot) getPending(chatID int64) (pendingAction, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	p, ok := b.pending[chatID]
	if !ok {
		return pendingAction{}, false
	}
	if time.Now().After(p.Expires) {
		delete(b.pending, chatID)
		return pendingAction{}, false
	}
	return p, true
}

func (b *Bot) clearPending(chatID int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.pending, chatID)
}

// HandleWebhook is mounted at POST /telegram/webhook, reached through the
// same host->tenant resolution as every other route, since each tenant's
// bot webhook points at that tenant's own subdomain.
func (b *Bot) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	t, ok := tenant.FromContext(r.Context())
	if !ok {
		http.Error(w, "unknown tenant", http.StatusNotFound)
		return
	}
	settings, err := t.ParsedSettings()
	if err != nil || settings.TelegramBotToken == "" || settings.TelegramWebhookSecret == "" {
		http.Error(w, "bot not configured", http.StatusNotFound)
		return
	}
	// Constant-time comparison: a plain != leaks, through response timing, how
	// many leading bytes of a guessed secret are correct.
	presented := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
	if subtle.ConstantTimeCompare([]byte(presented), []byte(settings.TelegramWebhookSecret)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var upd Update
	if err := json.NewDecoder(r.Body).Decode(&upd); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	client := NewClient(settings.TelegramBotToken)
	// Telegram expects a fast 200 regardless of outcome (retries otherwise);
	// errors are logged, not surfaced to the HTTP response.
	b.dispatch(r.Context(), client, t, settings, upd)
	w.WriteHeader(http.StatusOK)
}

func (b *Bot) dispatch(ctx context.Context, client *Client, t *store.Tenant, settings store.TenantSettings, upd Update) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("telegram: panic handling update %d: %v", upd.UpdateID, rec)
		}
	}()

	switch {
	case upd.CallbackQuery != nil:
		b.handleCallback(ctx, client, t, settings, upd.CallbackQuery)
	case upd.Message != nil:
		b.handleMessage(ctx, client, t, settings, upd.Message)
	case upd.EditedMessage != nil && upd.EditedMessage.Location != nil:
		// Live-location geofencing is a later phase; ignore for now.
	}
}

func (b *Bot) handleMessage(ctx context.Context, client *Client, t *store.Tenant, settings store.TenantSettings, msg *Message) {
	chatID := msg.Chat.ID
	text := strings.TrimSpace(msg.Text)

	if strings.HasPrefix(text, "/start") {
		b.handleStart(ctx, client, t, chatID, text)
		return
	}

	if driver, err := b.Store.GetDriverByTelegramChat(ctx, t.ID, chatID); err == nil {
		b.handleDriverMessage(ctx, client, t, chatID, driver, text)
		return
	}
	if user, err := b.Store.GetUserByTelegramChat(ctx, t.ID, chatID); err == nil {
		b.handleAdminMessage(ctx, client, t, settings, chatID, user, msg)
		return
	}

	client.SendMessage(ctx, chatID, "Nepoznat broj. Javite se dispečeru da vas poveže sa nalogom.", nil)
}

func (b *Bot) handleCallback(ctx context.Context, client *Client, t *store.Tenant, settings store.TenantSettings, cq *CallbackQuery) {
	chatID := cq.Message.Chat.ID

	if driver, err := b.Store.GetDriverByTelegramChat(ctx, t.ID, chatID); err == nil {
		b.handleDriverCallback(ctx, client, t, chatID, driver, cq)
		return
	}
	if user, err := b.Store.GetUserByTelegramChat(ctx, t.ID, chatID); err == nil {
		b.handleAdminCallback(ctx, client, t, settings, chatID, user, cq)
		return
	}
	client.AnswerCallbackQuery(ctx, cq.ID, "Nepoznat nalog.")
}

func (b *Bot) handleStart(ctx context.Context, client *Client, t *store.Tenant, chatID int64, text string) {
	parts := strings.Fields(text)
	if len(parts) < 2 {
		client.SendMessage(ctx, chatID, "Dobrodošli. Zatražite link za povezivanje od dispečera.", nil)
		return
	}
	token := parts[1]
	lt, err := b.Store.ConsumeLinkToken(ctx, token)
	if err != nil {
		client.SendMessage(ctx, chatID, "Link nije važeći ili je istekao. Zatražite novi od dispečera.", nil)
		return
	}
	if lt.TenantID != t.ID {
		client.SendMessage(ctx, chatID, "Link nije važeći za ovaj nalog.", nil)
		return
	}

	switch {
	case lt.DriverID != nil:
		if err := b.Store.SetDriverTelegramChatID(ctx, t.ID, *lt.DriverID, chatID); err != nil {
			log.Printf("telegram: link driver: %v", err)
			client.SendMessage(ctx, chatID, "Greška pri povezivanju, pokušajte ponovo.", nil)
			return
		}
		driver, _ := b.Store.GetDriver(ctx, t.ID, *lt.DriverID)
		name := "vozač"
		if driver != nil {
			name = driver.Name
		}
		client.SendMessage(ctx, chatID, "Zdravo, "+name+"! Povezani ste. Koristite dugmad ispod za prijavu izlaska/povratka iz Srbije.", DriverKeyboard())
	case lt.UserID != nil:
		if err := b.Store.SetUserTelegramChatID(ctx, t.ID, *lt.UserID, chatID); err != nil {
			log.Printf("telegram: link user: %v", err)
			client.SendMessage(ctx, chatID, "Greška pri povezivanju, pokušajte ponovo.", nil)
			return
		}
		client.SendMessage(ctx, chatID,
			"Povezani ste kao administrator. Dostupne komande:\n"+
				"/pregled — semafor svih vozača\n"+
				"/vozac <ime> — detalji vozača\n"+
				"/dodaj <ime> — dodaj vozača\n"+
				"/link <ime> — link za povezivanje vozača\n"+
				"/ispravi — poslednje izmene\n"+
				"/sablon — prazan CSV šablon\n"+
				"Možete i poslati CSV fajl, ili tekst poput: Marko 22.04-29.04, 06.05-15.05 p", nil)
	}
}
