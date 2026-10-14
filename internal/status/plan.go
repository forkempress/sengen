package status

import (
	"time"

	"shengen/internal/calc"
	"shengen/internal/store"
)

// TripPlan answers "can this driver go on a trip of Length days starting on
// Start". Shared by the Telegram /planer command and the driver page, so the
// two always give the same answer.
type TripPlan struct {
	Start  time.Time
	Length int

	// InSchengen: the driver is away right now. The plan then assumes he
	// returns today — left open, his current stay would run on through the
	// planned trip and every day of it would be counted twice. Callers must
	// say so, because every day he comes back later shortens the trip.
	InSchengen bool

	// StartsWhileAway: the trip would start today or earlier while he is
	// still in Schengen, so there is nothing to plan; MaxContinuousUntil
	// says how long the current stay may last instead.
	StartsWhileAway    bool
	MaxContinuousUntil *time.Time

	OK            bool
	PeakUsed      int
	FirstOverstay time.Time // set when !OK
	EarliestStart time.Time // when !OK: first day this trip could start; zero if none within ~10 years
	MaxLength     int       // longest trip that could start on Start
}

// PlanTrip evaluates a trip. Length must be between 1 and calc.MaxDays.
func PlanTrip(stays []store.Stay, today, start time.Time, length int) TripPlan {
	cstays := ToCalcStays(stays)
	p := TripPlan{Start: start, Length: length}

	for _, s := range cstays {
		if s.Exit == nil {
			p.InSchengen = true
		}
	}
	if p.InSchengen {
		if !start.After(today) {
			p.StartsWhileAway = true
			if until, ok := calc.MaxContinuousUntil(cstays, today); ok && !until.Before(today) {
				p.MaxContinuousUntil = &until
			}
			return p
		}
		cstays = calc.CloseOpenAt(cstays, today)
	}

	p.OK, p.PeakUsed, p.FirstOverstay = calc.CanDoTrip(cstays, start, length)
	if !p.OK {
		p.EarliestStart = calc.EarliestStart(cstays, length, start)
	}
	p.MaxLength = calc.MaxTripLength(cstays, start)
	return p
}
