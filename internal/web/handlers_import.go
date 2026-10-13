package web

import (
	"net/http"
	"strconv"
	"time"

	"shengen/internal/auth"
	"shengen/internal/importer"
	"shengen/internal/store"
	"shengen/internal/tenant"
)

type importPageData struct {
	baseData
	Rows      []importer.Row
	ValidRows []importer.Row
	HasFile   bool
}

func (s *Server) handleImportPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, "import.html", importPageData{baseData: s.base(r, "import")})
}

func (s *Server) loadExistingDriversForImport(r *http.Request, tenantID int64) ([]importer.ExistingDriver, error) {
	drivers, err := s.Store.ListDrivers(r.Context(), tenantID, true)
	if err != nil {
		return nil, err
	}
	out := make([]importer.ExistingDriver, len(drivers))
	for i, d := range drivers {
		stays, err := s.Store.ListStaysByDriver(r.Context(), tenantID, d.ID)
		if err != nil {
			return nil, err
		}
		existingStays := make([]importer.ExistingStay, len(stays))
		for j, st := range stays {
			existingStays[j] = importer.ExistingStay{Entry: st.Entry, Exit: st.Exit}
		}
		out[i] = importer.ExistingDriver{ID: d.ID, Name: d.Name, Stays: existingStays}
	}
	return out, nil
}

func (s *Server) handleImportPreview(w http.ResponseWriter, r *http.Request) {
	t, _ := tenant.FromContext(r.Context())

	file, _, err := r.FormFile("file")
	if err != nil {
		data := importPageData{baseData: s.base(r, "import")}
		data.FlashErr = "Izaberite CSV fajl."
		s.render(w, "import.html", data)
		return
	}
	defer file.Close()

	existing, err := s.loadExistingDriversForImport(r, t.ID)
	if err != nil {
		http.Error(w, "greška servera", http.StatusInternalServerError)
		return
	}

	rows, err := importer.Parse(file, existing)
	if err != nil {
		data := importPageData{baseData: s.base(r, "import")}
		data.FlashErr = "Greška pri čitanju fajla: " + err.Error()
		s.render(w, "import.html", data)
		return
	}

	data := importPageData{baseData: s.base(r, "import"), Rows: rows, HasFile: true}
	for _, row := range rows {
		if row.Valid() {
			data.ValidRows = append(data.ValidRows, row)
		}
	}
	s.render(w, "import.html", data)
}

func (s *Server) handleImportConfirm(w http.ResponseWriter, r *http.Request) {
	t, _ := tenant.FromContext(r.Context())
	u, _ := auth.UserFromContext(r.Context())

	if err := r.ParseForm(); err != nil {
		http.Error(w, "neispravan zahtev", http.StatusBadRequest)
		return
	}
	driverIDs := r.Form["row_driver_id"]
	entries := r.Form["row_entry"]
	exits := r.Form["row_exit"]
	types := r.Form["row_type"]
	notes := r.Form["row_note"]

	if len(driverIDs) == 0 {
		http.Redirect(w, r, "/import?err=Nema+redova+za+uvoz.", http.StatusSeeOther)
		return
	}

	err := s.Store.WithTx(r.Context(), func(txStore *store.Store) error {
		for i := range driverIDs {
			driverID, err := strconv.ParseInt(driverIDs[i], 10, 64)
			if err != nil {
				return err
			}
			entry, err := time.Parse("2006-01-02", entries[i])
			if err != nil {
				return err
			}
			var exit *time.Time
			if exits[i] != "" {
				e, err := time.Parse("2006-01-02", exits[i])
				if err != nil {
					return err
				}
				exit = &e
			}
			st, err := txStore.CreateStay(r.Context(), t.ID, driverID, entry, exit, types[i], "import", notes[i], &u.ID)
			if err != nil {
				return err
			}
			txStore.RecordAudit(r.Context(), t.ID, "user", &u.ID, "stay", st.ID, "create", nil, nil)
		}
		return nil
	})
	if err != nil {
		http.Redirect(w, r, "/import?err=Uvoz+neuspešan:+"+err.Error(), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/import?ok=Uvoz+završen+("+strconv.Itoa(len(driverIDs))+"+boravaka).", http.StatusSeeOther)
}
