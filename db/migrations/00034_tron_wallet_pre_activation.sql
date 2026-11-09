-- +goose Up
-- Description: Chain wallet activation workflow and CryptoNetworkOperation document

-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

CREATE TABLE doc_crypto_network_operations (
    id                    UUID PRIMARY KEY,
    number                TEXT NOT NULL DEFAULT '',
    date                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    basis_type            TEXT NOT NULL DEFAULT '',
    basis_id              UUID,
    posted                BOOLEAN NOT NULL DEFAULT FALSE,
    posted_version        INT NOT NULL DEFAULT 0,
    deletion_mark         BOOLEAN NOT NULL DEFAULT FALSE,
    version               INT NOT NULL DEFAULT 1 CHECK (version > 0),
    attributes            JSONB NOT NULL DEFAULT '{}'::jsonb,
    description           TEXT NOT NULL DEFAULT '',
    created_by            UUID,
    updated_by            UUID,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    operation_type        TEXT NOT NULL
                          CHECK (operation_type IN ('wallet_activation')),
    status                TEXT NOT NULL DEFAULT 'broadcast'
                          CHECK (status IN ('broadcast','succeeded','reverted')),
    network_id            UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    target_wallet_id      UUID NOT NULL REFERENCES cat_wallets(id),
    target_address        TEXT NOT NULL CHECK (BTRIM(target_address) <> ''),
    source_wallet_id      UUID NOT NULL REFERENCES cat_wallets(id),
    cost_token_id         UUID NOT NULL REFERENCES cat_tokens(id),
    cost_amount           NUMERIC(78,0) NOT NULL DEFAULT 0 CHECK (cost_amount >= 0),
    tx_hash               TEXT NOT NULL CHECK (BTRIM(tx_hash) <> ''),
    -- The FK is added in 00041 after the normalized settlement table exists.
    chain_settlement_id   UUID,
    finalized_at          TIMESTAMPTZ,
    manual_edit_allowed   BOOLEAN NOT NULL DEFAULT FALSE,
    _txid                 BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at           TIMESTAMPTZ,

    CONSTRAINT chk_crypto_network_operation_wallets
        CHECK (source_wallet_id <> target_wallet_id),
    CONSTRAINT chk_crypto_network_operation_finality
        CHECK (
            (status = 'broadcast' AND chain_settlement_id IS NULL AND finalized_at IS NULL)
            OR
            (status IN ('succeeded','reverted') AND chain_settlement_id IS NOT NULL AND finalized_at IS NOT NULL)
        )
);

CREATE UNIQUE INDEX idx_crypto_network_operations_chain_tx
    ON doc_crypto_network_operations (operation_type, network_id, tx_hash);
CREATE UNIQUE INDEX idx_crypto_network_operations_basis
    ON doc_crypto_network_operations (basis_type, basis_id)
    WHERE basis_id IS NOT NULL;
CREATE INDEX idx_crypto_network_operations_date
    ON doc_crypto_network_operations (date DESC, id DESC)
    WHERE deletion_mark = FALSE;

CREATE OR REPLACE FUNCTION fn_crypto_network_operations_cdc() RETURNS TRIGGER AS $$
BEGIN
    NEW._txid := txid_current();
    NEW.updated_at := NOW();
    IF OLD.deletion_mark = FALSE AND NEW.deletion_mark = TRUE THEN
        NEW._deleted_at := NOW();
    ELSIF OLD.deletion_mark = TRUE AND NEW.deletion_mark = FALSE THEN
        NEW._deleted_at := NULL;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_crypto_network_operations_cdc
    BEFORE UPDATE ON doc_crypto_network_operations
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_network_operations_cdc();

-- Worker-only execution state. Blockchain and accounting facts are reached
-- through network_operation_id and are never duplicated here.
CREATE TABLE reg_crypto_account_activations (
    id                         UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    wallet_id                  UUID NOT NULL REFERENCES cat_wallets(id),
    network_id                 UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    network_operation_id       UUID UNIQUE REFERENCES doc_crypto_network_operations(id),
    status                     TEXT NOT NULL DEFAULT 'claimed'
                               CHECK (status IN ('claimed','signing','broadcast','confirmed','active','held','failed')),
    error_message              TEXT NOT NULL DEFAULT '',
    broadcast_at               TIMESTAMPTZ,
    confirmed_at               TIMESTAMPTZ,
    activation_cost_reserved_minor NUMERIC(78,0) NOT NULL DEFAULT 0
                                   CHECK (activation_cost_reserved_minor >= 0),
    activation_cost_reserved_at TIMESTAMPTZ,
    activation_attempts        INT NOT NULL DEFAULT 0 CHECK (activation_attempts >= 0),
    activation_next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    activation_claimed_by      TEXT NOT NULL DEFAULT '',
    activation_claim_token     UUID,
    activation_lease_expires_at TIMESTAMPTZ,
    cost_status                TEXT NOT NULL DEFAULT 'unknown'
                               CHECK (cost_status IN ('unknown','repairing','recorded','not_applicable','held')),
    cost_checked_at            TIMESTAMPTZ,
    cost_error                 TEXT NOT NULL DEFAULT '',
    cost_claimed_by            TEXT NOT NULL DEFAULT '',
    cost_claimed_at            TIMESTAMPTZ,
    cost_lease_expires_at      TIMESTAMPTZ,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_crypto_account_activations_active_wallet
    ON reg_crypto_account_activations (network_id, wallet_id)
    WHERE status IN ('claimed','signing','broadcast','confirmed','held');
CREATE INDEX idx_crypto_account_activations_status
    ON reg_crypto_account_activations (status, updated_at);
CREATE INDEX idx_crypto_activation_budget_reservations
    ON reg_crypto_account_activations (network_id, status)
    WHERE activation_cost_reserved_at IS NOT NULL;
CREATE INDEX idx_crypto_activation_claim
    ON reg_crypto_account_activations (activation_next_attempt_at, updated_at)
    WHERE status IN ('claimed','signing','broadcast','confirmed','held');
CREATE INDEX idx_crypto_account_activations_cost_repair
    ON reg_crypto_account_activations (cost_status, cost_lease_expires_at, updated_at)
    WHERE network_operation_id IS NOT NULL
      AND status IN ('confirmed','active','failed')
      AND cost_status IN ('unknown','repairing','held');
CREATE TRIGGER trg_crypto_account_activations_updated_at
    BEFORE UPDATE ON reg_crypto_account_activations
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

INSERT INTO auth_permissions (code, name, description, resource, action) VALUES
    ('document:crypto_network_operation:read', 'Чтение сетевых криптоопераций',
     'View blockchain network operation documents', 'document:crypto_network_operation', 'read'),
    ('document:crypto_network_operation:update', 'Изменение сетевых криптоопераций',
     'Modify an explicitly unlocked network operation', 'document:crypto_network_operation', 'update'),
    ('document:crypto_network_operation:post', 'Проведение сетевых криптоопераций',
     'Post an explicitly unlocked network operation', 'document:crypto_network_operation', 'post'),
    ('document:crypto_network_operation:unpost', 'Распроведение сетевых криптоопераций',
     'Unpost an explicitly unlocked network operation', 'document:crypto_network_operation', 'unpost'),
    ('document:crypto_network_operation:delete', 'Пометка удаления сетевых криптоопераций',
     'Set the deletion mark on an explicitly unlocked network operation', 'document:crypto_network_operation', 'delete'),
    ('crypto.network_operation.allow_editing', 'Ручная корректировка сетевых криптоопераций',
     'Unlock a finalized blockchain operation after fresh MFA step-up', 'document:crypto_network_operation', 'unlock')
ON CONFLICT (code) DO NOTHING;

INSERT INTO auth_role_permissions (role_id, permission_id)
SELECT 'b0000000-0000-0000-0000-000000000001', id
FROM auth_permissions WHERE resource = 'document:crypto_network_operation'
ON CONFLICT DO NOTHING;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

DELETE FROM auth_role_permissions WHERE permission_id IN (
    SELECT id FROM auth_permissions WHERE resource = 'document:crypto_network_operation'
);
DELETE FROM auth_permissions WHERE resource = 'document:crypto_network_operation';

DROP TABLE IF EXISTS reg_crypto_account_activations CASCADE;
DROP TRIGGER IF EXISTS trg_crypto_network_operations_cdc ON doc_crypto_network_operations;
DROP FUNCTION IF EXISTS fn_crypto_network_operations_cdc();
DROP TABLE IF EXISTS doc_crypto_network_operations CASCADE;
DELETE FROM reg_crypto_signing_authorizations WHERE purpose = 'activation';

-- +goose StatementEnd
