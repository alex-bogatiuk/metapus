-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE doc_treasury_withdrawals ADD COLUMN accounting_model_version INT NOT NULL DEFAULT 1 CHECK(accounting_model_version IN (1,2));
ALTER TABLE doc_treasury_withdrawals ALTER COLUMN accounting_model_version SET DEFAULT 2;
CREATE TABLE reg_crypto_treasury_custody_bindings (
    posting_id UUID PRIMARY KEY REFERENCES reg_crypto_custody_effect_postings(id),
    treasury_id UUID NOT NULL UNIQUE REFERENCES doc_treasury_withdrawals(id),
    signing_authorization_id UUID NOT NULL REFERENCES reg_crypto_signing_authorizations(id),
    period TIMESTAMPTZ NOT NULL,
    actor TEXT NOT NULL DEFAULT 'treasury-settlement' CHECK(actor='treasury-settlement'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TRIGGER trg_treasury_custody_binding_immutable BEFORE UPDATE OR DELETE ON reg_crypto_treasury_custody_bindings
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_financial_movement_immutable();
CREATE OR REPLACE FUNCTION fn_crypto_payment_posting_transaction() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    NEW.posting_txid:=CASE WHEN NEW.recorder_type IN ('CryptoPayment','CryptoNativeFunding','CryptoSweep','TreasuryWithdrawal') AND NEW.reversal_of_line_id IS NULL THEN txid_current() ELSE NULL END;
    RETURN NEW;
END;
$$;

CREATE FUNCTION fn_crypto_assert_treasury(target UUID) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE d doc_treasury_withdrawals; w cat_wallets; f reg_crypto_fee_reservations; c reg_crypto_chain_settlements;
DECLARE b reg_crypto_treasury_custody_bindings; p reg_crypto_custody_effect_postings; success BOOLEAN;
BEGIN
    SELECT * INTO STRICT d FROM doc_treasury_withdrawals WHERE id=target;
    IF d.accounting_model_version<>2 THEN RETURN; END IF;
    SELECT * INTO STRICT w FROM cat_wallets WHERE id=d.source_wallet_id;
    SELECT * INTO f FROM reg_crypto_fee_reservations WHERE basis_type='TreasuryWithdrawal' AND basis_id=d.id AND purpose='treasury_withdrawal' AND attempt=1;
    SELECT * INTO c FROM reg_crypto_chain_settlements WHERE network_id=w.network_id AND tx_hash=d.tx_hash;
    SELECT * INTO b FROM reg_crypto_treasury_custody_bindings WHERE treasury_id=d.id;
    success:=c.status='succeeded';
    IF d.settlement_status<>'recorded' OR NOT d.posted OR d.posted_version<>1 OR c.id IS NULL OR f.id IS NULL
       OR c.basis_type<>'TreasuryWithdrawal' OR c.basis_id<>d.id OR c.status NOT IN ('succeeded','reverted') OR c.finality_status NOT IN ('solidified','finalized')
       OR f.status<>'settled' OR f.payer_wallet_id<>w.id OR f.operation_token_id<>d.token_id OR f.network_id<>w.network_id
       OR f.tx_hash<>d.tx_hash OR f.actual_fee_minor<>c.total_fee_minor OR f.native_token_id<>c.native_token_id OR f.reserved_principal_minor<>0
       OR w.custody_merchant_id IS NOT NULL OR w.vault_key_id='' OR w.wallet_role NOT IN ('treasury','liquidity_hot')
       OR NOT EXISTS(SELECT 1 FROM cat_tokens WHERE id=d.token_id AND network_id=w.network_id)
       OR d.network_fee<>c.total_fee_minor OR d.network_fee_token_id IS DISTINCT FROM c.native_token_id
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_expected_chain_effects x JOIN reg_crypto_signing_authorizations a ON a.id=x.signing_authorization_id JOIN cat_blockchain_networks n ON n.id=a.network_id
           WHERE a.basis_type='TreasuryWithdrawal' AND a.basis_id=d.id AND a.purpose='treasury_withdrawal' AND a.signed_envelope IS NOT NULL AND a.approval_bundle_digest<>''
             AND x.network_id=w.network_id AND x.token_id=d.token_id AND x.amount=d.amount AND regexp_replace(lower(x.tx_hash),'^0x','')=regexp_replace(lower(d.tx_hash),'^0x','')
             AND x.source_address_key=w.address_key AND x.destination_address_key=CASE WHEN n.chain_family='evm' THEN lower(d.dest_address) ELSE d.dest_address END)
       OR d.status<>(CASE WHEN success THEN 'confirmed' ELSE 'failed' END)
       OR (success AND d.chain_result<>'SUCCESS') OR (NOT success AND d.chain_result='SUCCESS')
       OR (SELECT count(*) FROM reg_crypto_balance_reservations WHERE document_id=d.id)<>1
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_balance_reservations WHERE document_type='TreasuryWithdrawal' AND document_id=d.id AND wallet_id=w.id AND token_id=d.token_id
           AND amount=d.amount AND purpose='treasury_withdrawal' AND status=CASE WHEN success THEN 'consumed' ELSE 'released' END)
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_revenue_reservations WHERE document_id=d.id)
       OR (SELECT COALESCE(sum(amount),0) FROM reg_crypto_revenue_reservations WHERE document_id=d.id)<>d.amount
       OR EXISTS(SELECT 1 FROM reg_crypto_revenue_reservations WHERE document_id=d.id AND (document_type<>'TreasuryWithdrawal' OR token_id<>d.token_id
           OR purpose<>'treasury_withdrawal' OR status<>CASE WHEN success THEN 'consumed' ELSE 'released' END))
       OR (SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=d.id AND recorder_type='ChainSettlement')<>(CASE WHEN c.total_fee_minor>0 THEN 1 ELSE 0 END)
       OR EXISTS(SELECT 1 FROM reg_crypto_balance_movements WHERE recorder_id=d.id AND recorder_type='ChainSettlement' AND
           (line_id<>f.id OR recorder_version<>1 OR record_type<>'expense' OR wallet_id<>w.id OR token_id<>c.native_token_id OR amount<>c.total_fee_minor OR reversal_of_line_id IS NOT NULL))
       OR EXISTS(SELECT 1 FROM reg_crypto_balance_movements WHERE recorder_id=d.id AND recorder_type NOT IN ('TreasuryWithdrawal','ChainSettlement'))
       OR (SELECT count(*) FROM reg_crypto_platform_cost_movements WHERE recorder_id=d.id)<>(CASE WHEN c.total_fee_minor>0 THEN 1 ELSE 0 END)
       OR EXISTS(SELECT 1 FROM reg_crypto_platform_cost_movements WHERE recorder_id=d.id AND
           (line_id<>f.id OR recorder_type<>'ChainSettlement' OR recorder_version<>1 OR record_type<>'expense' OR network_id<>w.network_id OR operation_token_id<>d.token_id OR cost_token_id<>c.native_token_id
            OR cost_type<>'chain_network_fee' OR purpose<>'treasury' OR provider<>'chain' OR external_order_id<>d.tx_hash OR amount<>c.total_fee_minor OR reversal_of_line_id IS NOT NULL))
       OR EXISTS(SELECT 1 FROM reg_crypto_merchant_balance_movements WHERE recorder_id=d.id)
       OR EXISTS(SELECT 1 FROM reg_crypto_provider_balance_movements WHERE recorder_id=d.id) THEN
        RAISE EXCEPTION 'treasury requires exact final principal, fee and reservation set' USING ERRCODE='23514';
    END IF;
    IF NOT success THEN
        IF b.posting_id IS NOT NULL OR EXISTS(SELECT 1 FROM reg_crypto_balance_movements WHERE recorder_id=d.id AND recorder_type='TreasuryWithdrawal')
           OR EXISTS(SELECT 1 FROM reg_crypto_fee_movements WHERE recorder_id=d.id) THEN
            RAISE EXCEPTION 'reverted treasury cannot recognize principal or revenue expense' USING ERRCODE='23514';
        END IF;
        RETURN;
    END IF;
    SELECT * INTO p FROM reg_crypto_custody_effect_postings WHERE id=b.posting_id;
    IF b.posting_id IS NULL OR p.recorder_type<>'TreasuryWithdrawal' OR p.recorder_id<>d.id OR p.recorder_version<>1 OR p.generation<>1
       OR EXISTS(SELECT 1 FROM doc_treasury_withdrawals prior JOIN cat_wallets pw ON pw.id=prior.source_wallet_id JOIN cat_tokens pt ON pt.id=prior.token_id
           JOIN cat_tokens dt ON dt.id=d.token_id JOIN cat_blockchain_networks n ON n.id=w.network_id
           WHERE prior.id<>d.id AND prior.accounting_model_version=1 AND prior.posted AND pw.network_id=w.network_id AND pw.address_key=w.address_key AND prior.amount=d.amount
             AND regexp_replace(lower(prior.tx_hash),'^0x','')=regexp_replace(lower(d.tx_hash),'^0x','')
             AND CASE WHEN n.chain_family='evm' THEN lower(COALESCE(pt.contract_address,''))=lower(COALESCE(dt.contract_address,'')) AND lower(prior.dest_address)=lower(d.dest_address)
                 ELSE COALESCE(pt.contract_address,'')=COALESCE(dt.contract_address,'') AND prior.dest_address=d.dest_address END)
       OR EXISTS(SELECT 1 FROM doc_crypto_withdrawals prior JOIN cat_wallets pw ON pw.id=prior.source_wallet_id JOIN cat_tokens pt ON pt.id=prior.token_id
           JOIN cat_tokens dt ON dt.id=d.token_id JOIN cat_blockchain_networks n ON n.id=w.network_id
           WHERE prior.posted AND pw.network_id=w.network_id AND pw.address_key=w.address_key AND prior.amount=d.amount
             AND regexp_replace(lower(prior.tx_hash),'^0x','')=regexp_replace(lower(d.tx_hash),'^0x','')
             AND CASE WHEN n.chain_family='evm' THEN lower(COALESCE(pt.contract_address,''))=lower(COALESCE(dt.contract_address,'')) AND lower(prior.dest_address)=lower(d.dest_address)
                 ELSE COALESCE(pt.contract_address,'')=COALESCE(dt.contract_address,'') AND prior.dest_address=d.dest_address END)
       OR EXISTS(SELECT 1 FROM doc_crypto_custody_adjustments a JOIN reg_crypto_external_outflows o ON o.id=a.external_outflow_id
           JOIN cat_tokens dt ON dt.id=d.token_id JOIN cat_blockchain_networks n ON n.id=w.network_id
           WHERE a.status='posted' AND o.network_id=w.network_id AND o.source_address_key=w.address_key AND o.amount=d.amount
             AND regexp_replace(lower(o.tx_hash),'^0x','')=regexp_replace(lower(d.tx_hash),'^0x','')
             AND o.contract_address_key=CASE WHEN n.chain_family='evm' THEN lower(COALESCE(dt.contract_address,'')) ELSE COALESCE(dt.contract_address,'') END
             AND o.destination_address_key=CASE WHEN n.chain_family='evm' THEN lower(d.dest_address) ELSE d.dest_address END)
       OR (SELECT count(*) FROM reg_crypto_custody_effect_recognitions WHERE posting_id=p.id)<>1
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_recognitions WHERE posting_id=p.id AND wallet_id=w.id AND token_id=d.token_id AND record_type='expense')
       OR (SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=d.id AND recorder_type='TreasuryWithdrawal')<>1
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_recognitions r JOIN reg_crypto_balance_movements m ON m.line_id=r.movement_line_id
           WHERE r.posting_id=p.id AND m.amount=d.amount AND m.period=b.period)
       OR EXISTS(SELECT 1 FROM cat_wallets recipient JOIN cat_blockchain_networks n ON n.id=w.network_id WHERE recipient.network_id=w.network_id
           AND recipient.address_key=CASE WHEN n.chain_family='evm' THEN lower(d.dest_address) ELSE d.dest_address END)
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_accounting_effect_occurrences o JOIN reg_crypto_accounting_effects e ON e.id=o.effect_id JOIN cat_blockchain_networks n ON n.id=e.network_id
           WHERE o.id=p.occurrence_id AND e.network_id=w.network_id AND e.tx_hash=regexp_replace(lower(d.tx_hash),'^0x','') AND o.source_address=w.address_key
             AND o.destination_address=CASE WHEN n.chain_family='evm' THEN lower(d.dest_address) ELSE d.dest_address END AND o.amount=d.amount
             AND o.block_hash=regexp_replace(lower(c.block_hash),'^0x','') AND o.block_number=c.block_height)
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_expected_chain_effects x JOIN reg_crypto_signing_authorizations a ON a.id=x.signing_authorization_id JOIN cat_blockchain_networks n ON n.id=a.network_id
           WHERE a.id=b.signing_authorization_id AND a.basis_type='TreasuryWithdrawal' AND a.basis_id=d.id AND a.purpose='treasury_withdrawal'
             AND a.signed_envelope IS NOT NULL AND a.approval_bundle_digest<>'' AND x.network_id=w.network_id AND x.token_id=d.token_id AND x.amount=d.amount
             AND regexp_replace(lower(x.tx_hash),'^0x','')=regexp_replace(lower(d.tx_hash),'^0x','') AND x.source_address_key=w.address_key
             AND x.destination_address_key=CASE WHEN n.chain_family='evm' THEN lower(d.dest_address) ELSE d.dest_address END)
       OR (SELECT count(*) FROM reg_crypto_fee_movements WHERE recorder_id=d.id)<>(SELECT count(*) FROM reg_crypto_revenue_reservations WHERE document_id=d.id)
       OR EXISTS(SELECT 1 FROM reg_crypto_revenue_reservations r LEFT JOIN reg_crypto_fee_movements m ON m.line_id=r.id
           WHERE r.document_id=d.id AND (m.line_id IS NULL OR m.recorder_id<>d.id OR m.merchant_id<>r.merchant_id OR m.token_id<>r.token_id OR m.fee_type<>r.fee_type OR m.amount<>r.amount))
       OR EXISTS(SELECT 1 FROM reg_crypto_fee_movements WHERE recorder_id=d.id AND (recorder_type<>'TreasuryWithdrawal' OR recorder_version<>1 OR record_type<>'expense' OR period<>b.period OR reversal_of_line_id IS NOT NULL))
       OR (SELECT count(*) FROM sys_outbox WHERE aggregate_type='TreasuryWithdrawal' AND aggregate_id=d.id AND event_type='custody_recognized')<>1
       OR NOT EXISTS(SELECT 1 FROM sys_outbox WHERE aggregate_type='TreasuryWithdrawal' AND aggregate_id=d.id AND event_type='custody_recognized'
           AND payload->>'postingId'=p.id::text AND payload->>'entityId'=d.id::text AND payload->>'entityType'='TreasuryWithdrawal' AND payload->>'action'='custody_recognized' AND payload->>'generation'='1') THEN
        RAISE EXCEPTION 'successful treasury requires one proven outgoing principal and exact revenue expense' USING ERRCODE='23514';
    END IF;
END;
$$;

CREATE FUNCTION fn_crypto_treasury_binding_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings WHERE id=NEW.posting_id AND recorder_type='TreasuryWithdrawal' AND recorder_id=NEW.treasury_id)
       OR (SELECT count(*) FROM reg_crypto_custody_effect_recognitions r JOIN reg_crypto_balance_movements m ON m.line_id=r.movement_line_id
           WHERE r.posting_id=NEW.posting_id AND m.posting_txid=txid_current())<>1 THEN
        RAISE EXCEPTION 'treasury requires a new principal line in the current transaction' USING ERRCODE='23514';
    END IF;
    PERFORM fn_crypto_assert_treasury(NEW.treasury_id);
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER trg_treasury_binding_complete AFTER INSERT ON reg_crypto_treasury_custody_bindings
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_treasury_binding_complete();
CREATE FUNCTION fn_crypto_treasury_posting_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target UUID;
BEGIN
    IF NEW.recorder_type<>'TreasuryWithdrawal' THEN RETURN NULL; END IF;
    SELECT treasury_id INTO target FROM reg_crypto_treasury_custody_bindings WHERE posting_id=NEW.id;
    IF NOT FOUND THEN RAISE EXCEPTION 'treasury principal requires its immutable binding' USING ERRCODE='23514'; END IF;
    PERFORM fn_crypto_assert_treasury(target);
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER trg_treasury_posting_complete AFTER INSERT ON reg_crypto_custody_effect_postings
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_treasury_posting_complete();

CREATE FUNCTION fn_crypto_treasury_financial_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS(SELECT 1 FROM doc_treasury_withdrawals WHERE id=NEW.recorder_id AND accounting_model_version=2) THEN
        PERFORM fn_crypto_assert_treasury(NEW.recorder_id);
    END IF;
    IF NEW.reversal_of_line_id IS NOT NULL AND EXISTS(SELECT 1 FROM reg_crypto_custody_effect_recognitions r
        JOIN reg_crypto_treasury_custody_bindings b ON b.posting_id=r.posting_id WHERE r.movement_line_id=NEW.reversal_of_line_id) THEN
        RAISE EXCEPTION 'recognized treasury requires explicit compensation workflow' USING ERRCODE='23514';
    END IF;
    RETURN NULL;
END;
$$;
DO $$ DECLARE target TEXT; BEGIN
    FOREACH target IN ARRAY ARRAY['reg_crypto_balance_movements','reg_crypto_merchant_balance_movements','reg_crypto_fee_movements',
        'reg_crypto_platform_cost_movements','reg_crypto_provider_balance_movements'] LOOP
        EXECUTE format('CREATE CONSTRAINT TRIGGER %I AFTER INSERT ON %I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_treasury_financial_complete()',
            'trg_treasury_complete_'||target,target);
    END LOOP;
END; $$;
CREATE FUNCTION fn_crypto_treasury_settlement_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target UUID;
BEGIN
    IF TG_TABLE_NAME='doc_treasury_withdrawals' THEN
        IF NEW.settlement_status<>'recorded' THEN RETURN NULL; END IF;
        target:=NEW.id;
    ELSE
        IF NEW.basis_type<>'TreasuryWithdrawal' OR (TG_TABLE_NAME='reg_crypto_fee_reservations' AND NEW.status<>'settled') THEN RETURN NULL; END IF;
        target:=NEW.basis_id;
    END IF;
    IF EXISTS(SELECT 1 FROM doc_treasury_withdrawals WHERE id=target AND accounting_model_version=2) THEN PERFORM fn_crypto_assert_treasury(target); END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER trg_treasury_document_complete AFTER INSERT OR UPDATE ON doc_treasury_withdrawals
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_treasury_settlement_complete();
CREATE CONSTRAINT TRIGGER trg_treasury_chain_complete AFTER INSERT ON reg_crypto_chain_settlements
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_treasury_settlement_complete();
CREATE CONSTRAINT TRIGGER trg_treasury_fee_complete AFTER INSERT OR UPDATE ON reg_crypto_fee_reservations
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_treasury_settlement_complete();

CREATE FUNCTION fn_crypto_treasury_basis_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        IF OLD.accounting_model_version=2 AND (OLD.tx_hash<>'' OR EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations WHERE basis_type='TreasuryWithdrawal' AND basis_id=OLD.id)) THEN
            RAISE EXCEPTION 'signed treasury history is immutable' USING ERRCODE='23514'; END IF;
        RETURN OLD;
    END IF;
    IF NEW.accounting_model_version<>OLD.accounting_model_version THEN RAISE EXCEPTION 'treasury accounting model is immutable' USING ERRCODE='23514'; END IF;
    IF OLD.accounting_model_version<>2 THEN RETURN NEW; END IF;
    IF (OLD.tx_hash<>'' OR EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations WHERE basis_type='TreasuryWithdrawal' AND basis_id=OLD.id)
        OR EXISTS(SELECT 1 FROM reg_crypto_balance_reservations WHERE document_id=OLD.id AND status='active')
        OR EXISTS(SELECT 1 FROM reg_crypto_revenue_reservations WHERE document_id=OLD.id AND status='active')) AND
       ((NEW.token_id,NEW.source_wallet_id,NEW.dest_address,NEW.address_id,NEW.amount,NEW.date,NEW.basis_type,NEW.basis_id,NEW.created_by,NEW.approved_by,NEW.approved_at)
        IS DISTINCT FROM (OLD.token_id,OLD.source_wallet_id,OLD.dest_address,OLD.address_id,OLD.amount,OLD.date,OLD.basis_type,OLD.basis_id,OLD.created_by,OLD.approved_by,OLD.approved_at)
        OR NEW.deletion_mark OR NEW._deleted_at IS NOT NULL OR NEW.posted_version>1 OR (OLD.posted AND NOT NEW.posted)) THEN
        RAISE EXCEPTION 'treasury signed or reserved basis is immutable' USING ERRCODE='23514';
    END IF;
    IF OLD.settlement_status='recorded' AND (NEW.tx_hash,NEW.status,NEW.settlement_status,NEW.chain_result,NEW.network_fee,NEW.network_fee_token_id)
        IS DISTINCT FROM (OLD.tx_hash,OLD.status,OLD.settlement_status,OLD.chain_result,OLD.network_fee,OLD.network_fee_token_id) THEN
        RAISE EXCEPTION 'treasury final outcome is immutable' USING ERRCODE='23514'; END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_treasury_basis BEFORE UPDATE OR DELETE ON doc_treasury_withdrawals FOR EACH ROW EXECUTE FUNCTION fn_crypto_treasury_basis_guard();

CREATE FUNCTION fn_crypto_treasury_reservation_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target UUID; d doc_treasury_withdrawals; balance NUMERIC; reserved NUMERIC; fees NUMERIC;
BEGIN
    IF TG_OP IN ('UPDATE','DELETE') THEN target:=OLD.document_id; ELSE target:=NEW.document_id; END IF;
    SELECT * INTO d FROM doc_treasury_withdrawals WHERE id=target;
    IF (NOT FOUND OR d.accounting_model_version<>2) AND TG_OP='UPDATE' THEN
        SELECT * INTO d FROM doc_treasury_withdrawals WHERE id=NEW.document_id;
    END IF;
    IF NOT FOUND OR d.accounting_model_version<>2 THEN
        IF TG_OP='DELETE' THEN RETURN OLD; END IF; RETURN NEW;
    END IF;
    IF TG_OP='DELETE' THEN RAISE EXCEPTION 'treasury reservation history is immutable' USING ERRCODE='23514'; END IF;
    IF NEW.document_type<>'TreasuryWithdrawal' OR NEW.token_id<>d.token_id OR NEW.purpose<>'treasury_withdrawal' THEN
        RAISE EXCEPTION 'treasury reservation requires exact document basis' USING ERRCODE='23514'; END IF;
    IF TG_OP='UPDATE' THEN
        IF (to_jsonb(NEW)-ARRAY['status','updated_at']) IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['status','updated_at']) OR OLD.status<>'active' AND NEW.status<>OLD.status THEN
            RAISE EXCEPTION 'treasury reservation basis and terminal status are immutable' USING ERRCODE='23514'; END IF;
        RETURN NEW;
    END IF;
    IF NEW.status<>'active' THEN RAISE EXCEPTION 'treasury reservation must start active' USING ERRCODE='23514'; END IF;
    IF TG_TABLE_NAME='reg_crypto_balance_reservations' THEN
        IF NEW.wallet_id<>d.source_wallet_id OR NEW.amount<>d.amount THEN RAISE EXCEPTION 'treasury wallet reservation must cover exact principal' USING ERRCODE='23514'; END IF;
        PERFORM fn_crypto_lock_receipt_balance(NEW.wallet_id,NEW.token_id);
        SELECT amount INTO STRICT balance FROM reg_crypto_balance_balances WHERE wallet_id=NEW.wallet_id AND token_id=NEW.token_id;
        SELECT COALESCE(sum(amount),0) INTO reserved FROM reg_crypto_balance_reservations WHERE wallet_id=NEW.wallet_id AND token_id=NEW.token_id AND status='active';
        SELECT COALESCE(sum(reserved_principal_minor+CASE WHEN strategy='resource_rental' THEN 0 ELSE max_fee_minor END),0) INTO fees FROM reg_crypto_fee_reservations
            WHERE payer_wallet_id=NEW.wallet_id AND native_token_id=NEW.token_id AND status IN ('reserved','broadcast','held');
    ELSE
        SELECT b.balance INTO STRICT balance FROM reg_crypto_fee_balances b WHERE merchant_id=NEW.merchant_id AND token_id=NEW.token_id AND fee_type=NEW.fee_type FOR UPDATE;
        SELECT COALESCE(sum(amount),0) INTO reserved FROM reg_crypto_revenue_reservations WHERE merchant_id=NEW.merchant_id AND token_id=NEW.token_id AND fee_type=NEW.fee_type AND status='active';
        fees:=0;
    END IF;
    IF NEW.amount>balance-reserved-fees THEN RAISE EXCEPTION 'treasury reservation exceeds available funds' USING ERRCODE='23514'; END IF;
    RETURN NEW;
END;
$$;
CREATE FUNCTION fn_crypto_treasury_reservation_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS(SELECT 1 FROM doc_treasury_withdrawals WHERE id=NEW.document_id AND accounting_model_version=2) AND
       (EXISTS(SELECT 1 FROM doc_treasury_withdrawals WHERE id=NEW.document_id AND (tx_hash<>'' OR settlement_status='recorded'))
        OR (NEW.status<>'active' AND EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations WHERE basis_type='TreasuryWithdrawal' AND basis_id=NEW.document_id AND signed_envelope IS NOT NULL AND status<>'failed'))) THEN
        IF NEW.status<>'active' OR EXISTS(SELECT 1 FROM doc_treasury_withdrawals WHERE id=NEW.document_id AND settlement_status='recorded') THEN
            PERFORM fn_crypto_assert_treasury(NEW.document_id);
        END IF;
    END IF;
    RETURN NULL;
END;
$$;
DO $$ DECLARE target TEXT; BEGIN
    FOREACH target IN ARRAY ARRAY['reg_crypto_balance_reservations','reg_crypto_revenue_reservations'] LOOP
        EXECUTE format('CREATE TRIGGER %I BEFORE INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION fn_crypto_treasury_reservation_guard()', 'trg_treasury_basis_'||target,target);
        EXECUTE format('CREATE CONSTRAINT TRIGGER %I AFTER INSERT OR UPDATE ON %I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_treasury_reservation_complete()', 'trg_treasury_complete_'||target,target);
    END LOOP;
END; $$;
CREATE FUNCTION fn_crypto_treasury_fee_release_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.basis_type='TreasuryWithdrawal' AND NEW.status='released' AND EXISTS(SELECT 1 FROM doc_treasury_withdrawals WHERE id=NEW.basis_id AND accounting_model_version=2)
       AND EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations WHERE basis_type='TreasuryWithdrawal' AND basis_id=NEW.basis_id AND signed_envelope IS NOT NULL AND status<>'failed') THEN
        RAISE EXCEPTION 'signed treasury fee reserve requires a proven final outcome' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_treasury_fee_release BEFORE UPDATE ON reg_crypto_fee_reservations FOR EACH ROW EXECUTE FUNCTION fn_crypto_treasury_fee_release_guard();
CREATE FUNCTION fn_crypto_treasury_outflow_owner_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.recorder_type IN ('TreasuryWithdrawal','CryptoWithdrawal') AND EXISTS(
        WITH candidate AS (
            SELECT token_id,source_wallet_id,tx_hash,dest_address,amount FROM doc_treasury_withdrawals WHERE id=NEW.recorder_id AND NEW.recorder_type='TreasuryWithdrawal'
            UNION ALL SELECT token_id,source_wallet_id,tx_hash,dest_address,amount FROM doc_crypto_withdrawals WHERE id=NEW.recorder_id AND NEW.recorder_type='CryptoWithdrawal'
        ) SELECT 1 FROM candidate c JOIN cat_tokens t ON t.id=c.token_id JOIN cat_wallets w ON w.id=c.source_wallet_id JOIN cat_blockchain_networks n ON n.id=t.network_id
        JOIN reg_crypto_expected_chain_effects x ON x.network_id=n.id AND regexp_replace(lower(x.tx_hash),'^0x','')=regexp_replace(lower(c.tx_hash),'^0x','')
            AND x.source_address_key=w.address_key AND x.destination_address_key=CASE WHEN n.chain_family='evm' THEN lower(c.dest_address) ELSE c.dest_address END
            AND x.contract_address_key=CASE WHEN n.chain_family='evm' THEN lower(COALESCE(t.contract_address,'')) ELSE COALESCE(t.contract_address,'') END AND x.amount=c.amount
        JOIN reg_crypto_signing_authorizations a ON a.id=x.signing_authorization_id AND a.basis_type='TreasuryWithdrawal' AND a.signed_envelope IS NOT NULL
        JOIN doc_treasury_withdrawals d ON d.id=a.basis_id AND d.accounting_model_version=2
        WHERE NEW.recorder_type<>'TreasuryWithdrawal' OR NEW.recorder_id<>d.id) THEN
        RAISE EXCEPTION 'expected treasury principal requires its settlement owner' USING ERRCODE='23514';
    END IF;
    IF NEW.recorder_type='CryptoCustodyAdjustment' AND EXISTS(
        SELECT 1 FROM doc_crypto_custody_adjustments adjustment JOIN reg_crypto_external_outflows o ON o.id=adjustment.external_outflow_id
        JOIN reg_crypto_expected_chain_effects x ON x.network_id=o.network_id AND regexp_replace(lower(x.tx_hash),'^0x','')=regexp_replace(lower(o.tx_hash),'^0x','')
            AND x.event_kind=o.event_kind AND x.source_address_key=o.source_address_key AND x.destination_address_key=o.destination_address_key AND x.contract_address_key=o.contract_address_key AND x.amount=o.amount
        JOIN reg_crypto_signing_authorizations a ON a.id=x.signing_authorization_id AND a.basis_type='TreasuryWithdrawal' AND a.signed_envelope IS NOT NULL
        JOIN doc_treasury_withdrawals d ON d.id=a.basis_id AND d.accounting_model_version=2 WHERE adjustment.id=NEW.recorder_id) THEN
        RAISE EXCEPTION 'expected treasury principal requires its settlement owner' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_treasury_outflow_owner BEFORE INSERT ON reg_crypto_balance_movements FOR EACH ROW EXECUTE FUNCTION fn_crypto_treasury_outflow_owner_guard();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN IF EXISTS(SELECT 1 FROM doc_treasury_withdrawals WHERE accounting_model_version=2) OR EXISTS(SELECT 1 FROM reg_crypto_treasury_custody_bindings) THEN
    RAISE EXCEPTION 'treasury accounting v2 requires forward migration'; END IF; END; $$;
DO $$ DECLARE target TEXT; BEGIN
    FOREACH target IN ARRAY ARRAY['reg_crypto_balance_reservations','reg_crypto_revenue_reservations'] LOOP
        EXECUTE format('DROP TRIGGER %I ON %I','trg_treasury_basis_'||target,target);
        EXECUTE format('DROP TRIGGER %I ON %I','trg_treasury_complete_'||target,target);
    END LOOP;
    FOREACH target IN ARRAY ARRAY['reg_crypto_balance_movements','reg_crypto_merchant_balance_movements','reg_crypto_fee_movements','reg_crypto_platform_cost_movements','reg_crypto_provider_balance_movements'] LOOP
        EXECUTE format('DROP TRIGGER %I ON %I','trg_treasury_complete_'||target,target);
    END LOOP;
END; $$;
DROP TRIGGER trg_treasury_basis ON doc_treasury_withdrawals;
DROP TRIGGER trg_treasury_document_complete ON doc_treasury_withdrawals;
DROP TRIGGER trg_treasury_chain_complete ON reg_crypto_chain_settlements;
DROP TRIGGER trg_treasury_fee_complete ON reg_crypto_fee_reservations;
DROP TRIGGER trg_treasury_fee_release ON reg_crypto_fee_reservations;
DROP TRIGGER trg_treasury_outflow_owner ON reg_crypto_balance_movements;
DROP TRIGGER trg_treasury_posting_complete ON reg_crypto_custody_effect_postings;
DROP TABLE reg_crypto_treasury_custody_bindings;
DROP FUNCTION fn_crypto_treasury_reservation_guard();
DROP FUNCTION fn_crypto_treasury_fee_release_guard();
DROP FUNCTION fn_crypto_treasury_outflow_owner_guard();
DROP FUNCTION fn_crypto_treasury_reservation_complete();
DROP FUNCTION fn_crypto_treasury_basis_guard();
DROP FUNCTION fn_crypto_treasury_settlement_complete();
DROP FUNCTION fn_crypto_treasury_financial_complete();
DROP FUNCTION fn_crypto_treasury_posting_complete();
DROP FUNCTION fn_crypto_treasury_binding_complete();
DROP FUNCTION fn_crypto_assert_treasury(UUID);
CREATE OR REPLACE FUNCTION fn_crypto_payment_posting_transaction() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    NEW.posting_txid:=CASE WHEN NEW.recorder_type IN ('CryptoPayment','CryptoNativeFunding','CryptoSweep') AND NEW.reversal_of_line_id IS NULL THEN txid_current() ELSE NULL END;
    RETURN NEW;
END;
$$;
ALTER TABLE doc_treasury_withdrawals DROP COLUMN accounting_model_version;
-- +goose StatementEnd
