-- +goose Up
-- sys_automation_files: temporary storage for generated report files.
-- Files are created during report generation (reaction_type = 'generate_report')
-- and cleaned up by the worker after expiration (default 24h).

CREATE TABLE sys_automation_files (
    id            UUID          PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    rule_id       UUID          NOT NULL REFERENCES sys_automation_rules(id) ON DELETE CASCADE,
    file_name     VARCHAR(255)  NOT NULL,
    mime_type     VARCHAR(100)  NOT NULL DEFAULT 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    file_data     BYTEA         NOT NULL,
    file_size     INT           NOT NULL,
    row_count     INT           NOT NULL DEFAULT 0,
    metadata      JSONB         NOT NULL DEFAULT '{}',
    -- { "datasetKey", "periodFrom", "periodTo", "variantName" }

    expires_at    TIMESTAMPTZ   NOT NULL,
    created_at    TIMESTAMPTZ   NOT NULL DEFAULT statement_timestamp()
);

-- Cleanup worker queries expired files
CREATE INDEX idx_sys_auto_files_expires ON sys_automation_files(expires_at);

-- List files by rule for debugging/monitoring
CREATE INDEX idx_sys_auto_files_rule ON sys_automation_files(rule_id);

COMMENT ON TABLE  sys_automation_files           IS 'Temporary storage for generated report files. Cleaned up by worker after 24h.';
COMMENT ON COLUMN sys_automation_files.file_data IS 'Raw binary file content (XLSX, CSV)';
COMMENT ON COLUMN sys_automation_files.metadata  IS 'Report generation context: dataset key, period, variant name';

-- +goose Down
DROP TABLE IF EXISTS sys_automation_files;
