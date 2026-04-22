-- +goose Up
-- Description: Nomenclature catalog (Справочник "Номенклатура")
-- +goose StatementBegin
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

CREATE TABLE cat_nomenclatures (
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
    parent_id UUID REFERENCES cat_nomenclatures(id),
    is_folder BOOLEAN      NOT NULL DEFAULT FALSE,

    -- Nomenclature-specific fields
    type               VARCHAR(20)   NOT NULL DEFAULT 'goods',
    article            VARCHAR(100),
    barcode            VARCHAR(50),
    base_unit_id       UUID REFERENCES cat_units(id),
    default_vat_rate_id UUID REFERENCES cat_vat_rates(id),
    weight             NUMERIC(15,4) DEFAULT 0,
    volume             NUMERIC(15,6) DEFAULT 0,
    description        TEXT,
    manufacturer_id    UUID REFERENCES cat_counterparties(id),
    country_of_origin  VARCHAR(2),
    is_weighed         BOOLEAN       NOT NULL DEFAULT FALSE,
    track_serial       BOOLEAN       NOT NULL DEFAULT FALSE,
    track_batch        BOOLEAN       NOT NULL DEFAULT FALSE,
    image_url          TEXT,

    CONSTRAINT chk_nomenclature_type CHECK (type IN ('goods', 'service', 'work', 'material', 'semi', 'product')),
    CONSTRAINT chk_weight_positive   CHECK (weight >= 0),
    CONSTRAINT chk_volume_positive   CHECK (volume >= 0)
);

-- Unique indexes
CREATE UNIQUE INDEX idx_cat_nomenclatures_code    ON cat_nomenclatures (code) WHERE deletion_mark = FALSE;
CREATE UNIQUE INDEX idx_cat_nomenclatures_article ON cat_nomenclatures (article) WHERE deletion_mark = FALSE AND article IS NOT NULL AND article != '';
CREATE UNIQUE INDEX idx_cat_nomenclatures_barcode ON cat_nomenclatures (barcode) WHERE deletion_mark = FALSE AND barcode IS NOT NULL AND barcode != '';

-- Search / filter indexes
CREATE INDEX idx_cat_nomenclatures_name         ON cat_nomenclatures USING gin (name gin_trgm_ops);
CREATE INDEX idx_cat_nomenclatures_type         ON cat_nomenclatures (type) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_nomenclatures_parent       ON cat_nomenclatures (parent_id) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_nomenclatures_manufacturer ON cat_nomenclatures (manufacturer_id) WHERE manufacturer_id IS NOT NULL;
CREATE INDEX idx_cat_nomenclatures_vat_rate     ON cat_nomenclatures (default_vat_rate_id) WHERE default_vat_rate_id IS NOT NULL;
CREATE INDEX idx_cat_nomenclatures_attrs        ON cat_nomenclatures USING gin (attributes);

-- CDC indexes & triggers
CREATE INDEX idx_cat_nomenclatures_txid ON cat_nomenclatures (_txid) WHERE _deleted_at IS NULL;

CREATE TRIGGER trg_cat_nomenclatures_txid
    BEFORE UPDATE ON cat_nomenclatures
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_cat_nomenclatures_soft_delete
    BEFORE UPDATE OF deletion_mark ON cat_nomenclatures
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

CREATE TRIGGER trg_cat_nomenclatures_updated_at
    BEFORE UPDATE ON cat_nomenclatures
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Keyset pagination
CREATE INDEX idx_cat_nomenclatures_name_id ON cat_nomenclatures (name ASC, id ASC);

COMMENT ON TABLE cat_nomenclatures IS 'Справочник Номенклатура — товары, услуги, материалы';

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_lock(hashtext('metapus_migrations'));
DROP TABLE IF EXISTS cat_nomenclatures CASCADE;
SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
-- +goose StatementEnd
