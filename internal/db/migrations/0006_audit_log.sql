CREATE TABLE audit_log (
    id          INTEGER PRIMARY KEY,
    tenant_id   INTEGER NOT NULL REFERENCES tenants(id),
    actor_type  TEXT NOT NULL CHECK (actor_type IN ('user', 'telegram_admin', 'telegram_driver', 'system')),
    actor_id    INTEGER,                        -- users.id or drivers.id, depending on actor_type; NULL for system
    entity      TEXT NOT NULL,                  -- e.g. 'stay', 'driver', 'user'
    entity_id   INTEGER NOT NULL,
    action      TEXT NOT NULL,                  -- e.g. 'create', 'update', 'delete'
    before_json TEXT,
    after_json  TEXT,
    occurred_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_audit_log_tenant ON audit_log(tenant_id, occurred_at);
CREATE INDEX idx_audit_log_entity ON audit_log(entity, entity_id);
