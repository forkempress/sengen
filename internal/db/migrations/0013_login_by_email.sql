-- Firms are no longer told apart by the web address: everyone signs in at
-- the same address and the account's email decides which firm it belongs
-- to. Two consequences:
--
-- 1. The column that used to hold the firm's domain is now just a short,
--    unique handle (used by cmd/seed to find an existing firm). RENAME
--    COLUMN rewrites no data and touches no foreign keys.
ALTER TABLE tenants RENAME COLUMN host TO slug;

-- 2. An email must identify exactly one account across all firms,
--    otherwise login could not know which firm to open. If two firms ever
--    shared an address this statement fails and the app refuses to start —
--    deliberately: running with ambiguous logins would be worse.
CREATE UNIQUE INDEX idx_users_email_global ON users(email);
