package calc

import (
	"testing"
	"time"
)

// Edge cases of the 90/180 rule that the golden fixture doesn't exercise.
// Each one is a situation a driver actually ends up in on the road.

// The window is [d-179, d]: a day exactly 179 days back still counts, the
// day before it has already fallen out.
func TestWindow_FirstAndLastDayOfWindow(t *testing.T) {
	today := d("30.06.2026")
	in := addDays(today, -179)
	out := addDays(today, -180)

	if got := UsedDays([]Stay{{Entry: in, Exit: dayPtr(in), Type: TypeWork}}, today); got != 1 {
		t.Errorf("day d-179 = %d used, want 1 (still inside the window)", got)
	}
	if got := UsedDays([]Stay{{Entry: out, Exit: dayPtr(out), Type: TypeWork}}, today); got != 0 {
		t.Errorf("day d-180 = %d used, want 0 (already out of the window)", got)
	}
}

// Leaving Schengen and coming back the same day (Neum corridor, a short hop
// to Serbia) can be recorded as two stays touching on one date. That date is
// one day in Schengen, not two.
func TestTouchingStays_SharedDayCountsOnce(t *testing.T) {
	stays := []Stay{
		{Entry: d("01.06.2026"), Exit: dp("05.06.2026"), Type: TypeWork},
		{Entry: d("05.06.2026"), Exit: dp("10.06.2026"), Type: TypeWork},
	}
	if got := UsedDays(stays, d("10.06.2026")); got != 10 {
		t.Errorf("UsedDays = %d, want 10 (01.06–10.06, 05.06 counted once)", got)
	}
}

// Several crossings on one calendar date are still one day.
func TestSeveralCrossingsInOneDay_CountOnce(t *testing.T) {
	stays := []Stay{
		{Entry: d("01.06.2026"), Exit: dp("01.06.2026"), Type: TypeWork},
		{Entry: d("01.06.2026"), Exit: dp("01.06.2026"), Type: TypePrivate},
		{Entry: d("01.06.2026"), Exit: dp("01.06.2026"), Type: TypeWork},
	}
	if got := UsedDays(stays, d("01.06.2026")); got != 1 {
		t.Errorf("UsedDays = %d, want 1", got)
	}
}

// Overlapping records (e.g. an import on top of button presses) must not
// count any day twice; the store rejects them, but the rule itself must not
// depend on that.
func TestOverlappingStays_CountUnionOfDays(t *testing.T) {
	stays := []Stay{
		{Entry: d("01.06.2026"), Exit: dp("20.06.2026"), Type: TypeWork},
		{Entry: d("10.06.2026"), Exit: dp("30.06.2026"), Type: TypeWork},
	}
	if got := UsedDays(stays, d("30.06.2026")); got != 30 {
		t.Errorf("UsedDays = %d, want 30 (01.06–30.06)", got)
	}
	// An open stay overlapping a closed one, too.
	open := []Stay{
		{Entry: d("01.06.2026"), Exit: dp("20.06.2026"), Type: TypeWork},
		{Entry: d("15.06.2026"), Exit: nil, Type: TypeWork},
	}
	if got := UsedDays(open, d("30.06.2026")); got != 30 {
		t.Errorf("UsedDays with open overlap = %d, want 30", got)
	}
}

// A crossing just after midnight belongs to the new calendar date in
// Belgrade, whatever zone the timestamp was produced in. Times carrying a
// zone must be reduced to their own local date, not shifted to UTC first.
func TestMidnightCrossing_UsesLocalDate(t *testing.T) {
	bg, err := time.LoadLocation("Europe/Belgrade")
	if err != nil {
		t.Skip("no tzdata")
	}
	entry := time.Date(2026, 6, 2, 0, 15, 0, 0, bg) // 01.06 22:15 UTC
	exit := time.Date(2026, 6, 2, 23, 50, 0, 0, bg)
	stays := []Stay{{Entry: entry, Exit: &exit, Type: TypeWork}}
	if got := UsedDays(stays, d("02.06.2026")); got != 1 {
		t.Errorf("UsedDays = %d, want 1 (whole stay is 02.06 local)", got)
	}
	if got := UsedDays(stays, d("01.06.2026")); got != 0 {
		t.Errorf("01.06 = %d used, want 0 (entry was after midnight local)", got)
	}
}

// Private and work days count the same.
func TestPrivateDaysCountTowardLimit(t *testing.T) {
	today := d("01.07.2026")
	stays := []Stay{
		{Entry: addDays(today, -89), Exit: dayPtr(addDays(today, -50)), Type: TypeWork},
		{Entry: addDays(today, -49), Exit: dayPtr(today), Type: TypePrivate},
	}
	if got := UsedDays(stays, today); got != 90 {
		t.Errorf("UsedDays = %d, want 90", got)
	}
}

// After a full 90 days the driver must wait until days age out; a trip
// starting the day after the 90th day is impossible, and the first possible
// 1-day trip is when the first day of that stay leaves the window.
func TestAfterFull90_FirstFreeDayIs180DaysAfterFirstEntry(t *testing.T) {
	first := d("01.01.2026")
	last := addDays(first, 89)
	stays := []Stay{{Entry: first, Exit: dayPtr(last), Type: TypeWork}}

	if ok, _, _ := CanDoTrip(stays, addDays(last, 1), 1); ok {
		t.Error("a trip the day after 90 used days must be refused")
	}
	if got, want := EarliestStart(stays, 1, addDays(last, 1)), addDays(first, 180); !got.Equal(want) {
		t.Errorf("EarliestStart = %s, want %s", formatDate(got), formatDate(want))
	}
}

// Countries a Serbian driver may pass that are commonly misclassified:
// Romania and Bulgaria are full members since 01.01.2025, Cyprus and
// Ireland are EU but not Schengen. Monaco, San Marino and Vatican City have
// no border controls with Schengen neighbours and are de facto inside, so a
// location there must count.
func TestIsSchengen_CommonlyMisclassified(t *testing.T) {
	cases := map[string]bool{
		"RO": true, "BG": true, "HR": true,
		"CY": false, "IE": false, "GB": false,
		"MC": true, "SM": true, "VA": true,
	}
	for code, want := range cases {
		if got := IsSchengen(code); got != want {
			t.Errorf("IsSchengen(%q) = %v, want %v", code, got, want)
		}
	}
}
