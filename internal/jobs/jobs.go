// Package jobs runs the recurring background checks that don't belong in a
// request handler: driver reminders, dispatcher threshold alerts, and the
// daily digest. One ticker loop scans every tenant each tick; dedup is
// handled by store.TryRecordAlert so a 5-minute tick cadence never produces
// duplicate notifications for the same day.
package jobs

import (
	"context"
	"fmt"
	"log"
	"time"

	"shengen/internal/clock"
	"shengen/internal/status"
	"shengen/internal/store"
	"shengen/internal/telegram"
)

const tickInterval = 5 * time.Minute

type Runner struct {
	Store *store.Store
}

func New(st *store.Store) *Runner {
	return &Runner{Store: st}
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
		}
	}()

	tenants, err := r.Store.ListAllTenants(ctx)
	if err != nil {
		log.Printf("jobs: list tenants: %v", err)
		return
	}
	for _, t := range tenants {
		r.processTenant(ctx, t)
	}
}

func (r *Runner) processTenant(ctx context.Context, t store.Tenant) {
	settings, err := t.ParsedSettings()
	if err != nil || settings.TelegramBotToken == "" {
		return // bot not configured for this tenant yet
	}
	client := telegram.NewClient(settings.TelegramBotToken)
	today := clock.Today()
	dateStr := today.Format("2006-01-02")

	drivers, err := r.Store.ListDrivers(ctx, t.ID, true)
	if err != nil {
		log.Printf("jobs: list drivers for tenant %d: %v", t.ID, err)
		return
	}
	allStays, err := r.Store.ListStaysForTenant(ctx, t.ID)
	if err != nil {
		log.Printf("jobs: list stays for tenant %d: %v", t.ID, err)
		return
	}
	statuses := status.ComputeAll(drivers, allStays, today)

	r.checkDispatcherAlerts(ctx, client, t, settings, statuses, dateStr)
	r.checkOpenStayReminders(ctx, client, t, drivers, allStays, today, dateStr)
	r.checkDailyDigest(ctx, client, t, settings, statuses, today, dateStr)
}

func (r *Runner) sendAlert(ctx context.Context, client *telegram.Client, t store.Tenant, settings store.TenantSettings, driverID int64, alertType, dateStr, text string) {
	if settings.TelegramAlertChatID == 0 {
		return
	}
	sent, err := r.Store.TryRecordAlert(ctx, t.ID, driverID, alertType, dateStr)
	if err != nil {
		log.Printf("jobs: dedup check failed for tenant %d: %v", t.ID, err)
		return
	}
	if !sent {
		return // already notified today
	}
	if err := client.SendMessage(ctx, settings.TelegramAlertChatID, text, nil); err != nil {
		log.Printf("jobs: send alert failed for tenant %d: %v", t.ID, err)
	}
}

func (r *Runner) checkDispatcherAlerts(ctx context.Context, client *telegram.Client, t store.Tenant, settings store.TenantSettings, statuses []status.DriverStatus, dateStr string) {
	redThreshold := settings.ThresholdRedDays
	if redThreshold == 0 {
		redThreshold = 15
	}
	orangeThreshold := settings.ThresholdOrangeDays
	if orangeThreshold == 0 {
		orangeThreshold = 30
	}

	for _, ds := range statuses {
		if ds.Remaining < redThreshold {
			r.sendAlert(ctx, client, t, settings, ds.Driver.ID, "threshold_red", dateStr,
				fmt.Sprintf("⚠️ %s: preostalo samo %d dana (ispod %d).", ds.Driver.Name, ds.Remaining, redThreshold))
		} else if ds.Remaining < orangeThreshold {
			r.sendAlert(ctx, client, t, settings, ds.Driver.ID, "threshold_orange", dateStr,
				fmt.Sprintf("%s: preostalo %d dana (ispod %d).", ds.Driver.Name, ds.Remaining, orangeThreshold))
		}

		if ds.InSchengen && ds.MaxContinuousUntil != nil {
			daysLeft := int(ds.MaxContinuousUntil.Sub(clock.Today()).Hours() / 24)
			if daysLeft <= 3 {
				r.sendAlert(ctx, client, t, settings, ds.Driver.ID, "expiring", dateStr,
					fmt.Sprintf("⏰ %s mora napustiti Šengen do %s (još %d dana).", ds.Driver.Name, ds.MaxContinuousUntil.Format("02.01.2006"), daysLeft))
			}
		}
	}
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
		sent, err := r.Store.TryRecordAlert(ctx, t.ID, driver.ID, "open_stay_reminder", dateStr)
		if err != nil || !sent {
			continue
		}
		client.SendMessage(ctx, *driver.TelegramChatID,
			fmt.Sprintf("Da li si još u EU? Imaš otvoren izlazak iz Srbije od %s.", st.Entry.Format("02.01.2006")),
			telegram.InlineKeyboard(telegram.InlineRow(
				telegram.InlineButton("Da, još sam u EU", "reminder_still_in"),
				telegram.InlineButton("Vratio sam se", "reminder_returned"),
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
	sent, err := r.Store.TryRecordAlert(ctx, t.ID, 0, "daily_digest", dateStr)
	if err != nil || !sent {
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
	client.SendMessage(ctx, settings.TelegramAlertChatID, msg, nil)
}
