-- +goose Up
-- Description: Currencies catalog (Справочник "Валюты")
-- +goose StatementBegin
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

CREATE TABLE cat_currencies (
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
    parent_id UUID REFERENCES cat_currencies(id),
    is_folder BOOLEAN      NOT NULL DEFAULT FALSE,

    -- Currency-specific fields
    iso_code          VARCHAR(10) NOT NULL,              -- ISO 4217 or crypto ticker (USD, USDT, MATIC)
    iso_numeric_code  VARCHAR(3),
    symbol            VARCHAR(10) NOT NULL,
    decimal_places    INT         NOT NULL DEFAULT 2,
    minor_multiplier  BIGINT      NOT NULL DEFAULT 100,
    is_base           BOOLEAN     NOT NULL DEFAULT FALSE,
    country           VARCHAR(100),

    CONSTRAINT chk_iso_code       CHECK (iso_code ~ '^[A-Z]{2,5}$'),
    CONSTRAINT chk_decimal_places CHECK (decimal_places >= 0 AND decimal_places <= 18)
);

-- Unique indexes
CREATE UNIQUE INDEX idx_cat_currencies_code ON cat_currencies (code) WHERE deletion_mark = FALSE;
CREATE UNIQUE INDEX idx_cat_currencies_iso  ON cat_currencies (iso_code) WHERE deletion_mark = FALSE;
CREATE UNIQUE INDEX idx_cat_currencies_base ON cat_currencies (is_base) WHERE is_base = TRUE AND deletion_mark = FALSE;

-- Search / filter indexes
CREATE INDEX idx_cat_currencies_name   ON cat_currencies USING gin (name gin_trgm_ops);
CREATE INDEX idx_cat_currencies_parent ON cat_currencies (parent_id) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_currencies_attrs  ON cat_currencies USING gin (attributes);

-- CDC indexes & triggers
CREATE INDEX idx_cat_currencies_txid ON cat_currencies (_txid) WHERE _deleted_at IS NULL;

CREATE TRIGGER trg_cat_currencies_txid
    BEFORE UPDATE ON cat_currencies
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_cat_currencies_soft_delete
    BEFORE UPDATE OF deletion_mark ON cat_currencies
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

CREATE TRIGGER trg_cat_currencies_updated_at
    BEFORE UPDATE ON cat_currencies
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Keyset pagination
CREATE INDEX idx_cat_currencies_name_id ON cat_currencies (name ASC, id ASC);

COMMENT ON TABLE cat_currencies IS 'Справочник Валюты — денежные единицы с курсами обмена';

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_lock(hashtext('metapus_migrations'));
DROP TABLE IF EXISTS cat_currencies CASCADE;
SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
-- +goose StatementEnd
