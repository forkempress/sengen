package web

import (
	"net/http"
	"net/url"
	"time"

	"shengen/internal/clock"
	"shengen/internal/status"
	"shengen/internal/tenant"
)

type pregledData struct {
	baseData

	// Statuses is the filtered table; the counters and the urgent banner are
	// always computed over every driver, so a filter never hides a problem
	// from the headline numbers.
	Statuses      []status.DriverStatus
	UrgentDrivers []status.DriverStatus
	// Plans: each driver's saved plans still ahead, by driver id; PlanProblems
	// counts the drivers with a plan over 90/180.
	Plans        map[int64]status.PlanSummary
	PlanProblems int

	TotalDrivers  int
	InSchengenNow int
	Thresholds    status.Thresholds
	// KPI cards, one per status from the worst; each is also a filter link.
	KPIs []kpiCard

	AsOf      time.Time
	IsToday   bool
	IsFuture  bool
	DateError string

	// Onboarding: a new firm sees its first steps instead of zeros (UX-06).
	Setup setupSteps

	// NoAlertRecipient is set while the firm's alerts go to nobody, so the
	// dashboard can say so instead of failing silently (UX-19, PO-14).
	NoAlertRecipient bool
	IsOwner          bool

	// BackURL brings a crossing entered from a row back to this same view.
	BackURL string

	Query         string
	Severity      string
	FiltersActive bool
}

// setupSteps are the first steps of a new firm, shown on the dashboard
// until the firm has drivers and at least one stay.
type setupSteps struct {
	HasDrivers bool
	HasStays   bool
	HasLinked  bool
}

// Show reports whether the checklist replaces (or tops) the dashboard.
func (s setupSteps) Show() bool { return !s.HasDrivers || !s.HasStays }

// Left counts the steps still to do.
func (s setupSteps) Left() int {
	n := 0
	for _, done := range []bool{s.HasDrivers, s.HasStays, s.HasLinked} {
		if !done {
			n++
		}
	}
	return n
}

// kpiCard is one status card on the dashboard. Clicking it filters the
// table to that status; clicking the active card clears the filter.
type kpiCard struct {
	Level  status.Level
	Range  string
	Count  int
	Active bool
	URL    string
}

// kpiURL keeps the search and the chosen day, so a card only changes the
// status filter.
func kpiURL(query, severity string, day time.Time, isToday bool) string {
	v := url.Values{}
	if query != "" {
		v.Set("q", query)
	}
	if severity != "" {
		v.Set("ozbiljnost", severity)
	}
	if !isToday {
		v.Set("na_dan", day.Format("2006-01-02"))
	}
	if len(v) == 0 {
		return "/pregled"
	}
	return "/pregled?" + v.Encode()
}

func (s *Server) handlePregled(w http.ResponseWriter, r *http.Request) {
	t, _ := tenant.FromContext(r.Context())
	today := clock.Today()

	settings, _ := t.ParsedSettings()
	thresholds := status.ThresholdsFor(settings)

	day, dateErr := parseAsOf(r.URL.Query().Get("na_dan"), today)
	query := r.URL.Query().Get("q")
	severity := r.URL.Query().Get("ozbiljnost")
	if !validSeverity(severity) {
		severity = ""
	}

	drivers, err := s.Store.ListDrivers(r.Context(), t.ID, true)
	if err != nil {
		s.serverError(w, r)
		return
	}
	allStays, err := s.Store.ListStaysForTenant(r.Context(), t.ID)
	if err != nil {
		s.serverError(w, r)
		return
	}

	plans, err := s.Store.ListPlannedStaysFrom(r.Context(), t.ID, today)
	if err != nil {
		s.serverError(w, r)
		return
	}

	statuses := status.ComputeAll(drivers, status.AsOf(allStays, day, today), day)
	for i := range statuses {
		statuses[i].ApplyThresholds(thresholds)
	}

	data := pregledData{
		baseData:      s.base(r, "pregled"),
		TotalDrivers:  len(statuses),
		Thresholds:    thresholds,
		AsOf:          day,
		IsToday:       day.Equal(today),
		BackURL:       r.URL.RequestURI(),
		IsFuture:      day.After(today),
		DateError:     dateErr,
		Query:         query,
		Severity:      severity,
		FiltersActive: query != "" || severity != "",
	}
	data.Plans = status.SummarizeAll(allStays, plans, today)
	for _, ps := range data.Plans {
		if ps.Problem != nil {
			data.PlanProblems++
		}
	}
	data.NoAlertRecipient = len(drivers) > 0 && settings.TelegramAlertChatID == 0
	data.IsOwner = data.User != nil && data.User.Role == "owner"
	data.Setup = setupSteps{HasDrivers: len(drivers) > 0, HasStays: len(allStays) > 0}
	for _, d := range drivers {
		if d.TelegramChatID != nil {
			data.Setup.HasLinked = true
			break
		}
	}

	counts := map[status.Level]int{}
	for _, st := range statuses {
		if st.InSchengen {
			data.InSchengenNow++
		}
		counts[st.Badge.Level]++
		if data.IsToday && st.InSchengen && st.Remaining <= 0 {
			data.UrgentDrivers = append(data.UrgentDrivers, st)
		}
	}
	for _, l := range status.Levels {
		card := kpiCard{Level: l, Range: thresholds.Range(l), Count: counts[l], Active: severity == l.Key()}
		next := l.Key()
		if card.Active {
			next = ""
		}
		card.URL = kpiURL(query, next, day, data.IsToday)
		data.KPIs = append(data.KPIs, card)
	}
	data.Statuses = filterStatuses(statuses, query, severity)

	s.render(w, "pregled.html", data)
}
