package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"shengen/internal/clock"
	"shengen/internal/store"
)

// follow returns the page a form redirected to, with its flash messages.
func follow(s *session, w *httptest.ResponseRecorder) string {
	s.app.t.Helper()
	if w.Code != http.StatusSeeOther {
		s.app.t.Fatalf("want a redirect, got %d: %s", w.Code, w.Body.String())
	}
	loc, _, _ := strings.Cut(w.Header().Get("Location"), "#") // a browser never sends the fragment
	return s.get(loc).Body.String()
}

func planDay(n int) string { return clock.Today().AddDate(0, 0, n).Format("2006-01-02") }

func (a *testApp) plans(tenantID, driverID int64) []store.PlannedStay {
	a.t.Helper()
	ps, err := a.st.ListPlannedStaysByDriver(context.Background(), tenantID, driverID)
	if err != nil {
		a.t.Fatal(err)
	}
	return ps
}

// auditCount counts audit entries that record the plan's dates: before
// them for a delete, after them otherwise.
func (a *testApp) auditCount(tenantID int64, entity, action string) int {
	a.t.Helper()
	var n int
	if err := a.st.DB.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM audit_log WHERE tenant_id = ? AND entity = ? AND action = ?
		 AND CASE action WHEN 'delete' THEN before_json ELSE after_json END LIKE '%"od":%'`,
		tenantID, entity, action).Scan(&n); err != nil {
		a.t.Fatal(err)
	}
	return n
}

func TestPlans_CreateUpdateDelete(t *testing.T) {
	app := newTestApp(t)
	f := app.addFirm("Prevoz A", "prevoz-a", "a@prevoz.rs", "Marko Test", 40)
	s := app.mustLogin(f)
	base := "/vozaci/" + id(f.driver.ID) + "/planovi"

	body := follow(s, s.post(base, url.Values{"od": {planDay(10)}, "do": {planDay(19)}, "napomena": {"Minhen"}}))
	if !strings.Contains(body, "Plan sačuvan.") || !strings.Contains(body, "Minhen") || !strings.Contains(body, "day-planned") {
		t.Fatal("saved plan not shown on the driver page with its calendar days")
	}
	ps := app.plans(f.tenant.ID, f.driver.ID)
	if len(ps) != 1 {
		t.Fatalf("plans = %d, want 1", len(ps))
	}
	if app.auditCount(f.tenant.ID, "planned_stay", "create") != 1 {
		t.Error("plan creation not in the audit log")
	}

	planPath := base + "/" + id(ps[0].ID)
	body = follow(s, s.post(planPath, url.Values{"od": {planDay(12)}, "do": {planDay(20)}, "napomena": {"Beč"}}))
	if !strings.Contains(body, "Plan izmenjen.") || !strings.Contains(body, "Beč") {
		t.Error("updated plan not shown")
	}
	if got := app.plans(f.tenant.ID, f.driver.ID)[0]; got.From.Format("2006-01-02") != planDay(12) {
		t.Errorf("plan starts %s, want %s", got.From.Format("2006-01-02"), planDay(12))
	}
	if app.auditCount(f.tenant.ID, "planned_stay", "update") != 1 {
		t.Error("plan update not in the audit log")
	}

	body = follow(s, s.post(planPath+"/obrisi", nil))
	if !strings.Contains(body, "Plan obrisan.") || len(app.plans(f.tenant.ID, f.driver.ID)) != 0 {
		t.Error("plan not deleted")
	}
	if app.auditCount(f.tenant.ID, "planned_stay", "delete") != 1 {
		t.Error("plan deletion not in the audit log")
	}
}

func TestPlans_InvalidPlansAreRefused(t *testing.T) {
	app := newTestApp(t)
	f := app.addFirm("Prevoz A", "prevoz-a", "a@prevoz.rs", "Marko Test", 40)
	s := app.mustLogin(f)
	base := "/vozaci/" + id(f.driver.ID) + "/planovi"

	for _, tc := range []struct {
		name string
		form url.Values
		want string
	}{
		{"u prošlosti", url.Values{"od": {planDay(-1)}, "do": {planDay(3)}}, "u prošlosti"},
		{"povratak pre polaska", url.Values{"od": {planDay(5)}, "do": {planDay(3)}}, "pre polaska"},
		{"predugo", url.Values{"od": {planDay(1)}, "do": {planDay(95)}}, "najviše 90 dana"},
		{"bez datuma", url.Values{"do": {planDay(3)}}, "datum polaska"},
	} {
		if msg := flashFrom(s, s.post(base, tc.form), "err"); !strings.Contains(msg, tc.want) {
			t.Errorf("%s: error %q, want it to mention %q", tc.name, msg, tc.want)
		}
	}
	if n := len(app.plans(f.tenant.ID, f.driver.ID)); n != 0 {
		t.Errorf("%d invalid plans were saved", n)
	}

	// While the driver is in Schengen, a plan cannot start today.
	if _, err := app.st.CreateStay(context.Background(), f.tenant.ID, f.driver.ID, clock.Today().AddDate(0, 0, -2), nil, "work", "manual", "", nil); err != nil {
		t.Fatal(err)
	}
	if msg := flashFrom(s, s.post(base, url.Values{"od": {planDay(0)}, "do": {planDay(3)}}), "err"); !strings.Contains(msg, "još u Šengenu") {
		t.Errorf("plan starting while away: %q", msg)
	}
}

// Overlapping plans and plans over the limit are saved, with a warning, and
// the overview flags the driver.
func TestPlans_WarningsAndOverview(t *testing.T) {
	app := newTestApp(t)
	f := app.addFirm("Prevoz A", "prevoz-a", "a@prevoz.rs", "Marko Test", 40) // 4 days used
	s := app.mustLogin(f)
	base := "/vozaci/" + id(f.driver.ID) + "/planovi"

	s.post(base, url.Values{"od": {planDay(10)}, "do": {planDay(60)}})
	if msg := flashFrom(s, s.post(base, url.Values{"od": {planDay(50)}, "do": {planDay(55)}}), "err"); !strings.Contains(msg, "Preklapa se") {
		t.Errorf("overlap warning = %q", msg)
	}
	if body := s.get("/pregled").Body.String(); strings.Contains(body, "plan prekoračuje") {
		t.Error("overview flags plans that fit")
	}

	// 51 days + 4 used + 40 more = over 90 within the window.
	if msg := flashFrom(s, s.post(base, url.Values{"od": {planDay(62)}, "do": {planDay(101)}}), "err"); !strings.Contains(msg, "prelazi limit") {
		t.Errorf("over-limit warning = %q", msg)
	}
	if n := len(app.plans(f.tenant.ID, f.driver.ID)); n != 3 {
		t.Errorf("plans = %d, want 3 (warnings never refuse a plan)", n)
	}
	body := s.get("/pregled").Body.String()
	if !strings.Contains(body, "plan prekoračuje 90/180") || !strings.Contains(body, "prelazi 90/180") {
		t.Error("overview does not flag the driver with a plan over the limit")
	}
	if page := s.get("/vozaci/" + id(f.driver.ID)).Body.String(); !strings.Contains(page, "day-planned-over") {
		t.Error("calendar does not mark the planned days over the limit")
	}
}

// When the driver goes and comes back within a plan's dates, the plan is
// shown as realized and no longer counts.
func TestPlans_RealizedByARealStay(t *testing.T) {
	app := newTestApp(t)
	f := app.addFirm("Prevoz A", "prevoz-a", "a@prevoz.rs", "Marko Test", 100)
	ctx := context.Background()
	if _, err := app.st.DB.ExecContext(ctx,
		`INSERT INTO planned_stays (tenant_id, driver_id, start_date, end_date) VALUES (?, ?, ?, ?)`,
		f.tenant.ID, f.driver.ID, planDay(-10), planDay(-2)); err != nil {
		t.Fatal(err)
	}
	exit := clock.Today().AddDate(0, 0, -3)
	if _, err := app.st.CreateStay(ctx, f.tenant.ID, f.driver.ID, clock.Today().AddDate(0, 0, -9), &exit, "work", "telegram_button", "", nil); err != nil {
		t.Fatal(err)
	}
	body := app.mustLogin(f).get("/vozaci/" + id(f.driver.ID)).Body.String()
	if !strings.Contains(body, "realizovan") {
		t.Error("plan covered by a real stay is not shown as realized")
	}
}

func TestPlanner_QuickCheckAndUpcoming(t *testing.T) {
	app := newTestApp(t)
	f := app.addFirm("Prevoz A", "prevoz-a", "a@prevoz.rs", "Marko Test", 40)
	s := app.mustLogin(f)

	if body := s.get("/planer").Body.String(); !strings.Contains(body, "Marko Test") || strings.Contains(body, "Može.") {
		t.Fatal("planner page should list the driver and show no result before the form is sent")
	}
	q := "/planer?vozac=" + id(f.driver.ID) + "&od=" + planDay(5) + "&do=" + planDay(14)
	body := s.get(q).Body.String()
	for _, want := range []string{"Može.", "Marko Test", "(10 dana)", "Sačuvaj kao plan", `action="/vozaci/` + id(f.driver.ID) + `/planovi"`} {
		if !strings.Contains(body, want) {
			t.Errorf("quick check result does not contain %q", want)
		}
	}
	if n := len(app.plans(f.tenant.ID, f.driver.ID)); n != 0 {
		t.Errorf("the quick check saved %d plans", n)
	}
	if body := s.get("/planer?od=" + planDay(5) + "&do=" + planDay(14)).Body.String(); !strings.Contains(body, "Izaberite vozača") {
		t.Error("check without a driver should ask for one")
	}

	s.post("/vozaci/"+id(f.driver.ID)+"/planovi", url.Values{"od": {planDay(5)}, "do": {planDay(14)}, "napomena": {"Lion"}})
	if body := s.get("/planer").Body.String(); !strings.Contains(body, "Lion") {
		t.Error("saved plan missing from the upcoming list")
	}
	// The quick check now counts the saved plan and warns about the overlap.
	if body := s.get(q).Body.String(); !strings.Contains(body, "Preklapa se sa planom") || !strings.Contains(body, "sačuvanim planovima (1)") {
		t.Error("quick check ignores the saved plan")
	}
}

func TestPlanner_ScheduleAndWhoCan(t *testing.T) {
	app := newTestApp(t)
	f := app.addFirm("Prevoz A", "prevoz-a", "a@prevoz.rs", "Marko Test", 40)
	ctx := context.Background()
	other, err := app.st.CreateDriver(ctx, f.tenant.ID, "Jovan Drugi", "")
	if err != nil {
		t.Fatal(err)
	}
	// Jovan is in Schengen now, so he cannot start a trip today.
	if _, err := app.st.CreateStay(ctx, f.tenant.ID, other.ID, clock.Today().AddDate(0, 0, -2), nil, "work", "manual", "", nil); err != nil {
		t.Fatal(err)
	}
	s := app.mustLogin(f)

	body := s.get("/planer").Body.String()
	if !strings.Contains(body, "Raspored dostupnosti") || !strings.Contains(body, "sched-busy") || !strings.Contains(body, "sched-ok") {
		t.Fatal("schedule missing, or without a busy and a free cell")
	}
	if !strings.Contains(body, "/planer?vozac="+id(f.driver.ID)+"&amp;od="+planDay(0)) {
		t.Error("today's free cell does not link to the quick check")
	}

	body = s.get("/planer?vozac=svi&od=" + planDay(0) + "&do=" + planDay(9)).Body.String()
	marko, jovan := strings.Index(body, ">Marko Test<"), strings.Index(body, ">Jovan Drugi<")
	if marko < 0 || jovan < 0 || !strings.Contains(body, "<strong>Može</strong>") || !strings.Contains(body, "sada je u Šengenu") {
		t.Fatal("fleet check does not answer for both drivers")
	}
	// The driver list and the schedule name drivers too; compare within the
	// fleet table only.
	fleet := body[strings.Index(body, `id="planer-rezultat"`):strings.Index(body, "Raspored dostupnosti")]
	if strings.Index(fleet, ">Marko Test<") > strings.Index(fleet, ">Jovan Drugi<") {
		t.Error("driver who can go is not listed before the one who is away")
	}

	body = s.get("/planer?vozac=svi&od=" + planDay(-3) + "&do=" + planDay(2)).Body.String()
	if strings.Count(body, "Polazak ne može biti u prošlosti.") != 1 {
		t.Error("a date error for the whole fleet is not said exactly once")
	}
}
