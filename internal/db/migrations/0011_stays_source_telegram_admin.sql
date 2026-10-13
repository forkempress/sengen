-- The original source CHECK constraint on stays forgot 'telegram_admin'
-- (used when the Telegram admin flow — quick text entry, CSV upload, /dodaj
-- corrections — creates a stay on a driver's behalf). SQLite can't ALTER a
-- CHECK constraint directly, so rebuild the table per the documented
-- 12-step procedure.
ALTER TABLE stays RENAME TO stays_old;

CREATE TABLE stays (
    id          INTEGER PRIMARY KEY,
    tenant_id   INTEGER NOT NULL REFERENCES tenants(id),
    driver_id   INTEGER NOT NULL REFERENCES drivers(id),
    entry_date  TEXT NOT NULL,
    exit_date   TEXT,
    type        TEXT NOT NULL CHECK (type IN ('work', 'private')),
    source      TEXT NOT NULL CHECK (source IN ('manual', 'import', 'telegram_button', 'telegram_location', 'telegram_admin')),
    note        TEXT NOT NULL DEFAULT '',
    created_by  INTEGER REFERENCES users(id),
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (exit_date IS NULL OR exit_date >= entry_date)
);

INSERT INTO stays SELECT * FROM stays_old;
DROP TABLE stays_old;

CREATE INDEX idx_stays_tenant ON stays(tenant_id);
CREATE INDEX idx_stays_driver ON stays(driver_id, entry_date);
CREATE UNIQUE INDEX idx_stays_one_open_per_driver ON stays(driver_id) WHERE exit_date IS NULL;
CREATE INDEX idx_stays_tenant_open ON stays(tenant_id, exit_date);
