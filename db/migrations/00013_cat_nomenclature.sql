-- Migration: Create Nomenclature catalog
-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

CREATE TABLE IF NOT EXISTS cat_nomenclature (
    -- Base fields (from entity.Base)
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),

    deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
    version INT NOT NULL DEFAULT 1,
    attributes JSONB DEFAULT '{}',

    -- CDC-ready columns
    _deleted_at TIMESTAMPTZ,
    _txid BIGINT DEFAULT txid_current(),
    
    -- Catalog fields (from entity.Catalog)
    code VARCHAR(50) NOT NULL,
    name VARCHAR(255) NOT NULL,
    parent_id UUID REFERENCES cat_nomenclature(id),
    is_folder BOOLEAN NOT NULL DEFAULT FALSE,
    
    -- Nomenclature-specific fields
    type VARCHAR(20) NOT NULL DEFAULT 'goods',
    article VARCHAR(100),
    barcode VARCHAR(50),
    base_unit_id UUID,
    vat_rate VARCHAR(5) NOT NULL DEFAULT '20',
    weight NUMERIC(15,4) DEFAULT 0,
    volume NUMERIC(15,6) DEFAULT 0,
    description TEXT,
    manufacturer_id UUID REFERENCES cat_counterparties(id),
    country_of_origin VARCHAR(2),
    is_weighed BOOLEAN NOT NULL DEFAULT FALSE,
    track_serial BOOLEAN NOT NULL DEFAULT FALSE,
    track_batch BOOLEAN NOT NULL DEFAULT FALSE,
    image_url TEXT,
    
    -- Constraints
    CONSTRAINT chk_nomenclature_type CHECK (type IN ('goods', 'service', 'work', 'material', 'semi', 'product')),
    CONSTRAINT chk_vat_rate CHECK (vat_rate IN ('0', '10', '20')),
    CONSTRAINT chk_weight_positive CHECK (weight >= 0),
    CONSTRAINT chk_volume_positive CHECK (volume >= 0)
);

-- Indexes
-- Unique code within tenant (excluding soft-deleted)
CREATE UNIQUE INDEX idx_cat_nomenclature_code 
    ON cat_nomenclature (code) 
    WHERE deletion_mark = FALSE;

-- Unique article within tenant (excluding soft-deleted)
CREATE UNIQUE INDEX idx_cat_nomenclature_article 
    ON cat_nomenclature (article) 
    WHERE deletion_mark = FALSE AND article IS NOT NULL AND article != '';

-- Unique barcode within tenant (excluding soft-deleted)
CREATE UNIQUE INDEX idx_cat_nomenclature_barcode 
    ON cat_nomenclature (barcode) 
    WHERE deletion_mark = FALSE AND barcode IS NOT NULL AND barcode != '';

-- Search indexes
CREATE INDEX idx_cat_nomenclature_name ON cat_nomenclature USING gin (name gin_trgm_ops);
CREATE INDEX idx_cat_nomenclature_type ON cat_nomenclature (type) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_nomenclature_parent ON cat_nomenclature (parent_id) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_nomenclature_manufacturer ON cat_nomenclature (manufacturer_id) WHERE manufacturer_id IS NOT NULL;

-- JSONB GIN index for custom fields search
CREATE INDEX idx_cat_nomenclature_attrs ON cat_nomenclature USING gin (attributes);

-- CDC indexes & triggers
CREATE INDEX IF NOT EXISTS idx_cat_nomenclature_txid
    ON cat_nomenclature (_txid) WHERE _deleted_at IS NULL;

CREATE TRIGGER trg_cat_nomenclature_txid
    BEFORE UPDATE ON cat_nomenclature
    FOR EACH ROW
    EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_cat_nomenclature_soft_delete
    BEFORE UPDATE OF deletion_mark ON cat_nomenclature
    FOR EACH ROW
    EXECUTE FUNCTION soft_delete_with_timestamp();

-- Comment
COMMENT ON TABLE cat_nomenclature IS 'Справочник Номенклатура - товары, услуги, материалы';

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

DROP TRIGGER IF EXISTS trg_cat_nomenclature_soft_delete ON cat_nomenclature;
DROP TRIGGER IF EXISTS trg_cat_nomenclature_txid ON cat_nomenclature;
DROP TABLE IF EXISTS cat_nomenclature CASCADE;

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
-- +goose StatementEnd
