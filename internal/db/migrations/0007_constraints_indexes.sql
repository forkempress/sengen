-- Supplementary indexes for common dashboard queries.
CREATE INDEX idx_drivers_tenant_active ON drivers(tenant_id, active);
CREATE INDEX idx_stays_tenant_open ON stays(tenant_id, exit_date);
