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

// CSV import confirmation carries driver ids in hidden form fields. Firm A
// rewriting one to firm B's driver must not plant a stay on B's driver — an
// open one would block B from recording anything for that driver.
func TestIsolation_ImportConfirmCannotWriteToAnotherFirmsDriver(t *testing.T) {
	app, a, b := twoFirmApp(t)
	sa := app.mustLogin(a)
	ctx := context.Background()
	before := app.stayCount(b.tenant.ID, b.driver.ID)
	entry := clock.Today().AddDate(0, 0, -2).Format("2006-01-02")

	sa.post("/import/potvrdi", url.Values{
		"row_driver_id": {id(b.driver.ID)},
		"row_entry":     {entry},
		"row_exit":      {""},
		"row_type":      {"work"},
		"row_note":      {""},
	})

	var planted int
	if err := app.st.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM stays WHERE driver_id = ? AND tenant_id <> ?`, b.driver.ID, b.tenant.ID,
	).Scan(&planted); err != nil {
		t.Fatal(err)
	}
	if planted != 0 {
		t.Errorf("firm A planted %d stay(s) on firm B's driver", planted)
	}
	if got := app.stayCount(b.tenant.ID, b.driver.ID); got != before {
		t.Errorf("firm B's driver has %d stays, want %d", got, before)
	}
	// Firm B can still open a stay for its own driver.
	if _, err := app.st.CreateStay(ctx, b.tenant.ID, b.driver.ID, clock.Today(), nil, "work", "manual", "", nil); err != nil {
		t.Errorf("firm B cannot open a stay for its own driver: %v", err)
	}
}

// Even a row that slips past the handler cannot tie a stay to another firm's
// driver: the database refuses it.
func TestIsolation_DatabaseRefusesStayForAnotherFirmsDriver(t *testing.T) {
	app, a, b := twoFirmApp(t)
	_, err := app.st.DB.ExecContext(context.Background(),
		`INSERT INTO stays (tenant_id, driver_id, entry_date, type, source) VALUES (?, ?, ?, 'work', 'import')`,
		a.tenant.ID, b.driver.ID, clock.Today().Format("2006-01-02"))
	if err == nil {
		t.Error("database accepted a stay whose driver belongs to another firm")
	}
}

// Hidden fields of different lengths must be rejected, not panic.
func TestImportConfirm_MismatchedFieldsAreRejected(t *testing.T) {
	app, a, _ := twoFirmApp(t)
	sa := app.mustLogin(a)
	before := app.stayCount(a.tenant.ID, a.driver.ID)

	w := sa.post("/import/potvrdi", url.Values{
		"row_driver_id": {id(a.driver.ID), id(a.driver.ID)},
		"row_entry":     {clock.Today().AddDate(0, 0, -2).Format("2006-01-02")},
	})
	if msg := flashFrom(sa, w, "err"); msg == "" {
		t.Errorf("POST /import/potvrdi with mismatched fields = %d, want a redirect with an error", w.Code)
	}
	if got := app.stayCount(a.tenant.ID, a.driver.ID); got != before {
		t.Errorf("stays = %d, want %d", got, before)
	}
}

// SEC-02: an owner manages only his own firm's users. Another firm's user
// id is "not found", and nothing about that user changes.
func TestIsolation_CannotManageAnotherFirmsUsers(t *testing.T) {
	app, a, b := twoFirmApp(t)
	sa, sb := app.mustLogin(a), app.mustLogin(b)
	ownerB := app.ownerOf(b)

	for _, tc := range []struct {
		path string
		form url.Values
	}{
		{"/deaktiviraj", nil},
		{"/aktiviraj", nil},
		{"/uloga", url.Values{"role": {"dispatcher"}}},
	} {
		if w := sa.post("/podesavanja/korisnici/"+id(ownerB.ID)+tc.path, tc.form); w.Code != http.StatusNotFound {
			t.Errorf("POST %s on firm B's owner = %d, want 404", tc.path, w.Code)
		}
	}
	if u := app.user(b.tenant.ID, ownerB.ID); !u.Active || u.Role != "owner" {
		t.Errorf("firm B's owner changed: %+v", u)
	}
	if !signedIn(sb) {
		t.Error("firm B's owner was signed out")
	}
	if got := app.auditActions(b.tenant.ID, ownerB.ID); len(got) != 0 {
		t.Errorf("audit rows for firm B's owner: %q", got)
	}
	if body := sa.get("/podesavanja/korisnici").Body.String(); strings.Contains(body, b.email) {
		t.Error("firm A's users page lists firm B's owner")
	}
}

// Plans: firm A guessing firm B's driver and plan ids can neither read nor
// change them, through B's driver or through its own.
func TestIsolation_Plans(t *testing.T) {
	app, a, b := twoFirmApp(t)
	ctx := context.Background()
	today := clock.Today()
	bPlan, err := app.st.CreatePlannedStay(ctx, b.tenant.ID, b.driver.ID, today.AddDate(0, 0, 10), today.AddDate(0, 0, 15), "Tajna tura B", nil)
	if err != nil {
		t.Fatal(err)
	}
	sa := app.mustLogin(a)
	day := func(offset int) string { return today.AddDate(0, 0, offset).Format("2006-01-02") }
	form := url.Values{"od": {day(20)}, "do": {day(25)}, "napomena": {"podmetnuto"}}

	for _, path := range []string{
		"/vozaci/" + id(b.driver.ID) + "/planovi",
		"/vozaci/" + id(b.driver.ID) + "/planovi/" + id(bPlan.ID),
		"/vozaci/" + id(a.driver.ID) + "/planovi/" + id(bPlan.ID),
		"/vozaci/" + id(b.driver.ID) + "/planovi/" + id(bPlan.ID) + "/obrisi",
		"/vozaci/" + id(a.driver.ID) + "/planovi/" + id(bPlan.ID) + "/obrisi",
	} {
		if w := sa.post(path, form); w.Code != http.StatusNotFound {
			t.Errorf("POST %s as firm A = %d, want 404", path, w.Code)
		}
	}
	got, err := app.st.GetPlannedStay(ctx, b.tenant.ID, bPlan.ID)
	if err != nil {
		t.Fatalf("firm B's plan is gone: %v", err)
	}
	if !got.From.Equal(bPlan.From) || got.Note != "Tajna tura B" {
		t.Errorf("firm B's plan was changed: %+v", got)
	}
	if n := len(app.plans(b.tenant.ID, b.driver.ID)); n != 1 {
		t.Errorf("firm B's driver has %d plans, want 1", n)
	}
	if _, err := app.st.GetPlannedStay(ctx, a.tenant.ID, bPlan.ID); err == nil {
		t.Error("the store returns firm B's plan to firm A")
	}

	// Reading: the planner treats B's driver as no choice, and neither page
	// shows B's plan.
	for _, path := range []string{
		"/planer?vozac=" + id(b.driver.ID) + "&od=" + day(5) + "&do=" + day(9),
		"/planer",
		"/pregled",
	} {
		body := sa.get(path).Body.String()
		if strings.Contains(body, "Boris Drugi") || strings.Contains(body, "Tajna tura B") {
			t.Errorf("GET %s as firm A shows firm B's driver or plan", path)
		}
	}
}
