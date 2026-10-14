// Command demo fills an existing firm with realistic demo drivers and stays,
// for showing the app to prospective clients. Dates are relative to today, so
// the data always looks current: the overview shows every situation at once —
// red, orange and green drivers, some currently in Schengen, one over the
// limit and one with no history yet.
//
// Drivers that already exist (by name) are skipped, so running it twice adds
// nothing. Stays go through the same validation as in the app.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"time"

	"shengen/internal/calc"
	"shengen/internal/clock"
	"shengen/internal/config"
	"shengen/internal/db"
	"shengen/internal/store"
)

// profile describes a driver's travel rhythm, walked backwards from today:
// trips of tripDays, home gapDays between them. The latest trip is either
// still open (started openSince days ago) or ended endedAgo days ago.
type profile struct {
	name, vehicle     string
	tripDays, gapDays int
	open              bool
	openSince         int
	endedAgo          int
	privateEvery      int // every n-th trip is private; 0 = none
	telegram          bool
}

var profiles = []profile{
	{name: "Marko Jovanović", vehicle: "BG 1234-AB", tripDays: 10, gapDays: 12, open: true, openSince: 9, telegram: true},
	{name: "Nikola Petrović", vehicle: "BG 2271-KM", tripDays: 8, gapDays: 14, open: true, openSince: 4, telegram: true},
	{name: "Stefan Nikolić", vehicle: "NS 0412-TR", tripDays: 6, gapDays: 20, endedAgo: 5},
	{name: "Miloš Đorđević", vehicle: "BG 9930-ZX", tripDays: 11, gapDays: 10, open: true, openSince: 7, telegram: true},
	{name: "Dragan Ilić", vehicle: "ČA 118-PD", tripDays: 10, gapDays: 13, endedAgo: 2, telegram: true},
	{name: "Ivan Stojanović", vehicle: "KG 0577-LJ", tripDays: 5, gapDays: 35, endedAgo: 12, privateEvery: 2},
	{name: "Nemanja Pavlović", vehicle: "BG 4410-NN", tripDays: 7, gapDays: 18, open: true, openSince: 2, telegram: true},
	{name: "Aleksandar Marković", vehicle: "NI 2093-BB", tripDays: 9, gapDays: 15, endedAgo: 1, privateEvery: 4},
	{name: "Dejan Kostić", vehicle: "ČA 774-RS", tripDays: 6, gapDays: 17, open: true, openSince: 6},
	{name: "Vladimir Todorović", vehicle: "BG 6650-VT"}, // nov vozač, bez istorije
}

func main() {
	slug := flag.String("slug", "", "oznaka firme koju treba popuniti (npr. dani90-demo)")
	dry := flag.Bool("dry", false, "samo ispiši šta bi bilo upisano, bez upisa")
	flag.Parse()
	if *slug == "" {
		log.Fatal("obavezan parametar: -slug (oznaka firme)")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	conn, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer conn.Close()
	st := store.New(conn)
	ctx := context.Background()

	t, err := st.GetTenantBySlug(ctx, *slug)
	if err != nil {
		log.Fatalf("firma %q: %v", *slug, err)
	}
	today := clock.Today()
	fmt.Printf("Firma #%d %q, danas %s\n\n", t.ID, t.Name, today.Format("02.01.2006"))

	for _, p := range profiles {
		stays := p.stays(today)
		used := calc.UsedDays(stays, today)
		fmt.Printf("%-22s %-11s %2d boravaka  iskorišćeno %3d/90  preostalo %2d%s\n",
			p.name, p.vehicle, len(stays), used, calc.Remaining(stays, today), inMark(stays))

		if *dry {
			continue
		}
		if _, err := st.FindDriverByName(ctx, t.ID, p.name); err == nil {
			fmt.Println("    već postoji — preskačem")
			continue
		} else if !errors.Is(err, store.ErrNotFound) {
			log.Fatalf("pretraga vozača: %v", err)
		}
		d, err := st.CreateDriver(ctx, t.ID, p.name, p.vehicle)
		if err != nil {
			log.Fatalf("vozač %s: %v", p.name, err)
		}
		for i, s := range stays {
			source := "import"
			if p.telegram && i >= len(stays)-3 { // novije boravke je javio vozač
				source = "telegram_button"
			}
			if _, err := st.CreateStay(ctx, t.ID, d.ID, s.Entry, s.Exit, string(s.Type), source, "", nil); err != nil {
				log.Fatalf("boravak %s %s: %v", p.name, s.Entry.Format("02.01.2006"), err)
			}
		}
	}
	if *dry {
		fmt.Println("\n-dry: ništa nije upisano.")
	}
}

// stays builds the driver's history oldest-first, covering a bit more than
// the 180-day window so that days also free up in the coming weeks.
func (p profile) stays(today time.Time) []calc.Stay {
	if p.tripDays == 0 {
		return nil
	}
	day := func(ago int) time.Time { return today.AddDate(0, 0, -ago) }

	var rev []calc.Stay
	var end int // days ago of the current trip's last day
	if p.open {
		rev = append(rev, calc.Stay{Entry: day(p.openSince), Type: calc.TypeWork})
		end = p.openSince + p.gapDays + 1
	} else {
		end = p.endedAgo
	}
	for n := len(rev); end < 200; n++ {
		start := end + p.tripDays - 1
		typ := calc.TypeWork
		if p.privateEvery > 0 && n%p.privateEvery == p.privateEvery-1 {
			typ = calc.TypePrivate
		}
		exit := day(end)
		rev = append(rev, calc.Stay{Entry: day(start), Exit: &exit, Type: typ})
		end = start + p.gapDays + 1
	}

	out := make([]calc.Stay, len(rev))
	for i, s := range rev {
		out[len(rev)-1-i] = s
	}
	return out
}

func inMark(stays []calc.Stay) string {
	for _, s := range stays {
		if s.Exit == nil {
			return "  (u Šengenu)"
		}
	}
	return ""
}
