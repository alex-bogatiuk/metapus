-- +goose Up
-- A tenant's application is the platform, a base configuration
-- (configuration) and at most one add-on built on it (addon, надстройка).
-- The add-on's migrations have a goose history of their own in the tenant
-- database, so it has its schema version. An empty add-on is a tenant of the
-- base configuration alone; an add-on can be added to it later, never
-- removed or replaced.
ALTER TABLE tenants ADD COLUMN addon VARCHAR(63) NOT NULL DEFAULT '';
ALTER TABLE tenants ADD COLUMN addon_schema_version INT NOT NULL DEFAULT 0;
COMMENT ON COLUMN tenants.addon IS 'Add-on built on the configuration whose migrations the tenant database holds; empty without one';
COMMENT ON COLUMN tenants.addon_schema_version IS 'Highest applied migration of the add-on';

-- +goose Down
ALTER TABLE tenants DROP COLUMN addon_schema_version;
ALTER TABLE tenants DROP COLUMN addon;
