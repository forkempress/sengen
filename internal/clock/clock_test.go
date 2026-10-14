package clock

import (
	"testing"
	"time"
)

func at(t *testing.T, s string) {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(Set(func() time.Time { return v }))
}

func TestToday_BelgradeDateAtMidnightAndDST(t *testing.T) {
	for _, tc := range []struct{ now, today, latest string }{
		{"2026-10-14T21:30:00Z", "2026-10-14", "2026-10-15"}, // 23:30 Belgrade, 00:30 Sofia
		{"2026-10-14T22:30:00Z", "2026-10-15", "2026-10-15"}, // 00:30 Belgrade
		{"2026-03-28T23:30:00Z", "2026-03-29", "2026-03-29"}, // 00:30 CET, night of the DST switch
		{"2026-10-24T23:30:00Z", "2026-10-25", "2026-10-25"}, // 01:30 CEST, night of the switch back
	} {
		at(t, tc.now)
		if got := Today().Format("2006-01-02"); got != tc.today {
			t.Errorf("Today() at %s = %s, want %s", tc.now, got, tc.today)
		}
		if got := LatestToday().Format("2006-01-02"); got != tc.latest {
			t.Errorf("LatestToday() at %s = %s, want %s", tc.now, got, tc.latest)
		}
	}
}

func TestNearMidnight(t *testing.T) {
	for _, tc := range []struct {
		now  string
		want bool
	}{
		{"2026-10-14T20:59:00Z", false}, // 22:59 Belgrade
		{"2026-10-14T21:00:00Z", true},  // 23:00
		{"2026-10-14T23:29:00Z", true},  // 01:29
		{"2026-10-14T23:30:00Z", false}, // 01:30
		{"2026-10-14T10:00:00Z", false},
	} {
		at(t, tc.now)
		if got := NearMidnight(); got != tc.want {
			t.Errorf("NearMidnight() at %s = %v, want %v", tc.now, got, tc.want)
		}
	}
}
