package web

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"shengen/internal/auth"
	"shengen/internal/clock"
	"shengen/internal/db"
	"shengen/internal/docread"
	"shengen/internal/store"
	"shengen/internal/telegram"
)

// fakeReader stands in for Claude: it returns a fixed result and records
// what it was given.
type fakeReader struct {
	res       docread.Result
	err       error
	calls     int
	mediaType string
	names     []string
}

func (f *fakeReader) Read(_ context.Context, _ []byte, mediaType string, names []string) (docread.Result, error) {
	f.calls++
	f.mediaType, f.names = mediaType, names
	return f.res, f.err
}

// docApp is twoFirmApp with a document reader on the server (nil: none).
func docApp(t *testing.T, reader docread.Reader) (*testApp, firm, firm) {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	st := store.New(conn)
	srv := New(st, auth.New(st, true), telegram.New(st, telegram.Config{}))
	if reader != nil {
		srv.DocReader = reader
	}
	app := &testApp{t: t, handler: srv.Routes(), st: st}
	a := app.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana Prva", 30)
	b := app.addFirm("Druga Firma", "druga", "vlasnik@druga.rs", "Boris Drugi", 50)
	return app, a, b
}

// uploadDocument posts a scan to /import/dokument the way the browser does.
func (s *session) uploadDocument(content []byte) *httptest.ResponseRecorder {
	s.app.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("csrf_token", s.csrfToken())
	fw, _ := mw.CreateFormFile("file", "evidencija.pdf")
	fw.Write(content)
	mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/import/dokument", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.AddCookie(s.cookie)
	w := httptest.NewRecorder()
	s.app.handler.ServeHTTP(w, r)
	return w
}

var fakePDF = []byte("%PDF-1.4\n1 0 obj << /Type /Page >> endobj\n%%EOF")

func TestImportDocument_PreviewWithoutWriting(t *testing.T) {
	day := clock.Today().AddDate(0, 0, -20).Format("02.01.2006")
	day2 := clock.Today().AddDate(0, 0, -15).Format("02.01.2006")
	reader := &fakeReader{res: docread.Result{Stays: []docread.Stay{
		{Driver: "Ana Prva", Entry: day, Exit: day2, Type: "posao"},
		{Driver: "Nepoznati Vozač", Entry: day, Exit: day2, Type: "posao", Uncertain: true},
	}}}
	app, a, _ := docApp(t, reader)
	sa := app.mustLogin(a)
	before := app.stayCount(a.tenant.ID, a.driver.ID)

	w := sa.uploadDocument(fakePDF)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"Pregled (2 redova, 2 ispravnih)", "proverite", "novi vozač", `name="add_driver" value="Nepoznati Vozač" checked`, "Potvrdi uvoz (2 boravaka)"} {
		if !strings.Contains(body, want) {
			t.Errorf("preview is missing %q", want)
		}
	}
	if reader.mediaType != "application/pdf" {
		t.Errorf("media type %q, want application/pdf", reader.mediaType)
	}
	if len(reader.names) != 1 || reader.names[0] != "Ana Prva" {
		t.Errorf("model got driver names %v, want only this firm's", reader.names)
	}
	if got := app.stayCount(a.tenant.ID, a.driver.ID); got != before {
		t.Errorf("preview wrote stays (%d -> %d)", before, got)
	}
}

func TestImportDocument_OffWithoutKey(t *testing.T) {
	app, a, _ := docApp(t, nil)
	sa := app.mustLogin(a)
	if strings.Contains(sa.get("/import").Body.String(), "/import/dokument") {
		t.Error("document upload offered without a reader")
	}
	if w := sa.uploadDocument(fakePDF); w.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404", w.Code)
	}
}

func TestImportDocument_UnsupportedFileNeverReachesModel(t *testing.T) {
	reader := &fakeReader{}
	app, a, _ := docApp(t, reader)
	w := app.mustLogin(a).uploadDocument([]byte("vozač,ulaz,izlaz,tip\n"))
	if !strings.Contains(w.Body.String(), "Podržani su PDF") {
		t.Error("no message for an unsupported file")
	}
	if reader.calls != 0 {
		t.Errorf("model called %d times for a text file", reader.calls)
	}
}

func TestImportDocument_TooManyDriversAsksForEmail(t *testing.T) {
	var stays []docread.Stay
	for i := range docread.MaxDrivers + 1 {
		stays = append(stays, docread.Stay{Driver: "Vozač " + string(rune('A'+i)), Entry: "01.01.2026", Type: "posao"})
	}
	app, a, _ := docApp(t, &fakeReader{res: docread.Result{Stays: stays}})
	w := app.mustLogin(a).uploadDocument(fakePDF)
	if !strings.Contains(w.Body.String(), "unećemo ga mi") {
		t.Error("no hand-off message for a document over the limit")
	}
}

func TestImportDocument_NothingFoundShowsReason(t *testing.T) {
	app, a, _ := docApp(t, &fakeReader{res: docread.Result{Reason: "Ovo je račun za gorivo."}})
	body := app.mustLogin(a).uploadDocument(fakePDF).Body.String()
	if !strings.Contains(body, "Ovo je račun za gorivo.") {
		t.Error("model's reason not shown")
	}
}

func TestImportDocument_DailyLimitPerFirm(t *testing.T) {
	reader := &fakeReader{res: docread.Result{Reason: "prazno"}}
	app, a, b := docApp(t, reader)
	sa := app.mustLogin(a)
	for range docReadLimitPerTenant {
		sa.uploadDocument(fakePDF)
	}
	if !strings.Contains(sa.uploadDocument(fakePDF).Body.String(), "Pokušajte sutra") {
		t.Error("no limit after the daily quota")
	}
	if reader.calls != docReadLimitPerTenant {
		t.Errorf("model called %d times, want %d", reader.calls, docReadLimitPerTenant)
	}
	app.mustLogin(b).uploadDocument(fakePDF)
	if reader.calls != docReadLimitPerTenant+1 {
		t.Error("one firm's quota blocked another firm")
	}
}

var docImportIDRe = regexp.MustCompile(`name="doc_import_id" value="(\d+)"`)

func TestImportDocument_RecordsCostAndConfirmation(t *testing.T) {
	day := clock.Today().AddDate(0, 0, -20).Format("02.01.2006")
	day2 := clock.Today().AddDate(0, 0, -15).Format("02.01.2006")
	reader := &fakeReader{res: docread.Result{
		Stays: []docread.Stay{{Driver: "ana prva", Entry: day, Exit: day2, Type: "posao"}},
		Usage: docread.Usage{Model: "claude-opus-5-5", InputTokens: 1000, OutputTokens: 100, CostMicroUSD: 6000},
	}}
	app, a, b := docApp(t, reader)
	sa := app.mustLogin(a)
	ctx := context.Background()

	body := sa.uploadDocument(fakePDF).Body.String()
	m := docImportIDRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("confirm form has no doc_import_id")
	}
	reads, err := app.st.ListDocumentImportsSince(ctx, clock.Now().Add(-time.Hour))
	if err != nil || len(reads) != 1 {
		t.Fatalf("recorded reads: %v, %v", reads, err)
	}
	d := reads[0]
	if d.TenantID != a.tenant.ID || d.Status != store.DocImportOK || d.CostMicroUSD != 6000 || d.StaysRead != 1 || d.DriversRead != 1 || d.RowsValid != 1 {
		t.Errorf("recorded %+v", d)
	}

	confirm := url.Values{
		"doc_import_id": {m[1]}, "row_driver_id": {id(a.driver.ID)},
		"row_entry": {clock.Today().AddDate(0, 0, -20).Format("2006-01-02")},
		"row_exit":  {clock.Today().AddDate(0, 0, -15).Format("2006-01-02")},
		"row_type":  {"work"}, "row_note": {""},
	}
	// Another firm posting this id does not mark it confirmed.
	sb := app.mustLogin(b)
	bConfirm := url.Values{}
	for k, v := range confirm {
		bConfirm[k] = v
	}
	bConfirm.Set("row_driver_id", id(b.driver.ID))
	sb.post("/import/potvrdi", bConfirm)
	if got, _ := app.st.ListDocumentImportsConfirmedSince(ctx, clock.Now().Add(-time.Hour)); len(got) != 0 {
		t.Errorf("another firm confirmed this read: %+v", got)
	}

	sa.post("/import/potvrdi", confirm)
	got, _ := app.st.ListDocumentImportsConfirmedSince(ctx, clock.Now().Add(-time.Hour))
	if len(got) != 1 || got[0].StaysWritten != 1 || got[0].DriversWritten != 1 {
		t.Errorf("confirmed reads: %+v", got)
	}
}

func TestImportDocument_FailedCallIsRecorded(t *testing.T) {
	app, a, _ := docApp(t, &fakeReader{err: errors.New("mreža")})
	app.mustLogin(a).uploadDocument(fakePDF)
	reads, _ := app.st.ListDocumentImportsSince(context.Background(), clock.Now().Add(-time.Hour))
	if len(reads) != 1 || reads[0].Status != store.DocImportError {
		t.Errorf("recorded %+v, want one error", reads)
	}
}

func TestImportDocument_ConfirmCreatesCheckedNewDrivers(t *testing.T) {
	entry := clock.Today().AddDate(0, 0, -20)
	exit := clock.Today().AddDate(0, 0, -15)
	app, a, _ := docApp(t, &fakeReader{res: docread.Result{Stays: []docread.Stay{
		{Driver: "Novi Jedan", Entry: entry.Format("02.01.2006"), Exit: exit.Format("02.01.2006"), Type: "posao"},
		{Driver: "Novi Dva", Entry: entry.Format("02.01.2006"), Exit: exit.Format("02.01.2006"), Type: "posao"},
	}}})
	sa := app.mustLogin(a)
	ctx := context.Background()
	sa.uploadDocument(fakePDF)

	row := func(name string) url.Values {
		return url.Values{"row_driver_id": {"0"}, "row_new_driver": {name},
			"row_entry": {entry.Format("2006-01-02")}, "row_exit": {exit.Format("2006-01-02")},
			"row_type": {"work"}, "row_note": {""}}
	}
	form := row("Novi Jedan")
	for k, v := range row("Novi Dva") {
		form[k] = append(form[k], v...)
	}
	// Only the first new driver stays checked.
	form["add_driver"] = []string{"Novi Jedan"}
	w := sa.post("/import/potvrdi", form)
	if msg := flashFrom(sa, w, "ok"); !strings.Contains(msg, "1 boravaka, 1 novih vozača") {
		t.Errorf("flash %q", msg)
	}

	drivers, err := app.st.ListDrivers(ctx, a.tenant.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	var created *store.Driver
	for i := range drivers {
		switch drivers[i].Name {
		case "Novi Jedan":
			created = &drivers[i]
		case "Novi Dva":
			t.Error("unchecked new driver was created")
		}
	}
	if created == nil {
		t.Fatal("checked new driver was not created")
	}
	if n := app.stayCount(a.tenant.ID, created.ID); n != 1 {
		t.Errorf("new driver has %d stays, want 1", n)
	}
}

func TestImportDocument_FromDriverPagePutsEveryRowOnHim(t *testing.T) {
	entry := clock.Today().AddDate(0, 0, -20).Format("02.01.2006")
	exit := clock.Today().AddDate(0, 0, -15).Format("02.01.2006")
	reader := &fakeReader{res: docread.Result{Stays: []docread.Stay{
		{Driver: "A. Prva", Entry: entry, Exit: exit, Type: "posao"},
	}}}
	app, a, b := docApp(t, reader)
	sa := app.mustLogin(a)

	body := sa.uploadDocumentFor(fakePDF, a.driver.ID).Body.String()
	if len(reader.names) != 1 || reader.names[0] != "Ana Prva" {
		t.Errorf("model got names %v, want only the driver", reader.names)
	}
	for _, want := range []string{"Pregled (1 redova, 1 ispravnih)", `name="driver_id" value="` + id(a.driver.ID) + `"`, `name="row_driver_id" value="` + id(a.driver.ID) + `"`} {
		if !strings.Contains(body, want) {
			t.Errorf("preview missing %q", want)
		}
	}
	if strings.Contains(body, "novi vozač") {
		t.Error("row from the driver's page offered as a new driver")
	}
	// Another firm's driver id is not found.
	if w := sa.uploadDocumentFor(fakePDF, b.driver.ID); w.Code != http.StatusNotFound {
		t.Errorf("other firm's driver: status %d, want 404", w.Code)
	}
}

// uploadDocumentFor posts a scan from a driver's page.
func (s *session) uploadDocumentFor(content []byte, driverID int64) *httptest.ResponseRecorder {
	s.app.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("csrf_token", s.csrfToken())
	mw.WriteField("driver_id", id(driverID))
	fw, _ := mw.CreateFormFile("file", "evidencija.pdf")
	fw.Write(content)
	mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/import/dokument", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.AddCookie(s.cookie)
	w := httptest.NewRecorder()
	s.app.handler.ServeHTTP(w, r)
	return w
}
