package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"shengen/internal/auth"
	"shengen/internal/clock"
	"shengen/internal/legal"
)

func (a *testApp) addDispatcher(f firm, email string) *session {
	a.t.Helper()
	hash, err := auth.HashPassword("lozinka-dispecera")
	if err != nil {
		a.t.Fatal(err)
	}
	if _, err := a.st.CreateUser(context.Background(), f.tenant.ID, email, hash, "dispatcher", "Dispečer"); err != nil {
		a.t.Fatal(err)
	}
	s, w := a.login(email, "lozinka-dispecera")
	if s == nil {
		a.t.Fatalf("dispatcher login failed: %d", w.Code)
	}
	return s
}

func TestLegalPages_PublicAndLinked(t *testing.T) {
	app := newTestApp(t)
	for path, want := range map[string]string{
		"/privatnost":      "Politika privatnosti",
		"/uslovi":          "Proračun nije pravni savet",
		"/obrada-podataka": "Obaveze obrađivača",
	} {
		w := app.request(http.MethodGet, path, nil, nil)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), want) {
			t.Errorf("GET %s without a session = %d, want 200 with %q", path, w.Code, want)
		}
	}
	login := app.request(http.MethodGet, "/login", nil, nil).Body.String()
	for _, want := range []string{`href="/privatnost"`, `href="/uslovi"`, `href="/obrada-podataka"`, "granična policija"} {
		if !strings.Contains(login, want) {
			t.Errorf("login page footer lacks %q", want)
		}
	}
}

func TestDisclaimer_UnderResults(t *testing.T) {
	app := newTestApp(t)
	f := app.addFirm("Prevoz A", "prevoz-a", "a@prevoz.rs", "Marko Test", 40)
	s := app.mustLogin(f)
	page := s.get("/vozaci/" + id(f.driver.ID) + "?od=" + planDay(5) + "&do=" + planDay(9)).Body.String()
	// The hero, the trip result and the footer.
	if n := strings.Count(page, "granična policija"); n < 3 {
		t.Errorf("disclaimer shown %d times on the driver page, want under the status, the result and in the footer", n)
	}
	if !strings.Contains(page, legal.EUCalculatorURL) {
		t.Error("no link to the EU short-stay calculator")
	}
}

// A firm that has not accepted the current terms: its owner must accept
// before anything else; a dispatcher keeps working with a notice.
func TestTerms_OwnerMustAccept(t *testing.T) {
	app := newTestApp(t)
	f := app.addFirm("Prevoz A", "prevoz-a", "a@prevoz.rs", "Marko Test", 40)
	ctx := context.Background()
	if _, err := app.st.DB.ExecContext(ctx, `UPDATE tenants SET terms_version = '', terms_accepted_at = NULL WHERE id = ?`, f.tenant.ID); err != nil {
		t.Fatal(err)
	}
	owner := app.mustLogin(f)
	disp := app.addDispatcher(f, "dispecer@prevoz.rs")

	if w := owner.get("/pregled"); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/prihvatanje" {
		t.Fatalf("owner on /pregled = %d %s, want a redirect to /prihvatanje", w.Code, w.Header().Get("Location"))
	}
	if body := owner.get("/prihvatanje").Body.String(); !strings.Contains(body, `name="prihvatam"`) {
		t.Fatal("acceptance page has no checkbox")
	}

	dispPage := disp.get("/pregled")
	if dispPage.Code != http.StatusOK || !strings.Contains(dispPage.Body.String(), "još nije prihvatio") {
		t.Errorf("dispatcher on /pregled = %d, want the page with a notice", dispPage.Code)
	}
	if w := disp.post("/prihvatanje", url.Values{"prihvatam": {"da"}}); w.Code != http.StatusForbidden {
		t.Errorf("dispatcher accepting = %d, want 403", w.Code)
	}

	// The CSRF token comes from the acceptance page itself here: /pregled
	// redirects until the terms are accepted.
	token := csrfRe.FindStringSubmatch(owner.get("/prihvatanje").Body.String())[1]
	if w := app.request(http.MethodPost, "/prihvatanje", url.Values{"csrf_token": {token}}, owner.cookie); w.Header().Get("Location") != "/prihvatanje" {
		t.Errorf("accepting without the checkbox = %d %s, want back to the form", w.Code, w.Header().Get("Location"))
	}
	if ts, _ := app.st.GetTermsStatus(ctx, f.tenant.ID); ts.Version != "" {
		t.Fatal("terms recorded as accepted without the checkbox")
	}
	w := app.request(http.MethodPost, "/prihvatanje", url.Values{"csrf_token": {token}, "prihvatam": {"da"}}, owner.cookie)
	if w.Header().Get("Location") != "/pregled" {
		t.Fatalf("accepting = %d %s, want a redirect to /pregled", w.Code, w.Header().Get("Location"))
	}
	ts, _ := app.st.GetTermsStatus(ctx, f.tenant.ID)
	if ts.Version != legal.TermsVersion || ts.AcceptedAt == nil {
		t.Errorf("terms status = %+v, want the current version with a date", ts)
	}
	if n := app.auditCountAny(f.tenant.ID, "tenant", "accept_terms"); n != 1 {
		t.Errorf("acceptance audit entries = %d, want 1", n)
	}
	if w := owner.get("/pregled"); w.Code != http.StatusOK {
		t.Errorf("owner on /pregled after accepting = %d, want 200", w.Code)
	}
}

func (a *testApp) auditCountAny(tenantID int64, entity, action string) int {
	a.t.Helper()
	var n int
	if err := a.st.DB.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM audit_log WHERE tenant_id = ? AND entity = ? AND action = ?`, tenantID, entity, action).Scan(&n); err != nil {
		a.t.Fatal(err)
	}
	return n
}

func TestDriverExport_CSV(t *testing.T) {
	app := newTestApp(t)
	f := app.addFirm("Prevoz A", "prevoz-a", "a@prevoz.rs", "=HYPERLINK(1)", 40)
	if _, err := app.st.CreatePlannedStay(context.Background(), f.tenant.ID, f.driver.ID, clock.Today().AddDate(0, 0, 5), clock.Today().AddDate(0, 0, 9), "Minhen", nil); err != nil {
		t.Fatal(err)
	}
	owner := app.mustLogin(f)
	w := owner.get("/vozaci/" + id(f.driver.ID) + "/izvoz.csv")
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("export = %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	body := w.Body.String()
	for _, want := range []string{"zapis,vozač,vozilo,od,do", "boravak,", f.stay.Entry.Format("02.01.2006"), "plan,", "Minhen", "'=HYPERLINK(1)"} {
		if !strings.Contains(body, want) {
			t.Errorf("export lacks %q:\n%s", want, body)
		}
	}
	if n := app.auditCountAny(f.tenant.ID, "driver", "export"); n != 1 {
		t.Errorf("export audit entries = %d, want 1", n)
	}

	disp := app.addDispatcher(f, "dispecer@prevoz.rs")
	if w := disp.get("/vozaci/" + id(f.driver.ID) + "/izvoz.csv"); w.Code != http.StatusForbidden {
		t.Errorf("dispatcher export = %d, want 403", w.Code)
	}
}

func TestDriverDeletePermanent(t *testing.T) {
	app := newTestApp(t)
	f := app.addFirm("Prevoz A", "prevoz-a", "a@prevoz.rs", "Marko Test", 40)
	owner := app.mustLogin(f)
	path := "/vozaci/" + id(f.driver.ID) + "/obrisi-trajno"

	if !strings.Contains(owner.get("/vozaci/"+id(f.driver.ID)).Body.String(), "Trajno obriši vozača") {
		t.Error("owner does not see the delete form")
	}
	disp := app.addDispatcher(f, "dispecer@prevoz.rs")
	if strings.Contains(disp.get("/vozaci/"+id(f.driver.ID)).Body.String(), "Trajno obriši vozača") {
		t.Error("dispatcher sees the delete form")
	}
	if w := disp.post(path, url.Values{"potvrda": {"Marko Test"}}); w.Code != http.StatusForbidden {
		t.Errorf("dispatcher delete = %d, want 403", w.Code)
	}

	if msg := flashFrom(owner, owner.post(path, url.Values{"potvrda": {"Marko"}}), "err"); !strings.Contains(msg, "upišite njegovo ime") {
		t.Errorf("wrong confirmation: %q", msg)
	}
	if _, err := app.st.GetDriver(context.Background(), f.tenant.ID, f.driver.ID); err != nil {
		t.Fatal("driver deleted without the right confirmation")
	}

	body := follow(owner, owner.post(path, url.Values{"potvrda": {"marko test"}}))
	if !strings.Contains(body, "trajno obrisani") || strings.Contains(body, "Marko Test") {
		t.Error("driver still listed after deletion")
	}
	if app.stayCount(f.tenant.ID, f.driver.ID) != 0 {
		t.Error("stays survived")
	}
	var actions, withData int
	app.st.DB.QueryRowContext(context.Background(),
		`SELECT COUNT(*), COUNT(before_json) + COUNT(after_json) FROM audit_log WHERE entity = 'driver' AND entity_id = ? AND action = 'delete_permanent'`,
		f.driver.ID).Scan(&actions, &withData)
	if actions != 1 || withData != 0 {
		t.Errorf("delete audit: %d entries, %d with data; want 1 without personal data", actions, withData)
	}
	if w := owner.get("/vozaci/" + id(f.driver.ID)); w.Code != http.StatusNotFound {
		t.Errorf("deleted driver page = %d, want 404", w.Code)
	}
}
