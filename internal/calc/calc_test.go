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
