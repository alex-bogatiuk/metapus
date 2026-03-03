-- +goose Up
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- User preferences: per-user UI settings stored in JSONB columns.
-- One row per user, three logical sections:
--   interface    → theme, language, pageSize, sidebarCollapsed, etc.
--   list_filters → { entityType: FilterValues } — opaque JSON owned by frontend
--   list_columns → { entityType: string[] }     — visible column keys per entity
CREATE TABLE user_preferences (
    user_id      UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    interface    JSONB       NOT NULL DEFAULT '{}',
    list_filters JSONB       NOT NULL DEFAULT '{}',
    list_columns JSONB       NOT NULL DEFAULT '{}',
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (user_id)
);

COMMENT ON TABLE  user_preferences              IS 'Per-user UI preferences (theme, filters, columns)';
COMMENT ON COLUMN user_preferences.interface    IS 'Typed UI settings: theme, language, pageSize, etc.';
COMMENT ON COLUMN user_preferences.list_filters IS 'Saved list filters per entity type (opaque JSON)';
COMMENT ON COLUMN user_preferences.list_columns IS 'Visible column keys per entity type';

-- Reuse the trigger function from 00001_init_extensions.sql
CREATE TRIGGER trg_user_preferences_updated_at
    BEFORE UPDATE ON user_preferences
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
DROP TRIGGER IF EXISTS trg_user_preferences_updated_at ON user_preferences;
DROP TABLE IF EXISTS user_preferences;
