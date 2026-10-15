package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"time"
	"unicode/utf8"

	"shengen/internal/auth"
	"shengen/internal/calc"
	"shengen/internal/clock"
	"shengen/internal/status"
	"shengen/internal/store"
	"shengen/internal/tenant"
)

// tripCheckView is the quick check ("može li vozač biti u Šengenu od–do"),
// rendered by the "tripResult" template on the driver page and on /planer.
type tripCheckView struct {
	DriverID   int64
	DriverName string
	From, To   string // as entered
	Check      *calc.TripCheck
	Err        string
	// Away: the trip would start while the driver is still in Schengen.
	Away bool
	// Notes are warnings about the saved plans: overlaps, a later plan
	// pushed over the limit.
	Notes []string
	// PlansCounted is how many saved plans still ahead the check included.
	PlansCounted int
	CSRFToken    string
}

// Submitted reports whether there is anything to show.
func (v tripCheckView) Submitted() bool { return v.From != "" || v.To != "" }

// CanSave reports whether the checked trip may be saved as a plan.
func (v tripCheckView) CanSave() bool { return v.Check != nil && v.Err == "" && !v.Away }

func parseDay(s string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02", s)
	return t, err == nil
}

// checkTrip runs the quick check for a driver; fromStr and toStr come from
// the form.
func checkTrip(stays []store.Stay, plans []store.PlannedStay, today time.Time, fromStr, toStr string) tripCheckView {
	v := tripCheckView{From: fromStr, To: toStr}
	from, ok := parseDay(fromStr)
	if !ok {
		v.Err = "Izaberite datum polaska."
		return v
	}
	to, ok := parseDay(toStr)
	if !ok {
		v.Err = "Izaberite datum povratka."
		return v
	}
	c, err := status.CheckTrip(stays, plans, today, from, to)
	switch {
	case errors.Is(err, calc.ErrTripStartsWhileAway):
		v.Away, v.Check = true, &c
		return v
	case errors.Is(err, calc.ErrTripInPast):
		v.Err = "Polazak ne može biti u prošlosti."
	case errors.Is(err, calc.ErrTripBadRange):
		v.Err = "Povratak ne može biti pre polaska."
	case errors.Is(err, calc.ErrTripTooLong):
		v.Err = fmt.Sprintf("Tura može trajati najviše %d dana.", calc.MaxDays)
	}
	if err != nil {
		return v
	}
	v.Check = &c
	for _, pv := range status.EvaluatePlans(stays, plans, today) {
		if pv.Active() {
			v.PlansCounted++
		}
	}
	for _, i := range c.OverlapsPlans {
		v.Notes = append(v.Notes, "Preklapa se sa planom "+status.PlanDates(plans[i])+".")
	}
	if c.BreaksPlan >= 0 {
		v.Notes = append(v.Notes, fmt.Sprintf("Posle ove ture plan %s prelazi limit %s.",
			status.PlanDates(plans[c.BreaksPlan]), c.BreaksOn.Format("02.01.2006")))
	}
	return v
}

// tripQuery reads the quick check from the URL. Old links that still carry
// polazak and dana (start and length) keep working.
func tripQuery(r *http.Request) (from, to string) {
	q := r.URL.Query()
	from, to = q.Get("od"), q.Get("do")
	if from == "" && to == "" && q.Get("polazak") != "" {
		from = q.Get("polazak")
		if start, ok := parseDay(from); ok {
			if n, err := strconv.Atoi(q.Get("dana")); err == nil && n > 0 {
				to = start.AddDate(0, 0, n-1).Format("2006-01-02")
			}
		}
	}
	return from, to
}

// --- /planer: the quick check for any driver, and the firm's plans ---

type plannerRow struct {
	DriverID   int64
	DriverName string
	View       status.PlanView
}

type plannerData struct {
	baseData
	Drivers  []store.Driver
	DriverID int64
	Trip     tripCheckView
	Upcoming []plannerRow
}

func (s *Server) handlePlanner(w http.ResponseWriter, r *http.Request) {
	t, _ := tenant.FromContext(r.Context())
	today := clock.Today()
	drivers, err := s.Store.ListDrivers(r.Context(), t.ID, true)
	if err != nil {
		s.serverError(w, r)
		return
	}
	data := plannerData{baseData: s.base(r, "planer"), Drivers: drivers}

	allStays, err := s.Store.ListStaysForTenant(r.Context(), t.ID)
	if err != nil {
		s.serverError(w, r)
		return
	}
	allPlans, err := s.Store.ListPlannedStaysFrom(r.Context(), t.ID, today)
	if err != nil {
		s.serverError(w, r)
		return
	}
	staysBy := map[int64][]store.Stay{}
	for _, st := range allStays {
		staysBy[st.DriverID] = append(staysBy[st.DriverID], st)
	}
	plansBy := map[int64][]store.PlannedStay{}
	for _, p := range allPlans {
		plansBy[p.DriverID] = append(plansBy[p.DriverID], p)
	}

	from, to := tripQuery(r)
	if id, err := strconv.ParseInt(r.URL.Query().Get("vozac"), 10, 64); err == nil {
		// Only the firm's own active drivers can be picked; any other id is
		// treated as no choice at all.
		for _, d := range drivers {
			if d.ID == id {
				data.DriverID = id
				if from != "" || to != "" {
					data.Trip = checkTrip(staysBy[id], plansBy[id], today, from, to)
				}
				data.Trip.DriverID, data.Trip.DriverName = id, d.Name
			}
		}
	}
	if data.DriverID == 0 {
		data.Trip.From, data.Trip.To = from, to
		if from != "" || to != "" {
			data.Trip.Err = "Izaberite vozača."
		}
	}
	data.Trip.CSRFToken = data.CSRFToken

	for _, d := range drivers {
		for _, v := range status.EvaluatePlans(staysBy[d.ID], plansBy[d.ID], today) {
			if v.Active() {
				data.Upcoming = append(data.Upcoming, plannerRow{DriverID: d.ID, DriverName: d.Name, View: v})
			}
		}
	}
	sort.SliceStable(data.Upcoming, func(i, j int) bool { return data.Upcoming[i].View.From.Before(data.Upcoming[j].View.From) })
	s.render(w, "planer.html", data)
}

// --- saved plans: create, update, delete ---

func plansPath(driverID int64) string {
	return "/vozaci/" + strconv.FormatInt(driverID, 10) + "#planovi"
}

func parsePlanForm(r *http.Request) (from, to time.Time, note string, err error) {
	from, ok := parseDay(r.FormValue("od"))
	if !ok {
		return from, to, "", errors.New("izaberite datum polaska")
	}
	if to, ok = parseDay(r.FormValue("do")); !ok {
		return from, to, "", errors.New("izaberite datum povratka")
	}
	note = r.FormValue("napomena")
	if utf8.RuneCountInString(note) > store.MaxNoteLen {
		return from, to, "", fmt.Errorf("napomena je predugačka (najviše %d znakova)", store.MaxNoteLen)
	}
	return from, to, note, nil
}

func (s *Server) handlePlanCreate(w http.ResponseWriter, r *http.Request) {
	driver, tenantID, ok := s.loadDriverOr404(w, r)
	if !ok {
		return
	}
	u, _ := auth.UserFromContext(r.Context())
	from, to, note, perr := parsePlanForm(r)
	if perr != nil {
		s.redirectErr(w, r, plansPath(driver.ID), "Plan nije sačuvan: "+perr.Error()+".")
		return
	}
	stays, err := s.Store.ListStaysByDriver(r.Context(), tenantID, driver.ID)
	if err != nil {
		s.serverError(w, r)
		return
	}
	// A plan obeys the same rules as the quick check: not in the past, not
	// while the driver is still away.
	if _, err := status.CheckTrip(stays, nil, clock.Today(), from, to); err != nil {
		s.redirectErr(w, r, plansPath(driver.ID), "Plan nije sačuvan: "+planErrMsg(err)+".")
		return
	}
	p, err := s.Store.CreatePlannedStay(r.Context(), tenantID, driver.ID, from, to, note, &u.ID)
	if s.planWriteFailed(w, r, driver.ID, err) {
		return
	}
	s.auditPlan(r, tenantID, u.ID, p.ID, "create", nil, p)
	s.redirectPlanSaved(w, r, tenantID, driver.ID, stays, p.ID, "Plan sačuvan.")
}

func (s *Server) handlePlanUpdate(w http.ResponseWriter, r *http.Request) {
	driver, tenantID, p, ok := s.loadPlanOr404(w, r)
	if !ok {
		return
	}
	u, _ := auth.UserFromContext(r.Context())
	from, to, note, perr := parsePlanForm(r)
	if perr != nil {
		s.redirectErr(w, r, plansPath(driver.ID), "Plan nije izmenjen: "+perr.Error()+".")
		return
	}
	stays, err := s.Store.ListStaysByDriver(r.Context(), tenantID, driver.ID)
	if err != nil {
		s.serverError(w, r)
		return
	}
	// Moving the start brings the quick check's rules back; a plan already
	// under way may only have its return or note changed.
	if !from.Equal(p.From) {
		if _, err := status.CheckTrip(stays, nil, clock.Today(), from, to); err != nil {
			s.redirectErr(w, r, plansPath(driver.ID), "Plan nije izmenjen: "+planErrMsg(err)+".")
			return
		}
	}
	err = s.Store.UpdatePlannedStay(r.Context(), tenantID, p.ID, from, to, note)
	if s.planWriteFailed(w, r, driver.ID, err) {
		return
	}
	after := *p
	after.From, after.To, after.Note = from, to, note
	s.auditPlan(r, tenantID, u.ID, p.ID, "update", p, &after)
	s.redirectPlanSaved(w, r, tenantID, driver.ID, stays, p.ID, "Plan izmenjen.")
}

func (s *Server) handlePlanDelete(w http.ResponseWriter, r *http.Request) {
	driver, tenantID, p, ok := s.loadPlanOr404(w, r)
	if !ok {
		return
	}
	u, _ := auth.UserFromContext(r.Context())
	if err := s.Store.DeletePlannedStay(r.Context(), tenantID, p.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.serverError(w, r)
		return
	}
	s.auditPlan(r, tenantID, u.ID, p.ID, "delete", p, nil)
	s.redirectOk(w, r, plansPath(driver.ID), "Plan obrisan.")
}

// loadPlanOr404 loads the driver and the plan from the path; a plan of
// another driver (or another firm) is not found.
func (s *Server) loadPlanOr404(w http.ResponseWriter, r *http.Request) (*store.Driver, int64, *store.PlannedStay, bool) {
	driver, tenantID, ok := s.loadDriverOr404(w, r)
	if !ok {
		return nil, 0, nil, false
	}
	planID, err := pathInt64(r, "plan_id")
	if err != nil {
		s.notFound(w, r)
		return nil, 0, nil, false
	}
	p, err := s.Store.GetPlannedStay(r.Context(), tenantID, planID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && p.DriverID != driver.ID) {
		s.notFound(w, r)
		return nil, 0, nil, false
	}
	if err != nil {
		s.serverError(w, r)
		return nil, 0, nil, false
	}
	return driver, tenantID, p, true
}

func planErrMsg(err error) string {
	switch {
	case errors.Is(err, calc.ErrTripInPast):
		return "polazak ne može biti u prošlosti"
	case errors.Is(err, calc.ErrTripBadRange):
		return "povratak ne može biti pre polaska"
	case errors.Is(err, calc.ErrTripTooLong):
		return fmt.Sprintf("plan može trajati najviše %d dana", calc.MaxDays)
	case errors.Is(err, calc.ErrTripStartsWhileAway):
		return "vozač je još u Šengenu, nova tura može početi tek posle povratka"
	}
	return err.Error()
}

func (s *Server) planWriteFailed(w http.ResponseWriter, r *http.Request, driverID int64, err error) bool {
	if err == nil {
		return false
	}
	var verr *store.ValidationError
	switch {
	case errors.As(err, &verr):
		s.redirectErr(w, r, plansPath(driverID), "Plan nije sačuvan: "+verr.Msg+".")
	case errors.Is(err, store.ErrNotFound):
		s.notFound(w, r)
	default:
		s.serverError(w, r)
	}
	return true
}

// redirectPlanSaved confirms the save and, next to it, warns when the plan
// overlaps another plan or does not fit in 90/180 with everything else.
// Saving is never refused for that: a dispatcher may keep a plan he still
// has to rearrange, and the overview keeps flagging it.
func (s *Server) redirectPlanSaved(w http.ResponseWriter, r *http.Request, tenantID, driverID int64, stays []store.Stay, planID int64, msg string) {
	plans, err := s.Store.ListPlannedStaysByDriver(r.Context(), tenantID, driverID)
	if err != nil {
		s.redirectOk(w, r, plansPath(driverID), msg)
		return
	}
	var warn string
	for _, v := range status.EvaluatePlans(stays, plans, clock.Today()) {
		over := v.Evaluated && !v.OK
		switch {
		case v.ID != planID:
			if over {
				warn += fmt.Sprintf(" Plan %s prelazi limit %s.", status.PlanDates(v.PlannedStay), v.FirstOverstay.Format("02.01.2006"))
			}
		default:
			if over {
				warn += fmt.Sprintf(" Ovaj plan prelazi limit %s.", v.FirstOverstay.Format("02.01.2006"))
			}
			if v.OverlapsOther {
				warn += " Preklapa se sa drugim planom."
			}
		}
	}
	if warn != "" {
		warn = "Pažnja:" + warn
	}
	s.redirectFlash(w, r, plansPath(driverID), msg, warn)
}

func (s *Server) auditPlan(r *http.Request, tenantID, actorID, planID int64, action string, before, after *store.PlannedStay) {
	asJSON := func(p *store.PlannedStay) *string {
		if p == nil {
			return nil
		}
		b, _ := json.Marshal(map[string]any{
			"vozac": p.DriverID, "od": p.From.Format("2006-01-02"), "do": p.To.Format("2006-01-02"), "napomena": p.Note,
		})
		str := string(b)
		return &str
	}
	if err := s.Store.RecordAudit(r.Context(), tenantID, "user", &actorID, "planned_stay", planID, action, asJSON(before), asJSON(after)); err != nil {
		log.Printf("audit planned_stay %d %s: %v", planID, action, err)
	}
}
