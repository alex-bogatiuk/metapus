-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

ALTER TABLE reg_crypto_custody_effect_postings
    DROP CONSTRAINT reg_crypto_custody_effect_postings_generation_check,
    ADD COLUMN predecessor_id UUID UNIQUE REFERENCES reg_crypto_custody_effect_postings(id),
    ADD CONSTRAINT ck_crypto_custody_generation CHECK(generation>0 AND
        ((generation=1 AND predecessor_id IS NULL) OR (generation>1 AND predecessor_id IS NOT NULL)));
CREATE UNIQUE INDEX uq_crypto_payment_custody_version
    ON reg_crypto_custody_effect_postings(recorder_id,recorder_version) WHERE recorder_type='CryptoPayment';

-- COPY may use a subtransaction, so xmin is not a reliable top-level posting
-- identity. Stamp new payment lines explicitly; historical rows stay NULL.
ALTER TABLE reg_crypto_balance_movements ADD COLUMN posting_txid BIGINT;
CREATE FUNCTION fn_crypto_payment_posting_transaction() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    NEW.posting_txid:=CASE WHEN NEW.recorder_type='CryptoPayment' AND NEW.reversal_of_line_id IS NULL THEN txid_current() ELSE NULL END;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_crypto_payment_posting_transaction BEFORE INSERT ON reg_crypto_balance_movements
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_payment_posting_transaction();

-- An immutable economic snapshot also serves as the admission/adoption audit.
-- Neither migration nor adoption creates another financial movement.
CREATE TABLE reg_crypto_payment_custody_bindings (
    posting_id UUID PRIMARY KEY REFERENCES reg_crypto_custody_effect_postings(id),
    payment_id UUID NOT NULL REFERENCES doc_crypto_payments(id),
    invoice_id UUID NOT NULL,
    merchant_id UUID NOT NULL REFERENCES cat_merchants(id),
    wallet_id UUID NOT NULL REFERENCES cat_wallets(id),
    token_id UUID NOT NULL REFERENCES cat_tokens(id),
    gross NUMERIC(78,0) NOT NULL CHECK(gross>0),
    net NUMERIC(78,0) NOT NULL CHECK(net>=0),
    processing_fee NUMERIC(78,0) NOT NULL CHECK(processing_fee>=0),
    fee_fixed NUMERIC(78,0) NOT NULL CHECK(fee_fixed>=0),
    fee_percent_bp INT NOT NULL CHECK(fee_percent_bp BETWEEN 0 AND 10000),
    fee_min NUMERIC(78,0) NOT NULL CHECK(fee_min>=0),
    fee_max NUMERIC(78,0) NOT NULL CHECK(fee_max>=0),
    period TIMESTAMPTZ NOT NULL,
    mode TEXT NOT NULL CHECK(mode IN ('posting','adoption')),
    actor TEXT NOT NULL CHECK(btrim(actor)<>''),
    evidence_reference TEXT NOT NULL CHECK(btrim(evidence_reference)<>''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK(gross=net+processing_fee)
);
CREATE INDEX idx_crypto_payment_custody_bindings_payment ON reg_crypto_payment_custody_bindings(payment_id);
CREATE TRIGGER trg_crypto_payment_custody_binding_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_payment_custody_bindings
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_financial_movement_immutable();

CREATE FUNCTION fn_crypto_payment_requires_recognition(payment UUID,network UUID,hash TEXT,kind TEXT,event_index BIGINT)
RETURNS BOOLEAN LANGUAGE sql VOLATILE AS $$
    SELECT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings WHERE recorder_type='CryptoPayment' AND recorder_id=payment)
       OR EXISTS(SELECT 1 FROM reg_crypto_transfer_verifications v
          JOIN reg_crypto_accounting_effect_evidence x ON x.verification_id=v.id
          WHERE v.network_id=network AND regexp_replace(lower(v.tx_hash),'^0x','')=regexp_replace(lower(hash),'^0x','')
            AND v.event_kind=kind AND v.event_index=$5)
       OR EXISTS(SELECT 1 FROM doc_crypto_payments d JOIN reg_crypto_accounting_effect_evidence x ON x.verification_id=d.verification_id WHERE d.id=payment)
$$;

CREATE FUNCTION fn_crypto_payment_reversal_shape_valid(target UUID) RETURNS BOOLEAN LANGUAGE sql VOLATILE AS $$
    SELECT NOT EXISTS(
        SELECT 1 FROM reg_crypto_custody_effect_postings p JOIN reg_crypto_balance_movements m
          ON m.recorder_id=p.recorder_id AND m.recorder_type='CryptoPayment' AND m.recorder_version=p.recorder_version AND m.reversal_of_line_id IS NULL
        JOIN reg_crypto_balance_movements r ON r.reversal_of_line_id=m.line_id
        WHERE p.id=target AND (r.record_type=m.record_type OR r.wallet_id<>m.wallet_id OR r.token_id<>m.token_id
            OR EXISTS(SELECT 1 FROM reg_crypto_balance_movements x WHERE x.reversal_of_line_id=r.line_id)))
    AND NOT EXISTS(
        SELECT 1 FROM reg_crypto_custody_effect_postings p JOIN reg_crypto_merchant_balance_movements m
          ON m.recorder_id=p.recorder_id AND m.recorder_type='CryptoPayment' AND m.recorder_version=p.recorder_version AND m.reversal_of_line_id IS NULL
        JOIN reg_crypto_merchant_balance_movements r ON r.reversal_of_line_id=m.line_id
        WHERE p.id=target AND (r.record_type=m.record_type OR r.merchant_id<>m.merchant_id OR r.token_id<>m.token_id
            OR EXISTS(SELECT 1 FROM reg_crypto_merchant_balance_movements x WHERE x.reversal_of_line_id=r.line_id)))
    AND NOT EXISTS(
        SELECT 1 FROM reg_crypto_custody_effect_postings p JOIN reg_crypto_fee_movements m
          ON m.recorder_id=p.recorder_id AND m.recorder_type='CryptoPayment' AND m.recorder_version=p.recorder_version AND m.reversal_of_line_id IS NULL
        JOIN reg_crypto_fee_movements r ON r.reversal_of_line_id=m.line_id
        WHERE p.id=target AND (r.record_type=m.record_type OR r.merchant_id<>m.merchant_id OR r.token_id<>m.token_id OR r.fee_type<>m.fee_type
            OR EXISTS(SELECT 1 FROM reg_crypto_fee_movements x WHERE x.reversal_of_line_id=r.line_id)))
$$;

CREATE FUNCTION fn_crypto_payment_fully_compensated(target UUID) RETURNS BOOLEAN LANGUAGE sql VOLATILE AS $$
    SELECT fn_crypto_payment_reversal_shape_valid(target)
    AND NOT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings p JOIN reg_crypto_balance_movements m
        ON m.recorder_id=p.recorder_id AND m.recorder_type='CryptoPayment' AND m.recorder_version=p.recorder_version AND m.reversal_of_line_id IS NULL
        WHERE p.id=target AND m.amount<>COALESCE((SELECT SUM(amount) FROM reg_crypto_balance_movements WHERE reversal_of_line_id=m.line_id),0))
    AND NOT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings p JOIN reg_crypto_merchant_balance_movements m
        ON m.recorder_id=p.recorder_id AND m.recorder_type='CryptoPayment' AND m.recorder_version=p.recorder_version AND m.reversal_of_line_id IS NULL
        WHERE p.id=target AND m.amount<>COALESCE((SELECT SUM(amount) FROM reg_crypto_merchant_balance_movements WHERE reversal_of_line_id=m.line_id),0))
    AND NOT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings p JOIN reg_crypto_fee_movements m
        ON m.recorder_id=p.recorder_id AND m.recorder_type='CryptoPayment' AND m.recorder_version=p.recorder_version AND m.reversal_of_line_id IS NULL
        WHERE p.id=target AND m.amount<>COALESCE((SELECT SUM(amount) FROM reg_crypto_fee_movements WHERE reversal_of_line_id=m.line_id),0))
$$;

CREATE FUNCTION fn_crypto_assert_payment_custody(target UUID) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE p reg_crypto_custody_effect_postings; b reg_crypto_payment_custody_bindings;
DECLARE d doc_crypto_payments; fee NUMERIC;
BEGIN
    SELECT * INTO STRICT p FROM reg_crypto_custody_effect_postings WHERE id=target;
    IF p.recorder_type<>'CryptoPayment' THEN RETURN; END IF;
    SELECT * INTO b FROM reg_crypto_payment_custody_bindings WHERE posting_id=p.id;
    IF NOT FOUND THEN RAISE EXCEPTION 'payment custody requires its immutable economic snapshot' USING ERRCODE='23514'; END IF;
    SELECT * INTO STRICT d FROM doc_crypto_payments WHERE id=p.recorder_id;
    fee:=b.fee_fixed+trunc(b.gross*b.fee_percent_bp/10000);
    IF b.fee_min>0 THEN fee:=greatest(fee,b.fee_min); END IF;
    IF b.fee_max>0 THEN fee:=least(fee,b.fee_max); END IF;
    IF b.payment_id<>p.recorder_id OR fee<>b.processing_fee OR fee>b.gross
       OR (d.invoice_id,d.merchant_id,d.wallet_id,d.token_id,d.amount,d.fee_fixed,d.fee_percent_bp,d.fee_min,d.fee_max,d.date)
          IS DISTINCT FROM (b.invoice_id,b.merchant_id,b.wallet_id,b.token_id,b.gross,b.fee_fixed,b.fee_percent_bp,b.fee_min,b.fee_max,b.period)
       OR NOT d.posted OR d.posted_version<>p.recorder_version OR d.status NOT IN ('confirmed','settled')
       OR (SELECT count(*) FROM reg_crypto_custody_effect_recognitions WHERE posting_id=p.id)<>1
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_recognitions r JOIN reg_crypto_balance_movements m ON m.line_id=r.movement_line_id
            WHERE r.posting_id=p.id AND r.wallet_id=b.wallet_id AND r.token_id=b.token_id AND r.record_type='receipt'
              AND m.period=b.period AND m.amount=b.gross AND m.recorder_id=d.id AND m.recorder_type='CryptoPayment' AND m.recorder_version=p.recorder_version)
       OR (b.mode='posting' AND NOT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_recognitions r
            JOIN reg_crypto_balance_movements m ON m.line_id=r.movement_line_id
            WHERE r.posting_id=p.id AND m.posting_txid=txid_current()))
       OR (SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=d.id AND recorder_type='CryptoPayment' AND recorder_version=p.recorder_version AND reversal_of_line_id IS NULL)<>1
       OR (SELECT count(*) FROM reg_crypto_merchant_balance_movements WHERE recorder_id=d.id AND recorder_type='CryptoPayment' AND recorder_version=p.recorder_version AND reversal_of_line_id IS NULL)<>(CASE WHEN b.net>0 THEN 1 ELSE 0 END)
       OR EXISTS(SELECT 1 FROM reg_crypto_merchant_balance_movements WHERE recorder_id=d.id AND recorder_type='CryptoPayment' AND recorder_version=p.recorder_version AND reversal_of_line_id IS NULL
            AND (record_type<>'receipt' OR merchant_id<>b.merchant_id OR token_id<>b.token_id OR amount<>b.net OR period<>b.period))
       OR (SELECT count(*) FROM reg_crypto_fee_movements WHERE recorder_id=d.id AND recorder_type='CryptoPayment' AND recorder_version=p.recorder_version AND reversal_of_line_id IS NULL)<>(CASE WHEN b.processing_fee>0 THEN 1 ELSE 0 END)
       OR EXISTS(SELECT 1 FROM reg_crypto_fee_movements WHERE recorder_id=d.id AND recorder_type='CryptoPayment' AND recorder_version=p.recorder_version AND reversal_of_line_id IS NULL
            AND (record_type<>'receipt' OR merchant_id<>b.merchant_id OR token_id<>b.token_id OR amount<>b.processing_fee OR fee_type<>'processing' OR period<>b.period))
       OR EXISTS(SELECT 1 FROM reg_crypto_platform_cost_movements WHERE recorder_id=d.id)
       OR EXISTS(SELECT 1 FROM reg_crypto_provider_balance_movements WHERE recorder_id=d.id)
       OR NOT fn_crypto_payment_reversal_shape_valid(p.id)
       OR (SELECT count(*) FROM sys_outbox WHERE aggregate_type='CryptoPayment' AND aggregate_id=d.id AND event_type='custody_recognized' AND payload->>'postingId'=p.id::text)<>1
       OR NOT EXISTS(SELECT 1 FROM sys_outbox WHERE aggregate_type='CryptoPayment' AND aggregate_id=d.id AND event_type='custody_recognized'
            AND payload->>'postingId'=p.id::text AND payload->>'entityId'=d.id::text AND payload->>'entityType'='CryptoPayment'
            AND payload->>'action'='custody_recognized' AND payload->>'generation'=p.generation::text
            AND payload->>'postedVersion'=p.recorder_version::text AND payload->>'mode'=b.mode) THEN
        RAISE EXCEPTION 'payment custody requires complete immutable financial set and outbox' USING ERRCODE='23514';
    END IF;
    IF EXISTS(SELECT 1 FROM reg_crypto_balance_movements m WHERE m.recorder_id=d.id AND m.recorder_type='CryptoPayment'
        AND m.recorder_version<>p.recorder_version AND m.reversal_of_line_id IS NULL
        AND m.amount<>COALESCE((SELECT SUM(amount) FROM reg_crypto_balance_movements WHERE reversal_of_line_id=m.line_id),0)) THEN
        RAISE EXCEPTION 'other payment principal generation is not fully compensated' USING ERRCODE='23514';
    END IF;
    IF EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings c JOIN reg_crypto_accounting_effects e ON e.id=c.effect_id
        JOIN reg_crypto_accounting_effect_occurrences o ON o.id=c.occurrence_id
        JOIN cat_wallets source ON source.network_id=e.network_id AND source.address_key=o.source_address WHERE c.id=p.id) THEN
        RAISE EXCEPTION 'managed payment transfer requires its settlement owner' USING ERRCODE='23514';
    END IF;
    IF b.mode='adoption' THEN PERFORM fn_crypto_receipt_maintenance_guard(d.network_id); END IF;
END;
$$;

CREATE FUNCTION fn_crypto_custody_generation_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE previous reg_crypto_custody_effect_postings; b reg_crypto_payment_custody_bindings; old_b reg_crypto_payment_custody_bindings;
BEGIN
    IF NEW.generation=1 THEN
        IF NEW.recorder_type='CryptoPayment' AND NEW.recorder_version<>1
            AND NOT EXISTS(SELECT 1 FROM reg_crypto_payment_custody_bindings WHERE posting_id=NEW.id AND mode='adoption') THEN
            RAISE EXCEPTION 'legacy payment generation requires explicit adoption' USING ERRCODE='23514';
        END IF;
    ELSE
        SELECT * INTO STRICT previous FROM reg_crypto_custody_effect_postings WHERE id=NEW.predecessor_id;
        SELECT * INTO STRICT b FROM reg_crypto_payment_custody_bindings WHERE posting_id=NEW.id;
        SELECT * INTO STRICT old_b FROM reg_crypto_payment_custody_bindings WHERE posting_id=previous.id;
        IF NEW.recorder_type<>'CryptoPayment' OR previous.recorder_type<>'CryptoPayment'
           OR NEW.effect_id<>previous.effect_id OR NEW.recorder_id<>previous.recorder_id
           OR NEW.generation<>previous.generation+1 OR NEW.recorder_version<>previous.recorder_version+1
           OR NOT fn_crypto_payment_fully_compensated(previous.id)
           OR (b.invoice_id,b.merchant_id,b.wallet_id,b.token_id,b.gross,b.net,b.processing_fee,b.fee_fixed,b.fee_percent_bp,b.fee_min,b.fee_max,b.period)
              IS DISTINCT FROM (old_b.invoice_id,old_b.merchant_id,old_b.wallet_id,old_b.token_id,old_b.gross,old_b.net,old_b.processing_fee,old_b.fee_fixed,old_b.fee_percent_bp,old_b.fee_min,old_b.fee_max,old_b.period)
           OR (SELECT economics_hash FROM reg_crypto_accounting_effect_occurrences WHERE id=NEW.occurrence_id)
              IS DISTINCT FROM (SELECT economics_hash FROM reg_crypto_accounting_effect_occurrences WHERE id=previous.occurrence_id) THEN
            RAISE EXCEPTION 'custody generation requires same owner and full prior compensation without repricing' USING ERRCODE='23514';
        END IF;
    END IF;
    PERFORM fn_crypto_assert_payment_custody(NEW.id);
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER trg_crypto_custody_generation_complete AFTER INSERT ON reg_crypto_custody_effect_postings
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_generation_complete();

CREATE FUNCTION fn_crypto_payment_binding_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN PERFORM fn_crypto_assert_payment_custody(NEW.posting_id); RETURN NULL; END;
$$;
CREATE CONSTRAINT TRIGGER trg_crypto_payment_binding_complete AFTER INSERT ON reg_crypto_payment_custody_bindings
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_payment_binding_complete();

CREATE FUNCTION fn_crypto_payment_financial_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE d doc_crypto_payments; target UUID;
BEGIN
    IF NEW.reversal_of_line_id IS NOT NULL THEN RETURN NULL; END IF;
    SELECT p.id INTO target FROM reg_crypto_custody_effect_postings p
        WHERE p.recorder_type='CryptoPayment' AND p.recorder_id=NEW.recorder_id AND p.recorder_version=NEW.recorder_version;
    IF FOUND THEN
        IF NEW.recorder_type<>'CryptoPayment' THEN RAISE EXCEPTION 'recognized payment recorder cannot be relabeled' USING ERRCODE='23514'; END IF;
        PERFORM fn_crypto_assert_payment_custody(target);
    ELSIF EXISTS(SELECT 1 FROM reg_crypto_payment_custody_bindings WHERE payment_id=NEW.recorder_id) THEN
        RAISE EXCEPTION 'recognized payment requires its exact custody generation' USING ERRCODE='23514';
    ELSIF NEW.recorder_type='CryptoPayment' THEN
        SELECT * INTO d FROM doc_crypto_payments WHERE id=NEW.recorder_id;
        IF FOUND AND fn_crypto_payment_requires_recognition(d.id,d.network_id,d.tx_hash,d.event_kind,d.event_index) THEN
            RAISE EXCEPTION 'indexed payment requires custody recognition' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NULL;
END;
$$;
DO $$
DECLARE target TEXT;
BEGIN
    FOREACH target IN ARRAY ARRAY['reg_crypto_balance_movements','reg_crypto_merchant_balance_movements','reg_crypto_fee_movements',
        'reg_crypto_platform_cost_movements','reg_crypto_provider_balance_movements'] LOOP
        EXECUTE format('CREATE CONSTRAINT TRIGGER %I AFTER INSERT ON %I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_payment_financial_complete()',
            'trg_payment_custody_complete_'||target,target);
    END LOOP;
END;
$$;

CREATE FUNCTION fn_crypto_payment_frozen_basis_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE b reg_crypto_payment_custody_bindings; p reg_crypto_custody_effect_postings; o reg_crypto_accounting_effect_occurrences; e reg_crypto_accounting_effects;
BEGIN
    SELECT * INTO b FROM reg_crypto_payment_custody_bindings WHERE payment_id=OLD.id ORDER BY created_at LIMIT 1;
    IF NOT FOUND THEN
        IF TG_OP='DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP='DELETE' THEN RAISE EXCEPTION 'recognized payment history is immutable' USING ERRCODE='23514'; END IF;
    SELECT * INTO STRICT p FROM reg_crypto_custody_effect_postings WHERE id=b.posting_id;
    SELECT * INTO STRICT o FROM reg_crypto_accounting_effect_occurrences WHERE id=p.occurrence_id;
    SELECT * INTO STRICT e FROM reg_crypto_accounting_effects WHERE id=p.effect_id;
    IF NEW.deletion_mark OR NEW._deleted_at IS NOT NULL
       OR (NEW.invoice_id,NEW.merchant_id,NEW.wallet_id,NEW.token_id,NEW.amount,NEW.fee_fixed,NEW.fee_percent_bp,NEW.fee_min,NEW.fee_max,NEW.date)
          IS DISTINCT FROM (b.invoice_id,b.merchant_id,b.wallet_id,b.token_id,b.gross,b.fee_fixed,b.fee_percent_bp,b.fee_min,b.fee_max,b.period)
       OR NEW.network_id<>e.network_id OR regexp_replace(lower(NEW.tx_hash),'^0x','')<>e.tx_hash OR NEW.event_kind<>e.event_kind
       OR (CASE WHEN e.family='evm' THEN lower(NEW.from_address) ELSE NEW.from_address END)<>o.source_address
       OR (OLD.posted AND NEW.posted AND OLD.posted_version=NEW.posted_version
            AND (OLD.block_number,OLD.block_hash) IS DISTINCT FROM (NEW.block_number,NEW.block_hash)) THEN
        RAISE EXCEPTION 'recognized payment basis and fee snapshot are frozen' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_crypto_payment_frozen_basis BEFORE UPDATE OR DELETE ON doc_crypto_payments
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_payment_frozen_basis_guard();

CREATE FUNCTION fn_crypto_payment_document_recognition_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE p reg_crypto_custody_effect_postings; required BOOLEAN; current_doc doc_crypto_payments;
BEGIN
    -- Lease/confirmation metadata on an unadopted legacy payment is not a
    -- financial transition. Do not make historical rows unusable at upgrade.
    IF TG_OP='UPDATE' AND NOT EXISTS(SELECT 1 FROM reg_crypto_payment_custody_bindings WHERE payment_id=NEW.id)
       AND (OLD.posted,OLD.posted_version,OLD.network_id,OLD.wallet_id,OLD.token_id,OLD.amount,OLD.tx_hash,OLD.event_kind,OLD.event_index,
            OLD.block_number,OLD.block_hash,OLD.merchant_id,OLD.invoice_id,OLD.from_address,OLD.date,OLD.fee_fixed,OLD.fee_percent_bp,OLD.fee_min,OLD.fee_max)
          IS NOT DISTINCT FROM
           (NEW.posted,NEW.posted_version,NEW.network_id,NEW.wallet_id,NEW.token_id,NEW.amount,NEW.tx_hash,NEW.event_kind,NEW.event_index,
            NEW.block_number,NEW.block_hash,NEW.merchant_id,NEW.invoice_id,NEW.from_address,NEW.date,NEW.fee_fixed,NEW.fee_percent_bp,NEW.fee_min,NEW.fee_max) THEN
        RETURN NULL;
    END IF;
    required:=fn_crypto_payment_requires_recognition(NEW.id,NEW.network_id,NEW.tx_hash,NEW.event_kind,NEW.event_index);
    IF TG_OP='UPDATE' THEN required:=required OR fn_crypto_payment_requires_recognition(OLD.id,OLD.network_id,OLD.tx_hash,OLD.event_kind,OLD.event_index); END IF;
    SELECT * INTO current_doc FROM doc_crypto_payments WHERE id=NEW.id;
    IF NOT FOUND THEN
        IF required AND EXISTS(SELECT 1 FROM reg_crypto_balance_movements WHERE recorder_id=NEW.id AND recorder_type='CryptoPayment') THEN
            RAISE EXCEPTION 'indexed payment principal requires its document' USING ERRCODE='23514';
        END IF;
        RETURN NULL;
    END IF;
    IF required THEN
        IF EXISTS(SELECT 1 FROM reg_crypto_balance_movements m WHERE m.recorder_id=NEW.id AND m.recorder_type='CryptoPayment' AND m.reversal_of_line_id IS NULL
            AND NOT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_recognitions r WHERE r.movement_line_id=m.line_id)
            AND m.amount>COALESCE((SELECT SUM(amount) FROM reg_crypto_balance_movements WHERE reversal_of_line_id=m.line_id),0)) THEN
            RAISE EXCEPTION 'indexed payment history requires explicit custody adoption' USING ERRCODE='23514';
        END IF;
        IF NOT current_doc.posted THEN
            FOR p IN SELECT * FROM reg_crypto_custody_effect_postings WHERE recorder_type='CryptoPayment' AND recorder_id=NEW.id LOOP
                IF NOT fn_crypto_payment_fully_compensated(p.id) THEN RAISE EXCEPTION 'payment unpost requires complete financial compensation' USING ERRCODE='23514'; END IF;
            END LOOP;
        END IF;
    END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER trg_crypto_payment_document_recognition AFTER INSERT OR UPDATE ON doc_crypto_payments
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_payment_document_recognition_complete();

-- Validate reversal dimensions and prohibit reversing a bound reversal. A sum
-- of same-direction or foreign-wallet rows must never authorize generation 2.
CREATE FUNCTION fn_crypto_bound_payment_reversal_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE original JSONB; parent UUID; bound BOOLEAN;
BEGIN
    IF NEW.reversal_of_line_id IS NULL THEN RETURN NEW; END IF;
    EXECUTE format('SELECT to_jsonb(m) FROM %I m WHERE line_id=$1',TG_TABLE_NAME) INTO original USING NEW.reversal_of_line_id;
    parent:=(original->>'reversal_of_line_id')::uuid;
    IF parent IS NOT NULL THEN
        EXECUTE format('SELECT to_jsonb(m) FROM %I m WHERE line_id=$1',TG_TABLE_NAME) INTO original USING parent;
    END IF;
    SELECT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings p WHERE p.recorder_type='CryptoPayment'
        AND p.recorder_id=(original->>'recorder_id')::uuid AND p.recorder_version=(original->>'recorder_version')::int) INTO bound;
    IF NOT bound THEN RETURN NEW; END IF;
    IF parent IS NOT NULL OR NEW.record_type=original->>'record_type' OR NEW.token_id<>(original->>'token_id')::uuid
       OR (TG_TABLE_NAME='reg_crypto_balance_movements' AND to_jsonb(NEW)->>'wallet_id' IS DISTINCT FROM original->>'wallet_id')
       OR (TG_TABLE_NAME<>'reg_crypto_balance_movements' AND to_jsonb(NEW)->>'merchant_id' IS DISTINCT FROM original->>'merchant_id')
       OR (TG_TABLE_NAME='reg_crypto_fee_movements' AND to_jsonb(NEW)->>'fee_type' IS DISTINCT FROM original->>'fee_type') THEN
        RAISE EXCEPTION 'bound payment reversal requires exact opposite source dimensions' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_bound_payment_reversal BEFORE INSERT ON reg_crypto_balance_movements FOR EACH ROW EXECUTE FUNCTION fn_crypto_bound_payment_reversal_guard();
CREATE TRIGGER trg_bound_payment_reversal BEFORE INSERT ON reg_crypto_merchant_balance_movements FOR EACH ROW EXECUTE FUNCTION fn_crypto_bound_payment_reversal_guard();
CREATE TRIGGER trg_bound_payment_reversal BEFORE INSERT ON reg_crypto_fee_movements FOR EACH ROW EXECUTE FUNCTION fn_crypto_bound_payment_reversal_guard();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM reg_crypto_payment_custody_bindings) OR EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings WHERE recorder_type='CryptoPayment' OR generation>1) THEN
        RAISE EXCEPTION 'payment recognition history requires forward migration';
    END IF;
END; $$;
DROP TRIGGER trg_crypto_payment_frozen_basis ON doc_crypto_payments;
DROP TRIGGER trg_crypto_payment_document_recognition ON doc_crypto_payments;
DROP TRIGGER trg_crypto_payment_posting_transaction ON reg_crypto_balance_movements;
DROP TRIGGER trg_bound_payment_reversal ON reg_crypto_balance_movements;
DROP TRIGGER trg_bound_payment_reversal ON reg_crypto_merchant_balance_movements;
DROP TRIGGER trg_bound_payment_reversal ON reg_crypto_fee_movements;
DROP TRIGGER trg_payment_custody_complete_reg_crypto_balance_movements ON reg_crypto_balance_movements;
DROP TRIGGER trg_payment_custody_complete_reg_crypto_merchant_balance_movements ON reg_crypto_merchant_balance_movements;
DROP TRIGGER trg_payment_custody_complete_reg_crypto_fee_movements ON reg_crypto_fee_movements;
DROP TRIGGER trg_payment_custody_complete_reg_crypto_platform_cost_movements ON reg_crypto_platform_cost_movements;
DROP TRIGGER trg_payment_custody_complete_reg_crypto_provider_balance_movements ON reg_crypto_provider_balance_movements;
DROP TRIGGER trg_crypto_custody_generation_complete ON reg_crypto_custody_effect_postings;
DROP TABLE reg_crypto_payment_custody_bindings;
DROP INDEX uq_crypto_payment_custody_version;
ALTER TABLE reg_crypto_balance_movements DROP COLUMN posting_txid;
ALTER TABLE reg_crypto_custody_effect_postings DROP CONSTRAINT ck_crypto_custody_generation,
    DROP COLUMN predecessor_id, ADD CONSTRAINT reg_crypto_custody_effect_postings_generation_check CHECK(generation=1);
DROP FUNCTION fn_crypto_bound_payment_reversal_guard();
DROP FUNCTION fn_crypto_payment_document_recognition_complete();
DROP FUNCTION fn_crypto_payment_frozen_basis_guard();
DROP FUNCTION fn_crypto_payment_financial_complete();
DROP FUNCTION fn_crypto_payment_binding_complete();
DROP FUNCTION fn_crypto_custody_generation_complete();
DROP FUNCTION fn_crypto_assert_payment_custody(UUID);
DROP FUNCTION fn_crypto_payment_fully_compensated(UUID);
DROP FUNCTION fn_crypto_payment_reversal_shape_valid(UUID);
DROP FUNCTION fn_crypto_payment_requires_recognition(UUID,UUID,TEXT,TEXT,BIGINT);
DROP FUNCTION fn_crypto_payment_posting_transaction();
-- +goose StatementEnd
