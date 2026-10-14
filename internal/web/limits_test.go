package web

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func (s *session) csrfToken() string {
	s.app.t.Helper()
	m := csrfRe.FindStringSubmatch(s.get("/pregled").Body.String())
	if m == nil {
		s.app.t.Fatal("no CSRF token on /pregled")
	}
	return m[1]
}

// uploadCSV posts a multipart import the way the browser does.
func (s *session) uploadCSV(content []byte) *httptest.ResponseRecorder {
	s.app.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("csrf_token", s.csrfToken())
	fw, _ := mw.CreateFormFile("file", "istorija.csv")
	fw.Write(content)
	mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/import/preview", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.AddCookie(s.cookie)
	w := httptest.NewRecorder()
	s.app.handler.ServeHTTP(w, r)
	return w
}

// SEC-13: the upload used to be read without a size cap.
func TestImportUpload_OverTwoMegabytesIsRefused(t *testing.T) {
	app, a, _ := twoFirmApp(t)
	sa := app.mustLogin(a)

	small := sa.uploadCSV([]byte("vozač,ulaz,izlaz,tip\n"))
	if small.Code != http.StatusOK {
		t.Fatalf("small upload: status %d, want 200", small.Code)
	}
	big := sa.uploadCSV(bytes.Repeat([]byte("a"), 3<<20))
	if big.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("3 MB upload: status %d, want 413", big.Code)
	}
}

// SEC-13: an ordinary form is capped at 1 MB.
func TestForm_OverOneMegabyteIsRefused(t *testing.T) {
	app, a, _ := twoFirmApp(t)
	sa := app.mustLogin(a)
	w := sa.post("/vozaci/"+id(a.driver.ID)+"/boravci", url.Values{
		"entry_date": {"2026-01-01"}, "type": {"work"}, "note": {strings.Repeat("x", 2<<20)},
	})
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("2 MB form: status %d, want 413", w.Code)
	}
}

// SEC-13: a note is at most 500 characters, a driver's name at most 100.
func TestFieldLengthLimits(t *testing.T) {
	app, a, _ := twoFirmApp(t)
	sa := app.mustLogin(a)
	ctx := context.Background()

	before := app.stayCount(a.tenant.ID, a.driver.ID)
	w := sa.post("/vozaci/"+id(a.driver.ID)+"/boravci", url.Values{
		"entry_date": {"2025-01-01"}, "exit_date": {"2025-01-02"}, "type": {"work"}, "note": {strings.Repeat("č", 501)},
	})
	if got := app.stayCount(a.tenant.ID, a.driver.ID); got != before {
		t.Errorf("501-character note was stored (stays %d -> %d)", before, got)
	}
	if msg := flashFrom(w, "err"); !strings.Contains(msg, "500") {
		t.Errorf("error = %q, want it to name the 500 limit", msg)
	}

	// Exactly 500 characters (multi-byte) is fine.
	sa.post("/vozaci/"+id(a.driver.ID)+"/boravci", url.Values{
		"entry_date": {"2025-01-01"}, "exit_date": {"2025-01-02"}, "type": {"work"}, "note": {strings.Repeat("č", 500)},
	})
	if got := app.stayCount(a.tenant.ID, a.driver.ID); got != before+1 {
		t.Errorf("500-character note was refused")
	}

	drivers, _ := app.st.ListDrivers(ctx, a.tenant.ID, false)
	sa.post("/podesavanja/vozaci", url.Values{"name": {strings.Repeat("Ž", 101)}})
	if after, _ := app.st.ListDrivers(ctx, a.tenant.ID, false); len(after) != len(drivers) {
		t.Error("101-character driver name was stored")
	}

	users, _ := app.st.ListUsers(ctx, a.tenant.ID)
	sa.post("/podesavanja/korisnici", url.Values{
		"name": {strings.Repeat("a", 101)}, "email": {"novi@prva.rs"}, "password": {"dugacka-lozinka"}, "role": {"dispatcher"},
	})
	if after, _ := app.st.ListUsers(ctx, a.tenant.ID); len(after) != len(users) {
		t.Error("101-character user name was stored")
	}

	// The store refuses too, whatever the caller (bot /dodaj, import).
	if _, err := app.st.CreateDriver(ctx, a.tenant.ID, strings.Repeat("a", 101), ""); err == nil {
		t.Error("store accepted a 101-character driver name")
	}
	if _, err := app.st.CreateStay(ctx, a.tenant.ID, a.driver.ID, a.stay.Entry.AddDate(-1, 0, 0), nil, "work", "manual", strings.Repeat("a", 501), nil); err == nil {
		t.Error("store accepted a 501-character note")
	}
}
