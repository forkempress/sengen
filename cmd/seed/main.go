// Command seed onboards a firm: it creates the firm (or reuses it, found by
// its slug) and an owner account. There is no public signup flow. Everyone
// signs in at the same address; the email decides the firm.
//
// It also sets a firm's billing (plan, trial end, price), which the
// superadmin bot reports on. With -slug and only billing flags it changes an
// existing firm without creating an owner:
//
//	make seed ARGS="-slug prevoz-djuric -plan paid -price 3,50 -currency EUR"
//
// Without -price and -currency a firm pays 300 RSD per active driver.
//
// -doc-model picks the model that reads the firm's scans (opus, sonnet,
// haiku or a model id); "default" goes back to the server's:
//
//	make seed ARGS="-slug prevoz-djuric -doc-model haiku"
//
// The owner's password is never a command-line argument (it would stay in
// the shell history and show in ps): it is typed at the prompt without echo,
// piped on stdin, or taken from SEED_PASSWORD.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"shengen/internal/auth"
	"shengen/internal/config"
	"shengen/internal/db"
	"shengen/internal/docread"
	"shengen/internal/store"
)

func main() {
	tenantName := flag.String("tenant-name", "", "ime firme, npr. 'Prevoz d.o.o.'")
	slug := flag.String("slug", "", "kratka oznaka firme (podrazumevano: izvedena iz imena)")
	email := flag.String("email", "", "email vlasnika za prijavu")
	passwordArg := flag.String("password", "", "ne koristi se: lozinka se kuca na upit, šalje na stdin ili u SEED_PASSWORD")
	ownerName := flag.String("name", "", "ime vlasnika")
	plan := flag.String("plan", "", "plan firme: trial, paid ili paused")
	trialUntil := flag.String("trial-until", "", "poslednji dan triala, YYYY-MM-DD")
	price := flag.String("price", "", "mesečna cena po aktivnom vozaču, npr. 300 ili 3,50 (podrazumevano 300)")
	currency := flag.String("currency", "", "valuta cene: RSD ili EUR (podrazumevano RSD)")
	docModel := flag.String("doc-model", "", "model za čitanje skenova firme: opus, sonnet, haiku ili default")
	flag.Parse()

	if *passwordArg != "" {
		log.Fatal("-password više nije podržan: lozinka bi ostala u istoriji shell-a. " +
			"Pokrenite bez njega i ukucajte lozinku na upit, ili je prosledite kroz SEED_PASSWORD.")
	}
	billing := *plan != "" || *trialUntil != "" || *price != "" || *currency != ""
	billingOnly := *email == "" && *ownerName == "" && *slug != "" && (billing || *docModel != "")
	if !billingOnly && (*tenantName == "" || *email == "" || *ownerName == "") {
		log.Fatal("obavezni parametri: -tenant-name -email -name (opciono: -slug, -plan, -trial-until, -price, -currency, -doc-model)\n" +
			"ili samo podešavanje postojeće firme: -slug uz -plan / -trial-until / -price / -currency / -doc-model")
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
		if billing {
			setBilling(ctx, st, t.ID, *plan, *trialUntil, *price, *currency)
		}
		if *docModel != "" {
			setDocModel(ctx, st, t, *docModel)
		}
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

	password, err := readPassword(os.Getenv("SEED_PASSWORD"), os.Stdin, os.Stderr)
	if err != nil {
		log.Fatal(err)
	}
	hash, err := auth.HashPassword(password)
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

	if billing {
		setBilling(ctx, st, t.ID, *plan, *trialUntil, *price, *currency)
	}
	if *docModel != "" {
		setDocModel(ctx, st, t, *docModel)
	}
}

// setDocModel sets the model that reads the firm's scans; "default" clears
// it, so the server's DOC_READ_MODEL applies.
func setDocModel(ctx context.Context, st *store.Store, t *store.Tenant, name string) {
	settings, err := t.ParsedSettings()
	if err != nil {
		log.Fatal(err)
	}
	if strings.EqualFold(strings.TrimSpace(name), "default") {
		settings.DocReadModel = ""
	} else {
		model, ok := docread.ResolveModel(name)
		if !ok {
			log.Fatalf("-doc-model: nepoznat model %q (opus, sonnet, haiku ili default)", name)
		}
		settings.DocReadModel = model
	}
	if err := st.UpdateTenantSettings(ctx, t.ID, settings); err != nil {
		log.Fatal(err)
	}
	if settings.DocReadModel == "" {
		fmt.Printf("Firma #%d: skenove čita podrazumevani model servera\n", t.ID)
	} else {
		fmt.Printf("Firma #%d: skenove čita %s\n", t.ID, settings.DocReadModel)
	}
}

// setBilling changes only the billing fields that were passed.
func setBilling(ctx context.Context, st *store.Store, tenantID int64, plan, trialUntil, price, currency string) {
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
	// A new currency without a new price would turn 300 din into €300.
	if currency != "" && price == "" && strings.ToUpper(currency) != cur.PriceCurrency {
		log.Fatal("uz -currency navedite i -price")
	}
	if currency == "" {
		currency = cur.PriceCurrency
	}
	currency = strings.ToUpper(currency)
	minor := cur.PricePerDriver
	if price != "" {
		var err error
		if minor, err = parsePrice(price); err != nil {
			log.Fatalf("-price: %v", err)
		}
	}
	if err := st.SetTenantBilling(ctx, tenantID, plan, trial, minor, currency); err != nil {
		log.Fatalf("naplata: %v", err)
	}
	until := "-"
	if trial != nil {
		until = trial.Format("02.01.2006")
	}
	fmt.Printf("Firma #%d: plan %s, trial do %s, %d,%02d %s po vozaču\n", tenantID, plan, until, minor/100, minor%100, currency)
}

// parsePrice reads "300", "3,50" or "3.50" into the minor unit (para, cent).
func parsePrice(s string) (int, error) {
	whole, frac, _ := strings.Cut(strings.ReplaceAll(strings.TrimSpace(s), ",", "."), ".")
	if len(frac) > 2 {
		return 0, fmt.Errorf("najviše dve decimale: %q", s)
	}
	frac += strings.Repeat("0", 2-len(frac))
	w, err1 := strconv.Atoi(whole)
	f, err2 := strconv.Atoi(frac)
	if whole == "" || err1 != nil || err2 != nil || w < 0 || f < 0 {
		return 0, fmt.Errorf("neispravan iznos %q (npr. 300 ili 3,50)", s)
	}
	return w*100 + f, nil
}

// readPassword takes the owner's password from SEED_PASSWORD if set, else
// from the terminal without echo (typed twice), else as the first line of a
// piped stdin.
func readPassword(env string, in *os.File, prompt io.Writer) (string, error) {
	var password string
	switch {
	case env != "":
		password = env
	case term.IsTerminal(int(in.Fd())):
		fmt.Fprint(prompt, "Lozinka vlasnika: ")
		first, err := term.ReadPassword(int(in.Fd()))
		fmt.Fprintln(prompt)
		if err != nil {
			return "", fmt.Errorf("čitanje lozinke: %w", err)
		}
		fmt.Fprint(prompt, "Ponovite lozinku: ")
		second, err := term.ReadPassword(int(in.Fd()))
		fmt.Fprintln(prompt)
		if err != nil {
			return "", fmt.Errorf("čitanje lozinke: %w", err)
		}
		if string(first) != string(second) {
			return "", errors.New("lozinke se ne poklapaju")
		}
		password = string(first)
	default:
		line, err := bufio.NewReader(in).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("čitanje lozinke sa stdin-a: %w", err)
		}
		password = strings.TrimRight(line, "\r\n")
	}
	// The same rules as on the web.
	if err := auth.CheckNewPassword(password); err != nil {
		return "", err
	}
	return password, nil
}
