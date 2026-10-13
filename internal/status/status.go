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

func BadgeFor(remaining int) (class, label string) {
	switch {
	case remaining <= 0:
		return "badge-urgent", "HITNO"
	case remaining < 15:
		return "badge-red", fmt.Sprintf("%d", remaining)
	case remaining <= 30:
		return "badge-orange", fmt.Sprintf("%d", remaining)
	default:
		return "badge-green", fmt.Sprintf("%d", remaining)
	}
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
