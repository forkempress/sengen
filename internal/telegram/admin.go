package telegram

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"shengen/internal/calc"
	"shengen/internal/clock"
	"shengen/internal/idgen"
	"shengen/internal/importer"
	"shengen/internal/status"
	"shengen/internal/store"
)

func bytesReader(data []byte) *bytes.Reader { return bytes.NewReader(data) }

func (b *Bot) handleAdminMessage(ctx context.Context, client *Client, t *store.Tenant, settings store.TenantSettings, chatID int64, user *store.User, msg *Message) {
	// Every admin entry point re-checks the role: being linked to a Telegram
	// chat proves identity, not authority.
	if !IsAdminRole(user.Role) {
		client.SendMessage(ctx, chatID, noAdminAccessMsg, nil)
		return
	}
	if msg.Document != nil {
		b.handleAdminDocument(ctx, client, t, chatID, user, msg.Document)
		return
	}
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}
	if isHelpRequest(text) {
		client.SendMessage(ctx, chatID, adminHelpText, nil)
		return
	}
	if strings.HasPrefix(text, "/") {
		b.handleAdminCommand(ctx, client, t, chatID, user, text)
		return
	}
	b.handleQuickEntry(ctx, client, t, chatID, user, text)
}

func (b *Bot) handleAdminCommand(ctx context.Context, client *Client, t *store.Tenant, chatID int64, user *store.User, text string) {
	fields := strings.Fields(text)
	cmd := strings.ToLower(fields[0])
	args := strings.TrimSpace(strings.TrimPrefix(text, fields[0]))

	switch cmd {
	case "/pregled":
		b.cmdPregled(ctx, client, t, chatID)
	case "/vozac", "/vozač":
		b.cmdVozac(ctx, client, t, chatID, args)
	case "/planer":
		b.cmdPlaner(ctx, client, t, chatID, args)
	case "/dodaj":
		b.cmdDodaj(ctx, client, t, chatID, args)
	case "/link":
		b.cmdLink(ctx, client, t, chatID, args)
	case "/ispravi":
		b.cmdIspravi(ctx, client, t, chatID)
	case "/sablon", "/šablon":
		b.cmdSablon(ctx, client, chatID)
	case "/pomoc", "/pomoć", "/help":
		client.SendMessage(ctx, chatID, adminHelpText, nil)
	case "/start":
		// already handled upstream, but a linked admin might resend it
	default:
		client.SendMessage(ctx, chatID, "Nepoznata komanda. Pošaljite /pomoc za spisak komandi.", nil)
	}
	_ = user
}

func (b *Bot) cmdPregled(ctx context.Context, client *Client, t *store.Tenant, chatID int64) {
	drivers, err := b.Store.ListDrivers(ctx, t.ID, true)
	if err != nil {
		client.SendMessage(ctx, chatID, "Greška pri učitavanju.", nil)
		return
	}
	allStays, err := b.Store.ListStaysForTenant(ctx, t.ID)
	if err != nil {
		client.SendMessage(ctx, chatID, "Greška pri učitavanju.", nil)
		return
	}
	statuses := status.ComputeAll(drivers, allStays, clock.Today())
	if len(statuses) == 0 {
		client.SendMessage(ctx, chatID, "Nema vozača.", nil)
		return
	}
	var b2 strings.Builder
	b2.WriteString("Pregled (sortirano po riziku):\n")
	for _, st := range statuses {
		fmt.Fprintf(&b2, "%s — %d/90, preostalo %d", st.Driver.Name, st.UsedDays, st.Remaining)
		if st.InSchengen {
			fmt.Fprintf(&b2, ", u Šengenu od %s", st.CurrentSince.Format("02.01.2006"))
		}
		b2.WriteString("\n")
	}
	client.SendMessage(ctx, chatID, b2.String(), nil)
}

func (b *Bot) cmdVozac(ctx context.Context, client *Client, t *store.Tenant, chatID int64, name string) {
	if name == "" {
		client.SendMessage(ctx, chatID, "Upotreba: /vozac <ime>", nil)
		return
	}
	driver, err := b.Store.FindDriverByName(ctx, t.ID, name)
	if err != nil {
		client.SendMessage(ctx, chatID, "Vozač nije pronađen: "+errMsg(err), nil)
		return
	}
	stays, err := b.Store.ListStaysByDriver(ctx, t.ID, driver.ID)
	if err != nil {
		client.SendMessage(ctx, chatID, "Greška pri učitavanju.", nil)
		return
	}
	ds := status.Compute(*driver, stays, clock.Today())
	client.SendMessage(ctx, chatID, driver.Name+" — "+driver.VehicleLabel+"\n"+formatMyDays(ds), nil)
}

func (b *Bot) cmdPlaner(ctx context.Context, client *Client, t *store.Tenant, chatID int64, args string) {
	fields := strings.Fields(args)
	if len(fields) < 3 {
		client.SendMessage(ctx, chatID, "Upotreba: /planer <ime> <dd.mm> <broj_dana>", nil)
		return
	}
	length, err := strconv.Atoi(fields[len(fields)-1])
	if err != nil || length <= 0 {
		client.SendMessage(ctx, chatID, "Neispravan broj dana.", nil)
		return
	}
	dateStr := fields[len(fields)-2]
	name := strings.Join(fields[:len(fields)-2], " ")

	today := clock.Today()
	start, err := resolveFutureDate(dateStr, today)
	if err != nil {
		client.SendMessage(ctx, chatID, "Neispravan datum: "+err.Error(), nil)
		return
	}

	driver, err := b.Store.FindDriverByName(ctx, t.ID, name)
	if err != nil {
		client.SendMessage(ctx, chatID, "Vozač nije pronađen: "+errMsg(err), nil)
		return
	}
	stays, err := b.Store.ListStaysByDriver(ctx, t.ID, driver.ID)
	if err != nil {
		client.SendMessage(ctx, chatID, "Greška pri učitavanju.", nil)
		return
	}
	cstays := status.ToCalcStays(stays)
	ok, peak, overstay := calc.CanDoTrip(cstays, start, length)
	maxLen := calc.MaxTripLength(cstays, start)

	var b2 strings.Builder
	if ok {
		fmt.Fprintf(&b2, "%s MOŽE na turu %s, %d dana (najviše iskorišćeno: %d/90).\n", driver.Name, start.Format("02.01.2006"), length, peak)
	} else {
		fmt.Fprintf(&b2, "%s NE MOŽE na turu %s, %d dana — prekoračenje od %s.\n", driver.Name, start.Format("02.01.2006"), length, overstay.Format("02.01.2006"))
	}
	fmt.Fprintf(&b2, "Najduža moguća tura od %s: %d dana.", start.Format("02.01.2006"), maxLen)
	client.SendMessage(ctx, chatID, b2.String(), nil)
}

func (b *Bot) cmdDodaj(ctx context.Context, client *Client, t *store.Tenant, chatID int64, name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		client.SendMessage(ctx, chatID, "Upotreba: /dodaj <ime>", nil)
		return
	}
	driver, err := b.Store.CreateDriver(ctx, t.ID, name, "")
	if err != nil {
		client.SendMessage(ctx, chatID, "Greška pri dodavanju.", nil)
		return
	}
	client.SendMessage(ctx, chatID, fmt.Sprintf("Dodat vozač: %s. Pošaljite /link %s da ga povežete sa Telegramom.", driver.Name, driver.Name), nil)
}

func (b *Bot) cmdLink(ctx context.Context, client *Client, t *store.Tenant, chatID int64, name string) {
	if name == "" {
		client.SendMessage(ctx, chatID, "Upotreba: /link <ime>", nil)
		return
	}
	driver, err := b.Store.FindDriverByName(ctx, t.ID, name)
	if err != nil {
		client.SendMessage(ctx, chatID, "Vozač nije pronađen: "+errMsg(err), nil)
		return
	}
	token, err := idgen.RandomToken(24)
	if err != nil {
		client.SendMessage(ctx, chatID, "Greška pri generisanju linka.", nil)
		return
	}
	if err := b.Store.CreateDriverLinkToken(ctx, token, t.ID, driver.ID, 48*time.Hour); err != nil {
		client.SendMessage(ctx, chatID, "Greška pri generisanju linka.", nil)
		return
	}
	client.SendMessage(ctx, chatID, b.linkInstructions(driver.Name, token), nil)
}

func (b *Bot) cmdIspravi(ctx context.Context, client *Client, t *store.Tenant, chatID int64) {
	entries, err := b.Store.ListRecentAudit(ctx, t.ID, 5)
	if err != nil || len(entries) == 0 {
		client.SendMessage(ctx, chatID, "Nema skorašnjih izmena.", nil)
		return
	}
	var rows [][]InlineKeyboardButton
	var b2 strings.Builder
	b2.WriteString("Poslednje izmene:\n")
	for i, e := range entries {
		fmt.Fprintf(&b2, "%d. %s na boravku #%d (%s)\n", i+1, auditActionLabel(e.Action), e.EntityID, e.OccurredAt.Format("02.01. 15:04"))
		if e.Action == "create" || e.Action == "close" {
			rows = append(rows, InlineRow(InlineButton(fmt.Sprintf("Poništi #%d", i+1), undoCallback(e.EntityID, e.Action))))
		}
	}
	var markup any
	if len(rows) > 0 {
		markup = InlineKeyboard(rows...)
	}
	client.SendMessage(ctx, chatID, b2.String(), markup)
}

func auditActionLabel(action string) string {
	switch action {
	case "create":
		return "Dodat izlazak"
	case "close":
		return "Dodat povratak"
	case "reopen":
		return "Poništen povratak"
	case "delete":
		return "Obrisan unos"
	case "update":
		return "Izmenjen unos"
	default:
		return action
	}
}

func (b *Bot) cmdSablon(ctx context.Context, client *Client, chatID int64) {
	client.SendMessage(ctx, chatID,
		"Format za uvoz (CSV ili tekst reda po red):\n"+
			"vozac,ulaz,izlaz,tip\n"+
			"Marko Markovic,22.04.2026,29.04.2026,posao\n"+
			"Marko Markovic,08.07.2026,14.07.2026,privatno\n\n"+
			"Ulaz/izlaz u formatu dd.mm.gggg, tip je posao ili privatno. Pošaljite ovakav .csv fajl botu za uvoz.", nil)
}

func errMsg(err error) string {
	if err == store.ErrNotFound {
		return "nema takvog vozača"
	}
	return err.Error()
}

func (b *Bot) linkInstructions(driverName, token string) string {
	if link := b.StartLink(token); link != "" {
		return fmt.Sprintf("Link za %s (važi 48h, jednokratan) — pošaljite ga vozaču, on samo tapne:\n%s", driverName, link)
	}
	return fmt.Sprintf("Link za %s (važi 48h): pošaljite vozaču da u botu pošalje komandu:\n/start %s", driverName, token)
}

// handleAdminDocument handles a CSV file sent directly to the bot, reusing
// the same parser and validation as the web CSV import.
func (b *Bot) handleAdminDocument(ctx context.Context, client *Client, t *store.Tenant, chatID int64, user *store.User, doc *Document) {
	if !strings.HasSuffix(strings.ToLower(doc.FileName), ".csv") {
		client.SendMessage(ctx, chatID, "Trenutno je podržan samo .csv fajl (vozač, ulaz, izlaz, tip).", nil)
		return
	}
	data, err := client.DownloadDocument(ctx, doc.FileID)
	if err != nil {
		client.SendMessage(ctx, chatID, "Greška pri preuzimanju fajla.", nil)
		return
	}

	drivers, err := b.Store.ListDrivers(ctx, t.ID, true)
	if err != nil {
		client.SendMessage(ctx, chatID, "Greška servera.", nil)
		return
	}
	existing := make([]importer.ExistingDriver, len(drivers))
	for i, d := range drivers {
		stays, _ := b.Store.ListStaysByDriver(ctx, t.ID, d.ID)
		es := make([]importer.ExistingStay, len(stays))
		for j, s := range stays {
			es[j] = importer.ExistingStay{Entry: s.Entry, Exit: s.Exit}
		}
		existing[i] = importer.ExistingDriver{ID: d.ID, Name: d.Name, Stays: es}
	}

	rows, err := importer.Parse(bytesReader(data), existing)
	if err != nil {
		client.SendMessage(ctx, chatID, "Greška pri čitanju fajla: "+err.Error(), nil)
		return
	}

	var valid []importer.Row
	var errLines []string
	driverNames := map[string]bool{}
	for _, row := range rows {
		driverNames[row.DriverName] = true
		if row.Valid() {
			valid = append(valid, row)
		} else {
			for _, e := range row.Errors {
				errLines = append(errLines, fmt.Sprintf("red %d (%s)", row.LineNum, e))
			}
		}
	}

	summary := fmt.Sprintf("Pronađeno %d vozača, %d boravka. %d ispravno, %d grešaka.", len(driverNames), len(rows), len(valid), len(errLines))
	if len(errLines) > 0 {
		max := len(errLines)
		if max > 10 {
			max = 10
		}
		summary += "\n" + strings.Join(errLines[:max], "\n")
	}
	client.SendMessage(ctx, chatID, summary, nil)

	if len(valid) == 0 {
		return
	}
	b.setPending(chatID, "import", importPending{Rows: valid, UserID: user.ID})
	client.SendMessage(ctx, chatID, fmt.Sprintf("Potvrditi uvoz %d boravka?", len(valid)),
		InlineKeyboard(InlineRow(InlineButton("Potvrdi uvoz", "import_confirm"), InlineButton("Otkaži", "import_cancel"))))
}

type importPending struct {
	Rows   []importer.Row
	UserID int64
}

type quickEntryPending struct {
	DriverID   int64
	DriverName string
	Ranges     []QuickRange
	UserID     int64
}

type newDriverPending struct {
	DriverName string
	Ranges     []QuickRange
	UserID     int64
}

func (b *Bot) handleQuickEntry(ctx context.Context, client *Client, t *store.Tenant, chatID int64, user *store.User, text string) {
	driverName, ranges, err := ParseQuickEntry(text, clock.Today())
	if err != nil {
		client.SendMessage(ctx, chatID, "Ne razumem: "+err.Error(), nil)
		return
	}

	driver, err := b.Store.FindDriverByName(ctx, t.ID, driverName)
	if err == store.ErrNotFound {
		b.setPending(chatID, "new_driver_quickentry", newDriverPending{DriverName: driverName, Ranges: ranges, UserID: user.ID})
		client.SendMessage(ctx, chatID, fmt.Sprintf("Nepoznat vozač %q. Dodati novog vozača?", driverName),
			InlineKeyboard(InlineRow(InlineButton("Da, dodaj", "newdriver_confirm"), InlineButton("Otkaži", "newdriver_cancel"))))
		return
	}
	if err != nil {
		client.SendMessage(ctx, chatID, "Greška: "+errMsg(err), nil)
		return
	}

	b.confirmQuickEntry(ctx, client, t, chatID, user, *driver, ranges)
}

func (b *Bot) confirmQuickEntry(ctx context.Context, client *Client, t *store.Tenant, chatID int64, user *store.User, driver store.Driver, ranges []QuickRange) {
	errs := b.validateQuickEntryRanges(ctx, t.ID, driver.ID, ranges)
	if len(errs) > 0 {
		client.SendMessage(ctx, chatID, "Greške:\n"+strings.Join(errs, "\n"), nil)
		return
	}
	b.setPending(chatID, "quickentry", quickEntryPending{DriverID: driver.ID, DriverName: driver.Name, Ranges: ranges, UserID: user.ID})

	var b2 strings.Builder
	fmt.Fprintf(&b2, "Tumačenje za %s:\n", driver.Name)
	for _, r := range ranges {
		typ := "posao"
		if r.Private {
			typ = "privatno"
		}
		fmt.Fprintf(&b2, "%s–%s (%s)\n", r.Entry.Format("02.01.2006"), r.Exit.Format("02.01.2006"), typ)
	}
	b2.WriteString("Potvrdi unos?")
	client.SendMessage(ctx, chatID, b2.String(),
		InlineKeyboard(InlineRow(InlineButton("Potvrdi", "quickentry_confirm"), InlineButton("Otkaži", "quickentry_cancel"))))
}

// validateQuickEntryRanges dry-runs the inserts inside a transaction that is
// always rolled back, so it reports the exact same validation errors
// (overlap, future dates, etc.) the real commit would, without writing
// anything.
func (b *Bot) validateQuickEntryRanges(ctx context.Context, tenantID, driverID int64, ranges []QuickRange) []string {
	var errs []string
	b.Store.WithTx(ctx, func(tx *store.Store) error {
		for i, r := range ranges {
			typ := "work"
			if r.Private {
				typ = "private"
			}
			exit := r.Exit
			if _, err := tx.CreateStay(ctx, tenantID, driverID, r.Entry, &exit, typ, "telegram_admin", "", nil); err != nil {
				errs = append(errs, fmt.Sprintf("red %d (%s–%s): %s", i+1, r.Entry.Format("02.01.2006"), r.Exit.Format("02.01.2006"), err.Error()))
			}
		}
		return fmt.Errorf("preview: force rollback")
	})
	return errs
}
