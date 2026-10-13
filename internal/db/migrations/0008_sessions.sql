CREATE TABLE sessions (
    token       TEXT PRIMARY KEY,
    tenant_id   INTEGER NOT NULL REFERENCES tenants(id),
    user_id     INTEGER NOT NULL REFERENCES users(id),
    csrf_secret TEXT NOT NULL,
    expires_at  TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_sessions_expires ON sessions(expires_at);
