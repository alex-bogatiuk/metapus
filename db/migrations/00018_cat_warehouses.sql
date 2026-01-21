-- file: db/migrations/00017_cat_warehouses.sql

-- Migration: Create Warehouses catalog
-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

CREATE TABLE IF NOT EXISTS cat_warehouses (
    -- Base fields (from entity.Base)
          id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),

          deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
          version INT NOT NULL DEFAULT 1,
          attributes JSONB DEFAULT '{}',

    -- Catalog fields (from entity.Catalog)
          code VARCHAR(50) NOT NULL,
          name VARCHAR(255) NOT NULL,
          parent_id UUID REFERENCES cat_warehouses(id),
          is_folder BOOLEAN NOT NULL DEFAULT FALSE,

    -- Warehouse-specific fields
          type VARCHAR(20) NOT NULL DEFAULT 'main',
          address TEXT,
          allow_negative_stock BOOLEAN NOT NULL DEFAULT FALSE,
          organization_id UUID,
          description TEXT,
          is_default BOOLEAN NOT NULL DEFAULT FALSE, -- Поле для указания основного склада
          is_active BOOLEAN NOT NULL DEFAULT TRUE,

    -- Constraints
          CONSTRAINT chk_warehouse_type CHECK (type IN ('main', 'distribution', 'retail', 'production', 'transit'))
);

-- Indexes

-- 1. Unique code within tenant (Business Key)
CREATE UNIQUE INDEX idx_cat_warehouses_code
    ON cat_warehouses (code)
    WHERE deletion_mark = FALSE;

-- 2. Unique Default Warehouse (Technical Constraint)
-- ИСПРАВЛЕНО: Индекс теперь разрешает множество складов, но только один "Основной" (is_default=true)
CREATE UNIQUE INDEX idx_cat_warehouses_default
    ON cat_warehouses (is_default)
    WHERE is_default = TRUE AND deletion_mark = FALSE;

-- Search and Filter Indexes
CREATE INDEX idx_cat_warehouses_name ON cat_warehouses USING gin (name gin_trgm_ops);
CREATE INDEX idx_cat_warehouses_address ON cat_warehouses USING gin (address gin_trgm_ops);
CREATE INDEX idx_cat_warehouses_type ON cat_warehouses (type) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_warehouses_parent ON cat_warehouses (parent_id) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_warehouses_org ON cat_warehouses (organization_id) WHERE organization_id IS NOT NULL;
CREATE INDEX idx_cat_warehouses_attrs ON cat_warehouses USING gin (attributes);

COMMENT ON TABLE cat_warehouses IS 'Справочник Склады - места хранения товаров и материалов';
COMMENT ON COLUMN cat_warehouses.is_default IS 'Признак основного склада для автоматической подстановки в документы';

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

DROP TABLE IF EXISTS cat_warehouses CASCADE;

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
-- +goose StatementEnd