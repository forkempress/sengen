package telegram

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"shengen/internal/store"
)

// Bot is the single Telegram bot shared by every firm. Its token and webhook
// secret come from the environment; which firm an update belongs to is
// decided per update from who sent it — a linked driver, a linked owner or
// dispatcher, or the one-time link token in /start — never from the request.
type Bot struct {
	Store *store.Store

	client        *Client // nil when no token is configured
	webhookSecret string
	username      string // without "@"; used to build t.me links

	mu      sync.Mutex
	pending map[int64]pendingAction // chat_id -> in-progress multi-step flow (import preview, quick-entry draft, reopen confirm)
}

type Config struct {
	Token         string // from @BotFather
	WebhookSecret string // echoed back by Telegram in X-Telegram-Bot-Api-Secret-Token
	Username      string // bot's @username, optional; enables one-tap t.me links
}

func New(st *store.Store, cfg Config) *Bot {
	b := &Bot{
		Store:         st,
		webhookSecret: cfg.WebhookSecret,
		username:      strings.TrimPrefix(strings.TrimSpace(cfg.Username), "@"),
		pending:       make(map[int64]pendingAction),
	}
	if cfg.Token != "" {
		b.client = NewClient(cfg.Token)
	}
	return b
}

// Configured reports whether the bot can receive and send messages.
func (b *Bot) Configured() bool { return b.client != nil && b.webhookSecret != "" }

// Client is the shared API client, or nil when no token is configured.
// Background jobs use it to send reminders and alerts for every firm.
func (b *Bot) Client() *Client { return b.client }

// StartLink returns the one-tap link that opens the bot and sends /start
// with the token, or "" when the bot's username isn't configured (people
// then type the /start command themselves).
func (b *Bot) StartLink(token string) string {
	if b.username == "" {
		return ""
	}
	return "https://t.me/" + b.username + "?start=" + token
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

// HandleWebhook is mounted at POST /telegram/webhook. It is not behind login:
// the shared secret Telegram echoes back is what proves the request came from
// Telegram, and the firm is worked out per update in dispatch.
func (b *Bot) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if !b.Configured() {
		http.Error(w, "bot not configured", http.StatusNotFound)
		return
	}
	// Constant-time comparison: a plain != leaks, through response timing, how
	// many leading bytes of a guessed secret are correct.
	presented := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
	if subtle.ConstantTimeCompare([]byte(presented), []byte(b.webhookSecret)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var upd Update
	if err := json.NewDecoder(r.Body).Decode(&upd); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Telegram expects a fast 200 regardless of outcome (retries otherwise);
	// errors are logged, not surfaced to the HTTP response.
	b.dispatch(r.Context(), b.client, upd)
	w.WriteHeader(http.StatusOK)
}

func (b *Bot) dispatch(ctx context.Context, client *Client, upd Update) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("telegram: panic handling update %d: %v", upd.UpdateID, rec)
		}
	}()

	switch {
	case upd.CallbackQuery != nil && upd.CallbackQuery.Message != nil:
		b.handleCallback(ctx, client, upd.CallbackQuery)
	case upd.Message != nil:
		b.handleMessage(ctx, client, upd.Message)
	case upd.EditedMessage != nil && upd.EditedMessage.Location != nil:
		// Live-location geofencing is a later phase; ignore for now.
	}
}

// tenantFor loads the firm a linked driver or user belongs to.
func (b *Bot) tenantFor(ctx context.Context, tenantID int64) (*store.Tenant, store.TenantSettings, error) {
	t, err := b.Store.GetTenant(ctx, tenantID)
	if err != nil {
		return nil, store.TenantSettings{}, err
	}
	settings, err := t.ParsedSettings()
	if err != nil {
		return nil, store.TenantSettings{}, err
	}
	return t, settings, nil
}

const tryAgainMsg = "Greška, pokušajte ponovo za minut."
const unknownChatMsg = "Nepoznat broj. Javite se dispečeru da vas poveže sa nalogom."

func (b *Bot) handleMessage(ctx context.Context, client *Client, msg *Message) {
	chatID := msg.Chat.ID
	text := strings.TrimSpace(msg.Text)

	if strings.HasPrefix(text, "/start") {
		b.handleStart(ctx, client, chatID, text)
		return
	}

	driver, err := b.Store.GetDriverByTelegramChat(ctx, chatID)
	if err == nil {
		t, _, err := b.tenantFor(ctx, driver.TenantID)
		if err != nil {
			log.Printf("telegram: tenant for driver %d: %v", driver.ID, err)
			client.SendMessage(ctx, chatID, tryAgainMsg, nil)
			return
		}
		b.handleDriverMessage(ctx, client, t, chatID, driver, text)
		return
	}
	if !errors.Is(err, store.ErrNotFound) {
		log.Printf("telegram: driver lookup for chat %d: %v", chatID, err)
		client.SendMessage(ctx, chatID, tryAgainMsg, nil)
		return
	}

	user, err := b.Store.GetUserByTelegramChat(ctx, chatID)
	if err == nil {
		t, settings, err := b.tenantFor(ctx, user.TenantID)
		if err != nil {
			log.Printf("telegram: tenant for user %d: %v", user.ID, err)
			client.SendMessage(ctx, chatID, tryAgainMsg, nil)
			return
		}
		b.handleAdminMessage(ctx, client, t, settings, chatID, user, msg)
		return
	}
	if !errors.Is(err, store.ErrNotFound) {
		log.Printf("telegram: user lookup for chat %d: %v", chatID, err)
		client.SendMessage(ctx, chatID, tryAgainMsg, nil)
		return
	}

	client.SendMessage(ctx, chatID, unknownChatMsg, nil)
}

func (b *Bot) handleCallback(ctx context.Context, client *Client, cq *CallbackQuery) {
	chatID := cq.Message.Chat.ID

	if driver, err := b.Store.GetDriverByTelegramChat(ctx, chatID); err == nil {
		t, _, err := b.tenantFor(ctx, driver.TenantID)
		if err != nil {
			client.AnswerCallbackQuery(ctx, cq.ID, tryAgainMsg)
			return
		}
		b.handleDriverCallback(ctx, client, t, chatID, driver, cq)
		return
	}
	if user, err := b.Store.GetUserByTelegramChat(ctx, chatID); err == nil {
		t, settings, err := b.tenantFor(ctx, user.TenantID)
		if err != nil {
			client.AnswerCallbackQuery(ctx, cq.ID, tryAgainMsg)
			return
		}
		b.handleAdminCallback(ctx, client, t, settings, chatID, user, cq)
		return
	}
	client.AnswerCallbackQuery(ctx, cq.ID, "Nepoznat nalog.")
}

// handleStart links a Telegram chat to a driver or a web user. The one-time
// link token is the only thing that ties an unknown chat to a firm: it was
// created inside that firm, by someone signed in to it.
func (b *Bot) handleStart(ctx context.Context, client *Client, chatID int64, text string) {
	parts := strings.Fields(text)
	if len(parts) < 2 {
		client.SendMessage(ctx, chatID, "Dobrodošli. Zatražite link za povezivanje od dispečera.", nil)
		return
	}
	lt, err := b.Store.ConsumeLinkToken(ctx, parts[1])
	if err != nil {
		client.SendMessage(ctx, chatID, "Link nije važeći ili je istekao. Zatražite novi od dispečera.", nil)
		return
	}
	t, err := b.Store.GetTenant(ctx, lt.TenantID)
	if err != nil {
		log.Printf("telegram: tenant for link token: %v", err)
		client.SendMessage(ctx, chatID, tryAgainMsg, nil)
		return
	}

	switch {
	case lt.DriverID != nil:
		if err := b.Store.SetDriverTelegramChatID(ctx, t.ID, *lt.DriverID, chatID); err != nil {
			b.reportLinkError(ctx, client, chatID, err)
			return
		}
		driver, _ := b.Store.GetDriver(ctx, t.ID, *lt.DriverID)
		name := "vozač"
		if driver != nil {
			name = driver.Name
		}
		client.SendMessage(ctx, chatID, "Zdravo, "+name+"! Povezani ste.\n\n"+driverHelpText, DriverKeyboard())
	case lt.UserID != nil:
		if err := b.Store.SetUserTelegramChatID(ctx, t.ID, *lt.UserID, chatID); err != nil {
			b.reportLinkError(ctx, client, chatID, err)
			return
		}
		client.SendMessage(ctx, chatID, "Povezani ste kao administrator.\n\n"+adminHelpText, nil)
	}
}

func (b *Bot) reportLinkError(ctx context.Context, client *Client, chatID int64, err error) {
	var verr *store.ValidationError
	if errors.As(err, &verr) {
		client.SendMessage(ctx, chatID, verr.Msg+" Javite se dispečeru.", nil)
		return
	}
	log.Printf("telegram: link chat %d: %v", chatID, err)
	client.SendMessage(ctx, chatID, "Greška pri povezivanju, pokušajte ponovo.", nil)
}
