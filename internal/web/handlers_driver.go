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
	// The calendar: one month, ?mesec=YYYY-MM, with links to the months
	// either side (empty at the end of the range) and back to today's.
	Month                      MonthGrid
	PrevURL, NextURL, TodayURL string
	Picker                     []pickerYear // the month picker behind the month's name
	Typical                    int          // the firm's usual trip, in days
	TypicalShort               int          // the shortest trip still colored Pažnja
	Selected                   DayCell      // the day the panel shows first
	CalMode                    string       // the firm's default number: store.CalendarRemaining | store.CalendarTrip
	Stays                      []store.Stay // most recent first

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
	var settings store.TenantSettings
	if t, ok := tenant.FromContext(r.Context()); ok {
		settings, _ = t.ParsedSettings()
	}
	thresholds := status.ThresholdsFor(settings)
	driverStatus.ApplyThresholds(thresholds)
	plans, err := s.Store.ListPlannedStaysByDriver(r.Context(), tenantID, driver.ID)
	if err != nil {
		s.serverError(w, r)
		return
	}
	views := status.EvaluatePlans(stays, plans, today)

	month := calendarMonth(r, today)
	grid := buildMonth(stays, views, driverStatus, thresholds, settings.TypicalTrip(), today, month)
	selected, ok := grid.cellOn(today)
	if !ok {
		selected, _ = grid.cellOn(month)
	}

	reversed := make([]store.Stay, len(stays))
	for i, st := range stays {
		reversed[len(stays)-1-i] = st
	}

	data := driverDetailData{
		baseData:     s.base(r, "vozaci"),
		Driver:       *driver,
		Status:       driverStatus,
		Month:        grid,
		Selected:     selected,
		CalMode:      settings.CalendarMode(),
		Typical:      settings.TypicalTrip(),
		TypicalShort: shortTrip(settings.TypicalTrip()),
		Picker:       monthPicker(r, today, month, views),
		Stays:        reversed,
		Plans:        views,
		PlanSum:      status.Summarize(views),
		HasPlans:     len(plans) > 0,
	}
	if from, to := tripQuery(r); from != "" || to != "" {
		data.Trip = checkTrip(stays, plans, today, from, to)
	}
	data.Trip.DriverID, data.Trip.DriverName, data.Trip.CSRFToken = driver.ID, driver.Name, data.CSRFToken
	data.PrevURL, data.NextURL, data.TodayURL = monthLinks(r, today, month)
	s.render(w, "vozac_detalj.html", data)
}

// calendarRange is how many months either side of today's the calendar
// goes.
const calendarRange = 12

// calendarMonth is the month the calendar shows: ?mesec=YYYY-MM, within
// calendarRange of today's month, which is also what anything else means.
func calendarMonth(r *http.Request, today time.Time) time.Time {
	cur := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	m, err := time.Parse("2006-01", r.URL.Query().Get("mesec"))
	if err != nil {
		return cur
	}
	if lo := cur.AddDate(0, -calendarRange, 0); m.Before(lo) {
		return lo
	}
	if hi := cur.AddDate(0, calendarRange, 0); m.After(hi) {
		return hi
	}
	return m
}

// monthURL is the driver page on month m. It keeps the rest of the query
// (a trip check), so its result stays on the page.
func monthURL(r *http.Request, today, m time.Time) string {
	q := r.URL.Query()
	if m.Year() == today.Year() && m.Month() == today.Month() {
		q.Del("mesec")
	} else {
		q.Set("mesec", m.Format("2006-01"))
	}
	u := r.URL.Path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u + "#kalendar"
}

// monthLinks are the calendar's links to the months either side and back to
// today's; one is empty where there is nowhere to go.
func monthLinks(r *http.Request, today, month time.Time) (prev, next, todays string) {
	cur := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	link := func(m time.Time) string { return monthURL(r, today, m) }
	if p := month.AddDate(0, -1, 0); !p.Before(cur.AddDate(0, -calendarRange, 0)) {
		prev = link(p)
	}
	if n := month.AddDate(0, 1, 0); !n.After(cur.AddDate(0, calendarRange, 0)) {
		next = link(n)
	}
	if !month.Equal(cur) {
		todays = link(cur)
	}
	return prev, next, todays
}

type pickerYear struct {
	Year   int
	Shown  bool // the year of the month on screen: the one the picker opens on
	Months []pickerMonth
}

type pickerMonth struct {
	Label   string // short name: Jan, Feb, …
	URL     string // empty outside the calendar's range
	Current bool   // the month on screen
	Today   bool   // today's month
	HasPlan bool   // a plan still ahead falls in it
}

var srMonthShort = [...]string{"Jan", "Feb", "Mar", "Apr", "Maj", "Jun", "Jul", "Avg", "Sep", "Okt", "Nov", "Dec"}

// monthPicker lists every year the calendar's range touches, twelve months
// each, so any month is two clicks away instead of a click per month.
func monthPicker(r *http.Request, today, month time.Time, plans []status.PlanView) []pickerYear {
	cur := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	lo, hi := cur.AddDate(0, -calendarRange, 0), cur.AddDate(0, calendarRange, 0)
	planned := map[string]bool{}
	for _, v := range plans {
		if !v.Active() {
			continue
		}
		for m := time.Date(v.From.Year(), v.From.Month(), 1, 0, 0, 0, 0, time.UTC); !m.After(v.To); m = m.AddDate(0, 1, 0) {
			planned[m.Format("2006-01")] = true
		}
	}
	var years []pickerYear
	for y := lo.Year(); y <= hi.Year(); y++ {
		py := pickerYear{Year: y, Shown: y == month.Year()}
		for i := range 12 {
			m := time.Date(y, time.Month(i+1), 1, 0, 0, 0, 0, time.UTC)
			pm := pickerMonth{Label: srMonthShort[i], Current: m.Equal(month), Today: m.Equal(cur),
				HasPlan: planned[m.Format("2006-01")]}
			if !m.Before(lo) && !m.After(hi) {
				pm.URL = monthURL(r, today, m)
			}
			py.Months = append(py.Months, pm)
		}
		years = append(years, py)
	}
	return years
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
