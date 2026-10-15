package web

import (
	"net/http"
	"time"

	"shengen/internal/clock"
	"shengen/internal/status"
	"shengen/internal/store"
	"shengen/internal/tenant"
)

// reportStay is one stay in the printable report, with the days it adds to
// the current 180-day window.
type reportStay struct {
	store.Stay
	DaysInWindow int
}

type driverReportData struct {
	baseData
	Driver     store.Driver
	FirmName   string
	Status     status.DriverStatus
	Today      time.Time
	WindowFrom time.Time
	// Stays: those that reach into the window, newest first.
	Stays []reportStay
	// Earlier is how many older stays the report leaves out; they no longer
	// count toward 90/180.
	Earlier int
}

// handleDriverReport is a one-page report on a driver's days in Schengen,
// for printing or saving as PDF from the browser: for the driver to carry,
// for a check at the border, or for the firm's records.
func (s *Server) handleDriverReport(w http.ResponseWriter, r *http.Request) {
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
	data := driverReportData{
		baseData:   s.base(r, "vozaci"),
		Driver:     *driver,
		Status:     status.Compute(*driver, stays, today),
		Today:      today,
		WindowFrom: today.AddDate(0, 0, -179),
	}
	if t, ok := tenant.FromContext(r.Context()); ok {
		data.FirmName = t.Name
		settings, _ := t.ParsedSettings()
		data.Status.ApplyThresholds(status.ThresholdsFor(settings))
	}
	for i := len(stays) - 1; i >= 0; i-- {
		st := stays[i]
		if n := daysInWindow(st, data.WindowFrom, today); n > 0 {
			data.Stays = append(data.Stays, reportStay{Stay: st, DaysInWindow: n})
		} else {
			data.Earlier++
		}
	}
	s.render(w, "vozac_izvestaj.html", data)
}

// daysInWindow counts a stay's days within [from, today]; an open stay runs
// through today.
func daysInWindow(st store.Stay, from, today time.Time) int {
	start, end := st.Entry, today
	if st.Exit != nil && st.Exit.Before(today) {
		end = *st.Exit
	}
	if start.Before(from) {
		start = from
	}
	if end.Before(start) {
		return 0
	}
	return int(end.Sub(start).Hours()/24+0.5) + 1
}
