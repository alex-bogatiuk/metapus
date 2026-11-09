-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- This first admission contract is deliberately generation 1 only. Further
-- canonical generations require the explicit compensation workflow, not a new
-- recorder or a changed block hash.
CREATE TABLE reg_crypto_custody_effect_postings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    effect_id UUID NOT NULL REFERENCES reg_crypto_accounting_effects(id),
    occurrence_id UUID NOT NULL REFERENCES reg_crypto_accounting_effect_occurrences(id),
    verification_id UUID NOT NULL REFERENCES reg_crypto_transfer_verifications(id),
    generation INT NOT NULL DEFAULT 1 CHECK(generation=1),
    recorder_id UUID NOT NULL,
    recorder_type TEXT NOT NULL CHECK(length(btrim(recorder_type)) BETWEEN 1 AND 50),
    recorder_version INT NOT NULL CHECK(recorder_version>0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(effect_id,generation)
);
CREATE TABLE reg_crypto_custody_effect_recognitions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    posting_id UUID NOT NULL REFERENCES reg_crypto_custody_effect_postings(id),
    wallet_id UUID NOT NULL REFERENCES cat_wallets(id),
    token_id UUID NOT NULL REFERENCES cat_tokens(id),
    record_type TEXT NOT NULL CHECK(record_type IN ('receipt','expense')),
    movement_line_id UUID NOT NULL UNIQUE REFERENCES reg_crypto_balance_movements(line_id)
        DEFERRABLE INITIALLY DEFERRED,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(posting_id,wallet_id,token_id,record_type),
    UNIQUE(posting_id,record_type)
);

CREATE TABLE doc_crypto_custody_receipts (
    id UUID PRIMARY KEY,
    deletion_mark BOOLEAN NOT NULL DEFAULT FALSE CHECK(NOT deletion_mark),
    version INT NOT NULL DEFAULT 1 CHECK(version>0),
    attributes JSONB NOT NULL DEFAULT '{}',
    _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID,
    updated_by UUID,
    number TEXT NOT NULL UNIQUE CHECK(btrim(number)<>''),
    date TIMESTAMPTZ NOT NULL,
    posted BOOLEAN NOT NULL DEFAULT FALSE,
    posted_version INT NOT NULL DEFAULT 0,
    basis_type TEXT NOT NULL CHECK(basis_type='CryptoTransferVerification'),
    basis_id UUID NOT NULL REFERENCES reg_crypto_transfer_verifications(id),
    description TEXT NOT NULL DEFAULT '',
    network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    wallet_id UUID NOT NULL REFERENCES cat_wallets(id),
    token_id UUID NOT NULL REFERENCES cat_tokens(id),
    custody_merchant_id UUID REFERENCES cat_merchants(id),
    verification_id UUID NOT NULL REFERENCES reg_crypto_transfer_verifications(id),
    occurrence_id UUID NOT NULL UNIQUE REFERENCES reg_crypto_accounting_effect_occurrences(id),
    posting_id UUID NOT NULL UNIQUE REFERENCES reg_crypto_custody_effect_postings(id),
    amount NUMERIC(78,0) NOT NULL CHECK(amount>0),
    custody_line_id UUID NOT NULL UNIQUE,
    suspense_line_id UUID NOT NULL UNIQUE,
    reason_code TEXT NOT NULL CHECK(reason_code IN (
        'no_allocation','wrong_token','timestamp_before_lease','retired_deposit_address',
        'additional_single_payment','dust','treasury_unidentified','matched_receipt')),
    CHECK(basis_id=verification_id),
    CHECK((NOT posted AND posted_version=0) OR (posted AND posted_version=1))
);
CREATE INDEX idx_crypto_custody_receipts_wallet ON doc_crypto_custody_receipts(wallet_id,token_id,date,id);
CREATE INDEX idx_crypto_custody_receipts_owner ON doc_crypto_custody_receipts(custody_merchant_id,date,id);
CREATE INDEX idx_crypto_custody_receipts_txid ON doc_crypto_custody_receipts(_txid);
CREATE TRIGGER trg_crypto_custody_receipt_txid BEFORE UPDATE ON doc_crypto_custody_receipts
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TABLE reg_crypto_receipt_suspense_movements (
    line_id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    recorder_id UUID NOT NULL REFERENCES doc_crypto_custody_receipts(id),
    recorder_type TEXT NOT NULL CHECK(recorder_type='CryptoCustodyReceipt'),
    recorder_version INT NOT NULL CHECK(recorder_version=1),
    period TIMESTAMPTZ NOT NULL,
    record_type TEXT NOT NULL CHECK(record_type='receipt'),
    receipt_id UUID NOT NULL REFERENCES doc_crypto_custody_receipts(id),
    token_id UUID NOT NULL REFERENCES cat_tokens(id),
    amount NUMERIC(78,0) NOT NULL CHECK(amount>0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(receipt_id)
);
CREATE TABLE reg_crypto_receipt_suspense_balances (
    receipt_id UUID NOT NULL REFERENCES doc_crypto_custody_receipts(id),
    token_id UUID NOT NULL REFERENCES cat_tokens(id),
    amount NUMERIC(78,0) NOT NULL CHECK(amount>=0),
    last_movement_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY(receipt_id,token_id)
);
CREATE TABLE reg_crypto_receipt_intake_audit (
    receipt_id UUID PRIMARY KEY REFERENCES doc_crypto_custody_receipts(id),
    verification_id UUID NOT NULL REFERENCES reg_crypto_transfer_verifications(id),
    posting_id UUID NOT NULL REFERENCES reg_crypto_custody_effect_postings(id),
    actor TEXT NOT NULL CHECK(btrim(actor)<>''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE FUNCTION fn_crypto_custody_receipt_mutation_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'custody receipt history is immutable' USING ERRCODE='23514';
    END IF;
    IF OLD.posted OR NOT NEW.posted OR NEW.posted_version<>1
       OR NEW.version<>OLD.version+1
       OR (to_jsonb(NEW)-ARRAY['posted','posted_version','version','updated_at','updated_by','_txid'])
          IS DISTINCT FROM
          (to_jsonb(OLD)-ARRAY['posted','posted_version','version','updated_at','updated_by','_txid']) THEN
        RAISE EXCEPTION 'custody receipt only permits its first posting' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_crypto_custody_receipt_mutation BEFORE UPDATE OR DELETE ON doc_crypto_custody_receipts
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_receipt_mutation_guard();

CREATE FUNCTION fn_crypto_receipt_suspense_apply() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM doc_crypto_custody_receipts d WHERE d.id=NEW.receipt_id
        AND d.id=NEW.recorder_id AND d.token_id=NEW.token_id AND d.amount=NEW.amount
        AND d.suspense_line_id=NEW.line_id AND d.date=NEW.period) THEN
        RAISE EXCEPTION 'suspense movement requires its exact receipt basis' USING ERRCODE='23514';
    END IF;
    INSERT INTO reg_crypto_receipt_suspense_balances(receipt_id,token_id,amount,last_movement_at)
    VALUES(NEW.receipt_id,NEW.token_id,NEW.amount,NEW.period);
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_crypto_receipt_suspense_apply AFTER INSERT ON reg_crypto_receipt_suspense_movements
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_receipt_suspense_apply();

DO $$
DECLARE target TEXT;
BEGIN
    FOREACH target IN ARRAY ARRAY['reg_crypto_custody_effect_postings',
        'reg_crypto_custody_effect_recognitions','reg_crypto_receipt_suspense_movements',
        'reg_crypto_receipt_intake_audit'] LOOP
        EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION fn_crypto_financial_movement_immutable()',
            'trg_immutable_'||target,target);
    END LOOP;
END;
$$;

CREATE FUNCTION fn_crypto_custody_posting_identity_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    PERFORM id FROM reg_crypto_accounting_effects WHERE id=NEW.effect_id FOR UPDATE;
    IF NOT EXISTS(SELECT 1 FROM reg_crypto_accounting_effect_occurrences o
        JOIN reg_crypto_accounting_effect_evidence e ON e.occurrence_id=o.id
        WHERE o.id=NEW.occurrence_id AND o.effect_id=NEW.effect_id
          AND e.verification_id=NEW.verification_id) THEN
        RAISE EXCEPTION 'custody posting requires exact indexed evidence' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_crypto_custody_posting_identity BEFORE INSERT ON reg_crypto_custody_effect_postings
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_posting_identity_guard();

CREATE FUNCTION fn_crypto_assert_custody_posting(target UUID) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE p reg_crypto_custody_effect_postings;
BEGIN
    SELECT * INTO STRICT p FROM reg_crypto_custody_effect_postings WHERE id=target;
    IF NOT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_recognitions r WHERE r.posting_id=p.id)
       OR EXISTS(
        SELECT 1 FROM reg_crypto_custody_effect_recognitions r
        JOIN reg_crypto_accounting_effects e ON e.id=p.effect_id
        JOIN reg_crypto_accounting_effect_occurrences o ON o.id=p.occurrence_id
        JOIN reg_crypto_transfer_verifications v ON v.id=p.verification_id
        JOIN cat_blockchain_networks n ON n.id=e.network_id
        JOIN cat_wallets w ON w.id=r.wallet_id
        JOIN cat_tokens t ON t.id=r.token_id
        LEFT JOIN reg_crypto_balance_movements m ON m.line_id=r.movement_line_id
        WHERE r.posting_id=p.id AND (
            m.line_id IS NULL OR m.recorder_id<>p.recorder_id OR m.recorder_type<>p.recorder_type
            OR m.recorder_version<>p.recorder_version OR m.record_type<>r.record_type
            OR m.wallet_id<>r.wallet_id OR m.token_id<>r.token_id OR m.amount<>o.amount
            OR m.reversal_of_line_id IS NOT NULL
            OR w.network_id<>e.network_id OR t.network_id<>e.network_id OR w.vault_key_id=''
            OR w.address_key IS DISTINCT FROM CASE WHEN r.record_type='receipt' THEN o.destination_address ELSE o.source_address END
            OR (CASE WHEN e.family='evm' THEN lower(COALESCE(t.contract_address,'')) ELSE COALESCE(t.contract_address,'') END)<>o.contract_address
            OR v.confirmations<n.confirmations_needed
            OR v.evidence->'event'->>'canonicality' IS DISTINCT FROM 'canonical'
            OR v.evidence->>'executionStatus' IS DISTINCT FROM 'succeeded'
            OR EXISTS(SELECT 1 FROM reg_crypto_transfer_verifications invalid
                WHERE invalid.network_id=v.network_id
                  AND regexp_replace(lower(invalid.tx_hash),'^0x','')=e.tx_hash
                  AND invalid.event_kind=e.event_kind AND invalid.event_index=v.event_index
                  AND invalid.verified_at>=v.verified_at
                  AND invalid.evidence->'event'->>'canonicality'='orphaned')
            OR EXISTS(SELECT 1 FROM reg_crypto_accounting_effect_evidence newer
                JOIN reg_crypto_accounting_effect_occurrences other ON other.id=newer.occurrence_id
                JOIN reg_crypto_transfer_verifications nv ON nv.id=newer.verification_id
                WHERE other.effect_id=e.id AND other.id<>p.occurrence_id AND nv.verified_at>=v.verified_at)
        )) THEN
        RAISE EXCEPTION 'custody recognition requires exact physical movements' USING ERRCODE='23514';
    END IF;
END;
$$;
CREATE FUNCTION fn_crypto_custody_posting_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME='reg_crypto_custody_effect_postings' THEN
        PERFORM fn_crypto_assert_custody_posting(NEW.id);
    ELSE
        PERFORM fn_crypto_assert_custody_posting(NEW.posting_id);
    END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER trg_crypto_custody_posting_complete
    AFTER INSERT ON reg_crypto_custody_effect_postings DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_posting_complete();
CREATE CONSTRAINT TRIGGER trg_crypto_custody_recognition_complete
    AFTER INSERT ON reg_crypto_custody_effect_recognitions DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_posting_complete();

-- Until availability/reorg/cutover are implemented, receipt posting is limited
-- to a disabled, drained network. A network with these receipts cannot resume
-- execution merely by changing enabled=true.
CREATE FUNCTION fn_crypto_receipt_maintenance_guard(target UUID) RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
    PERFORM network_id FROM sys_crypto_network_policies
        WHERE network_id=target AND NOT enabled AND _deleted_at IS NULL FOR SHARE;
    IF NOT FOUND
       OR EXISTS(SELECT 1 FROM reg_crypto_fee_reservations WHERE network_id=target AND status IN ('reserved','broadcast','held'))
       OR EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations a WHERE a.network_id=target
            AND a.status IN ('created','signed','broadcast','held','superseded')
            AND NOT EXISTS(SELECT 1 FROM reg_crypto_chain_settlements s WHERE s.network_id=a.network_id
                AND a.tx_hash<>'' AND regexp_replace(lower(s.tx_hash),'^0x','')=regexp_replace(lower(a.tx_hash),'^0x',''))) THEN
        RAISE EXCEPTION 'receipt admission requires disabled drained network' USING ERRCODE='23514';
    END IF;
END;
$$;
CREATE FUNCTION fn_crypto_receipt_network_resume_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.enabled AND EXISTS(SELECT 1 FROM doc_crypto_custody_receipts WHERE network_id=NEW.network_id) THEN
        RAISE EXCEPTION 'receipt accounting cutover is not available' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_crypto_receipt_network_resume BEFORE INSERT OR UPDATE ON sys_crypto_network_policies
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_receipt_network_resume_guard();

CREATE FUNCTION fn_crypto_lock_receipt_balance(wallet UUID,asset UUID) RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO reg_crypto_balance_balances(wallet_id,token_id,amount) VALUES(wallet,asset,0)
        ON CONFLICT(wallet_id,token_id) DO NOTHING;
    PERFORM wallet_id FROM reg_crypto_balance_balances WHERE wallet_id=wallet AND token_id=asset FOR UPDATE;
END;
$$;
CREATE FUNCTION fn_crypto_unallocated_custody_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    PERFORM fn_crypto_lock_receipt_balance(NEW.wallet_id,NEW.token_id);
    IF NEW.recorder_type<>'CryptoCustodyReceipt' AND EXISTS(
        SELECT 1 FROM doc_crypto_custody_receipts d
        WHERE d.wallet_id=NEW.wallet_id AND d.token_id=NEW.token_id) THEN
        RAISE EXCEPTION 'receipt inventory requires availability cutover' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_crypto_unallocated_custody_guard BEFORE INSERT ON reg_crypto_balance_movements
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_unallocated_custody_guard();

CREATE FUNCTION fn_crypto_assert_custody_receipt(target UUID) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE d doc_crypto_custody_receipts;
BEGIN
    SELECT * INTO STRICT d FROM doc_crypto_custody_receipts WHERE id=target;
    PERFORM fn_crypto_receipt_maintenance_guard(d.network_id);
    IF NOT d.posted OR d.posted_version<>1
       OR NOT EXISTS(SELECT 1 FROM cat_wallets w WHERE w.id=d.wallet_id AND w.network_id=d.network_id
            AND w.custody_merchant_id IS NOT DISTINCT FROM d.custody_merchant_id)
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings p
          JOIN reg_crypto_accounting_effect_occurrences o ON o.id=p.occurrence_id
          WHERE p.id=d.posting_id AND p.recorder_id=d.id AND p.recorder_type='CryptoCustodyReceipt'
            AND p.recorder_version=1 AND p.occurrence_id=d.occurrence_id
            AND p.verification_id=d.verification_id AND o.amount=d.amount)
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_recognitions r
          WHERE r.posting_id=d.posting_id AND r.wallet_id=d.wallet_id AND r.token_id=d.token_id
            AND r.record_type='receipt' AND r.movement_line_id=d.custody_line_id)
       OR (SELECT count(*) FROM reg_crypto_custody_effect_recognitions WHERE posting_id=d.posting_id)<>1
       OR (SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=d.id AND recorder_type='CryptoCustodyReceipt')<>1
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_balance_movements m
            WHERE m.line_id=d.custody_line_id AND m.period=d.date)
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_receipt_suspense_movements m
            WHERE m.line_id=d.suspense_line_id AND m.receipt_id=d.id AND m.token_id=d.token_id AND m.amount=d.amount)
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_receipt_intake_audit a
            WHERE a.receipt_id=d.id AND a.posting_id=d.posting_id AND a.verification_id=d.verification_id)
       OR (SELECT count(*) FROM sys_outbox WHERE aggregate_id=d.id AND aggregate_type='CryptoCustodyReceipt' AND event_type='posted')<>1
       OR NOT EXISTS(SELECT 1 FROM sys_outbox WHERE aggregate_id=d.id AND aggregate_type='CryptoCustodyReceipt'
            AND event_type='posted' AND payload->>'entityId'=d.id::text
            AND payload->>'entityType'='CryptoCustodyReceipt' AND payload->>'action'='posted')
       OR EXISTS(SELECT 1 FROM reg_crypto_merchant_balance_movements WHERE recorder_id=d.id)
       OR EXISTS(SELECT 1 FROM reg_crypto_fee_movements WHERE recorder_id=d.id)
       OR EXISTS(SELECT 1 FROM reg_crypto_platform_cost_movements WHERE recorder_id=d.id)
       OR EXISTS(SELECT 1 FROM reg_crypto_provider_balance_movements WHERE recorder_id=d.id) THEN
        RAISE EXCEPTION 'custody receipt requires complete atomic accounting' USING ERRCODE='23514';
    END IF;
    IF EXISTS(SELECT 1 FROM reg_crypto_balance_movements m
        WHERE m.wallet_id=d.wallet_id AND m.token_id=d.token_id
          AND m.recorder_type<>'CryptoCustodyReceipt'
          AND NOT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_recognitions r WHERE r.movement_line_id=m.line_id)) THEN
        RAISE EXCEPTION 'existing wallet inventory requires accounting adoption' USING ERRCODE='23514';
    END IF;
    IF EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings p
        JOIN reg_crypto_accounting_effect_occurrences o ON o.id=p.occurrence_id
        JOIN reg_crypto_accounting_effects e ON e.id=p.effect_id
        JOIN cat_wallets source ON source.network_id=e.network_id AND source.address_key=o.source_address
        WHERE p.id=d.posting_id) THEN
        RAISE EXCEPTION 'internal transfer requires its settlement owner' USING ERRCODE='23514';
    END IF;
    -- Legacy recognizers have not yet joined the shared registry. Never admit
    -- a second receipt over their already recorded or in-flight principal.
    IF EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings p
        JOIN reg_crypto_accounting_effects e ON e.id=p.effect_id
        JOIN doc_crypto_payments old ON old.network_id=e.network_id
          AND regexp_replace(lower(old.tx_hash),'^0x','')=e.tx_hash AND old.event_kind=e.event_kind
        WHERE p.id=d.posting_id)
       OR EXISTS(SELECT 1 FROM reg_crypto_wallet_fundings old
          JOIN reg_crypto_custody_effect_postings p ON p.id=d.posting_id
          JOIN reg_crypto_accounting_effects e ON e.id=p.effect_id
          WHERE old.network_id=e.network_id AND regexp_replace(lower(old.tx_hash),'^0x','')=e.tx_hash)
       OR EXISTS(SELECT 1 FROM reg_crypto_aml_operating_receipts old
          JOIN reg_crypto_custody_effect_postings p ON p.id=d.posting_id
          JOIN reg_crypto_accounting_effects e ON e.id=p.effect_id
          WHERE old.network_id=e.network_id AND regexp_replace(lower(old.tx_hash),'^0x','')=e.tx_hash) THEN
        RAISE EXCEPTION 'legacy intake evidence requires accounting adoption' USING ERRCODE='23514';
    END IF;
    PERFORM fn_crypto_assert_custody_posting(d.posting_id);
END;
$$;
CREATE FUNCTION fn_crypto_custody_receipt_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    PERFORM fn_crypto_assert_custody_receipt(NEW.id);
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER trg_crypto_custody_receipt_complete AFTER INSERT OR UPDATE ON doc_crypto_custody_receipts
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_receipt_complete();

CREATE FUNCTION fn_crypto_custody_receipt_movement_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.recorder_type='CryptoCustodyReceipt' THEN
        IF NOT EXISTS(SELECT 1 FROM doc_crypto_custody_receipts WHERE id=NEW.recorder_id) THEN
            RAISE EXCEPTION 'receipt movement requires its accounting document' USING ERRCODE='23514';
        END IF;
        PERFORM fn_crypto_assert_custody_receipt(NEW.recorder_id);
    END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER trg_crypto_custody_receipt_movement_complete AFTER INSERT ON reg_crypto_balance_movements
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_receipt_movement_complete();

CREATE FUNCTION fn_crypto_custody_receipt_no_attribution() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.recorder_type='CryptoCustodyReceipt' OR EXISTS(SELECT 1 FROM doc_crypto_custody_receipts WHERE id=NEW.recorder_id) THEN
        RAISE EXCEPTION 'custody receipt cannot create attributed financial movements' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
DO $$
DECLARE target TEXT;
BEGIN
    FOREACH target IN ARRAY ARRAY['reg_crypto_merchant_balance_movements','reg_crypto_fee_movements',
        'reg_crypto_platform_cost_movements','reg_crypto_provider_balance_movements'] LOOP
        EXECUTE format('CREATE TRIGGER %I BEFORE INSERT ON %I FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_receipt_no_attribution()',
            'trg_no_receipt_'||target,target);
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$
BEGIN
    IF EXISTS(SELECT 1 FROM doc_crypto_custody_receipts)
       OR EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings) THEN
        RAISE EXCEPTION 'custody recognition history requires forward migration';
    END IF;
END;
$$;
DROP TRIGGER trg_crypto_receipt_network_resume ON sys_crypto_network_policies;
DROP TRIGGER trg_crypto_unallocated_custody_guard ON reg_crypto_balance_movements;
DROP TRIGGER trg_crypto_custody_receipt_movement_complete ON reg_crypto_balance_movements;
DROP TRIGGER trg_no_receipt_reg_crypto_merchant_balance_movements ON reg_crypto_merchant_balance_movements;
DROP TRIGGER trg_no_receipt_reg_crypto_fee_movements ON reg_crypto_fee_movements;
DROP TRIGGER trg_no_receipt_reg_crypto_platform_cost_movements ON reg_crypto_platform_cost_movements;
DROP TRIGGER trg_no_receipt_reg_crypto_provider_balance_movements ON reg_crypto_provider_balance_movements;
DROP TABLE reg_crypto_receipt_intake_audit;
DROP TABLE reg_crypto_receipt_suspense_balances;
DROP TABLE reg_crypto_receipt_suspense_movements;
DROP TABLE doc_crypto_custody_receipts;
DROP TABLE reg_crypto_custody_effect_recognitions;
DROP TABLE reg_crypto_custody_effect_postings;
DROP FUNCTION fn_crypto_custody_receipt_complete();
DROP FUNCTION fn_crypto_assert_custody_receipt(UUID);
DROP FUNCTION fn_crypto_receipt_network_resume_guard();
DROP FUNCTION fn_crypto_receipt_maintenance_guard(UUID);
DROP FUNCTION fn_crypto_custody_posting_complete();
DROP FUNCTION fn_crypto_assert_custody_posting(UUID);
DROP FUNCTION fn_crypto_custody_posting_identity_guard();
DROP FUNCTION fn_crypto_receipt_suspense_apply();
DROP FUNCTION fn_crypto_custody_receipt_mutation_guard();
DROP FUNCTION fn_crypto_unallocated_custody_guard();
DROP FUNCTION fn_crypto_lock_receipt_balance(UUID,UUID);
DROP FUNCTION fn_crypto_custody_receipt_movement_complete();
DROP FUNCTION fn_crypto_custody_receipt_no_attribution();
-- +goose StatementEnd
