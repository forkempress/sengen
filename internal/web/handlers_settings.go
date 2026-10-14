package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"shengen/internal/store"
	"shengen/internal/tenant"
)

type settingsDriversData struct {
	baseData
	Drivers []store.Driver
	Link    *telegramLink // set right after generating a driver's link
	// Linked counts active drivers connected to the bot.
	Linked, ActiveCount int
}

func (s *Server) handleSettingsDriversPage(w http.ResponseWriter, r *http.Request) {
	s.renderDriversPage(w, r, nil)
}

func (s *Server) renderDriversPage(w http.ResponseWriter, r *http.Request, link *telegramLink) {
	s.renderDriversPageFlash(w, r, link, "")
}

// renderDriversPageFlash renders the page with a success message of its
// own, for actions that answer with the page instead of a redirect.
func (s *Server) renderDriversPageFlash(w http.ResponseWriter, r *http.Request, link *telegramLink, ok string) {
	t, _ := tenant.FromContext(r.Context())
	drivers, err := s.Store.ListDrivers(r.Context(), t.ID, false)
	if err != nil {
		s.serverError(w, r)
		return
	}
	data := settingsDriversData{baseData: s.base(r, "vozaci"), Drivers: drivers, Link: link}
	if ok != "" {
		data.Flash = ok
	}
	for _, d := range drivers {
		if d.Active {
			data.ActiveCount++
			if d.TelegramChatID != nil {
				data.Linked++
			}
		}
	}
	s.render(w, "podesavanja_vozaci.html", data)
}

func (s *Server) handleSettingsDriverCreate(w http.ResponseWriter, r *http.Request) {
	t, _ := tenant.FromContext(r.Context())
	name := strings.TrimSpace(r.FormValue("name"))
	vehicle := strings.TrimSpace(r.FormValue("vehicle_label"))
	if name == "" {
		s.redirectErr(w, r, "/podesavanja/vozaci", "Ime vozača je obavezno.")
		return
	}
	if utf8.RuneCountInString(name) > store.MaxNameLen || utf8.RuneCountInString(vehicle) > store.MaxNameLen {
		msg := fmt.Sprintf("Ime vozača i oznaka vozila mogu imati najviše %d znakova.", store.MaxNameLen)
		s.redirectErr(w, r, "/podesavanja/vozaci", msg)
		return
	}
	driver, err := s.Store.CreateDriver(r.Context(), t.ID, name, vehicle)
	if err != nil {
		s.serverError(w, r)
		return
	}
	// The link (and QR) come right away, so the dispatcher can connect the
	// driver while he is still in the office (UX-08).
	link, err := s.issueDriverLink(r, t.ID, driver)
	if err != nil {
		s.serverError(w, r)
		return
	}
	s.renderDriversPageFlash(w, r, link, "Vozač dodat. Povežite ga sa Telegram botom linkom ili QR kodom ispod.")
}

func (s *Server) setDriverActive(w http.ResponseWriter, r *http.Request, active bool) {
	t, _ := tenant.FromContext(r.Context())
	id, err := pathInt64(r, "id")
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := s.Store.SetDriverActive(r.Context(), t.ID, id, active); errors.Is(err, store.ErrNotFound) {
		s.notFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, r)
		return
	}
	http.Redirect(w, r, "/podesavanja/vozaci", http.StatusSeeOther)
}

func (s *Server) handleSettingsDriverDeactivate(w http.ResponseWriter, r *http.Request) {
	s.setDriverActive(w, r, false)
}

func (s *Server) handleSettingsDriverActivate(w http.ResponseWriter, r *http.Request) {
	s.setDriverActive(w, r, true)
}
