CREATE TABLE drivers (
    id               INTEGER PRIMARY KEY,
    tenant_id        INTEGER NOT NULL REFERENCES tenants(id),
    name             TEXT NOT NULL,
    vehicle_label    TEXT NOT NULL DEFAULT '',
    telegram_chat_id INTEGER,
    active           INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
    created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_drivers_tenant ON drivers(tenant_id);
CREATE UNIQUE INDEX idx_drivers_telegram_chat ON drivers(telegram_chat_id) WHERE telegram_chat_id IS NOT NULL;
