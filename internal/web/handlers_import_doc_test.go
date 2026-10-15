package web

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

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
	for _, want := range []string{"Pregled (2 redova, 1 ispravnih)", "proverite", "nepoznat vozač", "Potvrdi uvoz (1 boravaka)"} {
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
