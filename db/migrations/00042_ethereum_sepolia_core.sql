-- +goose Up
-- Description: Ethereum Sepolia catalog, durable EVM nonce state, block history and network holds.
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

CREATE TABLE reg_evm_nonce_cursors (
    network_id      UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    source_address  TEXT NOT NULL,
    next_nonce      NUMERIC(78,0) NOT NULL CHECK (next_nonce >= 0),
    held            BOOLEAN NOT NULL DEFAULT FALSE,
    hold_reason     TEXT NOT NULL DEFAULT '',
    version         INT NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (network_id,source_address)
);

CREATE TABLE reg_evm_nonce_leases (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    network_id       UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    source_address   TEXT NOT NULL,
    nonce            NUMERIC(78,0) NOT NULL CHECK (nonce >= 0),
    basis_type       TEXT NOT NULL,
    basis_id         UUID NOT NULL,
    purpose          TEXT NOT NULL CHECK (purpose IN ('withdrawal','sweep','treasury_withdrawal','activation','native_funding')),
    attempt          INT NOT NULL CHECK (attempt > 0),
    current_generation INT NOT NULL DEFAULT 1 CHECK (current_generation > 0),
    status           TEXT NOT NULL DEFAULT 'reserved'
                     CHECK (status IN ('reserved','signed','broadcast','mined','finalized','superseded','held')),
    hold_reason      TEXT NOT NULL DEFAULT '',
    reserved_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    signed_at        TIMESTAMPTZ,
    broadcast_at     TIMESTAMPTZ,
    mined_at         TIMESTAMPTZ,
    finalized_at     TIMESTAMPTZ,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (network_id,source_address,nonce),
    UNIQUE (basis_type,basis_id,purpose,attempt)
);

CREATE INDEX idx_evm_nonce_leases_reconcile
    ON reg_evm_nonce_leases (network_id,source_address,status,updated_at)
    WHERE status IN ('reserved','signed','broadcast','held');

CREATE TABLE reg_evm_nonce_lease_events (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    -- Deliberately not an FK: an unsigned tail lease may be reclaimed and
    -- deleted, while its immutable audit events must survive.
    lease_id       UUID NOT NULL,
    generation     INT NOT NULL CHECK (generation > 0),
    from_status    TEXT NOT NULL,
    to_status      TEXT NOT NULL,
    tx_hash        TEXT NOT NULL DEFAULT '',
    max_fee_minor  NUMERIC(78,0) NOT NULL DEFAULT 0 CHECK (max_fee_minor >= 0),
    metadata       JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE OR REPLACE FUNCTION fn_evm_nonce_lease_events_immutable() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'EVM nonce lease events are immutable';
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_evm_nonce_lease_events_immutable
    BEFORE UPDATE OR DELETE ON reg_evm_nonce_lease_events
    FOR EACH ROW EXECUTE FUNCTION fn_evm_nonce_lease_events_immutable();

CREATE TABLE reg_crypto_network_holds (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    network_id  UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    reason      TEXT NOT NULL CHECK (reason IN ('deep_reorg','posted_payment_reorg','nonce_divergence','external_outflow','manual')),
    owner_type  TEXT NOT NULL CHECK (BTRIM(owner_type) <> ''),
    owner_id    TEXT NOT NULL CHECK (BTRIM(owner_id) <> ''),
    details     JSONB NOT NULL DEFAULT '{}'::jsonb,
    status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved')),
    held_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    held_by     TEXT NOT NULL DEFAULT 'crypto-worker' CHECK (BTRIM(held_by) <> ''),
    resolved_at TIMESTAMPTZ,
    resolved_by TEXT NOT NULL DEFAULT '',
    resolution  TEXT NOT NULL DEFAULT '',
    CONSTRAINT chk_network_hold_resolution CHECK (
      (status='open' AND resolved_at IS NULL)
      OR (status='resolved' AND resolved_at IS NOT NULL AND resolved_by<>'' AND resolution<>'')
    )
);
CREATE UNIQUE INDEX idx_crypto_network_holds_open
    ON reg_crypto_network_holds (network_id,owner_type,owner_id) WHERE status='open';
CREATE INDEX idx_crypto_network_holds_open_network
    ON reg_crypto_network_holds (network_id) WHERE status='open';

CREATE OR REPLACE FUNCTION fn_crypto_hold_owner_guard() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP='DELETE' OR NEW.network_id<>OLD.network_id OR NEW.owner_type<>OLD.owner_type
       OR NEW.owner_id<>OLD.owner_id OR NEW.reason<>OLD.reason
       OR NEW.details IS DISTINCT FROM OLD.details OR NEW.held_at<>OLD.held_at
       OR NEW.held_by<>OLD.held_by OR OLD.status='resolved'
       OR (OLD.status='open' AND NEW.status NOT IN ('open','resolved')) THEN
        RAISE EXCEPTION 'operational hold acquisition identity is immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_network_hold_owner_guard
    BEFORE UPDATE OR DELETE ON reg_crypto_network_holds
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_hold_owner_guard();

CREATE TABLE sys_chain_watcher_block_history (
    network_id   UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    watcher_key  TEXT NOT NULL,
    block_number BIGINT NOT NULL CHECK (block_number >= 0),
    block_hash   TEXT NOT NULL,
    block_timestamp BIGINT NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (network_id,watcher_key,block_number)
);

CREATE TABLE reg_crypto_security_incidents (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    network_id     UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    payment_id     UUID REFERENCES doc_crypto_payments(id),
    incident_kind  TEXT NOT NULL CHECK (incident_kind IN ('posted_payment_reorg','deep_reorg','nonce_divergence')),
    tx_hash        TEXT NOT NULL DEFAULT '',
    event_kind     TEXT NOT NULL DEFAULT 'native_value' CHECK (event_kind IN ('native_value','contract_log','internal_value')),
    event_index    BIGINT NOT NULL DEFAULT 0 CHECK (event_index >= 0),
    details        JSONB NOT NULL DEFAULT '{}'::jsonb,
    status         TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','acknowledged','resolved')),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    acknowledged_at TIMESTAMPTZ,
    resolved_at    TIMESTAMPTZ,
    UNIQUE (network_id,incident_kind,tx_hash,event_kind,event_index)
);

INSERT INTO cat_blockchain_networks (
    id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals,
    confirmations_needed,block_time_seconds,explorer_url,finality_mode,is_active
) VALUES (
    '11155111-0000-4000-8000-000000000001','ETHEREUM-SEPOLIA','Ethereum Sepolia','evm','11155111','ETH',18,
    12,12,'https://sepolia.etherscan.io/tx/{txHash}','confirmations_or_finalized',FALSE
) ON CONFLICT (code) WHERE deletion_mark=FALSE DO NOTHING;

INSERT INTO cat_tokens (
    id,code,name,network_id,contract_address,symbol,decimal_places,token_standard,is_active
) VALUES
    ('11155111-0000-4000-8000-000000000002','ETH-SEPOLIA','Ethereum Sepolia ETH',
     '11155111-0000-4000-8000-000000000001','', 'ETH',18,'native',TRUE),
    ('11155111-0000-4000-8000-000000000003','PYUSD-SEPOLIA','PayPal USD',
     '11155111-0000-4000-8000-000000000001','0xCaC524BcA292aaade2DF8A05cC58F0a65B1B3bB9','PYUSD',6,'ERC-20',TRUE)
ON CONFLICT (code) WHERE deletion_mark=FALSE DO NOTHING;

CREATE UNIQUE INDEX idx_cat_tokens_contract_network
    ON cat_tokens (network_id,LOWER(contract_address))
    WHERE deletion_mark=FALSE AND contract_address <> '';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM reg_crypto_signing_authorizations
        WHERE network_id='11155111-0000-4000-8000-000000000001'
    ) OR EXISTS (
        SELECT 1 FROM doc_crypto_payments
        WHERE network_id='11155111-0000-4000-8000-000000000001'
    ) THEN
        RAISE EXCEPTION 'cannot remove Ethereum Sepolia while financial records reference it';
    END IF;
END $$;

DROP TRIGGER IF EXISTS trg_evm_nonce_lease_events_immutable ON reg_evm_nonce_lease_events;
DROP FUNCTION IF EXISTS fn_evm_nonce_lease_events_immutable();
DROP TABLE IF EXISTS reg_evm_nonce_lease_events;
DROP TABLE IF EXISTS reg_evm_nonce_leases;
DROP TABLE IF EXISTS reg_evm_nonce_cursors;
DROP TABLE IF EXISTS reg_crypto_security_incidents;
DROP TABLE IF EXISTS sys_chain_watcher_block_history;
DROP INDEX IF EXISTS idx_crypto_network_holds_open;
DROP TABLE IF EXISTS reg_crypto_network_holds;
DROP FUNCTION IF EXISTS fn_crypto_hold_owner_guard();

DELETE FROM sys_crypto_network_policies WHERE network_id='11155111-0000-4000-8000-000000000001';
DELETE FROM sys_crypto_wallet_pool_policies WHERE network_id='11155111-0000-4000-8000-000000000001';
DROP INDEX IF EXISTS idx_cat_tokens_contract_network;
DELETE FROM cat_tokens WHERE id IN (
    '11155111-0000-4000-8000-000000000002','11155111-0000-4000-8000-000000000003'
);
DELETE FROM cat_blockchain_networks WHERE id='11155111-0000-4000-8000-000000000001';

-- +goose StatementEnd
