-- One-time links (and, for sign-up, codes) sent by email: confirming a new
-- firm's address, resetting a password, inviting a user. Built like
-- telegram_link_tokens: only the SHA-256 of the token is kept, issuing a new
-- one voids the earlier unused ones, and redeeming is one guarded UPDATE.
--   purpose  'signup' | 'reset' | 'invite'
--   email    normalised address the message went to
--   tenant_id, user_id  the firm / account it is about (reset, invite)
--   code_hash, code_tries, browser_hash  sign-up only: the 6-digit code typed
--            in the tab that started the sign-up, and that tab's cookie
--   payload  JSON the token carries until it is redeemed (sign-up form,
--            invited name and role)
CREATE TABLE email_tokens (
    token_hash   TEXT PRIMARY KEY,
    purpose      TEXT NOT NULL CHECK (purpose IN ('signup', 'reset', 'invite')),
    email        TEXT NOT NULL,
    tenant_id    INTEGER REFERENCES tenants(id),
    user_id      INTEGER REFERENCES users(id),
    code_hash    TEXT,
    code_tries   INTEGER NOT NULL DEFAULT 0,
    browser_hash TEXT,
    payload      TEXT NOT NULL DEFAULT '{}',
    expires_at   TEXT NOT NULL,
    used_at      TEXT,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_email_tokens_email ON email_tokens(purpose, email);
CREATE INDEX idx_email_tokens_tenant ON email_tokens(tenant_id, purpose);
CREATE INDEX idx_email_tokens_expires ON email_tokens(expires_at);

-- Every message the app tried to send: for support ("the email never came"),
-- for the daily sending cap and, later, to send each reminder once.
-- status: 'sent' | 'failed' | 'over_limit'. Kept 90 days (jobs.Cleanup).
CREATE TABLE email_log (
    id         INTEGER PRIMARY KEY,
    tag        TEXT NOT NULL,
    tenant_id  INTEGER REFERENCES tenants(id),
    recipient  TEXT NOT NULL,
    status     TEXT NOT NULL CHECK (status IN ('sent', 'failed', 'over_limit')),
    error      TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_email_log_time ON email_log(created_at);
