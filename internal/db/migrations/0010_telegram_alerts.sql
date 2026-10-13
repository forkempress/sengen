-- Dedup log for Telegram notifications: background jobs insert a row before
-- sending, and skip sending if a row already exists for the same
-- (tenant, driver, alert_type, alert_date) — so a restart or a slow tick
-- never double-sends the same day's alert. driver_id uses 0 as a sentinel
-- for tenant-wide alerts (e.g. the daily digest), not a real driver —
-- deliberately no FK, so the sentinel doesn't need a matching row.
CREATE TABLE telegram_alert_log (
    tenant_id   INTEGER NOT NULL REFERENCES tenants(id),
    driver_id   INTEGER NOT NULL DEFAULT 0,
    alert_type  TEXT NOT NULL,
    alert_date  TEXT NOT NULL, -- 'YYYY-MM-DD', Europe/Belgrade
    sent_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (tenant_id, driver_id, alert_type, alert_date)
);
