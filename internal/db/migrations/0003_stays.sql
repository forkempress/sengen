CREATE TABLE stays (
    id          INTEGER PRIMARY KEY,
    tenant_id   INTEGER NOT NULL REFERENCES tenants(id),
    driver_id   INTEGER NOT NULL REFERENCES drivers(id),
    entry_date  TEXT NOT NULL,                 -- 'YYYY-MM-DD', local date Europe/Belgrade
    exit_date   TEXT,                          -- NULL = currently in Schengen (open stay)
    type        TEXT NOT NULL CHECK (type IN ('work', 'private')),
    source      TEXT NOT NULL CHECK (source IN ('manual', 'import', 'telegram_button', 'telegram_location')),
    note        TEXT NOT NULL DEFAULT '',
    created_by  INTEGER REFERENCES users(id),
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (exit_date IS NULL OR exit_date >= entry_date)
);

CREATE INDEX idx_stays_tenant ON stays(tenant_id);
CREATE INDEX idx_stays_driver ON stays(driver_id, entry_date);

-- At most one open stay (exit_date IS NULL) per driver.
CREATE UNIQUE INDEX idx_stays_one_open_per_driver ON stays(driver_id) WHERE exit_date IS NULL;

-- Overlap between a driver's stays and "no future dates" are validated in the
-- application layer (internal/store), since SQLite cannot express range-overlap
-- constraints declaratively.
