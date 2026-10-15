-- Planned trips: when a dispatcher intends to send a driver into Schengen.
-- Kept apart from stays, which record only days that actually happened (and
-- refuse future dates): a plan never counts as a real day, it only joins the
-- stays in projections. Whether a plan was realized is derived from the
-- stays each time, so editing or deleting a stay can never leave it stale.
CREATE TABLE planned_stays (
    id          INTEGER PRIMARY KEY,
    tenant_id   INTEGER NOT NULL REFERENCES tenants(id),
    driver_id   INTEGER NOT NULL,
    start_date  TEXT NOT NULL,                 -- 'YYYY-MM-DD', first day in Schengen
    end_date    TEXT NOT NULL,                 -- 'YYYY-MM-DD', last day in Schengen
    note        TEXT NOT NULL DEFAULT '',
    created_by  INTEGER REFERENCES users(id),
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (end_date >= start_date),
    CHECK (julianday(end_date) - julianday(start_date) < 90),
    FOREIGN KEY (tenant_id, driver_id) REFERENCES drivers(tenant_id, id)
);

CREATE INDEX idx_planned_stays_driver ON planned_stays(tenant_id, driver_id, start_date);
