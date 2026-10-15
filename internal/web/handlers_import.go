package web

import (
	"log"
	"net/http"
	"strconv"
	"strings"
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
	// FromDocument marks a preview read from a scan; Uncertain holds the
	// line numbers the model was not sure it read correctly.
	FromDocument bool
	Uncertain    map[int]bool
	// DocImportID ties the confirm form back to the recorded read.
	DocImportID int64
	// NewDrivers are the names of valid rows that match no driver, in order
	// of first appearance, each offered as "add as a new driver".
	NewDrivers []string
	// DriverID is set when the scan was started on a driver's page.
	DriverID int64
}

func (s *Server) importPage(r *http.Request) importPageData {
	return importPageData{baseData: s.base(r, "import"), Sample: sampleCSV(s.sampleNames(r), clock.Today())}
}

func (s *Server) handleImportPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, "import.html", s.importPage(r))
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
	s.renderImportPreview(w, r, rows, nil, 0, 0)
}

func (s *Server) renderImportPreview(w http.ResponseWriter, r *http.Request, rows []importer.Row, uncertain map[int]bool, docImportID, driverID int64) {
	data := s.importPage(r)
	data.Rows, data.HasFile = rows, true
	data.FromDocument, data.Uncertain, data.DocImportID = uncertain != nil, uncertain, docImportID
	data.DriverID = driverID
	seen := map[string]bool{}
	for _, row := range rows {
		if !row.Valid() {
			continue
		}
		data.ValidRows = append(data.ValidRows, row)
		if key := importer.NameKey(row.DriverName); row.NewDriver && !seen[key] {
			seen[key] = true
			data.NewDrivers = append(data.NewDrivers, row.DriverName)
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
	// A scan started on a driver's page returns there.
	back := "/import"
	if id, err := strconv.ParseInt(r.Form.Get("driver_id"), 10, 64); err == nil {
		back = "/vozaci/" + strconv.FormatInt(id, 10)
	}
	driverIDs := r.Form["row_driver_id"]
	entries := r.Form["row_entry"]
	exits := r.Form["row_exit"]
	types := r.Form["row_type"]
	notes := r.Form["row_note"]
	// row_new_driver names the driver to create for a row whose driver did
	// not exist (document import only); CSV imports do not send it.
	newNames := r.Form["row_new_driver"]

	if len(driverIDs) == 0 {
		s.redirectErr(w, r, back, "Nema redova za uvoz.")
		return
	}
	n := len(driverIDs)
	if len(entries) != n || len(exits) != n || len(types) != n || len(notes) != n || (newNames != nil && len(newNames) != n) {
		s.redirectErr(w, r, back, "Neispravan zahtev, učitajte fajl ponovo.")
		return
	}
	// New drivers the user kept checked; rows of an unchecked one are skipped.
	addDriver := map[string]bool{}
	for _, name := range r.Form["add_driver"] {
		addDriver[importer.NameKey(name)] = true
	}

	written, created := 0, 0
	usedDrivers := map[int64]bool{}
	err := s.Store.WithTx(r.Context(), func(txStore *store.Store) error {
		existing, err := txStore.ListDrivers(r.Context(), t.ID, true)
		if err != nil {
			return err
		}
		byName := make(map[string]int64, len(existing))
		for _, d := range existing {
			byName[importer.NameKey(d.Name)] = d.ID
		}
		for i := range driverIDs {
			driverID, err := strconv.ParseInt(driverIDs[i], 10, 64)
			if err != nil {
				return err
			}
			if newNames != nil && strings.TrimSpace(newNames[i]) != "" {
				name := strings.TrimSpace(newNames[i])
				key := importer.NameKey(name)
				if !addDriver[key] {
					continue
				}
				// Reuse a driver of that name (one created for an earlier
				// row, or added meanwhile) instead of creating a twin.
				id, ok := byName[key]
				if !ok {
					d, err := txStore.CreateDriver(r.Context(), t.ID, name, "")
					if err != nil {
						return err
					}
					id = d.ID
					byName[key] = id
					created++
				}
				driverID = id
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
			written++
			usedDrivers[driverID] = true
		}
		return nil
	})
	if err != nil {
		s.redirectErr(w, r, back, "Uvoz neuspešan: "+err.Error())
		return
	}
	if written == 0 {
		s.redirectErr(w, r, back, "Ništa nije upisano: niste izabrali nijednog vozača.")
		return
	}

	if docID, err := strconv.ParseInt(r.Form.Get("doc_import_id"), 10, 64); err == nil {
		if err := s.Store.ConfirmDocumentImport(r.Context(), t.ID, docID, written, len(usedDrivers)); err != nil {
			log.Printf("import dokumenta (firma %d): %v", t.ID, err)
		}
	}

	msg := "Uvoz završen (" + strconv.Itoa(written) + " boravaka"
	if created > 0 {
		msg += ", " + strconv.Itoa(created) + " novih vozača"
	}
	s.redirectOk(w, r, back, msg+").")
}
