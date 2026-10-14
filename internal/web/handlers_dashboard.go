package web

import (
	"net/http"
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

	TotalDrivers     int
	InSchengenNow    int
	BelowRedCount    int
	AboveOrangeCount int
	Thresholds       status.Thresholds

	AsOf      time.Time
	IsToday   bool
	IsFuture  bool
	DateError string

	Query         string
	Severity      string
	FiltersActive bool
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
		http.Error(w, "greška servera", http.StatusInternalServerError)
		return
	}
	allStays, err := s.Store.ListStaysForTenant(r.Context(), t.ID)
	if err != nil {
		http.Error(w, "greška servera", http.StatusInternalServerError)
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
		IsFuture:      day.After(today),
		DateError:     dateErr,
		Query:         query,
		Severity:      severity,
		FiltersActive: query != "" || severity != "",
	}
	for _, st := range statuses {
		if st.InSchengen {
			data.InSchengenNow++
		}
		if st.Remaining < thresholds.Red {
			data.BelowRedCount++
		}
		if st.Remaining > thresholds.Orange {
			data.AboveOrangeCount++
		}
		if data.IsToday && st.InSchengen && st.Remaining <= 0 {
			data.UrgentDrivers = append(data.UrgentDrivers, st)
		}
	}
	data.Statuses = filterStatuses(statuses, query, severity)

	s.render(w, "pregled.html", data)
}
