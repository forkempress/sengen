package status

import (
	"time"

	"shengen/internal/calc"
	"shengen/internal/store"
)

// AvailCell is one cell of the fleet schedule: what a driver could do if a
// trip started on Start.
type AvailCell struct {
	Start time.Time
	// Busy: on Start the driver is already in Schengen, by his current stay
	// (projected to end today or with the plan he is on) or a saved plan.
	Busy bool
	// MaxLength is the longest trip starting on Start, in days, and no
	// longer than the gap before his next saved plan. Zero when Busy.
	MaxLength int
	// UntilPlan: MaxLength was cut short by the next saved plan.
	UntilPlan bool
	Level     Level
}

// Availability answers, for each start day, how long a trip the driver could
// take from that day on, counting his real stays and the plans still ahead.
// A day from which he cannot leave at all reads as a zero-day cell.
func Availability(stays []store.Stay, plans []store.PlannedStay, today time.Time, starts []time.Time, t Thresholds) []AvailCell {
	return AvailabilityIn(calc.Projection(ToCalcStays(stays), PlansToCalc(plans), today), starts, t)
}

// AvailabilityIn is Availability against a projection the caller has
// already built (every stay in it closed).
func AvailabilityIn(proj []calc.Stay, starts []time.Time, t Thresholds) []AvailCell {
	cells := make([]AvailCell, len(starts))
	for i, start := range starts {
		c := AvailCell{Start: start}
		var next time.Time
		for _, s := range proj {
			if s.Exit == nil {
				continue // the projection closes every stay; be safe anyway
			}
			if !start.Before(s.Entry) && !start.After(*s.Exit) {
				c.Busy = true
				break
			}
			if s.Entry.After(start) && (next.IsZero() || s.Entry.Before(next)) {
				next = s.Entry
			}
		}
		if !c.Busy {
			c.MaxLength = calc.MaxTripLength(proj, start)
			if !next.IsZero() {
				if gap := int(next.Sub(start).Hours()/24 + 0.5); gap < c.MaxLength {
					c.MaxLength, c.UntilPlan = gap, true
				}
			}
			c.Level = t.Level(c.MaxLength)
		}
		cells[i] = c
	}
	return cells
}

// ScheduleStarts is the schedule's columns: today, then the next weeks'
// Mondays.
func ScheduleStarts(today time.Time, weeks int) []time.Time {
	starts := []time.Time{today}
	monday := today.AddDate(0, 0, (8-int(today.Weekday()))%7)
	if monday.Equal(today) {
		monday = monday.AddDate(0, 0, 7)
	}
	for i := 0; i < weeks; i++ {
		starts = append(starts, monday.AddDate(0, 0, 7*i))
	}
	return starts
}
