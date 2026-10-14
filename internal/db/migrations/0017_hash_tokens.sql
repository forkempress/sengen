-- Session and Telegram link tokens are now stored as their SHA-256 hash, so
-- a leaked database copy cannot be used to take over a session or redeem a
-- link. Existing rows hold plain tokens and SQLite cannot hash them, so they
-- are dropped: everyone signs in once more, and unused links are issued again.
DELETE FROM sessions;
ALTER TABLE sessions RENAME COLUMN token TO token_hash;
-- Idle expiry: a session unused for 24 hours ends even before its 14 days.
ALTER TABLE sessions ADD COLUMN last_seen_at TEXT NOT NULL DEFAULT '';

DELETE FROM telegram_link_tokens;
ALTER TABLE telegram_link_tokens RENAME COLUMN token TO token_hash;
CREATE INDEX idx_telegram_link_tokens_expires ON telegram_link_tokens(expires_at);
