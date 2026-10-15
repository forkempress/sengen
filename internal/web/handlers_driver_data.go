package web

import (
	"encoding/csv"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"shengen/internal/auth"
	"shengen/internal/store"
	"shengen/internal/tenant"
)

// The firm, as controller, answers its drivers' requests: a copy of
// everything stored about a driver (CSV) and permanent deletion. Both are
// for the owner only and recorded in the audit log without personal data.

// handleDriverExport serves everything stored about one driver as CSV.
func (s *Server) handleDriverExport(w http.ResponseWriter, r *http.Request) {
	t, _ := tenant.FromContext(r.Context())
	u, _ := auth.UserFromContext(r.Context())
	id, err := pathInt64(r, "id")
	if err != nil {
		s.notFound(w, r)
		return
	}
	ex, err := s.Store.ExportDriver(r.Context(), t.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r)
		return
	}
	if err := s.Store.RecordAudit(r.Context(), t.ID, "user", &u.ID, "driver", id, "export", nil, nil); err != nil {
		log.Printf("audit driver export: %v", err)
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="dani90-vozac-%d.csv"`, id))
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte{0xEF, 0xBB, 0xBF}) // Excel then shows č, ć, š, ž, đ
	cw := csv.NewWriter(w)
	cw.WriteAll(driverExportRows(ex))
}

// driverExportRows lays the export out as one table: a row for the driver,
// then one per stay and one per plan.
func driverExportRows(ex *store.DriverExport) [][]string {
	rows := [][]string{{"zapis", "vozač", "vozilo", "od", "do", "tip", "izvor", "napomena", "uneto"}}
	d := ex.Driver
	telegram := "Telegram: nije povezan"
	if d.TelegramChatID != nil {
		telegram = "Telegram: povezan"
		if ex.PrivacyAckAt != nil {
			telegram += ", obaveštenje potvrđeno " + ex.PrivacyAckAt.Format("02.01.2006")
		}
	}
	active := "aktivan"
	if !d.Active {
		active = "deaktiviran"
	}
	rows = append(rows, []string{"vozač", d.Name, d.VehicleLabel, "", "", active, "", telegram, ex.CreatedAt.Format("02.01.2006")})
	for _, st := range ex.Stays {
		exit := ""
		if st.Exit != nil {
			exit = st.Exit.Format("02.01.2006")
		}
		typ := "posao"
		if st.Type == "private" {
			typ = "privatno"
		}
		rows = append(rows, []string{"boravak", d.Name, "", st.Entry.Format("02.01.2006"), exit, typ, sourceLabel(st.Source), st.Note, st.CreatedAt.Format("02.01.2006")})
	}
	for _, p := range ex.Plans {
		rows = append(rows, []string{"plan", d.Name, "", p.From.Format("02.01.2006"), p.To.Format("02.01.2006"), "", "", p.Note, p.CreatedAt.Format("02.01.2006")})
	}
	for _, row := range rows {
		for i := range row {
			row[i] = csvSafe(row[i])
		}
	}
	return rows
}

// csvSafe keeps a spreadsheet from running a value as a formula (a driver
// named "=HYPERLINK(...)" must stay text).
func csvSafe(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}

// handleDriverDeletePermanent deletes a driver and everything about him.
// The owner confirms by typing the driver's name: unlike deactivation, this
// cannot be undone.
func (s *Server) handleDriverDeletePermanent(w http.ResponseWriter, r *http.Request) {
	driver, tenantID, ok := s.loadDriverOr404(w, r)
	if !ok {
		return
	}
	u, _ := auth.UserFromContext(r.Context())
	if !strings.EqualFold(strings.TrimSpace(r.FormValue("potvrda")), strings.TrimSpace(driver.Name)) {
		s.redirectErr(w, r, "/vozaci/"+strconv.FormatInt(driver.ID, 10)+"#podaci-vozaca", "Vozač nije obrisan: za potvrdu upišite njegovo ime tačno kako je napisano.")
		return
	}
	if err := s.Store.DeleteDriverPermanently(r.Context(), tenantID, driver.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.notFound(w, r)
			return
		}
		s.serverError(w, r)
		return
	}
	// Only the id: the name is exactly what was just erased.
	if err := s.Store.RecordAudit(r.Context(), tenantID, "user", &u.ID, "driver", driver.ID, "delete_permanent", nil, nil); err != nil {
		log.Printf("audit driver delete: %v", err)
	}
	s.redirectOk(w, r, "/podesavanja/vozaci", "Vozač i svi njegovi podaci su trajno obrisani.")
}
