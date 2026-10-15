package status

import (
	"time"

	"shengen/internal/calc"
	"shengen/internal/store"
)

// The planner: the web quick check, the saved plans and the Telegram
// /planer all go through these two functions, so they always agree.

// PlansToCalc turns saved plans into calc stays.
func PlansToCalc(plans []store.PlannedStay) []calc.Stay {
	out := make([]calc.Stay, len(plans))
	for i, p := range plans {
		to := p.To
		out[i] = calc.Stay{Entry: p.From, Exit: &to, Type: calc.TypeWork}
	}
	return out
}

// CheckTrip evaluates a trip from `from` to `to` against the driver's stays
// and saved plans (see calc.CheckTrip). OverlapsPlans and BreaksPlan index
// into plans.
func CheckTrip(stays []store.Stay, plans []store.PlannedStay, today, from, to time.Time) (calc.TripCheck, error) {
	return calc.CheckTrip(ToCalcStays(stays), PlansToCalc(plans), today, from, to)
}

// PlanView is a saved plan with how it fares in the projection.
type PlanView struct {
	store.PlannedStay
	calc.PlanResult
	Length int
	// OverlapsOther: another plan still ahead shares at least one day.
	OverlapsOther bool
}

// Active reports whether the plan still counts (upcoming or in progress).
func (v PlanView) Active() bool { return v.Evaluated }

// StateLabel is the plan's state in words, for the web and the bot.
func (v PlanView) StateLabel() string {
	switch v.State {
	case calc.PlanInProgress:
		return "u toku"
	case calc.PlanRealized:
		return "realizovan"
	case calc.PlanMissed:
		return "nije realizovan"
	default:
		return "planiran"
	}
}

// StateKey names the state for CSS classes.
func (v PlanView) StateKey() string {
	switch v.State {
	case calc.PlanInProgress:
		return "progress"
	case calc.PlanRealized:
		return "realized"
	case calc.PlanMissed:
		return "missed"
	default:
		return "planned"
	}
}

// EvaluatePlans checks a driver's saved plans against his stays and each
// other. The result is in the order of plans.
func EvaluatePlans(stays []store.Stay, plans []store.PlannedStay, today time.Time) []PlanView {
	cplans := PlansToCalc(plans)
	results := calc.EvaluatePlans(ToCalcStays(stays), cplans, today)
	out := make([]PlanView, len(plans))
	for i, p := range plans {
		out[i] = PlanView{PlannedStay: p, PlanResult: results[i], Length: int(p.To.Sub(p.From).Hours()/24) + 1}
	}
	for i := range out {
		for j := range out {
			if i != j && out[i].Active() && out[j].Active() &&
				!out[i].From.After(out[j].To) && !out[j].From.After(out[i].To) {
				out[i].OverlapsOther = true
			}
		}
	}
	return out
}

// PlanSummary is the overview's one-line view of a driver's plans.
type PlanSummary struct {
	Ahead int // plans upcoming or in progress
	Next  *PlanView
	// Problem is the first plan still ahead that goes over 90/180.
	Problem *PlanView
}

// More is how many plans follow the next one.
func (s PlanSummary) More() int {
	if s.Ahead < 2 {
		return 0
	}
	return s.Ahead - 1
}

// Summarize picks the next plan and the first problem out of a driver's
// evaluated plans (sorted by start date).
func Summarize(views []PlanView) PlanSummary {
	var s PlanSummary
	for i := range views {
		v := &views[i]
		if !v.Active() {
			continue
		}
		s.Ahead++
		if s.Next == nil {
			s.Next = v
		}
		if s.Problem == nil && !v.OK {
			s.Problem = v
		}
	}
	return s
}

// SummarizeAll groups a firm's stays and plans by driver and summarizes
// each driver's plans. Drivers without plans are left out.
func SummarizeAll(allStays []store.Stay, allPlans []store.PlannedStay, today time.Time) map[int64]PlanSummary {
	plansBy := map[int64][]store.PlannedStay{}
	for _, p := range allPlans {
		plansBy[p.DriverID] = append(plansBy[p.DriverID], p)
	}
	staysBy := map[int64][]store.Stay{}
	for _, st := range allStays {
		if len(plansBy[st.DriverID]) > 0 {
			staysBy[st.DriverID] = append(staysBy[st.DriverID], st)
		}
	}
	out := make(map[int64]PlanSummary, len(plansBy))
	for driverID, plans := range plansBy {
		if s := Summarize(EvaluatePlans(staysBy[driverID], plans, today)); s.Ahead > 0 {
			out[driverID] = s
		}
	}
	return out
}

// PlanDates is a plan's dates for messages: "01.12.–20.12.2026".
func PlanDates(p store.PlannedStay) string {
	if p.From.Year() != p.To.Year() {
		return p.From.Format("02.01.2006") + "–" + p.To.Format("02.01.2006")
	}
	return p.From.Format("02.01.") + "–" + p.To.Format("02.01.2006")
}

// DaysWord is "dan" or "dana" for n days: 1, 21, 31 dan; 11, 5 dana.
func DaysWord(n int) string {
	if n < 0 {
		n = -n
	}
	if n%10 == 1 && n%100 != 11 {
		return "dan"
	}
	return "dana"
}
