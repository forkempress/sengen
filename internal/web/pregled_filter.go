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

// Severity filter values as they appear in the URL.
const (
	severityRed    = "crveno"
	severityYellow = "zuto"
	severityGreen  = "zeleno"
)

func validSeverity(s string) bool {
	return s == severityRed || s == severityYellow || s == severityGreen
}

// matchesSeverity maps the badge colour — the single source of truth for
// how bad a driver looks — onto the three filter choices. Urgent (0 days
// left) counts as red: it is the worst case, and a "show me the red ones"
// filter that hid it would be dangerous.
func matchesSeverity(badgeClass, severity string) bool {
	switch severity {
	case severityRed:
		return badgeClass == "badge-urgent" || badgeClass == "badge-red"
	case severityYellow:
		return badgeClass == "badge-orange"
	case severityGreen:
		return badgeClass == "badge-green"
	default:
		return true
	}
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
		if !matchesSeverity(ds.BadgeClass, severity) {
			continue
		}
		out = append(out, ds)
	}
	return out
}
