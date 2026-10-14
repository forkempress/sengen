package web

import (
	"context"
	"strings"
	"testing"

	"shengen/internal/clock"
)

func TestDriverPage_TripCheck(t *testing.T) {
	app := newTestApp(t)
	f := app.addFirm("Prevoz A", "prevoz-a", "a@prevoz.rs", "Marko Test", 40)
	s := app.mustLogin(f)
	page := "/vozaci/" + id(f.driver.ID)
	day := func(n int) string { return clock.Today().AddDate(0, 0, n).Format("2006-01-02") }

	cases := []struct {
		name, query string
		want        []string
	}{
		{"moze", "?polazak=" + day(10) + "&dana=20", []string{"Može.", "Najduža moguća tura"}},
		{"predugo", "?polazak=" + day(10) + "&dana=120", []string{"od 1 do 90"}},
		{"u prošlosti", "?polazak=" + day(-1) + "&dana=5", []string{"ne može biti u prošlosti"}},
		{"bez datuma", "?dana=5", []string{"Izaberite datum polaska"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := s.get(page + c.query)
			if w.Code != 200 {
				t.Fatalf("status %d", w.Code)
			}
			for _, want := range c.want {
				if !strings.Contains(w.Body.String(), want) {
					t.Errorf("page does not contain %q", want)
				}
			}
		})
	}

	// Without the form submitted, there is no result or error on the page.
	if body := s.get(page).Body.String(); strings.Contains(body, "Može.") || strings.Contains(body, "flash-err") {
		t.Error("trip result shown without the form being submitted")
	}
}

// A driver who is away right now is planned as if he returns today, and the
// page says so; a trip starting while he is still away is refused.
func TestDriverPage_TripCheckWhileInSchengen(t *testing.T) {
	app := newTestApp(t)
	f := app.addFirm("Prevoz A", "prevoz-a", "a@prevoz.rs", "Marko Test", 40)
	today := clock.Today()
	if _, err := app.st.CreateStay(context.Background(), f.tenant.ID, f.driver.ID, today.AddDate(0, 0, -3), nil, "work", "manual", "", nil); err != nil {
		t.Fatal(err)
	}
	s := app.mustLogin(f)
	page := "/vozaci/" + id(f.driver.ID)

	body := s.get(page + "?polazak=" + today.AddDate(0, 0, 30).Format("2006-01-02") + "&dana=20").Body.String()
	if !strings.Contains(body, "Može.") || !strings.Contains(body, "vraća danas") {
		t.Errorf("expected a positive plan that assumes return today")
	}

	body = s.get(page + "?polazak=" + today.Format("2006-01-02") + "&dana=5").Body.String()
	if !strings.Contains(body, "tek kad se vrati") {
		t.Errorf("expected a refusal for a trip starting while he is away")
	}
}
