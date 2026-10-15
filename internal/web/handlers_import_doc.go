package web

import (
	"bytes"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"shengen/internal/auth"
	"shengen/internal/docread"
	"shengen/internal/importer"
	"shengen/internal/store"
	"shengen/internal/tenant"
)

// Reading a scan costs money per call, so each firm gets a few a day.
const (
	docReadLimitWindow    = 24 * time.Hour
	docReadLimitPerTenant = 10
)

// docReadDeadline is how long the server may take to answer a scan upload:
// the model call plus reading a 10 MB body (set in limitBody).
const docReadDeadline = docread.Timeout + 20*time.Second

// handleImportDocument reads a scanned history (PDF or photo) with Claude
// and shows the rows in the same preview as a CSV upload. Nothing is
// written until the user confirms.
func (s *Server) handleImportDocument(w http.ResponseWriter, r *http.Request) {
	if s.DocReader == nil {
		s.notFound(w, r)
		return
	}
	t, _ := tenant.FromContext(r.Context())
	fail := func(msg string) {
		data := s.importPage(r)
		data.FlashErr = msg
		s.render(w, "import.html", data)
	}
	tooLarge := func() {
		to := "mejl"
		if s.FeedbackEmail != "" {
			to = s.FeedbackEmail
		}
		fail("Dokument je prevelik za automatsko čitanje (najviše " + strconv.Itoa(docread.MaxPages) + " strana, " +
			strconv.Itoa(docread.MaxDrivers) + " vozača i " + strconv.Itoa(docread.MaxStays) +
			" boravaka). Pošaljite nam ga na " + to + " i unećemo ga mi.")
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			tooLarge()
			return
		}
		fail("Izaberite PDF ili sliku.")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		fail("Greška pri čitanju fajla.")
		return
	}
	mediaType := http.DetectContentType(data)
	if !docread.Supported(mediaType) {
		fail("Podržani su PDF, JPG, PNG i WEBP.")
		return
	}
	if err := docread.CheckFile(data, mediaType); err != nil {
		tooLarge()
		return
	}
	if !s.docReadLimiter.Take(strconv.FormatInt(t.ID, 10)) {
		fail("Danas ste već učitali " + strconv.Itoa(docReadLimitPerTenant) + " dokumenata. Pokušajte sutra ili nam pošaljite dokument na mejl.")
		return
	}

	existing, err := s.loadExistingDriversForImport(r, t.ID)
	if err != nil {
		s.serverError(w, r)
		return
	}
	names := make([]string, len(existing))
	for i, d := range existing {
		names[i] = d.Name
	}
	// From a driver's page the whole document is that driver's history:
	// the model gets only his name and every row is put on him.
	var only *store.Driver
	if id, err := strconv.ParseInt(r.FormValue("driver_id"), 10, 64); err == nil {
		d, err := s.Store.GetDriver(r.Context(), t.ID, id)
		if err != nil {
			s.notFound(w, r)
			return
		}
		only, names = d, []string{d.Name}
	}

	res, err := s.DocReader.Read(r.Context(), data, mediaType, names)
	if only != nil {
		for i := range res.Stays {
			res.Stays[i].Driver = only.Name
		}
	}
	// Every call is recorded, failed ones too: the superadmins hear about
	// each one and what it cost.
	record := func(status string, rowsValid int) int64 {
		var userID *int64
		if u, ok := auth.UserFromContext(r.Context()); ok {
			userID = &u.ID
		}
		id, err := s.Store.RecordDocumentImport(r.Context(), store.DocumentImport{
			TenantID: t.ID, UserID: userID, Status: status,
			Model: res.Usage.Model, InputTokens: res.Usage.InputTokens, OutputTokens: res.Usage.OutputTokens,
			CostMicroUSD: res.Usage.CostMicroUSD,
			StaysRead:    len(res.Stays), DriversRead: docread.DriverCount(res.Stays), RowsValid: rowsValid,
		})
		if err != nil {
			log.Printf("import dokumenta (firma %d): %v", t.ID, err)
		}
		return id
	}
	if errors.Is(err, docread.ErrTooLarge) {
		record(store.DocImportTooLarge, 0)
		tooLarge()
		return
	}
	if err != nil {
		log.Printf("import dokumenta (firma %d): %v", t.ID, err)
		record(store.DocImportError, 0)
		fail("Dokument trenutno ne možemo da pročitamo. Pokušajte ponovo za par minuta ili nam ga pošaljite na mejl.")
		return
	}
	if err := docread.CheckResult(res); err != nil {
		record(store.DocImportTooLarge, 0)
		tooLarge()
		return
	}
	if len(res.Stays) == 0 {
		record(store.DocImportEmpty, 0)
		msg := "U dokumentu nismo našli nijedan boravak."
		if res.Reason != "" {
			msg += " " + res.Reason
		}
		fail(msg)
		return
	}

	rows, err := importer.ParseAllowingNewDrivers(bytes.NewReader(docread.CSV(res.Stays)), existing)
	if err != nil {
		log.Printf("import dokumenta (firma %d): %v", t.ID, err)
		record(store.DocImportError, 0)
		s.serverError(w, r)
		return
	}
	valid := 0
	for _, row := range rows {
		if row.Valid() {
			valid++
		}
	}
	importID := record(store.DocImportOK, valid)
	// docread.CSV puts stay i on line i+2 (after the header).
	uncertain := map[int]bool{}
	for i, st := range res.Stays {
		if st.Uncertain {
			uncertain[i+2] = true
		}
	}
	var driverID int64
	if only != nil {
		driverID = only.ID
	}
	s.renderImportPreview(w, r, rows, uncertain, importID, driverID)
}
