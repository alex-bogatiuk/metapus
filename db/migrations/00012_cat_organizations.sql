-- Migration: Create Organizations catalog
-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

CREATE TABLE IF NOT EXISTS cat_organizations (
    -- Base fields (from entity.Base)
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),

    deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
    version INT NOT NULL DEFAULT 1,
    attributes JSONB DEFAULT '{}',

    -- CDC-ready columns
    _deleted_at TIMESTAMPTZ,
    _txid BIGINT DEFAULT txid_current(),

    -- Catalog fields (from entity.Catalog)
    code VARCHAR(20) NOT NULL,
    name VARCHAR(255) NOT NULL,
    parent_id UUID REFERENCES cat_organizations(id),
    is_folder BOOLEAN NOT NULL DEFAULT FALSE,

    -- Organization-specific fields
    full_name TEXT,
    inn VARCHAR(12),                        -- Tax ID
    kpp VARCHAR(9),                         -- Tax registration code
    ogrn VARCHAR(15),                       -- State registration number
    legal_address TEXT,
    actual_address TEXT,
    phone VARCHAR(50),
    email VARCHAR(255),
    base_currency_id UUID REFERENCES cat_currencies(id),
    is_default BOOLEAN NOT NULL DEFAULT FALSE
);

-- Indexes
-- Unique code within tenant (excluding soft-deleted)
CREATE UNIQUE INDEX idx_org_code ON cat_organizations (code)
    WHERE deletion_mark = FALSE;

-- Unique INN within tenant (excluding soft-deleted)
CREATE UNIQUE INDEX idx_org_inn ON cat_organizations (inn)
    WHERE deletion_mark = FALSE AND inn IS NOT NULL;

-- Search and hierarchy indexes
CREATE INDEX idx_cat_organizations_parent ON cat_organizations (parent_id) WHERE deletion_mark = FALSE;
CREATE INDEX idx_org_attributes ON cat_organizations USING GIN (attributes);
CREATE INDEX idx_cat_organizations_base_currency_id ON cat_organizations (base_currency_id);

-- CDC indexes & triggers
CREATE INDEX IF NOT EXISTS idx_cat_organizations_txid
    ON cat_organizations (_txid) WHERE _deleted_at IS NULL;

CREATE TRIGGER trg_cat_organizations_txid
    BEFORE UPDATE ON cat_organizations
    FOR EACH ROW
    EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_cat_organizations_soft_delete
    BEFORE UPDATE OF deletion_mark ON cat_organizations
    FOR EACH ROW
    EXECUTE FUNCTION soft_delete_with_timestamp();

-- Comments
COMMENT ON TABLE cat_organizations IS 'Legal entities owned by tenant';
COMMENT ON COLUMN cat_organizations.base_currency_id IS 'Main accounting currency for the organization';
COMMENT ON COLUMN cat_organizations.is_default IS 'Indicates if this is the default organization';

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

DROP TRIGGER IF EXISTS trg_cat_organizations_soft_delete ON cat_organizations;
DROP TRIGGER IF EXISTS trg_cat_organizations_txid ON cat_organizations;
DROP TABLE IF EXISTS cat_organizations CASCADE;

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
-- +goose StatementEnd
