-- Migration: Create Units catalog
-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

CREATE TABLE IF NOT EXISTS cat_units (
    -- Base fields (from entity.Base)
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),

    deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
    version INT NOT NULL DEFAULT 1,
    attributes JSONB DEFAULT '{}',
    
    -- Catalog fields (from entity.Catalog)
    code VARCHAR(50) NOT NULL,
    name VARCHAR(255) NOT NULL,
    parent_id UUID REFERENCES cat_units(id),
    is_folder BOOLEAN NOT NULL DEFAULT FALSE,
    
    -- Unit-specific fields
    type VARCHAR(20) NOT NULL DEFAULT 'piece',
    symbol VARCHAR(20) NOT NULL,
    international_code VARCHAR(20),
    base_unit_id UUID REFERENCES cat_units(id),
    conversion_factor NUMERIC(20,10) NOT NULL DEFAULT 1,
    is_base BOOLEAN NOT NULL DEFAULT TRUE,
    description TEXT,
    
    -- Constraints
    CONSTRAINT chk_unit_type CHECK (type IN ('piece', 'weight', 'length', 'area', 'volume', 'time', 'pack')),
    CONSTRAINT chk_conversion_positive CHECK (conversion_factor > 0)
);

-- Indexes
CREATE UNIQUE INDEX idx_cat_units_code 
    ON cat_units (code) 
    WHERE deletion_mark = FALSE;

CREATE UNIQUE INDEX idx_cat_units_symbol 
    ON cat_units (symbol) 
    WHERE deletion_mark = FALSE;

CREATE UNIQUE INDEX idx_cat_units_intl_code 
    ON cat_units (international_code) 
    WHERE deletion_mark = FALSE AND international_code IS NOT NULL AND international_code != '';

CREATE INDEX idx_cat_units_name ON cat_units USING gin (name gin_trgm_ops);
CREATE INDEX idx_cat_units_type ON cat_units (type) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_units_parent ON cat_units (parent_id) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_units_base ON cat_units (is_base) WHERE is_base = TRUE AND deletion_mark = FALSE;
CREATE INDEX idx_cat_units_base_unit ON cat_units (base_unit_id) WHERE base_unit_id IS NOT NULL;
CREATE INDEX idx_cat_units_attrs ON cat_units USING gin (attributes);

COMMENT ON TABLE cat_units IS 'Справочник Единицы измерения - единицы для учёта товаров и услуг';

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

DROP TABLE IF EXISTS cat_units CASCADE;

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
-- +goose StatementEnd
