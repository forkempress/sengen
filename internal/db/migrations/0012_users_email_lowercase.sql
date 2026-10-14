-- Login has always lower-cased the typed address, but accounts were stored
-- exactly as typed (e.g. by cmd/seed), so an owner seeded as "Ja@Firma.rs"
-- could never log in. Bring existing rows in line with store.NormalizeEmail.
--
-- A row whose normalised form already belongs to another account of the same
-- tenant is left alone: rewriting it would violate UNIQUE(tenant_id, email)
-- and fail the migration, which would stop the app from starting.
UPDATE users
SET email = lower(trim(email))
WHERE email <> lower(trim(email))
  AND NOT EXISTS (
    SELECT 1 FROM users AS other
    WHERE other.tenant_id = users.tenant_id
      AND other.email = lower(trim(users.email))
  );
