-- A stay's driver must belong to the same firm as the stay. Until now the
-- database only checked that the driver existed somewhere, so a forged CSV
-- import could write tenant A's stay onto tenant B's driver, and the
-- "one open stay per driver" index (on driver_id alone) then blocked firm B
-- from recording anything for its own driver. Tie stays to drivers through
-- (tenant_id, driver_id) and scope the open-stay index the same way.
CREATE UNIQUE INDEX idx_drivers_tenant_id ON drivers(tenant_id, id);

-- Rows that already point at another firm's driver can only come from that
-- forgery. Keep them aside for review instead of carrying them over.
CREATE TABLE stays_cross_tenant AS
    SELECT s.* FROM stays s JOIN drivers d ON d.id = s.driver_id
    WHERE d.tenant_id <> s.tenant_id;

ALTER TABLE stays RENAME TO stays_old;

CREATE TABLE stays (
    id          INTEGER PRIMARY KEY,
    tenant_id   INTEGER NOT NULL REFERENCES tenants(id),
    driver_id   INTEGER NOT NULL,
    entry_date  TEXT NOT NULL,
    exit_date   TEXT,
    type        TEXT NOT NULL CHECK (type IN ('work', 'private')),
    source      TEXT NOT NULL CHECK (source IN ('manual', 'import', 'telegram_button', 'telegram_location', 'telegram_admin')),
    note        TEXT NOT NULL DEFAULT '',
    created_by  INTEGER REFERENCES users(id),
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (exit_date IS NULL OR exit_date >= entry_date),
    FOREIGN KEY (tenant_id, driver_id) REFERENCES drivers(tenant_id, id)
);

INSERT INTO stays SELECT * FROM stays_old WHERE id NOT IN (SELECT id FROM stays_cross_tenant);
DROP TABLE stays_old;

CREATE INDEX idx_stays_tenant ON stays(tenant_id);
CREATE INDEX idx_stays_driver ON stays(driver_id, entry_date);
CREATE UNIQUE INDEX idx_stays_one_open_per_driver ON stays(tenant_id, driver_id) WHERE exit_date IS NULL;
CREATE INDEX idx_stays_tenant_open ON stays(tenant_id, exit_date);
