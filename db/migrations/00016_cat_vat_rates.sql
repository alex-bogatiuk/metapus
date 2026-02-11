-- +goose Up
-- Description: VATRate catalog (Справочник "Ставки НДС")

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

CREATE TABLE cat_vat_rates (
    -- Base catalog fields
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),

    deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
    version INT NOT NULL DEFAULT 1,
    attributes JSONB DEFAULT '{}',

    -- CDC-ready columns
    _deleted_at TIMESTAMPTZ,
    _txid BIGINT DEFAULT txid_current(),

    -- Audit fields
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Catalog fields
    code VARCHAR(20) NOT NULL,
    name VARCHAR(100) NOT NULL,
    parent_id UUID,
    is_folder BOOLEAN NOT NULL DEFAULT FALSE,

    -- VATRate specific fields
    rate NUMERIC(5,2) NOT NULL DEFAULT 0,
    is_tax_exempt BOOLEAN NOT NULL DEFAULT FALSE,
    description TEXT
);

-- Indexes
CREATE UNIQUE INDEX uq_cat_vat_rates_code ON cat_vat_rates (code) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_vat_rates_rate ON cat_vat_rates (rate) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_vat_rates_parent ON cat_vat_rates (parent_id) WHERE parent_id IS NOT NULL;

-- CDC index & triggers
CREATE INDEX IF NOT EXISTS idx_cat_vat_rates_txid
    ON cat_vat_rates (_txid) WHERE _deleted_at IS NULL;

CREATE TRIGGER trg_cat_vat_rates_txid
    BEFORE UPDATE ON cat_vat_rates
    FOR EACH ROW
    EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_cat_vat_rates_soft_delete
    BEFORE UPDATE OF deletion_mark ON cat_vat_rates
    FOR EACH ROW
    EXECUTE FUNCTION soft_delete_with_timestamp();

COMMENT ON TABLE cat_vat_rates IS 'Справочник Ставки НДС';

-- Seed default VAT rates
INSERT INTO cat_vat_rates (code, name, rate, is_tax_exempt) VALUES
    ('VR-001', 'НДС 20%', 20, FALSE),
    ('VR-002', 'НДС 10%', 10, FALSE),
    ('VR-003', 'НДС 0%', 0, FALSE),
    ('VR-004', 'Без НДС', 0, TRUE);

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

DROP TRIGGER IF EXISTS trg_cat_vat_rates_soft_delete ON cat_vat_rates;
DROP TRIGGER IF EXISTS trg_cat_vat_rates_txid ON cat_vat_rates;
DROP TABLE IF EXISTS cat_vat_rates;

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
