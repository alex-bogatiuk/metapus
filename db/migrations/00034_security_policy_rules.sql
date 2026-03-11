-- +goose Up
-- Migration: CEL Policy Rules for SecurityProfile
-- Stores CEL expressions evaluated at runtime for fine-grained authorization.

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

CREATE TABLE IF NOT EXISTS security_policy_rules (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id  UUID         NOT NULL REFERENCES security_profiles(id) ON DELETE CASCADE,
    name        VARCHAR(200) NOT NULL,
    description TEXT,
    entity_name VARCHAR(100) NOT NULL,  -- "*" = all entities, or specific e.g. "goods_receipt"
    actions     TEXT[]       NOT NULL DEFAULT '{*}', -- e.g. {"create","update"} or {"*"}
    expression  TEXT         NOT NULL,  -- CEL expression, must return bool
    effect      VARCHAR(10)  NOT NULL DEFAULT 'deny', -- 'deny' or 'allow'
    priority    INT          NOT NULL DEFAULT 0,       -- higher = evaluated first
    enabled     BOOLEAN      NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),

    CONSTRAINT spr_effect_check CHECK (effect IN ('deny', 'allow'))
);

CREATE INDEX IF NOT EXISTS idx_spr_profile_id ON security_policy_rules(profile_id);
CREATE INDEX IF NOT EXISTS idx_spr_entity_enabled ON security_policy_rules(entity_name, enabled);

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
DROP TABLE IF EXISTS security_policy_rules;
