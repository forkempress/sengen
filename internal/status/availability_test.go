package status

import (
	"testing"
	"time"

	"shengen/internal/store"
)

func TestScheduleStarts_TodayThenMondays(t *testing.T) {
	got := ScheduleStarts(d("15.10.2026"), 3) // a Thursday
	want := []string{"15.10.2026", "19.10.2026", "26.10.2026", "02.11.2026"}
	for i, w := range want {
		if got[i].Format("02.01.2006") != w {
			t.Errorf("start %d = %s, want %s", i, got[i].Format("02.01.2006"), w)
		}
	}
	if got := ScheduleStarts(d("19.10.2026"), 1); got[1].Format("02.01.2006") != "26.10.2026" {
		t.Errorf("on a Monday the next column is %s, want the next Monday", got[1].Format("02.01.2006"))
	}
}

func TestAvailability_BusyFreeAndCutByPlan(t *testing.T) {
	today := d("13.10.2026")
	plans := []store.PlannedStay{{DriverID: 1, From: d("02.11.2026"), To: d("09.11.2026")}}
	cells := Availability(markoStays(), plans, today,
		[]time.Time{d("13.10.2026"), d("20.10.2026"), d("05.11.2026"), d("01.12.2026")}, DefaultThresholds)

	if !cells[0].Busy {
		t.Error("driver in Schengen today is not shown as busy today")
	}
	if c := cells[1]; c.Busy || !c.UntilPlan || c.MaxLength != 13 {
		t.Errorf("20.10: %+v; want free, cut by the plan on 02.11 to 13 days", c)
	}
	if !cells[2].Busy {
		t.Error("a day inside a saved plan is not busy")
	}
	if c := cells[3]; c.Busy || c.UntilPlan || c.MaxLength <= 0 {
		t.Errorf("01.12: %+v; want free with days to spare", c)
	}
}
