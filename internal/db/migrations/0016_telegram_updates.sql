-- Telegram retries a webhook delivery it thinks failed (slow reply, restart),
-- so the same update can arrive twice. Each update_id is recorded once; a
-- second arrival is skipped. Rows older than a few days are deleted by the
-- daily cleanup job (Telegram never redelivers after 24 hours).
CREATE TABLE telegram_updates (
    update_id   INTEGER PRIMARY KEY,
    received_at TEXT NOT NULL
);
CREATE INDEX idx_telegram_updates_received_at ON telegram_updates(received_at);
