package web

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"shengen/internal/clock"
)

func TestDriverReport_CountsOnlyTheWindow(t *testing.T) {
	app := newTestApp(t)
	f := app.addFirm("Prevoz A", "prevoz-a", "a@prevoz.rs", "Marko Test", 40) // 4 days, 40 days ago
	ctx := context.Background()
	old := clock.Today().AddDate(0, 0, -300)
	oldExit := old.AddDate(0, 0, 9)
	if _, err := app.st.CreateStay(ctx, f.tenant.ID, f.driver.ID, old, &oldExit, "work", "manual", "stara tura", nil); err != nil {
		t.Fatal(err)
	}
	// Starts before the window and ends inside it: only its days inside count.
	edge := clock.Today().AddDate(0, 0, -182)
	edgeExit := clock.Today().AddDate(0, 0, -178)
	if _, err := app.st.CreateStay(ctx, f.tenant.ID, f.driver.ID, edge, &edgeExit, "private", "manual", "", nil); err != nil {
		t.Fatal(err)
	}
	s := app.mustLogin(f)

	if !strings.Contains(s.get("/vozaci/"+id(f.driver.ID)).Body.String(), "/vozaci/"+id(f.driver.ID)+"/izvestaj") {
		t.Error("driver page does not link to the report")
	}
	w := s.get("/vozaci/" + id(f.driver.ID) + "/izvestaj")
	if w.Code != http.StatusOK {
		t.Fatalf("report = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"Boravci u Šengenu — Marko Test", "Prevoz A", "6/90", "data-print", "Starijih boravaka: 1"} {
		if !strings.Contains(body, want) {
			t.Errorf("report lacks %q", want)
		}
	}
	if strings.Contains(body, "stara tura") {
		t.Error("a stay outside the 180 days is listed")
	}
	if !strings.Contains(body, `<td class="num report-days">2</td>`) {
		t.Error("the stay crossing into the window does not count just its 2 days inside")
	}
}
