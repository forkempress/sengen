package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"shengen/internal/auth"
	"shengen/internal/status"
	"shengen/internal/store"
)

// crossingRow is what the dashboard's "crossing" template gets for one row.
type crossingRow struct {
	Status status.DriverStatus
	Page   pregledData
	Where  string
}

// handleCrossing records a border crossing straight from the dashboard row,
// for firms that enter crossings themselves instead of through the bot:
// "ulaz" opens a stay on the date, "izlaz" closes the open one on it. The
// dashboard is reloaded with the same filters, so ten drivers take ten
// clicks.
func (s *Server) handleCrossing(w http.ResponseWriter, r *http.Request) {
	driver, tenantID, ok := s.loadDriverOr404(w, r)
	if !ok {
		return
	}
	u, _ := auth.UserFromContext(r.Context())
	back := pregledBackURL(r.FormValue("back"))

	date, err := time.Parse("2006-01-02", r.FormValue("date"))
	if err != nil {
		s.redirectErr(w, r, back, driver.Name+": neispravan datum.")
		return
	}
	day := date.Format("02.01.2006")

	switch r.FormValue("smer") {
	case "ulaz":
		msg, err := s.recordEntry(r, tenantID, driver.ID, u.ID, date)
		if s.crossingFailed(w, r, back, driver.Name, err) {
			return
		}
		s.redirectOk(w, r, back, fmt.Sprintf("%s: %s %s.", driver.Name, msg, day))
	case "izlaz":
		closed, err := s.Store.CloseOpenStay(r.Context(), tenantID, driver.ID, date)
		if errors.Is(err, store.ErrNotFound) {
			s.redirectErr(w, r, back, driver.Name+" nema otvoren boravak u Šengenu.")
			return
		}
		if s.crossingFailed(w, r, back, driver.Name, err) {
			return
		}
		s.Store.RecordAudit(r.Context(), tenantID, "user", &u.ID, "stay", closed.ID, "close", nil, nil)
		s.redirectOk(w, r, back, fmt.Sprintf("%s: izlazak iz Šengena %s.", driver.Name, day))
	default:
		s.redirectErr(w, r, back, "Nepoznata akcija.")
	}
}

// recordEntry opens a stay on date. Back into Schengen on the date the last
// stay ended: that date is already a Schengen day, so the stay continues
// instead of a new one overlapping it (same as the bot).
func (s *Server) recordEntry(r *http.Request, tenantID, driverID, userID int64, date time.Time) (string, error) {
	stays, err := s.Store.ListStaysByDriver(r.Context(), tenantID, driverID)
	if err != nil {
		return "", err
	}
	if n := len(stays); n > 0 && stays[n-1].Exit != nil && stays[n-1].Exit.Equal(date) {
		last := stays[n-1]
		if err := s.Store.UpdateStay(r.Context(), tenantID, last.ID, last.Entry, nil, last.Type, last.Note); err != nil {
			return "", err
		}
		s.Store.RecordAudit(r.Context(), tenantID, "user", &userID, "stay", last.ID, "reopen", nil, nil)
		return "ponovo u Šengenu, nastavak boravka od " + last.Entry.Format("02.01.2006") + ", ulazak", nil
	}
	st, err := s.Store.CreateStay(r.Context(), tenantID, driverID, date, nil, "work", "manual", "", &userID)
	if err != nil {
		return "", err
	}
	s.Store.RecordAudit(r.Context(), tenantID, "user", &userID, "stay", st.ID, "create", nil, nil)
	return "ulazak u Šengen", nil
}

// crossingFailed answers a failed crossing and reports whether it did.
func (s *Server) crossingFailed(w http.ResponseWriter, r *http.Request, back, name string, err error) bool {
	if err == nil {
		return false
	}
	var verr *store.ValidationError
	if errors.As(err, &verr) {
		s.redirectErr(w, r, back, name+": "+verr.Msg)
		return true
	}
	s.serverError(w, r)
	return true
}

// pregledBackURL keeps the dashboard filters across the redirect, but only
// ever to the dashboard itself, never to an address taken from the form.
func pregledBackURL(back string) string {
	if back == "/pregled" || strings.HasPrefix(back, "/pregled?") {
		return back
	}
	return "/pregled"
}
