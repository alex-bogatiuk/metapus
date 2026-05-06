-- +goose Up
-- sys_settings: single-row tenant-level configuration.
-- Only system-wide settings remain here.
-- Organization-specific settings are in cat_organizations.

CREATE TABLE sys_settings (
    singleton    BOOLEAN      PRIMARY KEY DEFAULT TRUE CHECK (singleton = TRUE),

    -- General
    general      JSONB        NOT NULL DEFAULT '{"timezone": "UTC"}',
    numbering    JSONB        NOT NULL DEFAULT '{"autoNumbering": true, "numberPrefix": ""}',
    performance  JSONB        NOT NULL DEFAULT '{"batchConcurrency": 5}',

    -- Module-scoped
    warehouse    JSONB        NOT NULL DEFAULT '{"inventoryMethod": "fifo", "negativeStockControl": true, "autoPostReceipts": false}',
    sales        JSONB        NOT NULL DEFAULT '{"defaultPaymentTermDays": 30, "autoReserveStock": false}',
    purchasing   JSONB        NOT NULL DEFAULT '{"defaultPaymentTermDays": 30, "requireApproval": false}',

    -- Metadata
    version      INT          NOT NULL DEFAULT 1,
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_by   UUID
);

COMMENT ON TABLE  sys_settings              IS 'Single-row tenant settings (system-wide only)';
COMMENT ON COLUMN sys_settings.singleton    IS 'Always TRUE — enforces exactly one row via CHECK constraint';
COMMENT ON COLUMN sys_settings.general      IS 'General settings: timezone (IANA), display preferences';
COMMENT ON COLUMN sys_settings.numbering    IS 'Document numbering: autoNumbering, numberPrefix';
COMMENT ON COLUMN sys_settings.performance  IS 'Batch concurrency, processing limits';
COMMENT ON COLUMN sys_settings.warehouse    IS 'Warehouse module: inventory method, stock control';
COMMENT ON COLUMN sys_settings.sales        IS 'Sales module: payment terms, stock reservation';
COMMENT ON COLUMN sys_settings.purchasing   IS 'Purchasing module: payment terms, approval workflow';
COMMENT ON COLUMN sys_settings.version      IS 'Optimistic locking version — incremented on each update';

-- Seed the single row with defaults
INSERT INTO sys_settings (singleton) VALUES (TRUE);

-- +goose Down
DROP TABLE IF EXISTS sys_settings;
