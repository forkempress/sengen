package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"shengen/internal/auth"
	"shengen/internal/clock"
	"shengen/internal/db"
	"shengen/internal/store"
	"shengen/internal/telegram"
)

// These tests drive the real router — login, session cookie, CSRF and
// handlers — with two firms in one database. Everyone signs in at the same
// address, so the only thing standing between firm A and firm B's data is
// the session. Requests deliberately carry an arbitrary Host header to show
// the address plays no part any more.

type testApp struct {
	t       *testing.T
	handler http.Handler
	st      *store.Store
}

type firm struct {
	tenant   *store.Tenant
	email    string
	password string
	driver   *store.Driver
	stay     *store.Stay
}

func newTestApp(t *testing.T) *testApp {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	st := store.New(conn)
	srv := New(st, auth.New(st, true), telegram.New(st, telegram.Config{}))
	return &testApp{t: t, handler: srv.Routes(), st: st}
}

func (a *testApp) addFirm(name, slug, email, driverName string, daysAgo int) firm {
	a.t.Helper()
	ctx := context.Background()
	tn, err := a.st.CreateTenant(ctx, name, slug)
	if err != nil {
		a.t.Fatal(err)
	}
	const password = "lozinka-za-test"
	hash, err := auth.HashPassword(password)
	if err != nil {
		a.t.Fatal(err)
	}
	if _, err := a.st.CreateUser(ctx, tn.ID, email, hash, "owner", "Vlasnik "+name); err != nil {
		a.t.Fatal(err)
	}
	d, err := a.st.CreateDriver(ctx, tn.ID, driverName, "")
	if err != nil {
		a.t.Fatal(err)
	}
	entry := clock.Today().AddDate(0, 0, -daysAgo)
	exit := entry.AddDate(0, 0, 3)
	st, err := a.st.CreateStay(ctx, tn.ID, d.ID, entry, &exit, "work", "manual", "", nil)
	if err != nil {
		a.t.Fatal(err)
	}
	return firm{tenant: tn, email: email, password: password, driver: d, stay: st}
}

type session struct {
	app    *testApp
	cookie *http.Cookie
}

func (a *testApp) request(method, path string, form url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	r := httptest.NewRequest(method, path, body)
	r.Host = "bilo-koja-adresa.example"
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	a.handler.ServeHTTP(w, r)
	return w
}

func (a *testApp) login(email, password string) (*session, *httptest.ResponseRecorder) {
	w := a.request(http.MethodPost, "/login", url.Values{"email": {email}, "password": {password}}, nil)
	for _, c := range w.Result().Cookies() {
		if c.Name == auth.SessionCookieName && c.Value != "" {
			return &session{app: a, cookie: c}, w
		}
	}
	return nil, w
}

func (a *testApp) mustLogin(f firm) *session {
	a.t.Helper()
	s, w := a.login(f.email, f.password)
	if s == nil {
		a.t.Fatalf("login as %s failed: %d %s", f.email, w.Code, w.Body.String())
	}
	return s
}

func (s *session) get(path string) *httptest.ResponseRecorder {
	return s.app.request(http.MethodGet, path, nil, s.cookie)
}

var csrfRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

func (s *session) post(path string, form url.Values) *httptest.ResponseRecorder {
	s.app.t.Helper()
	m := csrfRe.FindStringSubmatch(s.get("/pregled").Body.String())
	if m == nil {
		s.app.t.Fatal("no CSRF token on /pregled")
	}
	if form == nil {
		form = url.Values{}
	}
	form.Set("csrf_token", m[1])
	return s.app.request(http.MethodPost, path, form, s.cookie)
}

func id(n int64) string { return strconv.FormatInt(n, 10) }

func twoFirmApp(t *testing.T) (*testApp, firm, firm) {
	app := newTestApp(t)
	a := app.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana Prva", 30)
	b := app.addFirm("Druga Firma", "druga", "vlasnik@druga.rs", "Boris Drugi", 50)
	return app, a, b
}

// --- login ---

func TestLogin_EmailPicksTheFirm(t *testing.T) {
	app, a, b := twoFirmApp(t)

	for _, tc := range []struct {
		who          firm
		sees, hidden string
	}{
		{a, "Ana Prva", "Boris Drugi"},
		{b, "Boris Drugi", "Ana Prva"},
	} {
		body := app.mustLogin(tc.who).get("/pregled").Body.String()
		if !strings.Contains(body, tc.sees) {
			t.Errorf("%s does not see their own driver %q", tc.who.email, tc.sees)
		}
		if strings.Contains(body, tc.hidden) {
			t.Errorf("%s sees %q, a driver of another firm", tc.who.email, tc.hidden)
		}
	}
}

// Same status and the same message for "no such account" and "wrong
// password": anything else tells an attacker which addresses exist — across
// every firm, now that the email alone picks the firm.
func TestLogin_UnknownEmailAndWrongPasswordLookIdentical(t *testing.T) {
	app, a, _ := twoFirmApp(t)

	_, wrongPass := app.login(a.email, "pogresna-lozinka")
	_, noAccount := app.login("niko@nigde.rs", "pogresna-lozinka")

	if wrongPass.Code != http.StatusUnauthorized || noAccount.Code != http.StatusUnauthorized {
		t.Fatalf("status: wrong password %d, unknown email %d; want 401 for both", wrongPass.Code, noAccount.Code)
	}
	const msg = "Pogrešan email ili lozinka."
	if !strings.Contains(wrongPass.Body.String(), msg) || !strings.Contains(noAccount.Body.String(), msg) {
		t.Error("the two failures show different messages")
	}
}

func TestNoSession_RedirectsToLogin(t *testing.T) {
	app, a, _ := twoFirmApp(t)
	for _, path := range []string{"/pregled", "/vozaci/" + id(a.driver.ID), "/podesavanja/korisnici"} {
		w := app.request(http.MethodGet, path, nil, nil)
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
			t.Errorf("GET %s without session = %d %s, want redirect to /login", path, w.Code, w.Header().Get("Location"))
		}
	}
}

// --- isolation: reading ---

func TestIsolation_CannotOpenAnotherFirmsDriver(t *testing.T) {
	app, a, b := twoFirmApp(t)
	sa := app.mustLogin(a)

	if w := sa.get("/vozaci/" + id(a.driver.ID)); w.Code != http.StatusOK {
		t.Fatalf("own driver = %d, want 200", w.Code)
	}
	w := sa.get("/vozaci/" + id(b.driver.ID))
	if w.Code != http.StatusNotFound {
		t.Errorf("firm A opening firm B's driver = %d, want 404", w.Code)
	}
	if strings.Contains(w.Body.String(), "Boris Drugi") {
		t.Error("firm B's driver name leaked into the response")
	}
}

func TestIsolation_UsersPageShowsOnlyOwnFirm(t *testing.T) {
	app, a, b := twoFirmApp(t)
	body := app.mustLogin(a).get("/podesavanja/korisnici").Body.String()
	if strings.Contains(body, b.email) {
		t.Errorf("firm A's users page lists %s from firm B", b.email)
	}
}

// --- isolation: writing ---

func (a *testApp) stayCount(tenantID, driverID int64) int {
	a.t.Helper()
	stays, err := a.st.ListStaysByDriver(context.Background(), tenantID, driverID)
	if err != nil {
		a.t.Fatal(err)
	}
	return len(stays)
}

// Firm A guessing firm B's ids must not be able to add, change, close or
// delete anything of firm B's.
func TestIsolation_CannotWriteToAnotherFirmsData(t *testing.T) {
	app, a, b := twoFirmApp(t)
	sa := app.mustLogin(a)
	today := clock.Today()
	day := func(offset int) string { return today.AddDate(0, 0, offset).Format("2006-01-02") }
	before := app.stayCount(b.tenant.ID, b.driver.ID)

	attempts := []struct {
		name string
		path string
		form url.Values
	}{
		{"add a stay to B's driver", "/vozaci/" + id(b.driver.ID) + "/boravci",
			url.Values{"entry_date": {day(-2)}, "exit_date": {day(-1)}, "type": {"work"}}},
		{"close B's driver's stay", "/vozaci/" + id(b.driver.ID) + "/vratio-se", nil},
		{"edit B's stay through B's driver", "/vozaci/" + id(b.driver.ID) + "/boravci/" + id(b.stay.ID),
			url.Values{"entry_date": {day(-9)}, "exit_date": {day(-8)}, "type": {"work"}}},
		{"edit B's stay through A's own driver", "/vozaci/" + id(a.driver.ID) + "/boravci/" + id(b.stay.ID),
			url.Values{"entry_date": {day(-9)}, "exit_date": {day(-8)}, "type": {"work"}}},
		{"delete B's stay through A's own driver", "/vozaci/" + id(a.driver.ID) + "/boravci/" + id(b.stay.ID) + "/obrisi", nil},
		{"deactivate B's driver", "/podesavanja/vozaci/" + id(b.driver.ID) + "/deaktiviraj", nil},
		{"Telegram link for B's driver", "/podesavanja/vozaci/" + id(b.driver.ID) + "/telegram-link", nil},
	}
	for _, at := range attempts {
		sa.post(at.path, at.form)
	}

	ctx := context.Background()
	if got := app.stayCount(b.tenant.ID, b.driver.ID); got != before {
		t.Errorf("firm B's driver has %d stays after firm A's attempts, want %d", got, before)
	}
	stay, err := app.st.GetStay(ctx, b.tenant.ID, b.stay.ID)
	if err != nil {
		t.Fatalf("firm B's stay is gone: %v", err)
	}
	if !stay.Entry.Equal(b.stay.Entry) || stay.Exit == nil || !stay.Exit.Equal(*b.stay.Exit) {
		t.Errorf("firm B's stay was changed to %s–%v", stay.Entry.Format("02.01.2006"), stay.Exit)
	}
	driver, err := app.st.GetDriver(ctx, b.tenant.ID, b.driver.ID)
	if err != nil || !driver.Active {
		t.Errorf("firm B's driver was deactivated (or lost): %+v, %v", driver, err)
	}
}

// A request that names firm B's driver must be refused outright, not
// "succeed" by quietly doing nothing.
func TestIsolation_WritesToAnotherFirmReturn404(t *testing.T) {
	app, a, b := twoFirmApp(t)
	sa := app.mustLogin(a)

	for _, path := range []string{
		"/vozaci/" + id(b.driver.ID) + "/boravci",
		"/vozaci/" + id(b.driver.ID) + "/vratio-se",
		"/podesavanja/vozaci/" + id(b.driver.ID) + "/deaktiviraj",
		"/podesavanja/vozaci/" + id(b.driver.ID) + "/telegram-link",
	} {
		if w := sa.post(path, nil); w.Code != http.StatusNotFound {
			t.Errorf("POST %s as firm A = %d, want 404", path, w.Code)
		}
	}
}

// Timing must not reveal whether an address has an account. Without the
// dummy bcrypt comparison an unknown email answers in well under a
// millisecond while a wrong password costs a full bcrypt (tens of ms), a gap
// visible from across the internet. The check compares the fastest of a few
// runs and only demands the same order of magnitude, so it isn't sensitive
// to machine speed or load.
func TestLogin_UnknownEmailTakesAsLongAsWrongPassword(t *testing.T) {
	app, a, _ := twoFirmApp(t)

	fastest := func(email string) time.Duration {
		best := time.Hour
		for i := 0; i < 3; i++ {
			start := time.Now()
			app.login(email, "pogresna-lozinka")
			if d := time.Since(start); d < best {
				best = d
			}
		}
		return best
	}
	wrongPass := fastest(a.email)
	unknown := fastest("niko@nigde.rs")

	if unknown < wrongPass/3 {
		t.Errorf("unknown email answered in %v, wrong password in %v — the gap reveals which emails exist", unknown, wrongPass)
	}
}
