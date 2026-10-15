package superadmin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"shengen/internal/clock"
	"shengen/internal/health"
	"shengen/internal/status"
	"shengen/internal/store"
	"shengen/internal/telegram"
)

// InactiveAfter: a firm with nothing new for this long is listed by /neaktivne.
const InactiveAfter = 14 * 24 * time.Hour

// maxListed caps /firme so the reply stays under Telegram's 4096 characters.
const maxListed = 50

var planLabels = map[string]string{
	store.PlanTrial:  "trial",
	store.PlanPaid:   "plaćena",
	store.PlanPaused: "pauzirana",
}

func planLabel(p string) string {
	if l, ok := planLabels[p]; ok {
		return l
	}
	return p
}

var roleLabels = map[string]string{"owner": "vlasnici", "dispatcher": "dispečeri"}

// formatRSD renders whole dinars as "1.234 din".
func formatRSD(rsd int) string {
	sign := ""
	if rsd < 0 {
		sign, rsd = "-", -rsd
	}
	whole := strconv.Itoa(rsd)
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	return fmt.Sprintf("%s%s din", sign, b.String())
}

func belgrade(t time.Time) time.Time { return t.In(clock.Now().Location()) }

func formatDate(t time.Time) string { return belgrade(t).Format("02.01.2006") }

// daysUntil counts calendar days from today to a date-only value.
func daysUntil(date time.Time) int {
	return int(date.Sub(clock.Today()).Hours() / 24)
}

func countsLine(counts map[string]int, order []string, labels func(string) string) string {
	var parts []string
	seen := map[string]bool{}
	for _, k := range order {
		seen[k] = true
		if counts[k] > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", labels(k), counts[k]))
		}
	}
	var rest []string
	for k := range counts {
		if !seen[k] && counts[k] > 0 {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, k := range rest {
		parts = append(parts, fmt.Sprintf("%s %d", labels(k), counts[k]))
	}
	return strings.Join(parts, ", ")
}

func roleLabel(r string) string {
	if l, ok := roleLabels[r]; ok {
		return l
	}
	return r
}

func (s *Service) cmdStats(ctx context.Context) (string, error) {
	now := clock.Now()
	p, err := s.Store.PlatformStats(ctx, now.Add(-7*24*time.Hour))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "📊 Statistika (%s)\n", now.Format("02.01.2006 15:04"))
	fmt.Fprintf(&b, "Firme: %d", p.Tenants())
	if line := countsLine(p.TenantsByPlan, []string{store.PlanTrial, store.PlanPaid, store.PlanPaused}, planLabel); line != "" {
		fmt.Fprintf(&b, " (%s)", line)
	}
	fmt.Fprintf(&b, "\nKorisnici: %d", p.Users())
	if line := countsLine(p.UsersByRole, []string{"owner", "dispatcher"}, roleLabel); line != "" {
		fmt.Fprintf(&b, " (%s)", line)
	}
	fmt.Fprintf(&b, ", na Telegramu %d\n", p.UsersOnTelegram)
	fmt.Fprintf(&b, "Vozači: %d aktivnih, %d povezanih sa botom\n", p.DriversActive, p.DriversLinked)
	fmt.Fprintf(&b, "U Šengenu sada: %d vozača\n", p.DriversInSchengen)
	fmt.Fprintf(&b, "Boravci: %d ukupno, %d unetih u poslednjih 7 dana\n", p.Stays, p.StaysSince)
	signups, err := s.Store.CountSignupsBetween(ctx, now.Add(-7*24*time.Hour), now)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&b, "Registracije (7 dana): %s", signupsLine(signups))
	return b.String(), nil
}

func signupsLine(c store.SignupCounts) string {
	return fmt.Sprintf("%d započeto, %d potvrđeno", c.Started, c.Confirmed)
}

// MRR sums what firms pay per month: active drivers × the firm's price.
type MRR struct {
	PaidRSD, PaidTenants, PaidDrivers    int
	TrialRSD, TrialTenants, TrialDrivers int
}

func ComputeMRR(firms []store.TenantOverview) MRR {
	var m MRR
	for _, f := range firms {
		switch f.Plan {
		case store.PlanPaid:
			m.PaidRSD += f.MonthlyRSD()
			m.PaidTenants++
			m.PaidDrivers += f.ActiveDrivers
		case store.PlanTrial:
			m.TrialRSD += f.MonthlyRSD()
			m.TrialTenants++
			m.TrialDrivers += f.ActiveDrivers
		}
	}
	return m
}

func (s *Service) cmdMRR(ctx context.Context) (string, error) {
	firms, err := s.Store.ListTenantOverviews(ctx)
	if err != nil {
		return "", err
	}
	m := ComputeMRR(firms)
	var b strings.Builder
	fmt.Fprintf(&b, "💶 MRR: %s\n", formatRSD(m.PaidRSD))
	fmt.Fprintf(&b, "Plaćene firme: %d, vozača %d", m.PaidTenants, m.PaidDrivers)
	if m.PaidTenants > 0 {
		fmt.Fprintf(&b, ", prosek %s po firmi", formatRSD(m.PaidRSD/m.PaidTenants))
	}
	fmt.Fprintf(&b, "\nTrial (potencijal): %s iz %d firmi, %d vozača", formatRSD(m.TrialRSD), m.TrialTenants, m.TrialDrivers)
	return b.String(), nil
}

func activityLabel(o store.TenantOverview) string {
	if o.LastActivity == nil {
		return "bez aktivnosti"
	}
	return "aktivnost " + formatDate(*o.LastActivity)
}

func (s *Service) cmdFirme(ctx context.Context) (string, error) {
	firms, err := s.Store.ListTenantOverviews(ctx)
	if err != nil {
		return "", err
	}
	if len(firms) == 0 {
		return "Nema firmi.", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "🏢 Firme (%d):\n", len(firms))
	for i, f := range firms {
		if i == maxListed {
			fmt.Fprintf(&b, "… i još %d", len(firms)-maxListed)
			break
		}
		fmt.Fprintf(&b, "#%d %s — %s, %d vozača, %s\n", f.ID, f.Name, planLabel(f.Plan), f.ActiveDrivers, activityLabel(f))
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func trialLabel(f store.TenantOverview) string {
	if f.TrialUntil == nil {
		return "trial bez roka"
	}
	d := daysUntil(*f.TrialUntil)
	when := f.TrialUntil.Format("02.01.2006")
	switch {
	case d > 0:
		return fmt.Sprintf("trial do %s (još %d dana)", when, d)
	case d == 0:
		return fmt.Sprintf("trial ističe danas (%s)", when)
	default:
		return fmt.Sprintf("trial istekao %s (pre %d dana)", when, -d)
	}
}

// tenantDriverCounts says how many of a firm's active drivers are in each
// state today — counts only, computed in memory and never printed per driver.
type tenantDriverCounts struct {
	InSchengen, Red, Orange, Urgent int
}

func (s *Service) driverCounts(ctx context.Context, tenantID int64) (tenantDriverCounts, error) {
	var c tenantDriverCounts
	t, err := s.Store.GetTenant(ctx, tenantID)
	if err != nil {
		return c, err
	}
	settings, err := t.ParsedSettings()
	if err != nil {
		return c, err
	}
	drivers, err := s.Store.ListDrivers(ctx, tenantID, true)
	if err != nil {
		return c, err
	}
	stays, err := s.Store.ListStaysForTenant(ctx, tenantID)
	if err != nil {
		return c, err
	}
	th := status.ThresholdsFor(settings)
	for _, ds := range status.ComputeAll(drivers, stays, clock.Today()) {
		if ds.InSchengen {
			c.InSchengen++
		}
		switch th.Level(ds.Remaining) {
		case status.LevelOver:
			c.Urgent++
		case status.LevelCritical:
			c.Red++
		case status.LevelWarn:
			c.Orange++
		}
	}
	return c, nil
}

func (s *Service) cmdFirma(ctx context.Context, args string) (string, error) {
	id, err := strconv.ParseInt(strings.TrimPrefix(strings.TrimSpace(args), "#"), 10, 64)
	if err != nil || id <= 0 {
		return "Upotreba: /firma <id> (id iz /firme)", nil
	}
	f, err := s.Store.GetTenantOverview(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Sprintf("Firma #%d ne postoji.", id), nil
	}
	if err != nil {
		return "", err
	}
	roles, err := s.Store.CountUsersByRole(ctx, id)
	if err != nil {
		return "", err
	}
	dc, err := s.driverCounts(ctx, id)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "🏢 #%d %s (oznaka %s)\n", f.ID, f.Name, f.Slug)
	fmt.Fprintf(&b, "Kreirana: %s\n", formatDate(f.CreatedAt))
	switch f.Plan {
	case store.PlanTrial:
		fmt.Fprintf(&b, "Plan: %s\n", trialLabel(*f))
	default:
		fmt.Fprintf(&b, "Plan: %s\n", planLabel(f.Plan))
	}
	fmt.Fprintf(&b, "Cena: %s po vozaču, mesečno %s\n", formatRSD(f.PricePerDriverRSD), formatRSD(f.MonthlyRSD()))
	fmt.Fprintf(&b, "Korisnici: %d", f.Users)
	if line := countsLine(roles, []string{"owner", "dispatcher"}, roleLabel); line != "" {
		fmt.Fprintf(&b, " (%s)", line)
	}
	fmt.Fprintf(&b, "\nVozači: %d aktivnih, %d povezanih sa botom\n", f.ActiveDrivers, f.LinkedDrivers)
	fmt.Fprintf(&b, "Boravci: %d", f.Stays)
	if f.FirstStayAt != nil {
		fmt.Fprintf(&b, ", prvi unet %s", formatDate(*f.FirstStayAt))
	}
	fmt.Fprintf(&b, "\nPoslednja aktivnost: ")
	if f.LastActivity != nil {
		fmt.Fprintf(&b, "%s\n", formatDate(*f.LastActivity))
	} else {
		b.WriteString("nema\n")
	}
	fmt.Fprintf(&b, "Danas: %d u Šengenu, %d preko limita, %d kritično, %d pažnja", dc.InSchengen, dc.Urgent, dc.Red, dc.Orange)
	return b.String(), nil
}

func (s *Service) cmdTrial(ctx context.Context) (string, error) {
	firms, err := s.Store.ListTenantOverviews(ctx)
	if err != nil {
		return "", err
	}
	var trial []store.TenantOverview
	for _, f := range firms {
		if f.Plan == store.PlanTrial {
			trial = append(trial, f)
		}
	}
	if len(trial) == 0 {
		return "Nema firmi u trialu.", nil
	}
	// Soonest end first; a trial without an end date goes last.
	sort.SliceStable(trial, func(i, j int) bool {
		a, b := trial[i].TrialUntil, trial[j].TrialUntil
		if a == nil || b == nil {
			return b == nil && a != nil
		}
		return a.Before(*b)
	})
	var b strings.Builder
	fmt.Fprintf(&b, "⏳ Trial (%d):\n", len(trial))
	for i, f := range trial {
		if i == maxListed {
			fmt.Fprintf(&b, "… i još %d", len(trial)-maxListed)
			break
		}
		fmt.Fprintf(&b, "#%d %s — %s, %d vozača\n", f.ID, f.Name, trialLabel(f), f.ActiveDrivers)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// Inactive returns the firms (not paused) older than InactiveAfter with no
// activity in that time, least recently active first.
func Inactive(firms []store.TenantOverview, now time.Time) []store.TenantOverview {
	cutoff := now.Add(-InactiveAfter)
	var out []store.TenantOverview
	for _, f := range firms {
		if f.Plan == store.PlanPaused || f.CreatedAt.After(cutoff) {
			continue
		}
		if f.LastActivity == nil || f.LastActivity.Before(cutoff) {
			out = append(out, f)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].LastActivity, out[j].LastActivity
		if a == nil || b == nil {
			return a == nil && b != nil
		}
		return a.Before(*b)
	})
	return out
}

func (s *Service) cmdNeaktivne(ctx context.Context) (string, error) {
	firms, err := s.Store.ListTenantOverviews(ctx)
	if err != nil {
		return "", err
	}
	now := clock.Now()
	inactive := Inactive(firms, now)
	if len(inactive) == 0 {
		return "Nema neaktivnih firmi (14+ dana).", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "💤 Bez aktivnosti 14+ dana (%d):\n", len(inactive))
	for i, f := range inactive {
		if i == maxListed {
			fmt.Fprintf(&b, "… i još %d", len(inactive)-maxListed)
			break
		}
		last := "nikad"
		if f.LastActivity != nil {
			last = fmt.Sprintf("%s (pre %d dana)", formatDate(*f.LastActivity), int(now.Sub(*f.LastActivity).Hours()/24))
		}
		fmt.Fprintf(&b, "#%d %s — %s, poslednja aktivnost %s\n", f.ID, f.Name, planLabel(f.Plan), last)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// Webhook problems worth an alert.
const (
	webhookErrorWindow = 30 * time.Minute
	webhookMaxPending  = 100
)

// webhookProblem describes what is wrong with update delivery, or "".
func webhookProblem(info telegram.WebhookInfo, now time.Time) string {
	var problems []string
	if info.URL == "" {
		problems = append(problems, "webhook nije registrovan")
	}
	if info.LastErrorDate != 0 {
		at := time.Unix(info.LastErrorDate, 0)
		if now.Sub(at) <= webhookErrorWindow {
			problems = append(problems, fmt.Sprintf("greška u isporuci pre %s: %s", humanDuration(now.Sub(at)), info.LastErrorMessage))
		}
	}
	if info.PendingUpdateCount > webhookMaxPending {
		problems = append(problems, fmt.Sprintf("%d poruka čeka isporuku", info.PendingUpdateCount))
	}
	return strings.Join(problems, "; ")
}

// Backup heartbeat older than this is a failed backup.
const backupMaxAge = 26 * time.Hour

// backupStatus describes the backup state and whether it needs attention.
func backupStatus(file string, now time.Time) (text string, problem bool) {
	if file == "" {
		return "nije podešen", false
	}
	fi, err := os.Stat(file)
	if err != nil {
		return "⚠️ nema zapisa o uspešnom backupu", true
	}
	age := now.Sub(fi.ModTime())
	if age > backupMaxAge {
		return fmt.Sprintf("⚠️ poslednji uspešan pre %s", humanDuration(age)), true
	}
	return fmt.Sprintf("OK, poslednji pre %s", humanDuration(age)), false
}

func humanDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh %dmin", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%d dana", int(d.Hours()/24))
	}
}

var errorKindLabels = map[string]string{
	health.KindTelegramPanic: "pad obrade poruke",
	health.KindJobPanic:      "pad pozadinskog posla",
	health.KindJobError:      "greška pozadinskog posla",
	health.KindSendError:     "neuspelo slanje",
}

func errorsLine(counts map[string]int) string {
	if len(counts) == 0 {
		return "nema"
	}
	return countsLine(counts, []string{health.KindTelegramPanic, health.KindJobPanic, health.KindJobError, health.KindSendError},
		func(k string) string {
			if l, ok := errorKindLabels[k]; ok {
				return l
			}
			return k
		})
}

func (s *Service) cmdZdravlje(ctx context.Context, client *telegram.Client) string {
	now := clock.Now()
	var b strings.Builder
	b.WriteString("🩺 Zdravlje\n")
	fmt.Fprintf(&b, "Radi od: %s (%s)\n", belgrade(health.Started()).Format("02.01.2006 15:04"), humanDuration(now.Sub(health.Started())))

	if err := s.Store.Ping(ctx); err != nil {
		b.WriteString("Baza: ⚠️ ne odgovara\n")
	} else if size, err := s.Store.DatabaseSize(ctx); err == nil {
		fmt.Fprintf(&b, "Baza: OK, %.1f MB\n", float64(size)/(1<<20))
	} else {
		b.WriteString("Baza: OK\n")
	}

	if tick := health.LastJobTick(); tick.IsZero() {
		b.WriteString("Pozadinski poslovi: još nisu pokrenuti\n")
	} else {
		fmt.Fprintf(&b, "Pozadinski poslovi: poslednji krug pre %s\n", humanDuration(now.Sub(tick)))
	}

	switch {
	case client == nil:
		b.WriteString("Telegram: bot nije podešen\n")
	default:
		info, err := client.GetWebhookInfo(ctx)
		if err != nil {
			b.WriteString("Telegram: ⚠️ provera nije uspela\n")
		} else if p := webhookProblem(info, now); p != "" {
			fmt.Fprintf(&b, "Telegram: ⚠️ %s\n", p)
		} else {
			fmt.Fprintf(&b, "Telegram: OK, na čekanju %d\n", info.PendingUpdateCount)
		}
	}

	text, _ := backupStatus(s.BackupHeartbeatFile, now)
	fmt.Fprintf(&b, "Backup: %s\n", text)
	if s.AnthropicKeyExpires != nil {
		fmt.Fprintf(&b, "Claude ključ: %s\n", keyExpiryText(*s.AnthropicKeyExpires))
	}
	fmt.Fprintf(&b, "Greške (24h): %s", errorsLine(health.ErrorsSince(now.Add(-24*time.Hour))))
	return b.String()
}
