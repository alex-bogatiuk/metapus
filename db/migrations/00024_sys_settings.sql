-- +goose Up
-- sys_settings: single-row tenant-level configuration.
-- Only system-wide settings remain here.
-- Organization-specific settings are in cat_organizations.

CREATE TABLE sys_settings (
    singleton    BOOLEAN      PRIMARY KEY DEFAULT TRUE CHECK (singleton = TRUE),
    numbering    JSONB        NOT NULL DEFAULT '{"autoNumbering": true, "numberPrefix": ""}',
    performance  JSONB        NOT NULL DEFAULT '{"batchConcurrency": 5}',
    version      INT          NOT NULL DEFAULT 1,
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_by   UUID
);

COMMENT ON TABLE  sys_settings              IS 'Single-row tenant settings (system-wide only)';
COMMENT ON COLUMN sys_settings.singleton    IS 'Always TRUE — enforces exactly one row via CHECK constraint';
COMMENT ON COLUMN sys_settings.numbering    IS 'Document numbering: autoNumbering, numberPrefix';
COMMENT ON COLUMN sys_settings.performance  IS 'Batch concurrency, processing limits';
COMMENT ON COLUMN sys_settings.version      IS 'Optimistic locking version — incremented on each update';

-- Seed the single row with defaults
INSERT INTO sys_settings (singleton) VALUES (TRUE);

-- +goose Down
DROP TABLE IF EXISTS sys_settings;
