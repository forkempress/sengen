package telegram

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"shengen/internal/calc"
	"shengen/internal/clock"
	"shengen/internal/status"
	"shengen/internal/store"
)

const planerUsage = "Upotreba:\n/planer <ime> <dd.mm>-<dd.mm> — može li na turu\n/planer <ime> <dd.mm> <broj_dana>\n/planer <ime> — sačuvani planovi"

// planerArgs splits "/planer" arguments into the driver's name and the trip.
// The name may have spaces, so the trip is read from the end: "dd.mm-dd.mm",
// "dd.mm dd.mm" or "dd.mm <days>". Without one, the admin asks for the
// driver's saved plans.
type planerArgs struct {
	name           string
	from, to, days string // as typed; days is set for the "<dd.mm> <days>" form
	hasTrip        bool
}

func parsePlanerArgs(args string) planerArgs {
	f := strings.Fields(args)
	n := len(f)
	looksLikeDate := func(s string) bool { return strings.Contains(s, ".") }
	switch {
	case n >= 2 && strings.Contains(f[n-1], "-"):
		from, to, _ := strings.Cut(f[n-1], "-")
		return planerArgs{name: strings.Join(f[:n-1], " "), from: from, to: to, hasTrip: true}
	case n >= 3 && looksLikeDate(f[n-2]) && looksLikeDate(f[n-1]):
		return planerArgs{name: strings.Join(f[:n-2], " "), from: f[n-2], to: f[n-1], hasTrip: true}
	case n >= 3 && looksLikeDate(f[n-2]):
		return planerArgs{name: strings.Join(f[:n-2], " "), from: f[n-2], days: f[n-1], hasTrip: true}
	}
	return planerArgs{name: strings.Join(f, " ")}
}

// resolveTrip turns the typed dates into a trip. A return date without a
// year is the first such date on or after the start.
func (a planerArgs) resolveTrip(today time.Time) (from, to time.Time, errMsg string) {
	from, err := resolveFutureDate(a.from, today)
	if err != nil {
		return from, to, "Neispravan datum: " + err.Error()
	}
	if a.days != "" {
		n, err := strconv.Atoi(a.days)
		if err != nil || n <= 0 {
			return from, to, "Neispravan broj dana."
		}
		if n > calc.MaxDays {
			return from, to, fmt.Sprintf("Tura ne može biti duža od %d dana.", calc.MaxDays)
		}
		return from, from.AddDate(0, 0, n-1), ""
	}
	if to, err = resolveFutureDate(a.to, from); err != nil {
		return from, to, "Neispravan datum povratka: " + err.Error()
	}
	return from, to, ""
}

func (b *Bot) cmdPlaner(ctx context.Context, client *Client, t *store.Tenant, chatID int64, args string) {
	a := parsePlanerArgs(args)
	if a.name == "" {
		client.SendMessage(ctx, chatID, planerUsage, nil)
		return
	}
	today := clock.Today()
	var from, to time.Time
	if a.hasTrip {
		var msg string
		if from, to, msg = a.resolveTrip(today); msg != "" {
			client.SendMessage(ctx, chatID, msg, nil)
			return
		}
	}

	driver, err := b.Store.FindDriverByName(ctx, t.ID, a.name)
	if err != nil {
		client.SendMessage(ctx, chatID, "Vozač nije pronađen: "+errMsg(err), nil)
		return
	}
	stays, err := b.Store.ListStaysByDriver(ctx, t.ID, driver.ID)
	if err != nil {
		client.SendMessage(ctx, chatID, "Greška pri učitavanju.", nil)
		return
	}
	plans, err := b.Store.ListPlannedStaysByDriver(ctx, t.ID, driver.ID)
	if err != nil {
		client.SendMessage(ctx, chatID, "Greška pri učitavanju.", nil)
		return
	}
	if !a.hasTrip {
		client.SendMessage(ctx, chatID, formatPlans(driver.Name, status.EvaluatePlans(stays, plans, today), today), nil)
		return
	}
	client.SendMessage(ctx, chatID, formatTripCheck(driver.Name, stays, plans, today, from, to), nil)
}

func formatTripCheck(name string, stays []store.Stay, plans []store.PlannedStay, today, from, to time.Time) string {
	c, err := status.CheckTrip(stays, plans, today, from, to)
	switch {
	case errors.Is(err, calc.ErrTripStartsWhileAway):
		msg := name + " je trenutno u Šengenu — nova tura može početi tek kad se vrati."
		if c.MaxContinuousUntil != nil {
			msg += fmt.Sprintf(" U ovom boravku sme da ostane do %s.", c.MaxContinuousUntil.Format("02.01.2006"))
		}
		return msg
	case errors.Is(err, calc.ErrTripInPast):
		return "Polazak ne može biti u prošlosti."
	case errors.Is(err, calc.ErrTripBadRange):
		return "Povratak ne može biti pre polaska."
	case errors.Is(err, calc.ErrTripTooLong):
		return fmt.Sprintf("Tura ne može biti duža od %d dana.", calc.MaxDays)
	case err != nil:
		return "Greška pri proračunu."
	}

	var b strings.Builder
	trip := fmt.Sprintf("%s–%s (%d %s)", from.Format("02.01.2006"), to.Format("02.01.2006"), c.Length, status.DaysWord(c.Length))
	if c.OK {
		fmt.Fprintf(&b, "%s MOŽE na turu %s.\n", name, trip)
		fmt.Fprintf(&b, "Na dan povratka iskorišćeno %d/90, posle ture ostaje %d %s.\n", c.UsedOnReturn, c.RemainingAfter, status.DaysWord(c.RemainingAfter))
	} else {
		fmt.Fprintf(&b, "%s NE MOŽE na turu %s — prekoračenje od %s.\n", name, trip, c.FirstOverstay.Format("02.01.2006"))
		if !c.EarliestStart.IsZero() {
			fmt.Fprintf(&b, "Najraniji polazak za %d %s: %s.\n", c.Length, status.DaysWord(c.Length), c.EarliestStart.Format("02.01.2006"))
		}
	}
	if c.MaxLength > 0 {
		fmt.Fprintf(&b, "Ako krene %s, sme da ostane najkasnije do %s (%d %s).", from.Format("02.01.2006"), c.LastAllowedDay.Format("02.01.2006"), c.MaxLength, status.DaysWord(c.MaxLength))
	} else {
		fmt.Fprintf(&b, "Sa polaskom %s nema nijedan slobodan dan.", from.Format("02.01.2006"))
	}
	for _, i := range c.OverlapsPlans {
		fmt.Fprintf(&b, "\n⚠️ Preklapa se sa sačuvanim planom %s.", status.PlanDates(plans[i]))
	}
	if c.BreaksPlan >= 0 {
		fmt.Fprintf(&b, "\n⚠️ Posle ove ture plan %s prelazi limit %s.", status.PlanDates(plans[c.BreaksPlan]), c.BreaksOn.Format("02.01.2006"))
	}
	if counted := activePlans(stays, plans, today); counted > 0 {
		fmt.Fprintf(&b, "\nRačunato sa sačuvanim planovima (%d).", counted)
	}
	if c.AssumedReturn != nil {
		if c.AssumedReturn.Equal(today) {
			b.WriteString("\nVozač je sada u Šengenu — računato je kao da se vraća danas. Svaki dan kasnijeg povratka skraćuje ovu turu.")
		} else {
			fmt.Fprintf(&b, "\nVozač je sada u Šengenu — računato je da se vraća %s, po planu. Svaki dan kasnijeg povratka skraćuje ovu turu.", c.AssumedReturn.Format("02.01.2006"))
		}
	}
	return b.String()
}

func activePlans(stays []store.Stay, plans []store.PlannedStay, today time.Time) int {
	n := 0
	for _, v := range status.EvaluatePlans(stays, plans, today) {
		if v.Active() {
			n++
		}
	}
	return n
}

// formatPlans lists a driver's plans still ahead and those that ended in the
// last 30 days, with whether each fits in 90/180.
func formatPlans(name string, views []status.PlanView, today time.Time) string {
	var b strings.Builder
	cutoff := today.AddDate(0, 0, -30)
	for _, v := range views {
		if v.To.Before(cutoff) {
			continue
		}
		fmt.Fprintf(&b, "\n%s (%d %s) — %s", status.PlanDates(v.PlannedStay), v.Length, status.DaysWord(v.Length), v.StateLabel())
		switch {
		case v.Active() && !v.OK:
			fmt.Fprintf(&b, ", ⚠️ prelazi limit %s", v.FirstOverstay.Format("02.01.2006"))
		case v.Active():
			fmt.Fprintf(&b, ", posle ostaje %d %s", v.RemainingAfter, status.DaysWord(v.RemainingAfter))
		}
		if v.Note != "" {
			b.WriteString(" · " + v.Note)
		}
	}
	if b.Len() == 0 {
		return name + " nema sačuvanih planova. Planovi se čuvaju na webu, na stranici Planer.\n\n" + planerUsage
	}
	return "Planovi — " + name + ":" + b.String()
}
