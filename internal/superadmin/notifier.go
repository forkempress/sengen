package superadmin

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"shengen/internal/clock"
	"shengen/internal/health"
	"shengen/internal/store"
	"shengen/internal/telegram"
)

// Notifier pushes platform events to every operator. The job runner calls
// Tick every few minutes; each event is claimed in superadmin_notifications
// before it is sent, so a restart or an overlapping tick never sends it twice.
type Notifier struct {
	*Service
	Client *telegram.Client

	lastWebhookCheck time.Time
	lastErrorAlert   time.Time
}

func (s *Service) Notifier(client *telegram.Client) *Notifier {
	return &Notifier{Service: s, Client: client}
}

const (
	// DigestHour is when the daily digest goes out, Europe/Belgrade.
	DigestHour = 8
	// TrialWarnDays: operators hear about a trial this many days before it ends.
	TrialWarnDays = 3

	newTenantWindow     = 24 * time.Hour      // a firm this young counts as a new registration
	newSignupWindow     = 24 * time.Hour      // a sign-up started this recently is reported
	firstActivityWindow = 24 * time.Hour      // the first stay must be this recent to report
	newFirmAge          = 30 * 24 * time.Hour // "first activity" is reported for firms this young only
	webhookCheckEvery   = 15 * time.Minute
	errorAlertEvery     = time.Hour
)

// Tick checks every event once. Errors are logged; the next tick tries again.
func (n *Notifier) Tick(ctx context.Context) {
	if n.Client == nil || len(n.adminIDs) == 0 {
		return
	}
	now := clock.Now()

	n.checkSignups(ctx, now)
	n.checkDocImports(ctx, now)
	firms, err := n.Store.ListTenantOverviews(ctx)
	if err != nil {
		log.Printf("superadmin: list firms: %v", err)
		return
	}
	for _, f := range firms {
		n.checkFirm(ctx, f, now)
	}
	n.checkErrors(ctx, now)
	n.checkWebhook(ctx, now)
	n.checkBackup(ctx, now)
	n.checkAnthropicKey(ctx, now)
	n.checkDigest(ctx, firms, now)
}

func (n *Notifier) checkFirm(ctx context.Context, f store.TenantOverview, now time.Time) {
	id := strconv.FormatInt(f.ID, 10)
	if now.Sub(f.CreatedAt) <= newTenantWindow {
		msg := fmt.Sprintf("🆕 Nova firma: #%d %s (oznaka %s), %s", f.ID, f.Name, f.Slug, planLabel(f.Plan))
		if f.Plan == store.PlanTrial && f.TrialUntil != nil {
			msg += " do " + f.TrialUntil.Format("02.01.2006")
		}
		n.notify(ctx, "new_tenant", id, msg)
	}
	if f.Plan == store.PlanTrial && f.TrialUntil != nil {
		if d := daysUntil(*f.TrialUntil); d >= 0 && d <= TrialWarnDays {
			// The ref carries the date, so an extended trial warns again.
			n.notify(ctx, "trial_expiring", id+":"+f.TrialUntil.Format("2006-01-02"),
				fmt.Sprintf("⏳ %s: #%d %s, %d vozača", trialLabel(f), f.ID, f.Name, f.ActiveDrivers))
		}
	}
	if f.FirstStayAt != nil && now.Sub(f.CreatedAt) <= newFirmAge && now.Sub(*f.FirstStayAt) <= firstActivityWindow {
		n.notify(ctx, "first_activity", id,
			fmt.Sprintf("✅ Prva aktivnost: #%d %s je uneo prvi boravak (%s)", f.ID, f.Name, belgrade(*f.FirstStayAt).Format("02.01.2006 15:04")))
	}
}

// checkSignups reports each sign-up when its form is sent, before the email
// is confirmed; the firm itself is reported as new_tenant once it is.
func (n *Notifier) checkSignups(ctx context.Context, now time.Time) {
	signups, err := n.Store.ListSignupsSince(ctx, now.Add(-newSignupWindow))
	if err != nil {
		log.Printf("superadmin: list signups: %v", err)
		return
	}
	for _, su := range signups {
		n.notify(ctx, "signup_started", su.Ref,
			fmt.Sprintf("📝 Započeta registracija: %s — %s <%s>, %s. Čeka potvrdu mejla.",
				su.FirmName, su.Name, su.Email, belgrade(su.CreatedAt).Format("02.01.2006 15:04")))
	}
}

func hourRef(now time.Time) string { return now.Format("2006-01-02T15") }

// checkErrors sends at most one message an hour, with counts per kind.
func (n *Notifier) checkErrors(ctx context.Context, now time.Time) {
	if now.Sub(n.lastErrorAlert) < errorAlertEvery {
		return
	}
	counts := health.ErrorsSince(n.lastErrorAlert)
	if len(counts) == 0 {
		return
	}
	since := "od pokretanja"
	if !n.lastErrorAlert.IsZero() {
		since = "od " + belgrade(n.lastErrorAlert).Format("15:04")
	}
	if n.notify(ctx, "errors", hourRef(now), fmt.Sprintf("🔴 Greške %s: %s. Detalji su u logu servera.", since, errorsLine(counts))) {
		n.lastErrorAlert = now
	}
}

func (n *Notifier) checkWebhook(ctx context.Context, now time.Time) {
	if now.Sub(n.lastWebhookCheck) < webhookCheckEvery {
		return
	}
	n.lastWebhookCheck = now
	info, err := n.Client.GetWebhookInfo(ctx)
	var problem string
	if err != nil {
		problem = "provera webhook-a nije uspela"
		log.Printf("superadmin: webhook info: %v", err)
	} else {
		problem = webhookProblem(info, now)
	}
	if problem != "" {
		n.notify(ctx, "webhook", hourRef(now), "📵 Bot možda ne prima poruke: "+problem)
	}
}

func (n *Notifier) checkBackup(ctx context.Context, now time.Time) {
	if text, problem := backupStatus(n.BackupHeartbeatFile, now); problem {
		n.notify(ctx, "backup", now.Format("2006-01-02"), "💾 Backup: "+strings.TrimPrefix(text, "⚠️ "))
	}
}

func (n *Notifier) checkDigest(ctx context.Context, firms []store.TenantOverview, now time.Time) {
	if now.Hour() != DigestHour {
		return
	}
	date := now.Format("2006-01-02")
	sent, err := n.Store.TryRecordSuperadminNotification(ctx, "digest", date)
	if err != nil || !sent {
		if err != nil {
			log.Printf("superadmin: digest dedup: %v", err)
		}
		return
	}
	msg, err := n.Digest(ctx, firms, now)
	if err != nil {
		log.Printf("superadmin: digest: %v", err)
		return
	}
	n.send(ctx, msg)
}

// Digest is the daily summary: totals, MRR, and what happened yesterday
// (Belgrade calendar day).
func (n *Notifier) Digest(ctx context.Context, firms []store.TenantOverview, now time.Time) (string, error) {
	stats, err := n.cmdStats(ctx)
	if err != nil {
		return "", err
	}
	m := ComputeMRR(firms)

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	yesterday := today.AddDate(0, 0, -1)
	newFirms := 0
	for _, f := range firms {
		if !f.CreatedAt.Before(yesterday) && f.CreatedAt.Before(today) {
			newFirms++
		}
	}
	newStays, err := n.Store.CountStaysCreatedBetween(ctx, yesterday, today)
	if err != nil {
		return "", err
	}
	signups, err := n.Store.CountSignupsBetween(ctx, yesterday, today)
	if err != nil {
		return "", err
	}
	inactive := len(Inactive(firms, now))
	docsYesterday, err := n.Store.DocumentImportSpendSince(ctx, 0, yesterday)
	if err != nil {
		return "", err
	}
	docsToday, err := n.Store.DocumentImportSpendSince(ctx, 0, today)
	if err != nil {
		return "", err
	}
	docsMonth, err := n.Store.DocumentImportSpendSince(ctx, 0, monthStart(now))
	if err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "☀️ Dnevni pregled — %s\n\n", now.Format("02.01.2006"))
	fmt.Fprintf(&b, "Juče: %d novih firmi, %d unetih boravaka\n", newFirms, newStays)
	fmt.Fprintf(&b, "Registracije juče: %s\n", signupsLine(signups))
	fmt.Fprintf(&b, "MRR: %s (%d plaćenih firmi), trial potencijal %s\n", formatEUR(m.PaidCents), m.PaidTenants, formatEUR(m.TrialCents))
	fmt.Fprintf(&b, "Uvoz dokumenata juče: %d (%s), ovog meseca %s\n",
		docsYesterday.Count-docsToday.Count, formatUSD(docsYesterday.CostMicroUSD-docsToday.CostMicroUSD), formatUSD(docsMonth.CostMicroUSD))
	fmt.Fprintf(&b, "Neaktivne 14+ dana: %d\n", inactive)
	fmt.Fprintf(&b, "Greške (24h): %s\n\n", errorsLine(health.ErrorsSince(now.Add(-24*time.Hour))))
	// The stats header repeats the date; drop it.
	if i := strings.IndexByte(stats, '\n'); i >= 0 {
		stats = stats[i+1:]
	}
	b.WriteString(stats)
	return b.String(), nil
}

// notify claims (kind, ref) and sends text to every operator; it reports
// whether this call sent it.
func (n *Notifier) notify(ctx context.Context, kind, ref, text string) bool {
	sent, err := n.Store.TryRecordSuperadminNotification(ctx, kind, ref)
	if err != nil {
		log.Printf("superadmin: notification dedup %s/%s: %v", kind, ref, err)
		return false
	}
	if !sent {
		return false
	}
	n.send(ctx, text)
	return true
}

// send delivers to every operator. A failure is logged but not reported to
// health: an operator who never opened the bot would otherwise trigger an
// error alert that itself fails, every hour.
func (n *Notifier) send(ctx context.Context, text string) {
	for _, id := range n.adminIDs {
		// In a private chat the chat id is the user id.
		if err := n.Client.SendMessage(ctx, id, text, nil); err != nil {
			log.Printf("superadmin: send to operator %d: %v", id, err)
		}
	}
}
