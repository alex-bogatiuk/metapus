-- +goose Up
-- Description: Warehouses catalog (Справочник "Склады")
-- +goose StatementBegin
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

CREATE TABLE cat_warehouses (
    -- Base fields
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    deletion_mark BOOLEAN     NOT NULL DEFAULT FALSE,
    version       INT         NOT NULL DEFAULT 1,
    attributes    JSONB       DEFAULT '{}',

    -- CDC
    _deleted_at TIMESTAMPTZ,
    _txid       BIGINT DEFAULT txid_current(),

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Catalog fields
    code      VARCHAR(50)  NOT NULL,
    name      VARCHAR(255) NOT NULL,
    parent_id UUID REFERENCES cat_warehouses(id),
    is_folder BOOLEAN      NOT NULL DEFAULT FALSE,

    -- Warehouse-specific fields
    type                 VARCHAR(20) NOT NULL DEFAULT 'main',
    address              TEXT,
    allow_negative_stock BOOLEAN     NOT NULL DEFAULT FALSE,
    organization_id      UUID REFERENCES cat_organizations(id),
    description          TEXT,
    is_default           BOOLEAN     NOT NULL DEFAULT FALSE,
    is_active            BOOLEAN     NOT NULL DEFAULT TRUE,

    CONSTRAINT chk_warehouse_type CHECK (type IN ('main', 'distribution', 'retail', 'production', 'transit'))
);

-- Unique indexes
CREATE UNIQUE INDEX idx_cat_warehouses_code    ON cat_warehouses (code) WHERE deletion_mark = FALSE;
CREATE UNIQUE INDEX idx_cat_warehouses_default ON cat_warehouses (is_default) WHERE is_default = TRUE AND deletion_mark = FALSE;

-- Search / filter indexes
CREATE INDEX idx_cat_warehouses_name    ON cat_warehouses USING gin (name gin_trgm_ops);
CREATE INDEX idx_cat_warehouses_address ON cat_warehouses USING gin (address gin_trgm_ops);
CREATE INDEX idx_cat_warehouses_type    ON cat_warehouses (type) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_warehouses_parent  ON cat_warehouses (parent_id) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_warehouses_org     ON cat_warehouses (organization_id) WHERE organization_id IS NOT NULL;
CREATE INDEX idx_cat_warehouses_attrs   ON cat_warehouses USING gin (attributes);

-- CDC indexes & triggers
CREATE INDEX idx_cat_warehouses_txid ON cat_warehouses (_txid) WHERE _deleted_at IS NULL;

CREATE TRIGGER trg_cat_warehouses_txid
    BEFORE UPDATE ON cat_warehouses
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_cat_warehouses_soft_delete
    BEFORE UPDATE OF deletion_mark ON cat_warehouses
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

CREATE TRIGGER trg_cat_warehouses_updated_at
    BEFORE UPDATE ON cat_warehouses
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Keyset pagination
CREATE INDEX idx_cat_warehouses_name_id ON cat_warehouses (name ASC, id ASC);

COMMENT ON TABLE cat_warehouses IS 'Справочник Склады — места хранения товаров и материалов';
COMMENT ON COLUMN cat_warehouses.is_default IS 'Признак основного склада для автоматической подстановки в документы';

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_lock(hashtext('metapus_migrations'));
DROP TABLE IF EXISTS cat_warehouses CASCADE;
SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
-- +goose StatementEnd
