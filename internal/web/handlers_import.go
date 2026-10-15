package web

import (
	"net/http"
	"strconv"
	"time"

	"shengen/internal/auth"
	"shengen/internal/clock"
	"shengen/internal/importer"
	"shengen/internal/store"
	"shengen/internal/tenant"
)

type importPageData struct {
	baseData
	Rows      []importer.Row
	ValidRows []importer.Row
	HasFile   bool
	// Sample is the example file, shown as a table above the upload.
	Sample [][]string
	// DocEnabled offers the PDF/photo upload (a Claude API key is set).
	DocEnabled bool
	// FromDocument marks a preview read from a scan; Uncertain holds the
	// line numbers the model was not sure it read correctly.
	FromDocument bool
	Uncertain    map[int]bool
}

func (s *Server) importPage(r *http.Request) importPageData {
	return importPageData{baseData: s.base(r, "import"), DocEnabled: s.DocReader != nil}
}

func (s *Server) handleImportPage(w http.ResponseWriter, r *http.Request) {
	data := s.importPage(r)
	data.Sample = sampleCSV(s.sampleNames(r), clock.Today())
	s.render(w, "import.html", data)
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
		data := s.importPage(r)
		data.FlashErr = "Izaberite CSV fajl."
		s.render(w, "import.html", data)
		return
	}
	defer file.Close()

	existing, err := s.loadExistingDriversForImport(r, t.ID)
	if err != nil {
		s.serverError(w, r)
		return
	}

	rows, err := importer.Parse(file, existing)
	if err != nil {
		data := s.importPage(r)
		data.FlashErr = "Greška pri čitanju fajla: " + err.Error()
		s.render(w, "import.html", data)
		return
	}
	s.renderImportPreview(w, r, rows, nil)
}

func (s *Server) renderImportPreview(w http.ResponseWriter, r *http.Request, rows []importer.Row, uncertain map[int]bool) {
	data := s.importPage(r)
	data.Rows, data.HasFile = rows, true
	data.FromDocument, data.Uncertain = uncertain != nil, uncertain
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
		s.redirectErr(w, r, "/import", "Nema redova za uvoz.")
		return
	}
	n := len(driverIDs)
	if len(entries) != n || len(exits) != n || len(types) != n || len(notes) != n {
		s.redirectErr(w, r, "/import", "Neispravan zahtev, učitajte fajl ponovo.")
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
		s.redirectErr(w, r, "/import", "Uvoz neuspešan: "+err.Error())
		return
	}

	s.redirectOk(w, r, "/import", "Uvoz završen ("+strconv.Itoa(len(driverIDs))+" boravaka).")
}
