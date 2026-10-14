-- Billing fields the superadmin bot reports on (/mrr, /trial). Read-only in
-- v1: cmd/seed sets them, nothing in the app changes them yet.
--   plan: 'trial' until the firm pays, 'paid' while it does, 'paused' after.
--   trial_until: last trial day, 'YYYY-MM-DD' (Europe/Belgrade).
--   price_per_driver_cents: monthly price per active driver, in euro cents.
-- Firms that already exist start a 30-day trial counted from their creation.
ALTER TABLE tenants ADD COLUMN plan TEXT NOT NULL DEFAULT 'trial' CHECK (plan IN ('trial', 'paid', 'paused'));
ALTER TABLE tenants ADD COLUMN trial_until TEXT;
ALTER TABLE tenants ADD COLUMN price_per_driver_cents INTEGER NOT NULL DEFAULT 200 CHECK (price_per_driver_cents >= 0);

UPDATE tenants SET trial_until = date(created_at, '+30 days');

-- Every superadmin command, allowed or refused: who (Telegram user id), when,
-- what. Separate from audit_log, which belongs to one firm.
CREATE TABLE superadmin_audit_log (
    id               INTEGER PRIMARY KEY,
    telegram_user_id INTEGER NOT NULL,
    command          TEXT NOT NULL,
    args             TEXT NOT NULL DEFAULT '',
    allowed          INTEGER NOT NULL CHECK (allowed IN (0, 1)),
    occurred_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_superadmin_audit_log_time ON superadmin_audit_log(occurred_at);

-- Dedup for push notifications to superadmins: a row is inserted before
-- sending, so each (kind, ref) goes out once — e.g. ('new_tenant', '12') or
-- ('digest', '2026-10-14').
CREATE TABLE superadmin_notifications (
    kind    TEXT NOT NULL,
    ref     TEXT NOT NULL,
    sent_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (kind, ref)
);
