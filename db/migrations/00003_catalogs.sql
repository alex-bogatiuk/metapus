-- Base catalogs: currencies, organizations, counterparties and contracts.
-- Consolidated schema: each object is declared once in its final form.

-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- Function bodies are validated when first executed; definitions precede the tables they use.
SET LOCAL check_function_bodies = off;


-- ---- Tables ----
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
-- Keyset pagination
CREATE INDEX idx_cat_currencies_name_id ON cat_currencies (name ASC, id ASC);
COMMENT ON TABLE cat_currencies IS 'Справочник Валюты — денежные единицы с курсами обмена';

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
-- Keyset pagination
CREATE INDEX idx_cat_organizations_name_id ON cat_organizations (name ASC, id ASC);
COMMENT ON TABLE cat_organizations IS 'Legal entities owned by tenant';
COMMENT ON COLUMN cat_organizations.base_currency_id IS 'Main accounting currency for the organization';
COMMENT ON COLUMN cat_organizations.is_default IS 'Indicates if this is the default organization';

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
-- Keyset pagination
CREATE INDEX idx_cat_counterparties_name_id ON cat_counterparties (name ASC, id ASC);
COMMENT ON TABLE cat_counterparties IS 'Справочник Контрагенты — покупатели, поставщики и другие бизнес-партнеры';

CREATE TABLE cat_contracts (
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
    parent_id UUID,
    is_folder BOOLEAN      NOT NULL DEFAULT FALSE,

    -- Contract-specific fields
    counterparty_id  UUID NOT NULL REFERENCES cat_counterparties(id),
    type             VARCHAR(20) NOT NULL DEFAULT 'supply',
    currency_id      UUID REFERENCES cat_currencies(id),
    valid_from       DATE,
    valid_to         DATE,
    payment_term_days INT NOT NULL DEFAULT 0,
    description      TEXT,

    CONSTRAINT chk_contract_type      CHECK (type IN ('supply', 'sale', 'other')),
    CONSTRAINT chk_payment_term_days  CHECK (payment_term_days >= 0),
    CONSTRAINT chk_valid_dates        CHECK (valid_to IS NULL OR valid_from IS NULL OR valid_to >= valid_from)
);
-- Unique indexes
CREATE UNIQUE INDEX uq_cat_contracts_code ON cat_contracts (code) WHERE deletion_mark = FALSE;
-- Search / filter indexes
CREATE INDEX idx_cat_contracts_counterparty ON cat_contracts (counterparty_id);
CREATE INDEX idx_cat_contracts_type         ON cat_contracts (type);
CREATE INDEX idx_cat_contracts_currency     ON cat_contracts (currency_id) WHERE currency_id IS NOT NULL;
CREATE INDEX idx_cat_contracts_parent       ON cat_contracts (parent_id) WHERE parent_id IS NOT NULL;
CREATE INDEX idx_cat_contracts_name         ON cat_contracts USING gin (name gin_trgm_ops);
CREATE INDEX idx_cat_contracts_code_trgm    ON cat_contracts USING gin (code gin_trgm_ops);
-- CDC indexes & triggers
CREATE INDEX idx_cat_contracts_txid ON cat_contracts (_txid) WHERE _deleted_at IS NULL;
-- Keyset pagination
CREATE INDEX idx_cat_contracts_name_id ON cat_contracts (name ASC, id ASC);
COMMENT ON TABLE cat_contracts IS 'Справочник Договоры контрагентов';


-- ---- Triggers ----
CREATE TRIGGER trg_cat_currencies_txid
    BEFORE UPDATE ON cat_currencies
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_cat_currencies_soft_delete
    BEFORE UPDATE OF deletion_mark ON cat_currencies
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

CREATE TRIGGER trg_cat_currencies_updated_at
    BEFORE UPDATE ON cat_currencies
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_cat_organizations_txid
    BEFORE UPDATE ON cat_organizations
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_cat_organizations_soft_delete
    BEFORE UPDATE OF deletion_mark ON cat_organizations
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

CREATE TRIGGER trg_cat_organizations_updated_at
    BEFORE UPDATE ON cat_organizations
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_cat_counterparties_txid
    BEFORE UPDATE ON cat_counterparties
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_cat_counterparties_soft_delete
    BEFORE UPDATE OF deletion_mark ON cat_counterparties
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

CREATE TRIGGER trg_cat_counterparties_updated_at
    BEFORE UPDATE ON cat_counterparties
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_cat_contracts_txid
    BEFORE UPDATE ON cat_contracts
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_cat_contracts_soft_delete
    BEFORE UPDATE OF deletion_mark ON cat_contracts
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

CREATE TRIGGER trg_cat_contracts_updated_at
    BEFORE UPDATE ON cat_contracts
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TABLE cat_contracts;
DROP TABLE cat_counterparties;
DROP TABLE cat_organizations;
DROP TABLE cat_currencies;
-- +goose StatementEnd
