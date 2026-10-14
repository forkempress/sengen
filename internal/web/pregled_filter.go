package web

import (
	"strings"
	"time"

	"shengen/internal/status"
)

// Bounds for the "stanje na dan X" date picker. They only exist to keep a
// mistyped year (0001, 9999) from producing a meaningless — and slow —
// calculation.
const (
	maxAsOfFutureDays = 730
	maxAsOfPastDays   = 5 * 365
)

// parseAsOf turns the ?na_dan= query value into the day the dashboard should
// be computed for. An empty value means today. An unusable value also falls
// back to today, with a message to show the user — never a silent fallback,
// since a dispatcher who believes they are looking at 15.11. while seeing
// today's numbers would act on the wrong data.
func parseAsOf(raw string, today time.Time) (day time.Time, errMsg string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return today, ""
	}
	day, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return today, "Neispravan datum — prikazano je stanje za danas."
	}
	if day.After(today.AddDate(0, 0, maxAsOfFutureDays)) {
		return today, "Datum je predaleko u budućnosti — prikazano je stanje za danas."
	}
	if day.Before(today.AddDate(0, 0, -maxAsOfPastDays)) {
		return today, "Datum je predaleko u prošlosti — prikazano je stanje za danas."
	}
	return day, ""
}

// validSeverity reports whether s is a status key (?ozbiljnost=preko,
// kriticno, paznja or ok). Old colour values from bookmarks (crveno, zuto,
// zeleno) are not keys, so they show every driver rather than nothing.
func validSeverity(s string) bool {
	_, ok := status.LevelFromKey(s)
	return ok
}

// matchesSeverity compares the driver's status — the same one the badge
// shows — with the filter. Each status matches only itself, so the filter
// returns exactly the drivers its KPI card counts. Drivers over the limit
// are never hidden by that: they have their own card, listed first, and
// the HITNO banner above it.
func matchesSeverity(b status.Badge, severity string) bool {
	l, ok := status.LevelFromKey(severity)
	return !ok || b.Level == l
}

// serbianFold lower-cases and strips Serbian diacritics so that "Djordje"
// finds "Đorđe" and "Cosic" finds "Ćosić" — dispatchers search from phone
// keyboards without č/ć/š/ž/đ.
var serbianFold = strings.NewReplacer(
	"č", "c", "ć", "c", "š", "s", "ž", "z", "đ", "dj",
)

func fold(s string) string {
	return serbianFold.Replace(strings.ToLower(strings.TrimSpace(s)))
}

// filterStatuses keeps the drivers whose name or vehicle contains query and
// whose badge matches severity. An empty query or severity matches
// everything. Order is preserved (callers sort by risk beforehand).
func filterStatuses(in []status.DriverStatus, query, severity string) []status.DriverStatus {
	needle := fold(query)
	if needle == "" && !validSeverity(severity) {
		return in
	}
	out := make([]status.DriverStatus, 0, len(in))
	for _, ds := range in {
		if needle != "" &&
			!strings.Contains(fold(ds.Driver.Name), needle) &&
			!strings.Contains(fold(ds.Driver.VehicleLabel), needle) {
			continue
		}
		if !matchesSeverity(ds.Badge, severity) {
			continue
		}
		out = append(out, ds)
	}
	return out
}
