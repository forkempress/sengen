-- The price is in dinars: 300 RSD per active driver per month, in whole
-- dinars. It replaces price_per_driver_cents (euro cents, €2 by default);
-- every existing firm moves to the new price.
ALTER TABLE tenants ADD COLUMN price_per_driver_rsd INTEGER NOT NULL DEFAULT 300 CHECK (price_per_driver_rsd >= 0);
ALTER TABLE tenants DROP COLUMN price_per_driver_cents;
