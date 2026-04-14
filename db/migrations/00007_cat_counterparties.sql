-- +goose Up
-- Description: Counterparties catalog (Справочник "Контрагенты")
-- +goose StatementBegin
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

CREATE TABLE cat_counterparties (
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
    parent_id UUID REFERENCES cat_counterparties(id),
    is_folder BOOLEAN      NOT NULL DEFAULT FALSE,

    -- Counterparty-specific fields
    type           VARCHAR(20)  NOT NULL DEFAULT 'customer',
    legal_form     VARCHAR(20)  NOT NULL DEFAULT 'company',
    full_name      VARCHAR(500),
    inn            VARCHAR(12),
    kpp            VARCHAR(9),
    ogrn           VARCHAR(15),
    legal_address  TEXT,
    actual_address TEXT,
    phone          VARCHAR(50),
    email          VARCHAR(255),
    contact_person VARCHAR(255),
    comment        TEXT,

    CONSTRAINT chk_counterparty_type CHECK (type IN ('customer', 'supplier', 'both', 'other')),
    CONSTRAINT chk_legal_form        CHECK (legal_form IN ('individual', 'sole_trader', 'company', 'government'))
);

-- Unique indexes
CREATE UNIQUE INDEX idx_cat_counterparties_code ON cat_counterparties (code) WHERE deletion_mark = FALSE;
CREATE UNIQUE INDEX idx_cat_counterparties_inn  ON cat_counterparties (inn) WHERE deletion_mark = FALSE AND inn IS NOT NULL AND inn != '';

-- Search / filter indexes
CREATE INDEX idx_cat_counterparties_name      ON cat_counterparties USING gin (name gin_trgm_ops);
CREATE INDEX idx_cat_counterparties_full_name ON cat_counterparties USING gin (full_name gin_trgm_ops);
CREATE INDEX idx_cat_counterparties_type      ON cat_counterparties (type) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_counterparties_parent    ON cat_counterparties (parent_id) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_counterparties_attrs     ON cat_counterparties USING gin (attributes);

-- CDC indexes & triggers
CREATE INDEX idx_cat_counterparties_txid ON cat_counterparties (_txid) WHERE _deleted_at IS NULL;

CREATE TRIGGER trg_cat_counterparties_txid
    BEFORE UPDATE ON cat_counterparties
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_cat_counterparties_soft_delete
    BEFORE UPDATE OF deletion_mark ON cat_counterparties
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

CREATE TRIGGER trg_cat_counterparties_updated_at
    BEFORE UPDATE ON cat_counterparties
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Keyset pagination
CREATE INDEX idx_cat_counterparties_name_id ON cat_counterparties (name ASC, id ASC);

COMMENT ON TABLE cat_counterparties IS 'Справочник Контрагенты — покупатели, поставщики и другие бизнес-партнеры';

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_lock(hashtext('metapus_migrations'));
DROP TABLE IF EXISTS cat_counterparties CASCADE;
SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
-- +goose StatementEnd
