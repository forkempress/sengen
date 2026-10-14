// Package jobs runs the recurring background checks that don't belong in a
// request handler: driver reminders and threshold warnings, dispatcher
// threshold alerts, and the daily digest. One ticker loop scans every
// tenant each tick; dedup is handled by sendOnce (store.TryRecordAlert) so a
// 5-minute tick cadence never produces duplicate notifications for the same
// day, and a failed send is retried on the next tick.
package jobs

import (
	"context"
	"fmt"
	"log"
	"time"

	"shengen/internal/auth"
	"shengen/internal/clock"
	"shengen/internal/health"
	"shengen/internal/status"
	"shengen/internal/store"
	"shengen/internal/superadmin"
	"shengen/internal/telegram"
)

const tickInterval = 5 * time.Minute

type Runner struct {
	Store *store.Store
	// Client is the shared bot used to notify every firm; nil when no bot
	// token is configured, in which case there is nothing to send.
	Client *telegram.Client
	// SuperAdmin pushes platform events to the operators; nil when no
	// ADMIN_TELEGRAM_IDS are configured.
	SuperAdmin *superadmin.Notifier

	lastCleanup time.Time
}

// cleanupInterval: expired sessions, link tokens and old update ids are
// deleted once a day (and at startup).
const cleanupInterval = 24 * time.Hour

// telegramUpdateRetention: Telegram gives up redelivering an update after
// 24 hours, so a week of update ids is plenty for dedup.
const telegramUpdateRetention = 7 * 24 * time.Hour

func New(st *store.Store, client *telegram.Client) *Runner {
	return &Runner{Store: st, Client: client}
}

// Start runs the job loop until ctx is canceled. Call it in a goroutine.
func (r *Runner) Start(ctx context.Context) {
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	r.tick(ctx) // run once immediately so a restart doesn't wait a full interval
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

func (r *Runner) tick(ctx context.Context) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("jobs: panic during tick: %v", rec)
			health.ReportError(health.KindJobPanic)
		}
	}()

	health.MarkJobTick(time.Now())

	if now := time.Now(); now.Sub(r.lastCleanup) >= cleanupInterval {
		r.Cleanup(ctx, now)
		r.lastCleanup = now
	}

	if r.Client == nil {
		return
	}
	tenants, err := r.Store.ListAllTenants(ctx)
	if err != nil {
		log.Printf("jobs: list tenants: %v", err)
		health.ReportError(health.KindJobError)
		return
	}
	for _, t := range tenants {
		r.processTenant(ctx, t)
	}
	if r.SuperAdmin != nil {
		r.SuperAdmin.Tick(ctx)
	}
}

// Cleanup deletes what is no longer valid: sessions past their absolute or
// idle expiry, expired link tokens, and Telegram update ids too old to be
// redelivered. Failures are logged; the next day tries again.
func (r *Runner) Cleanup(ctx context.Context, now time.Time) {
	if n, err := r.Store.DeleteExpiredSessions(ctx, now, now.Add(-auth.SessionIdleTimeout)); err != nil {
		log.Printf("jobs: cleanup sessions: %v", err)
	} else if n > 0 {
		log.Printf("jobs: deleted %d expired sessions", n)
	}
	if n, err := r.Store.DeleteExpiredLinkTokens(ctx, now); err != nil {
		log.Printf("jobs: cleanup link tokens: %v", err)
	} else if n > 0 {
		log.Printf("jobs: deleted %d expired link tokens", n)
	}
	if _, err := r.Store.DeleteTelegramUpdatesBefore(ctx, now.Add(-telegramUpdateRetention)); err != nil {
		log.Printf("jobs: cleanup telegram updates: %v", err)
	}
}

func (r *Runner) processTenant(ctx context.Context, t store.Tenant) {
	settings, err := t.ParsedSettings()
	if err != nil {
		log.Printf("jobs: settings for tenant %d: %v", t.ID, err)
		health.ReportError(health.KindJobError)
		return
	}
	client := r.Client
	today := clock.Today()
	dateStr := today.Format("2006-01-02")

	drivers, err := r.Store.ListDrivers(ctx, t.ID, true)
	if err != nil {
		log.Printf("jobs: list drivers for tenant %d: %v", t.ID, err)
		health.ReportError(health.KindJobError)
		return
	}
	allStays, err := r.Store.ListStaysForTenant(ctx, t.ID)
	if err != nil {
		log.Printf("jobs: list stays for tenant %d: %v", t.ID, err)
		health.ReportError(health.KindJobError)
		return
	}
	statuses := status.ComputeAll(drivers, allStays, today)

	r.checkDispatcherAlerts(ctx, client, t, settings, statuses, dateStr)
	r.checkDriverWarnings(ctx, client, t, settings, statuses, today, dateStr)
	r.checkOpenStayReminders(ctx, client, t, drivers, allStays, today, dateStr)
	r.checkDailyDigest(ctx, client, t, settings, statuses, today, dateStr)
}

func (r *Runner) sendAlert(ctx context.Context, client *telegram.Client, t store.Tenant, settings store.TenantSettings, driverID int64, alertType, dateStr, text string) {
	if settings.TelegramAlertChatID == 0 {
		return
	}
	r.sendOnce(ctx, client, t.ID, driverID, alertType, dateStr, settings.TelegramAlertChatID, text, nil)
}

// sendOnce sends text unless the (driver, alertType, key) slot was already
// used. The slot is claimed before sending, so two ticks never both send;
// when the send fails it is released again, so the next tick retries
// instead of the message being lost until the next day (AI-60).
func (r *Runner) sendOnce(ctx context.Context, client *telegram.Client, tenantID, driverID int64, alertType, key string, chatID int64, text string, markup any) {
	sent, err := r.Store.TryRecordAlert(ctx, tenantID, driverID, alertType, key)
	if err != nil {
		log.Printf("jobs: dedup check failed for tenant %d: %v", tenantID, err)
		health.ReportError(health.KindJobError)
		return
	}
	if !sent {
		return // already sent
	}
	if err := client.SendMessage(ctx, chatID, text, markup); err != nil {
		log.Printf("jobs: send %s failed for tenant %d: %v", alertType, tenantID, err)
		health.ReportError(health.KindSendError)
		if err := r.Store.ReleaseAlert(ctx, tenantID, driverID, alertType, key); err != nil {
			log.Printf("jobs: release %s for tenant %d: %v", alertType, tenantID, err)
		}
	}
}

// thresholds returns the firm's red and orange limits in days, with the
// defaults for a firm that never set them.
func thresholds(settings store.TenantSettings) (red, orange int) {
	red, orange = settings.ThresholdRedDays, settings.ThresholdOrangeDays
	if red == 0 {
		red = 15
	}
	if orange == 0 {
		orange = 30
	}
	return red, orange
}

func (r *Runner) checkDispatcherAlerts(ctx context.Context, client *telegram.Client, t store.Tenant, settings store.TenantSettings, statuses []status.DriverStatus, dateStr string) {
	redThreshold, orangeThreshold := thresholds(settings)

	for _, ds := range statuses {
		if ds.Remaining < redThreshold {
			r.sendAlert(ctx, client, t, settings, ds.Driver.ID, "threshold_red", dateStr,
				fmt.Sprintf("⚠️ %s: preostalo samo %d dana (ispod %d).", ds.Driver.Name, ds.Remaining, redThreshold))
		} else if ds.Remaining < orangeThreshold {
			r.sendAlert(ctx, client, t, settings, ds.Driver.ID, "threshold_orange", dateStr,
				fmt.Sprintf("%s: preostalo %d dana (ispod %d).", ds.Driver.Name, ds.Remaining, orangeThreshold))
		}

		if ds.InSchengen && ds.OverBy > 0 {
			r.sendAlert(ctx, client, t, settings, ds.Driver.ID, "overstay", dateStr,
				fmt.Sprintf("⛔ %s je u Šengenu %d dana preko limita (rok je bio %s).", ds.Driver.Name, ds.OverBy, ds.MaxContinuousUntil.Format("02.01.2006")))
		} else if ds.InSchengen && ds.MaxContinuousUntil != nil {
			daysLeft := int(ds.MaxContinuousUntil.Sub(clock.Today()).Hours() / 24)
			if daysLeft <= 3 {
				r.sendAlert(ctx, client, t, settings, ds.Driver.ID, "expiring", dateStr,
					fmt.Sprintf("⏰ %s mora napustiti Šengen do %s (još %d dana).", ds.Driver.Name, ds.MaxContinuousUntil.Format("02.01.2006"), daysLeft))
			}
		}
	}
}

// checkDriverWarnings tells a linked driver in Schengen how long he may
// still stay, using the firm's thresholds. The count is the days left until
// he must leave (MaxContinuousUntil), not "Remaining": days dropping out of
// the window during the trip make the real deadline later, and the
// deadline is what he acts on.
//
// Crossing the orange and the red threshold is told once per trip (keyed
// by the entry date), so a long trip does not bring the same message every
// day. The last 3 days and an overstay are repeated daily, as for the
// dispatcher.
func (r *Runner) checkDriverWarnings(ctx context.Context, client *telegram.Client, t store.Tenant, settings store.TenantSettings, statuses []status.DriverStatus, today time.Time, dateStr string) {
	red, orange := thresholds(settings)
	for _, ds := range statuses {
		chatID := ds.Driver.TelegramChatID
		if !ds.InSchengen || chatID == nil || ds.MaxContinuousUntil == nil {
			continue
		}
		until := ds.MaxContinuousUntil.Format("02.01.2006")
		tripKey := dateStr
		if ds.CurrentSince != nil {
			tripKey = ds.CurrentSince.Format("2006-01-02")
		}
		daysLeft := int(ds.MaxContinuousUntil.Sub(today).Hours() / 24)

		var alertType, key, text string
		switch {
		case ds.OverBy > 0:
			alertType, key = "driver_overstay", dateStr
			text = fmt.Sprintf("⛔ Prešao si limit od 90 dana za %d %s (rok je bio %s). Odmah izađi iz Šengena i javi se dispečeru.", ds.OverBy, daysWord(ds.OverBy), until)
		case daysLeft <= 0:
			alertType, key = "driver_expiring", dateStr
			text = fmt.Sprintf("⏰ Danas (%s) je poslednji dan koji smeš da provedeš u Šengenu. Moraš da izađeš najkasnije danas.", until)
		case daysLeft <= 3:
			alertType, key = "driver_expiring", dateStr
			text = driverDaysLeftMsg("⏰", daysLeft, until)
		case daysLeft < red:
			alertType, key = "driver_red", tripKey
			text = driverDaysLeftMsg("⚠️", daysLeft, until)
		case daysLeft < orange:
			alertType, key = "driver_orange", tripKey
			text = driverDaysLeftMsg("", daysLeft, until)
		default:
			continue
		}
		r.sendOnce(ctx, client, t.ID, ds.Driver.ID, alertType, key, *chatID, text, nil)
	}
}

func driverDaysLeftMsg(icon string, daysLeft int, until string) string {
	msg := fmt.Sprintf("Ostalo ti je %d %s u Šengenu. Moraš da izađeš najkasnije %s.", daysLeft, daysWord(daysLeft), until)
	if icon != "" {
		msg = icon + " " + msg
	}
	return msg
}

// daysWord is "dan" or "dana" for n days: 1, 21, 31 dan; 11 dana.
func daysWord(n int) string {
	if n%10 == 1 && n%100 != 11 {
		return "dan"
	}
	return "dana"
}

func (r *Runner) checkOpenStayReminders(ctx context.Context, client *telegram.Client, t store.Tenant, drivers []store.Driver, allStays []store.Stay, today time.Time, dateStr string) {
	byDriver := make(map[int64]*store.Driver, len(drivers))
	for i := range drivers {
		byDriver[drivers[i].ID] = &drivers[i]
	}
	for _, st := range allStays {
		if st.Exit != nil {
			continue
		}
		driver := byDriver[st.DriverID]
		if driver == nil || driver.TelegramChatID == nil {
			continue
		}
		daysOpen := int(today.Sub(st.Entry).Hours() / 24)
		if daysOpen < 7 {
			continue
		}
		r.sendOnce(ctx, client, t.ID, driver.ID, "open_stay_reminder", dateStr, *driver.TelegramChatID,
			fmt.Sprintf("Da li si još u Šengenu? Ulazak je evidentiran %s, a izlazak još nije.", st.Entry.Format("02.01.2006")),
			telegram.InlineKeyboard(telegram.InlineRow(
				telegram.InlineButton("Da, još sam u Šengenu", "reminder_still_in"),
				telegram.InlineButton("Izašao sam iz Šengena", "reminder_returned"),
			)))
	}
}

func (r *Runner) checkDailyDigest(ctx context.Context, client *telegram.Client, t store.Tenant, settings store.TenantSettings, statuses []status.DriverStatus, today time.Time, dateStr string) {
	if settings.TelegramAlertChatID == 0 {
		return
	}
	now := clock.Now()
	if now.Hour() != 7 {
		return
	}
	var msg string
	if len(statuses) == 0 {
		msg = "Dnevni pregled: nema aktivnih vozača."
	} else {
		msg = fmt.Sprintf("Dnevni pregled (%s):\n", today.Format("02.01.2006"))
		for _, ds := range statuses {
			line := fmt.Sprintf("%s — %d/90, preostalo %d", ds.Driver.Name, ds.UsedDays, ds.Remaining)
			if ds.InSchengen {
				line += " (u Šengenu)"
			}
			msg += line + "\n"
		}
	}
	r.sendOnce(ctx, client, t.ID, 0, "daily_digest", dateStr, settings.TelegramAlertChatID, msg, nil)
}
