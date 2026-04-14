-- +goose Up
-- sys_settings: single-row tenant-level configuration (analogous to 1C "Constants")
-- Stores organization info, accounting parameters, and performance tuning.

CREATE TABLE sys_settings (
    singleton    BOOLEAN      PRIMARY KEY DEFAULT TRUE CHECK (singleton = TRUE),
    organization JSONB        NOT NULL DEFAULT '{}',
    accounting   JSONB        NOT NULL DEFAULT '{}',
    performance  JSONB        NOT NULL DEFAULT '{"batchConcurrency": 5}',
    version      INT          NOT NULL DEFAULT 1,
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_by   UUID
);

COMMENT ON TABLE  sys_settings              IS 'Single-row tenant settings (1C Constants analogue)';
COMMENT ON COLUMN sys_settings.singleton    IS 'Always TRUE — enforces exactly one row via CHECK constraint';
COMMENT ON COLUMN sys_settings.organization IS 'Company name, INN, KPP, addresses, contacts';
COMMENT ON COLUMN sys_settings.accounting   IS 'Tax system, VAT, inventory method, numbering';
COMMENT ON COLUMN sys_settings.performance  IS 'Batch concurrency, processing limits';
COMMENT ON COLUMN sys_settings.version      IS 'Optimistic locking version — incremented on each update';

-- Seed the single row with defaults
INSERT INTO sys_settings (singleton) VALUES (TRUE);

-- +goose Down
DROP TABLE IF EXISTS sys_settings;
