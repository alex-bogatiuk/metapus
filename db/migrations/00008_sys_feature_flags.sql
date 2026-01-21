-- +goose Up
-- Description: Feature flags storage for runtime configuration

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

CREATE TABLE sys_feature_flags (
                                   id            UUID        PRIMARY KEY DEFAULT gen_random_uuid_v7(),
                                   flag_name     VARCHAR(100) NOT NULL,
                                   description   TEXT,
                                   is_enabled    BOOLEAN     NOT NULL DEFAULT FALSE,
                                   variant       VARCHAR(50),
                                   config        JSONB,

                                   valid_from    TIMESTAMPTZ,
                                   valid_until   TIMESTAMPTZ,
                                   created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
                                   updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
                                   created_by    VARCHAR(50)
);

-- Уникальность: один флаг на имя (DB-per-tenant => отдельная БД на тенанта)
CREATE UNIQUE INDEX idx_feature_flag_unique
    ON sys_feature_flags (flag_name);
CREATE TRIGGER trg_sys_feature_flags_updated_at
    BEFORE UPDATE ON sys_feature_flags
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Notification trigger for feature flags cache invalidation
-- (squashed from 00017_sys_feature_flags_notify.sql)
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION notify_feature_flags_change()
RETURNS TRIGGER AS $func$
BEGIN
    PERFORM pg_notify('feature_flags_changed', COALESCE(NEW.flag_name, OLD.flag_name));
    RETURN COALESCE(NEW, OLD);
END;
$func$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_feature_flags_notify
    AFTER INSERT OR UPDATE OR DELETE ON sys_feature_flags
    FOR EACH ROW
    EXECUTE FUNCTION notify_feature_flags_change();

COMMENT ON FUNCTION notify_feature_flags_change() IS 'Sends NOTIFY on feature flag changes for cache invalidation';

-- Начальные флаги
INSERT INTO sys_feature_flags (flag_name, description, is_enabled) VALUES
                                                                       ('new_posting_algorithm', 'Use new posting algorithm with line_id tracking', FALSE),
                                                                       ('async_posting', 'Enable asynchronous document posting', FALSE),
                                                                       ('advanced_reports', 'Enable advanced reporting features', FALSE),
                                                                       ('beta_ui', 'Enable beta UI features', FALSE)
ON CONFLICT DO NOTHING;

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
DROP TRIGGER IF EXISTS trg_feature_flags_notify ON sys_feature_flags;
DROP FUNCTION IF EXISTS notify_feature_flags_change();
DROP TRIGGER IF EXISTS trg_sys_feature_flags_updated_at ON sys_feature_flags;
DROP TABLE IF EXISTS sys_feature_flags;