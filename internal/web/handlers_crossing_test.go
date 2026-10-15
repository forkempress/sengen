package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"shengen/internal/clock"
)

func TestCrossing_EntryExitAndSameDayReturnFromDashboard(t *testing.T) {
	app := newTestApp(t)
	f := app.addFirm("Prevoz A", "prevoz-a", "a@prevoz.rs", "Marko Test", 40)
	s := app.mustLogin(f)
	path := "/vozaci/" + id(f.driver.ID) + "/granica"
	today := planDay(0)
	ctx := context.Background()

	if !strings.Contains(s.get("/pregled").Body.String(), "▶ Ušao") {
		t.Fatal("driver at home has no entry button on the dashboard")
	}

	body := follow(s, s.post(path, url.Values{"smer": {"ulaz"}, "date": {today}, "back": {"/pregled"}}))
	if !strings.Contains(body, "Marko Test: ulazak u Šengen") {
		t.Errorf("no confirmation for the entry")
	}
	if !strings.Contains(body, "◀ Izašao") {
		t.Error("driver in Schengen has no exit button")
	}
	stays, _ := app.st.ListStaysByDriver(ctx, f.tenant.ID, f.driver.ID)
	if n := len(stays); n != 2 || stays[1].Exit != nil || stays[1].Source != "manual" {
		t.Fatalf("after entry: %d stays, last %+v; want a second, open, manual stay", n, stays[len(stays)-1])
	}

	body = follow(s, s.post(path, url.Values{"smer": {"izlaz"}, "date": {today}}))
	if !strings.Contains(body, "Marko Test: izlazak iz Šengena") {
		t.Error("no confirmation for the exit")
	}

	// Back in on the day he left: the stay continues, no overlapping one.
	body = follow(s, s.post(path, url.Values{"smer": {"ulaz"}, "date": {today}}))
	if !strings.Contains(body, "ponovo u Šengenu") {
		t.Error("same-day return not reported as a continued stay")
	}
	stays, _ = app.st.ListStaysByDriver(ctx, f.tenant.ID, f.driver.ID)
	if n := len(stays); n != 2 || stays[1].Exit != nil {
		t.Errorf("after same-day return: %d stays, want 2 with the last one open again", n)
	}
}

func TestCrossing_RefusalsComeBackAsMessages(t *testing.T) {
	app := newTestApp(t)
	f := app.addFirm("Prevoz A", "prevoz-a", "a@prevoz.rs", "Marko Test", 40)
	s := app.mustLogin(f)
	path := "/vozaci/" + id(f.driver.ID) + "/granica"

	body := follow(s, s.post(path, url.Values{"smer": {"izlaz"}, "date": {planDay(0)}}))
	if !strings.Contains(body, "nema otvoren boravak") {
		t.Error("exit without an open stay not explained")
	}
	body = follow(s, s.post(path, url.Values{"smer": {"ulaz"}, "date": {planDay(3)}}))
	if !strings.Contains(body, "flash-err") {
		t.Error("entry in the future accepted")
	}
	body = follow(s, s.post(path, url.Values{"smer": {"ulaz"}, "date": {"juče"}}))
	if !strings.Contains(body, "neispravan datum") {
		t.Error("bad date not explained")
	}
	if got := app.stayCount(f.tenant.ID, f.driver.ID); got != 1 {
		t.Errorf("refused crossings wrote stays: %d, want 1", got)
	}
}

func TestCrossing_ReturnsToTheSameDashboardViewOnly(t *testing.T) {
	app := newTestApp(t)
	f := app.addFirm("Prevoz A", "prevoz-a", "a@prevoz.rs", "Marko Test", 40)
	s := app.mustLogin(f)
	path := "/vozaci/" + id(f.driver.ID) + "/granica"

	for back, want := range map[string]string{
		"/pregled?status=zeleno": "/pregled?status=zeleno",
		"https://zlo.example/x":  "/pregled",
		"//zlo.example/pregled":  "/pregled",
		"/podesavanja/korisnici": "/pregled",
		"/podesavanja/vozaci":    "/podesavanja/vozaci",
	} {
		w := s.post(path, url.Values{"smer": {"ulaz"}, "date": {"x"}, "back": {back}})
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != want {
			t.Errorf("back=%q: %d to %q, want redirect to %q", back, w.Code, w.Header().Get("Location"), want)
		}
	}
}

func TestCrossing_OnlyOnTodaysDashboard(t *testing.T) {
	app := newTestApp(t)
	f := app.addFirm("Prevoz A", "prevoz-a", "a@prevoz.rs", "Marko Test", 40)
	s := app.mustLogin(f)
	past := clock.Today().AddDate(0, 0, -10).Format("2006-01-02")
	if strings.Contains(s.get("/pregled?na_dan="+past).Body.String(), "crossing-form") {
		t.Error("border buttons shown on a past date's dashboard")
	}
}
