package web

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
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

	Thresholds                 status.Thresholds
	ExampleCritical, ExampleOK int
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
		Thresholds:    status.ThresholdsFor(settings),
	}
	data.ExampleCritical = max(data.Thresholds.Red-1, 0)
	data.ExampleOK = data.Thresholds.Orange + 1
	if u := data.User; u != nil && u.TelegramChatID != nil {
		data.MyTelegramLinked = true
		data.AlertsToMe = settings.TelegramAlertChatID == *u.TelegramChatID
	}
	s.render(w, "podesavanja_telegram.html", data)
}

// handleAlertGroup sets (or, when empty, clears) the chat the firm's
// alerts go to — the manual fallback for a Telegram group.
func (s *Server) handleAlertGroup(w http.ResponseWriter, r *http.Request) {
	t, _ := tenant.FromContext(r.Context())
	settings, _ := t.ParsedSettings()

	msg := "Upozorenja su isključena."
	if chatIDStr := strings.TrimSpace(r.FormValue("alert_chat_id")); chatIDStr != "" {
		id, err := strconv.ParseInt(chatIDStr, 10, 64)
		if err != nil || id == 0 {
			s.redirectErr(w, r, "/podesavanja/telegram", "Broj grupe nije ispravan: to je ceo broj, obično počinje sa -100.")
			return
		}
		settings.TelegramAlertChatID = id
		msg = "Upozorenja će stizati u grupu."
	} else {
		settings.TelegramAlertChatID = 0
	}
	if err := s.Store.UpdateTenantSettings(r.Context(), t.ID, settings); err != nil {
		s.serverError(w, r)
		return
	}
	s.redirectOk(w, r, "/podesavanja/telegram", msg)
}

// handleThresholds saves the status cut-offs; it never touches where the
// alerts go.
func (s *Server) handleThresholds(w http.ResponseWriter, r *http.Request) {
	t, _ := tenant.FromContext(r.Context())
	settings, _ := t.ParsedSettings()

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
		s.serverError(w, r)
		return
	}
	s.redirectOk(w, r, "/podesavanja/telegram", "Pragovi sačuvani.")
}

func (s *Server) handleTelegramLinkSelf(w http.ResponseWriter, r *http.Request) {
	t, _ := tenant.FromContext(r.Context())
	u, _ := auth.UserFromContext(r.Context())

	token, err := idgen.RandomToken(24)
	if err != nil {
		s.serverError(w, r)
		return
	}
	if err := s.Store.CreateUserLinkToken(r.Context(), token, t.ID, u.ID, 48*time.Hour); err != nil {
		s.serverError(w, r)
		return
	}
	s.renderTelegramSettings(w, r, s.newTelegramLink(u.Name, token))
}

func (s *Server) handleDriverTelegramLink(w http.ResponseWriter, r *http.Request) {
	driver, tenantID, ok := s.loadDriverOr404(w, r)
	if !ok {
		return
	}
	link, err := s.issueDriverLink(r, tenantID, driver)
	if err != nil {
		s.serverError(w, r)
		return
	}
	s.renderDriversPage(w, r, link)
}

// issueDriverLink creates a one-time link that connects the driver's
// Telegram to the bot.
func (s *Server) issueDriverLink(r *http.Request, tenantID int64, driver *store.Driver) (*telegramLink, error) {
	token, err := idgen.RandomToken(24)
	if err != nil {
		return nil, err
	}
	if err := s.Store.CreateDriverLinkToken(r.Context(), token, tenantID, driver.ID, 48*time.Hour); err != nil {
		return nil, err
	}
	return s.newTelegramLink(driver.Name, token), nil
}

// handleAlertsToMe points the firm's alerts at the signed-in owner's own
// Telegram — the alternative to typing a numeric chat id, which a
// non-technical owner has no way of finding.
func (s *Server) handleAlertsToMe(w http.ResponseWriter, r *http.Request) {
	t, _ := tenant.FromContext(r.Context())
	u, _ := auth.UserFromContext(r.Context())

	if u.TelegramChatID == nil {
		s.redirectErr(w, r, "/podesavanja/telegram",
			"Prvo povežite svoj Telegram (korak 1, dugme „Poveži moj Telegram“), pa ponovo kliknite.")
		return
	}

	settings, _ := t.ParsedSettings()
	settings.TelegramAlertChatID = *u.TelegramChatID
	if err := s.Store.UpdateTenantSettings(r.Context(), t.ID, settings); err != nil {
		s.serverError(w, r)
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
