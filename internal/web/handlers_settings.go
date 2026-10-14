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
}

func (s *Server) handleSettingsDriversPage(w http.ResponseWriter, r *http.Request) {
	s.renderDriversPage(w, r, nil)
}

func (s *Server) renderDriversPage(w http.ResponseWriter, r *http.Request, link *telegramLink) {
	t, _ := tenant.FromContext(r.Context())
	drivers, err := s.Store.ListDrivers(r.Context(), t.ID, false)
	if err != nil {
		http.Error(w, "greška servera", http.StatusInternalServerError)
		return
	}
	data := settingsDriversData{baseData: s.base(r, "vozaci"), Drivers: drivers, Link: link}
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
	if _, err := s.Store.CreateDriver(r.Context(), t.ID, name, vehicle); err != nil {
		http.Error(w, "greška servera", http.StatusInternalServerError)
		return
	}
	s.redirectOk(w, r, "/podesavanja/vozaci", "Vozač dodat.")
}

func (s *Server) setDriverActive(w http.ResponseWriter, r *http.Request, active bool) {
	t, _ := tenant.FromContext(r.Context())
	id, err := pathInt64(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.Store.SetDriverActive(r.Context(), t.ID, id, active); errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		http.Error(w, "greška servera", http.StatusInternalServerError)
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
