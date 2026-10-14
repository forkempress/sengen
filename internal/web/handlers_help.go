package web

import (
	"net/http"

	"shengen/internal/status"
	"shengen/internal/tenant"
)

type helpData struct {
	baseData
	// The colour bands are explained with the tenant's own numbers, so the
	// help never contradicts what the dashboard shows.
	Thresholds status.Thresholds
	IsOwner    bool
}

func (s *Server) handleHelp(w http.ResponseWriter, r *http.Request) {
	t, _ := tenant.FromContext(r.Context())
	settings, _ := t.ParsedSettings()

	data := helpData{baseData: s.base(r, "pomoc"), Thresholds: status.ThresholdsFor(settings)}
	data.IsOwner = data.User != nil && data.User.Role == "owner"
	s.render(w, "pomoc.html", data)
}
