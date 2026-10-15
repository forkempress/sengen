-- Every scanned history (PDF, photo) sent to Claude on the import page:
-- what it cost and what came of it, for the superadmin notifications.
--   status         'ok' (stays found) | 'empty' (none found) | 'too_large'
--                  (over the driver/stay limits) | 'error' (the call failed)
--   model          the model that answered (a fallback may answer instead)
--   cost_microusd  what the call cost, in millionths of a US dollar
--   stays_read, drivers_read, rows_valid  what the preview showed
--   confirmed_at, stays_written, drivers_written  set when the user
--                  confirms the import; NULL while they have not
CREATE TABLE document_imports (
    id              INTEGER PRIMARY KEY,
    tenant_id       INTEGER NOT NULL REFERENCES tenants(id),
    user_id         INTEGER REFERENCES users(id),
    status          TEXT NOT NULL CHECK (status IN ('ok', 'empty', 'too_large', 'error')),
    model           TEXT NOT NULL DEFAULT '',
    input_tokens    INTEGER NOT NULL DEFAULT 0,
    output_tokens   INTEGER NOT NULL DEFAULT 0,
    cost_microusd   INTEGER NOT NULL DEFAULT 0,
    stays_read      INTEGER NOT NULL DEFAULT 0,
    drivers_read    INTEGER NOT NULL DEFAULT 0,
    rows_valid      INTEGER NOT NULL DEFAULT 0,
    created_at      TEXT NOT NULL,
    confirmed_at    TEXT,
    stays_written   INTEGER NOT NULL DEFAULT 0,
    drivers_written INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_document_imports_created ON document_imports(created_at);
CREATE INDEX idx_document_imports_confirmed ON document_imports(confirmed_at);
CREATE INDEX idx_document_imports_tenant ON document_imports(tenant_id, created_at);
