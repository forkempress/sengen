-- Which version of the terms of use and the data processing agreement a firm
-- accepted, when, and by which of its users. Empty version = not accepted
-- yet (every firm created before this migration): its owner is asked at the
-- next sign-in.
ALTER TABLE tenants ADD COLUMN terms_version TEXT NOT NULL DEFAULT '';
ALTER TABLE tenants ADD COLUMN terms_accepted_at TEXT;
ALTER TABLE tenants ADD COLUMN terms_accepted_by INTEGER REFERENCES users(id);

-- When a driver confirmed, in the bot, that he read the notice about his
-- data. NULL = not confirmed (also for every driver linked before this
-- migration); linking a Telegram account again clears it.
ALTER TABLE drivers ADD COLUMN privacy_ack_at TEXT;
