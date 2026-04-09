-- +goose Up
-- Description: Security profiles subsystem (RLS dimensions, FLS field policies, CEL policy rules)

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- ── Security Profiles ──────────────────────────────────────────────────────
CREATE TABLE security_profiles (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    code        VARCHAR(50)  NOT NULL,
    name        VARCHAR(200) NOT NULL,
    description TEXT,
    is_system   BOOLEAN      NOT NULL DEFAULT FALSE,
    is_active   BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_security_profiles_code UNIQUE (code)
);

CREATE TRIGGER trg_security_profiles_updated_at
    BEFORE UPDATE ON security_profiles
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ── RLS Dimensions (row-level security) ────────────────────────────────────
CREATE TABLE security_profile_dimensions (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id  UUID        NOT NULL REFERENCES security_profiles(id) ON DELETE CASCADE,
    entity_name VARCHAR(50) NOT NULL DEFAULT '*',
    dimension   VARCHAR(50) NOT NULL,
    value_id    UUID        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_dim UNIQUE (profile_id, entity_name, dimension, value_id)
);

CREATE INDEX idx_spd_profile   ON security_profile_dimensions (profile_id);
CREATE INDEX idx_spd_dimension ON security_profile_dimensions (dimension, value_id);

COMMENT ON TABLE security_profile_dimensions IS 'RLS dimensions — grants access to specific entity IDs per dimension';
COMMENT ON COLUMN security_profile_dimensions.entity_name IS 'Entity scope (* = all, or specific like goods_receipt)';
COMMENT ON COLUMN security_profile_dimensions.dimension IS 'Dimension name (organization, warehouse, counterparty)';
COMMENT ON COLUMN security_profile_dimensions.value_id IS 'Allowed entity ID for this dimension';

-- ── FLS Field Policies (field-level security) ──────────────────────────────
CREATE TABLE security_profile_field_policies (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id  UUID        NOT NULL REFERENCES security_profiles(id) ON DELETE CASCADE,
    entity_name VARCHAR(50) NOT NULL,
    field_name  VARCHAR(50) NOT NULL,
    visibility  VARCHAR(20) NOT NULL DEFAULT 'visible',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_visibility CHECK (visibility IN ('visible', 'hidden', 'readonly')),
    CONSTRAINT uq_fls UNIQUE (profile_id, entity_name, field_name)
);

CREATE INDEX idx_fls_profile ON security_profile_field_policies (profile_id);
CREATE INDEX idx_fls_entity  ON security_profile_field_policies (entity_name);

COMMENT ON TABLE security_profile_field_policies IS 'FLS — controls field visibility/editability per entity';

-- ── User ↔ Security Profile (M2M) ─────────────────────────────────────────
CREATE TABLE user_security_profiles (
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    profile_id UUID NOT NULL REFERENCES security_profiles(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, profile_id)
);

CREATE INDEX idx_usp_user    ON user_security_profiles (user_id);
CREATE INDEX idx_usp_profile ON user_security_profiles (profile_id);

-- ── CEL Policy Rules ───────────────────────────────────────────────────────
CREATE TABLE security_policy_rules (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id  UUID        NOT NULL REFERENCES security_profiles(id) ON DELETE CASCADE,
    name        VARCHAR(200) NOT NULL,
    description TEXT,
    entity_name VARCHAR(50) NOT NULL,
    actions     TEXT[]      NOT NULL DEFAULT '{}',
    effect      VARCHAR(10) NOT NULL DEFAULT 'deny',
    expression  TEXT        NOT NULL,
    priority    INT         NOT NULL DEFAULT 0,
    is_active   BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_effect CHECK (effect IN ('deny', 'allow'))
);

CREATE INDEX idx_spr_profile ON security_policy_rules (profile_id);
CREATE INDEX idx_spr_entity  ON security_policy_rules (entity_name) WHERE is_active = TRUE;

CREATE TRIGGER trg_security_policy_rules_updated_at
    BEFORE UPDATE ON security_policy_rules
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

COMMENT ON TABLE security_policy_rules IS 'CEL-based fine-grained authorization rules';
COMMENT ON COLUMN security_policy_rules.expression IS 'CEL expression evaluated at runtime';
COMMENT ON COLUMN security_policy_rules.actions IS 'Array of actions: create, read, update, delete, post, unpost';

-- ── Seed system profiles ───────────────────────────────────────────────────
INSERT INTO security_profiles (id, code, name, description, is_system) VALUES
    ('a0000000-0000-0000-0000-000000000001', 'full_access', 'Full Access', 'No restrictions — all dimensions, all fields', TRUE),
    ('a0000000-0000-0000-0000-000000000002', 'viewer', 'Viewer (Read-Only)', 'Read-only with hidden financial fields', TRUE);

-- Viewer FLS: hide financial fields
INSERT INTO security_profile_field_policies (profile_id, entity_name, field_name, visibility) VALUES
    ('a0000000-0000-0000-0000-000000000002', 'goods_receipt', 'unit_price', 'hidden'),
    ('a0000000-0000-0000-0000-000000000002', 'goods_receipt', 'amount', 'hidden'),
    ('a0000000-0000-0000-0000-000000000002', 'goods_receipt', 'total_amount', 'hidden'),
    ('a0000000-0000-0000-0000-000000000002', 'goods_receipt', 'total_vat', 'hidden'),
    ('a0000000-0000-0000-0000-000000000002', 'goods_issue', 'unit_price', 'hidden'),
    ('a0000000-0000-0000-0000-000000000002', 'goods_issue', 'amount', 'hidden'),
    ('a0000000-0000-0000-0000-000000000002', 'goods_issue', 'total_amount', 'hidden'),
    ('a0000000-0000-0000-0000-000000000002', 'goods_issue', 'total_vat', 'hidden');

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
SELECT pg_advisory_lock(hashtext('metapus_migrations'));
DROP TABLE IF EXISTS security_policy_rules;
DROP TABLE IF EXISTS user_security_profiles;
DROP TABLE IF EXISTS security_profile_field_policies;
DROP TABLE IF EXISTS security_profile_dimensions;
DROP TABLE IF EXISTS security_profiles;
SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
