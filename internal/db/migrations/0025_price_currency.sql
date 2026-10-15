-- A firm's price per active driver per month, in its own currency:
--   price_per_driver_minor: in the currency's minor unit (para or cent),
--     so 30000 is 300 din and 350 is €3,50.
--   price_currency: 'RSD' (default) or 'EUR'.
-- It replaces price_per_driver_cents (euro cents, €2 by default); every
-- existing firm moves to the new default, 300 RSD.
ALTER TABLE tenants ADD COLUMN price_per_driver_minor INTEGER NOT NULL DEFAULT 30000 CHECK (price_per_driver_minor >= 0);
ALTER TABLE tenants ADD COLUMN price_currency TEXT NOT NULL DEFAULT 'RSD' CHECK (price_currency IN ('RSD', 'EUR'));
ALTER TABLE tenants DROP COLUMN price_per_driver_cents;
