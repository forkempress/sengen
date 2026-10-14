package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"shengen/internal/auth"
	"shengen/internal/calc"
	"shengen/internal/clock"
	"shengen/internal/status"
	"shengen/internal/store"
	"shengen/internal/tenant"
)

func pathInt64(r *http.Request, name string) (int64, error) {
	return strconv.ParseInt(r.PathValue(name), 10, 64)
}

type driverDetailData struct {
	baseData
	Driver store.Driver
	Status status.DriverStatus
	Months []MonthGrid
	Stays  []store.Stay // most recent first

	// "Provera ture": the form's values as entered, and either a result or
	// an error message once the form has been submitted.
	PlanStart string
	PlanDays  string
	Plan      *status.TripPlan
	PlanErr   string
}

func (s *Server) loadDriverOr404(w http.ResponseWriter, r *http.Request) (*store.Driver, int64, bool) {
	t, _ := tenant.FromContext(r.Context())
	id, err := pathInt64(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return nil, 0, false
	}
	driver, err := s.Store.GetDriver(r.Context(), t.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return nil, 0, false
	}
	if err != nil {
		http.Error(w, "greška servera", http.StatusInternalServerError)
		return nil, 0, false
	}
	return driver, t.ID, true
}

func (s *Server) handleDriverDetail(w http.ResponseWriter, r *http.Request) {
	driver, tenantID, ok := s.loadDriverOr404(w, r)
	if !ok {
		return
	}
	stays, err := s.Store.ListStaysByDriver(r.Context(), tenantID, driver.ID)
	if err != nil {
		http.Error(w, "greška servera", http.StatusInternalServerError)
		return
	}
	today := clock.Today()
	driverStatus := status.Compute(*driver, stays, today)
	if t, ok := tenant.FromContext(r.Context()); ok {
		settings, _ := t.ParsedSettings()
		driverStatus.ApplyThresholds(status.ThresholdsFor(settings))
	}
	months := buildMonthGrids(stays, today, 3, 2)

	reversed := make([]store.Stay, len(stays))
	for i, st := range stays {
		reversed[len(stays)-1-i] = st
	}

	data := driverDetailData{
		baseData: s.base(r, "vozaci"),
		Driver:   *driver,
		Status:   driverStatus,
		Months:   months,
		Stays:    reversed,
	}
	data.PlanStart, data.PlanDays = r.URL.Query().Get("polazak"), r.URL.Query().Get("dana")
	if data.PlanStart != "" || data.PlanDays != "" {
		data.Plan, data.PlanErr = planFromForm(stays, today, data.PlanStart, data.PlanDays)
	}
	if msg := r.URL.Query().Get("ok"); msg != "" {
		data.Flash = msg
	}
	if msg := r.URL.Query().Get("err"); msg != "" {
		data.FlashErr = msg
	}
	s.render(w, "vozac_detalj.html", data)
}

// planFromForm validates the "Provera ture" form (a GET, so the result can
// be bookmarked or sent to a colleague) and evaluates the trip.
func planFromForm(stays []store.Stay, today time.Time, startStr, daysStr string) (*status.TripPlan, string) {
	start, err := time.Parse("2006-01-02", startStr)
	if err != nil {
		return nil, "Izaberite datum polaska."
	}
	if start.Before(today) {
		return nil, "Polazak ne može biti u prošlosti."
	}
	days, err := strconv.Atoi(strings.TrimSpace(daysStr))
	if err != nil || days < 1 || days > calc.MaxDays {
		return nil, fmt.Sprintf("Trajanje upišite kao broj dana, od 1 do %d.", calc.MaxDays)
	}
	p := status.PlanTrip(stays, today, start, days)
	return &p, ""
}

func (s *Server) handleDriverReturned(w http.ResponseWriter, r *http.Request) {
	driver, tenantID, ok := s.loadDriverOr404(w, r)
	if !ok {
		return
	}
	u, _ := auth.UserFromContext(r.Context())
	today := clock.Today()

	closed, err := s.Store.CloseOpenStay(r.Context(), tenantID, driver.ID, today)
	if errors.Is(err, store.ErrNotFound) {
		s.redirectDriverWithError(w, r, driver.ID, "Vozač nema otvoren boravak.")
		return
	}
	var verr *store.ValidationError
	if errors.As(err, &verr) {
		s.redirectDriverWithError(w, r, driver.ID, verr.Msg)
		return
	}
	if err != nil {
		http.Error(w, "greška servera", http.StatusInternalServerError)
		return
	}
	s.Store.RecordAudit(r.Context(), tenantID, "user", &u.ID, "stay", closed.ID, "close", nil, nil)
	s.redirectDriverOk(w, r, driver.ID, "Zabeleženo: vozač se vratio.")
}

func (s *Server) redirectDriverWithError(w http.ResponseWriter, r *http.Request, driverID int64, msg string) {
	http.Redirect(w, r, "/vozaci/"+strconv.FormatInt(driverID, 10)+"?err="+url.QueryEscape(msg), http.StatusSeeOther)
}

func (s *Server) redirectDriverOk(w http.ResponseWriter, r *http.Request, driverID int64, msg string) {
	http.Redirect(w, r, "/vozaci/"+strconv.FormatInt(driverID, 10)+"?ok="+url.QueryEscape(msg), http.StatusSeeOther)
}

func parseStayForm(r *http.Request) (entry time.Time, exit *time.Time, typ, note string, err error) {
	entry, err = time.Parse("2006-01-02", r.FormValue("entry_date"))
	if err != nil {
		return time.Time{}, nil, "", "", errors.New("neispravan datum ulaska")
	}
	if exitStr := r.FormValue("exit_date"); exitStr != "" {
		e, perr := time.Parse("2006-01-02", exitStr)
		if perr != nil {
			return time.Time{}, nil, "", "", errors.New("neispravan datum izlaska")
		}
		exit = &e
	}
	typ = r.FormValue("type")
	if typ != "work" && typ != "private" {
		return time.Time{}, nil, "", "", errors.New("neispravan tip boravka")
	}
	note = r.FormValue("note")
	return entry, exit, typ, note, nil
}

func (s *Server) handleStayCreate(w http.ResponseWriter, r *http.Request) {
	driver, tenantID, ok := s.loadDriverOr404(w, r)
	if !ok {
		return
	}
	u, _ := auth.UserFromContext(r.Context())

	entry, exit, typ, note, perr := parseStayForm(r)
	if perr != nil {
		s.redirectDriverWithError(w, r, driver.ID, perr.Error())
		return
	}

	st, err := s.Store.CreateStay(r.Context(), tenantID, driver.ID, entry, exit, typ, "manual", note, &u.ID)
	var verr *store.ValidationError
	if errors.As(err, &verr) {
		s.redirectDriverWithError(w, r, driver.ID, verr.Msg)
		return
	}
	if err != nil {
		http.Error(w, "greška servera", http.StatusInternalServerError)
		return
	}
	s.Store.RecordAudit(r.Context(), tenantID, "user", &u.ID, "stay", st.ID, "create", nil, nil)
	s.redirectDriverOk(w, r, driver.ID, "Boravak dodat.")
}

func (s *Server) handleStayUpdate(w http.ResponseWriter, r *http.Request) {
	driver, tenantID, ok := s.loadDriverOr404(w, r)
	if !ok {
		return
	}
	stayID, err := pathInt64(r, "stay_id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	u, _ := auth.UserFromContext(r.Context())

	entry, exit, typ, note, perr := parseStayForm(r)
	if perr != nil {
		s.redirectDriverWithError(w, r, driver.ID, perr.Error())
		return
	}

	err = s.Store.UpdateStay(r.Context(), tenantID, stayID, entry, exit, typ, note)
	var verr *store.ValidationError
	if errors.As(err, &verr) {
		s.redirectDriverWithError(w, r, driver.ID, verr.Msg)
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "greška servera", http.StatusInternalServerError)
		return
	}
	s.Store.RecordAudit(r.Context(), tenantID, "user", &u.ID, "stay", stayID, "update", nil, nil)
	s.redirectDriverOk(w, r, driver.ID, "Boravak izmenjen.")
}

func (s *Server) handleStayDelete(w http.ResponseWriter, r *http.Request) {
	driver, tenantID, ok := s.loadDriverOr404(w, r)
	if !ok {
		return
	}
	stayID, err := pathInt64(r, "stay_id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	u, _ := auth.UserFromContext(r.Context())

	if err := s.Store.DeleteStay(r.Context(), tenantID, stayID); err != nil && !errors.Is(err, store.ErrNotFound) {
		http.Error(w, "greška servera", http.StatusInternalServerError)
		return
	}
	s.Store.RecordAudit(r.Context(), tenantID, "user", &u.ID, "stay", stayID, "delete", nil, nil)
	s.redirectDriverOk(w, r, driver.ID, "Boravak obrisan.")
}
