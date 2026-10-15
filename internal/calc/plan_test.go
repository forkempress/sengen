package calc

import (
	"errors"
	"testing"
)

func plan(from, to string) Stay {
	return Stay{Entry: d(from), Exit: dp(to), Type: TypeWork}
}

func mustCheck(t *testing.T, stays, plans []Stay, today, from, to string) TripCheck {
	t.Helper()
	c, err := CheckTrip(stays, plans, d(today), d(from), d(to))
	if err != nil {
		t.Fatalf("CheckTrip %s–%s: %v", from, to, err)
	}
	return c
}

func TestCheckTrip_WithinLimit(t *testing.T) {
	c := mustCheck(t, nil, nil, "01.11.2026", "10.11.2026", "29.11.2026")
	if !c.OK || c.Length != 20 || c.PeakUsed != 20 || c.UsedOnReturn != 20 || c.RemainingAfter != 70 {
		t.Errorf("got %+v, want OK, 20 days, 20 used, 70 left", c)
	}
	if c.MaxLength != 90 || formatDate(c.LastAllowedDay) != "07.02.2027" {
		t.Errorf("MaxLength %d, LastAllowedDay %s; want 90, 07.02.2027", c.MaxLength, formatDate(c.LastAllowedDay))
	}
	if !c.EarliestStart.IsZero() || c.AssumedReturn != nil || c.BreaksPlan != -1 || len(c.OverlapsPlans) != 0 {
		t.Errorf("unexpected extras: %+v", c)
	}
}

// 30 days in October, then a 60-day trip ending on 31.12: the 90th day is
// allowed, the 91st is not.
func TestCheckTrip_ExactlyOnThe90thDay(t *testing.T) {
	history := []Stay{{Entry: d("01.10.2026"), Exit: dp("30.10.2026"), Type: TypeWork}}

	c := mustCheck(t, history, nil, "01.11.2026", "02.11.2026", "31.12.2026")
	if !c.OK || c.Length != 60 || c.UsedOnReturn != 90 || c.RemainingAfter != 0 || c.PeakUsed != 90 {
		t.Errorf("60-day trip: %+v, want OK with 90 used and 0 left", c)
	}
	if c.MaxLength != 60 || formatDate(c.LastAllowedDay) != "31.12.2026" {
		t.Errorf("MaxLength %d, LastAllowedDay %s; want 60, 31.12.2026", c.MaxLength, formatDate(c.LastAllowedDay))
	}

	over := mustCheck(t, history, nil, "01.11.2026", "02.11.2026", "01.01.2027")
	if over.OK || formatDate(over.FirstOverstay) != "01.01.2027" || over.UsedOnReturn != 91 || over.RemainingAfter != 0 {
		t.Errorf("61-day trip: %+v, want refused from 01.01.2027 with 91 used", over)
	}
	// October's first day leaves the window on 30.03.2027, so a 61-day
	// trip fits from 29.01.2027 at the earliest.
	if formatDate(over.EarliestStart) != "29.01.2027" {
		t.Errorf("EarliestStart = %s, want 29.01.2027", formatDate(over.EarliestStart))
	}
}

func TestCheckTrip_OverLimitWithNoDayLeft(t *testing.T) {
	history := []Stay{{Entry: d("03.08.2026"), Exit: dp("31.10.2026"), Type: TypeWork}} // 90 days
	c := mustCheck(t, history, nil, "01.11.2026", "02.11.2026", "03.11.2026")
	if c.OK || formatDate(c.FirstOverstay) != "02.11.2026" {
		t.Errorf("got %+v, want refused from the first day", c)
	}
	if c.MaxLength != 0 || !c.LastAllowedDay.IsZero() {
		t.Errorf("MaxLength %d, LastAllowedDay %v; want 0 and none", c.MaxLength, c.LastAllowedDay)
	}
	if c.EarliestStart.IsZero() || !c.EarliestStart.After(d("02.11.2026")) {
		t.Errorf("EarliestStart = %v, want a later day", c.EarliestStart)
	}
}

func TestCheckTrip_RefusesBadInput(t *testing.T) {
	for _, tc := range []struct {
		name, from, to string
		want           error
	}{
		{"u prošlosti", "31.10.2026", "05.11.2026", ErrTripInPast},
		{"povratak pre polaska", "10.11.2026", "09.11.2026", ErrTripBadRange},
		{"duže od 90", "10.11.2026", "08.02.2027", ErrTripTooLong},
	} {
		if _, err := CheckTrip(nil, nil, d("01.11.2026"), d(tc.from), d(tc.to)); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	// Exactly 90 days is a valid question.
	if _, err := CheckTrip(nil, nil, d("01.11.2026"), d("10.11.2026"), d("07.02.2027")); err != nil {
		t.Errorf("90-day trip: %v", err)
	}
}

// A driver in Schengen since 25.10 cannot start a new trip today; one that
// starts later is planned as if he came back today.
func TestCheckTrip_StartsWhileStillInSchengen(t *testing.T) {
	stays := []Stay{{Entry: d("25.10.2026"), Type: TypeWork}}

	c, err := CheckTrip(stays, nil, d("01.11.2026"), d("01.11.2026"), d("05.11.2026"))
	if !errors.Is(err, ErrTripStartsWhileAway) {
		t.Fatalf("err = %v, want ErrTripStartsWhileAway", err)
	}
	if c.MaxContinuousUntil == nil || !c.MaxContinuousUntil.After(d("01.11.2026")) {
		t.Errorf("MaxContinuousUntil = %v, want a day after today", c.MaxContinuousUntil)
	}

	later := mustCheck(t, stays, nil, "01.11.2026", "05.11.2026", "09.11.2026")
	if later.AssumedReturn == nil || formatDate(*later.AssumedReturn) != "01.11.2026" {
		t.Fatalf("AssumedReturn = %v, want today", later.AssumedReturn)
	}
	// 25.10–01.11 is 8 days, plus 5: counted once, not through the trip.
	if !later.OK || later.UsedOnReturn != 13 {
		t.Errorf("got %+v, want OK with 13 used", later)
	}
}

// Two plans that fit together (46 + 43 = 89 days) leave one day; a trip
// after them breaks the limit, and a trip slipped in between them pushes the
// second plan over.
func TestCheckTrip_SeveralPlansInARow(t *testing.T) {
	plans := []Stay{plan("01.12.2026", "15.01.2027"), plan("01.02.2027", "15.03.2027")}
	const today = "01.11.2026"

	res := EvaluatePlans(nil, plans, d(today))
	if !res[0].OK || res[0].UsedOnReturn != 46 || res[0].RemainingAfter != 44 {
		t.Errorf("first plan: %+v, want OK, 46 used, 44 left", res[0])
	}
	if !res[1].OK || res[1].UsedOnReturn != 89 || res[1].RemainingAfter != 1 {
		t.Errorf("second plan: %+v, want OK, 89 used, 1 left", res[1])
	}

	after := mustCheck(t, nil, plans, today, "20.03.2027", "21.03.2027")
	if after.OK || formatDate(after.FirstOverstay) != "21.03.2027" || after.MaxLength != 1 {
		t.Errorf("trip after both plans: %+v, want refused on 21.03.2027, longest 1 day", after)
	}

	between := mustCheck(t, nil, plans, today, "16.01.2027", "17.01.2027")
	if !between.OK {
		t.Errorf("trip between the plans should fit on its own: %+v", between)
	}
	if between.BreaksPlan != 1 || formatDate(between.BreaksOn) != "15.03.2027" {
		t.Errorf("BreaksPlan %d on %s, want plan 1 on 15.03.2027", between.BreaksPlan, formatDate(between.BreaksOn))
	}

	overlap := mustCheck(t, nil, plans, today, "10.01.2027", "20.01.2027")
	if len(overlap.OverlapsPlans) != 1 || overlap.OverlapsPlans[0] != 0 {
		t.Errorf("OverlapsPlans = %v, want [0]", overlap.OverlapsPlans)
	}
}

func TestStateOf(t *testing.T) {
	today := d("01.11.2026")
	p := plan("25.10.2026", "10.11.2026")
	for _, tc := range []struct {
		name  string
		stays []Stay
		plan  Stay
		want  PlanState
	}{
		{"nothing recorded yet", nil, plan("05.11.2026", "10.11.2026"), PlanUpcoming},
		{"started, not entered yet", nil, p, PlanUpcoming},
		{"driver entered, still away", []Stay{{Entry: d("26.10.2026")}}, p, PlanInProgress},
		{"driver came back", []Stay{{Entry: d("26.10.2026"), Exit: dp("30.10.2026")}}, p, PlanRealized},
		{"dates passed, no stay", nil, plan("01.10.2026", "05.10.2026"), PlanMissed},
		{"stay elsewhere", []Stay{{Entry: d("01.09.2026"), Exit: dp("05.09.2026")}}, plan("05.11.2026", "10.11.2026"), PlanUpcoming},
	} {
		if got := StateOf(tc.plan, tc.stays, today); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The plan a driver is on tells when he is expected back: until then his
// open stay keeps counting, and a later trip is checked against that.
func TestCheckTrip_PlanInProgressSetsTheReturn(t *testing.T) {
	stays := []Stay{{Entry: d("28.10.2026"), Type: TypeWork}}
	plans := []Stay{plan("28.10.2026", "10.11.2026")}

	c := mustCheck(t, stays, plans, "01.11.2026", "20.11.2026", "21.11.2026")
	if c.AssumedReturn == nil || formatDate(*c.AssumedReturn) != "10.11.2026" {
		t.Fatalf("AssumedReturn = %v, want 10.11.2026", c.AssumedReturn)
	}
	if c.UsedOnReturn != 16 { // 28.10–10.11 (14) + 2
		t.Errorf("UsedOnReturn = %d, want 16", c.UsedOnReturn)
	}
	res := EvaluatePlans(stays, plans, d("01.11.2026"))
	if res[0].State != PlanInProgress || !res[0].Evaluated || res[0].UsedOnReturn != 14 {
		t.Errorf("in-progress plan: %+v, want evaluated with 14 used", res[0])
	}

	// A trip starting before he is expected back overlaps the plan.
	early := mustCheck(t, stays, plans, "01.11.2026", "05.11.2026", "06.11.2026")
	if len(early.OverlapsPlans) != 1 {
		t.Errorf("OverlapsPlans = %v, want the plan in progress", early.OverlapsPlans)
	}
}

// Realized and missed plans no longer count; an upcoming plan whose first
// days have passed without an entry counts from today on.
func TestProjection_OnlyPlansStillAheadCount(t *testing.T) {
	today := d("01.11.2026")
	stays := []Stay{{Entry: d("02.10.2026"), Exit: dp("04.10.2026"), Type: TypeWork}}
	plans := []Stay{
		plan("01.10.2026", "05.10.2026"), // realized by the stay above
		plan("10.10.2026", "12.10.2026"), // missed
		plan("30.10.2026", "05.11.2026"), // started, not entered: counts 01.11–05.11
	}
	proj := Projection(stays, plans, today)
	if got := UsedDays(proj, d("05.11.2026")); got != 3+5 {
		t.Errorf("UsedDays on 05.11 = %d, want 8", got)
	}
	res := EvaluatePlans(stays, plans, today)
	if res[0].State != PlanRealized || res[0].Evaluated || res[1].State != PlanMissed || res[1].Evaluated {
		t.Errorf("past plans: %+v %+v, want realized and missed, not evaluated", res[0], res[1])
	}
	if !res[2].Evaluated || res[2].UsedOnReturn != 8 || res[2].PeakUsed != 8 {
		t.Errorf("current plan: %+v, want 8 used", res[2])
	}
}

// Plans do not change the real history, and an overstaying plan is reported
// on its first day over.
func TestEvaluatePlans_FlagsAPlanOverTheLimit(t *testing.T) {
	history := []Stay{{Entry: d("01.09.2026"), Exit: dp("30.10.2026"), Type: TypeWork}} // 60 days
	plans := []Stay{plan("10.11.2026", "15.12.2026")}                                   // 36 days
	res := EvaluatePlans(history, plans, d("01.11.2026"))[0]
	if res.OK || formatDate(res.FirstOverstay) != "10.12.2026" || res.RemainingAfter != 0 {
		t.Errorf("got %+v, want over from 10.12.2026", res)
	}
	if history[0].Exit == nil || formatDate(*history[0].Exit) != "30.10.2026" {
		t.Error("EvaluatePlans changed the caller's stays")
	}
}
