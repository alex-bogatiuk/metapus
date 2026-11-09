-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- Historical decisions and pending proposals retain their original semantics.
-- A chain compensation refers to the original owner; it never claims an effect.
ALTER TABLE doc_crypto_adjustments
    ADD COLUMN accounting_model_version INT NOT NULL DEFAULT 1 CHECK(accounting_model_version IN (1,2)),
    ADD COLUMN basis_kind TEXT NOT NULL DEFAULT 'legacy',
    ADD COLUMN source_posting_id UUID REFERENCES reg_crypto_custody_effect_postings(id),
    ADD COLUMN proposal_txid BIGINT,
    ADD CONSTRAINT ck_crypto_adjustment_accounting_basis CHECK (
        (accounting_model_version=1 AND basis_kind='legacy' AND source_posting_id IS NULL AND proposal_txid IS NULL)
        OR (accounting_model_version=2 AND proposal_txid IS NOT NULL AND (
            (mode='wallet_balance' AND basis_kind='wallet_variance' AND source_posting_id IS NULL)
            OR (mode='payment_reversal' AND (
                (basis_kind='legacy_payment' AND source_posting_id IS NULL)
                OR (basis_kind='payment_compensation' AND source_posting_id IS NOT NULL))))));
ALTER TABLE doc_crypto_adjustments ALTER COLUMN accounting_model_version SET DEFAULT 2;
CREATE INDEX idx_crypto_adjustment_source_posting ON doc_crypto_adjustments(source_posting_id) WHERE source_posting_id IS NOT NULL;

CREATE FUNCTION fn_crypto_adjustment_accounting_basis() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE d doc_crypto_payments; posting UUID;
BEGIN
    IF TG_OP='DELETE' THEN RAISE EXCEPTION 'crypto adjustment history is immutable' USING ERRCODE='23514'; END IF;
    IF TG_OP='INSERT' THEN
        IF NEW.accounting_model_version<>2 OR NEW.status<>'pending_approval' THEN
            RAISE EXCEPTION 'new crypto adjustment requires model 2 proposal' USING ERRCODE='23514';
        END IF;
        NEW.proposal_txid:=txid_current();
    ELSE
        IF (OLD.accounting_model_version,OLD.basis_kind,OLD.source_posting_id,OLD.proposal_txid,OLD.created_at)
            IS DISTINCT FROM (NEW.accounting_model_version,NEW.basis_kind,NEW.source_posting_id,NEW.proposal_txid,NEW.created_at) THEN
            RAISE EXCEPTION 'crypto adjustment accounting basis is immutable' USING ERRCODE='23514';
        END IF;
    END IF;
    IF NEW.accounting_model_version=1 OR NEW.mode<>'payment_reversal'
        OR (TG_OP='UPDATE' AND NEW.status<>'posted') THEN RETURN NEW; END IF;
    SELECT * INTO STRICT d FROM doc_crypto_payments WHERE id=NEW.payment_id;
    SELECT p.id INTO posting FROM reg_crypto_custody_effect_postings p
        JOIN reg_crypto_payment_custody_bindings b ON b.posting_id=p.id AND b.payment_id=d.id
        WHERE p.recorder_type='CryptoPayment' AND p.recorder_id=d.id AND p.recorder_version=NEW.posted_version;
    IF d.status<>'settled' OR NOT d.posted OR d.posted_version<>NEW.posted_version THEN
        RAISE EXCEPTION 'crypto adjustment requires current settled payment generation' USING ERRCODE='23514';
    END IF;
    IF posting IS NOT NULL THEN
        IF NEW.basis_kind<>'payment_compensation' OR NEW.source_posting_id IS DISTINCT FROM posting THEN
            RAISE EXCEPTION 'crypto adjustment requires exact source custody posting' USING ERRCODE='23514';
        END IF;
    ELSIF NEW.basis_kind<>'legacy_payment' OR NEW.source_posting_id IS NOT NULL
        OR fn_crypto_payment_requires_recognition(d.id,d.network_id,d.tx_hash,d.event_kind,d.event_index) THEN
        RAISE EXCEPTION 'indexed payment requires explicit adoption before adjustment' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_crypto_adjustment_accounting_basis BEFORE INSERT OR UPDATE OR DELETE ON doc_crypto_adjustments
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_adjustment_accounting_basis();

CREATE OR REPLACE FUNCTION fn_crypto_adjustment_source_insert_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE a doc_crypto_adjustments; source JSONB; source_table TEXT; used NUMERIC;
BEGIN
    SELECT * INTO STRICT a FROM doc_crypto_adjustments WHERE id=NEW.adjustment_id;
    IF a.status<>'pending_approval' THEN RAISE EXCEPTION 'crypto adjustment sources are frozen after decision' USING ERRCODE='23514'; END IF;
    IF a.accounting_model_version=1 THEN RETURN NEW; END IF;
    IF a.mode<>'payment_reversal' OR a.proposal_txid<>txid_current() OR NEW.amount IS NULL THEN
        RAISE EXCEPTION 'crypto adjustment sources are frozen at proposal' USING ERRCODE='23514';
    END IF;
    source_table:=CASE NEW.source_register WHEN 'custody' THEN 'reg_crypto_balance_movements'
        WHEN 'merchant' THEN 'reg_crypto_merchant_balance_movements' WHEN 'fee' THEN 'reg_crypto_fee_movements' END;
    IF source_table IS NULL THEN RAISE EXCEPTION 'invalid adjustment source register' USING ERRCODE='23514'; END IF;
    EXECUTE format('SELECT to_jsonb(m) FROM %I m WHERE line_id=$1',source_table) INTO source USING NEW.source_line_id;
    EXECUTE format('SELECT COALESCE(SUM(amount),0) FROM %I WHERE reversal_of_line_id=$1',source_table) INTO used USING NEW.source_line_id;
    IF source IS NULL OR source->>'recorder_type'<>'CryptoPayment' OR (source->>'recorder_id')::uuid<>a.payment_id
        OR (source->>'recorder_version')::int<>a.posted_version OR source->>'reversal_of_line_id' IS NOT NULL
        OR source->>'record_type'<>'receipt' OR NEW.amount>(source->>'amount')::numeric-used THEN
        RAISE EXCEPTION 'crypto adjustment requires available original payment source' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION fn_crypto_assert_adjustment_accounting(target UUID) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE a doc_crypto_adjustments; kind TEXT; source_table TEXT; selected_count INT; movement_count INT; exact_count INT;
DECLARE custody NUMERIC; merchant NUMERIC; fee NUMERIC;
BEGIN
    SELECT * INTO a FROM doc_crypto_adjustments WHERE id=target;
    IF NOT FOUND OR a.accounting_model_version=1 THEN RETURN; END IF;
    IF a.mode='payment_reversal' THEN
        SELECT COALESCE(SUM(amount) FILTER(WHERE source_register='custody'),0),
            COALESCE(SUM(amount) FILTER(WHERE source_register='merchant'),0),
            COALESCE(SUM(amount) FILTER(WHERE source_register='fee'),0)
            INTO custody,merchant,fee FROM reg_crypto_adjustment_sources WHERE adjustment_id=a.id;
        IF custody<=0 OR custody<>merchant+fee THEN
            RAISE EXCEPTION 'crypto adjustment requires balanced source selection' USING ERRCODE='23514';
        END IF;
    ELSIF EXISTS(SELECT 1 FROM reg_crypto_adjustment_sources WHERE adjustment_id=a.id) THEN
        RAISE EXCEPTION 'wallet variance cannot select payment sources' USING ERRCODE='23514';
    END IF;
    FOREACH kind IN ARRAY ARRAY['custody','merchant','fee'] LOOP
        source_table:=CASE kind WHEN 'custody' THEN 'reg_crypto_balance_movements'
            WHEN 'merchant' THEN 'reg_crypto_merchant_balance_movements' ELSE 'reg_crypto_fee_movements' END;
        SELECT count(*) INTO selected_count FROM reg_crypto_adjustment_sources WHERE adjustment_id=a.id AND source_register=kind;
        EXECUTE format('SELECT count(*) FROM %I WHERE recorder_id=$1',source_table) INTO movement_count USING a.id;
        IF a.status<>'posted' THEN
            IF movement_count<>0 THEN RAISE EXCEPTION 'undecided adjustment cannot have financial movements' USING ERRCODE='23514'; END IF;
        ELSIF a.mode='payment_reversal' THEN
            EXECUTE format('SELECT count(*) FROM %1$I m JOIN reg_crypto_adjustment_sources s ON s.adjustment_id=$1 AND s.source_register=$2 AND s.source_line_id=m.reversal_of_line_id
                JOIN %1$I original ON original.line_id=s.source_line_id
                WHERE m.recorder_id=$1 AND m.recorder_type=''CryptoAdjustment'' AND m.recorder_version=1
                AND m.record_type=''expense'' AND m.amount=s.amount AND m.reversal_reason=''manual_financial_correction''
                AND (to_jsonb(m)->>''wallet_id'',to_jsonb(m)->>''merchant_id'',m.token_id,to_jsonb(m)->>''fee_type'')
                    IS NOT DISTINCT FROM (to_jsonb(original)->>''wallet_id'',to_jsonb(original)->>''merchant_id'',original.token_id,to_jsonb(original)->>''fee_type'')',source_table)
                INTO exact_count USING a.id,kind;
            IF movement_count<>selected_count OR exact_count<>selected_count THEN
                RAISE EXCEPTION 'crypto adjustment requires exact selected source reversals' USING ERRCODE='23514';
            END IF;
        ELSIF kind='custody' THEN
            SELECT count(*) INTO exact_count FROM reg_crypto_balance_movements WHERE recorder_id=a.id
                AND recorder_type='CryptoAdjustment' AND recorder_version=1 AND reversal_of_line_id IS NULL
                AND (wallet_id,token_id,record_type,amount)=(a.wallet_id,a.token_id,a.direction,a.amount);
            IF movement_count<>1 OR exact_count<>1 THEN RAISE EXCEPTION 'wallet variance requires exact custody movement' USING ERRCODE='23514'; END IF;
        ELSIF movement_count<>0 THEN
            RAISE EXCEPTION 'wallet variance cannot change merchant or fee balances' USING ERRCODE='23514';
        END IF;
    END LOOP;
    IF EXISTS(SELECT 1 FROM reg_crypto_platform_cost_movements WHERE recorder_id=a.id)
        OR EXISTS(SELECT 1 FROM reg_crypto_provider_balance_movements WHERE recorder_id=a.id)
        OR EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings WHERE recorder_id=a.id) THEN
        RAISE EXCEPTION 'manual adjustment cannot claim chain principal or unrelated financial accounts' USING ERRCODE='23514';
    END IF;
    IF a.status='posted' AND a.mode='wallet_balance' THEN
        IF (SELECT count(*) FROM reg_crypto_wallet_variance_movements WHERE adjustment_id=a.id)<>1
            OR NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_variance_movements WHERE adjustment_id=a.id
                AND (wallet_id,token_id,amount,variance_type)=(a.wallet_id,a.token_id,a.amount,CASE a.direction WHEN 'receipt' THEN 'surplus' ELSE 'shortage' END)) THEN
            RAISE EXCEPTION 'wallet variance requires exact balancing variance account' USING ERRCODE='23514';
        END IF;
    ELSIF EXISTS(SELECT 1 FROM reg_crypto_wallet_variance_movements WHERE adjustment_id=a.id) THEN
        RAISE EXCEPTION 'payment compensation or undecided adjustment cannot have variance' USING ERRCODE='23514';
    END IF;
    IF a.status='posted' THEN
        IF (SELECT count(*) FROM sys_outbox WHERE aggregate_type='CryptoAdjustment' AND aggregate_id=a.id AND event_type='financial_correction_posted')<>1
            OR NOT EXISTS(SELECT 1 FROM sys_outbox WHERE aggregate_type='CryptoAdjustment' AND aggregate_id=a.id AND event_type='financial_correction_posted'
                AND payload->>'entityType'='CryptoAdjustment' AND payload->>'entityId'=a.id::text AND payload->>'action'='financial_correction_posted'
                AND payload->>'accountingModelVersion'='2' AND payload->>'basisKind'=a.basis_kind
                AND (payload->>'sourcePostingId') IS NOT DISTINCT FROM a.source_posting_id::text
                AND payload->>'postedVersion'=COALESCE(a.posted_version,0)::text) THEN
            RAISE EXCEPTION 'crypto adjustment requires accounting basis outbox' USING ERRCODE='23514';
        END IF;
    ELSIF EXISTS(SELECT 1 FROM sys_outbox WHERE aggregate_type='CryptoAdjustment' AND aggregate_id=a.id AND event_type='financial_correction_posted') THEN
        RAISE EXCEPTION 'undecided adjustment cannot publish financial correction' USING ERRCODE='23514';
    END IF;
END;
$$;

CREATE FUNCTION fn_crypto_adjustment_accounting_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target UUID;
BEGIN
    IF TG_ARGV[0]='doc_crypto_adjustments' THEN target:=NEW.id;
    ELSIF TG_ARGV[0] IN ('reg_crypto_adjustment_sources','reg_crypto_wallet_variance_movements') THEN target:=NEW.adjustment_id;
    ELSIF TG_ARGV[0]='sys_outbox' THEN
        IF NEW.aggregate_type<>'CryptoAdjustment' OR NEW.event_type<>'financial_correction_posted' THEN RETURN NULL; END IF;
        target:=NEW.aggregate_id;
    ELSE
        target:=NEW.recorder_id;
        IF NEW.recorder_type='CryptoAdjustment' AND NOT EXISTS(SELECT 1 FROM doc_crypto_adjustments WHERE id=target) THEN
            RAISE EXCEPTION 'manual correction requires its adjustment document' USING ERRCODE='23514';
        END IF;
    END IF;
    PERFORM fn_crypto_assert_adjustment_accounting(target);
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER trg_crypto_adjustment_accounting_complete AFTER INSERT OR UPDATE ON doc_crypto_adjustments
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_adjustment_accounting_complete('doc_crypto_adjustments');
DO $$
DECLARE table_name TEXT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY['reg_crypto_adjustment_sources','reg_crypto_wallet_variance_movements',
        'reg_crypto_balance_movements','reg_crypto_merchant_balance_movements','reg_crypto_fee_movements',
        'reg_crypto_platform_cost_movements','reg_crypto_provider_balance_movements','sys_outbox'] LOOP
        EXECUTE format('CREATE CONSTRAINT TRIGGER trg_crypto_adjustment_accounting_complete AFTER INSERT ON %I
            DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_adjustment_accounting_complete(%L)',table_name,table_name);
    END LOOP;
END;
$$;

CREATE FUNCTION fn_crypto_adjustment_cannot_claim_effect() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.recorder_type='CryptoAdjustment' OR EXISTS(SELECT 1 FROM doc_crypto_adjustments WHERE id=NEW.recorder_id) THEN
        RAISE EXCEPTION 'manual correction references source posting and cannot claim a chain effect' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_crypto_adjustment_cannot_claim_effect BEFORE INSERT ON reg_crypto_custody_effect_postings
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_adjustment_cannot_claim_effect();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN IF EXISTS(SELECT 1 FROM doc_crypto_adjustments WHERE accounting_model_version=2) THEN
    RAISE EXCEPTION 'crypto adjustment accounting basis requires forward migration'; END IF; END; $$;
DROP TRIGGER trg_crypto_adjustment_cannot_claim_effect ON reg_crypto_custody_effect_postings;
DROP FUNCTION fn_crypto_adjustment_cannot_claim_effect();
DROP FUNCTION fn_crypto_adjustment_accounting_complete() CASCADE;
DROP FUNCTION fn_crypto_assert_adjustment_accounting(UUID);
DROP TRIGGER trg_crypto_adjustment_accounting_basis ON doc_crypto_adjustments;
DROP FUNCTION fn_crypto_adjustment_accounting_basis();
CREATE OR REPLACE FUNCTION fn_crypto_adjustment_source_insert_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM doc_crypto_adjustments WHERE id=NEW.adjustment_id AND status='pending_approval') THEN
        RAISE EXCEPTION 'crypto adjustment sources are frozen after decision';
    END IF;
    RETURN NEW;
END;
$$;
ALTER TABLE doc_crypto_adjustments DROP CONSTRAINT ck_crypto_adjustment_accounting_basis,
    DROP COLUMN accounting_model_version,DROP COLUMN basis_kind,DROP COLUMN source_posting_id,DROP COLUMN proposal_txid;
-- +goose StatementEnd
