// Package calc implements the Schengen 90/180 day rule, pure functions, no I/O.
//
// Dates are always local calendar dates in Europe/Belgrade (no time-of-day component).
// Entry and exit days both count as full days in Schengen: entering and leaving on the
// same calendar date counts as 1 day.
package calc

import "time"

const WindowDays = 180
const MaxDays = 90

// StayType distinguishes work from private travel; both count toward the 90/180 limit.
type StayType string

const (
	TypeWork    StayType = "work"
	TypePrivate StayType = "private"
)

// Stay is a single continuous period in the Schengen area for one driver.
// Exit is nil for a stay that is still open (driver currently in Schengen);
// callers evaluating "today" should treat an open stay as running through that date.
type Stay struct {
	Entry time.Time
	Exit  *time.Time
	Type  StayType
}

// normalize strips time-of-day, returning a date-only UTC midnight value so that
// day-difference arithmetic is exact regardless of the caller's original time zone.
func normalize(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func daysBetween(a, b time.Time) int {
	return int(normalize(b).Sub(normalize(a)).Hours() / 24)
}

func addDays(t time.Time, n int) time.Time {
	return normalize(t).AddDate(0, 0, n)
}

// effectiveExit returns the last day a stay occupies as of `today`: its exit date,
// or `today` itself if the stay is still open.
func effectiveExit(s Stay, today time.Time) time.Time {
	if s.Exit == nil {
		return normalize(today)
	}
	return normalize(*s.Exit)
}

// overlapDays counts how many days of stay s fall within [winStart, winEnd] (inclusive).
func overlapDays(s Stay, today, winStart, winEnd time.Time) int {
	entry := normalize(s.Entry)
	exit := effectiveExit(s, today)
	start := entry
	if winStart.After(start) {
		start = winStart
	}
	end := exit
	if winEnd.Before(end) {
		end = winEnd
	}
	if end.Before(start) {
		return 0
	}
	return daysBetween(start, end) + 1
}

// UsedDays returns the number of Schengen days counted in the 180-day window ending on d,
// i.e. the window [d-179, d] inclusive.
func UsedDays(stays []Stay, d time.Time) int {
	d = normalize(d)
	winStart := addDays(d, -(WindowDays - 1))
	total := 0
	for _, s := range stays {
		// A stay that starts after d cannot contribute (defensive; callers should not pass such data).
		if normalize(s.Entry).After(d) {
			continue
		}
		total += overlapDays(s, d, winStart, d)
	}
	return total
}

// Remaining returns how many Schengen days are still available as of today.
func Remaining(stays []Stay, today time.Time) int {
	r := MaxDays - UsedDays(stays, today)
	if r < 0 {
		return 0
	}
	return r
}

// isCurrentlyIn reports whether the driver has an open stay (no exit date) as of today.
func isCurrentlyIn(stays []Stay) (Stay, bool) {
	for _, s := range stays {
		if s.Exit == nil {
			return s, true
		}
	}
	return Stay{}, false
}

// MaxContinuousUntil returns the last date through which a driver currently in Schengen
// may remain, assuming the stay continues uninterrupted. As older days fall out of the
// trailing 180-day window during the stay, this date is normally later than
// today + Remaining(today) would naively suggest.
//
// Returns the zero time and false if the driver is not currently in Schengen.
func MaxContinuousUntil(stays []Stay, today time.Time) (time.Time, bool) {
	open, ok := isCurrentlyIn(stays)
	if !ok {
		return time.Time{}, false
	}
	today = normalize(today)

	// Already over the limit today: there is no legal day left, and the
	// forward walk below would wrongly report today itself as allowed. Report
	// instead the last day of this stay on which the driver was still within
	// the limit — a deadline that has already passed — so callers can say
	// "trebalo je da izađe do X". The result is then before today, which is
	// how callers recognise an overstay.
	if UsedDays(stays, today) > MaxDays {
		entry := normalize(open.Entry)
		for d := addDays(today, -1); !d.Before(entry); d = addDays(d, -1) {
			if UsedDays(stays, d) <= MaxDays {
				return d, true
			}
		}
		// Over the limit from the very first day of the stay: the driver
		// should not have entered at all.
		return addDays(entry, -1), true
	}

	// Walk forward day by day from today until UsedDays would exceed 90 on the next day.
	// This is O(days) but days is bounded (at most ~90-ish forward from today in practice);
	// a closed-form derivation is error-prone given arbitrary overlapping history, so we
	// favor a simple, obviously-correct simulation.
	last := today
	d := today
	for {
		next := addDays(d, 1)
		// Simulate the open stay extending through `next`.
		extended := make([]Stay, len(stays))
		copy(extended, stays)
		for i, s := range extended {
			if s.Entry.Equal(open.Entry) && s.Exit == nil {
				nextCopy := next
				extended[i] = Stay{Entry: s.Entry, Exit: &nextCopy, Type: s.Type}
			}
		}
		if UsedDays(extended, next) > MaxDays {
			break
		}
		last = next
		d = next
		if d.Sub(today).Hours()/24 > 3650 {
			// Safety valve: should never happen given MaxDays=90, but avoid infinite loop
			// on malformed input.
			break
		}
	}
	return last, true
}

// MaxTripLength returns the largest L such that a single uninterrupted trip of L days
// starting on `start` (with no other stays overlapping it) keeps UsedDays(d) <= 90 for
// every day d in [start, start+L-1].
func MaxTripLength(stays []Stay, start time.Time) int {
	start = normalize(start)
	length := 0
	d := start
	for {
		trial := append(cloneStays(stays), Stay{Entry: start, Exit: dayPtr(d), Type: TypeWork})
		if UsedDays(trial, d) > MaxDays {
			break
		}
		length++
		d = addDays(d, 1)
		if length > 3650 {
			break
		}
	}
	return length
}

// CanDoTrip checks whether a trip of `length` days starting on `start` is possible without
// ever exceeding 90 used days during the trip. It returns the peak UsedDays reached and,
// if it fails, the first date on which the limit is exceeded.
func CanDoTrip(stays []Stay, start time.Time, length int) (ok bool, peakUsed int, firstOverstay time.Time) {
	start = normalize(start)
	for i := 0; i < length; i++ {
		d := addDays(start, i)
		trial := append(cloneStays(stays), Stay{Entry: start, Exit: dayPtr(d), Type: TypeWork})
		used := UsedDays(trial, d)
		if used > peakUsed {
			peakUsed = used
		}
		if used > MaxDays && firstOverstay.IsZero() {
			firstOverstay = d
		}
	}
	ok = firstOverstay.IsZero()
	return ok, peakUsed, firstOverstay
}

// EarliestStart returns the earliest date on or after `from` on which a trip of `length`
// days could begin without ever exceeding the 90-day limit during the trip.
func EarliestStart(stays []Stay, length int, from time.Time) time.Time {
	from = normalize(from)
	d := from
	for i := 0; i < 3650; i++ {
		if ok, _, _ := CanDoTrip(stays, d, length); ok {
			return d
		}
		d = addDays(d, 1)
	}
	return time.Time{}
}

// FreeUpEvent describes a date on which previously-used days fall out of the trailing
// window, freeing up that many days of future allowance.
type FreeUpEvent struct {
	Date      time.Time
	DaysFreed int
}

// FreeUpSchedule returns, for each day in (today, today+horizon], how many days free up
// (i.e. how much Remaining increases) relative to the previous day, limited to increases
// only (days where the allowance grows because old stays exit the window).
//
// This answers "if the driver leaves today and takes no further trips, when does his
// allowance grow as old usage ages out of the 180-day window?" — so an open stay is
// treated as closing today, not as continuing (that forward-projection is what
// MaxContinuousUntil answers instead).
func FreeUpSchedule(stays []Stay, today time.Time, horizon int) []FreeUpEvent {
	today = normalize(today)
	closed := closeOpenAt(stays, today)
	var events []FreeUpEvent
	prevRemaining := Remaining(closed, today)
	for i := 1; i <= horizon; i++ {
		d := addDays(today, i)
		r := Remaining(closed, d)
		if r > prevRemaining {
			events = append(events, FreeUpEvent{Date: d, DaysFreed: r - prevRemaining})
		}
		prevRemaining = r
	}
	return events
}

// closeOpenAt returns a copy of stays with any open stay (Exit == nil) closed on `today`,
// so that future-dated evaluations don't implicitly keep extending it.
func closeOpenAt(stays []Stay, today time.Time) []Stay {
	out := make([]Stay, len(stays))
	for i, s := range stays {
		if s.Exit == nil {
			t := normalize(today)
			out[i] = Stay{Entry: s.Entry, Exit: &t, Type: s.Type}
			continue
		}
		out[i] = s
	}
	return out
}

// OverstayDay marks a historical date where UsedDays exceeded 90.
type OverstayDay struct {
	Date     time.Time
	UsedDays int
}

// Overstays scans every day from the first stay's entry through `today` and reports
// each day where UsedDays > 90.
func Overstays(stays []Stay, today time.Time) []OverstayDay {
	if len(stays) == 0 {
		return nil
	}
	today = normalize(today)
	earliest := normalize(stays[0].Entry)
	for _, s := range stays[1:] {
		if normalize(s.Entry).Before(earliest) {
			earliest = normalize(s.Entry)
		}
	}
	var out []OverstayDay
	for d := earliest; !d.After(today); d = addDays(d, 1) {
		u := UsedDays(stays, d)
		if u > MaxDays {
			out = append(out, OverstayDay{Date: d, UsedDays: u})
		}
	}
	return out
}

func cloneStays(stays []Stay) []Stay {
	out := make([]Stay, len(stays))
	copy(out, stays)
	return out
}

func dayPtr(t time.Time) *time.Time {
	tt := normalize(t)
	return &tt
}
