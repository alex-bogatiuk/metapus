-- +goose Up
-- Description: Organizations catalog (Справочник "Организации")
-- +goose StatementBegin
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

CREATE TABLE cat_organizations (
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
    code      VARCHAR(20)  NOT NULL,
    name      VARCHAR(255) NOT NULL,
    parent_id UUID REFERENCES cat_organizations(id),
    is_folder BOOLEAN      NOT NULL DEFAULT FALSE,

    -- Organization-specific fields
    full_name        TEXT,
    inn              VARCHAR(12),
    kpp              VARCHAR(9),
    ogrn             VARCHAR(15),
    legal_address    TEXT,
    actual_address   TEXT,
    phone            VARCHAR(50),
    email            VARCHAR(255),
    website          VARCHAR(255),
    base_currency_id UUID REFERENCES cat_currencies(id),
    is_default       BOOLEAN NOT NULL DEFAULT FALSE,

    -- Responsible persons (moved from sys_settings.organization)
    director         VARCHAR(255),
    accountant       VARCHAR(255),
    logo_url         TEXT,

    -- Accounting policy (moved from sys_settings.accounting)
    tax_system            VARCHAR(50)  NOT NULL DEFAULT 'osno',
    vat_payer             BOOLEAN      NOT NULL DEFAULT FALSE,
    default_vat_rate_id   UUID REFERENCES cat_vat_rates(id),
    inventory_method      VARCHAR(50)  NOT NULL DEFAULT 'fifo',
    fiscal_year_start     VARCHAR(10)  NOT NULL DEFAULT '01-01'
);

-- Unique indexes
CREATE UNIQUE INDEX idx_org_code ON cat_organizations (code) WHERE deletion_mark = FALSE;
CREATE UNIQUE INDEX idx_org_inn  ON cat_organizations (inn) WHERE deletion_mark = FALSE AND inn IS NOT NULL;

-- Search / filter indexes
CREATE INDEX idx_cat_organizations_parent          ON cat_organizations (parent_id) WHERE deletion_mark = FALSE;
CREATE INDEX idx_org_attributes                    ON cat_organizations USING GIN (attributes);
CREATE INDEX idx_cat_organizations_base_currency_id ON cat_organizations (base_currency_id);

-- CDC indexes & triggers
CREATE INDEX idx_cat_organizations_txid ON cat_organizations (_txid) WHERE _deleted_at IS NULL;

CREATE TRIGGER trg_cat_organizations_txid
    BEFORE UPDATE ON cat_organizations
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_cat_organizations_soft_delete
    BEFORE UPDATE OF deletion_mark ON cat_organizations
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

CREATE TRIGGER trg_cat_organizations_updated_at
    BEFORE UPDATE ON cat_organizations
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Keyset pagination
CREATE INDEX idx_cat_organizations_name_id ON cat_organizations (name ASC, id ASC);

COMMENT ON TABLE cat_organizations IS 'Legal entities owned by tenant';
COMMENT ON COLUMN cat_organizations.base_currency_id IS 'Main accounting currency for the organization';
COMMENT ON COLUMN cat_organizations.is_default IS 'Indicates if this is the default organization';

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_lock(hashtext('metapus_migrations'));
DROP TABLE IF EXISTS cat_organizations CASCADE;
SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
-- +goose StatementEnd
