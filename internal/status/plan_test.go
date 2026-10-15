package status

import (
	"testing"
	"time"

	"shengen/internal/store"
)

func pday(s string) time.Time {
	t, err := time.Parse("02.01.2006", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestEvaluatePlans_StatesOverlapsAndSummary(t *testing.T) {
	today := pday("01.11.2026")
	exit := pday("05.10.2026")
	stays := []store.Stay{{DriverID: 1, Entry: pday("02.10.2026"), Exit: &exit, Type: "work"}}
	plans := []store.PlannedStay{
		{ID: 1, DriverID: 1, From: pday("01.10.2026"), To: pday("06.10.2026")}, // realized
		{ID: 2, DriverID: 1, From: pday("10.11.2026"), To: pday("19.11.2026")},
		{ID: 3, DriverID: 1, From: pday("15.11.2026"), To: pday("10.02.2027")}, // overlaps 2, over the limit
	}
	views := EvaluatePlans(stays, plans, today)
	if views[0].StateLabel() != "realizovan" || views[0].StateKey() != "realized" || views[0].Active() {
		t.Errorf("first plan: %s/%s active=%v", views[0].StateLabel(), views[0].StateKey(), views[0].Active())
	}
	if views[1].StateLabel() != "planiran" || views[1].Length != 10 || !views[1].OK || !views[1].OverlapsOther {
		t.Errorf("second plan: %+v", views[1])
	}
	if views[2].OK || !views[2].OverlapsOther {
		t.Errorf("third plan should be over the limit and overlapping: %+v", views[2])
	}

	sum := Summarize(views)
	if sum.Ahead != 2 || sum.Next.ID != 2 || sum.Problem == nil || sum.Problem.ID != 3 || sum.More() != 1 {
		t.Errorf("summary: %+v", sum)
	}
	all := SummarizeAll(stays, plans, today)
	if len(all) != 1 || all[1].Problem == nil {
		t.Errorf("SummarizeAll = %+v", all)
	}
	if got := SummarizeAll(stays, plans[:1], today); len(got) != 0 {
		t.Errorf("a driver with only past plans is listed: %+v", got)
	}
}

func TestPlanLabels(t *testing.T) {
	if PlanDates(store.PlannedStay{From: pday("01.12.2026"), To: pday("20.12.2026")}) != "01.12.–20.12.2026" {
		t.Error("PlanDates within a year")
	}
	if PlanDates(store.PlannedStay{From: pday("20.12.2026"), To: pday("05.01.2027")}) != "20.12.2026–05.01.2027" {
		t.Error("PlanDates across years")
	}
	for n, want := range map[int]string{1: "dan", 21: "dan", 11: "dana", 5: "dana", -1: "dan", 0: "dana"} {
		if got := DaysWord(n); got != want {
			t.Errorf("DaysWord(%d) = %q, want %q", n, got, want)
		}
	}
	if (PlanSummary{Ahead: 1}).More() != 0 {
		t.Error("More with one plan")
	}
}
