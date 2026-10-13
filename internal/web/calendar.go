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
	Class string // "day-work" | "day-private" | "day-projection" | "day-outside" | ""
	Today bool
	Freed bool
}

type MonthGrid struct {
	Label string
	Weeks [][]DayCell
}

var srMonthNames = [...]string{
	"Januar", "Februar", "Mart", "April", "Maj", "Jun",
	"Jul", "Avgust", "Septembar", "Oktobar", "Novembar", "Decembar",
}

// buildMonthGrids renders monthsBefore..monthsAfter calendar months around
// today (inclusive), coloring each day according to the driver's stays:
//   - work/private stays within the current 180-day window: solid colors
//   - stays (or parts of stays) before the window: muted grey ("van prozora")
//   - days after today that belong to the still-open stay, projected through
//     MaxContinuousUntil: hatched yellow
//   - days on which FreeUpSchedule reports allowance growing: green border
func buildMonthGrids(stays []store.Stay, today time.Time, monthsBefore, monthsAfter int) []MonthGrid {
	cstays := status.ToCalcStays(stays)
	windowStart := today.AddDate(0, 0, -(calc.WindowDays - 1))

	var projectionEnd time.Time
	hasOpenStay := false
	for _, st := range stays {
		if st.Exit == nil {
			hasOpenStay = true
			break
		}
	}
	if hasOpenStay {
		if until, ok := calc.MaxContinuousUntil(cstays, today); ok {
			projectionEnd = until
		}
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

	freed := make(map[string]bool)
	for _, ev := range calc.FreeUpSchedule(cstays, today, 400) {
		freed[ev.Date.Format(dateKeyLayout)] = true
	}

	startMonth := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -monthsBefore, 0)

	grids := make([]MonthGrid, 0, monthsBefore+monthsAfter+1)
	for m := 0; m <= monthsBefore+monthsAfter; m++ {
		monthStart := startMonth.AddDate(0, m, 0)
		grids = append(grids, buildOneMonth(monthStart, today, byDate, freed, hasOpenStay, projectionEnd, windowStart))
	}
	return grids
}

const dateKeyLayout = "2006-01-02"

type dayInfo struct {
	typ string
}

func buildOneMonth(monthStart, today time.Time, byDate map[string]dayInfo, freed map[string]bool, hasOpenStay bool, projectionEnd, windowStart time.Time) MonthGrid {
	label := srMonthNames[monthStart.Month()-1] + " " + strconv.Itoa(monthStart.Year())

	firstWeekday := int(monthStart.Weekday()) // Sunday=0 ... Saturday=6
	leadingBlanks := (firstWeekday + 6) % 7   // Monday-first offset

	daysInMonth := monthStart.AddDate(0, 1, -1).Day()

	var cells []DayCell
	for i := 0; i < leadingBlanks; i++ {
		cells = append(cells, DayCell{})
	}
	for day := 1; day <= daysInMonth; day++ {
		d := time.Date(monthStart.Year(), monthStart.Month(), day, 0, 0, 0, 0, time.UTC)
		key := d.Format(dateKeyLayout)
		cell := DayCell{Day: day, Date: d, Today: d.Equal(today)}

		if info, ok := byDate[key]; ok {
			switch {
			case d.Before(windowStart):
				cell.Class = "day-outside"
			case info.typ == "private":
				cell.Class = "day-private"
			default:
				cell.Class = "day-work"
			}
		} else if hasOpenStay && d.After(today) && !projectionEnd.IsZero() && !d.After(projectionEnd) {
			cell.Class = "day-projection"
		}
		if freed[key] {
			cell.Freed = true
		}
		cells = append(cells, cell)
	}
	for len(cells)%7 != 0 {
		cells = append(cells, DayCell{})
	}

	var weeks [][]DayCell
	for i := 0; i < len(cells); i += 7 {
		weeks = append(weeks, cells[i:i+7])
	}
	return MonthGrid{Label: label, Weeks: weeks}
}
