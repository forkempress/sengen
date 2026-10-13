CREATE TABLE telegram_link_tokens (
    token      TEXT PRIMARY KEY,
    tenant_id  INTEGER NOT NULL REFERENCES tenants(id),
    driver_id  INTEGER REFERENCES drivers(id),  -- NULL for admin (owner/dispatcher) link tokens
    user_id    INTEGER REFERENCES users(id),    -- set when linking an admin Telegram account instead of a driver
    expires_at TEXT NOT NULL,
    used_at    TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK ((driver_id IS NOT NULL AND user_id IS NULL) OR (driver_id IS NULL AND user_id IS NOT NULL))
);

CREATE INDEX idx_telegram_link_tokens_tenant ON telegram_link_tokens(tenant_id);
