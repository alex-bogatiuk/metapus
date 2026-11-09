-- +goose Up
-- Description: Immutable signing authorizations for production Vault crypto signing

-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

CREATE TABLE reg_crypto_signing_authorizations (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    authorization_id   TEXT NOT NULL,
    tenant_id          TEXT NOT NULL,
    merchant_id        UUID,
    purpose            TEXT NOT NULL
                       CHECK (purpose IN ('withdrawal','sweep','treasury_withdrawal','activation','native_funding')),
    schema_version     INT NOT NULL CHECK (schema_version IN (1,2)),
    network_id         UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    network_code       TEXT NOT NULL,
    chain_family       TEXT NOT NULL
                       CHECK (chain_family IN ('tron','evm','ton','solana')),
    network_identifier TEXT NOT NULL,
    source_address     TEXT NOT NULL,
    fee_payer_address  TEXT NOT NULL CHECK (fee_payer_address <> ''),
    vault_key_id       TEXT NOT NULL,
    destination_address TEXT NOT NULL,
    asset_standard     TEXT NOT NULL,
    contract_address   TEXT NOT NULL DEFAULT '',
    amount_minor_units NUMERIC(78,0) NOT NULL CHECK (amount_minor_units >= 0),
    max_network_fee_minor NUMERIC(78,0) NOT NULL DEFAULT 0 CHECK (max_network_fee_minor >= 0),
    basis_type         TEXT NOT NULL,
    basis_id           UUID NOT NULL,
    attempt            INT NOT NULL DEFAULT 1 CHECK (attempt > 0),
    generation         INT NOT NULL DEFAULT 1 CHECK (generation > 0),
    network_sequence   TEXT NOT NULL DEFAULT '',
    replacement_claimed_by TEXT NOT NULL DEFAULT '',
    replacement_claimed_at TIMESTAMPTZ,
    replacement_lease_expires_at TIMESTAMPTZ,
    expires_at         TIMESTAMPTZ NOT NULL,
    policy_version     TEXT NOT NULL
                       CHECK (policy_version = chain_family || '-v1'),
    status             TEXT NOT NULL DEFAULT 'created'
                       CHECK (status IN ('created','signed','broadcast','superseded','held','failed')),
    authorization_jws  TEXT NOT NULL,
    tx_hash            TEXT NOT NULL DEFAULT '',
    unsigned_fingerprint TEXT NOT NULL CHECK (unsigned_fingerprint <> ''),
    unsigned_envelope  JSONB NOT NULL,
    signed_envelope    JSONB,
    approval_bundle_digest TEXT NOT NULL DEFAULT '',
    approval_bundle_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_evm_signing_authorization_sequence
        CHECK (schema_version <> 2 OR chain_family <> 'evm' OR BTRIM(network_sequence) <> ''),
    CONSTRAINT reg_crypto_signing_authorizations_money_movement_bundle_check CHECK (
        (purpose IN ('withdrawal','treasury_withdrawal') AND approval_bundle_digest <> '' AND approval_bundle_json <> '{}'::jsonb)
        OR
        (purpose NOT IN ('withdrawal','treasury_withdrawal') AND approval_bundle_digest = '' AND approval_bundle_json = '{}'::jsonb)
    )
);

CREATE UNIQUE INDEX idx_crypto_signing_auth_basis_attempt_generation
    ON reg_crypto_signing_authorizations (basis_type, basis_id, purpose, attempt, generation);

CREATE UNIQUE INDEX idx_crypto_signing_auth_authorization
    ON reg_crypto_signing_authorizations (authorization_id);

CREATE UNIQUE INDEX idx_crypto_signing_auth_unsigned_fingerprint
    ON reg_crypto_signing_authorizations (network_id, unsigned_fingerprint);

CREATE INDEX idx_crypto_signing_auth_status
    ON reg_crypto_signing_authorizations (status)
    WHERE status IN ('created','signed','held');

CREATE OR REPLACE FUNCTION fn_crypto_signing_auth_updated_at() RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at := NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_crypto_signing_auth_updated_at
    BEFORE UPDATE ON reg_crypto_signing_authorizations
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_signing_auth_updated_at();

CREATE OR REPLACE FUNCTION fn_crypto_signing_auth_identity_immutable() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.authorization_id IS DISTINCT FROM OLD.authorization_id
       OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
       OR NEW.merchant_id IS DISTINCT FROM OLD.merchant_id
       OR NEW.purpose IS DISTINCT FROM OLD.purpose
       OR NEW.schema_version IS DISTINCT FROM OLD.schema_version
       OR NEW.network_id IS DISTINCT FROM OLD.network_id
       OR NEW.network_code IS DISTINCT FROM OLD.network_code
       OR NEW.chain_family IS DISTINCT FROM OLD.chain_family
       OR NEW.network_identifier IS DISTINCT FROM OLD.network_identifier
       OR NEW.source_address IS DISTINCT FROM OLD.source_address
       OR NEW.fee_payer_address IS DISTINCT FROM OLD.fee_payer_address
       OR NEW.vault_key_id IS DISTINCT FROM OLD.vault_key_id
       OR NEW.destination_address IS DISTINCT FROM OLD.destination_address
       OR NEW.asset_standard IS DISTINCT FROM OLD.asset_standard
       OR NEW.contract_address IS DISTINCT FROM OLD.contract_address
       OR NEW.amount_minor_units IS DISTINCT FROM OLD.amount_minor_units
       OR NEW.max_network_fee_minor IS DISTINCT FROM OLD.max_network_fee_minor
       OR NEW.basis_type IS DISTINCT FROM OLD.basis_type
       OR NEW.basis_id IS DISTINCT FROM OLD.basis_id
       OR NEW.attempt IS DISTINCT FROM OLD.attempt
       OR NEW.generation IS DISTINCT FROM OLD.generation
       OR NEW.network_sequence IS DISTINCT FROM OLD.network_sequence
       OR NEW.expires_at IS DISTINCT FROM OLD.expires_at
       OR NEW.policy_version IS DISTINCT FROM OLD.policy_version
       OR (
            NEW.authorization_jws IS DISTINCT FROM OLD.authorization_jws
            AND NOT (
                OLD.status = 'created'
                AND NEW.status = 'created'
                AND OLD.authorization_jws = ''
                AND BTRIM(NEW.authorization_jws) <> ''
            )
       )
       OR NEW.unsigned_fingerprint IS DISTINCT FROM OLD.unsigned_fingerprint
       OR NEW.unsigned_envelope IS DISTINCT FROM OLD.unsigned_envelope
       OR NEW.approval_bundle_digest IS DISTINCT FROM OLD.approval_bundle_digest
       OR NEW.approval_bundle_json IS DISTINCT FROM OLD.approval_bundle_json
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'signing authorization identity is immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_crypto_signing_auth_identity_immutable
    BEFORE UPDATE ON reg_crypto_signing_authorizations
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_signing_auth_identity_immutable();

-- Immutable description of what a persisted signed envelope is authorized to
-- change on-chain. A tx hash alone is never proof of an expected transfer.
CREATE TABLE reg_crypto_expected_chain_effects (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    signing_authorization_id UUID NOT NULL REFERENCES reg_crypto_signing_authorizations(id),
    ordinal                  INT NOT NULL CHECK (ordinal >= 0),
    network_id               UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    event_kind               TEXT NOT NULL CHECK (event_kind IN ('native_value','contract_log','internal_value')),
    source_address_key       TEXT NOT NULL CHECK (BTRIM(source_address_key) <> ''),
    destination_address_key  TEXT NOT NULL CHECK (BTRIM(destination_address_key) <> ''),
    token_id                 UUID NOT NULL REFERENCES cat_tokens(id),
    contract_address_key     TEXT NOT NULL DEFAULT '',
    amount                   NUMERIC(78,0) NOT NULL CHECK (amount >= 0),
    tx_hash                  TEXT NOT NULL CHECK (BTRIM(tx_hash) <> ''),
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (signing_authorization_id, ordinal)
);

CREATE INDEX idx_crypto_expected_effect_exact
    ON reg_crypto_expected_chain_effects (
        network_id, tx_hash, event_kind, source_address_key,
        destination_address_key, contract_address_key, amount
    );

CREATE TABLE reg_crypto_chain_effect_matches (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    expected_effect_id UUID NOT NULL REFERENCES reg_crypto_expected_chain_effects(id),
    network_id         UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    tx_hash            TEXT NOT NULL CHECK (BTRIM(tx_hash) <> ''),
    event_kind         TEXT NOT NULL CHECK (event_kind IN ('native_value','contract_log','internal_value')),
    event_index        BIGINT NOT NULL CHECK (event_index >= 0),
    matched_source     TEXT NOT NULL DEFAULT 'chain_watcher',
    matched_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (network_id, tx_hash, event_kind, event_index),
    UNIQUE (expected_effect_id)
);

CREATE OR REPLACE FUNCTION fn_crypto_chain_effect_evidence_immutable() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'expected chain effects and matches are immutable';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_crypto_expected_chain_effects_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_expected_chain_effects
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_chain_effect_evidence_immutable();
CREATE TRIGGER trg_crypto_chain_effect_matches_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_chain_effect_matches
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_chain_effect_evidence_immutable();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TABLE IF EXISTS reg_crypto_chain_effect_matches;
DROP TABLE IF EXISTS reg_crypto_expected_chain_effects;
DROP FUNCTION IF EXISTS fn_crypto_chain_effect_evidence_immutable();
DROP TABLE IF EXISTS reg_crypto_signing_authorizations;
DROP FUNCTION IF EXISTS fn_crypto_signing_auth_identity_immutable();
DROP FUNCTION IF EXISTS fn_crypto_signing_auth_updated_at();
-- +goose StatementEnd
