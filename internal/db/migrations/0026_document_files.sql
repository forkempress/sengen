-- The scan behind a document import, kept 30 days so another model can be
-- tested on it against what the firm ended up with in the app (cmd/doceval).
-- Only reads that found stays are kept.
--   driver_names      JSON array of the names the model was given
--   single_driver_id  set when read from a driver's page: every row went on him
CREATE TABLE document_files (
    document_import_id INTEGER PRIMARY KEY REFERENCES document_imports(id),
    tenant_id          INTEGER NOT NULL REFERENCES tenants(id),
    media_type         TEXT NOT NULL,
    data               BLOB NOT NULL,
    driver_names       TEXT NOT NULL,
    single_driver_id   INTEGER,
    created_at         TEXT NOT NULL
);

CREATE INDEX idx_document_files_created ON document_files(created_at);

-- Set when the import is confirmed: which drivers got stays (",3,7,", so a
-- driver can be matched with LIKE) and the period the written stays cover.
ALTER TABLE document_imports ADD COLUMN written_driver_ids TEXT NOT NULL DEFAULT '';
ALTER TABLE document_imports ADD COLUMN period_from TEXT;
ALTER TABLE document_imports ADD COLUMN period_to TEXT;
