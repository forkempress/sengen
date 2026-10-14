// Command seed onboards a firm: it creates the firm (or reuses it, found by
// its slug) and an owner account. There is no public signup flow. Everyone
// signs in at the same address; the email decides the firm.
//
// It also sets a firm's billing (plan, trial end, price), which the
// superadmin bot reports on. With -slug and only billing flags it changes an
// existing firm without creating an owner:
//
//	make seed ARGS="-slug prevoz-djuric -plan paid -price-cents 200"
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"time"

	"shengen/internal/auth"
	"shengen/internal/config"
	"shengen/internal/db"
	"shengen/internal/store"
)

func main() {
	tenantName := flag.String("tenant-name", "", "ime firme, npr. 'Prevoz d.o.o.'")
	slug := flag.String("slug", "", "kratka oznaka firme (podrazumevano: izvedena iz imena)")
	email := flag.String("email", "", "email vlasnika za prijavu")
	password := flag.String("password", "", "lozinka vlasnika za prijavu")
	ownerName := flag.String("name", "", "ime vlasnika")
	plan := flag.String("plan", "", "plan firme: trial, paid ili paused")
	trialUntil := flag.String("trial-until", "", "poslednji dan triala, YYYY-MM-DD")
	priceCents := flag.Int("price-cents", -1, "mesečna cena po aktivnom vozaču, u evro centima (200 = €2)")
	flag.Parse()

	billingOnly := *email == "" && *password == "" && *ownerName == "" && *slug != "" &&
		(*plan != "" || *trialUntil != "" || *priceCents >= 0)
	if !billingOnly && (*tenantName == "" || *email == "" || *password == "" || *ownerName == "") {
		log.Fatal("obavezni parametri: -tenant-name -email -password -name (opciono: -slug, -plan, -trial-until, -price-cents)\n" +
			"ili samo naplata postojeće firme: -slug uz -plan / -trial-until / -price-cents")
	}
	if *slug == "" {
		*slug = store.SlugFromName(*tenantName)
	}
	if *slug == "" {
		log.Fatal("od imena firme ne može da se napravi oznaka; prosledite -slug")
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
	if billingOnly {
		if err != nil {
			log.Fatalf("firma %q: %v", *slug, err)
		}
		setBilling(ctx, st, t.ID, *plan, *trialUntil, *priceCents)
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		t, err = st.CreateTenant(ctx, *tenantName, *slug)
		if err != nil {
			log.Fatalf("kreiranje firme: %v", err)
		}
		fmt.Printf("Kreirana firma #%d %q (oznaka %s)\n", t.ID, t.Name, t.Slug)
	} else if err != nil {
		log.Fatalf("pretraga firme: %v", err)
	} else {
		fmt.Printf("Firma već postoji: #%d %q (oznaka %s)\n", t.ID, t.Name, t.Slug)
	}

	// Emails are unique across all firms: the address alone picks the firm.
	if existing, err := st.GetUserByEmail(ctx, *email); err == nil {
		log.Fatalf("email %s već koristi korisnik u firmi #%d", existing.Email, existing.TenantID)
	} else if !errors.Is(err, store.ErrNotFound) {
		log.Fatalf("pretraga korisnika: %v", err)
	}

	hash, err := auth.HashPassword(*password)
	if err != nil {
		log.Fatalf("hash lozinke: %v", err)
	}
	u, err := st.CreateUser(ctx, t.ID, *email, hash, "owner", *ownerName)
	if err != nil {
		log.Fatalf("kreiranje korisnika: %v", err)
	}

	where := "na /login"
	if cfg.PublicBaseURL != "" {
		where = "na " + cfg.PublicBaseURL + "/login"
	}
	fmt.Printf("Kreiran vlasnik #%d (%s) — prijava %s\n", u.ID, u.Email, where)

	if *plan != "" || *trialUntil != "" || *priceCents >= 0 {
		setBilling(ctx, st, t.ID, *plan, *trialUntil, *priceCents)
	}
}

// setBilling changes only the billing fields that were passed.
func setBilling(ctx context.Context, st *store.Store, tenantID int64, plan, trialUntil string, priceCents int) {
	cur, err := st.GetTenantOverview(ctx, tenantID)
	if err != nil {
		log.Fatalf("učitavanje firme: %v", err)
	}
	if plan == "" {
		plan = cur.Plan
	}
	trial := cur.TrialUntil
	if trialUntil != "" {
		d, err := time.Parse("2006-01-02", trialUntil)
		if err != nil {
			log.Fatalf("-trial-until mora biti u obliku YYYY-MM-DD: %v", err)
		}
		trial = &d
	}
	if priceCents < 0 {
		priceCents = cur.PricePerDriverCents
	}
	if err := st.SetTenantBilling(ctx, tenantID, plan, trial, priceCents); err != nil {
		log.Fatalf("naplata: %v", err)
	}
	until := "-"
	if trial != nil {
		until = trial.Format("02.01.2006")
	}
	fmt.Printf("Firma #%d: plan %s, trial do %s, %d centi po vozaču\n", tenantID, plan, until, priceCents)
}
