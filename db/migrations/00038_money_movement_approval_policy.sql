-- +goose Up
-- Description: Signed configurable approval artifacts for crypto money movement.

SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

CREATE TABLE reg_money_movement_intents (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    basis_type            TEXT NOT NULL,
    basis_id              UUID NOT NULL,
    purpose               TEXT NOT NULL CHECK (purpose IN ('withdrawal','treasury_withdrawal')),
    intent_digest         TEXT NOT NULL,
    policy_version        TEXT NOT NULL,
    policy_digest         TEXT NOT NULL,
    policy_bundle_jws     TEXT NOT NULL,
    intent_jws            TEXT NOT NULL,
    risk_assessment_jws   TEXT NOT NULL,
    policy_decision_jws   TEXT NOT NULL,
    required_principals   TEXT[] NOT NULL CHECK (cardinality(required_principals) > 0),
    matched_rule_ids      TEXT[] NOT NULL DEFAULT '{}',
    expires_at            TIMESTAMPTZ NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (basis_type, basis_id),
    UNIQUE (intent_digest)
);

CREATE INDEX idx_money_movement_intents_basis
    ON reg_money_movement_intents (basis_type, basis_id);

CREATE INDEX idx_money_movement_intents_policy
    ON reg_money_movement_intents (policy_version, policy_digest);

CREATE TABLE reg_money_movement_approvals (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    basis_type              TEXT NOT NULL,
    basis_id                UUID NOT NULL,
    intent_digest           TEXT NOT NULL,
    principal_type          TEXT NOT NULL CHECK (principal_type IN ('admin','risk')),
    principal_id            TEXT NOT NULL,
    approval_jws            TEXT NOT NULL,
    policy_version          TEXT NOT NULL,
    policy_digest           TEXT NOT NULL,
    policy_decision_digest  TEXT NOT NULL,
    expires_at              TIMESTAMPTZ NOT NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (basis_type, basis_id, principal_type, policy_version),
    FOREIGN KEY (intent_digest) REFERENCES reg_money_movement_intents(intent_digest)
);

CREATE INDEX idx_money_movement_approvals_basis
    ON reg_money_movement_approvals (basis_type, basis_id);

-- +goose Down

SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

DROP TABLE IF EXISTS reg_money_movement_approvals;
DROP TABLE IF EXISTS reg_money_movement_intents;
