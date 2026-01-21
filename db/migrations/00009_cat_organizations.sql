-- +goose Up
-- Description: Organizations catalog (required for multi-org support)
-- All documents reference organization_id

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

CREATE TABLE cat_organizations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    code VARCHAR(20) NOT NULL,
    name VARCHAR(255) NOT NULL,
    full_name TEXT,
    inn VARCHAR(12),                        -- Tax ID
    kpp VARCHAR(9),                         -- Tax registration code
    ogrn VARCHAR(15),                       -- State registration number
    legal_address TEXT,
    actual_address TEXT,
    phone VARCHAR(50),
    email VARCHAR(255),
    deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
    version INT NOT NULL DEFAULT 1,
    attributes JSONB
);

-- Unique code within tenant (excluding deleted)
CREATE UNIQUE INDEX idx_org_code ON cat_organizations (code) 
    WHERE deletion_mark = FALSE;

-- Unique INN within tenant (excluding deleted)
CREATE UNIQUE INDEX idx_org_inn ON cat_organizations (inn) 
    WHERE deletion_mark = FALSE AND inn IS NOT NULL;

-- Index for tenant


-- GIN index for attributes
CREATE INDEX idx_org_attributes ON cat_organizations USING GIN (attributes);

COMMENT ON TABLE cat_organizations IS 'Legal entities owned by tenant';

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
DROP TABLE IF EXISTS cat_organizations;
