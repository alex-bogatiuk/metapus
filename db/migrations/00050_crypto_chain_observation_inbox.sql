-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
CREATE TABLE reg_crypto_chain_observations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    source_stream TEXT NOT NULL,
    tx_hash TEXT NOT NULL,
    event_kind TEXT NOT NULL,
    event_index BIGINT NOT NULL,
    block_number BIGINT NOT NULL,
    block_hash TEXT NOT NULL,
    payload_hash TEXT NOT NULL,
    payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','processing','processed','needs_review')),
    attempts INT NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    lease_owner TEXT,
    lease_expires_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ,
    UNIQUE (network_id, payload_hash)
);
CREATE INDEX idx_crypto_chain_observations_due
    ON reg_crypto_chain_observations(next_attempt_at,id)
    WHERE status IN ('pending','processing');
CREATE FUNCTION fn_crypto_chain_observation_evidence_guard() RETURNS TRIGGER AS $$
BEGIN
    IF (OLD.network_id,OLD.source_stream,OLD.tx_hash,OLD.event_kind,OLD.event_index,
        OLD.block_number,OLD.block_hash,OLD.payload_hash,OLD.payload)
       IS DISTINCT FROM
       (NEW.network_id,NEW.source_stream,NEW.tx_hash,NEW.event_kind,NEW.event_index,
        NEW.block_number,NEW.block_hash,NEW.payload_hash,NEW.payload) THEN
        RAISE EXCEPTION 'chain observation evidence is immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_chain_observation_evidence_guard
    BEFORE UPDATE ON reg_crypto_chain_observations
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_chain_observation_evidence_guard();

CREATE TABLE sys_crypto_block_ingestion (
    network_id UUID PRIMARY KEY REFERENCES cat_blockchain_networks(id),
    start_height BIGINT NOT NULL CHECK (start_height >= 0),
    start_hash TEXT NOT NULL CHECK (start_hash <> ''),
    start_timestamp BIGINT NOT NULL,
    mode TEXT NOT NULL CHECK (mode IN ('shadow','solidified','hybrid','paused')),
    decoder_version INT NOT NULL DEFAULT 2 CHECK (decoder_version = 2),
    activated_at TIMESTAMPTZ,
    shadow_paused BOOLEAN NOT NULL DEFAULT FALSE,
    shadow_control_version INT NOT NULL DEFAULT 1 CHECK (shadow_control_version > 0),
    last_error_code TEXT NOT NULL DEFAULT '',
    last_error_stream TEXT NOT NULL DEFAULT '',
    last_error_height BIGINT,
    last_error_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ
);
CREATE TABLE sys_crypto_watch_targets (
    network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    target_kind TEXT NOT NULL CHECK (target_kind IN ('address','asset')),
    target_key TEXT NOT NULL CHECK (target_key <> ''),
    owner_id UUID NOT NULL,
    start_height BIGINT NOT NULL CHECK (start_height >= 0),
    source TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ,
    PRIMARY KEY (network_id,target_kind,target_key)
);
CREATE TABLE reg_crypto_transfer_verifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    tx_hash TEXT NOT NULL,
    event_kind TEXT NOT NULL CHECK (event_kind IN ('native_value','contract_log','internal_value')),
    event_index BIGINT NOT NULL CHECK (event_index >= 0),
    block_number BIGINT NOT NULL CHECK (block_number >= 0),
    block_hash TEXT NOT NULL CHECK (block_hash <> ''),
    economics_hash TEXT NOT NULL,
    evidence_hash TEXT NOT NULL,
    confirmations BIGINT NOT NULL CHECK (confirmations >= 1),
    evidence JSONB NOT NULL,
    verified_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ,
    UNIQUE (network_id,tx_hash,event_kind,event_index,block_hash,economics_hash,confirmations,evidence_hash)
);
CREATE INDEX IF NOT EXISTS idx_crypto_transfer_verifications_identity
    ON reg_crypto_transfer_verifications(network_id,tx_hash,event_kind,event_index,confirmations DESC);
ALTER TABLE doc_crypto_payments ADD COLUMN IF NOT EXISTS verification_id UUID
    REFERENCES reg_crypto_transfer_verifications(id);
ALTER TABLE reg_crypto_wallet_fundings ADD COLUMN IF NOT EXISTS verification_id UUID
    REFERENCES reg_crypto_transfer_verifications(id);

CREATE OR REPLACE FUNCTION fn_crypto_transfer_verification_immutable() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'transfer verification evidence is immutable';
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS trg_crypto_transfer_verification_immutable ON reg_crypto_transfer_verifications;
CREATE TRIGGER trg_crypto_transfer_verification_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_transfer_verifications
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_transfer_verification_immutable();

CREATE OR REPLACE FUNCTION fn_crypto_register_watch_address() RETURNS TRIGGER AS $$
DECLARE
    boundary BIGINT;
BEGIN
    IF NEW.vault_key_id = '' OR NEW.address = '' OR NOT EXISTS(SELECT 1 FROM cat_blockchain_networks WHERE id=NEW.network_id AND chain_family='tron') THEN RETURN NEW; END IF;
    -- Capture takes this same network lock. No RPC is performed under it.
    SELECT start_height+1 INTO boundary FROM sys_crypto_block_ingestion
      WHERE network_id=NEW.network_id FOR UPDATE;
    IF boundary IS NULL THEN boundary := 0; END IF;
    SELECT GREATEST(boundary,COALESCE(MAX(last_block)+1,boundary)) INTO boundary
      FROM sys_chain_watcher_state WHERE network_id=NEW.network_id AND watcher_key='tron:solidified';
    INSERT INTO sys_crypto_watch_targets(network_id,target_kind,target_key,owner_id,start_height,source)
      VALUES(NEW.network_id,'address',NEW.address,NEW.id,boundary,'wallet_write')
      ON CONFLICT(network_id,target_kind,target_key) DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS trg_crypto_register_watch_address ON cat_wallets;
CREATE TRIGGER trg_crypto_register_watch_address
    AFTER INSERT OR UPDATE OF address,network_id,vault_key_id ON cat_wallets
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_register_watch_address();

CREATE OR REPLACE FUNCTION fn_crypto_register_watch_asset() RETURNS TRIGGER AS $$
DECLARE
    boundary BIGINT;
BEGIN
    IF NOT NEW.is_active OR NEW.deletion_mark OR NEW.contract_address='' OR NEW.token_standard<>'TRC-20' OR NOT EXISTS(SELECT 1 FROM cat_blockchain_networks WHERE id=NEW.network_id AND chain_family='tron') THEN RETURN NEW; END IF;
    SELECT start_height+1 INTO boundary FROM sys_crypto_block_ingestion
      WHERE network_id=NEW.network_id FOR UPDATE;
    IF boundary IS NULL THEN boundary := 0; END IF;
    SELECT GREATEST(boundary,COALESCE(MAX(last_block)+1,boundary)) INTO boundary
      FROM sys_chain_watcher_state WHERE network_id=NEW.network_id AND watcher_key='tron:solidified';
    INSERT INTO sys_crypto_watch_targets(network_id,target_kind,target_key,owner_id,start_height,source)
      VALUES(NEW.network_id,'asset',NEW.contract_address,NEW.id,boundary,'asset_write')
      ON CONFLICT(network_id,target_kind,target_key) DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS trg_crypto_register_watch_asset ON cat_tokens;
CREATE TRIGGER trg_crypto_register_watch_asset
    AFTER INSERT OR UPDATE OF network_id,contract_address,is_active,deletion_mark ON cat_tokens
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_register_watch_asset();
DROP TRIGGER IF EXISTS trg_crypto_block_ingestion_txid ON sys_crypto_block_ingestion;
CREATE TRIGGER trg_crypto_block_ingestion_txid BEFORE UPDATE ON sys_crypto_block_ingestion
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TRIGGER trg_crypto_register_watch_address ON cat_wallets;
DROP TRIGGER trg_crypto_register_watch_asset ON cat_tokens;
DROP FUNCTION fn_crypto_register_watch_address();
DROP FUNCTION fn_crypto_register_watch_asset();
ALTER TABLE doc_crypto_payments DROP COLUMN verification_id;
ALTER TABLE reg_crypto_wallet_fundings DROP COLUMN verification_id;
DROP TABLE reg_crypto_transfer_verifications;
DROP FUNCTION fn_crypto_transfer_verification_immutable();
DROP TABLE sys_crypto_watch_targets;
DROP TABLE sys_crypto_block_ingestion;
DROP TABLE reg_crypto_chain_observations;
DROP FUNCTION fn_crypto_chain_observation_evidence_guard();
-- +goose StatementEnd
