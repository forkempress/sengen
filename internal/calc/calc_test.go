package calc

import (
	"math/rand"
	"testing"
	"time"
)

func d(s string) time.Time {
	t, err := time.Parse("02.01.2006", s)
	if err != nil {
		panic(err)
	}
	return t
}

func dp(s string) *time.Time {
	t := d(s)
	return &t
}

// markoStays is the golden fixture from the spec: driver Marko, today = 13.10.2026.
func markoStays() []Stay {
	return []Stay{
		{Entry: d("22.04.2026"), Exit: dp("29.04.2026"), Type: TypeWork},
		{Entry: d("06.05.2026"), Exit: dp("15.05.2026"), Type: TypeWork},
		{Entry: d("26.05.2026"), Exit: dp("02.06.2026"), Type: TypeWork},
		{Entry: d("15.06.2026"), Exit: dp("24.06.2026"), Type: TypeWork},
		{Entry: d("08.07.2026"), Exit: dp("14.07.2026"), Type: TypePrivate},
		{Entry: d("12.08.2026"), Exit: dp("18.08.2026"), Type: TypeWork},
		{Entry: d("27.08.2026"), Exit: dp("03.09.2026"), Type: TypeWork},
		{Entry: d("14.09.2026"), Exit: dp("21.09.2026"), Type: TypeWork},
		{Entry: d("29.09.2026"), Exit: dp("04.10.2026"), Type: TypeWork},
		{Entry: d("10.10.2026"), Exit: nil, Type: TypeWork},
	}
}

func TestGolden_UsedDaysAndRemaining(t *testing.T) {
	today := d("13.10.2026")
	stays := markoStays()

	got := UsedDays(stays, today)
	if got != 76 {
		t.Errorf("UsedDays(today) = %d, want 76", got)
	}

	rem := Remaining(stays, today)
	if rem != 14 {
		t.Errorf("Remaining(today) = %d, want 14", rem)
	}
}

func TestGolden_MaxContinuousUntil(t *testing.T) {
	today := d("13.10.2026")
	stays := markoStays()

	until, ok := MaxContinuousUntil(stays, today)
	if !ok {
		t.Fatal("expected driver to be currently in Schengen")
	}
	want := d("14.11.2026")
	if !until.Equal(want) {
		t.Errorf("MaxContinuousUntil = %s, want %s", until.Format("02.01.2006"), want.Format("02.01.2006"))
	}

	daysLeft := daysBetween(today, until)
	if daysLeft != 32 {
		t.Errorf("days until MaxContinuousUntil = %d, want 32", daysLeft)
	}
}

func TestGolden_FreeUpSchedule(t *testing.T) {
	today := d("13.10.2026")
	stays := markoStays()

	events := FreeUpSchedule(stays, today, 60)
	if len(events) == 0 {
		t.Fatal("expected at least one free-up event")
	}
	first := events[0]
	wantDate := d("19.10.2026")
	if !first.Date.Equal(wantDate) {
		t.Errorf("first free-up date = %s, want %s", first.Date.Format("02.01.2006"), wantDate.Format("02.01.2006"))
	}

	// Cumulative freed days from today through 26.10 (inclusive) should reach +8.
	cutoff := d("26.10.2026")
	total := 0
	for _, e := range events {
		if !e.Date.After(cutoff) {
			total += e.DaysFreed
		}
	}
	if total != 8 {
		t.Errorf("cumulative days freed by 26.10 = %d, want 8", total)
	}
}

// Golden scenario continuation: "ako se vrati 18.10" — close the open stay on 18.10,
// then evaluate a planned trip starting 25.10 for 20 days.
func TestGolden_CanDoTripAndMaxTripLength(t *testing.T) {
	stays := markoStays()
	// Replace the open stay with one closed on 18.10.2026.
	closed := make([]Stay, 0, len(stays))
	for _, s := range stays {
		if s.Exit == nil {
			closed = append(closed, Stay{Entry: s.Entry, Exit: dp("18.10.2026"), Type: s.Type})
			continue
		}
		closed = append(closed, s)
	}

	ok, peak, _ := CanDoTrip(closed, d("25.10.2026"), 20)
	if !ok {
		t.Error("CanDoTrip(25.10, 20) = not ok, want ok")
	}
	if peak != 83 {
		t.Errorf("CanDoTrip peak used = %d, want 83", peak)
	}

	maxLen := MaxTripLength(closed, d("25.10.2026"))
	if maxLen != 27 {
		t.Errorf("MaxTripLength(25.10) = %d, want 27", maxLen)
	}
	end := addDays(d("25.10.2026"), maxLen-1)
	wantEnd := d("20.11.2026")
	if !end.Equal(wantEnd) {
		t.Errorf("MaxTripLength end date = %s, want %s", end.Format("02.01.2006"), wantEnd.Format("02.01.2006"))
	}
}

// --- Tabular tests ---

func TestSameDayEntryExit(t *testing.T) {
	stays := []Stay{{Entry: d("01.06.2026"), Exit: dp("01.06.2026"), Type: TypeWork}}
	got := UsedDays(stays, d("01.06.2026"))
	if got != 1 {
		t.Errorf("same-day entry/exit = %d, want 1", got)
	}
}

func TestStaySpanningWindowBoundary(t *testing.T) {
	// Window for d=30.06.2026 is [03.01.2026, 30.06.2026] (180 days inclusive).
	today := d("30.06.2026")
	winStart := addDays(today, -179)
	stays := []Stay{{Entry: addDays(winStart, -5), Exit: dp(formatDate(addDays(winStart, 10))), Type: TypeWork}}
	got := UsedDays(stays, today)
	// Only days from winStart through winStart+10 count: 11 days.
	if got != 11 {
		t.Errorf("UsedDays across window boundary = %d, want 11", got)
	}
}

func formatDate(t time.Time) string {
	return t.Format("02.01.2006")
}

func TestExactly90IsOk(t *testing.T) {
	today := d("01.07.2026")
	stays := []Stay{{Entry: addDays(today, -89), Exit: dp(formatDate(today)), Type: TypeWork}}
	if UsedDays(stays, today) != 90 {
		t.Fatalf("setup error: expected exactly 90 used days")
	}
	if Remaining(stays, today) != 0 {
		t.Errorf("Remaining at exactly 90 = %d, want 0", Remaining(stays, today))
	}
	if len(Overstays(stays, today)) != 0 {
		t.Errorf("exactly 90 should not be an overstay")
	}
}

func TestNinetyOneIsOverstay(t *testing.T) {
	today := d("01.07.2026")
	stays := []Stay{{Entry: addDays(today, -90), Exit: dp(formatDate(today)), Type: TypeWork}}
	if UsedDays(stays, today) != 91 {
		t.Fatalf("setup error: expected 91 used days")
	}
	overstays := Overstays(stays, today)
	if len(overstays) == 0 {
		t.Error("91 used days should register as overstay")
	}
}

func TestEmptyHistoryGivesFullAllowance(t *testing.T) {
	if Remaining(nil, d("01.01.2026")) != 90 {
		t.Error("empty history should give full 90-day allowance")
	}
	if UsedDays(nil, d("01.01.2026")) != 0 {
		t.Error("empty history should have 0 used days")
	}
}

func TestLeapYear(t *testing.T) {
	// 2028 is a leap year; stay spans 28.02-01.03.2028, must count 29.02 correctly.
	stays := []Stay{{Entry: d("27.02.2028"), Exit: dp("02.03.2028"), Type: TypeWork}}
	got := UsedDays(stays, d("02.03.2028"))
	if got != 5 { // 27,28,29 feb + 1,2 mar
		t.Errorf("leap year UsedDays = %d, want 5", got)
	}
}

func TestOpenStayCountsThroughToday(t *testing.T) {
	stays := []Stay{{Entry: d("01.10.2026"), Exit: nil, Type: TypeWork}}
	got := UsedDays(stays, d("13.10.2026"))
	want := 13 // 01.10 through 13.10 inclusive
	if got != want {
		t.Errorf("open stay UsedDays = %d, want %d", got, want)
	}
}

// --- Property test ---

func TestProperty_UsedDaysNeverJumpsByMoreThanOnePerDay(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for trial := 0; trial < 50; trial++ {
		var stays []Stay
		cursor := d("01.01.2026")
		for i := 0; i < 15; i++ {
			gap := rng.Intn(10)
			cursor = addDays(cursor, gap)
			length := 1 + rng.Intn(20)
			entry := cursor
			exit := addDays(cursor, length-1)
			typ := TypeWork
			if rng.Intn(2) == 0 {
				typ = TypePrivate
			}
			stays = append(stays, Stay{Entry: entry, Exit: &exit, Type: typ})
			cursor = addDays(exit, 1)
		}
		start := d("01.01.2026")
		prev := UsedDays(stays, start)
		for i := 1; i <= 400; i++ {
			day := addDays(start, i)
			cur := UsedDays(stays, day)
			if cur-prev > 1 {
				t.Fatalf("trial %d: UsedDays jumped by %d from %s to %s", trial, cur-prev, day.AddDate(0, 0, -1).Format("02.01.2006"), day.Format("02.01.2006"))
			}
			prev = cur
		}
	}
}

// --- EarliestStart ---

func TestEarliestStart_EmptyHistoryStartsImmediately(t *testing.T) {
	from := d("01.02.2026")
	got := EarliestStart(nil, 20, from)
	if !got.Equal(from) {
		t.Errorf("EarliestStart with no history = %s, want %s", got.Format("02.01.2006"), from.Format("02.01.2006"))
	}
}

// With the golden fixture closed on 18.10, a 20-day trip already fits on
// 25.10 (TestGolden_CanDoTripAndMaxTripLength), so the earliest start asked
// from that date must be that very date.
func TestEarliestStart_ReturnsFromWhenTripAlreadyFits(t *testing.T) {
	stays := markoStaysClosedOn18Oct()
	from := d("25.10.2026")
	got := EarliestStart(stays, 20, from)
	if !got.Equal(from) {
		t.Errorf("EarliestStart = %s, want %s", got.Format("02.01.2006"), from.Format("02.01.2006"))
	}
}

// A trip too long to fit today must be pushed out, and the date returned has
// to be the *first* one that works: the day before it must still fail.
func TestEarliestStart_IsTrulyTheEarliestFeasibleDay(t *testing.T) {
	stays := markoStaysClosedOn18Oct()
	from := d("19.10.2026")
	const length = 60

	got := EarliestStart(stays, length, from)
	if got.IsZero() {
		t.Fatalf("EarliestStart returned no date for a %d-day trip", length)
	}
	if got.Before(from) {
		t.Fatalf("EarliestStart = %s, which is before `from` %s", got.Format("02.01.2006"), from.Format("02.01.2006"))
	}
	if ok, _, _ := CanDoTrip(stays, got, length); !ok {
		t.Errorf("EarliestStart returned %s but CanDoTrip says the trip doesn't fit there", got.Format("02.01.2006"))
	}
	if got.After(from) {
		dayBefore := addDays(got, -1)
		if ok, _, _ := CanDoTrip(stays, dayBefore, length); ok {
			t.Errorf("trip also fits on %s, so %s is not the earliest start",
				dayBefore.Format("02.01.2006"), got.Format("02.01.2006"))
		}
	}
}

// No 91-day trip can ever satisfy the 90-day limit, regardless of history,
// so the search has to give up rather than return a bogus date.
func TestEarliestStart_ImpossibleTripReturnsZero(t *testing.T) {
	got := EarliestStart(nil, MaxDays+1, d("01.02.2026"))
	if !got.IsZero() {
		t.Errorf("EarliestStart for a %d-day trip = %s, want zero time", MaxDays+1, got.Format("02.01.2006"))
	}
}

// markoStaysClosedOn18Oct is the golden fixture with the open stay closed on
// 18.10.2026 — the "ako se vrati 18.10" scenario from the spec.
func markoStaysClosedOn18Oct() []Stay {
	stays := markoStays()
	out := make([]Stay, 0, len(stays))
	for _, s := range stays {
		if s.Exit == nil {
			out = append(out, Stay{Entry: s.Entry, Exit: dp("18.10.2026"), Type: s.Type})
			continue
		}
		out = append(out, s)
	}
	return out
}

// --- Overstays ---

func TestOverstays_CleanHistoryHasNone(t *testing.T) {
	if got := Overstays(markoStays(), d("13.10.2026")); len(got) != 0 {
		t.Errorf("golden fixture reports %d overstay days, want 0", len(got))
	}
}

func TestOverstays_EmptyHistory(t *testing.T) {
	if got := Overstays(nil, d("13.10.2026")); got != nil {
		t.Errorf("Overstays(nil) = %v, want nil", got)
	}
}

// The scan must start at the earliest entry even when the slice isn't sorted,
// otherwise overstay days before the first listed stay would be missed.
func TestOverstays_UnsortedStaysStillScanFromEarliestEntry(t *testing.T) {
	today := d("01.07.2026")
	stays := []Stay{
		{Entry: addDays(today, -40), Exit: dp(formatDate(today)), Type: TypeWork},
		{Entry: addDays(today, -120), Exit: dp(formatDate(addDays(today, -41))), Type: TypeWork},
	}

	got := Overstays(stays, today)
	if len(got) == 0 {
		t.Fatal("expected overstay days for a 121-day continuous stay")
	}
	firstOverstay := got[0]
	if firstOverstay.UsedDays != MaxDays+1 {
		t.Errorf("first overstay day reports %d used days, want %d", firstOverstay.UsedDays, MaxDays+1)
	}
	// Day 91 of a stay that began 120 days ago: entry + 90.
	wantDate := addDays(today, -120+MaxDays)
	if !firstOverstay.Date.Equal(wantDate) {
		t.Errorf("first overstay = %s, want %s", firstOverstay.Date.Format("02.01.2006"), wantDate.Format("02.01.2006"))
	}
}

// --- Remaining / MaxContinuousUntil edge cases ---

// Spec: Remaining = max(0, 90 − UsedDays). A driver already over the limit
// must read 0, never a negative number, so the UI can't show "−5 preostalo".
func TestRemaining_ClampsToZeroOnOverstay(t *testing.T) {
	today := d("01.07.2026")
	stays := []Stay{{Entry: addDays(today, -99), Exit: dp(formatDate(today)), Type: TypeWork}}

	if used := UsedDays(stays, today); used != 100 {
		t.Fatalf("setup error: UsedDays = %d, want 100", used)
	}
	if got := Remaining(stays, today); got != 0 {
		t.Errorf("Remaining for an overstayed driver = %d, want 0", got)
	}
}

// A driver who is back in Serbia has no open stay, so there is no
// "can stay until" date — the dashboard relies on the false return to show
// "najduža tura sada" instead.
func TestMaxContinuousUntil_NotInSchengen(t *testing.T) {
	got, ok := MaxContinuousUntil(markoStaysClosedOn18Oct(), d("19.10.2026"))
	if ok {
		t.Errorf("MaxContinuousUntil reported ok for a driver outside Schengen (got %s)", got.Format("02.01.2006"))
	}
	if !got.IsZero() {
		t.Errorf("MaxContinuousUntil returned %s, want zero time", got.Format("02.01.2006"))
	}

	if _, ok := MaxContinuousUntil(nil, d("19.10.2026")); ok {
		t.Error("MaxContinuousUntil reported ok for a driver with no stays at all")
	}
}

// --- MaxContinuousUntil when the limit is already exceeded ---

// A driver who is already over 90 days today has no legal day left. The
// function must not report today as allowed: it returns the last day of the
// stay on which the driver was still within the limit — a deadline that has
// already passed — so the UI can say "trebalo je da izađe do X".
func TestMaxContinuousUntil_AlreadyOverLimit(t *testing.T) {
	entry := d("01.06.2026")
	today := addDays(entry, 92) // 93rd day of a continuous stay
	stays := []Stay{{Entry: entry, Exit: nil, Type: TypeWork}}

	if used := UsedDays(stays, today); used != 93 {
		t.Fatalf("setup: UsedDays = %d, want 93", used)
	}

	until, ok := MaxContinuousUntil(stays, today)
	if !ok {
		t.Fatal("driver is in Schengen, ok should be true")
	}
	if !until.Before(today) {
		t.Errorf("MaxContinuousUntil = %s, which is not before today %s — it claims an illegal day is allowed",
			until.Format("02.01.2006"), today.Format("02.01.2006"))
	}
	wantLastLegal := addDays(entry, 89) // day 90
	if !until.Equal(wantLastLegal) {
		t.Errorf("MaxContinuousUntil = %s, want last legal day %s", until.Format("02.01.2006"), wantLastLegal.Format("02.01.2006"))
	}
}

// Golden fixture, one day past the spec's 14.11 deadline: the answer must
// still be 14.11, not 15.11.
func TestMaxContinuousUntil_GoldenOneDayPastDeadline(t *testing.T) {
	until, ok := MaxContinuousUntil(markoStays(), d("15.11.2026"))
	if !ok {
		t.Fatal("ok should be true while the stay is open")
	}
	if want := d("14.11.2026"); !until.Equal(want) {
		t.Errorf("MaxContinuousUntil(15.11) = %s, want %s", until.Format("02.01.2006"), want.Format("02.01.2006"))
	}
}

// Exactly on the limit (90/90) today is still a legal day.
func TestMaxContinuousUntil_ExactlyAtLimitTodayIsLegal(t *testing.T) {
	entry := d("01.06.2026")
	today := addDays(entry, 89) // day 90
	stays := []Stay{{Entry: entry, Exit: nil, Type: TypeWork}}

	until, _ := MaxContinuousUntil(stays, today)
	if !until.Equal(today) {
		t.Errorf("at exactly 90 days MaxContinuousUntil = %s, want today %s", until.Format("02.01.2006"), today.Format("02.01.2006"))
	}
}

// A driver who had already used all 90 days and enters again is over the
// limit on the very first day: there was no legal day in this stay at all,
// so the reported deadline is the day before entering.
func TestMaxContinuousUntil_OverFromTheFirstDayOfStay(t *testing.T) {
	first := d("01.06.2026")
	firstExit := addDays(first, 89) // 90 days used
	reentry := addDays(firstExit, 1)
	stays := []Stay{
		{Entry: first, Exit: &firstExit, Type: TypeWork},
		{Entry: reentry, Exit: nil, Type: TypeWork},
	}
	if used := UsedDays(stays, reentry); used != 91 {
		t.Fatalf("setup: UsedDays on re-entry = %d, want 91", used)
	}

	until, ok := MaxContinuousUntil(stays, reentry)
	if !ok {
		t.Fatal("ok should be true while the stay is open")
	}
	if want := addDays(reentry, -1); !until.Equal(want) {
		t.Errorf("MaxContinuousUntil = %s, want %s (the day before entering)", until.Format("02.01.2006"), want.Format("02.01.2006"))
	}
}
