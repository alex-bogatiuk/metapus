-- +goose Up
-- +goose StatementBegin

CREATE TABLE sys_list_views (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    entity_type VARCHAR(255) NOT NULL,
    name VARCHAR(255) NOT NULL,
    author_id UUID REFERENCES users(id) ON DELETE SET NULL,
    visibility VARCHAR(50) NOT NULL CHECK (visibility IN ('personal', 'shared', 'system')),
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    sort_order INT NOT NULL DEFAULT 0,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,

    -- CDC
    deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
    _deleted_at TIMESTAMPTZ,
    _txid BIGINT DEFAULT txid_current(),
    version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_sys_list_views_entity_type ON sys_list_views(entity_type);
CREATE INDEX idx_sys_list_views_author_id ON sys_list_views(author_id);
CREATE INDEX idx_sys_list_views_visibility ON sys_list_views(visibility);

-- Triggers for CDC
CREATE TRIGGER trg_sys_list_views_update_txid
    BEFORE UPDATE ON sys_list_views
    FOR EACH ROW
    EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_sys_list_views_soft_delete
    BEFORE UPDATE ON sys_list_views
    FOR EACH ROW
    EXECUTE FUNCTION soft_delete_with_timestamp();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE sys_list_views;
-- +goose StatementEnd
