package web

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"shengen/internal/calc"
	"shengen/internal/status"
	"shengen/internal/store"
)

func calDay(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func calStay(entry time.Time, exit *time.Time) store.Stay {
	return store.Stay{Entry: entry, Exit: exit, Type: "work"}
}

func monthCells(g MonthGrid) []DayCell {
	var out []DayCell
	for _, w := range g.Weeks {
		for _, c := range w {
			if c.Day != 0 {
				out = append(out, c)
			}
		}
	}
	return out
}

func buildFor(stays []store.Stay, plans []store.PlannedStay, today, month time.Time) MonthGrid {
	ds := status.Compute(store.Driver{}, stays, today)
	views := status.EvaluatePlans(stays, plans, today)
	return buildMonth(stays, views, ds, status.DefaultThresholds, today, month)
}

func TestBuildMonth_NumbersFromTodayOn(t *testing.T) {
	today := calDay(2026, 10, 16)
	exit := calDay(2026, 10, 1)
	stays := []store.Stay{calStay(calDay(2026, 8, 1), &exit)} // 62 days
	g := buildFor(stays, nil, today, today)

	ds := status.Compute(store.Driver{}, stays, today)
	for _, c := range monthCells(g) {
		if c.Future != !c.Date.Before(today) {
			t.Errorf("%s: Future = %v", c.Date.Format("02.01."), c.Future)
		}
		if c.Remaining != calc.MaxDays-c.Used {
			t.Errorf("%s: remaining %d with %d used", c.Date.Format("02.01."), c.Remaining, c.Used)
		}
		if c.Future && !c.TripBusy && c.Trip < c.Remaining {
			t.Errorf("%s: trip %d shorter than remaining %d", c.Date.Format("02.01."), c.Trip, c.Remaining)
		}
	}
	c, _ := g.cellOn(today)
	if c.Remaining != ds.Remaining || c.Trip != ds.MaxTripLengthToday {
		t.Errorf("today = %d left, trip %d; status says %d, trip %d", c.Remaining, c.Trip, ds.Remaining, ds.MaxTripLengthToday)
	}
	if past, _ := g.cellOn(calDay(2026, 10, 1)); past.Future || past.Used != 62 {
		t.Errorf("1.10. = future %v, used %d; want past, 62", past.Future, past.Used)
	}
}

func TestBuildMonth_InSchengenCountsDownToDeadline(t *testing.T) {
	today := calDay(2026, 10, 16)
	stays := []store.Stay{calStay(calDay(2026, 8, 1), nil)} // 77 days so far
	ds := status.Compute(store.Driver{}, stays, today)
	deadline := *ds.MaxContinuousUntil

	g := buildFor(stays, nil, today, today)
	prev := -1
	for _, c := range monthCells(g) {
		if !c.Date.After(today) || c.Date.After(deadline) {
			continue
		}
		if c.Class != "day-projection" || !c.TripBusy {
			t.Errorf("%s: class %q busy %v, want projection, busy", c.Date.Format("02.01."), c.Class, c.TripBusy)
		}
		if prev >= 0 && c.Remaining != prev-1 {
			t.Errorf("%s: remaining %d after %d", c.Date.Format("02.01."), c.Remaining, prev)
		}
		prev = c.Remaining
	}
	if c, ok := g.cellOn(deadline); ok && c.Remaining != 0 {
		t.Errorf("deadline %s: remaining %d, want 0", deadline.Format("02.01."), c.Remaining)
	}
}

func TestBuildMonth_PlusWhenTheTripRunsLonger(t *testing.T) {
	today := calDay(2026, 10, 16)
	// 85 days at the start of the window: 5 left today, but each day away
	// one old day drops out, so the trip runs far longer.
	entry := today.AddDate(0, 0, -(calc.WindowDays - 1))
	exit := entry.AddDate(0, 0, 84)
	g := buildFor([]store.Stay{calStay(entry, &exit)}, nil, today, today)

	c, _ := g.cellOn(today)
	if c.Remaining != 5 || !c.TripBetter || c.Trip <= 15 {
		t.Errorf("today: %d left, trip %d, ⁺ %v; want 5 left, trip over 15, ⁺", c.Remaining, c.Trip, c.TripBetter)
	}

	plenty := buildFor(nil, nil, today, today)
	if c, _ := plenty.cellOn(today); c.TripBetter {
		t.Error("⁺ on a driver with every day free")
	}
}

func TestBuildMonth_PlannedDays(t *testing.T) {
	today := calDay(2026, 10, 16)
	plans := []store.PlannedStay{{ID: 1, From: calDay(2026, 11, 2), To: calDay(2026, 11, 5)}}
	g := buildFor(nil, plans, today, calDay(2026, 11, 1))
	c, _ := g.cellOn(calDay(2026, 11, 3))
	if c.Class != "day-planned" || !c.TripBusy || c.Used != 2 {
		t.Errorf("3.11.: class %q busy %v used %d; want planned, busy, 2", c.Class, c.TripBusy, c.Used)
	}
	if c, _ := g.cellOn(calDay(2026, 11, 1)); !c.TripUntilPlan || c.Trip != 1 {
		t.Errorf("1.11.: trip %d until plan %v; want 1, cut by the plan", c.Trip, c.TripUntilPlan)
	}
}

func TestDriverPage_MonthNavigation(t *testing.T) {
	app := newTestApp(t)
	f := app.addFirm("Prevoz A", "prevoz-a", "a@prevoz.rs", "Marko Test", 10)
	s := app.mustLogin(f)
	path := "/vozaci/" + id(f.driver.ID)

	page := s.get(path + "?mesec=nije-mesec").Body.String()
	if !strings.Contains(page, `id="kalendar"`) || strings.Contains(page, ">Danas</a>") {
		t.Error("a bad month does not fall back to today's")
	}
	if !strings.Contains(page, "cal-show-remaining") {
		t.Error("calendar does not show remaining days by default")
	}

	far := s.get(path + "?mesec=2099-01").Body.String()
	if !strings.Contains(far, ">Danas</a>") || strings.Contains(far, `aria-label="Sledeći mesec"`) {
		t.Error("a month past the range is not clamped to its end")
	}

	withTrip := s.get(path + "?od=" + planDay(5) + "&do=" + planDay(9)).Body.String()
	if !strings.Contains(withTrip, "od="+planDay(5)) || !strings.Contains(withTrip, "mesec=") {
		t.Error("month links drop the trip check")
	}
}

func TestCalendarNumberSetting(t *testing.T) {
	app := newTestApp(t)
	f := app.addFirm("Prevoz A", "prevoz-a", "a@prevoz.rs", "Marko Test", 10)
	s := app.mustLogin(f)
	s.post("/podesavanja/telegram/pragovi", url.Values{"threshold_red": {"10"}, "threshold_orange": {"20"}})

	if msg := flashFrom(s, s.post("/podesavanja/kalendar", url.Values{"calendar_number": {"nesto"}}), "err"); msg == "" {
		t.Error("an unknown calendar number is accepted")
	}
	s.post("/podesavanja/kalendar", url.Values{"calendar_number": {store.CalendarTrip}})
	if page := s.get("/vozaci/" + id(f.driver.ID)).Body.String(); !strings.Contains(page, "cal-show-trip") {
		t.Error("the firm's choice does not change the calendar")
	}
	settings := s.get("/podesavanja/telegram").Body.String()
	if !strings.Contains(settings, `name="threshold_red" min="1" max="90" inputmode="numeric" value="10"`) ||
		!strings.Contains(settings, `id="cal-trip" name="calendar_number" value="trip" checked`) {
		t.Error("saving the calendar lost the thresholds, or the choice is not shown")
	}
}
