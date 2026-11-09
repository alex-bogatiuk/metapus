-- +goose Up
-- Description: Production-ready multichain crypto core (TRON v1)

-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- Operational policy is fail-closed. Runtime readiness (RPC roles and signer)
-- is checked by the application immediately before an enable/update commit.
CREATE TABLE sys_crypto_network_policies (
    network_id                         UUID PRIMARY KEY REFERENCES cat_blockchain_networks(id),
    enabled                            BOOLEAN NOT NULL DEFAULT FALSE,
    per_transaction_max_fee_minor      NUMERIC(78,0) NOT NULL DEFAULT 0 CHECK (per_transaction_max_fee_minor >= 0),
    daily_network_fee_budget_minor     NUMERIC(78,0) NOT NULL DEFAULT 0 CHECK (daily_network_fee_budget_minor >= 0),
    fee_funding_mode                   TEXT NOT NULL DEFAULT 'source_only'
                                       CHECK (fee_funding_mode IN ('source_only','jit')),
    daily_jit_funding_budget_minor     NUMERIC(78,0) NOT NULL DEFAULT 0 CHECK (daily_jit_funding_budget_minor >= 0),
    min_fee_wallet_balance_minor       NUMERIC(78,0) NOT NULL DEFAULT 0 CHECK (min_fee_wallet_balance_minor >= 0),
    min_source_wallet_residual_minor   NUMERIC(78,0) NOT NULL DEFAULT 0 CHECK (min_source_wallet_residual_minor >= 0),
    jit_funding_max_amount_minor       NUMERIC(78,0) NOT NULL DEFAULT 0 CHECK (jit_funding_max_amount_minor >= 0),
    retry_delay_seconds                INT NOT NULL DEFAULT 30 CHECK (retry_delay_seconds > 0),
    rpc_failure_threshold              INT NOT NULL DEFAULT 5 CHECK (rpc_failure_threshold > 0),
    rpc_open_seconds                   INT NOT NULL DEFAULT 30 CHECK (rpc_open_seconds > 0),
    version                            INT NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_by                         TEXT NOT NULL DEFAULT '',
    created_at                         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid                              BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at                        TIMESTAMPTZ,
    CONSTRAINT chk_crypto_network_policy_enablement CHECK (
        enabled = FALSE OR (
            per_transaction_max_fee_minor > 0
            AND daily_network_fee_budget_minor > 0
            AND (
                fee_funding_mode = 'source_only'
                OR (
                    jit_funding_max_amount_minor >= per_transaction_max_fee_minor + min_source_wallet_residual_minor
                    AND daily_jit_funding_budget_minor > jit_funding_max_amount_minor
                )
            )
        )
    )
);

-- External resource acquisition is a chain-family capability, not a property
-- of native fee funding. Families without that capability have no row here.
CREATE TABLE sys_crypto_external_resource_policies (
    network_id                 UUID PRIMARY KEY REFERENCES cat_blockchain_networks(id),
    resource_strategy          TEXT NOT NULL CHECK (resource_strategy IN ('rental_required','prefer_rental','burn_only')),
    provider_account_ids       UUID[] NOT NULL DEFAULT '{}',
    provider_failure_threshold INT NOT NULL CHECK (provider_failure_threshold > 0),
    provider_open_seconds      INT NOT NULL CHECK (provider_open_seconds > 0),
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_crypto_external_resource_providers CHECK (
        resource_strategy = 'burn_only' OR cardinality(provider_account_ids) > 0
    )
);

COMMENT ON TABLE sys_crypto_external_resource_policies IS
    'Optional external resource rental policy for chain families that declare the capability';

CREATE OR REPLACE FUNCTION initialize_crypto_network_policy()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO sys_crypto_network_policies (network_id)
    VALUES (NEW.id)
    ON CONFLICT (network_id) DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_blockchain_network_init_crypto_policy
    AFTER INSERT ON cat_blockchain_networks
    FOR EACH ROW EXECUTE FUNCTION initialize_crypto_network_policy();

INSERT INTO sys_crypto_network_policies (network_id)
SELECT id FROM cat_blockchain_networks
ON CONFLICT (network_id) DO NOTHING;

CREATE TRIGGER trg_crypto_network_policies_updated_at
    BEFORE UPDATE ON sys_crypto_network_policies
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_crypto_network_policies_txid
    BEFORE UPDATE ON sys_crypto_network_policies
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

INSERT INTO auth_permissions (code,name,description,resource,action) VALUES
    ('crypto_network:read','Чтение криптосетей','View crypto network policies and runtime health','crypto_network','read'),
    ('crypto_network:update','Настройка криптосетей','Update guarded crypto network execution policies','crypto_network','update')
ON CONFLICT (code) DO NOTHING;
INSERT INTO auth_role_permissions (role_id,permission_id)
SELECT 'b0000000-0000-0000-0000-000000000001',id FROM auth_permissions WHERE resource='crypto_network'
ON CONFLICT DO NOTHING;

-- Worker-published health snapshot consumed by the tenant Admin API. It holds
-- only provider IDs/roles and sanitized errors; URLs, API keys, addresses, and
-- Vault paths are deliberately absent.
CREATE TABLE sys_crypto_network_runtime_status (
    network_id              UUID PRIMARY KEY REFERENCES cat_blockchain_networks(id),
    rpc_endpoints           JSONB NOT NULL DEFAULT '[]'::jsonb,
    signer_ready            BOOLEAN NOT NULL DEFAULT FALSE,
    signer_error            TEXT NOT NULL DEFAULT '',
    resource_provider_ready BOOLEAN NOT NULL DEFAULT FALSE,
    resource_provider_state TEXT NOT NULL DEFAULT 'unconfigured',
    resource_providers      JSONB NOT NULL DEFAULT '[]'::jsonb,
    observed_at             TIMESTAMPTZ NOT NULL,
    worker_id               TEXT NOT NULL DEFAULT '',
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Reservations fence the worst-case native fee before any signature exists.
-- The append-only event stream is the audit source for every state change.
CREATE TABLE reg_crypto_fee_reservations (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    network_id            UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    payer_wallet_id       UUID NOT NULL REFERENCES cat_wallets(id),
    native_token_id       UUID NOT NULL REFERENCES cat_tokens(id),
    operation_token_id    UUID NOT NULL REFERENCES cat_tokens(id),
    reserved_principal_minor NUMERIC(78,0) NOT NULL DEFAULT 0 CHECK (reserved_principal_minor >= 0),
    basis_type            TEXT NOT NULL,
    basis_id              UUID NOT NULL,
    purpose               TEXT NOT NULL CHECK (purpose IN ('withdrawal','sweep','treasury_withdrawal','activation','native_funding')),
    attempt               INT NOT NULL CHECK (attempt > 0),
    strategy              TEXT NOT NULL CHECK (strategy IN ('resource_rental','native_burn','native_funding')),
    max_fee_minor         NUMERIC(78,0) NOT NULL CHECK (max_fee_minor > 0),
    actual_fee_minor      NUMERIC(78,0) NOT NULL DEFAULT 0 CHECK (actual_fee_minor >= 0),
    unsigned_fingerprint  TEXT NOT NULL DEFAULT '',
    tx_hash               TEXT NOT NULL DEFAULT '',
    status                TEXT NOT NULL DEFAULT 'reserved'
                          CHECK (status IN ('reserved','broadcast','settled','released','held')),
    hold_reason           TEXT NOT NULL DEFAULT '',
    reserved_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    broadcast_at          TIMESTAMPTZ,
    settled_at            TIMESTAMPTZ,
    released_at           TIMESTAMPTZ,
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_crypto_fee_reservation_actual
        CHECK (status <> 'settled' OR actual_fee_minor <= max_fee_minor)
);

CREATE UNIQUE INDEX idx_crypto_fee_reservation_operation_attempt
    ON reg_crypto_fee_reservations (basis_type, basis_id, purpose, attempt);
CREATE INDEX idx_crypto_fee_reservation_budget
    ON reg_crypto_fee_reservations (network_id, reserved_at, status);
CREATE INDEX idx_crypto_fee_reservation_active_wallet
    ON reg_crypto_fee_reservations (payer_wallet_id, status)
    WHERE status IN ('reserved','broadcast','held');

CREATE TRIGGER trg_crypto_fee_reservations_updated_at
    BEFORE UPDATE ON reg_crypto_fee_reservations
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE reg_crypto_fee_reservation_events (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    reservation_id     UUID NOT NULL REFERENCES reg_crypto_fee_reservations(id),
    from_status        TEXT NOT NULL,
    to_status          TEXT NOT NULL,
    reason             TEXT NOT NULL DEFAULT '',
    amount_minor       NUMERIC(78,0) NOT NULL DEFAULT 0 CHECK (amount_minor >= 0),
    metadata           JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_crypto_fee_reservation_events_reservation
    ON reg_crypto_fee_reservation_events (reservation_id, created_at, id);

CREATE OR REPLACE FUNCTION fn_crypto_fee_reservation_events_immutable() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'crypto fee reservation events are immutable';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_crypto_fee_reservation_events_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_fee_reservation_events
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_fee_reservation_events_immutable();

-- Idempotent dedicated fee-wallet -> source-wallet native transfer.
CREATE TABLE doc_crypto_native_fundings (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    number                TEXT NOT NULL DEFAULT '',
    date                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    basis_type            TEXT NOT NULL,
    basis_id              UUID NOT NULL,
    attempt               INT NOT NULL CHECK (attempt > 0),
    network_id            UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    native_token_id       UUID NOT NULL REFERENCES cat_tokens(id),
    source_wallet_id      UUID NOT NULL REFERENCES cat_wallets(id),
    destination_wallet_id UUID NOT NULL REFERENCES cat_wallets(id),
    amount_minor          NUMERIC(78,0) NOT NULL CHECK (amount_minor > 0),
    fee_reservation_id    UUID REFERENCES reg_crypto_fee_reservations(id),
    signing_authorization_id UUID REFERENCES reg_crypto_signing_authorizations(id),
    unsigned_fingerprint  TEXT NOT NULL DEFAULT '',
    signed_envelope       JSONB,
    tx_hash               TEXT NOT NULL DEFAULT '',
    status                TEXT NOT NULL DEFAULT 'created'
                          CHECK (status IN ('created','reserved','signed','broadcast','solidified','settled','retry_wait','held','failed')),
    last_error            TEXT NOT NULL DEFAULT '',
    next_attempt_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    claimed_by            TEXT NOT NULL DEFAULT '',
    claimed_at            TIMESTAMPTZ,
    lease_expires_at      TIMESTAMPTZ,
    solidified_at         TIMESTAMPTZ,
    settled_at            TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    version               INT NOT NULL DEFAULT 1 CHECK (version > 0),
    CONSTRAINT chk_crypto_native_funding_wallets
        CHECK (source_wallet_id <> destination_wallet_id)
);

CREATE UNIQUE INDEX idx_crypto_native_funding_basis_attempt
    ON doc_crypto_native_fundings (basis_type, basis_id, attempt);
CREATE UNIQUE INDEX idx_crypto_native_funding_tx_hash
    ON doc_crypto_native_fundings (network_id, tx_hash) WHERE tx_hash <> '';
CREATE INDEX idx_crypto_native_funding_worker
    ON doc_crypto_native_fundings (next_attempt_at, lease_expires_at)
    WHERE status IN ('broadcast','solidified');

CREATE TRIGGER trg_crypto_native_fundings_updated_at
    BEFORE UPDATE ON doc_crypto_native_fundings
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Normalized settlement is queryable without understanding family-specific
-- receipts. Raw evidence remains immutable alongside analytical components.
CREATE TABLE reg_crypto_chain_settlements (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    network_id            UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    basis_type            TEXT NOT NULL,
    basis_id              UUID NOT NULL,
    tx_hash               TEXT NOT NULL,
    status                TEXT NOT NULL CHECK (status IN ('succeeded','reverted')),
    block_hash            TEXT NOT NULL,
    block_height          BIGINT NOT NULL CHECK (block_height >= 0),
    finality_status       TEXT NOT NULL CHECK (finality_status IN ('solidified','finalized')),
    native_token_id       UUID NOT NULL REFERENCES cat_tokens(id),
    total_fee_minor       NUMERIC(78,0) NOT NULL CHECK (total_fee_minor >= 0),
    raw_receipt           JSONB NOT NULL,
    observed_at           TIMESTAMPTZ NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_crypto_chain_settlement_tx
    ON reg_crypto_chain_settlements (network_id, tx_hash);
CREATE UNIQUE INDEX idx_crypto_chain_settlement_basis
    ON reg_crypto_chain_settlements (basis_type, basis_id);

ALTER TABLE doc_crypto_network_operations
    ADD CONSTRAINT fk_crypto_network_operation_settlement
    FOREIGN KEY (chain_settlement_id) REFERENCES reg_crypto_chain_settlements(id);

CREATE TABLE reg_crypto_chain_settlement_components (
    settlement_id   UUID NOT NULL REFERENCES reg_crypto_chain_settlements(id),
    component       TEXT NOT NULL,
    amount_minor    NUMERIC(78,0) NOT NULL DEFAULT 0 CHECK (amount_minor >= 0),
    quantity        BIGINT NOT NULL DEFAULT 0 CHECK (quantity >= 0),
    metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (settlement_id, component)
);

CREATE OR REPLACE FUNCTION fn_crypto_chain_settlement_evidence_immutable() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'crypto chain settlement evidence is immutable';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_crypto_chain_settlements_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_chain_settlements
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_chain_settlement_evidence_immutable();

CREATE TRIGGER trg_crypto_chain_settlement_components_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_chain_settlement_components
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_chain_settlement_evidence_immutable();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

DELETE FROM auth_role_permissions WHERE permission_id IN (
    SELECT id FROM auth_permissions WHERE resource = 'crypto_network'
);
DELETE FROM auth_permissions WHERE resource = 'crypto_network';

-- The normalized settlement evidence introduced by this migration cannot
-- exist on the previous schema version. Return finalized operations to their
-- pre-settlement, retryable state before removing the evidence table; leaving
-- dangling UUIDs here makes a later Up unable to restore the foreign key.
UPDATE doc_crypto_network_operations
SET status = 'broadcast',
    chain_settlement_id = NULL,
    finalized_at = NULL
WHERE chain_settlement_id IS NOT NULL;

ALTER TABLE doc_crypto_network_operations
    DROP CONSTRAINT IF EXISTS fk_crypto_network_operation_settlement;
DROP TABLE IF EXISTS reg_crypto_chain_settlement_components;
DROP TRIGGER IF EXISTS trg_crypto_chain_settlements_immutable ON reg_crypto_chain_settlements;
DROP FUNCTION IF EXISTS fn_crypto_chain_settlement_evidence_immutable();
DROP TABLE IF EXISTS reg_crypto_chain_settlements;
DROP TABLE IF EXISTS doc_crypto_native_fundings;
DROP TRIGGER IF EXISTS trg_crypto_fee_reservation_events_immutable ON reg_crypto_fee_reservation_events;
DROP FUNCTION IF EXISTS fn_crypto_fee_reservation_events_immutable();
DROP TABLE IF EXISTS reg_crypto_fee_reservation_events;
DROP TABLE IF EXISTS reg_crypto_fee_reservations;

DROP TRIGGER IF EXISTS trg_blockchain_network_init_crypto_policy ON cat_blockchain_networks;
DROP FUNCTION IF EXISTS initialize_crypto_network_policy();
DROP TABLE IF EXISTS sys_crypto_network_runtime_status;
DROP TABLE IF EXISTS sys_crypto_external_resource_policies;
DROP TABLE IF EXISTS sys_crypto_network_policies;

-- +goose StatementEnd
