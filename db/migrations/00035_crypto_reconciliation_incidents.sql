-- +goose Up
-- Description: Immutable reconciliation observations and wallet allocation holds.

-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

CREATE TABLE reg_crypto_wallet_allocation_holds (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    wallet_id   UUID NOT NULL REFERENCES cat_wallets(id),
    reason      TEXT NOT NULL CHECK (BTRIM(reason) <> ''),
    owner_type  TEXT NOT NULL CHECK (BTRIM(owner_type) <> ''),
    owner_id    TEXT NOT NULL CHECK (BTRIM(owner_id) <> ''),
    details     JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(details)='object'),
    status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved')),
    held_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    held_by     TEXT NOT NULL CHECK (BTRIM(held_by) <> ''),
    resolved_at TIMESTAMPTZ,
    resolved_by TEXT NOT NULL DEFAULT '',
    resolution  TEXT NOT NULL DEFAULT '',
    CONSTRAINT chk_wallet_hold_resolution CHECK (
      (status='open' AND resolved_at IS NULL)
      OR (status='resolved' AND resolved_at IS NOT NULL AND resolved_by<>'' AND resolution<>'')
    )
);
CREATE UNIQUE INDEX idx_crypto_wallet_holds_open_owner
    ON reg_crypto_wallet_allocation_holds (wallet_id,owner_type,owner_id)
    WHERE status='open';
CREATE INDEX idx_crypto_wallet_holds_open_wallet
    ON reg_crypto_wallet_allocation_holds (wallet_id) WHERE status='open';

CREATE OR REPLACE FUNCTION fn_crypto_wallet_hold_owner_guard() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP='DELETE' OR NEW.wallet_id<>OLD.wallet_id OR NEW.owner_type<>OLD.owner_type
       OR NEW.owner_id<>OLD.owner_id OR NEW.reason<>OLD.reason
       OR NEW.details IS DISTINCT FROM OLD.details OR NEW.held_at<>OLD.held_at
       OR NEW.held_by<>OLD.held_by OR OLD.status='resolved'
       OR (OLD.status='open' AND NEW.status NOT IN ('open','resolved')) THEN
        RAISE EXCEPTION 'wallet hold acquisition identity is immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_wallet_hold_owner_guard
    BEFORE UPDATE OR DELETE ON reg_crypto_wallet_allocation_holds
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_wallet_hold_owner_guard();

CREATE TABLE reg_crypto_reconciliation_observations (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    run_id         UUID NOT NULL REFERENCES reg_crypto_reconciliation_runs(id),
    incident_id    UUID NOT NULL REFERENCES reg_crypto_reconciliation_items(id),
    network_id     UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    wallet_id      UUID NOT NULL REFERENCES cat_wallets(id),
    token_id       UUID REFERENCES cat_tokens(id),
    kind           TEXT NOT NULL CHECK (kind IN ('token_balance_mismatch','native_fee_balance_mismatch','external_outflow')),
    ledger_amount  NUMERIC(78,0) NOT NULL DEFAULT 0,
    chain_amount   NUMERIC(78,0) NOT NULL DEFAULT 0,
    delta_amount   NUMERIC(78,0) NOT NULL DEFAULT 0,
    tx_hash        TEXT NOT NULL DEFAULT '',
    event_kind     TEXT NOT NULL DEFAULT 'native_value' CHECK (event_kind IN ('native_value','contract_log','internal_value')),
    event_index    BIGINT NOT NULL DEFAULT 0 CHECK (event_index >= 0),
    block_hash     TEXT NOT NULL DEFAULT '',
    observed_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_crypto_reconciliation_observation UNIQUE (run_id, incident_id)
);

CREATE INDEX idx_crypto_reconciliation_observations_incident
    ON reg_crypto_reconciliation_observations (incident_id, observed_at DESC);

CREATE TABLE reg_crypto_external_outflow_observations (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    external_outflow_id UUID NOT NULL REFERENCES reg_crypto_external_outflows(id),
    run_id              UUID NOT NULL REFERENCES reg_crypto_reconciliation_runs(id),
    network_id          UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    tx_hash             TEXT NOT NULL CHECK (BTRIM(tx_hash)<>''),
    event_kind          TEXT NOT NULL CHECK (event_kind IN ('native_value','contract_log','internal_value')),
    event_index         BIGINT NOT NULL CHECK (event_index>=0),
    block_number        BIGINT NOT NULL DEFAULT 0 CHECK (block_number>=0),
    block_hash          TEXT NOT NULL DEFAULT '',
    confirmations       INT NOT NULL DEFAULT 0 CHECK (confirmations>=0),
    required_confs      INT NOT NULL DEFAULT 0 CHECK (required_confs>=0),
    finality_status     TEXT NOT NULL CHECK (finality_status IN ('observed','confirmed','finalized','removed','reorged')),
    observation_source  TEXT NOT NULL CHECK (BTRIM(observation_source)<>''),
    observed_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_crypto_external_outflow_observation UNIQUE (run_id,external_outflow_id),
    CONSTRAINT fk_crypto_external_outflow_observation_identity
      FOREIGN KEY (external_outflow_id,network_id,tx_hash,event_kind,event_index)
      REFERENCES reg_crypto_external_outflows(id,network_id,tx_hash,event_kind,event_index)
);
CREATE INDEX idx_crypto_external_outflow_observations_identity
    ON reg_crypto_external_outflow_observations
       (network_id,tx_hash,event_kind,event_index,observed_at DESC);

-- Every successful chain balance read is retained, including zero-delta reads
-- that do not create an incident. Resolution workflows use this evidence.
CREATE TABLE reg_crypto_reconciliation_checks (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    run_id         UUID NOT NULL REFERENCES reg_crypto_reconciliation_runs(id),
    network_id     UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    wallet_id      UUID NOT NULL REFERENCES cat_wallets(id),
    token_id       UUID NOT NULL REFERENCES cat_tokens(id),
    ledger_amount  NUMERIC(78,0) NOT NULL,
    chain_amount   NUMERIC(78,0) NOT NULL,
    delta_amount   NUMERIC(78,0) NOT NULL,
    observed_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (run_id,wallet_id,token_id)
);
CREATE INDEX idx_crypto_reconciliation_checks_resolution
    ON reg_crypto_reconciliation_checks (wallet_id,token_id,observed_at DESC);

-- Financial workflows enqueue a request in their own transaction. Workers
-- claim it later, so no blockchain RPC runs while money rows are locked.
CREATE TABLE reg_crypto_reconciliation_requests (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    network_id     UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    wallet_id      UUID NOT NULL REFERENCES cat_wallets(id),
    token_id       UUID NOT NULL REFERENCES cat_tokens(id),
    source_type    TEXT NOT NULL CHECK (BTRIM(source_type)<>''),
    source_id      UUID NOT NULL,
    status         TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','running','completed','failed')),
    attempts       INT NOT NULL DEFAULT 0 CHECK (attempts>=0),
    requested_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    claimed_at     TIMESTAMPTZ,
    completed_at   TIMESTAMPTZ,
    last_error     TEXT NOT NULL DEFAULT '',
    UNIQUE (source_type,source_id),
    CONSTRAINT chk_crypto_reconciliation_request_state CHECK (
      (status='pending' AND completed_at IS NULL)
      OR (status='running' AND claimed_at IS NOT NULL AND completed_at IS NULL)
      OR (status IN ('completed','failed') AND claimed_at IS NOT NULL AND completed_at IS NOT NULL)
    )
);
CREATE INDEX idx_crypto_reconciliation_requests_pending
    ON reg_crypto_reconciliation_requests (requested_at,id) WHERE status='pending';

CREATE INDEX idx_cat_wallets_allocation_ready
    ON cat_wallets (network_id, status, tier, chain_activation_status, id)
    WHERE is_active = TRUE AND deletion_mark = FALSE;

CREATE OR REPLACE FUNCTION fn_reconciliation_observations_immutable() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'reconciliation observations are immutable';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_reconciliation_checks_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_reconciliation_checks
    FOR EACH ROW EXECUTE FUNCTION fn_reconciliation_observations_immutable();

CREATE TRIGGER trg_reconciliation_observations_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_reconciliation_observations
    FOR EACH ROW EXECUTE FUNCTION fn_reconciliation_observations_immutable();

CREATE TRIGGER trg_external_outflow_observations_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_external_outflow_observations
    FOR EACH ROW EXECUTE FUNCTION fn_reconciliation_observations_immutable();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

DROP TRIGGER trg_external_outflow_observations_immutable ON reg_crypto_external_outflow_observations;
DROP TABLE reg_crypto_reconciliation_requests;
DROP TRIGGER trg_reconciliation_checks_immutable ON reg_crypto_reconciliation_checks;
DROP TABLE reg_crypto_reconciliation_checks;
DROP TABLE reg_crypto_external_outflow_observations;
DROP TRIGGER trg_reconciliation_observations_immutable ON reg_crypto_reconciliation_observations;
DROP TABLE reg_crypto_reconciliation_observations;
DROP FUNCTION fn_reconciliation_observations_immutable();
DROP INDEX idx_cat_wallets_allocation_ready;
DROP TRIGGER trg_crypto_wallet_hold_owner_guard ON reg_crypto_wallet_allocation_holds;
DROP TABLE reg_crypto_wallet_allocation_holds;
DROP FUNCTION fn_crypto_wallet_hold_owner_guard();

-- +goose StatementEnd
