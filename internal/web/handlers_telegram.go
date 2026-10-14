package web

import (
	"fmt"
	"log"
	"net/http"
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

	// Where alerts currently go, so the owner never has to read a raw chat id
	// to know whether they are being sent at all.
	MyTelegramLinked bool
	AlertsToMe       bool
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
	if u := data.User; u != nil && u.TelegramChatID != nil {
		data.MyTelegramLinked = true
		data.AlertsToMe = settings.TelegramAlertChatID == *u.TelegramChatID
	}
	s.render(w, "podesavanja_telegram.html", data)
}

func (s *Server) handleTelegramSettingsUpdate(w http.ResponseWriter, r *http.Request) {
	t, _ := tenant.FromContext(r.Context())
	settings, _ := t.ParsedSettings()

	if chatIDStr := r.FormValue("alert_chat_id"); chatIDStr != "" {
		id, err := strconv.ParseInt(chatIDStr, 10, 64)
		if err != nil {
			s.redirectErr(w, r, "/podesavanja/telegram", "Neispravan chat ID.")
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
		s.redirectErr(w, r, "/podesavanja/telegram",
			fmt.Sprintf("Prag za Kritično (%d) mora biti manji od praga za Pažnju (%d).", th.Red, th.Orange))
		return
	}

	if err := s.Store.UpdateTenantSettings(r.Context(), t.ID, settings); err != nil {
		http.Error(w, "greška servera", http.StatusInternalServerError)
		return
	}
	s.redirectOk(w, r, "/podesavanja/telegram", "Podešavanja sačuvana.")
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

// handleAlertsToMe points the firm's alerts at the signed-in owner's own
// Telegram — the alternative to typing a numeric chat id, which a
// non-technical owner has no way of finding.
func (s *Server) handleAlertsToMe(w http.ResponseWriter, r *http.Request) {
	t, _ := tenant.FromContext(r.Context())
	u, _ := auth.UserFromContext(r.Context())

	if u.TelegramChatID == nil {
		s.redirectErr(w, r, "/podesavanja/telegram",
			"Prvo povežite svoj Telegram (dugme „Generiši link za povezivanje“), pa ponovo kliknite.")
		return
	}

	settings, _ := t.ParsedSettings()
	settings.TelegramAlertChatID = *u.TelegramChatID
	if err := s.Store.UpdateTenantSettings(r.Context(), t.ID, settings); err != nil {
		http.Error(w, "greška servera", http.StatusInternalServerError)
		return
	}

	msg := "Upozorenja će stizati na vaš Telegram."
	if client := s.Bot.Client(); client != nil {
		probe := "✅ Ovde će stizati upozorenja za firmu " + t.Name + ": vozači ispod praga, rokovi koji ističu i dnevni pregled u 07:00."
		if err := client.SendMessage(r.Context(), *u.TelegramChatID, probe, nil); err != nil {
			log.Printf("telegram: probe message to owner %d: %v", u.ID, err)
			msg += " Probna poruka ipak nije stigla — proverite da niste blokirali bota, pa pokušajte ponovo."
		} else {
			msg += " Poslali smo vam probnu poruku."
		}
	} else {
		msg += " Bot još nije podešen na serveru, pa poruke počinju da stižu kad proradi."
	}
	s.redirectOk(w, r, "/podesavanja/telegram", msg)
}
