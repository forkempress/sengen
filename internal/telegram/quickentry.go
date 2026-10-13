package telegram

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// QuickRange is one parsed date range from the admin's quick-text entry,
// e.g. "22.04-29.04" or "08.07-14.07 p" (p = private).
type QuickRange struct {
	Entry   time.Time
	Exit    time.Time
	Private bool
}

var quickEntryNameRe = regexp.MustCompile(`^(.+?)\s+(\d{1,2}\.\d{1,2}.*)$`)
var quickRangeRe = regexp.MustCompile(`^(\d{1,2}\.\d{1,2}(?:\.\d{2,4})?)-(\d{1,2}\.\d{1,2}(?:\.\d{2,4})?)\s*(p)?$`)

// ParseQuickEntry parses a deterministic free-text line like
// "Marko 22.04-29.04, 06.05-15.05, 08.07-14.07 p" into a driver name and a
// list of date ranges. Trailing " p" on an individual range marks that
// range as private; every other range defaults to work. Dates without a
// year are resolved to the most recent year that doesn't land in the
// future, relative to today — per the "unesena godina ne sme biti u
// budućnosti" rule.
func ParseQuickEntry(text string, today time.Time) (driverName string, ranges []QuickRange, err error) {
	m := quickEntryNameRe.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil {
		return "", nil, fmt.Errorf("neprepoznat format — očekivano: Ime 22.04-29.04, 06.05-15.05 p")
	}
	driverName = strings.TrimSpace(m[1])
	rest := m[2]

	for i, segment := range strings.Split(rest, ",") {
		segment = strings.TrimSpace(segment)
		rm := quickRangeRe.FindStringSubmatch(segment)
		if rm == nil {
			return "", nil, fmt.Errorf("red %d: neispravan opseg datuma %q", i+1, segment)
		}
		entry, err := parseQuickDate(rm[1], today)
		if err != nil {
			return "", nil, fmt.Errorf("red %d: %w", i+1, err)
		}
		exit, err := parseQuickDate(rm[2], today)
		if err != nil {
			return "", nil, fmt.Errorf("red %d: %w", i+1, err)
		}
		if exit.Before(entry) {
			return "", nil, fmt.Errorf("red %d: izlaz je pre ulaska", i+1)
		}
		ranges = append(ranges, QuickRange{Entry: entry, Exit: exit, Private: rm[3] == "p"})
	}
	if len(ranges) == 0 {
		return "", nil, fmt.Errorf("nijedan opseg datuma nije prepoznat")
	}
	return driverName, ranges, nil
}

// parseQuickDate accepts dd.mm, dd.mm.yy, and dd.mm.yyyy. When the year is
// omitted, it picks the most recent year (starting from today's) for which
// the resulting date is not after today.
func parseQuickDate(s string, today time.Time) (time.Time, error) {
	parts := strings.Split(s, ".")
	day, err := strconv.Atoi(parts[0])
	if err != nil || day < 1 || day > 31 {
		return time.Time{}, fmt.Errorf("neispravan dan u %q", s)
	}
	month, err := strconv.Atoi(parts[1])
	if err != nil || month < 1 || month > 12 {
		return time.Time{}, fmt.Errorf("neispravan mesec u %q", s)
	}

	if len(parts) == 2 {
		year := today.Year()
		t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
		if t.After(today) {
			t = t.AddDate(-1, 0, 0)
		}
		return t, nil
	}

	yearStr := parts[2]
	year, err := strconv.Atoi(yearStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("neispravna godina u %q", s)
	}
	if len(yearStr) == 2 {
		year += 2000
	}
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC), nil
}

// resolveFutureDate parses dd.mm[.yyyy] for forward-looking contexts (the
// trip planner): when the year is omitted, it picks today's year unless
// that date has already passed, in which case it rolls forward to next
// year — the mirror image of parseQuickDate's backward-looking rule for
// logging past stays.
func resolveFutureDate(s string, today time.Time) (time.Time, error) {
	parts := strings.Split(s, ".")
	day, err := strconv.Atoi(parts[0])
	if err != nil || day < 1 || day > 31 {
		return time.Time{}, fmt.Errorf("neispravan dan u %q", s)
	}
	month, err := strconv.Atoi(parts[1])
	if err != nil || month < 1 || month > 12 {
		return time.Time{}, fmt.Errorf("neispravan mesec u %q", s)
	}
	if len(parts) >= 3 {
		year, err := strconv.Atoi(parts[2])
		if err != nil {
			return time.Time{}, fmt.Errorf("neispravna godina u %q", s)
		}
		if len(parts[2]) == 2 {
			year += 2000
		}
		return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC), nil
	}
	t := time.Date(today.Year(), time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if t.Before(today) {
		t = t.AddDate(1, 0, 0)
	}
	return t, nil
}
