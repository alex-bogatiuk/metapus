-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
CREATE OR REPLACE FUNCTION fn_crypto_payment_requires_recognition(payment UUID,network UUID,hash TEXT,kind TEXT,event_index BIGINT)
RETURNS BOOLEAN LANGUAGE sql VOLATILE AS $$
    SELECT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings WHERE recorder_type='CryptoPayment' AND recorder_id=payment)
       OR EXISTS(SELECT 1 FROM reg_crypto_transfer_verifications v
          JOIN reg_crypto_accounting_effect_evidence x ON x.verification_id=v.id
          WHERE v.network_id=network AND regexp_replace(lower(v.tx_hash),'^0x','')=regexp_replace(lower(hash),'^0x','')
            )
       OR EXISTS(SELECT 1 FROM doc_crypto_payments d JOIN reg_crypto_accounting_effect_evidence x ON x.verification_id=d.verification_id WHERE d.id=payment)
$$;

ALTER TABLE doc_crypto_sweeps ADD COLUMN accounting_model_version INT NOT NULL DEFAULT 1 CHECK(accounting_model_version IN (1,2));
ALTER TABLE doc_crypto_sweeps ALTER COLUMN accounting_model_version SET DEFAULT 2;
CREATE TABLE reg_crypto_sweep_custody_bindings (
    posting_id UUID PRIMARY KEY REFERENCES reg_crypto_custody_effect_postings(id),
    sweep_id UUID NOT NULL REFERENCES doc_crypto_sweeps(id),
    line_id UUID NOT NULL UNIQUE REFERENCES doc_crypto_sweep_lines(line_id),
    period TIMESTAMPTZ NOT NULL,
    actor TEXT NOT NULL DEFAULT 'sweep-settlement' CHECK(actor='sweep-settlement'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_sweep_custody_bindings_document ON reg_crypto_sweep_custody_bindings(sweep_id);
CREATE TRIGGER trg_sweep_custody_binding_immutable BEFORE UPDATE OR DELETE ON reg_crypto_sweep_custody_bindings
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_financial_movement_immutable();
CREATE OR REPLACE FUNCTION fn_crypto_payment_posting_transaction() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    NEW.posting_txid:=CASE WHEN NEW.recorder_type IN ('CryptoPayment','CryptoNativeFunding','CryptoSweep') AND NEW.reversal_of_line_id IS NULL THEN txid_current() ELSE NULL END;
    RETURN NEW;
END;
$$;

CREATE FUNCTION fn_crypto_assert_sweep_line(target UUID) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE l doc_crypto_sweep_lines; s doc_crypto_sweeps; b reg_crypto_sweep_custody_bindings; p reg_crypto_custody_effect_postings;
DECLARE src cat_wallets; dst cat_wallets; f reg_crypto_fee_reservations; c reg_crypto_chain_settlements;
BEGIN
    SELECT * INTO STRICT l FROM doc_crypto_sweep_lines WHERE line_id=target;
    SELECT * INTO STRICT s FROM doc_crypto_sweeps WHERE id=l.document_id;
    IF s.accounting_model_version<>2 THEN RETURN; END IF;
    SELECT * INTO STRICT src FROM cat_wallets WHERE id=l.wallet_id;
    SELECT * INTO STRICT dst FROM cat_wallets WHERE id=s.hot_wallet_id;
    SELECT * INTO f FROM reg_crypto_fee_reservations WHERE basis_type='CryptoSweepAttempt' AND basis_id=l.line_id AND purpose='sweep' AND attempt=1;
    SELECT * INTO c FROM reg_crypto_chain_settlements WHERE network_id=src.network_id AND tx_hash=l.tx_hash;
    SELECT * INTO b FROM reg_crypto_sweep_custody_bindings WHERE line_id=l.line_id;
    IF l.settlement_status<>'recorded' OR c.id IS NULL OR f.id IS NULL
       OR c.basis_type<>'CryptoSweepAttempt' OR c.basis_id<>l.line_id OR c.finality_status NOT IN ('solidified','finalized')
       OR f.status<>'settled' OR f.payer_wallet_id<>src.id OR f.operation_token_id<>s.token_id OR f.network_id<>src.network_id
       OR f.tx_hash<>l.tx_hash OR f.actual_fee_minor<>c.total_fee_minor OR f.native_token_id<>c.native_token_id
       OR src.network_id<>dst.network_id OR src.id=dst.id OR src.vault_key_id='' OR dst.vault_key_id=''
       OR src.wallet_role<>'merchant_pool' OR dst.wallet_role<>'liquidity_hot'
       OR src.custody_merchant_id IS DISTINCT FROM dst.custody_merchant_id
       OR NOT EXISTS(SELECT 1 FROM cat_tokens WHERE id=s.token_id AND network_id=src.network_id)
       OR f.reserved_principal_minor<>(CASE WHEN s.token_id=c.native_token_id THEN l.amount ELSE 0 END)
       OR l.network_fee<>c.total_fee_minor OR l.network_fee_token_id IS DISTINCT FROM c.native_token_id
       OR (SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=l.line_id AND recorder_type='ChainSettlement')<>(CASE WHEN c.total_fee_minor>0 THEN 1 ELSE 0 END)
       OR EXISTS(SELECT 1 FROM reg_crypto_balance_movements WHERE recorder_id=l.line_id AND
           (recorder_type<>'ChainSettlement' OR recorder_version<>1 OR record_type<>'expense' OR wallet_id<>src.id OR token_id<>c.native_token_id OR amount<>c.total_fee_minor OR reversal_of_line_id IS NOT NULL))
       OR (SELECT count(*) FROM reg_crypto_platform_cost_movements WHERE recorder_id=l.line_id)<>(CASE WHEN c.total_fee_minor>0 THEN 1 ELSE 0 END)
       OR EXISTS(SELECT 1 FROM reg_crypto_platform_cost_movements WHERE recorder_id=l.line_id AND
           (recorder_type<>'ChainSettlement' OR record_type<>'expense' OR network_id<>src.network_id OR operation_token_id<>s.token_id
            OR cost_token_id<>c.native_token_id OR cost_type<>'chain_network_fee' OR purpose<>'sweep' OR provider<>'chain' OR external_order_id<>l.tx_hash OR amount<>c.total_fee_minor OR reversal_of_line_id IS NOT NULL))
       OR EXISTS(SELECT 1 FROM reg_crypto_merchant_balance_movements WHERE recorder_id IN (s.id,l.line_id))
       OR EXISTS(SELECT 1 FROM reg_crypto_fee_movements WHERE recorder_id IN (s.id,l.line_id))
       OR EXISTS(SELECT 1 FROM reg_crypto_provider_balance_movements WHERE recorder_id IN (s.id,l.line_id)) THEN
        RAISE EXCEPTION 'sweep requires exact final line, principal and fee set' USING ERRCODE='23514';
    END IF;
    IF c.status='reverted' THEN
        IF b.posting_id IS NOT NULL OR l.chain_result='SUCCESS' OR l.confirmed THEN
            RAISE EXCEPTION 'reverted sweep cannot recognize principal' USING ERRCODE='23514';
        END IF;
        RETURN;
    END IF;
    SELECT * INTO p FROM reg_crypto_custody_effect_postings WHERE id=b.posting_id;
    IF c.status<>'succeeded' OR b.posting_id IS NULL OR b.sweep_id<>s.id OR p.recorder_id<>s.id OR p.recorder_type<>'CryptoSweep'
       OR p.recorder_version<>1 OR p.generation<>1 OR NOT l.confirmed OR l.chain_result<>'SUCCESS'
       OR (SELECT count(*) FROM reg_crypto_custody_effect_recognitions WHERE posting_id=p.id)<>2
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_recognitions WHERE posting_id=p.id AND wallet_id=src.id AND token_id=s.token_id AND record_type='expense')
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_recognitions WHERE posting_id=p.id AND wallet_id=dst.id AND token_id=s.token_id AND record_type='receipt')
       OR EXISTS(SELECT 1 FROM reg_crypto_custody_effect_recognitions r JOIN reg_crypto_balance_movements m ON m.line_id=r.movement_line_id
           WHERE r.posting_id=p.id AND (m.amount<>l.amount OR m.period<>b.period OR m.reversal_of_line_id IS NOT NULL))
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_accounting_effect_occurrences o JOIN reg_crypto_accounting_effects e ON e.id=o.effect_id
           WHERE o.id=p.occurrence_id AND e.network_id=src.network_id AND e.tx_hash=regexp_replace(lower(l.tx_hash),'^0x','')
             AND o.source_address=src.address_key AND o.destination_address=dst.address_key AND o.amount=l.amount
             AND o.block_hash=regexp_replace(lower(c.block_hash),'^0x','') AND o.block_number=c.block_height)
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_expected_chain_effects x JOIN reg_crypto_signing_authorizations a ON a.id=x.signing_authorization_id
           WHERE a.basis_type='CryptoSweepAttempt' AND a.basis_id=l.line_id AND a.signed_envelope IS NOT NULL
             AND x.network_id=src.network_id AND regexp_replace(lower(x.tx_hash),'^0x','')=regexp_replace(lower(l.tx_hash),'^0x','')
             AND x.source_address_key=src.address_key AND x.destination_address_key=dst.address_key AND x.token_id=s.token_id AND x.amount=l.amount)
       OR (SELECT count(*) FROM sys_outbox WHERE aggregate_type='CryptoSweep' AND aggregate_id=s.id AND event_type='custody_recognized' AND payload->>'lineId'=l.line_id::text)<>1
       OR NOT EXISTS(SELECT 1 FROM sys_outbox WHERE aggregate_type='CryptoSweep' AND aggregate_id=s.id AND event_type='custody_recognized'
           AND payload->>'lineId'=l.line_id::text AND payload->>'postingId'=p.id::text AND payload->>'entityId'=s.id::text AND payload->>'entityType'='CryptoSweep' AND payload->>'action'='custody_recognized' AND payload->>'generation'='1') THEN
        RAISE EXCEPTION 'successful sweep requires one proven principal owner per line' USING ERRCODE='23514';
    END IF;
END;
$$;

CREATE FUNCTION fn_crypto_sweep_binding_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings WHERE id=NEW.posting_id AND recorder_type='CryptoSweep' AND recorder_id=NEW.sweep_id)
       OR (SELECT count(*) FROM reg_crypto_custody_effect_recognitions r JOIN reg_crypto_balance_movements m ON m.line_id=r.movement_line_id
           WHERE r.posting_id=NEW.posting_id AND m.posting_txid=txid_current())<>2 THEN
        RAISE EXCEPTION 'sweep requires both new principal lines in the current transaction' USING ERRCODE='23514';
    END IF;
    PERFORM fn_crypto_assert_sweep_line(NEW.line_id);
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER trg_sweep_binding_complete AFTER INSERT ON reg_crypto_sweep_custody_bindings
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_sweep_binding_complete();
CREATE FUNCTION fn_crypto_sweep_posting_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target UUID;
BEGIN
    IF NEW.recorder_type<>'CryptoSweep' THEN RETURN NULL; END IF;
    SELECT line_id INTO target FROM reg_crypto_sweep_custody_bindings WHERE posting_id=NEW.id;
    IF NOT FOUND THEN RAISE EXCEPTION 'sweep principal requires its line binding' USING ERRCODE='23514'; END IF;
    PERFORM fn_crypto_assert_sweep_line(target);
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER trg_sweep_posting_complete AFTER INSERT ON reg_crypto_custody_effect_postings
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_sweep_posting_complete();

CREATE FUNCTION fn_crypto_sweep_financial_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target UUID;
BEGIN
    IF TG_TABLE_NAME IN ('reg_crypto_merchant_balance_movements','reg_crypto_fee_movements','reg_crypto_provider_balance_movements')
       AND EXISTS(SELECT 1 FROM doc_crypto_sweeps WHERE id=NEW.recorder_id AND accounting_model_version=2) THEN
        RAISE EXCEPTION 'sweep cannot create merchant credit, revenue or provider principal' USING ERRCODE='23514';
    END IF;
    IF TG_TABLE_NAME='reg_crypto_balance_movements'
       AND EXISTS(SELECT 1 FROM doc_crypto_sweeps WHERE id=NEW.recorder_id AND accounting_model_version=2) THEN
        IF NEW.recorder_type<>'CryptoSweep' THEN RAISE EXCEPTION 'sweep principal cannot relabel its recorder' USING ERRCODE='23514'; END IF;
        SELECT b.line_id INTO target FROM reg_crypto_custody_effect_recognitions r
            JOIN reg_crypto_sweep_custody_bindings b ON b.posting_id=r.posting_id WHERE r.movement_line_id=NEW.line_id AND b.sweep_id=NEW.recorder_id;
        IF NOT FOUND THEN RAISE EXCEPTION 'sweep principal cannot be posted at broadcast' USING ERRCODE='23514'; END IF;
        PERFORM fn_crypto_assert_sweep_line(target);
    END IF;
    SELECT l.line_id INTO target FROM doc_crypto_sweep_lines l JOIN doc_crypto_sweeps s ON s.id=l.document_id
        WHERE l.line_id=NEW.recorder_id AND s.accounting_model_version=2;
    IF FOUND THEN PERFORM fn_crypto_assert_sweep_line(target); END IF;
    IF NEW.reversal_of_line_id IS NOT NULL AND EXISTS(SELECT 1 FROM reg_crypto_custody_effect_recognitions r
        JOIN reg_crypto_sweep_custody_bindings b ON b.posting_id=r.posting_id WHERE r.movement_line_id=NEW.reversal_of_line_id) THEN
        RAISE EXCEPTION 'recognized sweep requires explicit compensation workflow' USING ERRCODE='23514';
    END IF;
    RETURN NULL;
END;
$$;
DO $$ DECLARE target TEXT; BEGIN
    FOREACH target IN ARRAY ARRAY['reg_crypto_balance_movements','reg_crypto_merchant_balance_movements','reg_crypto_fee_movements',
        'reg_crypto_platform_cost_movements','reg_crypto_provider_balance_movements'] LOOP
        EXECUTE format('CREATE CONSTRAINT TRIGGER %I AFTER INSERT ON %I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_sweep_financial_complete()',
            'trg_sweep_complete_'||target,target);
    END LOOP;
END; $$;

CREATE FUNCTION fn_crypto_sweep_settlement_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target UUID;
BEGIN
    IF TG_TABLE_NAME='doc_crypto_sweep_lines' THEN
        IF NEW.settlement_status<>'recorded' THEN RETURN NULL; END IF;
        target:=NEW.line_id;
    ELSE
        IF NEW.basis_type<>'CryptoSweepAttempt' THEN RETURN NULL; END IF;
        IF TG_TABLE_NAME='reg_crypto_fee_reservations' AND NEW.status<>'settled' THEN RETURN NULL; END IF;
        target:=NEW.basis_id;
    END IF;
    IF EXISTS(SELECT 1 FROM doc_crypto_sweep_lines l JOIN doc_crypto_sweeps s ON s.id=l.document_id WHERE l.line_id=target AND s.accounting_model_version=2) THEN
        PERFORM fn_crypto_assert_sweep_line(target);
    END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER trg_sweep_line_complete AFTER INSERT OR UPDATE ON doc_crypto_sweep_lines
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_sweep_settlement_complete();
CREATE CONSTRAINT TRIGGER trg_sweep_chain_complete AFTER INSERT ON reg_crypto_chain_settlements
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_sweep_settlement_complete();
CREATE CONSTRAINT TRIGGER trg_sweep_fee_complete AFTER INSERT OR UPDATE ON reg_crypto_fee_reservations
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_sweep_settlement_complete();

CREATE FUNCTION fn_crypto_sweep_basis_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE model INT;
BEGIN
    IF TG_TABLE_NAME='doc_crypto_sweeps' THEN
        IF TG_OP='DELETE' THEN
            IF OLD.accounting_model_version=2 AND EXISTS(SELECT 1 FROM doc_crypto_sweep_lines l WHERE l.document_id=OLD.id AND
                (l.tx_hash<>'' OR EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations a WHERE a.basis_type='CryptoSweepAttempt' AND a.basis_id=l.line_id))) THEN
                RAISE EXCEPTION 'broadcast sweep history is immutable' USING ERRCODE='23514'; END IF;
            RETURN OLD;
        END IF;
        IF NEW.accounting_model_version<>OLD.accounting_model_version THEN RAISE EXCEPTION 'sweep accounting model is immutable' USING ERRCODE='23514'; END IF;
        IF OLD.accounting_model_version<>2 THEN RETURN NEW; END IF;
        IF EXISTS(SELECT 1 FROM doc_crypto_sweep_lines l WHERE l.document_id=OLD.id AND
            (l.tx_hash<>'' OR EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations a WHERE a.basis_type='CryptoSweepAttempt' AND a.basis_id=l.line_id))) AND
           ((NEW.token_id,NEW.hot_wallet_id,NEW.date) IS DISTINCT FROM (OLD.token_id,OLD.hot_wallet_id,OLD.date) OR NEW.deletion_mark OR NEW._deleted_at IS NOT NULL
            OR NEW.posted_version>1 OR (OLD.posted AND NOT NEW.posted)) THEN
            RAISE EXCEPTION 'sweep basis is frozen after signing starts' USING ERRCODE='23514';
        END IF;
        NEW.total_amount:=COALESCE((SELECT SUM(amount) FROM doc_crypto_sweep_lines WHERE document_id=NEW.id),NEW.total_amount);
        NEW.total_fee:=COALESCE((SELECT SUM(network_fee) FROM doc_crypto_sweep_lines WHERE document_id=NEW.id),0);
        IF EXISTS(SELECT 1 FROM doc_crypto_sweep_lines WHERE document_id=NEW.id AND settlement_status='recorded' AND chain_result<>'SUCCESS') THEN NEW.status:='partial_failed';
        ELSIF EXISTS(SELECT 1 FROM doc_crypto_sweep_lines WHERE document_id=NEW.id)
           AND NOT EXISTS(SELECT 1 FROM doc_crypto_sweep_lines WHERE document_id=NEW.id AND settlement_status<>'recorded') THEN NEW.status:='confirmed'; END IF;
        RETURN NEW;
    END IF;
    SELECT accounting_model_version INTO model FROM doc_crypto_sweeps WHERE id=OLD.document_id;
    IF model<>2 THEN
        IF TG_OP='DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP='DELETE' THEN
        IF OLD.tx_hash<>'' OR EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations WHERE basis_type='CryptoSweepAttempt' AND basis_id=OLD.line_id) THEN
            RAISE EXCEPTION 'signed sweep line is immutable' USING ERRCODE='23514'; END IF;
        RETURN OLD;
    END IF;
    IF (NEW.line_id,NEW.document_id,NEW.wallet_id) IS DISTINCT FROM (OLD.line_id,OLD.document_id,OLD.wallet_id)
       OR (OLD.tx_hash<>'' AND NEW.amount<>OLD.amount)
       OR (NEW.amount<>OLD.amount AND EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations WHERE basis_type='CryptoSweepAttempt' AND basis_id=OLD.line_id)
           AND NOT EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations WHERE basis_type='CryptoSweepAttempt' AND basis_id=OLD.line_id
               AND signed_envelope IS NOT NULL AND amount_minor_units=NEW.amount AND tx_hash=NEW.tx_hash))
       OR (OLD.settlement_status='recorded' AND (NEW.tx_hash,NEW.settlement_status,NEW.chain_result,NEW.confirmed,NEW.network_fee,NEW.network_fee_token_id)
            IS DISTINCT FROM (OLD.tx_hash,OLD.settlement_status,OLD.chain_result,OLD.confirmed,OLD.network_fee,OLD.network_fee_token_id)) THEN
        RAISE EXCEPTION 'sweep line basis and final outcome are immutable' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_sweep_header_basis BEFORE UPDATE OR DELETE ON doc_crypto_sweeps FOR EACH ROW EXECUTE FUNCTION fn_crypto_sweep_basis_guard();
CREATE TRIGGER trg_sweep_line_basis BEFORE UPDATE OR DELETE ON doc_crypto_sweep_lines FOR EACH ROW EXECUTE FUNCTION fn_crypto_sweep_basis_guard();

CREATE OR REPLACE VIEW crypto_aml_allocatable_transfers AS
 SELECT plan.id allocation_id,plan.merchant_id,plan.network_id,plan.token_id,plan.source_wallet_id,plan.destination_wallet_id,
        plan.principal,plan.service_fee,e.id effect_id,spent.line_id source_movement_id,received.line_id destination_movement_id
 FROM reg_crypto_aml_internal_allocations plan
 JOIN doc_crypto_sweep_lines line ON line.line_id=plan.sweep_attempt_id AND line.wallet_id=plan.source_wallet_id AND line.amount=plan.gross AND line.tx_hash<>''
 JOIN doc_crypto_sweeps sweep ON sweep.id=line.document_id AND sweep.posted AND sweep.hot_wallet_id=plan.destination_wallet_id AND sweep.token_id=plan.token_id
 JOIN reg_crypto_aml_effects e ON e.wallet_id=plan.destination_wallet_id AND e.source_wallet_id=plan.source_wallet_id AND e.token_id=plan.token_id AND e.network_id=plan.network_id AND e.tx_hash=line.tx_hash AND e.amount=plan.gross
 JOIN reg_crypto_aml_effect_chain_states cs ON cs.effect_id=e.id AND cs.canonicality='canonical'
 JOIN reg_crypto_expected_chain_effects expected ON expected.id=e.expected_effect_id
 JOIN reg_crypto_signing_authorizations auth ON auth.id=expected.signing_authorization_id AND auth.basis_type='CryptoSweepAttempt' AND auth.basis_id=plan.sweep_attempt_id AND auth.signed_envelope IS NOT NULL
 JOIN reg_crypto_balance_movements spent ON spent.recorder_id=sweep.id AND spent.recorder_type='CryptoSweep' AND spent.recorder_version=sweep.posted_version
    AND spent.wallet_id=plan.source_wallet_id AND spent.token_id=plan.token_id AND spent.record_type='expense' AND spent.amount=plan.gross
 JOIN reg_crypto_balance_movements received ON received.recorder_id=sweep.id AND received.recorder_type='CryptoSweep' AND received.recorder_version=sweep.posted_version
    AND received.wallet_id=plan.destination_wallet_id AND received.token_id=plan.token_id AND received.record_type='receipt' AND received.amount=sweep.total_amount
 WHERE plan.status IN ('reserved','applied')
   AND sweep.total_amount=(SELECT SUM(amount) FROM doc_crypto_sweep_lines WHERE document_id=sweep.id AND tx_hash<>'')
   AND spent.created_at>=(SELECT created_at FROM reg_crypto_aml_wallet_baselines WHERE wallet_id=plan.source_wallet_id AND token_id=plan.token_id)
   AND received.created_at>=(SELECT created_at FROM reg_crypto_aml_wallet_baselines WHERE wallet_id=plan.destination_wallet_id AND token_id=plan.token_id)
   AND (SELECT COUNT(*) FROM reg_crypto_balance_movements m WHERE m.recorder_id=sweep.id AND m.recorder_type='CryptoSweep' AND m.recorder_version=sweep.posted_version AND m.wallet_id=plan.source_wallet_id AND m.token_id=plan.token_id AND m.record_type='expense')=1
   AND (SELECT COUNT(*) FROM reg_crypto_balance_movements m WHERE m.recorder_id=sweep.id AND m.recorder_type='CryptoSweep' AND m.recorder_version=sweep.posted_version AND m.wallet_id=plan.destination_wallet_id AND m.token_id=plan.token_id AND m.record_type='receipt')=1 AND sweep.accounting_model_version=1
UNION ALL
SELECT plan.id,plan.merchant_id,plan.network_id,plan.token_id,plan.source_wallet_id,plan.destination_wallet_id,
 plan.principal,plan.service_fee,e.id,spent.line_id,received.line_id
 FROM reg_crypto_aml_internal_allocations plan
 JOIN reg_crypto_sweep_custody_bindings b ON b.line_id=plan.sweep_attempt_id
 JOIN doc_crypto_sweep_lines line ON line.line_id=b.line_id AND line.document_id=b.sweep_id AND line.wallet_id=plan.source_wallet_id AND line.amount=plan.gross
 JOIN doc_crypto_sweeps sweep ON sweep.id=b.sweep_id AND sweep.accounting_model_version=2 AND sweep.hot_wallet_id=plan.destination_wallet_id AND sweep.token_id=plan.token_id
 JOIN reg_crypto_custody_effect_postings p ON p.id=b.posting_id AND p.recorder_id=sweep.id AND p.recorder_type='CryptoSweep' AND p.recorder_version=1
 JOIN reg_crypto_custody_effect_recognitions sr ON sr.posting_id=p.id AND sr.record_type='expense' AND sr.wallet_id=plan.source_wallet_id AND sr.token_id=plan.token_id
 JOIN reg_crypto_custody_effect_recognitions dr ON dr.posting_id=p.id AND dr.record_type='receipt' AND dr.wallet_id=plan.destination_wallet_id AND dr.token_id=plan.token_id
 JOIN reg_crypto_balance_movements spent ON spent.line_id=sr.movement_line_id AND spent.amount=plan.gross
 JOIN reg_crypto_balance_movements received ON received.line_id=dr.movement_line_id AND received.amount=plan.gross
 JOIN reg_crypto_aml_effects e ON e.wallet_id=plan.destination_wallet_id AND e.source_wallet_id=plan.source_wallet_id AND e.token_id=plan.token_id AND e.network_id=plan.network_id AND e.tx_hash=line.tx_hash AND e.amount=plan.gross
 JOIN reg_crypto_aml_effect_chain_states cs ON cs.effect_id=e.id AND cs.canonicality='canonical'
 JOIN reg_crypto_accounting_effect_evidence ae ON ae.verification_id=cs.verification_id AND ae.occurrence_id=p.occurrence_id
 JOIN reg_crypto_expected_chain_effects expected ON expected.id=e.expected_effect_id
 JOIN reg_crypto_signing_authorizations auth ON auth.id=expected.signing_authorization_id AND auth.basis_type='CryptoSweepAttempt' AND auth.basis_id=line.line_id AND auth.signed_envelope IS NOT NULL
 WHERE plan.status IN ('reserved','applied') AND line.settlement_status='recorded' AND line.chain_result='SUCCESS'
 AND spent.created_at>=(SELECT created_at FROM reg_crypto_aml_wallet_baselines WHERE wallet_id=plan.source_wallet_id AND token_id=plan.token_id)
 AND received.created_at>=(SELECT created_at FROM reg_crypto_aml_wallet_baselines WHERE wallet_id=plan.destination_wallet_id AND token_id=plan.token_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN IF EXISTS(SELECT 1 FROM doc_crypto_sweeps WHERE accounting_model_version=2)
    OR EXISTS(SELECT 1 FROM reg_crypto_sweep_custody_bindings) THEN
    RAISE EXCEPTION 'sweep accounting v2 requires forward migration'; END IF; END; $$;
CREATE OR REPLACE VIEW crypto_aml_allocatable_transfers AS
 SELECT plan.id allocation_id,plan.merchant_id,plan.network_id,plan.token_id,plan.source_wallet_id,plan.destination_wallet_id,
        plan.principal,plan.service_fee,e.id effect_id,spent.line_id source_movement_id,received.line_id destination_movement_id
 FROM reg_crypto_aml_internal_allocations plan
 JOIN doc_crypto_sweep_lines line ON line.line_id=plan.sweep_attempt_id AND line.wallet_id=plan.source_wallet_id AND line.amount=plan.gross AND line.tx_hash<>''
 JOIN doc_crypto_sweeps sweep ON sweep.id=line.document_id AND sweep.posted AND sweep.hot_wallet_id=plan.destination_wallet_id AND sweep.token_id=plan.token_id
 JOIN reg_crypto_aml_effects e ON e.wallet_id=plan.destination_wallet_id AND e.source_wallet_id=plan.source_wallet_id AND e.token_id=plan.token_id AND e.network_id=plan.network_id AND e.tx_hash=line.tx_hash AND e.amount=plan.gross
 JOIN reg_crypto_aml_effect_chain_states cs ON cs.effect_id=e.id AND cs.canonicality='canonical'
 JOIN reg_crypto_expected_chain_effects expected ON expected.id=e.expected_effect_id
 JOIN reg_crypto_signing_authorizations auth ON auth.id=expected.signing_authorization_id AND auth.basis_type='CryptoSweepAttempt' AND auth.basis_id=plan.sweep_attempt_id AND auth.signed_envelope IS NOT NULL
 JOIN reg_crypto_balance_movements spent ON spent.recorder_id=sweep.id AND spent.recorder_type='CryptoSweep' AND spent.recorder_version=sweep.posted_version
    AND spent.wallet_id=plan.source_wallet_id AND spent.token_id=plan.token_id AND spent.record_type='expense' AND spent.amount=plan.gross
 JOIN reg_crypto_balance_movements received ON received.recorder_id=sweep.id AND received.recorder_type='CryptoSweep' AND received.recorder_version=sweep.posted_version
    AND received.wallet_id=plan.destination_wallet_id AND received.token_id=plan.token_id AND received.record_type='receipt' AND received.amount=sweep.total_amount
 WHERE plan.status IN ('reserved','applied')
   AND sweep.total_amount=(SELECT SUM(amount) FROM doc_crypto_sweep_lines WHERE document_id=sweep.id AND tx_hash<>'')
   AND spent.created_at>=(SELECT created_at FROM reg_crypto_aml_wallet_baselines WHERE wallet_id=plan.source_wallet_id AND token_id=plan.token_id)
   AND received.created_at>=(SELECT created_at FROM reg_crypto_aml_wallet_baselines WHERE wallet_id=plan.destination_wallet_id AND token_id=plan.token_id)
   AND (SELECT COUNT(*) FROM reg_crypto_balance_movements m WHERE m.recorder_id=sweep.id AND m.recorder_type='CryptoSweep' AND m.recorder_version=sweep.posted_version AND m.wallet_id=plan.source_wallet_id AND m.token_id=plan.token_id AND m.record_type='expense')=1
   AND (SELECT COUNT(*) FROM reg_crypto_balance_movements m WHERE m.recorder_id=sweep.id AND m.recorder_type='CryptoSweep' AND m.recorder_version=sweep.posted_version AND m.wallet_id=plan.destination_wallet_id AND m.token_id=plan.token_id AND m.record_type='receipt')=1;
DROP TRIGGER trg_sweep_header_basis ON doc_crypto_sweeps;
DROP TRIGGER trg_sweep_line_basis ON doc_crypto_sweep_lines;
DROP TRIGGER trg_sweep_line_complete ON doc_crypto_sweep_lines;
DROP TRIGGER trg_sweep_chain_complete ON reg_crypto_chain_settlements;
DROP TRIGGER trg_sweep_fee_complete ON reg_crypto_fee_reservations;
DROP TRIGGER trg_sweep_posting_complete ON reg_crypto_custody_effect_postings;
DROP TRIGGER trg_sweep_complete_reg_crypto_balance_movements ON reg_crypto_balance_movements;
DROP TRIGGER trg_sweep_complete_reg_crypto_merchant_balance_movements ON reg_crypto_merchant_balance_movements;
DROP TRIGGER trg_sweep_complete_reg_crypto_fee_movements ON reg_crypto_fee_movements;
DROP TRIGGER trg_sweep_complete_reg_crypto_platform_cost_movements ON reg_crypto_platform_cost_movements;
DROP TRIGGER trg_sweep_complete_reg_crypto_provider_balance_movements ON reg_crypto_provider_balance_movements;
DROP TABLE reg_crypto_sweep_custody_bindings;
ALTER TABLE doc_crypto_sweeps DROP COLUMN accounting_model_version;
CREATE OR REPLACE FUNCTION fn_crypto_payment_posting_transaction() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    NEW.posting_txid:=CASE WHEN NEW.recorder_type IN ('CryptoPayment','CryptoNativeFunding') AND NEW.reversal_of_line_id IS NULL THEN txid_current() ELSE NULL END;
    RETURN NEW;
END;
$$;
CREATE OR REPLACE FUNCTION fn_crypto_payment_requires_recognition(payment UUID,network UUID,hash TEXT,kind TEXT,event_index BIGINT)
RETURNS BOOLEAN LANGUAGE sql VOLATILE AS $$
    SELECT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings WHERE recorder_type='CryptoPayment' AND recorder_id=payment)
       OR EXISTS(SELECT 1 FROM reg_crypto_transfer_verifications v
          JOIN reg_crypto_accounting_effect_evidence x ON x.verification_id=v.id
          WHERE v.network_id=network AND regexp_replace(lower(v.tx_hash),'^0x','')=regexp_replace(lower(hash),'^0x','')
            AND (v.event_kind='native_value' OR (v.event_kind=kind AND v.event_index=$5)))
       OR EXISTS(SELECT 1 FROM doc_crypto_payments d JOIN reg_crypto_accounting_effect_evidence x ON x.verification_id=d.verification_id WHERE d.id=payment)
$$;
DROP FUNCTION fn_crypto_sweep_basis_guard();
DROP FUNCTION fn_crypto_sweep_settlement_complete();
DROP FUNCTION fn_crypto_sweep_financial_complete();
DROP FUNCTION fn_crypto_sweep_posting_complete();
DROP FUNCTION fn_crypto_sweep_binding_complete();
DROP FUNCTION fn_crypto_assert_sweep_line(UUID);
-- +goose StatementEnd
