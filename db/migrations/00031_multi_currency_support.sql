-- Migration: 00031_multi_currency_support.sql
-- Description: Add currency_id to document tables and default_currency_id to warehouses

-- Add currency_id to doc_goods_receipts (replacing currency VARCHAR)
ALTER TABLE doc_goods_receipts
    ADD COLUMN IF NOT EXISTS currency_id UUID REFERENCES cat_currencies(id);

-- Drop the old currency column if exists
ALTER TABLE doc_goods_receipts
    DROP COLUMN IF EXISTS currency;

-- Add currency_id to doc_goods_issues
ALTER TABLE doc_goods_issues
    ADD COLUMN IF NOT EXISTS currency_id UUID REFERENCES cat_currencies(id);

-- Drop the old currency column if exists
ALTER TABLE doc_goods_issues
    DROP COLUMN IF EXISTS currency;

-- Add default_currency_id to cat_warehouses
ALTER TABLE cat_warehouses
    ADD COLUMN IF NOT EXISTS default_currency_id UUID REFERENCES cat_currencies(id);

-- Add base_currency_id and is_default to cat_organizations
ALTER TABLE cat_organizations
    ADD COLUMN IF NOT EXISTS base_currency_id UUID REFERENCES cat_currencies(id),
    ADD COLUMN IF NOT EXISTS is_default BOOLEAN NOT NULL DEFAULT FALSE;

-- Create indexes for foreign keys
CREATE INDEX IF NOT EXISTS idx_doc_goods_receipts_currency_id ON doc_goods_receipts(currency_id);
CREATE INDEX IF NOT EXISTS idx_doc_goods_issues_currency_id ON doc_goods_issues(currency_id);
CREATE INDEX IF NOT EXISTS idx_cat_warehouses_default_currency_id ON cat_warehouses(default_currency_id);
CREATE INDEX IF NOT EXISTS idx_cat_organizations_base_currency_id ON cat_organizations(base_currency_id);

COMMENT ON COLUMN doc_goods_receipts.currency_id IS 'Reference to currency catalog';
COMMENT ON COLUMN doc_goods_issues.currency_id IS 'Reference to currency catalog';
COMMENT ON COLUMN cat_warehouses.default_currency_id IS 'Default currency for documents on this warehouse';
COMMENT ON COLUMN cat_organizations.base_currency_id IS 'Main accounting currency for the organization';
COMMENT ON COLUMN cat_organizations.is_default IS 'Indicates if this is the default organization';
