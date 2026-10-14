package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"shengen/internal/auth"
	"shengen/internal/idgen"
	"shengen/internal/status"
	"shengen/internal/store"
	"shengen/internal/telegram"
	"shengen/internal/tenant"
)

type telegramSettingsData struct {
	baseData
	Settings   store.TenantSettings
	WebhookURL string
	LinkMsg    string
}

func (s *Server) handleTelegramSettingsPage(w http.ResponseWriter, r *http.Request) {
	t, _ := tenant.FromContext(r.Context())
	settings, _ := t.ParsedSettings()
	data := telegramSettingsData{
		baseData:   s.base(r, "telegram"),
		Settings:   settings,
		WebhookURL: "https://" + t.Host + "/telegram/webhook",
	}
	if msg := r.URL.Query().Get("ok"); msg != "" {
		data.Flash = msg
	}
	if msg := r.URL.Query().Get("err"); msg != "" {
		data.FlashErr = msg
	}
	if msg := r.URL.Query().Get("link"); msg != "" {
		data.LinkMsg = msg
	}
	s.render(w, "podesavanja_telegram.html", data)
}

func (s *Server) handleTelegramSettingsUpdate(w http.ResponseWriter, r *http.Request) {
	t, _ := tenant.FromContext(r.Context())
	settings, _ := t.ParsedSettings()

	settings.TelegramBotToken = r.FormValue("bot_token")
	if settings.TelegramWebhookSecret == "" {
		secret, err := idgen.RandomToken(24)
		if err != nil {
			http.Error(w, "greška servera", http.StatusInternalServerError)
			return
		}
		settings.TelegramWebhookSecret = secret
	}
	if chatIDStr := r.FormValue("alert_chat_id"); chatIDStr != "" {
		id, err := strconv.ParseInt(chatIDStr, 10, 64)
		if err != nil {
			http.Redirect(w, r, "/podesavanja/telegram?err=Neispravan+chat+ID.", http.StatusSeeOther)
			return
		}
		settings.TelegramAlertChatID = id
	} else {
		settings.TelegramAlertChatID = 0
	}
	if red, err := strconv.Atoi(r.FormValue("threshold_red")); err == nil && red > 0 {
		settings.ThresholdRedDays = red
	}
	if orange, err := strconv.Atoi(r.FormValue("threshold_orange")); err == nil && orange > 0 {
		settings.ThresholdOrangeDays = orange
	}

	if th := status.ThresholdsFor(settings); th.Red >= th.Orange {
		http.Redirect(w, r, "/podesavanja/telegram?err="+url.QueryEscape(
			fmt.Sprintf("Crveni prag (%d) mora biti manji od narandžastog (%d).", th.Red, th.Orange)), http.StatusSeeOther)
		return
	}

	if err := s.Store.UpdateTenantSettings(r.Context(), t.ID, settings); err != nil {
		http.Error(w, "greška servera", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/podesavanja/telegram?ok=Podešavanja+sačuvana.", http.StatusSeeOther)
}

func (s *Server) handleTelegramSetWebhook(w http.ResponseWriter, r *http.Request) {
	t, _ := tenant.FromContext(r.Context())
	settings, _ := t.ParsedSettings()
	if settings.TelegramBotToken == "" || settings.TelegramWebhookSecret == "" {
		http.Redirect(w, r, "/podesavanja/telegram?err=Prvo+unesite+i+sačuvajte+bot+token.", http.StatusSeeOther)
		return
	}
	client := telegram.NewClient(settings.TelegramBotToken)
	webhookURL := "https://" + t.Host + "/telegram/webhook"
	if err := client.SetWebhook(r.Context(), webhookURL, settings.TelegramWebhookSecret); err != nil {
		http.Redirect(w, r, "/podesavanja/telegram?err="+url.QueryEscape("Greška: "+err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/podesavanja/telegram?ok=Webhook+postavljen.", http.StatusSeeOther)
}

func (s *Server) handleTelegramLinkSelf(w http.ResponseWriter, r *http.Request) {
	t, _ := tenant.FromContext(r.Context())
	u, _ := auth.UserFromContext(r.Context())

	token, err := idgen.RandomToken(24)
	if err != nil {
		http.Error(w, "greška servera", http.StatusInternalServerError)
		return
	}
	if err := s.Store.CreateUserLinkToken(r.Context(), token, t.ID, u.ID, 48*time.Hour); err != nil {
		http.Error(w, "greška servera", http.StatusInternalServerError)
		return
	}
	msg := fmt.Sprintf("Pošaljite botu komandu: /start %s (link važi 48h)", token)
	http.Redirect(w, r, "/podesavanja/telegram?link="+url.QueryEscape(msg), http.StatusSeeOther)
}

func (s *Server) handleDriverTelegramLink(w http.ResponseWriter, r *http.Request) {
	driver, tenantID, ok := s.loadDriverOr404(w, r)
	if !ok {
		return
	}
	token, err := idgen.RandomToken(24)
	if err != nil {
		http.Error(w, "greška servera", http.StatusInternalServerError)
		return
	}
	if err := s.Store.CreateDriverLinkToken(r.Context(), token, tenantID, driver.ID, 48*time.Hour); err != nil {
		http.Error(w, "greška servera", http.StatusInternalServerError)
		return
	}
	msg := fmt.Sprintf("Link za %s: neka vozač pošalje botu /start %s (važi 48h)", driver.Name, token)
	http.Redirect(w, r, "/podesavanja/vozaci?ok="+url.QueryEscape(msg), http.StatusSeeOther)
}
