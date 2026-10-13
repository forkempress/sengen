// Command seed bootstraps a tenant and its first owner user. There is no
// public signup flow — a new customer is onboarded by running this once
// against their database after provisioning their subdomain.
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
	host := flag.String("host", "", "domen po kom se prepoznaje firma, npr. vozaci.firma.rs")
	email := flag.String("email", "", "email vlasnika za login")
	password := flag.String("password", "", "lozinka vlasnika za login")
	ownerName := flag.String("name", "", "ime vlasnika")
	flag.Parse()

	if *tenantName == "" || *host == "" || *email == "" || *password == "" || *ownerName == "" {
		log.Fatal("obavezni parametri: -tenant-name -host -email -password -name")
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

	t, err := st.GetTenantByHost(ctx, *host)
	if errors.Is(err, store.ErrNotFound) {
		t, err = st.CreateTenant(ctx, *tenantName, *host)
		if err != nil {
			log.Fatalf("create tenant: %v", err)
		}
		fmt.Printf("Kreiran tenant #%d (%s, %s)\n", t.ID, t.Name, t.Host)
	} else if err != nil {
		log.Fatalf("lookup tenant: %v", err)
	} else {
		fmt.Printf("Tenant već postoji: #%d (%s, %s)\n", t.ID, t.Name, t.Host)
	}

	if _, err := st.GetUserByEmail(ctx, t.ID, *email); err == nil {
		log.Fatalf("korisnik sa emailom %s već postoji za ovaj tenant", *email)
	} else if !errors.Is(err, store.ErrNotFound) {
		log.Fatalf("lookup user: %v", err)
	}

	hash, err := auth.HashPassword(*password)
	if err != nil {
		log.Fatalf("hash password: %v", err)
	}
	u, err := st.CreateUser(ctx, t.ID, *email, hash, "owner", *ownerName)
	if err != nil {
		log.Fatalf("create user: %v", err)
	}
	fmt.Printf("Kreiran owner korisnik #%d (%s) — možete se prijaviti na https://%s/login\n", u.ID, u.Email, t.Host)
}
