-- +goose Up
-- +goose StatementBegin

CREATE TABLE sys_report_variants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    dataset_key VARCHAR(255) NOT NULL,
    name VARCHAR(255) NOT NULL,
    author_id UUID REFERENCES users(id) ON DELETE SET NULL,
    visibility VARCHAR(50) NOT NULL CHECK (visibility IN ('personal', 'shared', 'system')),
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    
    -- CDC 
    deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
    _deleted_at TIMESTAMPTZ,
    _txid BIGINT DEFAULT txid_current(),
    version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_sys_report_variants_dataset_key ON sys_report_variants(dataset_key);
CREATE INDEX idx_sys_report_variants_visibility ON sys_report_variants(visibility);
CREATE INDEX idx_sys_report_variants_author_id ON sys_report_variants(author_id);

-- Triggers for CDC
CREATE TRIGGER trg_sys_report_variants_update_txid
    BEFORE UPDATE ON sys_report_variants
    FOR EACH ROW
    EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_sys_report_variants_soft_delete
    BEFORE UPDATE ON sys_report_variants
    FOR EACH ROW
    EXECUTE FUNCTION soft_delete_with_timestamp();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE sys_report_variants;
-- +goose StatementEnd
