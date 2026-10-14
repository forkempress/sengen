package status

import (
	"testing"
	"time"

	"shengen/internal/store"
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

func stay(entry string, exit *time.Time) store.Stay {
	return store.Stay{DriverID: 1, Entry: d(entry), Exit: exit, Type: "work", Source: "manual"}
}

// markoStays is the golden fixture from the spec (driver Marko, today = 13.10.2026).
func markoStays() []store.Stay {
	return []store.Stay{
		stay("22.04.2026", dp("29.04.2026")),
		stay("06.05.2026", dp("15.05.2026")),
		stay("26.05.2026", dp("02.06.2026")),
		stay("15.06.2026", dp("24.06.2026")),
		{DriverID: 1, Entry: d("08.07.2026"), Exit: dp("14.07.2026"), Type: "private", Source: "manual"},
		stay("12.08.2026", dp("18.08.2026")),
		stay("27.08.2026", dp("03.09.2026")),
		stay("14.09.2026", dp("21.09.2026")),
		stay("29.09.2026", dp("04.10.2026")),
		stay("10.10.2026", nil),
	}
}

var marko = store.Driver{ID: 1, Name: "Marko"}

// --- Thresholds ---

func TestThresholds_DefaultBoundaries(t *testing.T) {
	cases := []struct {
		remaining int
		wantClass string
	}{
		{-3, "badge-urgent"}, // never produced by calc, but must not fall through to green
		{0, "badge-urgent"},
		{1, "badge-red"},
		{14, "badge-red"},
		{15, "badge-orange"}, // spec: "15–30" is orange, "ispod 15" is red
		{30, "badge-orange"},
		{31, "badge-green"}, // spec: "preko 30" is green
		{90, "badge-green"},
	}
	for _, tc := range cases {
		if got, _ := DefaultThresholds.Badge(tc.remaining); got != tc.wantClass {
			t.Errorf("Badge(%d) = %s, want %s", tc.remaining, got, tc.wantClass)
		}
	}
}

func TestThresholds_CustomBoundaries(t *testing.T) {
	th := Thresholds{Red: 10, Orange: 20}
	cases := map[int]string{
		0: "badge-urgent", 9: "badge-red", 10: "badge-orange",
		20: "badge-orange", 21: "badge-green",
	}
	for remaining, want := range cases {
		if got, _ := th.Badge(remaining); got != want {
			t.Errorf("custom Badge(%d) = %s, want %s", remaining, got, want)
		}
	}
}

func TestThresholds_LabelShowsDaysOrUrgent(t *testing.T) {
	if _, label := DefaultThresholds.Badge(0); label != "HITNO" {
		t.Errorf("label at 0 days = %q, want HITNO", label)
	}
	if _, label := DefaultThresholds.Badge(14); label != "14" {
		t.Errorf("label at 14 days = %q, want 14", label)
	}
}

func TestThresholdsFor_FallsBackToDefaults(t *testing.T) {
	if got := ThresholdsFor(store.TenantSettings{}); got != DefaultThresholds {
		t.Errorf("empty settings = %+v, want defaults %+v", got, DefaultThresholds)
	}
	got := ThresholdsFor(store.TenantSettings{ThresholdRedDays: 7})
	if got.Red != 7 || got.Orange != DefaultThresholds.Orange {
		t.Errorf("only red set = %+v, want Red=7 Orange=%d", got, DefaultThresholds.Orange)
	}
	got = ThresholdsFor(store.TenantSettings{ThresholdRedDays: 5, ThresholdOrangeDays: 12})
	if got != (Thresholds{Red: 5, Orange: 12}) {
		t.Errorf("both set = %+v, want {5 12}", got)
	}
	if got := ThresholdsFor(store.TenantSettings{ThresholdRedDays: -4}); got.Red != DefaultThresholds.Red {
		t.Errorf("negative threshold = %d, want default %d", got.Red, DefaultThresholds.Red)
	}
}

func TestApplyThresholds_ChangesBadgeNotNumbers(t *testing.T) {
	ds := Compute(marko, markoStays(), d("13.10.2026"))
	if ds.Remaining != 14 || ds.BadgeClass != "badge-red" {
		t.Fatalf("setup: golden Marko = %d remaining / %s, want 14 / badge-red", ds.Remaining, ds.BadgeClass)
	}

	ds.ApplyThresholds(Thresholds{Red: 10, Orange: 20})
	if ds.BadgeClass != "badge-orange" {
		t.Errorf("14 days with red<10 = %s, want badge-orange", ds.BadgeClass)
	}
	if ds.Remaining != 14 || ds.UsedDays != 76 {
		t.Errorf("thresholds must not alter the counted days: remaining=%d used=%d", ds.Remaining, ds.UsedDays)
	}
}

// --- AsOf ---

func TestAsOf_TodayAndFutureReturnStaysUnchanged(t *testing.T) {
	stays := markoStays()
	today := d("13.10.2026")

	for _, day := range []string{"13.10.2026", "14.11.2026", "01.01.2027"} {
		got := AsOf(stays, d(day), today)
		if len(got) != len(stays) {
			t.Fatalf("AsOf(%s) changed the number of stays: %d -> %d", day, len(stays), len(got))
		}
		for i := range got {
			if got[i].Entry != stays[i].Entry || got[i].Exit != stays[i].Exit {
				t.Errorf("AsOf(%s) altered stay %d", day, i)
			}
		}
	}
}

func TestAsOf_PastDropsLaterStaysAndOpensTheRunningOne(t *testing.T) {
	today := d("13.10.2026")
	stays := []store.Stay{
		stay("01.10.2026", dp("10.10.2026")),
		stay("11.10.2026", nil),
	}

	// 05.10: first stay is running (ends 10.10, later than the day), second hasn't begun.
	got := AsOf(stays, d("05.10.2026"), today)
	if len(got) != 1 {
		t.Fatalf("AsOf(05.10) kept %d stays, want 1", len(got))
	}
	if got[0].Exit != nil {
		t.Errorf("stay running on 05.10 should be open as of that day, got exit %v", got[0].Exit)
	}

	// 10.10: the stay's own exit day — closed, in the past, not reopened.
	got = AsOf(stays, d("10.10.2026"), today)
	if len(got) != 1 || got[0].Exit == nil || !got[0].Exit.Equal(d("10.10.2026")) {
		t.Errorf("AsOf(10.10) should keep the stay closed on its exit day, got %+v", got)
	}

	// 11.10: second stay begins, still open.
	got = AsOf(stays, d("11.10.2026"), today)
	if len(got) != 2 || got[1].Exit != nil {
		t.Errorf("AsOf(11.10) = %+v, want both stays with the second open", got)
	}

	// Before everything.
	if got := AsOf(stays, d("30.09.2026"), today); len(got) != 0 {
		t.Errorf("AsOf(30.09) kept %d stays, want 0", len(got))
	}
}

func TestAsOf_DoesNotMutateInput(t *testing.T) {
	stays := []store.Stay{stay("01.10.2026", dp("10.10.2026"))}
	original := *stays[0].Exit

	AsOf(stays, d("05.10.2026"), d("13.10.2026"))

	if stays[0].Exit == nil || !stays[0].Exit.Equal(original) {
		t.Errorf("AsOf modified the caller's stay: exit is now %v, was %v", stays[0].Exit, original)
	}
}

// --- state as of a date, end to end ---

func TestComputeAsOf_SimpleStayAcrossDays(t *testing.T) {
	today := d("13.10.2026")
	stays := []store.Stay{stay("01.10.2026", dp("10.10.2026"))}

	cases := []struct {
		day         string
		wantUsed    int
		wantInside  bool
		wantSinceOK bool
	}{
		{"30.09.2026", 0, false, false},
		{"01.10.2026", 1, true, true}, // entry day counts as a full day
		{"05.10.2026", 5, true, true},
		{"10.10.2026", 10, false, false}, // exit day counts, but the driver has left
		{"13.10.2026", 10, false, false},
	}
	for _, tc := range cases {
		day := d(tc.day)
		ds := Compute(marko, AsOf(stays, day, today), day)
		if ds.UsedDays != tc.wantUsed {
			t.Errorf("as of %s: UsedDays = %d, want %d", tc.day, ds.UsedDays, tc.wantUsed)
		}
		if ds.InSchengen != tc.wantInside {
			t.Errorf("as of %s: InSchengen = %v, want %v", tc.day, ds.InSchengen, tc.wantInside)
		}
		if (ds.CurrentSince != nil) != tc.wantSinceOK {
			t.Errorf("as of %s: CurrentSince set = %v, want %v", tc.day, ds.CurrentSince != nil, tc.wantSinceOK)
		}
	}
}

// Worked out by hand, independent of the calculator: on 03.10.2026 the
// window is [07.04, 03.10] and holds 8+10+8+10+7+7+8+8 full days plus
// 29.09–03.10 (5 days) of the stay that was still running = 71 used.
func TestComputeAsOf_GoldenMarkoInThePast(t *testing.T) {
	today := d("13.10.2026")
	day := d("03.10.2026")

	ds := Compute(marko, AsOf(markoStays(), day, today), day)

	if ds.UsedDays != 71 {
		t.Errorf("UsedDays as of 03.10 = %d, want 71", ds.UsedDays)
	}
	if ds.Remaining != 19 {
		t.Errorf("Remaining as of 03.10 = %d, want 19", ds.Remaining)
	}
	if !ds.InSchengen {
		t.Error("Marko was inside Schengen on 03.10 (29.09–04.10) but InSchengen is false")
	}
	if ds.CurrentSince == nil || !ds.CurrentSince.Equal(d("29.09.2026")) {
		t.Errorf("CurrentSince as of 03.10 = %v, want 29.09.2026", ds.CurrentSince)
	}
}

// The identical stays at today must still produce the numbers the rest of the
// app (and the spec) treat as golden.
func TestComputeAsOf_TodayMatchesGolden(t *testing.T) {
	today := d("13.10.2026")
	ds := Compute(marko, AsOf(markoStays(), today, today), today)
	if ds.UsedDays != 76 || ds.Remaining != 14 {
		t.Errorf("golden today = %d used / %d remaining, want 76 / 14", ds.UsedDays, ds.Remaining)
	}
}

// A future date with no planned trips keeps the open stay running, so the
// driver is shown accumulating days — the cautious direction.
func TestComputeAsOf_FutureKeepsOpenStayRunning(t *testing.T) {
	today := d("13.10.2026")
	day := d("20.10.2026")

	ds := Compute(marko, AsOf(markoStays(), day, today), day)

	if !ds.InSchengen {
		t.Error("open stay should still be open on a future date")
	}
	if ds.UsedDays <= 76 {
		t.Errorf("UsedDays on 20.10 = %d, want more than today's 76 (7 more days in Schengen, minus any that left the window)", ds.UsedDays)
	}
}

// --- overstay ---

func TestCompute_OverBy(t *testing.T) {
	today := d("13.10.2026")

	within := Compute(marko, markoStays(), today)
	if within.OverBy != 0 {
		t.Errorf("golden Marko today: OverBy = %d, want 0", within.OverBy)
	}

	// One day past the spec's 14.11 deadline.
	day := d("15.11.2026")
	over := Compute(marko, AsOf(markoStays(), day, today), day)
	if over.OverBy != 1 {
		t.Errorf("Marko on 15.11: OverBy = %d, want 1 (91/90)", over.OverBy)
	}
	if over.Remaining != 0 {
		t.Errorf("Marko on 15.11: Remaining = %d, want 0", over.Remaining)
	}
	if over.MaxContinuousUntil == nil || !over.MaxContinuousUntil.Equal(d("14.11.2026")) {
		t.Errorf("Marko on 15.11: deadline = %v, want 14.11.2026 (already passed)", over.MaxContinuousUntil)
	}
}
