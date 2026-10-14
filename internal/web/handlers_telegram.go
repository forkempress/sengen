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
	"shengen/internal/tenant"
)

// telegramLink is a freshly issued one-time link. It is rendered straight
// into the response page — never put in a redirect URL — so the token
// doesn't end up in browser history or server logs.
type telegramLink struct {
	For     string // whose account it links
	URL     string // one-tap t.me link; empty when the bot username isn't configured
	Command string // the /start command to type when there is no URL
}

func (s *Server) newTelegramLink(forName, token string) *telegramLink {
	return &telegramLink{For: forName, URL: s.Bot.StartLink(token), Command: "/start " + token}
}

type telegramSettingsData struct {
	baseData
	Settings      store.TenantSettings
	BotConfigured bool
	Link          *telegramLink
}

func (s *Server) handleTelegramSettingsPage(w http.ResponseWriter, r *http.Request) {
	s.renderTelegramSettings(w, r, nil)
}

func (s *Server) renderTelegramSettings(w http.ResponseWriter, r *http.Request, link *telegramLink) {
	t, _ := tenant.FromContext(r.Context())
	settings, _ := t.ParsedSettings()
	data := telegramSettingsData{
		baseData:      s.base(r, "telegram"),
		Settings:      settings,
		BotConfigured: s.Bot.Configured(),
		Link:          link,
	}
	if msg := r.URL.Query().Get("ok"); msg != "" {
		data.Flash = msg
	}
	if msg := r.URL.Query().Get("err"); msg != "" {
		data.FlashErr = msg
	}
	s.render(w, "podesavanja_telegram.html", data)
}

func (s *Server) handleTelegramSettingsUpdate(w http.ResponseWriter, r *http.Request) {
	t, _ := tenant.FromContext(r.Context())
	settings, _ := t.ParsedSettings()

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
	s.renderTelegramSettings(w, r, s.newTelegramLink(u.Name, token))
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
	s.renderDriversPage(w, r, s.newTelegramLink(driver.Name, token))
}
