package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"shengen/internal/auth"
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
	// The calendar: the current month and the next ones, then the earlier
	// months, which phones show only on request (UX-14).
	Months        []MonthGrid
	EarlierMonths []MonthGrid
	Stays         []store.Stay // most recent first

	// Saved plans, earliest first, and the quick check once its form has
	// been submitted.
	Plans    []status.PlanView
	PlanSum  status.PlanSummary
	Trip     tripCheckView
	HasPlans bool
}

func (s *Server) loadDriverOr404(w http.ResponseWriter, r *http.Request) (*store.Driver, int64, bool) {
	t, _ := tenant.FromContext(r.Context())
	id, err := pathInt64(r, "id")
	if err != nil {
		s.notFound(w, r)
		return nil, 0, false
	}
	driver, err := s.Store.GetDriver(r.Context(), t.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		s.notFound(w, r)
		return nil, 0, false
	}
	if err != nil {
		s.serverError(w, r)
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
		s.serverError(w, r)
		return
	}
	today := clock.Today()
	driverStatus := status.Compute(*driver, stays, today)
	if t, ok := tenant.FromContext(r.Context()); ok {
		settings, _ := t.ParsedSettings()
		driverStatus.ApplyThresholds(status.ThresholdsFor(settings))
	}
	plans, err := s.Store.ListPlannedStaysByDriver(r.Context(), tenantID, driver.ID)
	if err != nil {
		s.serverError(w, r)
		return
	}
	views := status.EvaluatePlans(stays, plans, today)

	const monthsBefore = 3
	months := buildMonthGrids(stays, views, today, monthsBefore, monthsAhead(views, today))

	reversed := make([]store.Stay, len(stays))
	for i, st := range stays {
		reversed[len(stays)-1-i] = st
	}

	data := driverDetailData{
		baseData:      s.base(r, "vozaci"),
		Driver:        *driver,
		Status:        driverStatus,
		Months:        months[monthsBefore:],
		EarlierMonths: months[:monthsBefore],
		Stays:         reversed,
		Plans:         views,
		PlanSum:       status.Summarize(views),
		HasPlans:      len(plans) > 0,
	}
	if from, to := tripQuery(r); from != "" || to != "" {
		data.Trip = checkTrip(stays, plans, today, from, to)
	}
	data.Trip.DriverID, data.Trip.DriverName, data.Trip.CSRFToken = driver.ID, driver.Name, data.CSRFToken
	s.render(w, "vozac_detalj.html", data)
}

// monthsAhead is how many months after the current one the calendar shows:
// two, or as many as it takes to reach the end of the last plan still ahead
// (at most a year).
func monthsAhead(views []status.PlanView, today time.Time) int {
	n := 2
	for _, v := range views {
		if !v.Active() {
			continue
		}
		m := (v.To.Year()-today.Year())*12 + int(v.To.Month()) - int(today.Month())
		if m > n {
			n = m
		}
	}
	return min(n, 12)
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
		s.serverError(w, r)
		return
	}
	s.Store.RecordAudit(r.Context(), tenantID, "user", &u.ID, "stay", closed.ID, "close", nil, nil)
	s.redirectDriverOk(w, r, driver.ID, "Zabeleženo: vozač se vratio.")
}

func (s *Server) redirectDriverWithError(w http.ResponseWriter, r *http.Request, driverID int64, msg string) {
	s.redirectErr(w, r, "/vozaci/"+strconv.FormatInt(driverID, 10), msg)
}

func (s *Server) redirectDriverOk(w http.ResponseWriter, r *http.Request, driverID int64, msg string) {
	s.redirectOk(w, r, "/vozaci/"+strconv.FormatInt(driverID, 10), msg)
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
	if utf8.RuneCountInString(note) > store.MaxNoteLen {
		return time.Time{}, nil, "", "", fmt.Errorf("napomena je predugačka (najviše %d znakova)", store.MaxNoteLen)
	}
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
		s.serverError(w, r)
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
		s.notFound(w, r)
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
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r)
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
		s.notFound(w, r)
		return
	}
	u, _ := auth.UserFromContext(r.Context())

	if err := s.Store.DeleteStay(r.Context(), tenantID, stayID); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.serverError(w, r)
		return
	}
	s.Store.RecordAudit(r.Context(), tenantID, "user", &u.ID, "stay", stayID, "delete", nil, nil)
	s.redirectDriverOk(w, r, driver.ID, "Boravak obrisan.")
}
