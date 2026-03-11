-- +goose Up
-- Migration: Security Profiles subsystem
-- Tables for RLS dimensions and FLS field policies per user profile.

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- Security Profiles (named sets of RLS + FLS rules)
CREATE TABLE IF NOT EXISTS security_profiles (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code        VARCHAR(50)  NOT NULL,
    name        VARCHAR(200) NOT NULL,
    description TEXT,
    is_system   BOOLEAN      NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),

    CONSTRAINT security_profiles_code_unique UNIQUE (code)
);

-- RLS dimensions per profile
-- Each row grants access to a set of IDs for a given dimension.
-- Example: profile "sales_manager", dimension "organization", allowed_ids = {org-1, org-2}
CREATE TABLE IF NOT EXISTS security_profile_dimensions (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id     UUID        NOT NULL REFERENCES security_profiles(id) ON DELETE CASCADE,
    dimension_name VARCHAR(50) NOT NULL,  -- e.g. "organization", "counterparty", "warehouse"
    allowed_ids    UUID[]      NOT NULL DEFAULT '{}',

    CONSTRAINT spd_profile_dimension_unique UNIQUE (profile_id, dimension_name)
);

CREATE INDEX IF NOT EXISTS idx_spd_profile_id ON security_profile_dimensions(profile_id);

-- FLS field policies per profile
-- Each row defines which fields are visible/editable for a given entity + action.
CREATE TABLE IF NOT EXISTS security_profile_field_policies (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id     UUID         NOT NULL REFERENCES security_profiles(id) ON DELETE CASCADE,
    entity_name    VARCHAR(100) NOT NULL,  -- e.g. "goods_receipt", "counterparty"
    action         VARCHAR(20)  NOT NULL DEFAULT 'read', -- "read" or "write"
    allowed_fields TEXT[]       NOT NULL DEFAULT '{"*"}', -- mini-DSL: ["*", "-unit_price"]
    table_parts    JSONB        NOT NULL DEFAULT '{}',    -- {"lines": ["*", "-amount"]}

    CONSTRAINT spfp_profile_entity_action_unique UNIQUE (profile_id, entity_name, action)
);

CREATE INDEX IF NOT EXISTS idx_spfp_profile_id ON security_profile_field_policies(profile_id);

-- User ↔ Security Profile mapping (many-to-many, but typically 1 profile per user)
CREATE TABLE IF NOT EXISTS user_security_profiles (
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    profile_id UUID NOT NULL REFERENCES security_profiles(id) ON DELETE CASCADE,

    PRIMARY KEY (user_id, profile_id)
);

CREATE INDEX IF NOT EXISTS idx_usp_user_id ON user_security_profiles(user_id);
CREATE INDEX IF NOT EXISTS idx_usp_profile_id ON user_security_profiles(profile_id);

-- Seed: system profiles
INSERT INTO security_profiles (code, name, description, is_system) VALUES
    ('full_access', 'Full Access', 'Unrestricted access to all data and fields (admin-level)', true),
    ('viewer', 'Viewer', 'Read-only access with restricted financial fields', true)
ON CONFLICT (code) DO NOTHING;

-- Seed: viewer profile FLS — hide sensitive financial fields on documents
INSERT INTO security_profile_field_policies (profile_id, entity_name, action, allowed_fields, table_parts)
SELECT sp.id, 'goods_receipt', 'read',
       ARRAY['*', '-unit_price', '-amount', '-total_amount', '-total_vat', '-discount_amount'],
       '{"lines": ["*", "-unit_price", "-amount", "-vat_amount", "-discount_amount"]}'::jsonb
FROM security_profiles sp WHERE sp.code = 'viewer'
ON CONFLICT (profile_id, entity_name, action) DO NOTHING;

INSERT INTO security_profile_field_policies (profile_id, entity_name, action, allowed_fields, table_parts)
SELECT sp.id, 'goods_issue', 'read',
       ARRAY['*', '-unit_price', '-amount', '-total_amount', '-total_vat', '-discount_amount'],
       '{"lines": ["*", "-unit_price", "-amount", "-vat_amount", "-discount_amount"]}'::jsonb
FROM security_profiles sp WHERE sp.code = 'viewer'
ON CONFLICT (profile_id, entity_name, action) DO NOTHING;

-- Seed: viewer profile FLS — block all writes
INSERT INTO security_profile_field_policies (profile_id, entity_name, action, allowed_fields)
SELECT sp.id, 'goods_receipt', 'write', ARRAY[]::TEXT[]
FROM security_profiles sp WHERE sp.code = 'viewer'
ON CONFLICT (profile_id, entity_name, action) DO NOTHING;

INSERT INTO security_profile_field_policies (profile_id, entity_name, action, allowed_fields)
SELECT sp.id, 'goods_issue', 'write', ARRAY[]::TEXT[]
FROM security_profiles sp WHERE sp.code = 'viewer'
ON CONFLICT (profile_id, entity_name, action) DO NOTHING;

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
DROP TABLE IF EXISTS user_security_profiles;
DROP TABLE IF EXISTS security_profile_field_policies;
DROP TABLE IF EXISTS security_profile_dimensions;
DROP TABLE IF EXISTS security_profiles;
