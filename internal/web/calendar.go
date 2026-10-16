package web

import (
	"strconv"
	"time"

	"shengen/internal/calc"
	"shengen/internal/status"
	"shengen/internal/store"
)

type DayCell struct {
	Day   int // 0 for a padding cell outside the month
	Date  time.Time
	Class string // "day-work" | "day-private" | "day-projection" | "day-planned" | "day-planned-over" | "day-outside" | ""
	Today bool
	Freed bool
	// Title explains a planned day: days used on it, counting the plans.
	Title string

	// The driver's count on this day, for the number in the cell and the
	// panel of the selected day. Days from today on are counted against
	// the projection (stays, the current stay up to its deadline, plans);
	// earlier days against the real stays.
	Future     bool // today or later: the cell shows a number
	Used       int
	Remaining  int // 90 − Used, never below 0
	OverBy     int
	Level      string    // status key of Remaining
	WindowFrom time.Time // first day of the 180-day window ending on Date

	// The longest trip starting on this day (Future days only).
	Trip          int
	TripBusy      bool // already in Schengen on this day: stay, projection or plan
	TripUntilPlan bool // Trip is cut short by the next saved plan
	TripLevel     string
	// TripBetter: fewer days are left than the firm's usual trip, but that
	// trip still fits, because old days drop out of the window while he is
	// away (the ⁺ mark).
	TripBetter bool
}

// Label is the cell read out by screen readers and shown on hover.
func (c DayCell) Label() string {
	s := c.Date.Format("02.01.2006")
	switch {
	case !c.Future:
		return s + " — iskorišćeno " + strconv.Itoa(c.Used) + "/90"
	case c.OverBy > 0:
		return s + " — " + strconv.Itoa(c.OverBy) + " " + status.DaysWord(c.OverBy) + " preko limita"
	}
	s += " — preostalo " + strconv.Itoa(c.Remaining) + " " + status.DaysWord(c.Remaining)
	if !c.TripBusy {
		s += ", može na turu " + strconv.Itoa(c.Trip) + " " + status.DaysWord(c.Trip)
	}
	return s
}

// TripTo is the last day of the longest trip starting on this day.
func (c DayCell) TripTo() time.Time { return c.Date.AddDate(0, 0, c.Trip-1) }

type MonthGrid struct {
	Label string
	Weeks [][]DayCell
	// Has says which day classes appear in the month, so the legend lists
	// only those; Plus and Freed likewise for the ⁺ mark and freed days.
	Has         map[string]bool
	Plus, Freed bool
}

// shortTrip is the shortest trip still colored Pažnja: two thirds of the
// usual one, rounded up.
func shortTrip(typical int) int { return (2*typical + 2) / 3 }

// tripLevel colors a trip by the firm's usual trip (typical days): it fits
// (OK), only a trip two thirds as long fits (Pažnja), something shorter fits
// (Kritično), or he cannot leave at all (Preko limita).
func tripLevel(trip, typical int) status.Level {
	switch {
	case trip <= 0:
		return status.LevelOver
	case trip >= typical:
		return status.LevelOK
	case trip >= shortTrip(typical):
		return status.LevelWarn
	default:
		return status.LevelCritical
	}
}

var srMonthNames = [...]string{
	"Januar", "Februar", "Mart", "April", "Maj", "Jun",
	"Jul", "Avgust", "Septembar", "Oktobar", "Novembar", "Decembar",
}

func monthLabel(month time.Time) string {
	return srMonthNames[month.Month()-1] + " " + strconv.Itoa(month.Year())
}

// buildMonth renders one calendar month, coloring each day according to the
// driver's stays:
//   - work/private stays within the current 180-day window: solid colors
//   - stays (or parts of stays) before the window: muted grey ("van prozora")
//   - days after today that belong to the still-open stay, projected through
//     its deadline (MaxContinuousUntil): hatched yellow
//   - days of saved plans still ahead: hatched light blue, or hatched red
//     on a day the projection (stays plus plans) goes over 90
//   - days on which the allowance grows as old days drop out: green border
//
// and giving every day from today on its remaining days and longest trip.
func buildMonth(stays []store.Stay, plans []status.PlanView, ds status.DriverStatus, th status.Thresholds, typical int, today, month time.Time) MonthGrid {
	cstays := status.ToCalcStays(stays)
	windowStart := today.AddDate(0, 0, -(calc.WindowDays - 1))

	var projectionEnd time.Time
	if ds.InSchengen && ds.MaxContinuousUntil != nil {
		projectionEnd = *ds.MaxContinuousUntil
	}

	byDate := make(map[string]dayInfo, len(stays)*5)
	for _, st := range stays {
		end := today
		if st.Exit != nil {
			end = *st.Exit
		}
		for d := st.Entry; !d.After(end); d = d.AddDate(0, 0, 1) {
			byDate[d.Format(dateKeyLayout)] = dayInfo{typ: st.Type}
		}
	}

	var active []store.PlannedStay
	for _, v := range plans {
		if v.Active() {
			active = append(active, v.PlannedStay)
		}
	}
	proj := projectAhead(cstays, active, today, projectionEnd)

	// Planned days, from today on: what is already recorded stays as it is.
	for _, p := range active {
		for d := p.From; !d.After(p.To); d = d.AddDate(0, 0, 1) {
			key := d.Format(dateKeyLayout)
			if d.Before(today) {
				continue
			}
			if _, real := byDate[key]; real && !d.After(today) {
				continue
			}
			byDate[key] = dayInfo{planned: true, used: calc.UsedDays(proj, d)}
		}
	}

	monthStart := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	daysInMonth := monthStart.AddDate(0, 1, -1).Day()

	// The longest trip from each day still ahead, in one pass.
	var starts []time.Time
	for day := 1; day <= daysInMonth; day++ {
		if d := monthStart.AddDate(0, 0, day-1); !d.Before(today) {
			starts = append(starts, d)
		}
	}
	avail := status.AvailabilityIn(proj, starts, th)
	// starts[0] falls on day firstAhead of the month. prevUsed is the days
	// used on the day before, once that day is today or later.
	firstAhead := daysInMonth - len(starts) + 1
	prevUsed := -1
	if len(starts) > 0 && starts[0].After(today) {
		prevUsed = calc.UsedDays(proj, starts[0].AddDate(0, 0, -1))
	}

	grid := MonthGrid{Label: monthLabel(monthStart), Has: map[string]bool{}}
	leadingBlanks := (int(monthStart.Weekday()) + 6) % 7 // Monday-first offset

	var cells []DayCell
	for i := 0; i < leadingBlanks; i++ {
		cells = append(cells, DayCell{})
	}
	for day := 1; day <= daysInMonth; day++ {
		d := monthStart.AddDate(0, 0, day-1)
		key := d.Format(dateKeyLayout)
		cell := DayCell{Day: day, Date: d, Today: d.Equal(today), Future: !d.Before(today),
			WindowFrom: d.AddDate(0, 0, -(calc.WindowDays - 1))}

		if info, ok := byDate[key]; ok {
			switch {
			case info.planned && info.used > calc.MaxDays:
				cell.Class = "day-planned-over"
				cell.Title = "Plan: " + strconv.Itoa(info.used) + "/90 — preko limita"
			case info.planned:
				cell.Class = "day-planned"
				cell.Title = "Plan: " + strconv.Itoa(info.used) + "/90"
			case d.Before(windowStart):
				cell.Class = "day-outside"
			case info.typ == "private":
				cell.Class = "day-private"
			default:
				cell.Class = "day-work"
			}
		} else if d.After(today) && !projectionEnd.IsZero() && !d.After(projectionEnd) {
			cell.Class = "day-projection"
		}

		if cell.Future {
			cell.Used = calc.UsedDays(proj, d)
			a := avail[day-firstAhead]
			cell.TripBusy, cell.TripUntilPlan = a.Busy, a.UntilPlan
			if !a.Busy {
				cell.Trip, cell.TripLevel = a.MaxLength, tripLevel(a.MaxLength, typical).Key()
			}
			cell.Freed = prevUsed >= 0 && cell.Used < prevUsed
			prevUsed = cell.Used
		} else {
			cell.Used = calc.UsedDays(cstays, d)
		}
		cell.Remaining = max(calc.MaxDays-cell.Used, 0)
		cell.OverBy = max(cell.Used-calc.MaxDays, 0)
		cell.Level = th.Level(cell.Remaining).Key()
		cell.TripBetter = cell.Future && !cell.TripBusy && cell.OverBy == 0 &&
			cell.Remaining < typical && cell.Trip >= typical

		grid.Has[cell.Class] = true
		grid.Plus = grid.Plus || cell.TripBetter
		grid.Freed = grid.Freed || cell.Freed
		cells = append(cells, cell)
	}
	for len(cells)%7 != 0 {
		cells = append(cells, DayCell{})
	}

	for i := 0; i < len(cells); i += 7 {
		grid.Weeks = append(grid.Weeks, cells[i:i+7])
	}
	return grid
}

// projectAhead is the history the days ahead are counted against: the real
// stays plus the plans still ahead (calc.Projection). A driver in Schengen
// now is taken to stay until his deadline, so the days of the projected stay
// count down to it, unless a plan he is on already keeps him there longer.
func projectAhead(cstays []calc.Stay, plans []store.PlannedStay, today, deadline time.Time) []calc.Stay {
	proj := calc.Projection(cstays, status.PlansToCalc(plans), today)
	if !deadline.After(today) {
		return proj
	}
	// Projection keeps the real stays first, in their order.
	for i, s := range cstays {
		if s.Exit == nil && proj[i].Exit != nil && proj[i].Exit.Before(deadline) {
			until := deadline
			proj[i].Exit = &until
		}
	}
	return proj
}

// cellOn finds a day's cell in a month grid.
func (g MonthGrid) cellOn(d time.Time) (DayCell, bool) {
	for _, w := range g.Weeks {
		for _, c := range w {
			if c.Day != 0 && c.Date.Equal(d) {
				return c, true
			}
		}
	}
	return DayCell{}, false
}

const dateKeyLayout = "2006-01-02"

type dayInfo struct {
	typ     string
	planned bool
	used    int // planned days: days used on that day, counting the plans
}
