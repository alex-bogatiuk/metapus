-- +goose Up
-- A tenant database holds the migrations of one configuration for life. The
-- platform's and the configuration's migrations have goose histories of their
-- own, so each has its schema version. An empty configuration marks a tenant
-- that no binary recording it has migrated yet; its first migration records it.
ALTER TABLE tenants ADD COLUMN configuration VARCHAR(63) NOT NULL DEFAULT '';
ALTER TABLE tenants ADD COLUMN configuration_schema_version INT NOT NULL DEFAULT 0;
COMMENT ON COLUMN tenants.schema_version IS 'Highest applied platform migration';
COMMENT ON COLUMN tenants.configuration IS 'Configuration whose migrations the tenant database holds';
COMMENT ON COLUMN tenants.configuration_schema_version IS 'Highest applied migration of the configuration';

-- +goose Down
ALTER TABLE tenants DROP COLUMN configuration_schema_version;
ALTER TABLE tenants DROP COLUMN configuration;
