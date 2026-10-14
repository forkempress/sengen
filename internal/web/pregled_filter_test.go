package web

import (
	"testing"
	"time"

	"shengen/internal/status"
	"shengen/internal/store"
)

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

// --- parseAsOf ---

func TestParseAsOf(t *testing.T) {
	today := day("2026-10-13")

	cases := []struct {
		name     string
		raw      string
		want     time.Time
		wantWarn bool
	}{
		{"empty means today", "", today, false},
		{"whitespace means today", "   ", today, false},
		{"today", "2026-10-13", today, false},
		{"past date", "2026-05-01", day("2026-05-01"), false},
		{"future date", "2026-11-15", day("2026-11-15"), false},
		{"far but allowed future", "2028-10-01", day("2028-10-01"), false},
		{"garbage falls back to today with a warning", "sutra", today, true},
		{"wrong format falls back with a warning", "15.11.2026", today, true},
		{"impossible date falls back with a warning", "2026-02-30", today, true},
		{"absurd future falls back with a warning", "9999-12-31", today, true},
		{"absurd past falls back with a warning", "0001-01-01", today, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, msg := parseAsOf(tc.raw, today)
			if !got.Equal(tc.want) {
				t.Errorf("day = %s, want %s", got.Format("2006-01-02"), tc.want.Format("2006-01-02"))
			}
			if (msg != "") != tc.wantWarn {
				t.Errorf("warning = %q, want warning: %v", msg, tc.wantWarn)
			}
		})
	}
}

// --- filtering ---

func ds(name, vehicle, badge string) status.DriverStatus {
	return status.DriverStatus{
		Driver:     store.Driver{Name: name, VehicleLabel: vehicle},
		BadgeClass: badge,
	}
}

func names(in []status.DriverStatus) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = s.Driver.Name
	}
	return out
}

func sameNames(t *testing.T, got []status.DriverStatus, want ...string) {
	t.Helper()
	g := names(got)
	if len(g) != len(want) {
		t.Fatalf("got %v, want %v", g, want)
	}
	for i := range g {
		if g[i] != want[i] {
			t.Fatalf("got %v, want %v", g, want)
		}
	}
}

var fleet = []status.DriverStatus{
	ds("Marko Marković", "BG-123-AB", "badge-urgent"),
	ds("Đorđe Ćosić", "NS-456-CD", "badge-red"),
	ds("Ana Anić", "KG-789-EF", "badge-orange"),
	ds("Petar Petrović", "BG-555-GH", "badge-green"),
}

func TestFilterStatuses_NoFilterReturnsEverythingInOrder(t *testing.T) {
	sameNames(t, filterStatuses(fleet, "", ""), "Marko Marković", "Đorđe Ćosić", "Ana Anić", "Petar Petrović")
}

func TestFilterStatuses_ByName(t *testing.T) {
	sameNames(t, filterStatuses(fleet, "mark", ""), "Marko Marković")
	sameNames(t, filterStatuses(fleet, "  ANA ", ""), "Ana Anić")
	sameNames(t, filterStatuses(fleet, "nepostojeci", ""))
}

func TestFilterStatuses_ByVehicle(t *testing.T) {
	sameNames(t, filterStatuses(fleet, "ns-456", ""), "Đorđe Ćosić")
	sameNames(t, filterStatuses(fleet, "bg-", ""), "Marko Marković", "Petar Petrović")
}

// Dispatchers search from phone keyboards without č/ć/š/ž/đ.
func TestFilterStatuses_IgnoresSerbianDiacritics(t *testing.T) {
	sameNames(t, filterStatuses(fleet, "Djordje", ""), "Đorđe Ćosić")
	sameNames(t, filterStatuses(fleet, "cosic", ""), "Đorđe Ćosić")
	sameNames(t, filterStatuses(fleet, "Ćosić", ""), "Đorđe Ćosić")
	sameNames(t, filterStatuses(fleet, "markovic", ""), "Marko Marković")
	sameNames(t, filterStatuses(fleet, "petrovic", ""), "Petar Petrović")
}

// "Show me the red ones" must include the urgent (0 days left) drivers — a
// filter that hid the worst case behind the wrong bucket would be dangerous.
func TestFilterStatuses_RedIncludesUrgent(t *testing.T) {
	sameNames(t, filterStatuses(fleet, "", "crveno"), "Marko Marković", "Đorđe Ćosić")
}

func TestFilterStatuses_YellowAndGreen(t *testing.T) {
	sameNames(t, filterStatuses(fleet, "", "zuto"), "Ana Anić")
	sameNames(t, filterStatuses(fleet, "", "zeleno"), "Petar Petrović")
}

func TestFilterStatuses_CombinesNameAndSeverity(t *testing.T) {
	sameNames(t, filterStatuses(fleet, "an", "zuto"), "Ana Anić")
	sameNames(t, filterStatuses(fleet, "ana", "zeleno")) // right name, wrong colour
}

// An unrecognised severity value (hand-edited URL) must be ignored, not turn
// into "match nothing", which would look like an empty fleet.
func TestFilterStatuses_UnknownSeverityIsIgnored(t *testing.T) {
	sameNames(t, filterStatuses(fleet, "", "bilosta"), "Marko Marković", "Đorđe Ćosić", "Ana Anić", "Petar Petrović")
	sameNames(t, filterStatuses(fleet, "ana", "bilosta"), "Ana Anić")
}

func TestFilterStatuses_DoesNotModifyInput(t *testing.T) {
	before := names(fleet)
	filterStatuses(fleet, "ana", "zuto")
	after := names(fleet)
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("input modified: %v -> %v", before, after)
		}
	}
}
