package web

import (
	"net/http"

	"shengen/internal/clock"
	"shengen/internal/status"
	"shengen/internal/tenant"
)

type pregledData struct {
	baseData
	Statuses        []status.DriverStatus
	TotalDrivers    int
	InSchengenNow   int
	Under15Count    int
	Free30PlusCount int
	UrgentDrivers   []status.DriverStatus
}

func (s *Server) handlePregled(w http.ResponseWriter, r *http.Request) {
	t, _ := tenant.FromContext(r.Context())
	today := clock.Today()

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

	statuses := status.ComputeAll(drivers, allStays, today)

	data := pregledData{baseData: s.base(r, "pregled"), Statuses: statuses, TotalDrivers: len(statuses)}
	for _, st := range statuses {
		if st.InSchengen {
			data.InSchengenNow++
		}
		if st.Remaining < 15 {
			data.Under15Count++
		}
		if st.Remaining > 30 {
			data.Free30PlusCount++
		}
		if st.InSchengen && st.Remaining <= 0 {
			data.UrgentDrivers = append(data.UrgentDrivers, st)
		}
	}

	s.render(w, "pregled.html", data)
}
