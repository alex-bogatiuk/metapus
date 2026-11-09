-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

CREATE OR REPLACE FUNCTION fn_crypto_payment_posting_transaction() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    NEW.posting_txid:=CASE WHEN NEW.recorder_type IN ('CryptoPayment','CryptoNativeFunding') AND NEW.reversal_of_line_id IS NULL THEN txid_current() ELSE NULL END;
    RETURN NEW;
END;
$$;

-- A known indexed native transaction also gates legacy payment aliases.
CREATE OR REPLACE FUNCTION fn_crypto_payment_requires_recognition(payment UUID,network UUID,hash TEXT,kind TEXT,event_index BIGINT)
RETURNS BOOLEAN LANGUAGE sql VOLATILE AS $$
    SELECT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings WHERE recorder_type='CryptoPayment' AND recorder_id=payment)
       OR EXISTS(SELECT 1 FROM reg_crypto_transfer_verifications v
          JOIN reg_crypto_accounting_effect_evidence x ON x.verification_id=v.id
          WHERE v.network_id=network AND regexp_replace(lower(v.tx_hash),'^0x','')=regexp_replace(lower(hash),'^0x','')
            AND (v.event_kind='native_value' OR (v.event_kind=kind AND v.event_index=$5)))
       OR EXISTS(SELECT 1 FROM doc_crypto_payments d JOIN reg_crypto_accounting_effect_evidence x ON x.verification_id=d.verification_id WHERE d.id=payment)
$$;

CREATE UNIQUE INDEX uq_crypto_native_funding_custody ON reg_crypto_custody_effect_postings(recorder_id)
    WHERE recorder_type='CryptoNativeFunding';
CREATE TABLE reg_crypto_native_funding_custody_bindings (
    posting_id UUID PRIMARY KEY REFERENCES reg_crypto_custody_effect_postings(id),
    funding_id UUID NOT NULL UNIQUE REFERENCES doc_crypto_native_fundings(id),
    period TIMESTAMPTZ NOT NULL,
    actor TEXT NOT NULL DEFAULT 'native-funding-settlement' CHECK(actor='native-funding-settlement'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TRIGGER trg_native_funding_binding_immutable BEFORE UPDATE OR DELETE ON reg_crypto_native_funding_custody_bindings
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_financial_movement_immutable();

CREATE FUNCTION fn_crypto_assert_native_funding_custody(target UUID) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE p reg_crypto_custody_effect_postings; b reg_crypto_native_funding_custody_bindings;
DECLARE f doc_crypto_native_fundings; s cat_wallets; d cat_wallets; chain reg_crypto_chain_settlements; fee reg_crypto_fee_reservations;
BEGIN
    SELECT * INTO STRICT p FROM reg_crypto_custody_effect_postings WHERE id=target;
    IF p.recorder_type<>'CryptoNativeFunding' THEN RETURN; END IF;
    SELECT * INTO b FROM reg_crypto_native_funding_custody_bindings WHERE posting_id=p.id;
    IF NOT FOUND THEN RAISE EXCEPTION 'native funding requires immutable custody binding' USING ERRCODE='23514'; END IF;
    SELECT * INTO STRICT f FROM doc_crypto_native_fundings WHERE id=b.funding_id;
    SELECT * INTO STRICT s FROM cat_wallets WHERE id=f.source_wallet_id;
    SELECT * INTO STRICT d FROM cat_wallets WHERE id=f.destination_wallet_id;
    SELECT * INTO fee FROM reg_crypto_fee_reservations WHERE id=f.fee_reservation_id;
    SELECT * INTO chain FROM reg_crypto_chain_settlements WHERE network_id=f.network_id AND tx_hash=f.tx_hash;
    IF f.status<>'settled' OR p.recorder_id<>f.id OR p.recorder_version<>1 OR p.generation<>1
       OR s.network_id<>f.network_id OR d.network_id<>f.network_id OR s.id=d.id
       OR s.vault_key_id='' OR d.vault_key_id='' OR s.custody_merchant_id IS DISTINCT FROM d.custody_merchant_id
       OR NOT EXISTS(SELECT 1 FROM cat_tokens WHERE id=f.native_token_id AND network_id=f.network_id AND token_standard='native')
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_accounting_effect_occurrences o JOIN reg_crypto_accounting_effects e ON e.id=o.effect_id
            WHERE o.id=p.occurrence_id AND e.network_id=f.network_id AND e.event_kind='native_value' AND e.effect_ordinal=0
              AND e.tx_hash=regexp_replace(lower(f.tx_hash),'^0x','') AND o.source_address=s.address_key
              AND o.destination_address=d.address_key AND o.contract_address='' AND o.amount=f.amount_minor)
       OR (SELECT count(*) FROM reg_crypto_custody_effect_recognitions WHERE posting_id=p.id)<>2
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_recognitions WHERE posting_id=p.id AND wallet_id=s.id AND token_id=f.native_token_id AND record_type='expense')
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_recognitions WHERE posting_id=p.id AND wallet_id=d.id AND token_id=f.native_token_id AND record_type='receipt')
       OR (SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=f.id AND recorder_type='CryptoNativeFunding')<>2
       OR EXISTS(SELECT 1 FROM reg_crypto_custody_effect_recognitions r JOIN reg_crypto_balance_movements m ON m.line_id=r.movement_line_id
            WHERE r.posting_id=p.id AND (m.period<>b.period OR m.amount<>f.amount_minor OR m.reversal_of_line_id IS NOT NULL))
       OR chain.id IS NULL OR chain.basis_type<>'CryptoNativeFunding' OR chain.basis_id<>f.id OR chain.status<>'succeeded'
       OR chain.native_token_id<>f.native_token_id
       OR chain.finality_status NOT IN ('solidified','finalized')
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_accounting_effect_occurrences WHERE id=p.occurrence_id AND block_hash=regexp_replace(lower(chain.block_hash),'^0x','') AND block_number=chain.block_height)
       OR fee.id IS NULL OR fee.status<>'settled' OR fee.basis_type<>'CryptoNativeFunding' OR fee.basis_id<>f.id
       OR fee.payer_wallet_id<>s.id OR fee.native_token_id<>f.native_token_id OR fee.network_id<>f.network_id
       OR fee.operation_token_id<>f.native_token_id OR fee.purpose<>'native_funding' OR fee.attempt<>1 OR fee.strategy<>'native_funding'
       OR fee.tx_hash<>f.tx_hash OR fee.reserved_principal_minor<>f.amount_minor OR fee.actual_fee_minor<>chain.total_fee_minor
       OR (SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=f.id AND recorder_type='ChainSettlement')<>(CASE WHEN chain.total_fee_minor>0 THEN 1 ELSE 0 END)
       OR EXISTS(SELECT 1 FROM reg_crypto_balance_movements WHERE recorder_id=f.id AND recorder_type='ChainSettlement'
            AND (record_type<>'expense' OR wallet_id<>s.id OR token_id<>f.native_token_id OR amount<>chain.total_fee_minor OR reversal_of_line_id IS NOT NULL))
       OR EXISTS(SELECT 1 FROM reg_crypto_balance_movements WHERE recorder_id=f.id AND recorder_type NOT IN ('CryptoNativeFunding','ChainSettlement'))
       OR EXISTS(SELECT 1 FROM reg_crypto_merchant_balance_movements WHERE recorder_id=f.id)
       OR EXISTS(SELECT 1 FROM reg_crypto_fee_movements WHERE recorder_id=f.id)
       OR EXISTS(SELECT 1 FROM reg_crypto_provider_balance_movements WHERE recorder_id=f.id)
       OR (SELECT count(*) FROM reg_crypto_platform_cost_movements WHERE recorder_id=f.id)<>(CASE WHEN chain.total_fee_minor>0 THEN 1 ELSE 0 END)
       OR EXISTS(SELECT 1 FROM reg_crypto_platform_cost_movements WHERE recorder_id=f.id AND
            (recorder_type<>'ChainSettlement' OR record_type<>'expense' OR network_id<>f.network_id
             OR operation_token_id<>f.native_token_id OR cost_token_id<>f.native_token_id OR cost_type<>'chain_network_fee'
             OR purpose<>'native_funding' OR provider<>'chain' OR external_order_id<>f.tx_hash OR amount<>chain.total_fee_minor OR reversal_of_line_id IS NOT NULL))
       OR EXISTS(SELECT 1 FROM doc_crypto_native_fundings other WHERE other.id<>f.id AND other.network_id=f.network_id
            AND regexp_replace(lower(other.tx_hash),'^0x','')=regexp_replace(lower(f.tx_hash),'^0x',''))
       OR EXISTS(SELECT 1 FROM doc_crypto_payments WHERE network_id=f.network_id AND wallet_id=f.destination_wallet_id AND token_id=f.native_token_id
            AND regexp_replace(lower(tx_hash),'^0x','')=regexp_replace(lower(f.tx_hash),'^0x',''))
       OR (SELECT count(*) FROM sys_outbox WHERE aggregate_type='CryptoNativeFunding' AND aggregate_id=f.id AND event_type='custody_recognized')<>1
       OR NOT EXISTS(SELECT 1 FROM sys_outbox WHERE aggregate_type='CryptoNativeFunding' AND aggregate_id=f.id AND event_type='custody_recognized'
            AND payload->>'postingId'=p.id::text AND payload->>'entityId'=f.id::text AND payload->>'entityType'='CryptoNativeFunding'
            AND payload->>'action'='custody_recognized' AND payload->>'generation'='1') THEN
        RAISE EXCEPTION 'native funding requires complete proven principal and fee settlement' USING ERRCODE='23514';
    END IF;
END;
$$;
CREATE FUNCTION fn_crypto_native_funding_custody_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target UUID;
BEGIN
    IF TG_TABLE_NAME='reg_crypto_custody_effect_postings' THEN target:=NEW.id;
    ELSE
        target:=NEW.posting_id;
        IF NOT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings WHERE id=target AND recorder_type='CryptoNativeFunding' AND recorder_id=NEW.funding_id) THEN
            RAISE EXCEPTION 'native funding binding requires its exact posting owner' USING ERRCODE='23514';
        END IF;
        IF (SELECT count(*) FROM reg_crypto_custody_effect_recognitions r JOIN reg_crypto_balance_movements m ON m.line_id=r.movement_line_id
            WHERE r.posting_id=target AND m.posting_txid=txid_current())<>2 THEN
            RAISE EXCEPTION 'native funding admission requires both new principal lines in the current transaction' USING ERRCODE='23514';
        END IF;
    END IF;
    PERFORM fn_crypto_assert_native_funding_custody(target);
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER trg_native_funding_posting_complete AFTER INSERT ON reg_crypto_custody_effect_postings
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_native_funding_custody_complete();
CREATE CONSTRAINT TRIGGER trg_native_funding_binding_complete AFTER INSERT ON reg_crypto_native_funding_custody_bindings
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_native_funding_custody_complete();

-- FeeBudgetManager cannot release a successful native principal commitment in
-- a separate transaction, including a zero-fee receipt with no fee movement.
CREATE FUNCTION fn_crypto_native_funding_settlement_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target UUID;
BEGIN
    IF NEW.basis_type<>'CryptoNativeFunding' THEN RETURN NULL; END IF;
    IF TG_TABLE_NAME='reg_crypto_chain_settlements' THEN
        IF NEW.status<>'succeeded' THEN RETURN NULL; END IF;
    ELSE
        IF NEW.status<>'settled' OR NOT EXISTS(SELECT 1 FROM reg_crypto_chain_settlements
            WHERE basis_type='CryptoNativeFunding' AND basis_id=NEW.basis_id AND status='succeeded') THEN RETURN NULL; END IF;
    END IF;
    SELECT posting_id INTO target FROM reg_crypto_native_funding_custody_bindings WHERE funding_id=NEW.basis_id;
    IF NOT FOUND THEN RAISE EXCEPTION 'successful native funding fee requires atomic principal recognition' USING ERRCODE='23514'; END IF;
    PERFORM fn_crypto_assert_native_funding_custody(target);
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER trg_native_funding_chain_complete AFTER INSERT ON reg_crypto_chain_settlements
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_native_funding_settlement_complete();
CREATE CONSTRAINT TRIGGER trg_native_funding_fee_complete AFTER INSERT OR UPDATE ON reg_crypto_fee_reservations
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_native_funding_settlement_complete();

CREATE FUNCTION fn_crypto_native_funding_movement_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target UUID;
BEGIN
    SELECT posting_id INTO target FROM reg_crypto_native_funding_custody_bindings WHERE funding_id=NEW.recorder_id;
    IF FOUND THEN PERFORM fn_crypto_assert_native_funding_custody(target);
    ELSIF NEW.recorder_type='CryptoNativeFunding' THEN
        RAISE EXCEPTION 'native funding principal requires custody recognition' USING ERRCODE='23514';
    END IF;
    IF NEW.reversal_of_line_id IS NOT NULL AND EXISTS(SELECT 1 FROM reg_crypto_balance_movements m
        JOIN reg_crypto_native_funding_custody_bindings b ON b.funding_id=m.recorder_id WHERE m.line_id=NEW.reversal_of_line_id) THEN
        RAISE EXCEPTION 'recognized native funding requires explicit compensation workflow' USING ERRCODE='23514';
    END IF;
    RETURN NULL;
END;
$$;
DO $$ DECLARE target TEXT; BEGIN
    FOREACH target IN ARRAY ARRAY['reg_crypto_balance_movements','reg_crypto_merchant_balance_movements','reg_crypto_fee_movements',
        'reg_crypto_platform_cost_movements','reg_crypto_provider_balance_movements'] LOOP
        EXECUTE format('CREATE CONSTRAINT TRIGGER %I AFTER INSERT ON %I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_native_funding_movement_complete()',
            'trg_native_funding_complete_'||target,target);
    END LOOP;
END; $$;

CREATE FUNCTION fn_crypto_native_funding_basis_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        IF EXISTS(SELECT 1 FROM reg_crypto_native_funding_custody_bindings WHERE funding_id=OLD.id) THEN
            RAISE EXCEPTION 'recognized native funding is immutable' USING ERRCODE='23514';
        END IF;
        RETURN OLD;
    END IF;
    IF (OLD.id,OLD.basis_type,OLD.basis_id,OLD.attempt,OLD.network_id,OLD.native_token_id,OLD.source_wallet_id,OLD.destination_wallet_id,OLD.amount_minor)
       IS DISTINCT FROM (NEW.id,NEW.basis_type,NEW.basis_id,NEW.attempt,NEW.network_id,NEW.native_token_id,NEW.source_wallet_id,NEW.destination_wallet_id,NEW.amount_minor)
       OR (OLD.tx_hash<>'' AND OLD.tx_hash IS DISTINCT FROM NEW.tx_hash)
       OR (OLD.fee_reservation_id IS NOT NULL AND OLD.fee_reservation_id IS DISTINCT FROM NEW.fee_reservation_id)
       OR (EXISTS(SELECT 1 FROM reg_crypto_native_funding_custody_bindings WHERE funding_id=OLD.id)
            AND (NEW.status<>'settled' OR to_jsonb(NEW)->>'_deleted_at' IS NOT NULL)) THEN
        RAISE EXCEPTION 'native funding basis is frozen' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_native_funding_basis BEFORE UPDATE OR DELETE ON doc_crypto_native_fundings
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_native_funding_basis_guard();
CREATE FUNCTION fn_crypto_native_funding_document_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target UUID;
BEGIN
    IF NEW.status='settled' AND (TG_OP='INSERT' OR OLD.status IS DISTINCT FROM NEW.status) THEN
        SELECT posting_id INTO target FROM reg_crypto_native_funding_custody_bindings WHERE funding_id=NEW.id;
        IF NOT FOUND THEN RAISE EXCEPTION 'native funding settlement requires custody recognition' USING ERRCODE='23514'; END IF;
        PERFORM fn_crypto_assert_native_funding_custody(target);
    END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER trg_native_funding_document_complete AFTER INSERT OR UPDATE ON doc_crypto_native_fundings
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_native_funding_document_complete();

-- An unindexed payment alias cannot bypass ownership of a proven internal tx.
CREATE FUNCTION fn_crypto_payment_native_funding_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    PERFORM fn_crypto_lock_receipt_balance(NEW.wallet_id,NEW.token_id);
    IF EXISTS(SELECT 1 FROM doc_crypto_native_fundings f JOIN reg_crypto_native_funding_custody_bindings b ON b.funding_id=f.id
        WHERE f.network_id=NEW.network_id AND f.destination_wallet_id=NEW.wallet_id AND f.native_token_id=NEW.token_id
          AND regexp_replace(lower(f.tx_hash),'^0x','')=regexp_replace(lower(NEW.tx_hash),'^0x','')) THEN
        RAISE EXCEPTION 'internal native transfer belongs to funding settlement' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_payment_native_funding BEFORE INSERT OR UPDATE ON doc_crypto_payments
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_payment_native_funding_guard();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN IF EXISTS(SELECT 1 FROM reg_crypto_native_funding_custody_bindings)
    OR EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings WHERE recorder_type='CryptoNativeFunding') THEN
    RAISE EXCEPTION 'native funding recognition history requires forward migration'; END IF; END; $$;
DROP TRIGGER trg_payment_native_funding ON doc_crypto_payments;
DROP TRIGGER trg_native_funding_document_complete ON doc_crypto_native_fundings;
DROP TRIGGER trg_native_funding_basis ON doc_crypto_native_fundings;
DROP TRIGGER trg_native_funding_posting_complete ON reg_crypto_custody_effect_postings;
DROP TRIGGER trg_native_funding_chain_complete ON reg_crypto_chain_settlements;
DROP TRIGGER trg_native_funding_fee_complete ON reg_crypto_fee_reservations;
DROP TRIGGER trg_native_funding_complete_reg_crypto_balance_movements ON reg_crypto_balance_movements;
DROP TRIGGER trg_native_funding_complete_reg_crypto_merchant_balance_movements ON reg_crypto_merchant_balance_movements;
DROP TRIGGER trg_native_funding_complete_reg_crypto_fee_movements ON reg_crypto_fee_movements;
DROP TRIGGER trg_native_funding_complete_reg_crypto_platform_cost_movements ON reg_crypto_platform_cost_movements;
DROP TRIGGER trg_native_funding_complete_reg_crypto_provider_balance_movements ON reg_crypto_provider_balance_movements;
DROP TABLE reg_crypto_native_funding_custody_bindings;
DROP INDEX uq_crypto_native_funding_custody;
CREATE OR REPLACE FUNCTION fn_crypto_payment_posting_transaction() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    NEW.posting_txid:=CASE WHEN NEW.recorder_type='CryptoPayment' AND NEW.reversal_of_line_id IS NULL THEN txid_current() ELSE NULL END;
    RETURN NEW;
END;
$$;
CREATE OR REPLACE FUNCTION fn_crypto_payment_requires_recognition(payment UUID,network UUID,hash TEXT,kind TEXT,event_index BIGINT)
RETURNS BOOLEAN LANGUAGE sql VOLATILE AS $$
    SELECT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings WHERE recorder_type='CryptoPayment' AND recorder_id=payment)
       OR EXISTS(SELECT 1 FROM reg_crypto_transfer_verifications v
          JOIN reg_crypto_accounting_effect_evidence x ON x.verification_id=v.id
          WHERE v.network_id=network AND regexp_replace(lower(v.tx_hash),'^0x','')=regexp_replace(lower(hash),'^0x','')
            AND v.event_kind=kind AND v.event_index=$5)
       OR EXISTS(SELECT 1 FROM doc_crypto_payments d JOIN reg_crypto_accounting_effect_evidence x ON x.verification_id=d.verification_id WHERE d.id=payment)
$$;
DROP FUNCTION fn_crypto_payment_native_funding_guard();
DROP FUNCTION fn_crypto_native_funding_document_complete();
DROP FUNCTION fn_crypto_native_funding_basis_guard();
DROP FUNCTION fn_crypto_native_funding_movement_complete();
DROP FUNCTION fn_crypto_native_funding_custody_complete();
DROP FUNCTION fn_crypto_native_funding_settlement_complete();
DROP FUNCTION fn_crypto_assert_native_funding_custody(UUID);
-- +goose StatementEnd
