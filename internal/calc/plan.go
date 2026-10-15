package calc

import (
	"errors"
	"time"
)

// Planned trips. A plan is a Stay with both dates set that has not happened
// yet; it is kept apart from the real stays and only joins them in a
// projection, so a forgotten plan can never pass for a day actually spent in
// Schengen.

// Errors CheckTrip returns for a trip that cannot be evaluated at all.
var (
	ErrTripBadRange        = errors.New("povratak je pre polaska")
	ErrTripTooLong         = errors.New("tura je duža od 90 dana")
	ErrTripInPast          = errors.New("polazak je u prošlosti")
	ErrTripStartsWhileAway = errors.New("vozač je još u Šengenu")
)

// PlanState says where a saved plan stands against the real stays.
type PlanState int

const (
	// PlanUpcoming: nothing recorded in the plan's dates yet. Its days from
	// today on count in the projection.
	PlanUpcoming PlanState = iota
	// PlanInProgress: the driver entered Schengen within the plan and has
	// not come back. His open stay is assumed to last until the plan's end.
	PlanInProgress
	// PlanRealized: a finished stay falls within the plan. The real stay
	// has replaced it; the plan no longer counts.
	PlanRealized
	// PlanMissed: the plan's dates have passed with no stay recorded.
	PlanMissed
)

// StateOf compares a plan with the real stays as of today. An open stay
// counts as running through today.
func StateOf(plan Stay, stays []Stay, today time.Time) PlanState {
	today = normalize(today)
	from, to := normalize(plan.Entry), effectiveExit(plan, today)
	for _, s := range stays {
		if overlaps(normalize(s.Entry), effectiveExit(s, today), from, to) {
			if s.Exit == nil {
				return PlanInProgress
			}
			return PlanRealized
		}
	}
	if to.Before(today) {
		return PlanMissed
	}
	return PlanUpcoming
}

func overlaps(aFrom, aTo, bFrom, bTo time.Time) bool {
	return !aFrom.After(bTo) && !bFrom.After(aTo)
}

// Projection is the history future questions are answered against: the real
// stays plus the plans still ahead. A driver in Schengen now is assumed to
// return today, or on the last day of the plan he is on, if that is later;
// left open, his stay would run on through every later date and each
// planned day would be counted twice. Upcoming plans that began before today
// without a recorded entry count from today on.
func Projection(stays, plans []Stay, today time.Time) []Stay {
	today = normalize(today)
	var ahead []Stay
	for _, p := range plans {
		if StateOf(p, stays, today) != PlanUpcoming {
			continue
		}
		from := normalize(p.Entry)
		if from.Before(today) {
			from = today
		}
		ahead = append(ahead, Stay{Entry: from, Exit: dayPtr(*p.Exit), Type: TypeWork})
	}
	return append(CloseOpenAt(stays, assumedReturn(stays, plans, today)), ahead...)
}

// assumedReturn is the day a driver in Schengen now is taken to come back:
// today, or the last day of the plan he is on if that is later.
func assumedReturn(stays, plans []Stay, today time.Time) time.Time {
	back := today
	for _, p := range plans {
		if StateOf(p, stays, today) == PlanInProgress && normalize(*p.Exit).After(back) {
			back = normalize(*p.Exit)
		}
	}
	return back
}

// PlanResult is how one saved plan fares in the projection.
type PlanResult struct {
	State PlanState
	// Evaluated is false for realized and missed plans, which no longer
	// count; the figures below are then zero.
	Evaluated      bool
	OK             bool
	PeakUsed       int
	FirstOverstay  time.Time // set when !OK
	UsedOnReturn   int       // days used on the plan's last day
	RemainingAfter int       // days left after it
}

// EvaluatePlans checks every plan against the real stays and all the other
// plans, so a trip that only fits on its own but not after the one before it
// is caught. Results are in the order of plans.
func EvaluatePlans(stays, plans []Stay, today time.Time) []PlanResult {
	today = normalize(today)
	proj := Projection(stays, plans, today)
	out := make([]PlanResult, len(plans))
	for i, p := range plans {
		r := PlanResult{State: StateOf(p, stays, today)}
		if r.State == PlanUpcoming || r.State == PlanInProgress {
			r.Evaluated = true
			from := normalize(p.Entry)
			if from.Before(today) {
				from = today
			}
			r.PeakUsed, r.FirstOverstay = peakOver(proj, from, normalize(*p.Exit))
			r.OK = r.FirstOverstay.IsZero()
			r.UsedOnReturn = UsedDays(proj, *p.Exit)
			r.RemainingAfter = remainingOf(r.UsedOnReturn)
		}
		out[i] = r
	}
	return out
}

// peakOver walks [from, to] and returns the highest count of used days and
// the first day over the limit (zero if none).
func peakOver(stays []Stay, from, to time.Time) (peak int, firstOver time.Time) {
	for d := from; !d.After(to); d = addDays(d, 1) {
		u := UsedDays(stays, d)
		if u > peak {
			peak = u
		}
		if u > MaxDays && firstOver.IsZero() {
			firstOver = d
		}
	}
	return peak, firstOver
}

func remainingOf(used int) int {
	if used >= MaxDays {
		return 0
	}
	return MaxDays - used
}

// TripCheck answers "can the driver be in Schengen from From to To", for the
// quick check and the Telegram /planer alike.
type TripCheck struct {
	From, To time.Time
	Length   int

	OK             bool
	PeakUsed       int
	FirstOverstay  time.Time // when !OK: first day over the limit
	UsedOnReturn   int       // days used on To
	RemainingAfter int       // days left after the trip

	MaxLength      int       // longest trip that could start on From
	LastAllowedDay time.Time // From + MaxLength - 1; zero when MaxLength is 0
	EarliestStart  time.Time // when !OK: first day a trip this long fits; zero if none within ~10 years

	// AssumedReturn is set when the driver is in Schengen now: the
	// projection takes him back on this day (today, or the end of the plan
	// he is on). Every day he stays longer shortens this trip.
	AssumedReturn *time.Time

	// OverlapsPlans lists the indexes of plans whose dates overlap the trip.
	OverlapsPlans []int
	// BreaksPlan is the index of a later plan this trip would push over the
	// limit, or -1. BreaksOn is that plan's first day over.
	BreaksPlan int
	BreaksOn   time.Time

	// MaxContinuousUntil is set with ErrTripStartsWhileAway: how long the
	// driver's current stay may last instead.
	MaxContinuousUntil *time.Time
}

// CheckTrip evaluates a trip from `from` to `to` (both days in Schengen)
// against the real stays and the saved plans. A trip must not start in the
// past, nor today or earlier while the driver is still in Schengen; the
// returned error then says why, and for ErrTripStartsWhileAway the check
// carries MaxContinuousUntil.
func CheckTrip(stays, plans []Stay, today, from, to time.Time) (TripCheck, error) {
	today, from, to = normalize(today), normalize(from), normalize(to)
	c := TripCheck{From: from, To: to, Length: daysBetween(from, to) + 1, BreaksPlan: -1}
	switch {
	case to.Before(from):
		return c, ErrTripBadRange
	case c.Length > MaxDays:
		return c, ErrTripTooLong
	case from.Before(today):
		return c, ErrTripInPast
	}
	if _, away := isCurrentlyIn(stays); away {
		if !from.After(today) {
			if until, ok := MaxContinuousUntil(stays, today); ok && !until.Before(today) {
				c.MaxContinuousUntil = &until
			}
			return c, ErrTripStartsWhileAway
		}
	}

	proj := Projection(stays, plans, today)
	if _, away := isCurrentlyIn(stays); away {
		back := assumedReturn(stays, plans, today)
		c.AssumedReturn = &back
	}

	trip := Stay{Entry: from, Exit: dayPtr(to), Type: TypeWork}
	with := append(cloneStays(proj), trip)
	c.PeakUsed, c.FirstOverstay = peakOver(with, from, to)
	c.OK = c.FirstOverstay.IsZero()
	c.UsedOnReturn = UsedDays(with, to)
	c.RemainingAfter = remainingOf(c.UsedOnReturn)

	c.MaxLength = MaxTripLength(proj, from)
	if c.MaxLength > 0 {
		c.LastAllowedDay = addDays(from, c.MaxLength-1)
	}
	if !c.OK {
		c.EarliestStart = EarliestStart(proj, c.Length, from)
	}

	before := EvaluatePlans(stays, plans, today)
	after := EvaluatePlans(stays, append(cloneStays(plans), trip), today)
	for i, p := range plans {
		if before[i].Evaluated && overlaps(normalize(p.Entry), normalize(*p.Exit), from, to) {
			c.OverlapsPlans = append(c.OverlapsPlans, i)
		}
		if c.BreaksPlan < 0 && before[i].OK && after[i].Evaluated && !after[i].OK && normalize(p.Entry).After(to) {
			c.BreaksPlan, c.BreaksOn = i, after[i].FirstOverstay
		}
	}
	return c, nil
}
