// Command seed onboards a firm: it creates the firm (or reuses it, found by
// its slug) and an owner account. There is no public signup flow. Everyone
// signs in at the same address; the email decides the firm.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"

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
	flag.Parse()

	if *tenantName == "" || *email == "" || *password == "" || *ownerName == "" {
		log.Fatal("obavezni parametri: -tenant-name -email -password -name (opciono: -slug)")
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
}
