-- A user who leaves the firm is deactivated rather than deleted: audit_log
-- and stays.created_by keep pointing at the row. A deactivated user cannot
-- sign in, and the bot no longer answers his Telegram.
ALTER TABLE users ADD COLUMN active INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1));
