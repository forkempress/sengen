CREATE TABLE events (
    id           INTEGER PRIMARY KEY,
    tenant_id    INTEGER NOT NULL REFERENCES tenants(id),
    driver_id    INTEGER NOT NULL REFERENCES drivers(id),
    kind         TEXT NOT NULL CHECK (kind IN ('exit_serbia', 'enter_serbia', 'location_ping')),
    occurred_at  TEXT NOT NULL,                -- ISO8601 UTC timestamp
    lat          REAL,
    lng          REAL,
    country_code TEXT,                         -- ISO 3166-1 alpha-2, resolved via point-in-polygon
    raw          TEXT NOT NULL DEFAULT '{}',   -- raw Telegram update JSON, for debugging/replay
    processed    INTEGER NOT NULL DEFAULT 0 CHECK (processed IN (0, 1)),
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_events_tenant ON events(tenant_id);
CREATE INDEX idx_events_driver ON events(driver_id, occurred_at);
CREATE INDEX idx_events_unprocessed ON events(processed) WHERE processed = 0;
