-- Crypto processing core: networks, tokens, wallets, invoices, payments, payment
-- links, fees, chain watcher and observations, funding, wallet pool, merchant
-- webhooks, financial registers and adjustments.
-- Consolidated schema: each object is declared once in its final form.

-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- Function bodies are validated when first executed; definitions precede the tables they use.
SET LOCAL check_function_bodies = off;


-- ---- Functions ----
-- Trigger: auto-update balances on movement insert/delete
CREATE OR REPLACE FUNCTION update_crypto_balance()
RETURNS TRIGGER AS $func$
DECLARE
    v_signed_amount NUMERIC(78,0);
    v_wid  UUID;
    v_tid  UUID;
    v_per  TIMESTAMPTZ;
BEGIN
    IF TG_OP = 'DELETE' THEN
        v_wid := OLD.wallet_id;
        v_tid := OLD.token_id;
        v_per := OLD.period;
        IF OLD.record_type = 'receipt' THEN
            v_signed_amount := -OLD.amount;
        ELSE
            v_signed_amount := OLD.amount;
        END IF;
    ELSE
        v_wid := NEW.wallet_id;
        v_tid := NEW.token_id;
        v_per := NEW.period;
        IF NEW.record_type = 'receipt' THEN
            v_signed_amount := NEW.amount;
        ELSE
            v_signed_amount := -NEW.amount;
        END IF;
    END IF;

    INSERT INTO reg_crypto_balance_balances (wallet_id, token_id, amount, last_movement_at, updated_at)
    VALUES (v_wid, v_tid, v_signed_amount, v_per, NOW())
    ON CONFLICT (wallet_id, token_id) DO UPDATE SET
        amount = reg_crypto_balance_balances.amount + v_signed_amount,
        last_movement_at = GREATEST(reg_crypto_balance_balances.last_movement_at, v_per),
        updated_at = NOW();

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    ELSE
        RETURN NEW;
    END IF;
END;
$func$ LANGUAGE plpgsql;

-- Full recalculation function (for audit / recovery)
CREATE OR REPLACE FUNCTION recalculate_crypto_balance()
RETURNS void AS $func$
BEGIN
    TRUNCATE reg_crypto_balance_balances;
    INSERT INTO reg_crypto_balance_balances (wallet_id, token_id, amount, last_movement_at, updated_at)
    SELECT
        wallet_id,
        token_id,
        SUM(CASE WHEN record_type = 'receipt' THEN amount ELSE -amount END),
        MAX(period),
        NOW()
    FROM reg_crypto_balance_movements
    GROUP BY wallet_id, token_id;
END;
$func$ LANGUAGE plpgsql;

CREATE FUNCTION guard_exchange_rate_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'exchange-rate observations are immutable' USING ERRCODE = '23514';
END;
$$;

CREATE FUNCTION guard_rate_source_type() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.source_type IS DISTINCT FROM OLD.source_type AND (
        EXISTS (SELECT 1 FROM reg_rate_source_mappings WHERE rate_source_id = OLD.id)
        OR EXISTS (SELECT 1 FROM reg_exchange_rates WHERE rate_source_id = OLD.id)
        OR EXISTS (SELECT 1 FROM sys_settings WHERE exchange_rates->>'sourceId' = OLD.id::text)
    ) THEN
        RAISE EXCEPTION 'source type is in use' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

-- Trigger: auto-update merchant balances on movement insert/delete
CREATE OR REPLACE FUNCTION update_crypto_merchant_balance()
RETURNS TRIGGER AS $func$
DECLARE
    v_signed_amount NUMERIC(78,0);
    v_mid  UUID;
    v_tid  UUID;
    v_per  TIMESTAMPTZ;
BEGIN
    IF TG_OP = 'DELETE' THEN
        v_mid := OLD.merchant_id;
        v_tid := OLD.token_id;
        v_per := OLD.period;
        IF OLD.record_type = 'receipt' THEN
            v_signed_amount := -OLD.amount;
        ELSE
            v_signed_amount := OLD.amount;
        END IF;
    ELSE
        v_mid := NEW.merchant_id;
        v_tid := NEW.token_id;
        v_per := NEW.period;
        IF NEW.record_type = 'receipt' THEN
            v_signed_amount := NEW.amount;
        ELSE
            v_signed_amount := -NEW.amount;
        END IF;
    END IF;

    INSERT INTO reg_crypto_merchant_balance_balances (merchant_id, token_id, amount, last_movement_at, updated_at)
    VALUES (v_mid, v_tid, v_signed_amount, v_per, NOW())
    ON CONFLICT (merchant_id, token_id) DO UPDATE SET
        amount = reg_crypto_merchant_balance_balances.amount + v_signed_amount,
        last_movement_at = GREATEST(reg_crypto_merchant_balance_balances.last_movement_at, v_per),
        updated_at = NOW();

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    ELSE
        RETURN NEW;
    END IF;
END;
$func$ LANGUAGE plpgsql;

-- Full recalculation function (for audit / recovery)
CREATE OR REPLACE FUNCTION recalculate_crypto_merchant_balance()
RETURNS void AS $func$
BEGIN
    TRUNCATE reg_crypto_merchant_balance_balances;
    INSERT INTO reg_crypto_merchant_balance_balances (merchant_id, token_id, amount, last_movement_at, updated_at)
    SELECT
        merchant_id,
        token_id,
        SUM(CASE WHEN record_type = 'receipt' THEN amount ELSE -amount END),
        MAX(period),
        NOW()
    FROM reg_crypto_merchant_balance_movements
    GROUP BY merchant_id, token_id;
END;
$func$ LANGUAGE plpgsql;

-- CDC trigger for merchants
CREATE OR REPLACE FUNCTION fn_cat_merchants_cdc() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        NEW._txid := txid_current();
        IF OLD.deletion_mark = FALSE AND NEW.deletion_mark = TRUE THEN
            NEW._deleted_at := NOW();
        ELSIF OLD.deletion_mark = TRUE AND NEW.deletion_mark = FALSE THEN
            NEW._deleted_at := NULL;
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_deposit_address_retirement_guard() RETURNS TRIGGER AS $$
BEGIN
    IF TG_TABLE_NAME='reg_crypto_deposit_address_rotations' THEN
        RAISE EXCEPTION 'deposit address rotation history is immutable';
    END IF;
    IF OLD.deposit_retired_at IS NOT NULL THEN
        IF TG_OP='DELETE' THEN
            RAISE EXCEPTION 'retired deposit addresses must remain monitored';
        END IF;
        IF NEW.deposit_retired_at IS DISTINCT FROM OLD.deposit_retired_at
           OR NEW.merchant_id IS DISTINCT FROM OLD.merchant_id
           OR NEW.customer_id IS DISTINCT FROM OLD.customer_id
           OR NEW.network_id IS DISTINCT FROM OLD.network_id
           OR NEW.address IS DISTINCT FROM OLD.address
           OR NEW.address_key IS DISTINCT FROM OLD.address_key
           OR NEW.vault_key_id IS DISTINCT FROM OLD.vault_key_id
           OR NEW.deposit_assigned_at IS DISTINCT FROM OLD.deposit_assigned_at THEN
            RAISE EXCEPTION 'retired deposit address attribution is immutable';
        END IF;
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_deposit_rotation_identity_guard() RETURNS TRIGGER AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM cat_wallets old_wallet JOIN cat_wallets new_wallet ON new_wallet.id=NEW.new_wallet_id
        WHERE old_wallet.id=NEW.old_wallet_id AND old_wallet.deposit_retired_at=NEW.created_at
          AND old_wallet.merchant_id=NEW.merchant_id AND new_wallet.merchant_id=NEW.merchant_id
          AND old_wallet.network_id=NEW.network_id AND new_wallet.network_id=NEW.network_id
          AND old_wallet.customer_id=NEW.customer_id AND new_wallet.customer_id=NEW.customer_id
          AND old_wallet.address=NEW.old_address AND new_wallet.address=NEW.new_address
          AND new_wallet.deposit_assigned_at=NEW.created_at
    ) THEN RAISE EXCEPTION 'deposit rotation assignment identity mismatch'; END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- CDC trigger for wallets
CREATE OR REPLACE FUNCTION fn_cat_wallets_cdc() RETURNS TRIGGER AS $$
DECLARE family TEXT;
BEGIN
	SELECT chain_family INTO family FROM cat_blockchain_networks WHERE id=NEW.network_id;
	NEW.address_key := CASE WHEN family='evm' THEN LOWER(NEW.address) ELSE NEW.address END;
    IF TG_OP = 'UPDATE' THEN
        NEW._txid := txid_current();
        IF OLD.deletion_mark = FALSE AND NEW.deletion_mark = TRUE THEN
            NEW._deleted_at := NOW();
        ELSIF OLD.deletion_mark = TRUE AND NEW.deletion_mark = FALSE THEN
            NEW._deleted_at := NULL;
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_merchant_network_policy_guard() RETURNS TRIGGER AS $$
DECLARE policy_mode TEXT;
BEGIN
    IF TG_TABLE_NAME='auth_merchant_network_policy_audit' THEN
        RAISE EXCEPTION 'merchant network policy audit is immutable';
    END IF;
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'merchant network policies are revoked, not deleted';
    END IF;
    IF TG_TABLE_NAME='auth_merchant_network_policies' THEN
        IF TG_OP='UPDATE' AND OLD.mode='off' AND NEW.mode='enforce' THEN
            RAISE EXCEPTION 'merchant network policy must pass through warn mode';
        END IF;
        NEW.version := CASE WHEN TG_OP='UPDATE' THEN OLD.version+1 ELSE NEW.version END;
        NEW.updated_at := NOW();
        policy_mode := NEW.mode;
    ELSE
        SELECT mode INTO policy_mode FROM auth_merchant_network_policies WHERE id=NEW.policy_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_merchant_network_policy_enforce_has_cidr() RETURNS TRIGGER AS $$
DECLARE target_policy UUID;
BEGIN
    -- This trigger is shared by two tables with different row types. Accessing
    -- NEW.id/NEW.policy_id in one CASE expression is unsafe because PL/pgSQL can
    -- cache the expression plan for the row type that fires first.
    IF TG_TABLE_NAME='auth_merchant_network_policies' THEN
        target_policy := (to_jsonb(NEW)->>'id')::UUID;
    ELSIF TG_TABLE_NAME='auth_merchant_allowed_networks' THEN
        target_policy := (to_jsonb(NEW)->>'policy_id')::UUID;
    ELSE
        RAISE EXCEPTION 'unsupported merchant network policy trigger table: %', TG_TABLE_NAME;
    END IF;
    IF EXISTS (SELECT 1 FROM auth_merchant_network_policies p WHERE p.id=target_policy AND p.mode='enforce')
       AND NOT EXISTS (SELECT 1 FROM auth_merchant_allowed_networks n WHERE n.policy_id=target_policy AND n.is_active) THEN
        RAISE EXCEPTION 'enforce mode requires at least one active CIDR';
    END IF;
    PERFORM pg_notify('merchant_network_policy_changed',target_policy::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- CDC trigger
CREATE OR REPLACE FUNCTION fn_doc_crypto_payments_cdc() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        NEW._txid := txid_current();
        NEW.updated_at := NOW();
        IF OLD.deletion_mark = FALSE AND NEW.deletion_mark = TRUE THEN
            NEW._deleted_at := NOW();
        ELSIF OLD.deletion_mark = TRUE AND NEW.deletion_mark = FALSE THEN
            NEW._deleted_at := NULL;
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Auto-update balance on movement insert/delete
CREATE OR REPLACE FUNCTION fn_crypto_fee_balance_update() RETURNS TRIGGER AS $$
DECLARE
    v_delta NUMERIC(78,0);
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.record_type = 'receipt' THEN v_delta := -OLD.amount;
        ELSE v_delta := OLD.amount;
        END IF;
        INSERT INTO reg_crypto_fee_balances (merchant_id, token_id, fee_type, balance)
        VALUES (OLD.merchant_id, OLD.token_id, OLD.fee_type, v_delta)
        ON CONFLICT (merchant_id, token_id, fee_type)
        DO UPDATE SET balance = reg_crypto_fee_balances.balance + v_delta;
        RETURN OLD;
    ELSE
        IF NEW.record_type = 'receipt' THEN v_delta := NEW.amount;
        ELSE v_delta := -NEW.amount;
        END IF;
        INSERT INTO reg_crypto_fee_balances (merchant_id, token_id, fee_type, balance)
        VALUES (NEW.merchant_id, NEW.token_id, NEW.fee_type, v_delta)
        ON CONFLICT (merchant_id, token_id, fee_type)
        DO UPDATE SET balance = reg_crypto_fee_balances.balance + v_delta;
        RETURN NEW;
    END IF;
END;
$$ LANGUAGE plpgsql;

-- RECALCULATE function for reconciliation
CREATE OR REPLACE FUNCTION recalculate_crypto_fee() RETURNS void AS $$
BEGIN
    TRUNCATE reg_crypto_fee_balances;
    INSERT INTO reg_crypto_fee_balances (merchant_id, token_id, fee_type, balance)
    SELECT
        merchant_id,
        token_id,
        fee_type,
        SUM(CASE WHEN record_type = 'receipt' THEN amount ELSE -amount END) AS balance
    FROM reg_crypto_fee_movements
    GROUP BY merchant_id, token_id, fee_type;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_crypto_wallet_fundings_immutable() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'crypto wallet funding receipts are immutable';
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION initialize_crypto_wallet_pool_policy()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO sys_crypto_wallet_pool_policies (
        network_id, enabled, low_watermark, target_ready, max_total_wallets,
        max_provision_batch, retry_after_seconds
    ) VALUES (
        NEW.id, FALSE, 0, 1, 1, 1, 30
    )
    ON CONFLICT (network_id) DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION publish_crypto_wallet_derivation()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.derivation_path = '' THEN
        RETURN NEW;
    END IF;

    INSERT INTO sys_crypto_wallet_derivations (
        network_id, derivation_path, wallet_id, source
    ) VALUES (
        NEW.network_id, NEW.derivation_path, NEW.id, 'wallet'
    )
    ON CONFLICT (network_id, derivation_path) DO UPDATE
    SET wallet_id = EXCLUDED.wallet_id,
        version = sys_crypto_wallet_derivations.version + 1
    WHERE sys_crypto_wallet_derivations.wallet_id IS NULL
       OR sys_crypto_wallet_derivations.wallet_id = EXCLUDED.wallet_id;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'crypto wallet derivation path is already bound to another wallet';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION initialize_crypto_network_policy()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO sys_crypto_network_policies (network_id)
    VALUES (NEW.id)
    ON CONFLICT (network_id) DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_crypto_fee_reservation_events_immutable() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'crypto fee reservation events are immutable';
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_crypto_chain_settlement_evidence_immutable() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'crypto chain settlement evidence is immutable';
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_evm_nonce_lease_events_immutable() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'EVM nonce lease events are immutable';
END;
$$ LANGUAGE plpgsql;

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

CREATE FUNCTION fn_crypto_token_identity_guard()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.financial_identity_locked AND NOT NEW.financial_identity_locked THEN
        RAISE EXCEPTION 'token financial identity lock cannot be cleared'
            USING ERRCODE = '23514';
    END IF;
    IF OLD.financial_identity_locked AND (
        NEW.network_id IS DISTINCT FROM OLD.network_id OR
        NEW.contract_address IS DISTINCT FROM OLD.contract_address OR
        NEW.token_standard IS DISTINCT FROM OLD.token_standard OR
        NEW.decimal_places IS DISTINCT FROM OLD.decimal_places
    ) THEN
        RAISE EXCEPTION 'token identity cannot change after financial use'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Marking the row happens before the FK check and serializes with an identity edit.
CREATE FUNCTION fn_crypto_mark_token_identity_used()
RETURNS TRIGGER AS $$
DECLARE
    field_name TEXT;
    token_key UUID;
BEGIN
    FOREACH field_name IN ARRAY TG_ARGV LOOP
        token_key := NULLIF(to_jsonb(NEW)->>field_name, '')::UUID;
        IF token_key IS NOT NULL THEN
            UPDATE cat_tokens
               SET financial_identity_locked = TRUE
             WHERE id = token_key AND financial_identity_locked = FALSE;
        END IF;
    END LOOP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_financial_movement_immutable()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'crypto financial movements are append-only'
        USING ERRCODE = '23514';
END;
$$ LANGUAGE plpgsql;

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

CREATE OR REPLACE FUNCTION fn_crypto_transfer_verification_immutable() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'transfer verification evidence is immutable';
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_crypto_register_watch_address() RETURNS TRIGGER AS $$
DECLARE boundary BIGINT;
BEGIN
 IF NEW.vault_key_id='' OR NEW.address='' OR NOT EXISTS(SELECT 1 FROM cat_blockchain_networks WHERE id=NEW.network_id AND chain_family='tron') THEN RETURN NEW; END IF;
 IF NOT fn_crypto_try_lock_watch_network(NEW.network_id) THEN RAISE EXCEPTION 'watch registration is busy' USING ERRCODE='55P03'; END IF;
 SELECT start_height+1 INTO boundary FROM sys_crypto_block_ingestion WHERE network_id=NEW.network_id FOR UPDATE;
 IF boundary IS NULL THEN boundary:=0; END IF;
 SELECT GREATEST(boundary,COALESCE(MAX(last_block)+1,boundary)) INTO boundary FROM sys_chain_watcher_state WHERE network_id=NEW.network_id AND watcher_key='tron:solidified';
 INSERT INTO sys_crypto_watch_targets(network_id,target_kind,target_key,owner_id,start_height,source) VALUES(NEW.network_id,'address',NEW.address,NEW.id,boundary,'wallet_write') ON CONFLICT(network_id,target_kind,target_key) DO NOTHING;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_crypto_register_watch_asset() RETURNS TRIGGER AS $$
DECLARE boundary BIGINT;
BEGIN
 IF NOT NEW.is_active OR NEW.deletion_mark OR NEW.contract_address='' OR NEW.token_standard<>'TRC-20' OR NOT EXISTS(SELECT 1 FROM cat_blockchain_networks WHERE id=NEW.network_id AND chain_family='tron') THEN RETURN NEW; END IF;
 IF NOT fn_crypto_try_lock_watch_network(NEW.network_id) THEN RAISE EXCEPTION 'watch registration is busy' USING ERRCODE='55P03'; END IF;
 SELECT start_height+1 INTO boundary FROM sys_crypto_block_ingestion WHERE network_id=NEW.network_id FOR UPDATE;
 IF boundary IS NULL THEN boundary:=0; END IF;
 SELECT GREATEST(boundary,COALESCE(MAX(last_block)+1,boundary)) INTO boundary FROM sys_chain_watcher_state WHERE network_id=NEW.network_id AND watcher_key='tron:solidified';
 INSERT INTO sys_crypto_watch_targets(network_id,target_kind,target_key,owner_id,start_height,source) VALUES(NEW.network_id,'asset',NEW.contract_address,NEW.id,boundary,'asset_write') ON CONFLICT(network_id,target_kind,target_key) DO NOTHING;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_crypto_adjustment_guard() RETURNS TRIGGER AS $$
BEGIN
    IF (OLD.payment_id,OLD.posted_version,OLD.reason_code,OLD.evidence,
        OLD.idempotency_key,OLD.request_hash,OLD.maker_id,OLD.mode,
        OLD.wallet_id,OLD.token_id,OLD.direction,OLD.amount,OLD.decision_mode)
       IS DISTINCT FROM
       (NEW.payment_id,NEW.posted_version,NEW.reason_code,NEW.evidence,
        NEW.idempotency_key,NEW.request_hash,NEW.maker_id,NEW.mode,
        NEW.wallet_id,NEW.token_id,NEW.direction,NEW.amount,NEW.decision_mode) THEN
        RAISE EXCEPTION 'crypto adjustment basis and evidence are immutable';
    END IF;
    IF OLD.status<>'pending_approval' OR NEW.status='pending_approval' THEN
        RAISE EXCEPTION 'crypto adjustment decision is final';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_crypto_adjustment_source_insert_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE a doc_crypto_adjustments; source JSONB; source_table TEXT; used NUMERIC; original_type TEXT;
BEGIN
    SELECT * INTO STRICT a FROM doc_crypto_adjustments WHERE id=NEW.adjustment_id;
    IF a.status<>'pending_approval' THEN RAISE EXCEPTION 'crypto adjustment sources are frozen after decision' USING ERRCODE='23514'; END IF;
    IF a.mode<>'payment_reversal' OR a.proposal_txid<>txid_current() OR NEW.amount IS NULL THEN
        RAISE EXCEPTION 'crypto adjustment sources are frozen at proposal' USING ERRCODE='23514';
    END IF;
    source_table:=CASE NEW.source_register WHEN 'suspense' THEN 'reg_crypto_receipt_suspense_movements'
        WHEN 'merchant' THEN 'reg_crypto_merchant_balance_movements' WHEN 'fee' THEN 'reg_crypto_fee_movements' END;
    IF source_table IS NULL THEN RAISE EXCEPTION 'invalid adjustment source register' USING ERRCODE='23514'; END IF;
    EXECUTE format('SELECT to_jsonb(m) FROM %I m WHERE line_id=$1',source_table) INTO source USING NEW.source_line_id;
    EXECUTE format('SELECT COALESCE(SUM(amount),0) FROM %I WHERE reversal_of_line_id=$1',source_table) INTO used USING NEW.source_line_id;
    -- A payment consumes suspense and credits merchant and fee.
    original_type:=CASE WHEN NEW.source_register='suspense' THEN 'expense' ELSE 'receipt' END;
    IF source IS NULL OR source->>'recorder_type'<>'CryptoPayment' OR (source->>'recorder_id')::uuid<>a.payment_id
        OR (source->>'recorder_version')::int<>a.posted_version OR source->>'reversal_of_line_id' IS NOT NULL
        OR source->>'record_type'<>original_type OR NEW.amount>(source->>'amount')::numeric-used THEN
        RAISE EXCEPTION 'crypto adjustment requires available original payment source' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION fn_crypto_payment_reversal_cap() RETURNS TRIGGER AS $$
DECLARE source_amount NUMERIC(78,0);
DECLARE used_amount NUMERIC(78,0);
DECLARE available NUMERIC(78,0);
DECLARE already_recorded BOOLEAN;
BEGIN
    IF NEW.reversal_of_line_id IS NULL THEN RETURN NEW; END IF;
    EXECUTE format('SELECT amount FROM %I WHERE line_id=$1 FOR UPDATE', TG_TABLE_NAME)
        INTO source_amount USING NEW.reversal_of_line_id;
    IF source_amount IS NULL THEN
        RAISE EXCEPTION 'reversal source is missing' USING ERRCODE='23514';
    END IF;
    EXECUTE format('SELECT COALESCE(SUM(amount),0) FROM %I WHERE reversal_of_line_id=$1', TG_TABLE_NAME)
        INTO used_amount USING NEW.reversal_of_line_id;
    available := source_amount-used_amount;
    IF NEW.recorder_type IN ('CryptoLedgerReversal','CryptoPaymentReorg') THEN
        EXECUTE format('SELECT EXISTS (SELECT 1 FROM %I WHERE reversal_of_line_id=$1 AND recorder_type=$2)', TG_TABLE_NAME)
            INTO already_recorded USING NEW.reversal_of_line_id,NEW.recorder_type;
        IF already_recorded OR available<=0 THEN RETURN NULL; END IF;
        IF NEW.amount>available THEN NEW.amount:=available; END IF;
    ELSIF NEW.amount>available THEN
        RAISE EXCEPTION 'reversal exceeds source amount' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;


-- ---- Tables ----
-- Blockchain Networks catalog (Справочник «Блокчейн-сети»)
CREATE TABLE cat_blockchain_networks (
    -- Base fields
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    deletion_mark BOOLEAN     NOT NULL DEFAULT FALSE,
    version       INT         NOT NULL DEFAULT 1,
    attributes    JSONB       DEFAULT '{}',

    -- CDC
    _deleted_at TIMESTAMPTZ,
    _txid       BIGINT DEFAULT txid_current(),

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Catalog fields
    code      VARCHAR(50)  NOT NULL,
    name      VARCHAR(255) NOT NULL,
    parent_id UUID REFERENCES cat_blockchain_networks(id),
    is_folder BOOLEAN      NOT NULL DEFAULT FALSE,

    rpc_config_source TEXT NOT NULL DEFAULT 'file' CHECK (rpc_config_source IN ('file','database')),
    -- Network-specific fields
    chain_family          TEXT         NOT NULL DEFAULT 'tron', -- driver family: tron, evm, ton, solana
    network_identifier    VARCHAR(50)  NOT NULL,     -- concrete runtime network: nile, mainnet, 1, 137
    native_token_symbol   VARCHAR(10)  NOT NULL,     -- "TRX", "ETH", "BTC"
    native_decimals       INT          NOT NULL,     -- 6, 18, 8
    confirmations_needed  INT          NOT NULL DEFAULT 1,
    finality_mode         TEXT         NOT NULL DEFAULT 'confirmations'
                          CHECK (finality_mode IN ('confirmations','protocol_finalized','confirmations_or_finalized')),
    block_time_seconds    INT          NOT NULL DEFAULT 10,
    explorer_url          TEXT         NOT NULL DEFAULT '', -- HTTPS tx URL template with one {txHash}; empty disables links
    color                 TEXT         NOT NULL DEFAULT '', -- interface palette key; empty lets the interface choose
    is_active             BOOLEAN      NOT NULL DEFAULT TRUE,

    CONSTRAINT chk_native_decimals      CHECK (native_decimals >= 0 AND native_decimals <= 18),
    CONSTRAINT chk_confirmations_needed CHECK (confirmations_needed >= 1),
    CONSTRAINT chk_blockchain_network_family
        CHECK (chain_family IN ('tron','evm','ton','solana')),
    CONSTRAINT chk_blockchain_network_identifier
        CHECK (BTRIM(network_identifier) <> ''),
    CONSTRAINT chk_blockchain_network_color
        CHECK (color IN ('','red','orange','amber','yellow','lime','green','teal','sky','blue','indigo','violet','pink','gray')),
    CONSTRAINT chk_blockchain_network_explorer_url
        CHECK (
            explorer_url = '' OR (
                explorer_url ~ '^https://[^[:space:]]+$'
                AND (LENGTH(explorer_url) - LENGTH(REPLACE(explorer_url, '{txHash}', ''))) = LENGTH('{txHash}')
                AND POSITION('{' IN REPLACE(explorer_url, '{txHash}', '')) = 0
                AND POSITION('}' IN REPLACE(explorer_url, '{txHash}', '')) = 0
            )
        )
);
-- Unique indexes
CREATE UNIQUE INDEX idx_cat_blockchain_networks_code     ON cat_blockchain_networks (code) WHERE deletion_mark = FALSE;
CREATE UNIQUE INDEX idx_cat_blockchain_networks_family_identifier
    ON cat_blockchain_networks (chain_family, network_identifier)
    WHERE deletion_mark = FALSE;
-- Search indexes
CREATE INDEX idx_cat_blockchain_networks_name   ON cat_blockchain_networks USING gin (name gin_trgm_ops);
CREATE INDEX idx_cat_blockchain_networks_parent ON cat_blockchain_networks (parent_id) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_blockchain_networks_attrs  ON cat_blockchain_networks USING gin (attributes);
-- CDC indexes & triggers
CREATE INDEX idx_cat_blockchain_networks_txid ON cat_blockchain_networks (_txid) WHERE _deleted_at IS NULL;
-- Keyset pagination
CREATE INDEX idx_cat_blockchain_networks_name_id ON cat_blockchain_networks (name ASC, id ASC);
COMMENT ON TABLE cat_blockchain_networks IS 'Справочник Блокчейн-сети — поддерживаемые blockchain платформы (Bitcoin, Ethereum, TRON и др.)';
COMMENT ON COLUMN cat_blockchain_networks.explorer_url IS 'HTTPS-шаблон ссылки на транзакцию с единственным плейсхолдером {txHash}; пустое значение отключает ссылку';

-- Tokens catalog (Справочник «Токены»)
CREATE TABLE cat_tokens (
    -- Base fields
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    deletion_mark BOOLEAN     NOT NULL DEFAULT FALSE,
    version       INT         NOT NULL DEFAULT 1,
    attributes    JSONB       DEFAULT '{}',

    -- CDC
    _deleted_at TIMESTAMPTZ,
    _txid       BIGINT DEFAULT txid_current(),

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Catalog fields
    code      VARCHAR(50)  NOT NULL,
    name      VARCHAR(255) NOT NULL,
    parent_id UUID REFERENCES cat_tokens(id),
    is_folder BOOLEAN      NOT NULL DEFAULT FALSE,

    -- Token-specific fields
    network_id       UUID         NOT NULL REFERENCES cat_blockchain_networks(id),
    contract_address TEXT         NOT NULL DEFAULT '',   -- empty for native tokens
    symbol           VARCHAR(20)  NOT NULL,              -- "USDT", "ETH", "BTC"
    decimal_places   INT          NOT NULL,              -- 6, 18, 8 — NEVER hardcode!
    token_standard   VARCHAR(20)  NOT NULL,              -- "native", "TRC-20", "ERC-20"
    is_active        BOOLEAN      NOT NULL DEFAULT TRUE,

    -- Currency link: maps token to its currency for exchange rate lookups.
    -- Multiple tokens can reference the same currency (e.g., USDT-TRC20 + USDT-ERC20 → USDT).
    currency_id      UUID         REFERENCES cat_currencies(id),

    -- Sweep defaults (merchant can override via reg_merchant_token_config)
    sweep_threshold     NUMERIC(78,0) NOT NULL DEFAULT 0, -- raw units. 0 = sweep after every payment
    sweep_max_age_hours INT      NOT NULL DEFAULT 0,     -- max hours before forced sweep. 0 = disabled
    sweep_retain_amount NUMERIC(78,0) NOT NULL DEFAULT 0,
    financial_identity_locked BOOLEAN NOT NULL DEFAULT FALSE,

    CONSTRAINT chk_token_decimals CHECK (decimal_places >= 0 AND decimal_places <= 18),
    CONSTRAINT chk_sweep_threshold CHECK (sweep_threshold >= 0),
    CONSTRAINT chk_sweep_max_age CHECK (sweep_max_age_hours >= 0),
    CONSTRAINT chk_sweep_retain_amount CHECK (sweep_retain_amount >= 0),
    CONSTRAINT uq_crypto_token_network_scope UNIQUE(id,network_id)
);
-- Unique indexes
CREATE UNIQUE INDEX idx_cat_tokens_code ON cat_tokens (code) WHERE deletion_mark = FALSE;
CREATE UNIQUE INDEX idx_cat_tokens_symbol_network ON cat_tokens (symbol, network_id) WHERE deletion_mark = FALSE;
-- Search indexes
CREATE INDEX idx_cat_tokens_name    ON cat_tokens USING gin (name gin_trgm_ops);
CREATE INDEX idx_cat_tokens_parent  ON cat_tokens (parent_id) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_tokens_network ON cat_tokens (network_id) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_tokens_currency ON cat_tokens (currency_id) WHERE deletion_mark = FALSE AND currency_id IS NOT NULL;
CREATE INDEX idx_cat_tokens_attrs   ON cat_tokens USING gin (attributes);
-- CDC indexes & triggers
CREATE INDEX idx_cat_tokens_txid ON cat_tokens (_txid) WHERE _deleted_at IS NULL;
-- Keyset pagination
CREATE INDEX idx_cat_tokens_name_id ON cat_tokens (name ASC, id ASC);
COMMENT ON COLUMN cat_tokens.sweep_retain_amount IS
    'Token minor units retained on reusable deposit wallets after sweep';
COMMENT ON TABLE cat_tokens IS 'Справочник Токены — криптовалютные активы на блокчейн-сетях (USDT-TRC20, ETH, BTC и др.)';
CREATE UNIQUE INDEX idx_cat_tokens_contract_network
    ON cat_tokens (network_id,LOWER(contract_address))
    WHERE deletion_mark=FALSE AND contract_address <> '';

CREATE TABLE sys_crypto_rpc_network_settings(network_id UUID PRIMARY KEY REFERENCES cat_blockchain_networks(id),evm_settings JSONB,tron_settings JSONB,ingestion_alert_settings JSONB NOT NULL DEFAULT '{}' CHECK(jsonb_typeof(ingestion_alert_settings)='object'),version INT NOT NULL DEFAULT 1,updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE sys_crypto_rpc_endpoints(
 id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),code VARCHAR(50) NOT NULL,name VARCHAR(255) NOT NULL,
 parent_id UUID CHECK(parent_id IS NULL),is_folder BOOLEAN NOT NULL DEFAULT FALSE CHECK(NOT is_folder),
 deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,version INT NOT NULL DEFAULT 1,attributes JSONB NOT NULL DEFAULT '{}',
 _deleted_at TIMESTAMPTZ,_txid BIGINT DEFAULT txid_current(),created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id),
 role TEXT NOT NULL CHECK(role IN ('rpc','indexer','fullnode','solidity')),
 provider_id VARCHAR(64) NOT NULL,trust_group VARCHAR(64) NOT NULL,
 url TEXT NOT NULL,pool_id UUID REFERENCES cat_integration_pools(id) ON DELETE RESTRICT,
 credential_slot VARCHAR(64) NOT NULL DEFAULT 'default',priority INT NOT NULL DEFAULT 0 CHECK(priority>=0),
 min_request_interval_ms INT NOT NULL DEFAULT 0 CHECK(min_request_interval_ms=0 OR min_request_interval_ms BETWEEN 100 AND 30000),
 disable_keep_alives BOOLEAN NOT NULL DEFAULT FALSE,is_active BOOLEAN NOT NULL DEFAULT TRUE
);
CREATE UNIQUE INDEX uq_rpc_endpoint_code ON sys_crypto_rpc_endpoints(code) WHERE NOT deletion_mark;
CREATE UNIQUE INDEX uq_rpc_endpoint_provider ON sys_crypto_rpc_endpoints(network_id,role,provider_id) WHERE NOT deletion_mark;
CREATE INDEX idx_rpc_endpoint_name_id ON sys_crypto_rpc_endpoints(name,id);
CREATE INDEX idx_rpc_endpoint_pool ON sys_crypto_rpc_endpoints(pool_id);
CREATE INDEX idx_rpc_endpoint_network ON sys_crypto_rpc_endpoints(network_id);
CREATE INDEX idx_rpc_endpoint_txid ON sys_crypto_rpc_endpoints(_txid) WHERE _deleted_at IS NULL;

-- Crypto Invoice document (Документ «Крипто-инвойс»)
CREATE TABLE doc_crypto_invoices (
    -- Base fields
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    public_ref    TEXT        NOT NULL,
    deletion_mark BOOLEAN     NOT NULL DEFAULT FALSE,
    version       INT         NOT NULL DEFAULT 1,
    attributes    JSONB       DEFAULT '{}',

    -- CDC
    _deleted_at TIMESTAMPTZ,
    _txid       BIGINT DEFAULT txid_current(),

    -- Audit fields
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by UUID,        -- NULL for merchant API requests (no auth user context)
    updated_by UUID,

    -- Document fields
    number          TEXT         NOT NULL,
    date            TIMESTAMPTZ  NOT NULL,
    posted          BOOLEAN      NOT NULL DEFAULT FALSE,
    posted_version  INT          NOT NULL DEFAULT 0,
    basis_type      TEXT         NOT NULL DEFAULT '',
    basis_id        UUID,
    description     TEXT         NOT NULL DEFAULT '',

    -- CryptoInvoice-specific fields
    merchant_id    UUID    NOT NULL,                                   -- FK to future cat_merchants
    token_id       UUID    NOT NULL REFERENCES cat_tokens(id),
    wallet_id      UUID,                                               -- assigned during payment setup
    expected_amount NUMERIC(78,0) NOT NULL,
    received_amount NUMERIC(78,0) NOT NULL DEFAULT 0,
    confirmed_amount NUMERIC(78,0) NOT NULL DEFAULT 0,
    credited_amount NUMERIC(78,0) NOT NULL DEFAULT 0,
    overpaid_amount NUMERIC(78,0) NOT NULL DEFAULT 0,
    status         TEXT     NOT NULL DEFAULT 'created',                 -- InvoiceStatus enum (string)
    single_payment BOOLEAN NOT NULL DEFAULT FALSE,
    selected_payment_id UUID,
    expires_at     TIMESTAMPTZ NOT NULL,
    notification_url TEXT  NOT NULL DEFAULT '',
    return_url     TEXT    NOT NULL DEFAULT '',
    metadata       JSONB   NOT NULL DEFAULT '{}'::jsonb,
    required_confirmations INT NOT NULL DEFAULT 0,
    finality_mode  TEXT    NOT NULL DEFAULT '',
    paid_at        TIMESTAMPTZ,
    confirmed_at   TIMESTAMPTZ,
    completed_at   TIMESTAMPTZ,
    reversed_at    TIMESTAMPTZ,
    view_count     BIGINT NOT NULL DEFAULT 0,
    first_viewed_at TIMESTAMPTZ,
    last_viewed_at TIMESTAMPTZ,
    external_id    TEXT    NOT NULL DEFAULT '',                         -- merchant business reference
    order_id       TEXT    NOT NULL DEFAULT '',
    ttl_minutes    INT     NOT NULL DEFAULT 60 CHECK (ttl_minutes BETWEEN 5 AND 10080),
    customer_id    TEXT    NOT NULL DEFAULT '',
    creation_source TEXT   NOT NULL DEFAULT 'portal'
                   CHECK (creation_source IN ('merchant_api','portal','system')),
    invoice_kind   TEXT   NOT NULL DEFAULT 'payment'
                   CHECK (invoice_kind IN ('payment','top_up')),
    CONSTRAINT chk_crypto_invoice_top_up_source CHECK (invoice_kind <> 'top_up' OR creation_source = 'system'),

    -- API key audit trail: which merchant API key created this invoice.
    -- NULL for invoices created via internal/admin flows (JWT user context).
    -- Chain: api_key_id → cat_merchant_api_keys.created_by_user_id → platform user.
    api_key_id     UUID,

    CONSTRAINT uq_crypto_invoice_number UNIQUE (number),
    CONSTRAINT uq_crypto_invoice_public_ref UNIQUE (public_ref),
    CONSTRAINT chk_crypto_invoice_public_ref_format CHECK (public_ref ~ '^ci_[a-f0-9]{32}$'),
    CONSTRAINT chk_expected_amount_positive CHECK (expected_amount > 0),
    CONSTRAINT chk_received_amount_nonneg CHECK (received_amount >= 0),
    CONSTRAINT chk_confirmed_amount_nonneg CHECK (confirmed_amount >= 0),
    CONSTRAINT chk_credited_amount_nonneg CHECK (credited_amount >= 0),
    CONSTRAINT chk_overpaid_amount_nonneg CHECK (overpaid_amount >= 0),
    CONSTRAINT chk_invoice_amount_order CHECK (
        credited_amount <= confirmed_amount AND confirmed_amount <= received_amount
    ),
    CONSTRAINT chk_invoice_metadata_object CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT chk_invoice_required_confirmations CHECK (required_confirmations >= 0),
    CONSTRAINT chk_invoice_view_count_nonneg CHECK (view_count >= 0),
    CONSTRAINT chk_status_valid CHECK (
        status IN ('created','partially_paid','paid','confirmed','completed','underpaid','expired','cancelled','reversed')
    )
    -- No FK on created_by/updated_by: merchant API requests have no auth_users entry.
    -- Audit enrichment is best-effort: set when JWT user is present, NULL otherwise.
    -- No FK on api_key_id: cat_merchant_api_keys is declared after this table.
);
-- Header indexes
CREATE INDEX idx_crypto_invoices_date         ON doc_crypto_invoices (date DESC);
CREATE INDEX idx_crypto_invoices_merchant     ON doc_crypto_invoices (merchant_id);
CREATE INDEX idx_crypto_invoices_token        ON doc_crypto_invoices (token_id);
CREATE INDEX idx_crypto_invoices_wallet       ON doc_crypto_invoices (wallet_id) WHERE wallet_id IS NOT NULL;
CREATE INDEX idx_crypto_invoices_status       ON doc_crypto_invoices (status) WHERE status IN ('created','partially_paid','paid','confirmed','reversed');
CREATE UNIQUE INDEX idx_crypto_invoices_merchant_order_id
    ON doc_crypto_invoices (merchant_id, order_id)
    WHERE order_id <> '' AND _deleted_at IS NULL;
CREATE UNIQUE INDEX idx_crypto_invoices_merchant_external_id
    ON doc_crypto_invoices (merchant_id, external_id)
    WHERE external_id <> '' AND _deleted_at IS NULL;
CREATE INDEX idx_crypto_invoices_expires      ON doc_crypto_invoices (expires_at) WHERE status IN ('created','partially_paid');
CREATE INDEX idx_crypto_invoices_posted       ON doc_crypto_invoices (posted) WHERE posted = FALSE;
CREATE INDEX idx_crypto_invoices_number_trgm  ON doc_crypto_invoices USING gin (number gin_trgm_ops);
CREATE INDEX idx_crypto_invoices_api_key      ON doc_crypto_invoices (api_key_id) WHERE api_key_id IS NOT NULL;
-- CDC indexes & triggers
CREATE INDEX idx_doc_crypto_invoices_txid ON doc_crypto_invoices (_txid) WHERE _deleted_at IS NULL;
-- Keyset pagination
CREATE INDEX idx_doc_crypto_invoices_date_id    ON doc_crypto_invoices (date DESC, id DESC);
CREATE INDEX idx_doc_crypto_invoices_created_id ON doc_crypto_invoices (created_at DESC, id DESC);
CREATE INDEX idx_crypto_invoices_merchant_created_id ON doc_crypto_invoices (merchant_id, created_at DESC, id DESC) WHERE _deleted_at IS NULL;
CREATE INDEX idx_crypto_invoices_merchant_completed_id ON doc_crypto_invoices (merchant_id, completed_at DESC, id DESC) WHERE completed_at IS NOT NULL AND _deleted_at IS NULL;
COMMENT ON TABLE doc_crypto_invoices IS 'Документ Крипто-инвойс — запрос на оплату криптовалютой (без табличной части)';
COMMENT ON COLUMN doc_crypto_invoices.public_ref IS 'Opaque customer-facing checkout reference: ci_<128-bit-hex>';
COMMENT ON COLUMN doc_crypto_invoices.expected_amount IS 'Ожидаемая сумма в точных raw units токена (NUMERIC(78,0))';
COMMENT ON COLUMN doc_crypto_invoices.received_amount IS 'Canonical observed payments, including provisional payments';
COMMENT ON COLUMN doc_crypto_invoices.confirmed_amount IS 'Canonical payments that reached platform network finality';
COMMENT ON COLUMN doc_crypto_invoices.credited_amount IS 'Canonical finalized payments posted to merchant ledger';
COMMENT ON COLUMN doc_crypto_invoices.overpaid_amount IS 'Сумма переплаты (received - expected), 0 если переплаты нет';
COMMENT ON COLUMN doc_crypto_invoices.status IS 'Lifecycle status; overpayment is represented by overpaid_amount, not a status';
COMMENT ON COLUMN doc_crypto_invoices.external_id IS 'Business external reference supplied by the merchant; transport idempotency lives in sys_idempotency';
COMMENT ON COLUMN doc_crypto_invoices.invoice_kind IS 'Platform-owned invoice classification; never inferred from merchant business references';
COMMENT ON COLUMN doc_crypto_invoices.version IS 'Monotonic public resourceVersion';
COMMENT ON COLUMN doc_crypto_invoices.return_url IS 'Browser UX destination only; never authoritative for payment status';
COMMENT ON COLUMN doc_crypto_invoices.view_count IS 'Internal checkout analytics only; never emitted as a merchant event';

-- Crypto Balance accumulation register (Регистр «Крипто-балансы»)
CREATE TABLE reg_crypto_balance_movements (
    line_id          UUID         PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    recorder_id      UUID         NOT NULL,
    recorder_type    VARCHAR(50)  NOT NULL,
    recorder_version INT          NOT NULL DEFAULT 1,
    period           TIMESTAMPTZ  NOT NULL,
    record_type      VARCHAR(10)  NOT NULL,
    wallet_id        UUID         NOT NULL,
    token_id         UUID         NOT NULL REFERENCES cat_tokens(id),
    amount           NUMERIC(78,0) NOT NULL,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    reversal_of_line_id UUID         REFERENCES reg_crypto_balance_movements(line_id),
    reversal_reason  TEXT         NOT NULL DEFAULT '',
    posting_txid     BIGINT,
    CONSTRAINT chk_crypto_balance_record_type CHECK (record_type IN ('receipt', 'expense')),
    CONSTRAINT chk_crypto_balance_amount_positive CHECK (amount > 0),
    CONSTRAINT chk_provider_opening_no_wallet_effect CHECK (recorder_type<>'CryptoProviderBalanceOperation')
);
COMMENT ON TABLE reg_crypto_balance_movements IS 'Регистр крипто-балансов — движения';
CREATE INDEX idx_reg_crypto_balance_movements_recorder
    ON reg_crypto_balance_movements (recorder_id, recorder_version);
CREATE UNIQUE INDEX uq_crypto_custody_adjustment_balance_effect
    ON reg_crypto_balance_movements
       (recorder_id,recorder_version,record_type,wallet_id,token_id)
    WHERE recorder_type = 'CryptoCustodyAdjustment';
CREATE INDEX idx_reg_crypto_balance_movements_balance
    ON reg_crypto_balance_movements (wallet_id, token_id, record_type);
CREATE INDEX idx_reg_crypto_balance_movements_period
    ON reg_crypto_balance_movements (period);
CREATE INDEX idx_reg_crypto_balance_movements_token
    ON reg_crypto_balance_movements (token_id, period DESC);
CREATE INDEX idx_crypto_balance_reversal_line
    ON reg_crypto_balance_movements(reversal_of_line_id) WHERE reversal_of_line_id IS NOT NULL;
CREATE UNIQUE INDEX uq_crypto_balance_ledger_reversal
    ON reg_crypto_balance_movements(reversal_of_line_id)
    WHERE recorder_type='CryptoLedgerReversal' AND reversal_of_line_id IS NOT NULL;

CREATE TABLE reg_crypto_balance_balances (
    wallet_id        UUID        NOT NULL,
    token_id         UUID        NOT NULL REFERENCES cat_tokens(id),
    amount           NUMERIC(78,0) NOT NULL DEFAULT 0,
    last_movement_at TIMESTAMPTZ,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (wallet_id, token_id)
);
COMMENT ON TABLE reg_crypto_balance_balances IS 'Регистр крипто-балансов — текущие остатки';
CREATE INDEX idx_reg_crypto_balance_balances_token
    ON reg_crypto_balance_balances (token_id) WHERE amount != 0;
CREATE INDEX idx_reg_crypto_balance_balances_wallet
    ON reg_crypto_balance_balances (wallet_id) WHERE amount != 0;

-- Rate Sources catalog (Справочник «Источники курсов»)
CREATE TABLE cat_rate_sources (
    -- Base catalog fields
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    deletion_mark BOOLEAN     NOT NULL DEFAULT FALSE,
    version       INT         NOT NULL DEFAULT 1,
    attributes    JSONB       DEFAULT '{}',

    _deleted_at TIMESTAMPTZ,
    _txid       BIGINT DEFAULT txid_current(),

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    code      VARCHAR(50)  NOT NULL,
    name      VARCHAR(255) NOT NULL,
    parent_id UUID REFERENCES cat_rate_sources(id),
    is_folder BOOLEAN      NOT NULL DEFAULT FALSE,

    -- Rate source specific fields
    source_type    VARCHAR(50)  NOT NULL,            -- 'coingecko', 'binance', 'coinmarketcap', 'manual'
    base_url       VARCHAR(255),                     -- API base URL (e.g. 'https://api.coingecko.com/api/v3')
    integration_pool_id UUID REFERENCES cat_integration_pools(id) ON DELETE RESTRICT,
    api_key        TEXT CHECK (api_key IS NULL OR (
        octet_length(api_key) BETWEEN 1 AND 512 AND api_key !~ '[[:cntrl:]]'
    )),                                             -- write-only provider credential managed in ERP
    rate_limit_rpm INT          NOT NULL DEFAULT 100, -- requests per minute
    priority       INT          NOT NULL DEFAULT 100, -- for fallback ordering (lower = higher priority)
    is_active      BOOLEAN      NOT NULL DEFAULT TRUE
);
CREATE UNIQUE INDEX idx_cat_rate_sources_code ON cat_rate_sources (code) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_rate_sources_name ON cat_rate_sources USING gin (name gin_trgm_ops);
CREATE INDEX idx_cat_rate_sources_txid ON cat_rate_sources (_txid) WHERE _deleted_at IS NULL;
CREATE INDEX idx_cat_rate_sources_active ON cat_rate_sources (priority) WHERE is_active = TRUE AND deletion_mark = FALSE;
CREATE INDEX idx_cat_rate_sources_name_id ON cat_rate_sources (name ASC, id ASC);
COMMENT ON TABLE cat_rate_sources IS 'Справочник Источники курсов — провайдеры курсов валют (CoinGecko, Binance, ручной ввод)';

-- Rate Source Mappings register (Регистр сведений «Соответствие валют источникам»)
CREATE TABLE reg_rate_source_mappings (
    currency_id UUID NOT NULL REFERENCES cat_currencies(id),
    base_currency_id UUID NOT NULL REFERENCES cat_currencies(id),
    rate_source_id UUID NOT NULL REFERENCES cat_rate_sources(id),
    external_id VARCHAR(100) NOT NULL CHECK (length(trim(external_id)) > 0),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    version INT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ,
    PRIMARY KEY (currency_id, base_currency_id, rate_source_id),
    CHECK (currency_id <> base_currency_id)
);
CREATE INDEX idx_rate_source_mappings_active ON reg_rate_source_mappings(rate_source_id, base_currency_id) WHERE is_active;

CREATE TABLE reg_exchange_rates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    currency_id UUID NOT NULL REFERENCES cat_currencies(id),
    base_currency_id UUID NOT NULL REFERENCES cat_currencies(id),
    rate_source_id UUID NOT NULL REFERENCES cat_rate_sources(id),
    rate NUMERIC(38,18) NOT NULL CHECK (rate > 0),
    multiplier INT NOT NULL DEFAULT 1 CHECK (multiplier > 0),
    provider_updated_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    version INT NOT NULL CHECK (version > 0),
    mapping_version INT NOT NULL CHECK (mapping_version > 0),
    feed_attempt_id UUID NOT NULL,
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ,
    CHECK (provider_updated_at <= received_at),
    UNIQUE (currency_id, base_currency_id, rate_source_id, provider_updated_at, version),
    UNIQUE (feed_attempt_id, currency_id, base_currency_id, rate_source_id)
);
CREATE INDEX idx_reg_exchange_rates_latest ON reg_exchange_rates(currency_id, base_currency_id, rate_source_id, provider_updated_at DESC, version DESC);
CREATE INDEX idx_reg_exchange_rates_history ON reg_exchange_rates(currency_id, base_currency_id, provider_updated_at DESC, version DESC, id DESC);
COMMENT ON TABLE reg_exchange_rates IS 'Immutable provider observations; all history retained. multiplier currency = rate base_currency.';

CREATE TABLE sys_rate_feed_state (
    rate_source_id UUID NOT NULL REFERENCES cat_rate_sources(id),
    base_currency_id UUID NOT NULL REFERENCES cat_currencies(id),
    lease_token UUID,
    lease_until TIMESTAMPTZ,
    next_run_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_attempt_at TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    last_completed_token UUID,
    status TEXT NOT NULL DEFAULT 'idle',
    requested INT NOT NULL DEFAULT 0,
    received INT NOT NULL DEFAULT 0,
    inserted INT NOT NULL DEFAULT 0,
    repeated INT NOT NULL DEFAULT 0,
    rejected INT NOT NULL DEFAULT 0,
    problems JSONB NOT NULL DEFAULT '[]',
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ,
    PRIMARY KEY (rate_source_id, base_currency_id)
);

-- Crypto Merchant Balance accumulation register
-- (Регистр накопления «Крипто-расчёты с мерчантами»)
-- Аналог reg_settlement, но для крипто: отслеживает долг платформы перед мерчантом
CREATE TABLE reg_crypto_merchant_balance_movements (
    line_id          UUID         PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    recorder_id      UUID         NOT NULL,
    recorder_type    VARCHAR(50)  NOT NULL,
    recorder_version INT          NOT NULL DEFAULT 1,
    period           TIMESTAMPTZ  NOT NULL,
    record_type      VARCHAR(10)  NOT NULL,
    merchant_id      UUID         NOT NULL,
    token_id         UUID         NOT NULL REFERENCES cat_tokens(id),
    amount           NUMERIC(78,0) NOT NULL,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    reversal_of_line_id UUID         REFERENCES reg_crypto_merchant_balance_movements(line_id),
    reversal_reason  TEXT         NOT NULL DEFAULT '',
    CONSTRAINT chk_merchant_bal_record_type CHECK (record_type IN ('receipt', 'expense')),
    CONSTRAINT chk_merchant_bal_amount_positive CHECK (amount > 0),
    CONSTRAINT chk_provider_opening_no_merchant_effect CHECK (recorder_type<>'CryptoProviderBalanceOperation')
);
COMMENT ON TABLE reg_crypto_merchant_balance_movements IS 'Регистр крипто-расчётов с мерчантами — движения (аналог reg_settlement для крипто)';
CREATE INDEX idx_reg_crypto_merch_bal_movements_recorder
    ON reg_crypto_merchant_balance_movements (recorder_id, recorder_version);
CREATE INDEX idx_reg_crypto_merch_bal_movements_balance
    ON reg_crypto_merchant_balance_movements (merchant_id, token_id, record_type);
CREATE INDEX idx_reg_crypto_merch_bal_movements_period
    ON reg_crypto_merchant_balance_movements (period);
CREATE INDEX idx_crypto_merchant_reversal_line
    ON reg_crypto_merchant_balance_movements(reversal_of_line_id) WHERE reversal_of_line_id IS NOT NULL;
CREATE UNIQUE INDEX uq_crypto_merchant_ledger_reversal
    ON reg_crypto_merchant_balance_movements(reversal_of_line_id)
    WHERE recorder_type='CryptoLedgerReversal' AND reversal_of_line_id IS NOT NULL;

CREATE TABLE reg_crypto_merchant_balance_balances (
    merchant_id      UUID        NOT NULL,
    token_id         UUID        NOT NULL REFERENCES cat_tokens(id),
    amount           NUMERIC(78,0) NOT NULL DEFAULT 0,
    last_movement_at TIMESTAMPTZ,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (merchant_id, token_id)
);
COMMENT ON TABLE reg_crypto_merchant_balance_balances IS 'Регистр крипто-расчётов с мерчантами — текущие остатки (долг платформы перед мерчантом)';
CREATE INDEX idx_reg_crypto_merch_bal_balances_token
    ON reg_crypto_merchant_balance_balances (token_id) WHERE amount != 0;
CREATE INDEX idx_reg_crypto_merch_bal_balances_merchant
    ON reg_crypto_merchant_balance_balances (merchant_id) WHERE amount != 0;

-- MERCHANT CATALOG
CREATE TABLE cat_merchants (
    -- Base entity fields
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    code            TEXT NOT NULL DEFAULT '',
    name            TEXT NOT NULL,
    parent_id       UUID REFERENCES cat_merchants(id),
    is_folder       BOOLEAN NOT NULL DEFAULT FALSE,
    deletion_mark   BOOLEAN NOT NULL DEFAULT FALSE,
    version         INT NOT NULL DEFAULT 1,
    attributes      JSONB NOT NULL DEFAULT '{}'::jsonb,

    -- Timestamps
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Merchant-specific fields
    legal_name      TEXT NOT NULL DEFAULT '',
    webhook_url     TEXT NOT NULL DEFAULT '',
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    kyb_status      TEXT NOT NULL DEFAULT 'pending' CHECK (kyb_status IN ('pending','approved','rejected')),

    -- CDC fields
    _txid           BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at     TIMESTAMPTZ
);
CREATE UNIQUE INDEX idx_cat_merchants_code ON cat_merchants (code) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_merchants_name ON cat_merchants (name) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_merchants_name_id ON cat_merchants (name ASC, id ASC);

-- MERCHANT-USER JUNCTION TABLE (M:N)
CREATE TABLE sys_merchant_users (
    user_id     UUID NOT NULL,
    merchant_id UUID NOT NULL REFERENCES cat_merchants(id),
    role        INT NOT NULL DEFAULT 1 CHECK (role BETWEEN 1 AND 3),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, merchant_id)
);
CREATE INDEX idx_merchant_users_merchant ON sys_merchant_users (merchant_id);

-- WALLET CATALOG
CREATE TABLE cat_wallets (
    -- Base entity fields
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    code            TEXT NOT NULL DEFAULT '',
    name            TEXT NOT NULL,
    parent_id       UUID REFERENCES cat_wallets(id),
    is_folder       BOOLEAN NOT NULL DEFAULT FALSE,
    deletion_mark   BOOLEAN NOT NULL DEFAULT FALSE,
    version         INT NOT NULL DEFAULT 1,
    attributes      JSONB NOT NULL DEFAULT '{}'::jsonb,

    -- Timestamps
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Wallet-specific fields
    network_id      UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    merchant_id     UUID REFERENCES cat_merchants(id),
    address         TEXT NOT NULL,
    address_key     TEXT NOT NULL DEFAULT '',
    derivation_path TEXT NOT NULL DEFAULT '',
    vault_key_id    TEXT NOT NULL DEFAULT '',
    tier            TEXT NOT NULL DEFAULT 'pool' CHECK (tier IN ('pool','hot','warm','cold')),
    wallet_role     TEXT NOT NULL DEFAULT 'merchant_pool'
                    CHECK (wallet_role IN ('merchant_pool','liquidity_hot','network_fee','treasury')),
    status          TEXT NOT NULL DEFAULT 'free' CHECK (status IN ('free','leased','assigned','sweep_pending','sweeping','frozen')),
    leased_until    TIMESTAMPTZ,
    leased_for_id   UUID,
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    chain_activation_status TEXT NOT NULL DEFAULT 'unknown'
        CHECK (chain_activation_status IN ('unknown','activating','active','held','failed')),
    cooldown_until TIMESTAMPTZ,

    -- Allocation mode: transient (lease per invoice) or persistent (assigned to customer)
    allocation_mode TEXT NOT NULL DEFAULT 'transient' CHECK (allocation_mode IN ('transient', 'persistent')),
    customer_id     TEXT NOT NULL DEFAULT '',  -- external customer ID (for persistent wallets)
    deposit_assigned_at TIMESTAMPTZ,
    deposit_retired_at  TIMESTAMPTZ,
    CONSTRAINT chk_deposit_address_retired CHECK (deposit_retired_at IS NULL OR
        (allocation_mode='persistent' AND merchant_id IS NOT NULL AND customer_id<>''
         AND status IN ('assigned','frozen') AND is_active AND NOT deletion_mark AND _deleted_at IS NULL)),
    last_swept_at   TIMESTAMPTZ,      -- when last sweep was executed

    -- CDC fields
    _txid           BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at     TIMESTAMPTZ,
    custody_merchant_id UUID REFERENCES cat_merchants(id),
    CONSTRAINT uq_crypto_wallet_custody_scope UNIQUE(id,network_id,custody_merchant_id)
);
-- Unique address per network (prevent duplicate allocation)
CREATE UNIQUE INDEX idx_cat_wallets_network_address
    ON cat_wallets (network_id, address_key);
CREATE INDEX idx_cat_wallets_active_free_pool
    ON cat_wallets (network_id)
    WHERE status = 'free'
      AND tier = 'pool'
      AND chain_activation_status = 'active'
      AND is_active = TRUE
      AND deletion_mark = FALSE;
CREATE INDEX idx_cat_wallets_cooldown
    ON cat_wallets (network_id, cooldown_until)
    WHERE status = 'free' AND cooldown_until IS NOT NULL;
-- Fast free wallet lookup for LeaseForInvoice (transient only)
CREATE INDEX idx_cat_wallets_free_pool
    ON cat_wallets (network_id)
    WHERE status = 'free' AND tier = 'pool' AND allocation_mode = 'transient'
      AND is_active = TRUE AND deletion_mark = FALSE;
CREATE UNIQUE INDEX idx_cat_wallets_code ON cat_wallets (code) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_wallets_merchant ON cat_wallets (merchant_id) WHERE merchant_id IS NOT NULL;
CREATE INDEX idx_cat_wallets_name ON cat_wallets (name) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_wallets_name_id ON cat_wallets (name ASC, id ASC);
-- Persistent customer address uniqueness: one wallet per (merchant, network, customer).
-- Prevents TOCTOU race in AssignPersistentAddress: if two concurrent requests
-- both pass the idempotency check, the second UPDATE will fail with a unique violation.
CREATE UNIQUE INDEX idx_cat_wallets_persistent_customer
    ON cat_wallets (merchant_id, network_id, customer_id)
    WHERE allocation_mode = 'persistent' AND deletion_mark = FALSE AND customer_id != ''
      AND deposit_retired_at IS NULL;
CREATE INDEX idx_cat_wallets_allocation_ready
    ON cat_wallets (network_id, status, tier, chain_activation_status, id)
    WHERE is_active = TRUE AND deletion_mark = FALSE;
CREATE UNIQUE INDEX idx_cat_wallets_network_derivation_path
    ON cat_wallets (network_id, derivation_path)
    WHERE derivation_path <> '';
CREATE INDEX idx_crypto_wallet_custody_owner
    ON cat_wallets(custody_merchant_id,network_id,wallet_role,id)
    WHERE custody_merchant_id IS NOT NULL AND deletion_mark=FALSE;
CREATE UNIQUE INDEX idx_cat_wallets_one_active_network_fee
    ON cat_wallets(network_id)
    WHERE wallet_role='network_fee' AND custody_merchant_id IS NULL AND is_active AND NOT deletion_mark;
CREATE UNIQUE INDEX idx_cat_wallets_one_merchant_network_fee
    ON cat_wallets(network_id,custody_merchant_id)
    WHERE wallet_role='network_fee' AND custody_merchant_id IS NOT NULL AND is_active AND NOT deletion_mark;

CREATE TABLE reg_crypto_deposit_address_rotations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    merchant_id UUID NOT NULL REFERENCES cat_merchants(id),
    network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    customer_id TEXT NOT NULL CHECK (BTRIM(customer_id)<>''),
    old_wallet_id UUID NOT NULL UNIQUE REFERENCES cat_wallets(id),
    new_wallet_id UUID NOT NULL UNIQUE REFERENCES cat_wallets(id),
    network_code TEXT NOT NULL,
    network_name TEXT NOT NULL,
    tokens TEXT[] NOT NULL,
    old_address TEXT NOT NULL,
    new_address TEXT NOT NULL,
    reason TEXT NOT NULL CHECK (reason IN ('aml_high_risk','customer_request')),
    actor_id UUID NOT NULL,
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    version INT NOT NULL CHECK (version>0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ,
    UNIQUE (merchant_id,idempotency_key),
    CHECK (old_wallet_id<>new_wallet_id)
);
CREATE INDEX idx_deposit_rotations_customer
    ON reg_crypto_deposit_address_rotations (merchant_id,customer_id,created_at DESC);
CREATE INDEX idx_deposit_rotations_merchant_window
    ON reg_crypto_deposit_address_rotations (merchant_id,network_id,created_at DESC);


-- Merchant Token Config (Регистр сведений «Настройки токенов мерчанта»)
-- NULL = use token default from cat_tokens
CREATE TABLE reg_merchant_token_config (
    merchant_id         UUID NOT NULL REFERENCES cat_merchants(id),
    token_id            UUID NOT NULL REFERENCES cat_tokens(id),

    -- Sweep overrides (NULL = use token default)
    sweep_threshold     NUMERIC(78,0),
    sweep_max_age_hours INT,
    sweep_retain_amount NUMERIC(78,0),

    -- Audit
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (merchant_id, token_id),
    CONSTRAINT chk_mtc_sweep_threshold CHECK (sweep_threshold IS NULL OR sweep_threshold >= 0),
    CONSTRAINT chk_mtc_sweep_max_age CHECK (sweep_max_age_hours IS NULL OR sweep_max_age_hours >= 0),
    CONSTRAINT chk_mtc_sweep_retain_amount CHECK (sweep_retain_amount IS NULL OR sweep_retain_amount >= 0)
);
COMMENT ON TABLE reg_merchant_token_config IS
    'Регистр сведений: крипто-настройки мерчанта per token. NULL = использовать дефолт из cat_tokens.';

-- MERCHANT API KEYS
-- Stores hashed API keys for the merchant public API (/merchant/v1/).
-- The plaintext key is shown once at creation and never stored.
-- key_hash = SHA-256(plaintext) stored as lowercase hex.
CREATE TABLE cat_merchant_api_keys (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    merchant_id         UUID        NOT NULL REFERENCES cat_merchants(id) ON DELETE CASCADE,
    name                TEXT        NOT NULL,                   -- human-readable label ("Production Key")
    key_prefix          VARCHAR(16) NOT NULL,                   -- "mk_" + first 8 chars, for UI display
    key_hash            CHAR(64)    NOT NULL,                   -- hex(SHA-256(plaintext_key))
    scopes              TEXT[]      NOT NULL DEFAULT ARRAY['invoice:create','invoice:read'],
    is_active           BOOLEAN     NOT NULL DEFAULT TRUE,
    last_used_at        TIMESTAMPTZ,
    expires_at          TIMESTAMPTZ,
    -- Audit: which platform user issued this key (best-effort, no FK — users are in auth schema).
    -- NULL for keys created before this field was added or via automated processes.
    created_by_user_id  UUID,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (key_hash),
    CONSTRAINT uq_merchant_api_key_identity UNIQUE (id,merchant_id)
);
-- Hot-path: auth lookup by hash (only active keys)
CREATE UNIQUE INDEX idx_merchant_api_keys_hash
    ON cat_merchant_api_keys (key_hash)
    WHERE is_active = TRUE;
CREATE INDEX idx_merchant_api_keys_merchant
    ON cat_merchant_api_keys (merchant_id);

CREATE TABLE auth_merchant_network_policies (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    merchant_id    UUID NOT NULL REFERENCES cat_merchants(id) ON DELETE CASCADE,
    surface        TEXT NOT NULL CHECK (surface IN ('portal','api')),
    api_key_id     UUID,
    mode           TEXT NOT NULL DEFAULT 'off' CHECK (mode IN ('off','warn','enforce')),
    version        INT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by     UUID,
    updated_by     UUID,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_merchant_network_policy_surface CHECK (
      (surface='portal' AND api_key_id IS NULL) OR surface='api'
    ),
    CONSTRAINT fk_merchant_network_policy_api_key FOREIGN KEY (api_key_id,merchant_id)
      REFERENCES cat_merchant_api_keys(id,merchant_id)
);
CREATE UNIQUE INDEX uq_merchant_network_policy_baseline
    ON auth_merchant_network_policies(merchant_id,surface) WHERE api_key_id IS NULL;
CREATE UNIQUE INDEX uq_merchant_network_policy_api_key
    ON auth_merchant_network_policies(merchant_id,surface,api_key_id) WHERE api_key_id IS NOT NULL;

CREATE TABLE auth_merchant_allowed_networks (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    policy_id      UUID NOT NULL REFERENCES auth_merchant_network_policies(id) ON DELETE RESTRICT,
    network        CIDR NOT NULL,
    label          TEXT NOT NULL DEFAULT '',
    is_active      BOOLEAN NOT NULL DEFAULT TRUE,
    created_by     UUID,
    revoked_by     UUID,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at     TIMESTAMPTZ,
    CONSTRAINT chk_merchant_allowed_network_revocation CHECK (
      (is_active AND revoked_at IS NULL AND revoked_by IS NULL)
      OR (NOT is_active AND revoked_at IS NOT NULL)
    )
);
CREATE UNIQUE INDEX uq_merchant_allowed_network_active
    ON auth_merchant_allowed_networks(policy_id,network) WHERE is_active;

CREATE TABLE auth_merchant_network_policy_audit (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    merchant_id UUID NOT NULL,
    policy_id   UUID,
    actor_type  TEXT NOT NULL CHECK (actor_type IN ('user','api_key','system','system_admin','request')),
    actor_id    TEXT NOT NULL,
    action      TEXT NOT NULL,
    decision    TEXT NOT NULL DEFAULT '' CHECK (decision IN ('','allow','warn','deny')),
    client_ip   INET,
    old_policy  JSONB NOT NULL DEFAULT '{}'::jsonb,
    new_policy  JSONB NOT NULL DEFAULT '{}'::jsonb,
    reason      TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- CRYPTO FEE REGISTER
CREATE TABLE reg_crypto_fee_movements (
    line_id           UUID PRIMARY KEY,
    recorder_id       UUID NOT NULL,
    recorder_type     TEXT NOT NULL,
    recorder_version  INT NOT NULL,
    period            TIMESTAMPTZ NOT NULL,
    record_type       TEXT NOT NULL CHECK (record_type IN ('receipt', 'expense')),

    -- Dimensions
    merchant_id       UUID NOT NULL,
    token_id          UUID NOT NULL,
    fee_type          TEXT NOT NULL CHECK (fee_type IN ('processing','network','withdrawal','sweep','payout','settlement','refund')),

    -- Resources
    amount            NUMERIC(78,0) NOT NULL,

    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reversal_of_line_id UUID REFERENCES reg_crypto_fee_movements(line_id),
    reversal_reason   TEXT NOT NULL DEFAULT '',
    CONSTRAINT chk_provider_opening_no_fee_effect CHECK (recorder_type<>'CryptoProviderBalanceOperation')
);
-- Immutable ledger: no UPDATE trigger, only DELETE + INSERT
CREATE INDEX idx_reg_crypto_fee_movements_recorder ON reg_crypto_fee_movements (recorder_id);
CREATE INDEX idx_reg_crypto_fee_movements_merchant ON reg_crypto_fee_movements (merchant_id, token_id, period);
CREATE INDEX idx_crypto_fee_reversal_line
    ON reg_crypto_fee_movements(reversal_of_line_id) WHERE reversal_of_line_id IS NOT NULL;
CREATE UNIQUE INDEX uq_crypto_fee_ledger_reversal
    ON reg_crypto_fee_movements(reversal_of_line_id)
    WHERE recorder_type='CryptoLedgerReversal' AND reversal_of_line_id IS NOT NULL;

-- Balance materialized table (maintained by trigger)
CREATE TABLE reg_crypto_fee_balances (
    merchant_id   UUID NOT NULL,
    token_id      UUID NOT NULL,
    fee_type      TEXT NOT NULL,
    balance       NUMERIC(78,0) NOT NULL DEFAULT 0,
    PRIMARY KEY (merchant_id, token_id, fee_type)
);

-- FEE SCHEDULE (Регистр сведений «Тарифы комиссий»)
-- NULL merchant_id = global default for all merchants
CREATE TABLE reg_fee_schedule (
    -- Dimensions
    merchant_id  UUID REFERENCES cat_merchants(id),   -- NULL = global default
    token_id     UUID NOT NULL REFERENCES cat_tokens(id),
    direction    TEXT NOT NULL CHECK (direction IN ('processing', 'withdrawal', 'payout', 'settlement', 'refund')),

    -- Fee formula: clamp(fixed_fee + amount × percent_bp / 10000, min_fee, max_fee)
    fixed_fee    NUMERIC(78,0) NOT NULL DEFAULT 0,
    percent_bp   INT    NOT NULL DEFAULT 0,           -- basis points [0..10000]
    min_fee      NUMERIC(78,0) NOT NULL DEFAULT 0,
    max_fee      NUMERIC(78,0) NOT NULL DEFAULT 0,

    -- Audit
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT chk_fs_percent CHECK (percent_bp >= 0 AND percent_bp <= 10000),
    CONSTRAINT chk_fs_fixed   CHECK (fixed_fee >= 0),
    CONSTRAINT chk_fs_min     CHECK (min_fee >= 0),
    CONSTRAINT chk_fs_max     CHECK (max_fee >= 0)
);
-- Composite unique: COALESCE maps NULL merchant_id → zero UUID for uniqueness
CREATE UNIQUE INDEX idx_fee_schedule_pk
    ON reg_fee_schedule (COALESCE(merchant_id, '00000000-0000-0000-0000-000000000000'::UUID), token_id, direction);
COMMENT ON TABLE reg_fee_schedule IS
    'Регистр сведений: тарифы комиссий. NULL merchant_id = глобальный дефолт.';
-- Fast lookup: merchant-specific fee
CREATE INDEX idx_fee_schedule_merchant ON reg_fee_schedule (merchant_id, token_id, direction)
    WHERE merchant_id IS NOT NULL;
-- Fast lookup: global defaults
CREATE INDEX idx_fee_schedule_global ON reg_fee_schedule (token_id, direction)
    WHERE merchant_id IS NULL;

-- CHAIN WATCHER STATE (checkpoint persistence)
CREATE TABLE sys_chain_watcher_state (
    network_id      UUID NOT NULL,
    watcher_key     TEXT NOT NULL DEFAULT 'trc20_primary',
    last_block      BIGINT NOT NULL DEFAULT 0,
    last_timestamp  BIGINT NOT NULL DEFAULT 0,
    fingerprint     TEXT NOT NULL DEFAULT '',
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    lease_owner     TEXT,
    fencing_token   BIGINT NOT NULL DEFAULT 0,
    lease_expires_at TIMESTAMPTZ,
    PRIMARY KEY (network_id, watcher_key)
);

-- Payment Links (Платёжные ссылки)
-- A payment link is a reusable template that creates invoices on demand.
-- When a customer opens the link, the system creates a fresh invoice
-- from the template (amount, token, description) and redirects to checkout.
CREATE TABLE doc_payment_links (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    deletion_mark   BOOLEAN     NOT NULL DEFAULT FALSE,
    version         INT         NOT NULL DEFAULT 1,
    merchant_id     UUID        NOT NULL,
    token_id        UUID        NOT NULL REFERENCES cat_tokens(id),
    amount          NUMERIC(78,0) NOT NULL,
    description     TEXT        NOT NULL DEFAULT '',
    public_ref      TEXT        NOT NULL,             -- URL: /pay/l/pl_<128-bit-hex>
    reusable        BOOLEAN     NOT NULL DEFAULT FALSE,
    max_uses        INT         NOT NULL DEFAULT 1,   -- 0 = unlimited (only if reusable=true)
    current_uses    INT         NOT NULL DEFAULT 0,
    status          TEXT        NOT NULL DEFAULT 'active', -- active | disabled
    ttl_minutes     INT         NOT NULL DEFAULT 60,  -- TTL for generated invoices
    created_by      UUID,                              -- platform user who created
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- CDC
    _deleted_at     TIMESTAMPTZ,
    _txid           BIGINT DEFAULT txid_current(),

    CONSTRAINT uq_payment_link_public_ref UNIQUE (public_ref),
    CONSTRAINT chk_payment_link_amount_positive CHECK (amount > 0),
    CONSTRAINT chk_payment_link_public_ref_format CHECK (public_ref ~ '^pl_[a-f0-9]{32}$'),
    CONSTRAINT chk_payment_link_max_uses_nonneg CHECK (max_uses >= 0),
    CONSTRAINT chk_payment_link_current_uses_nonneg CHECK (current_uses >= 0),
    CONSTRAINT chk_payment_link_status_valid CHECK (status IN ('active', 'disabled')),
    CONSTRAINT chk_payment_link_ttl_valid CHECK (ttl_minutes BETWEEN 5 AND 10080)
);
-- Indexes
CREATE INDEX idx_payment_links_merchant    ON doc_payment_links (merchant_id);
CREATE INDEX idx_payment_links_public_ref  ON doc_payment_links (public_ref) WHERE status = 'active';
CREATE INDEX idx_payment_links_status      ON doc_payment_links (status, merchant_id);
-- CDC
CREATE INDEX idx_doc_payment_links_txid ON doc_payment_links (_txid) WHERE _deleted_at IS NULL;
COMMENT ON TABLE doc_payment_links IS 'Платёжные ссылки — многоразовые шаблоны для создания инвойсов без API интеграции';
COMMENT ON COLUMN doc_payment_links.public_ref IS 'Opaque customer-facing checkout reference: pl_<128-bit-hex>';
COMMENT ON COLUMN doc_payment_links.reusable IS 'true = можно использовать многократно (до max_uses)';
COMMENT ON COLUMN doc_payment_links.max_uses IS 'Лимит использований (0 = безлимит). Действует только при reusable=true';

CREATE TABLE sys_crypto_wallet_pool_policies (
    network_id                      UUID PRIMARY KEY REFERENCES cat_blockchain_networks(id),
    enabled                         BOOLEAN NOT NULL DEFAULT FALSE,
    low_watermark                   INT NOT NULL DEFAULT 0,
    target_ready                    INT NOT NULL DEFAULT 1,
    max_total_wallets               INT NOT NULL DEFAULT 1,
    max_provision_batch             INT NOT NULL DEFAULT 1,
    -- NULL activation columns mean that this family derives usable addresses
    -- without a separate on-chain account-materialization step.
    max_concurrent_activations      INT,
    max_active_invoice_leases_per_merchant INT NOT NULL DEFAULT 1,
    max_persistent_addresses_per_merchant  INT NOT NULL DEFAULT 1,
    max_lease_starts_per_minute_per_merchant INT NOT NULL DEFAULT 5,
    min_global_ready_reserve        INT NOT NULL DEFAULT 0,
    activation_reserve_minor        NUMERIC(78,0),
    min_hot_native_balance_minor    NUMERIC(78,0),
    daily_activation_budget_minor   NUMERIC(78,0),
    retry_after_seconds             INT NOT NULL DEFAULT 30,
    address_reuse_cooldown_seconds  INT NOT NULL DEFAULT 0,
    max_watcher_lag_seconds         INT NOT NULL DEFAULT 0,
    lease_blocked_reason            TEXT NOT NULL DEFAULT '',
    lease_blocked_at                TIMESTAMPTZ,
    derivation_namespace            TEXT NOT NULL DEFAULT '',
    needs_review                    BOOLEAN NOT NULL DEFAULT TRUE,
    version                         INT NOT NULL DEFAULT 1,
    updated_at                      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by                      TEXT NOT NULL DEFAULT '',
    last_reconciled_at              TIMESTAMPTZ,
    last_error_code                 TEXT NOT NULL DEFAULT '',
    last_error_message              TEXT NOT NULL DEFAULT '',
    last_emitted_state              TEXT NOT NULL DEFAULT '',
    created_at                      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid                           BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at                     TIMESTAMPTZ,
    CONSTRAINT chk_wallet_pool_watermarks
        CHECK (low_watermark >= 0
           AND low_watermark < target_ready
           AND target_ready <= max_total_wallets),
    CONSTRAINT chk_wallet_pool_limits
        CHECK (max_provision_batch > 0
           AND max_active_invoice_leases_per_merchant > 0
           AND max_persistent_addresses_per_merchant > 0
           AND max_lease_starts_per_minute_per_merchant > 0
           AND min_global_ready_reserve >= 0
           AND min_global_ready_reserve <= low_watermark
           AND retry_after_seconds > 0
           AND address_reuse_cooldown_seconds >= 0
           AND max_watcher_lag_seconds >= 0
           AND (address_reuse_cooldown_seconds = 0 OR max_watcher_lag_seconds > 0)
           AND (lease_blocked_reason = '' OR lease_blocked_reason = 'watcher_lag')),
    CONSTRAINT chk_wallet_pool_activation_policy CHECK (
        (
            max_concurrent_activations IS NULL
            AND activation_reserve_minor IS NULL
            AND min_hot_native_balance_minor IS NULL
            AND daily_activation_budget_minor IS NULL
        ) OR (
            max_concurrent_activations IS NOT NULL
            AND activation_reserve_minor IS NOT NULL
            AND min_hot_native_balance_minor IS NOT NULL
            AND daily_activation_budget_minor IS NOT NULL
            AND max_concurrent_activations > 0
            AND activation_reserve_minor >= 0
            AND activation_reserve_minor <= 9007199254740991
            AND min_hot_native_balance_minor >= 0
            AND min_hot_native_balance_minor <= 9007199254740991
            AND daily_activation_budget_minor >= 0
            AND daily_activation_budget_minor <= 9007199254740991
        )
    ),
    CONSTRAINT chk_wallet_pool_version CHECK (version > 0)
);
CREATE INDEX idx_wallet_pool_policies_txid
    ON sys_crypto_wallet_pool_policies (_txid)
    WHERE _deleted_at IS NULL;

-- Optional per-merchant exceptions. NULL means "inherit the network default".
-- Tenant isolation is provided by the physical tenant database boundary.
CREATE TABLE reg_merchant_wallet_allocation_overrides (
    merchant_id                      UUID NOT NULL REFERENCES cat_merchants(id) ON DELETE CASCADE,
    network_id                       UUID NOT NULL REFERENCES cat_blockchain_networks(id) ON DELETE CASCADE,
    max_active_invoice_leases        INT,
    max_persistent_addresses         INT,
    max_lease_starts_per_minute      INT,
    version                          INT NOT NULL DEFAULT 1,
    created_at                       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by                       TEXT NOT NULL DEFAULT '',
    _txid                            BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at                      TIMESTAMPTZ,
    PRIMARY KEY (merchant_id, network_id),
    CONSTRAINT chk_merchant_wallet_allocation_override_limits
        CHECK ((max_active_invoice_leases IS NULL OR max_active_invoice_leases > 0)
           AND (max_persistent_addresses IS NULL OR max_persistent_addresses > 0)
           AND (max_lease_starts_per_minute IS NULL OR max_lease_starts_per_minute > 0)),
    CONSTRAINT chk_merchant_wallet_allocation_override_version CHECK (version > 0)
);
CREATE INDEX idx_merchant_wallet_allocation_overrides_txid
    ON reg_merchant_wallet_allocation_overrides (_txid)
    WHERE _deleted_at IS NULL;

-- Fixed one-minute admission window. Updates are serialized by the matching
-- network policy row, so a burst cannot oversubscribe the configured limit.
CREATE TABLE reg_merchant_wallet_lease_rate_windows (
    merchant_id       UUID NOT NULL REFERENCES cat_merchants(id) ON DELETE CASCADE,
    network_id        UUID NOT NULL REFERENCES cat_blockchain_networks(id) ON DELETE CASCADE,
    window_started_at TIMESTAMPTZ NOT NULL,
    lease_starts      INT NOT NULL DEFAULT 0 CHECK (lease_starts >= 0),
    version           INT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid             BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at       TIMESTAMPTZ,
    PRIMARY KEY (merchant_id, network_id)
);
CREATE INDEX idx_merchant_wallet_lease_rate_windows_txid
    ON reg_merchant_wallet_lease_rate_windows (_txid)
    WHERE _deleted_at IS NULL;

-- A single append-only publication registry fences every HD path across
-- wallets and provisioning jobs. A path is inserted before it is
-- sent to Vault and is never deleted or reused.
CREATE TABLE sys_crypto_wallet_derivations (
    network_id       UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    derivation_path  TEXT NOT NULL,
    -- Keep the published wallet identity even if an operator attempts a hard
    -- delete. Wallets use soft deletion; a hard delete must not make an HD
    -- path reusable.
    wallet_id        UUID REFERENCES cat_wallets(id),
    source           TEXT NOT NULL
                     CHECK (source IN ('wallet','provisioning','bootstrap')),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    version          INT NOT NULL DEFAULT 1 CHECK (version > 0),
    _txid            BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at      TIMESTAMPTZ,
    PRIMARY KEY (network_id, derivation_path),
    CONSTRAINT chk_crypto_wallet_derivation_path_nonempty
        CHECK (BTRIM(derivation_path) <> '')
);
CREATE INDEX idx_wallet_derivations_txid
    ON sys_crypto_wallet_derivations (_txid)
    WHERE _deleted_at IS NULL;

CREATE TABLE sys_crypto_wallet_derivation_cursors (
    network_id           UUID PRIMARY KEY REFERENCES cat_blockchain_networks(id),
    derivation_namespace TEXT NOT NULL,
    next_index           BIGINT NOT NULL CHECK (next_index >= 0),
    needs_review         BOOLEAN NOT NULL DEFAULT FALSE,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    version              INT NOT NULL DEFAULT 1 CHECK (version > 0),
    _txid                BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at          TIMESTAMPTZ,
    CONSTRAINT uq_wallet_derivation_cursor_namespace
        UNIQUE (network_id, derivation_namespace)
);
CREATE INDEX idx_wallet_derivation_cursors_txid
    ON sys_crypto_wallet_derivation_cursors (_txid)
    WHERE _deleted_at IS NULL;

CREATE TABLE sys_crypto_wallet_provisioning_jobs (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    network_id          UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    derivation_path     TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending','processing','retry_wait','completed','held')),
    attempts            INT NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    claimed_by          TEXT NOT NULL DEFAULT '',
    claim_token         UUID,
    claimed_at          TIMESTAMPTZ,
    lease_expires_at    TIMESTAMPTZ,
    last_error          TEXT NOT NULL DEFAULT '',
    wallet_id           UUID REFERENCES cat_wallets(id),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at        TIMESTAMPTZ,
    version             INT NOT NULL DEFAULT 1 CHECK (version > 0),
    _txid               BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at         TIMESTAMPTZ,
    CONSTRAINT uq_wallet_provisioning_derivation_path
        UNIQUE (network_id, derivation_path),
    CONSTRAINT fk_wallet_provisioning_derivation
        FOREIGN KEY (network_id, derivation_path)
        REFERENCES sys_crypto_wallet_derivations (network_id, derivation_path)
);
CREATE INDEX idx_wallet_provisioning_claim
    ON sys_crypto_wallet_provisioning_jobs (next_attempt_at, created_at)
    WHERE status IN ('pending','retry_wait','processing');
CREATE INDEX idx_wallet_provisioning_network_status
    ON sys_crypto_wallet_provisioning_jobs (network_id, status);
CREATE INDEX idx_wallet_provisioning_jobs_txid
    ON sys_crypto_wallet_provisioning_jobs (_txid)
    WHERE _deleted_at IS NULL;

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
    ingestion               JSONB,
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

CREATE TABLE reg_crypto_fee_reservation_events (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    reservation_id     UUID NOT NULL REFERENCES reg_crypto_fee_reservations(id),
    from_status        TEXT NOT NULL,
    to_status          TEXT NOT NULL,
    reason             TEXT NOT NULL DEFAULT '',
    amount_minor       NUMERIC(78,0) NOT NULL DEFAULT 0 CHECK (amount_minor >= 0),
    metadata           JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    transition_txid    BIGINT
);
CREATE INDEX idx_crypto_fee_reservation_events_reservation
    ON reg_crypto_fee_reservation_events (reservation_id, created_at, id);

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
    network_fee_reservation_id UUID NOT NULL REFERENCES reg_crypto_fee_reservations(id),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_crypto_chain_settlement_tx
    ON reg_crypto_chain_settlements (network_id, tx_hash);
CREATE UNIQUE INDEX idx_crypto_chain_settlement_basis
    ON reg_crypto_chain_settlements (basis_type, basis_id);

CREATE TABLE reg_crypto_chain_settlement_components (
    settlement_id   UUID NOT NULL REFERENCES reg_crypto_chain_settlements(id),
    component       TEXT NOT NULL,
    amount_minor    NUMERIC(78,0) NOT NULL DEFAULT 0 CHECK (amount_minor >= 0),
    quantity        BIGINT NOT NULL DEFAULT 0 CHECK (quantity >= 0),
    metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (settlement_id, component)
);

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

CREATE TABLE sys_chain_watcher_block_history (
    network_id   UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    watcher_key  TEXT NOT NULL,
    block_number BIGINT NOT NULL CHECK (block_number >= 0),
    block_hash   TEXT NOT NULL,
    block_timestamp BIGINT NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (network_id,watcher_key,block_number)
);

CREATE TABLE reg_crypto_wallet_allocations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    wallet_id       UUID NOT NULL REFERENCES cat_wallets(id),
    invoice_id      UUID NOT NULL REFERENCES doc_crypto_invoices(id) DEFERRABLE INITIALLY DEFERRED,
    merchant_id     UUID NOT NULL REFERENCES cat_merchants(id),
    token_id        UUID NOT NULL REFERENCES cat_tokens(id),
    network_id      UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    leased_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    closed_at       TIMESTAMPTZ,
    cooldown_until  TIMESTAMPTZ,
    close_reason    TEXT NOT NULL DEFAULT ''
                    CHECK (close_reason IN ('', 'expired', 'cancelled', 'confirmed', 'orphaned_lease')),
    version         INT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid           BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at     TIMESTAMPTZ,
    CONSTRAINT chk_allocation_close CHECK (
        (closed_at IS NULL AND cooldown_until IS NULL AND close_reason = '')
        OR (closed_at IS NOT NULL AND cooldown_until IS NOT NULL AND close_reason <> '')
    ),
    CONSTRAINT chk_allocation_window CHECK (
        closed_at IS NULL OR closed_at >= leased_at
    ),
    CONSTRAINT excl_crypto_wallet_allocations_window
    EXCLUDE USING gist (
        wallet_id WITH =,
        tstzrange(
            leased_at,
            COALESCE(cooldown_until, 'infinity'::timestamptz),
            '[]'
        ) WITH &&
    )
    WHERE (_deleted_at IS NULL)
);
CREATE UNIQUE INDEX idx_crypto_wallet_allocations_invoice
    ON reg_crypto_wallet_allocations (invoice_id)
    WHERE _deleted_at IS NULL;
CREATE UNIQUE INDEX idx_crypto_wallet_allocations_open_wallet
    ON reg_crypto_wallet_allocations (wallet_id)
    WHERE closed_at IS NULL AND _deleted_at IS NULL;
CREATE UNIQUE INDEX idx_crypto_wallet_allocations_wallet_leased_at
    ON reg_crypto_wallet_allocations (wallet_id, leased_at)
    WHERE _deleted_at IS NULL;
CREATE INDEX idx_crypto_wallet_allocations_cover
    ON reg_crypto_wallet_allocations (wallet_id, token_id, leased_at, cooldown_until)
    WHERE _deleted_at IS NULL;
CREATE INDEX idx_crypto_wallet_allocations_txid
    ON reg_crypto_wallet_allocations (_txid)
    WHERE _deleted_at IS NULL;

CREATE TABLE cat_merchant_webhook_endpoints (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    merchant_id UUID NOT NULL REFERENCES cat_merchants(id),
    name VARCHAR(100) NOT NULL,
    url TEXT NOT NULL,
    enabled_events TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    secret_ref TEXT NOT NULL,
    secret_version INT NOT NULL DEFAULT 1,
    previous_secret_ref TEXT NOT NULL DEFAULT '',
    previous_secret_version INT,
    previous_secret_valid_until TIMESTAMPTZ,
    version INT NOT NULL DEFAULT 1,
    deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
    attributes JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ,
    CONSTRAINT chk_merchant_webhook_endpoint_name CHECK (BTRIM(name) <> ''),
    CONSTRAINT chk_merchant_webhook_endpoint_url CHECK (url ~ '^https://'),
    CONSTRAINT chk_merchant_webhook_endpoint_secret_ref CHECK (BTRIM(secret_ref) <> ''),
    CONSTRAINT chk_merchant_webhook_endpoint_secret_version CHECK (secret_version >= 1),
    CONSTRAINT chk_merchant_webhook_endpoint_version CHECK (version >= 1),
    CONSTRAINT chk_merchant_webhook_endpoint_attributes CHECK (jsonb_typeof(attributes) = 'object'),
    CONSTRAINT chk_merchant_webhook_endpoint_previous_secret CHECK (
        (previous_secret_ref = '' AND previous_secret_version IS NULL AND previous_secret_valid_until IS NULL)
        OR
        (previous_secret_ref <> '' AND previous_secret_version >= 1 AND previous_secret_valid_until IS NOT NULL)
    )
);
CREATE UNIQUE INDEX uq_merchant_webhook_default
    ON cat_merchant_webhook_endpoints(merchant_id)
    WHERE is_default AND is_active AND deletion_mark = FALSE;
CREATE INDEX idx_merchant_webhook_endpoints_merchant
    ON cat_merchant_webhook_endpoints(merchant_id, created_at DESC)
    WHERE deletion_mark = FALSE;
CREATE INDEX idx_merchant_webhook_endpoints_txid
    ON cat_merchant_webhook_endpoints(_txid)
    WHERE _deleted_at IS NULL;

CREATE TABLE sys_merchant_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    event_id TEXT NOT NULL UNIQUE,
    merchant_id UUID NOT NULL REFERENCES cat_merchants(id),
    aggregate_type TEXT NOT NULL,
    aggregate_id UUID NOT NULL,
    invoice_id UUID REFERENCES doc_crypto_invoices(id),
    event_type TEXT NOT NULL,
    api_version TEXT NOT NULL,
    resource_version INT NOT NULL,
    causation_key TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    traceparent TEXT NOT NULL DEFAULT '',
    correlation_id TEXT NOT NULL DEFAULT '',
    CONSTRAINT uq_merchant_event_causation UNIQUE(merchant_id, causation_key),
    CONSTRAINT chk_merchant_event_resource_version CHECK (resource_version >= 1),
    CONSTRAINT chk_merchant_event_payload CHECK (jsonb_typeof(payload) = 'object')
);
CREATE INDEX idx_merchant_events_merchant_created
    ON sys_merchant_events(merchant_id, created_at DESC);
CREATE INDEX idx_merchant_events_invoice_created
    ON sys_merchant_events(invoice_id, created_at DESC) WHERE invoice_id IS NOT NULL;

CREATE TABLE sys_webhook_delivery_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    event_id TEXT NOT NULL REFERENCES sys_merchant_events(event_id),
    endpoint_id UUID REFERENCES cat_merchant_webhook_endpoints(id) ON DELETE SET NULL,
    merchant_id UUID NOT NULL REFERENCES cat_merchants(id),
    snapshot_url TEXT NOT NULL,
    secret_ref TEXT NOT NULL,
    secret_version INT NOT NULL,
    previous_secret_ref TEXT NOT NULL DEFAULT '',
    previous_secret_valid_until TIMESTAMPTZ,
    attempt_count INT NOT NULL DEFAULT 0,
    claim_token UUID,
    lease_until TIMESTAMPTZ,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','processing','succeeded','failed','cancelled')),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deadline_at TIMESTAMPTZ NOT NULL,
    last_error TEXT NOT NULL DEFAULT '',
    manual BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_webhook_delivery_job_attempt CHECK (attempt_count >= 0),
    CONSTRAINT chk_webhook_delivery_job_deadline CHECK (deadline_at >= created_at)
);
CREATE UNIQUE INDEX uq_webhook_delivery_job_automatic
    ON sys_webhook_delivery_jobs(event_id, endpoint_id) WHERE manual = FALSE;
CREATE UNIQUE INDEX uq_webhook_delivery_jobs_processing_merchant
    ON sys_webhook_delivery_jobs(merchant_id) WHERE status='processing';
CREATE INDEX idx_webhook_delivery_jobs_expired
    ON sys_webhook_delivery_jobs(lease_until) WHERE status='processing';
CREATE INDEX idx_webhook_delivery_jobs_last_attempt
    ON sys_webhook_delivery_jobs(merchant_id,updated_at DESC) WHERE attempt_count>0;
CREATE INDEX idx_webhook_delivery_jobs_due
    ON sys_webhook_delivery_jobs(next_attempt_at) WHERE status = 'pending';

CREATE TABLE sys_webhook_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    invoice_id UUID REFERENCES doc_crypto_invoices(id),
    merchant_id UUID NOT NULL REFERENCES cat_merchants(id),
    event_type TEXT NOT NULL,
    webhook_url TEXT NOT NULL,
    event_id TEXT NOT NULL,
    delivery_id TEXT NOT NULL UNIQUE,
    status_code INT,
    response_time_ms INT,
    attempt INT NOT NULL DEFAULT 1,
    error_message TEXT,
    error_class TEXT NOT NULL DEFAULT '',
    response_body TEXT NOT NULL DEFAULT '',
    next_retry_at TIMESTAMPTZ,
    request_body JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_webhook_delivery_attempt CHECK (attempt >= 1),
    CONSTRAINT chk_webhook_delivery_status CHECK (status_code IS NULL OR status_code BETWEEN 100 AND 599),
    CONSTRAINT chk_webhook_delivery_response_time CHECK (response_time_ms IS NULL OR response_time_ms >= 0)
);
CREATE INDEX idx_webhook_del_invoice
    ON sys_webhook_deliveries(invoice_id) WHERE invoice_id IS NOT NULL;
CREATE INDEX idx_webhook_del_merchant
    ON sys_webhook_deliveries(merchant_id, created_at DESC);
CREATE INDEX idx_webhook_del_created
    ON sys_webhook_deliveries(created_at DESC);
CREATE INDEX idx_webhook_del_event
    ON sys_webhook_deliveries(event_id, created_at DESC);
COMMENT ON TABLE sys_webhook_deliveries IS
    'Immutable audit trail: every webhook HTTP attempt is stored as a separate row';
COMMENT ON COLUMN sys_webhook_deliveries.invoice_id IS
    'NULL for endpoint test events';
COMMENT ON COLUMN sys_webhook_deliveries.event_id IS
    'Stable merchant event identifier sent in Metapus-Event-Id';
COMMENT ON COLUMN sys_webhook_deliveries.delivery_id IS
    'Unique HTTP attempt identifier sent in Metapus-Delivery-Id';

CREATE TABLE sys_webhook_secret_retirements (
    secret_ref TEXT PRIMARY KEY,
    retire_after TIMESTAMPTZ NOT NULL,
    claimed_until TIMESTAMPTZ,
    attempt_count INT NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_webhook_secret_retirement_ref CHECK (BTRIM(secret_ref) <> '')
);
CREATE INDEX idx_webhook_secret_retirements_due
    ON sys_webhook_secret_retirements(retire_after, claimed_until);

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
    traceparent TEXT NOT NULL DEFAULT '',
    correlation_id TEXT NOT NULL DEFAULT '',
    causation_id TEXT NOT NULL DEFAULT '',
    UNIQUE (network_id, payload_hash)
);
CREATE INDEX idx_crypto_chain_observations_due
    ON reg_crypto_chain_observations(next_attempt_at,id)
    WHERE status IN ('pending','processing');

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
    _deleted_at TIMESTAMPTZ,
    registration_basis JSONB,
    activation_basis JSONB
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
CREATE INDEX idx_crypto_transfer_verifications_identity
    ON reg_crypto_transfer_verifications(network_id,tx_hash,event_kind,event_index,confirmations DESC);

-- CRYPTO PAYMENT DOCUMENT
CREATE TABLE doc_crypto_payments (
    -- Base document fields
    id                UUID PRIMARY KEY,
    number            TEXT NOT NULL DEFAULT '',
    date              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    basis_type        TEXT NOT NULL DEFAULT '',
    basis_id          UUID,
    posted            BOOLEAN NOT NULL DEFAULT FALSE,
    posted_version    INT NOT NULL DEFAULT 0,
    deletion_mark     BOOLEAN NOT NULL DEFAULT FALSE,
    version           INT NOT NULL DEFAULT 1,
    attributes        JSONB DEFAULT '{}',
    description       TEXT NOT NULL DEFAULT '',
    created_by        UUID,
    updated_by        UUID,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- CryptoPayment-specific fields
    invoice_id        UUID NOT NULL,
    merchant_id       UUID NOT NULL,
    network_id        UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    token_id          UUID NOT NULL,
    wallet_id         UUID NOT NULL,
    tx_hash           TEXT NOT NULL,
    from_address      TEXT NOT NULL DEFAULT '',
    source_address    TEXT NOT NULL DEFAULT '',
    source_nonce      BIGINT,
    replacement_tx_hash TEXT NOT NULL DEFAULT '',
    amount            NUMERIC(78,0) NOT NULL DEFAULT 0,
    event_kind        TEXT NOT NULL DEFAULT 'native_value'
                      CHECK (event_kind IN ('native_value','contract_log','internal_value')),
    event_index       BIGINT NOT NULL DEFAULT 0 CHECK (event_index >= 0),
    block_number      BIGINT NOT NULL DEFAULT 0,
    block_hash        TEXT NOT NULL DEFAULT '',
    confirmations     INT NOT NULL DEFAULT 0,
    required_confs    INT NOT NULL DEFAULT 0,
    finality_status   TEXT NOT NULL DEFAULT 'provisional'
                      CHECK (finality_status IN ('provisional','solidified','finalized')),
    canonicality      TEXT NOT NULL DEFAULT 'canonical'
                      CHECK (canonicality IN ('canonical','orphaned','unknown')),
    status            TEXT NOT NULL DEFAULT 'detected'
                      CHECK (status IN ('detected','confirming','confirmed','settled','reorged','replaced','failed')),
    -- Fee snapshot (from reg_fee_schedule at payment creation time)
    -- Formula: clamp(fee_fixed + amount × fee_percent_bp / 10000, fee_min, fee_max)
    fee_fixed         NUMERIC(78,0) NOT NULL DEFAULT 0,
    fee_percent_bp    INT NOT NULL DEFAULT 0 CHECK (fee_percent_bp >= 0 AND fee_percent_bp <= 10000),
    fee_min           NUMERIC(78,0) NOT NULL DEFAULT 0,
    fee_max           NUMERIC(78,0) NOT NULL DEFAULT 0,
    detected_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    confirmed_at      TIMESTAMPTZ,
    confirmation_claimed_by       TEXT NOT NULL DEFAULT '',
    confirmation_claimed_at       TIMESTAMPTZ,
    confirmation_lease_expires_at TIMESTAMPTZ,

    -- CDC fields
    _txid             BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at       TIMESTAMPTZ,
    next_confirmation_check_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    confirmation_check_attempts INT NOT NULL DEFAULT 0
        CHECK (confirmation_check_attempts >= 0),
    last_confirmation_checked_at TIMESTAMPTZ,
    verification_id   UUID REFERENCES reg_crypto_transfer_verifications(id),
    receipt_id        UUID
);
-- One transaction can contain multiple transfer events. The chain event index
-- is part of the idempotency identity.
CREATE UNIQUE INDEX idx_doc_crypto_payments_chain_event
    ON doc_crypto_payments (network_id, tx_hash, event_kind, event_index)
    WHERE deletion_mark = FALSE;
CREATE INDEX idx_doc_crypto_payments_invoice ON doc_crypto_payments (invoice_id);
CREATE INDEX idx_doc_crypto_payments_merchant ON doc_crypto_payments (merchant_id);
CREATE INDEX idx_doc_crypto_payments_status ON doc_crypto_payments (status) WHERE status NOT IN ('settled','reorged');
CREATE INDEX idx_doc_crypto_payments_confirmation_claim
    ON doc_crypto_payments (status, confirmation_lease_expires_at, created_at)
    WHERE status IN ('detected','confirming') AND deletion_mark = FALSE;
CREATE INDEX idx_doc_crypto_payments_date ON doc_crypto_payments (date DESC);
CREATE INDEX idx_crypto_payments_confirmation_due
    ON doc_crypto_payments(next_confirmation_check_at, created_at, id)
    WHERE status IN ('detected','confirming') AND deletion_mark = FALSE;
CREATE INDEX idx_crypto_payments_receipt ON doc_crypto_payments(receipt_id) WHERE receipt_id IS NOT NULL;

-- PAYMENT EVENT LOG (FSM audit trail)
CREATE TABLE reg_crypto_payment_events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    payment_id    UUID NOT NULL REFERENCES doc_crypto_payments(id),
    from_status   TEXT NOT NULL,
    to_status     TEXT NOT NULL,
    event_type    TEXT NOT NULL,
    metadata      JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_crypto_payment_events_payment ON reg_crypto_payment_events (payment_id, created_at);

CREATE TABLE reg_crypto_wallet_fundings (
    id              UUID PRIMARY KEY,
    network_id      UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    wallet_id       UUID NOT NULL REFERENCES cat_wallets(id),
    token_id        UUID NOT NULL REFERENCES cat_tokens(id),
    tx_hash         TEXT NOT NULL,
    from_address    TEXT NOT NULL,
    to_address      TEXT NOT NULL,
    amount          NUMERIC(78,0) NOT NULL CHECK (amount > 0),
    block_number    BIGINT NOT NULL CHECK (block_number > 0),
    confirmations   INT NOT NULL CHECK (confirmations >= 1),
    block_timestamp TIMESTAMPTZ NOT NULL,
    recorded_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    verification_id UUID NOT NULL REFERENCES reg_crypto_transfer_verifications(id),
    occurrence_id   UUID NOT NULL UNIQUE,
    posting_id      UUID NOT NULL UNIQUE
);
CREATE INDEX idx_crypto_wallet_fundings_wallet
    ON reg_crypto_wallet_fundings (wallet_id, token_id, block_timestamp DESC);
CREATE INDEX idx_crypto_wallet_fundings_tx ON reg_crypto_wallet_fundings(network_id,tx_hash);

CREATE TABLE reg_crypto_security_incidents (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    network_id     UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    payment_id     UUID REFERENCES doc_crypto_payments(id),
    incident_kind  TEXT NOT NULL CHECK (incident_kind IN ('posted_payment_reorg','deep_reorg','nonce_divergence','invoice_projection_drift')),
    tx_hash        TEXT NOT NULL DEFAULT '',
    event_kind     TEXT NOT NULL DEFAULT 'native_value' CHECK (event_kind IN ('native_value','contract_log','internal_value')),
    event_index    BIGINT NOT NULL DEFAULT 0 CHECK (event_index >= 0),
    details        JSONB NOT NULL DEFAULT '{}'::jsonb,
    status         TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','acknowledged','resolved')),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    acknowledged_at TIMESTAMPTZ,
    resolved_at    TIMESTAMPTZ,
    invoice_id     UUID REFERENCES doc_crypto_invoices(id),
    UNIQUE (network_id,incident_kind,tx_hash,event_kind,event_index)
);

CREATE TABLE reg_crypto_inbound_chain_events (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    network_id       UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    tx_hash          TEXT NOT NULL,
    event_kind       TEXT NOT NULL CHECK (event_kind IN ('native_value','contract_log','internal_value')),
    event_index      BIGINT NOT NULL CHECK (event_index >= 0),
    from_address     TEXT NOT NULL DEFAULT '',
    to_address       TEXT NOT NULL,
    token_contract   TEXT NOT NULL DEFAULT '',
    token_id         UUID REFERENCES cat_tokens(id),
    wallet_id        UUID REFERENCES cat_wallets(id),
    amount           NUMERIC(78,0) NOT NULL DEFAULT 0,
    block_number     BIGINT NOT NULL DEFAULT 0,
    block_hash       TEXT NOT NULL DEFAULT '',
    block_timestamp  TIMESTAMPTZ NOT NULL,
    confirmations    INT NOT NULL DEFAULT 0,
    required_confs   INT NOT NULL DEFAULT 0,
    finality_status  TEXT NOT NULL DEFAULT 'provisional',
    canonicality     TEXT NOT NULL DEFAULT 'unknown',
    event_type       TEXT NOT NULL DEFAULT 'transfer',
    direction        TEXT NOT NULL DEFAULT 'incoming',
    payload          JSONB NOT NULL DEFAULT '{}'::jsonb,
    match_status     TEXT NOT NULL DEFAULT 'received'
                     CHECK (match_status IN (
                         'received','matched','unmatched','needs_review','ignored','poison'
                     )),
    match_reason     TEXT NOT NULL DEFAULT '',
    invoice_id       UUID REFERENCES doc_crypto_invoices(id),
    payment_id       UUID REFERENCES doc_crypto_payments(id),
    allocation_id    UUID REFERENCES reg_crypto_wallet_allocations(id),
    details          JSONB NOT NULL DEFAULT '{}'::jsonb,
    retry_count      INT NOT NULL DEFAULT 0,
    next_retry_at    TIMESTAMPTZ,
    version          INT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid            BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at      TIMESTAMPTZ,
    receipt_id       UUID UNIQUE,
    receipt_check_at TIMESTAMPTZ,
    receipt_attempts INT NOT NULL DEFAULT 0 CHECK(receipt_attempts>=0),
    receipt_error    TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX idx_crypto_inbound_chain_events_identity
    ON reg_crypto_inbound_chain_events (network_id, tx_hash, event_kind, event_index)
    WHERE _deleted_at IS NULL;
CREATE INDEX idx_crypto_inbound_chain_events_retry
    ON reg_crypto_inbound_chain_events (next_retry_at, created_at)
    WHERE match_status = 'received' AND next_retry_at IS NOT NULL;
CREATE INDEX idx_crypto_inbound_chain_events_single_review
    ON reg_crypto_inbound_chain_events (match_status, created_at, id)
    WHERE match_reason IN ('additional_single_payment','retired_deposit_address') AND _deleted_at IS NULL;
CREATE INDEX idx_crypto_inbound_chain_events_txid
    ON reg_crypto_inbound_chain_events (_txid)
    WHERE _deleted_at IS NULL;
CREATE INDEX idx_crypto_inbound_chain_events_replay_due
    ON reg_crypto_inbound_chain_events (match_reason, next_retry_at, created_at, id)
    WHERE match_status = 'unmatched' AND _deleted_at IS NULL;
CREATE INDEX idx_crypto_inbound_receipt_candidates ON reg_crypto_inbound_chain_events(receipt_check_at NULLS FIRST,created_at,id)
    WHERE receipt_id IS NULL AND match_status IN ('unmatched','needs_review') AND wallet_id IS NOT NULL AND token_id IS NOT NULL AND _deleted_at IS NULL;

CREATE TABLE sys_invoice_reconciliation_drift (
    invoice_id UUID PRIMARY KEY REFERENCES doc_crypto_invoices(id),
    consecutive_cycles INT NOT NULL DEFAULT 1 CHECK (consecutive_cycles >= 1),
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMPTZ,
    episode_id UUID NOT NULL,
    initial_details JSONB NOT NULL CHECK(jsonb_typeof(initial_details)='object'),
    incident_id UUID REFERENCES reg_crypto_security_incidents(id),
    CONSTRAINT chk_invoice_reconciliation_details CHECK (jsonb_typeof(details) = 'object')
);
CREATE UNIQUE INDEX uq_crypto_invoice_drift_episode ON sys_invoice_reconciliation_drift(episode_id);
CREATE UNIQUE INDEX uq_crypto_invoice_drift_incident ON sys_invoice_reconciliation_drift(incident_id) WHERE incident_id IS NOT NULL;

CREATE TABLE reg_crypto_reorg_compensations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    incident_id UUID NOT NULL REFERENCES reg_crypto_security_incidents(id),
    payment_id UUID NOT NULL REFERENCES doc_crypto_payments(id),
    posted_version INT NOT NULL CHECK (posted_version > 0),
    observed_block_number BIGINT NOT NULL,
    observed_block_hash TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    request_basis JSONB
 CHECK(request_basis IS NULL OR jsonb_typeof(request_basis)='object'),
    CONSTRAINT uq_crypto_reorg_compensation_generation UNIQUE (payment_id, posted_version)
);
CREATE INDEX idx_crypto_reorg_compensations_incident
    ON reg_crypto_reorg_compensations(incident_id, posted_version);

CREATE TABLE sys_chain_watcher_rewinds (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    network_id UUID NOT NULL,
    watcher_key TEXT NOT NULL,
    fencing_token BIGINT NOT NULL,
    old_block BIGINT NOT NULL,
    new_block BIGINT NOT NULL,
    evidence TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE doc_crypto_adjustments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    payment_id UUID REFERENCES doc_crypto_payments(id),
    posted_version INT CHECK (posted_version > 0),
    reason_code TEXT NOT NULL CHECK (length(btrim(reason_code)) >= 3),
    evidence JSONB NOT NULL CHECK (evidence <> '{}'::jsonb),
    idempotency_key TEXT NOT NULL UNIQUE,
    request_hash TEXT NOT NULL,
    maker_id UUID NOT NULL,
    checker_id UUID,
    status TEXT NOT NULL DEFAULT 'pending_approval'
        CHECK (status IN ('pending_approval','posted','rejected')),
    decision_reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decided_at TIMESTAMPTZ,
    mode TEXT NOT NULL DEFAULT 'payment_reversal'
        CHECK (mode IN ('payment_reversal','wallet_balance')),
    wallet_id UUID REFERENCES cat_wallets(id),
    token_id UUID REFERENCES cat_tokens(id),
    direction TEXT CHECK (direction IN ('receipt','expense')),
    amount NUMERIC(78,0) CHECK (amount > 0),
    decision_mode TEXT NOT NULL DEFAULT 'two_person'
        CHECK (decision_mode IN ('two_person','direct')),
    basis_kind TEXT NOT NULL,
    proposal_txid BIGINT,
    CONSTRAINT chk_crypto_adjustment_mode_basis CHECK (
        (mode='payment_reversal' AND payment_id IS NOT NULL AND posted_version>0
         AND wallet_id IS NULL AND token_id IS NULL AND direction IS NULL AND amount IS NULL)
        OR (mode='wallet_balance' AND payment_id IS NULL AND posted_version IS NULL
            AND wallet_id IS NOT NULL AND token_id IS NOT NULL
            AND direction IS NOT NULL AND amount>0)),
    CONSTRAINT chk_crypto_adjustment_decision CHECK (
        (status='pending_approval' AND checker_id IS NULL AND decided_at IS NULL)
        OR (status IN ('posted','rejected') AND checker_id IS NOT NULL
            AND decided_at IS NOT NULL AND length(btrim(decision_reason))>=3
            AND (checker_id<>maker_id OR (status='posted' AND decision_mode='direct')))),
    CONSTRAINT ck_crypto_adjustment_accounting_basis CHECK (
        proposal_txid IS NOT NULL AND (
            (mode='wallet_balance' AND basis_kind='wallet_variance')
            OR (mode='payment_reversal' AND basis_kind='payment_attribution')))
);

CREATE TABLE reg_crypto_adjustment_sources (
    adjustment_id UUID NOT NULL REFERENCES doc_crypto_adjustments(id),
    source_register TEXT NOT NULL
        CHECK(source_register IN ('suspense','merchant','fee')),
    source_line_id UUID NOT NULL,
    amount NUMERIC(78,0) CHECK (amount>0),
    PRIMARY KEY (adjustment_id,source_register,source_line_id)
);

-- A wallet correction has a separate gain/loss account. It never mutates
-- merchant liabilities or platform fee revenue.
CREATE TABLE reg_crypto_wallet_variance_movements (
    line_id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    adjustment_id UUID NOT NULL UNIQUE REFERENCES doc_crypto_adjustments(id),
    wallet_id UUID NOT NULL REFERENCES cat_wallets(id),
    token_id UUID NOT NULL REFERENCES cat_tokens(id),
    variance_type TEXT NOT NULL CHECK (variance_type IN ('surplus','shortage')),
    amount NUMERIC(78,0) NOT NULL CHECK (amount>0),
    period TIMESTAMPTZ NOT NULL DEFAULT NOW()
);


-- ---- Triggers ----
CREATE TRIGGER trg_cat_blockchain_networks_txid
    BEFORE UPDATE ON cat_blockchain_networks
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_cat_blockchain_networks_soft_delete
    BEFORE UPDATE OF deletion_mark ON cat_blockchain_networks
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

CREATE TRIGGER trg_cat_blockchain_networks_updated_at
    BEFORE UPDATE ON cat_blockchain_networks
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_cat_tokens_txid
    BEFORE UPDATE ON cat_tokens
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_cat_tokens_soft_delete
    BEFORE UPDATE OF deletion_mark ON cat_tokens
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

CREATE TRIGGER trg_cat_tokens_updated_at
    BEFORE UPDATE ON cat_tokens
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_rpc_endpoint_txid BEFORE INSERT OR UPDATE ON sys_crypto_rpc_endpoints FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_rpc_endpoint_modtime BEFORE UPDATE ON sys_crypto_rpc_endpoints FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_rpc_endpoint_delete BEFORE UPDATE OF deletion_mark ON sys_crypto_rpc_endpoints FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

CREATE TRIGGER trg_doc_crypto_invoices_txid
    BEFORE UPDATE ON doc_crypto_invoices
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_doc_crypto_invoices_soft_delete
    BEFORE UPDATE OF deletion_mark ON doc_crypto_invoices
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

CREATE TRIGGER trg_crypto_balance_movements_balance
    AFTER INSERT OR DELETE ON reg_crypto_balance_movements
    FOR EACH ROW
    EXECUTE FUNCTION update_crypto_balance();

CREATE TRIGGER trg_cat_rate_sources_txid
    BEFORE UPDATE ON cat_rate_sources
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_cat_rate_sources_soft_delete
    BEFORE UPDATE OF deletion_mark ON cat_rate_sources
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

CREATE TRIGGER trg_cat_rate_sources_updated_at
    BEFORE UPDATE ON cat_rate_sources
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_rate_source_mappings_txid BEFORE UPDATE ON reg_rate_source_mappings FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_rate_feed_state_txid BEFORE UPDATE ON sys_rate_feed_state FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_exchange_rate_immutable BEFORE UPDATE OR DELETE ON reg_exchange_rates FOR EACH ROW EXECUTE FUNCTION guard_exchange_rate_history();

CREATE TRIGGER trg_rate_source_type BEFORE UPDATE OF source_type ON cat_rate_sources FOR EACH ROW EXECUTE FUNCTION guard_rate_source_type();

CREATE TRIGGER trg_crypto_merchant_balance_movements_balance
    AFTER INSERT OR DELETE ON reg_crypto_merchant_balance_movements
    FOR EACH ROW
    EXECUTE FUNCTION update_crypto_merchant_balance();

CREATE TRIGGER trg_cat_merchants_cdc
    BEFORE UPDATE ON cat_merchants
    FOR EACH ROW EXECUTE FUNCTION fn_cat_merchants_cdc();

CREATE TRIGGER trg_cat_merchants_updated_at
    BEFORE UPDATE ON cat_merchants
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_deposit_address_retirement_guard
    BEFORE UPDATE OR DELETE ON cat_wallets
    FOR EACH ROW EXECUTE FUNCTION fn_deposit_address_retirement_guard();

CREATE TRIGGER trg_deposit_rotation_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_deposit_address_rotations
    FOR EACH ROW EXECUTE FUNCTION fn_deposit_address_retirement_guard();

CREATE TRIGGER trg_deposit_rotation_identity
    BEFORE INSERT ON reg_crypto_deposit_address_rotations
    FOR EACH ROW EXECUTE FUNCTION fn_deposit_rotation_identity_guard();

CREATE TRIGGER trg_cat_wallets_cdc
    BEFORE INSERT OR UPDATE ON cat_wallets
    FOR EACH ROW EXECUTE FUNCTION fn_cat_wallets_cdc();

CREATE TRIGGER trg_cat_wallets_updated_at
    BEFORE UPDATE ON cat_wallets
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_merchant_api_keys_updated_at
    BEFORE UPDATE ON cat_merchant_api_keys
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_merchant_network_policy_guard
    BEFORE UPDATE OR DELETE ON auth_merchant_network_policies
    FOR EACH ROW EXECUTE FUNCTION fn_merchant_network_policy_guard();

CREATE TRIGGER trg_merchant_allowed_network_guard
    BEFORE UPDATE OR DELETE ON auth_merchant_allowed_networks
    FOR EACH ROW EXECUTE FUNCTION fn_merchant_network_policy_guard();

CREATE TRIGGER trg_merchant_network_policy_audit_immutable
    BEFORE UPDATE OR DELETE ON auth_merchant_network_policy_audit
    FOR EACH ROW EXECUTE FUNCTION fn_merchant_network_policy_guard();

CREATE CONSTRAINT TRIGGER trg_merchant_network_policy_has_cidr
    AFTER INSERT OR UPDATE ON auth_merchant_network_policies
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_merchant_network_policy_enforce_has_cidr();

CREATE CONSTRAINT TRIGGER trg_merchant_allowed_network_has_cidr
    AFTER INSERT OR UPDATE ON auth_merchant_allowed_networks
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_merchant_network_policy_enforce_has_cidr();

CREATE TRIGGER trg_doc_crypto_payments_cdc
    BEFORE UPDATE ON doc_crypto_payments
    FOR EACH ROW EXECUTE FUNCTION fn_doc_crypto_payments_cdc();

CREATE TRIGGER trg_crypto_fee_balance
    AFTER INSERT OR DELETE ON reg_crypto_fee_movements
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_fee_balance_update();

CREATE TRIGGER trg_doc_payment_links_txid
    BEFORE UPDATE ON doc_payment_links
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_doc_payment_links_soft_delete
    BEFORE UPDATE OF deletion_mark ON doc_payment_links
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

CREATE TRIGGER trg_crypto_wallet_fundings_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_wallet_fundings
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_wallet_fundings_immutable();

CREATE TRIGGER trg_blockchain_network_init_wallet_pool
    AFTER INSERT ON cat_blockchain_networks
    FOR EACH ROW EXECUTE FUNCTION initialize_crypto_wallet_pool_policy();

CREATE TRIGGER trg_cat_wallets_publish_derivation
    AFTER INSERT OR UPDATE OF network_id, derivation_path ON cat_wallets
    FOR EACH ROW EXECUTE FUNCTION publish_crypto_wallet_derivation();

CREATE TRIGGER trg_wallet_pool_policies_updated_at
    BEFORE UPDATE ON sys_crypto_wallet_pool_policies
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_wallet_pool_policies_txid
    BEFORE UPDATE ON sys_crypto_wallet_pool_policies
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_merchant_wallet_allocation_overrides_updated_at
    BEFORE UPDATE ON reg_merchant_wallet_allocation_overrides
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_merchant_wallet_allocation_overrides_txid
    BEFORE UPDATE ON reg_merchant_wallet_allocation_overrides
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_merchant_wallet_lease_rate_windows_updated_at
    BEFORE UPDATE ON reg_merchant_wallet_lease_rate_windows
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_merchant_wallet_lease_rate_windows_txid
    BEFORE UPDATE ON reg_merchant_wallet_lease_rate_windows
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_wallet_derivations_updated_at
    BEFORE UPDATE ON sys_crypto_wallet_derivations
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_wallet_derivations_txid
    BEFORE UPDATE ON sys_crypto_wallet_derivations
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_wallet_derivation_cursors_updated_at
    BEFORE UPDATE ON sys_crypto_wallet_derivation_cursors
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_wallet_derivation_cursors_txid
    BEFORE UPDATE ON sys_crypto_wallet_derivation_cursors
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_wallet_provisioning_jobs_updated_at
    BEFORE UPDATE ON sys_crypto_wallet_provisioning_jobs
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_wallet_provisioning_jobs_txid
    BEFORE UPDATE ON sys_crypto_wallet_provisioning_jobs
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_blockchain_network_init_crypto_policy
    AFTER INSERT ON cat_blockchain_networks
    FOR EACH ROW EXECUTE FUNCTION initialize_crypto_network_policy();

CREATE TRIGGER trg_crypto_network_policies_updated_at
    BEFORE UPDATE ON sys_crypto_network_policies
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_crypto_network_policies_txid
    BEFORE UPDATE ON sys_crypto_network_policies
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_crypto_fee_reservations_updated_at
    BEFORE UPDATE ON reg_crypto_fee_reservations
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_crypto_fee_reservation_events_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_fee_reservation_events
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_fee_reservation_events_immutable();

CREATE TRIGGER trg_crypto_chain_settlements_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_chain_settlements
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_chain_settlement_evidence_immutable();

CREATE TRIGGER trg_crypto_chain_settlement_components_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_chain_settlement_components
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_chain_settlement_evidence_immutable();

CREATE TRIGGER trg_evm_nonce_lease_events_immutable
    BEFORE UPDATE OR DELETE ON reg_evm_nonce_lease_events
    FOR EACH ROW EXECUTE FUNCTION fn_evm_nonce_lease_events_immutable();

CREATE TRIGGER trg_crypto_wallet_allocations_updated_at
    BEFORE UPDATE ON reg_crypto_wallet_allocations
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_crypto_wallet_allocations_txid
    BEFORE UPDATE ON reg_crypto_wallet_allocations
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_crypto_inbound_chain_events_updated_at
    BEFORE UPDATE ON reg_crypto_inbound_chain_events
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_crypto_inbound_chain_events_txid
    BEFORE UPDATE ON reg_crypto_inbound_chain_events
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_merchant_webhook_endpoints_updated_at
    BEFORE UPDATE ON cat_merchant_webhook_endpoints
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_merchant_webhook_endpoints_txid
    BEFORE UPDATE ON cat_merchant_webhook_endpoints
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_merchant_webhook_endpoints_soft_delete
    BEFORE UPDATE OF deletion_mark ON cat_merchant_webhook_endpoints
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

CREATE TRIGGER trg_crypto_token_identity_guard
    BEFORE UPDATE ON cat_tokens
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_token_identity_guard();

CREATE TRIGGER trg_crypto_chain_observation_evidence_guard
    BEFORE UPDATE ON reg_crypto_chain_observations
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_chain_observation_evidence_guard();

CREATE TRIGGER trg_crypto_transfer_verification_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_transfer_verifications
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_transfer_verification_immutable();

CREATE TRIGGER trg_crypto_register_watch_address
    AFTER INSERT OR UPDATE OF address,network_id,vault_key_id ON cat_wallets
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_register_watch_address();

CREATE TRIGGER trg_crypto_register_watch_asset
    AFTER INSERT OR UPDATE OF network_id,contract_address,is_active,deletion_mark ON cat_tokens
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_register_watch_asset();

CREATE TRIGGER trg_crypto_block_ingestion_txid BEFORE UPDATE ON sys_crypto_block_ingestion
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_crypto_adjustment_guard
    BEFORE UPDATE ON doc_crypto_adjustments
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_adjustment_guard();

CREATE TRIGGER trg_crypto_adjustment_source_insert_guard
    BEFORE INSERT ON reg_crypto_adjustment_sources
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_adjustment_source_insert_guard();

CREATE TRIGGER trg_crypto_adjustment_sources_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_adjustment_sources
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_financial_movement_immutable();

CREATE TRIGGER trg_crypto_balance_reversal_cap BEFORE INSERT ON reg_crypto_balance_movements
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_payment_reversal_cap();

CREATE TRIGGER trg_crypto_merchant_reversal_cap BEFORE INSERT ON reg_crypto_merchant_balance_movements
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_payment_reversal_cap();

CREATE TRIGGER trg_crypto_fee_reversal_cap BEFORE INSERT ON reg_crypto_fee_movements
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_payment_reversal_cap();

CREATE TRIGGER trg_crypto_wallet_variance_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_wallet_variance_movements
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_financial_movement_immutable();


-- Every policy starts fail-closed. A deployment bootstrap or an MFA-protected
-- policy update explicitly enables the intended network.
INSERT INTO sys_crypto_wallet_pool_policies (
    network_id, enabled, low_watermark, target_ready, max_total_wallets,
    max_provision_batch, retry_after_seconds
)
SELECT id, FALSE, 0, 1, 1, 1, 30
FROM cat_blockchain_networks
ON CONFLICT (network_id) DO NOTHING;

INSERT INTO sys_crypto_network_policies (network_id)
SELECT id FROM cat_blockchain_networks
ON CONFLICT (network_id) DO NOTHING;

INSERT INTO cat_blockchain_networks (
    id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals,
    confirmations_needed,block_time_seconds,explorer_url,finality_mode,color,is_active
) VALUES (
    '11155111-0000-4000-8000-000000000001','ETHEREUM-SEPOLIA','Ethereum Sepolia','evm','11155111','ETH',18,
    12,12,'https://sepolia.etherscan.io/tx/{txHash}','confirmations_or_finalized','blue',FALSE
) ON CONFLICT (code) WHERE deletion_mark=FALSE DO NOTHING;

INSERT INTO cat_tokens (
    id,code,name,network_id,contract_address,symbol,decimal_places,token_standard,is_active
) VALUES
    ('11155111-0000-4000-8000-000000000002','ETH-SEPOLIA','Ethereum Sepolia ETH',
     '11155111-0000-4000-8000-000000000001','', 'ETH',18,'native',TRUE),
    ('11155111-0000-4000-8000-000000000003','PYUSD-SEPOLIA','PayPal USD',
     '11155111-0000-4000-8000-000000000001','0xCaC524BcA292aaade2DF8A05cC58F0a65B1B3bB9','PYUSD',6,'ERC-20',TRUE)
ON CONFLICT (code) WHERE deletion_mark=FALSE DO NOTHING;

-- Old reorg movements used the incident ID as recorder ID. Preserve that identity
-- so a retry after rollout cannot compensate the same posting twice.
INSERT INTO reg_crypto_reorg_compensations
    (id, incident_id, payment_id, posted_version, observed_block_number, observed_block_hash)
SELECT id, id, payment_id, (details->>'postedVersion')::INT,
       COALESCE((details->>'blockNumber')::BIGINT, 0),
       COALESCE(details->>'observedBlockHash', '')
FROM reg_crypto_security_incidents
WHERE incident_kind = 'posted_payment_reorg'
  AND payment_id IS NOT NULL
  AND details->>'postedVersion' ~ '^[1-9][0-9]*$';

-- Integration providers of this configuration (cryptointegrations.Catalog):
-- the platform's and its own. dRPC is the only provider with a manual key period.
ALTER TABLE cat_integration_accounts ADD CONSTRAINT ck_integration_accounts_provider CHECK (provider IN ('alchemy','coingecko_demo','tron_api','chainstack','getblock','infura','drpc','telegram','smtp','resource_api','getblock_net','testnet_aml'));
ALTER TABLE cat_integration_accounts ADD CONSTRAINT ck_integration_accounts_drpc_manual_period CHECK ((provider='drpc') = (credentials_expires_at IS NOT NULL));
ALTER TABLE cat_integration_pools ADD CONSTRAINT ck_integration_pools_provider CHECK (provider IN ('alchemy','coingecko_demo','tron_api','chainstack','getblock','infura','drpc','telegram','smtp','resource_api','getblock_net','testnet_aml'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE cat_integration_pools DROP CONSTRAINT IF EXISTS ck_integration_pools_provider;
ALTER TABLE cat_integration_accounts
    DROP CONSTRAINT IF EXISTS ck_integration_accounts_drpc_manual_period,
    DROP CONSTRAINT IF EXISTS ck_integration_accounts_provider;
DROP TABLE reg_crypto_wallet_variance_movements;
DROP TABLE reg_crypto_adjustment_sources;
DROP TABLE doc_crypto_adjustments;
DROP TABLE sys_chain_watcher_rewinds;
DROP TABLE reg_crypto_reorg_compensations;
DROP TABLE sys_invoice_reconciliation_drift;
DROP TABLE reg_crypto_inbound_chain_events;
DROP TABLE reg_crypto_security_incidents;
DROP TABLE reg_crypto_wallet_fundings;
DROP TABLE reg_crypto_payment_events;
DROP TABLE doc_crypto_payments;
DROP TABLE reg_crypto_transfer_verifications;
DROP TABLE sys_crypto_watch_targets;
DROP TABLE sys_crypto_block_ingestion;
DROP TABLE reg_crypto_chain_observations;
DROP TABLE sys_webhook_secret_retirements;
DROP TABLE sys_webhook_deliveries;
DROP TABLE sys_webhook_delivery_jobs;
DROP TABLE sys_merchant_events;
DROP TABLE cat_merchant_webhook_endpoints;
DROP TABLE reg_crypto_wallet_allocations;
DROP TABLE sys_chain_watcher_block_history;
DROP TABLE reg_crypto_network_holds;
DROP TABLE reg_evm_nonce_lease_events;
DROP TABLE reg_evm_nonce_leases;
DROP TABLE reg_evm_nonce_cursors;
DROP TABLE reg_crypto_chain_settlement_components;
DROP TABLE reg_crypto_chain_settlements;
DROP TABLE reg_crypto_fee_reservation_events;
DROP TABLE reg_crypto_fee_reservations;
DROP TABLE sys_crypto_network_runtime_status;
DROP TABLE sys_crypto_external_resource_policies;
DROP TABLE sys_crypto_network_policies;
DROP TABLE sys_crypto_wallet_provisioning_jobs;
DROP TABLE sys_crypto_wallet_derivation_cursors;
DROP TABLE sys_crypto_wallet_derivations;
DROP TABLE reg_merchant_wallet_lease_rate_windows;
DROP TABLE reg_merchant_wallet_allocation_overrides;
DROP TABLE sys_crypto_wallet_pool_policies;
DROP TABLE doc_payment_links;
DROP TABLE sys_chain_watcher_state;
DROP TABLE reg_fee_schedule;
DROP TABLE reg_crypto_fee_balances;
DROP TABLE reg_crypto_fee_movements;
DROP TABLE auth_merchant_network_policy_audit;
DROP TABLE auth_merchant_allowed_networks;
DROP TABLE auth_merchant_network_policies;
DROP TABLE cat_merchant_api_keys;
DROP TABLE reg_merchant_token_config;
DROP TABLE reg_crypto_deposit_address_rotations;
DROP TABLE cat_wallets;
DROP TABLE sys_merchant_users;
DROP TABLE cat_merchants;
DROP TABLE reg_crypto_merchant_balance_balances;
DROP TABLE reg_crypto_merchant_balance_movements;
DROP TABLE sys_rate_feed_state;
DROP TABLE reg_exchange_rates;
DROP TABLE reg_rate_source_mappings;
DROP TABLE cat_rate_sources;
DROP TABLE reg_crypto_balance_balances;
DROP TABLE reg_crypto_balance_movements;
DROP TABLE doc_crypto_invoices;
DROP TABLE sys_crypto_rpc_endpoints;
DROP TABLE sys_crypto_rpc_network_settings;
DROP TABLE cat_tokens;
DROP TABLE cat_blockchain_networks;
DROP FUNCTION fn_crypto_payment_reversal_cap();
DROP FUNCTION fn_crypto_adjustment_source_insert_guard();
DROP FUNCTION fn_crypto_adjustment_guard();
DROP FUNCTION fn_crypto_register_watch_asset();
DROP FUNCTION fn_crypto_register_watch_address();
DROP FUNCTION fn_crypto_transfer_verification_immutable();
DROP FUNCTION fn_crypto_chain_observation_evidence_guard();
DROP FUNCTION fn_crypto_financial_movement_immutable();
DROP FUNCTION fn_crypto_mark_token_identity_used();
DROP FUNCTION fn_crypto_token_identity_guard();
DROP FUNCTION fn_crypto_hold_owner_guard();
DROP FUNCTION fn_evm_nonce_lease_events_immutable();
DROP FUNCTION fn_crypto_chain_settlement_evidence_immutable();
DROP FUNCTION fn_crypto_fee_reservation_events_immutable();
DROP FUNCTION initialize_crypto_network_policy();
DROP FUNCTION publish_crypto_wallet_derivation();
DROP FUNCTION initialize_crypto_wallet_pool_policy();
DROP FUNCTION fn_crypto_wallet_fundings_immutable();
DROP FUNCTION recalculate_crypto_fee();
DROP FUNCTION fn_crypto_fee_balance_update();
DROP FUNCTION fn_doc_crypto_payments_cdc();
DROP FUNCTION fn_merchant_network_policy_enforce_has_cidr();
DROP FUNCTION fn_merchant_network_policy_guard();
DROP FUNCTION fn_cat_wallets_cdc();
DROP FUNCTION fn_deposit_rotation_identity_guard();
DROP FUNCTION fn_deposit_address_retirement_guard();
DROP FUNCTION fn_cat_merchants_cdc();
DROP FUNCTION recalculate_crypto_merchant_balance();
DROP FUNCTION update_crypto_merchant_balance();
DROP FUNCTION guard_rate_source_type();
DROP FUNCTION guard_exchange_rate_history();
DROP FUNCTION recalculate_crypto_balance();
DROP FUNCTION update_crypto_balance();
-- +goose StatementEnd
