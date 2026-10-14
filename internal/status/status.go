// Package status computes each driver's 90/180 snapshot from their stays.
// It is the single place both the web dashboard and the Telegram bot derive
// a driver's numbers from, so the two can never disagree.
package status

import (
	"fmt"
	"sort"
	"time"

	"shengen/internal/calc"
	"shengen/internal/store"
)

// DriverStatus is the computed 90/180 snapshot for one driver as of a given
// day.
type DriverStatus struct {
	Driver     store.Driver
	UsedDays   int
	Remaining  int
	InSchengen bool

	CurrentSince       *time.Time // entry date of the open stay, if any
	MaxContinuousUntil *time.Time // only set when InSchengen
	MaxTripLengthToday int        // only meaningful when !InSchengen: "najduža tura sada"
	NextFreeUp         *calc.FreeUpEvent

	LastStaySource string
	LastStayEntry  *time.Time
	LastStayExit   *time.Time

	// OverBy is how many days past the 90-day limit the driver is (0 when
	// within it). When it is non-zero, MaxContinuousUntil holds the deadline
	// that has already passed, so it must not be shown as "može do".
	OverBy int

	BadgeClass string
	BadgeLabel string
}

func ToCalcStays(stays []store.Stay) []calc.Stay {
	out := make([]calc.Stay, len(stays))
	for i, st := range stays {
		out[i] = calc.Stay{Entry: st.Entry, Exit: st.Exit, Type: calc.StayType(st.Type)}
	}
	return out
}

func Compute(d store.Driver, stays []store.Stay, today time.Time) DriverStatus {
	cstays := ToCalcStays(stays)
	status := DriverStatus{
		Driver:    d,
		UsedDays:  calc.UsedDays(cstays, today),
		Remaining: calc.Remaining(cstays, today),
	}

	if status.UsedDays > calc.MaxDays {
		status.OverBy = status.UsedDays - calc.MaxDays
	}

	if until, ok := calc.MaxContinuousUntil(cstays, today); ok {
		status.InSchengen = true
		status.MaxContinuousUntil = &until
		for i := range stays {
			if stays[i].Exit == nil {
				status.CurrentSince = &stays[i].Entry
				break
			}
		}
	} else {
		status.MaxTripLengthToday = calc.MaxTripLength(cstays, today)
	}

	if events := calc.FreeUpSchedule(cstays, today, 400); len(events) > 0 {
		status.NextFreeUp = &events[0]
	}

	if len(stays) > 0 {
		last := stays[len(stays)-1]
		status.LastStaySource = last.Source
		status.LastStayEntry = &last.Entry
		status.LastStayExit = last.Exit
	}

	status.BadgeClass, status.BadgeLabel = BadgeFor(status.Remaining)
	return status
}

// Thresholds are the remaining-days cut-offs for the traffic-light badge.
// A driver with fewer than Red days left is red, up to and including Orange
// days is orange, and anything above is green. Zero days left is always
// "urgent", regardless of configuration.
type Thresholds struct {
	Red    int
	Orange int
}

var DefaultThresholds = Thresholds{Red: 15, Orange: 30}

// ThresholdsFor returns the tenant's configured cut-offs, falling back to
// the defaults for any value left unset.
func ThresholdsFor(s store.TenantSettings) Thresholds {
	t := DefaultThresholds
	if s.ThresholdRedDays > 0 {
		t.Red = s.ThresholdRedDays
	}
	if s.ThresholdOrangeDays > 0 {
		t.Orange = s.ThresholdOrangeDays
	}
	return t
}

func (t Thresholds) Badge(remaining int) (class, label string) {
	switch {
	case remaining <= 0:
		return "badge-urgent", "HITNO"
	case remaining < t.Red:
		return "badge-red", fmt.Sprintf("%d", remaining)
	case remaining <= t.Orange:
		return "badge-orange", fmt.Sprintf("%d", remaining)
	default:
		return "badge-green", fmt.Sprintf("%d", remaining)
	}
}

// BadgeFor uses the default thresholds. Callers that know the tenant should
// call ApplyThresholds on the computed status instead.
func BadgeFor(remaining int) (class, label string) {
	return DefaultThresholds.Badge(remaining)
}

// ApplyThresholds recomputes the badge using a tenant's own cut-offs.
func (ds *DriverStatus) ApplyThresholds(t Thresholds) {
	ds.BadgeClass, ds.BadgeLabel = t.Badge(ds.Remaining)
}

// AsOf returns the stays as they stood on `day`, so that Compute(…, day) can
// answer "how would this driver look on that date".
//
// For a day in the past, stays that began later are dropped, and a stay that
// was still running on `day` (it ended afterwards, or is still open) is
// reported as open — which is how Compute understands "in Schengen right
// now". For today or a future day the stays are returned unchanged: an open
// stay then simply keeps running up to that day, which is the safe
// assumption (it never hides days a driver might still accumulate).
//
// The input slice and its elements are not modified.
func AsOf(stays []store.Stay, day, today time.Time) []store.Stay {
	if !day.Before(today) {
		return stays
	}
	out := make([]store.Stay, 0, len(stays))
	for _, st := range stays {
		if st.Entry.After(day) {
			continue
		}
		if st.Exit != nil && st.Exit.After(day) {
			st.Exit = nil // st is a copy; the caller's stay keeps its exit date
		}
		out = append(out, st)
	}
	return out
}

// ComputeAll groups stays by driver and returns one DriverStatus per driver,
// sorted by Remaining ascending (highest risk first).
func ComputeAll(drivers []store.Driver, allStays []store.Stay, today time.Time) []DriverStatus {
	byDriver := make(map[int64][]store.Stay, len(drivers))
	for _, st := range allStays {
		byDriver[st.DriverID] = append(byDriver[st.DriverID], st)
	}
	out := make([]DriverStatus, 0, len(drivers))
	for _, d := range drivers {
		out = append(out, Compute(d, byDriver[d.ID], today))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Remaining < out[j].Remaining })
	return out
}
