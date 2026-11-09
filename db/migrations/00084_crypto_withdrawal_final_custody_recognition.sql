-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE doc_crypto_withdrawals ADD COLUMN accounting_model_version INT NOT NULL DEFAULT 1 CHECK(accounting_model_version IN (1,2));
ALTER TABLE doc_crypto_withdrawals ALTER COLUMN accounting_model_version SET DEFAULT 2;
ALTER TABLE reg_crypto_aml_withdrawal_facts ALTER COLUMN expense_movement_id DROP NOT NULL;
ALTER TABLE reg_crypto_aml_withdrawal_facts DROP CONSTRAINT reg_crypto_aml_withdrawal_facts_check;
ALTER TABLE reg_crypto_aml_withdrawal_facts ADD CONSTRAINT reg_crypto_aml_withdrawal_facts_check CHECK(
    (succeeded AND expense_movement_id IS NOT NULL AND refund_movement_id IS NULL)
    OR (NOT succeeded AND ((expense_movement_id IS NULL AND refund_movement_id IS NULL) OR (expense_movement_id IS NOT NULL AND refund_movement_id IS NOT NULL))));
CREATE TABLE reg_crypto_withdrawal_custody_bindings (
    posting_id UUID PRIMARY KEY REFERENCES reg_crypto_custody_effect_postings(id),
    withdrawal_id UUID NOT NULL UNIQUE REFERENCES doc_crypto_withdrawals(id),
    request_id UUID NOT NULL REFERENCES doc_withdrawal_requests(id),
    line_id UUID UNIQUE REFERENCES reg_crypto_aml_withdrawal_lines(id),
    signing_authorization_id UUID NOT NULL REFERENCES reg_crypto_signing_authorizations(id),
    merchant_debit_line_id UUID NOT NULL REFERENCES reg_crypto_merchant_balance_movements(line_id),
    period TIMESTAMPTZ NOT NULL,
    actor TEXT NOT NULL DEFAULT 'withdrawal-settlement' CHECK(actor='withdrawal-settlement'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_withdrawal_custody_bindings_request ON reg_crypto_withdrawal_custody_bindings(request_id);
CREATE TRIGGER trg_withdrawal_custody_binding_immutable BEFORE UPDATE OR DELETE ON reg_crypto_withdrawal_custody_bindings FOR EACH ROW EXECUTE FUNCTION fn_crypto_financial_movement_immutable();
CREATE OR REPLACE FUNCTION fn_crypto_payment_posting_transaction() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    NEW.posting_txid:=CASE WHEN NEW.recorder_type IN ('CryptoPayment','CryptoNativeFunding','CryptoSweep','TreasuryWithdrawal','CryptoWithdrawal') AND NEW.reversal_of_line_id IS NULL THEN txid_current() ELSE NULL END;
    RETURN NEW;
END;
$$;

CREATE FUNCTION fn_crypto_assert_withdrawal_parent(target UUID) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE r doc_withdrawal_requests; d doc_crypto_withdrawals;
BEGIN
    SELECT * INTO STRICT r FROM doc_withdrawal_requests WHERE id=target;
    IF NOT EXISTS(SELECT 1 FROM doc_crypto_withdrawals c WHERE c.accounting_model_version=2 AND ((c.basis_type='WithdrawalRequest' AND c.basis_id=r.id)
        OR EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_lines WHERE withdrawal_id=c.id AND request_id=r.id))) THEN RETURN; END IF;
    IF NOT r.posted OR r.posted_version<>1
       OR (SELECT count(*) FROM reg_crypto_merchant_balance_movements WHERE recorder_id=r.id AND record_type='expense')<>1
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_merchant_balance_movements WHERE recorder_id=r.id AND recorder_type='WithdrawalRequest' AND recorder_version=1 AND record_type='expense'
           AND merchant_id=r.merchant_id AND token_id=r.token_id AND amount=r.amount AND period=r.date AND reversal_of_line_id IS NULL)
       OR EXISTS(SELECT 1 FROM reg_crypto_merchant_balance_movements WHERE recorder_id=r.id AND
           (recorder_type<>'WithdrawalRequest' OR merchant_id<>r.merchant_id OR token_id<>r.token_id OR reversal_of_line_id IS NOT NULL OR recorder_version<>(CASE WHEN record_type='expense' THEN 1 ELSE 2 END))) THEN
        RAISE EXCEPTION 'withdrawal requires its single original merchant debit' USING ERRCODE='23514';
    END IF;
    IF EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_plans WHERE request_id=r.id) THEN
        IF EXISTS(SELECT 1 FROM reg_crypto_merchant_balance_movements m WHERE m.recorder_id=r.id AND m.record_type='receipt' AND NOT EXISTS(
            SELECT 1 FROM reg_crypto_aml_withdrawal_lines l WHERE l.request_id=r.id AND l.id=m.line_id AND l.amount=m.amount AND l.status IN ('failed','cancelled')))
           OR EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_lines l WHERE l.request_id=r.id AND l.status IN ('failed','cancelled') AND NOT EXISTS(
            SELECT 1 FROM reg_crypto_merchant_balance_movements m WHERE m.recorder_id=r.id AND m.line_id=l.id AND m.record_type='receipt' AND m.amount=l.amount)) THEN
            RAISE EXCEPTION 'withdrawal refund requires an exact failed or cancelled source line' USING ERRCODE='23514'; END IF;
    ELSE
        SELECT * INTO STRICT d FROM doc_crypto_withdrawals WHERE basis_type='WithdrawalRequest' AND basis_id=r.id;
        IF (d.settlement_status='recorded' AND d.chain_result<>'SUCCESS') OR (d.tx_hash='' AND NOT EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations WHERE basis_type='WithdrawalRequest' AND basis_id=r.id) AND r.status IN ('failed','rejected')) THEN
            IF (SELECT count(*) FROM reg_crypto_merchant_balance_movements WHERE recorder_id=r.id AND record_type='receipt')<>1
               OR NOT EXISTS(SELECT 1 FROM reg_crypto_merchant_balance_movements WHERE recorder_id=r.id AND record_type='receipt' AND amount=r.amount) THEN
                RAISE EXCEPTION 'failed withdrawal requires one exact merchant refund' USING ERRCODE='23514'; END IF;
        ELSIF EXISTS(SELECT 1 FROM reg_crypto_merchant_balance_movements WHERE recorder_id=r.id AND record_type='receipt') THEN
            RAISE EXCEPTION 'pending or successful withdrawal cannot restore merchant debit' USING ERRCODE='23514';
        END IF;
        IF d.settlement_status='recorded' AND r.status<>(CASE WHEN d.chain_result='SUCCESS' THEN 'confirmed' ELSE 'failed' END) THEN
            RAISE EXCEPTION 'withdrawal request requires its final execution outcome' USING ERRCODE='23514'; END IF;
    END IF;
END;
$$;

CREATE FUNCTION fn_crypto_assert_withdrawal(target UUID) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE d doc_crypto_withdrawals; r doc_withdrawal_requests; l reg_crypto_aml_withdrawal_lines; w cat_wallets;
DECLARE f reg_crypto_fee_reservations; c reg_crypto_chain_settlements; b reg_crypto_withdrawal_custody_bindings; p reg_crypto_custody_effect_postings;
DECLARE reserve reg_crypto_balance_reservations; success BOOLEAN; basis TEXT; owner_basis_id UUID;
BEGIN
    SELECT * INTO STRICT d FROM doc_crypto_withdrawals WHERE id=target;
    IF d.accounting_model_version<>2 THEN RETURN; END IF;
    IF d.basis_type='CryptoAMLWithdrawalLine' THEN
        SELECT * INTO STRICT l FROM reg_crypto_aml_withdrawal_lines WHERE id=d.basis_id AND withdrawal_id=d.id;
        SELECT * INTO STRICT r FROM doc_withdrawal_requests WHERE id=l.request_id;
        SELECT * INTO reserve FROM reg_crypto_balance_reservations WHERE id=l.reservation_id;
    ELSIF d.basis_type='WithdrawalRequest' THEN
        SELECT * INTO STRICT r FROM doc_withdrawal_requests WHERE id=d.basis_id AND withdrawal_id=d.id;
        SELECT * INTO reserve FROM reg_crypto_balance_reservations WHERE document_type='WithdrawalRequest' AND document_id=r.id;
    ELSE RAISE EXCEPTION 'withdrawal requires an approved request or AML source line' USING ERRCODE='23514'; END IF;
    basis:=d.basis_type; owner_basis_id:=d.basis_id;
    SELECT * INTO STRICT w FROM cat_wallets WHERE id=d.source_wallet_id;
    SELECT * INTO f FROM reg_crypto_fee_reservations WHERE basis_type=basis AND reg_crypto_fee_reservations.basis_id=owner_basis_id AND purpose='withdrawal' AND attempt=1;
    SELECT * INTO c FROM reg_crypto_chain_settlements WHERE network_id=w.network_id AND tx_hash=d.tx_hash;
    SELECT * INTO b FROM reg_crypto_withdrawal_custody_bindings WHERE withdrawal_id=d.id;
    success:=c.status='succeeded';
    IF d.settlement_status<>'recorded' OR NOT d.posted OR d.posted_version<>1 OR f.id IS NULL OR c.id IS NULL OR reserve.id IS NULL
       OR d.merchant_id<>r.merchant_id OR d.token_id<>r.token_id OR d.dest_address<>r.dest_address OR NOT r.posted OR r.posted_version<>1
       OR (l.id IS NULL AND d.amount<>r.amount) OR (l.id IS NOT NULL AND (l.wallet_id<>w.id OR l.token_id<>d.token_id OR l.amount<>d.amount OR l.request_id<>r.id))
       OR w.vault_key_id='' OR (w.custody_merchant_id IS NOT NULL AND w.custody_merchant_id<>r.merchant_id)
       OR (l.id IS NULL AND w.wallet_role<>'liquidity_hot') OR (l.id IS NOT NULL AND (w.wallet_role NOT IN ('merchant_pool','liquidity_hot') OR w.custody_merchant_id IS DISTINCT FROM r.merchant_id))
       OR NOT EXISTS(SELECT 1 FROM cat_tokens WHERE id=d.token_id AND network_id=w.network_id)
       OR c.basis_type<>basis OR c.basis_id<>owner_basis_id OR c.status NOT IN ('succeeded','reverted') OR c.finality_status NOT IN ('solidified','finalized')
       OR f.status<>'settled' OR f.payer_wallet_id<>w.id OR f.operation_token_id<>d.token_id OR f.network_id<>w.network_id OR f.reserved_principal_minor<>0
       OR f.tx_hash<>d.tx_hash OR f.native_token_id<>c.native_token_id OR f.actual_fee_minor<>c.total_fee_minor
       OR d.network_fee<>c.total_fee_minor OR d.network_fee_token_id IS DISTINCT FROM c.native_token_id
       OR d.status<>(CASE WHEN success THEN 'confirmed' ELSE 'failed' END) OR (d.chain_result='SUCCESS') IS DISTINCT FROM success
       OR reserve.wallet_id<>w.id OR reserve.token_id<>d.token_id OR reserve.amount<>d.amount OR reserve.document_type<>basis OR reserve.document_id<>owner_basis_id
       OR reserve.purpose<>'withdrawal' OR reserve.status<>(CASE WHEN success THEN 'consumed' ELSE 'released' END)
       OR (SELECT count(*) FROM reg_crypto_balance_reservations WHERE document_type=basis AND document_id=owner_basis_id)<>1
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations a JOIN reg_crypto_expected_chain_effects x ON x.signing_authorization_id=a.id JOIN cat_blockchain_networks n ON n.id=a.network_id
           WHERE a.basis_type=basis AND a.basis_id=owner_basis_id AND a.attempt=1 AND a.purpose='withdrawal' AND a.merchant_id=d.merchant_id AND a.signed_envelope IS NOT NULL AND a.approval_bundle_digest<>''
             AND x.network_id=w.network_id AND x.token_id=d.token_id AND x.amount=d.amount AND x.source_address_key=w.address_key
             AND regexp_replace(lower(x.tx_hash),'^0x','')=regexp_replace(lower(d.tx_hash),'^0x','') AND x.destination_address_key=CASE WHEN n.chain_family='evm' THEN lower(d.dest_address) ELSE d.dest_address END)
       OR (SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=owner_basis_id AND recorder_type='ChainSettlement')<>(CASE WHEN c.total_fee_minor>0 THEN 1 ELSE 0 END)
       OR EXISTS(SELECT 1 FROM reg_crypto_balance_movements WHERE recorder_id=owner_basis_id AND recorder_type='ChainSettlement' AND
           (line_id<>f.id OR recorder_version<>1 OR record_type<>'expense' OR wallet_id<>w.id OR token_id<>c.native_token_id OR amount<>c.total_fee_minor OR reversal_of_line_id IS NOT NULL))
       OR (SELECT count(*) FROM reg_crypto_platform_cost_movements WHERE recorder_id=owner_basis_id)<>(CASE WHEN c.total_fee_minor>0 THEN 1 ELSE 0 END)
       OR EXISTS(SELECT 1 FROM reg_crypto_platform_cost_movements WHERE recorder_id=owner_basis_id AND
           (line_id<>f.id OR recorder_type<>'ChainSettlement' OR recorder_version<>1 OR record_type<>'expense' OR network_id<>w.network_id OR operation_token_id<>d.token_id
            OR cost_token_id<>c.native_token_id OR cost_type<>'chain_network_fee' OR purpose<>'withdrawal' OR provider<>'chain' OR external_order_id<>d.tx_hash OR amount<>c.total_fee_minor OR reversal_of_line_id IS NOT NULL))
       OR EXISTS(SELECT 1 FROM reg_crypto_fee_movements WHERE recorder_id IN(d.id,owner_basis_id))
       OR EXISTS(SELECT 1 FROM reg_crypto_provider_balance_movements WHERE recorder_id IN(d.id,owner_basis_id))
       OR EXISTS(SELECT 1 FROM reg_crypto_merchant_balance_movements WHERE recorder_id=d.id)
       OR EXISTS(SELECT 1 FROM reg_crypto_balance_movements WHERE recorder_id=owner_basis_id AND recorder_type<>'ChainSettlement')
       OR EXISTS(SELECT 1 FROM reg_crypto_platform_cost_movements WHERE recorder_id=d.id)
       OR EXISTS(SELECT 1 FROM reg_crypto_balance_movements WHERE recorder_id=d.id AND recorder_type<>'CryptoWithdrawal') THEN
        RAISE EXCEPTION 'withdrawal requires exact final principal, fee, request and reserve set' USING ERRCODE='23514'; END IF;
    PERFORM fn_crypto_assert_withdrawal_parent(r.id);
    IF l.id IS NOT NULL AND (l.status<>(CASE WHEN success THEN 'confirmed' ELSE 'failed' END) OR NOT EXISTS(
        SELECT 1 FROM crypto_aml_valid_withdrawal_facts WHERE line_id=l.id)) THEN RAISE EXCEPTION 'withdrawal requires its exact AML outcome fact' USING ERRCODE='23514'; END IF;
    IF NOT success THEN
        IF b.posting_id IS NOT NULL OR EXISTS(SELECT 1 FROM reg_crypto_balance_movements WHERE recorder_id=d.id)
           OR (SELECT count(*) FROM sys_outbox WHERE aggregate_type='CryptoWithdrawal' AND aggregate_id=d.id AND event_type='execution_reverted')<>1
           OR NOT EXISTS(SELECT 1 FROM sys_outbox WHERE aggregate_type='CryptoWithdrawal' AND aggregate_id=d.id AND event_type='execution_reverted'
                AND payload->>'entityId'=d.id::text AND payload->>'requestId'=r.id::text AND payload->>'entityType'='CryptoWithdrawal' AND payload->>'action'='execution_reverted') THEN
            RAISE EXCEPTION 'reverted withdrawal requires fee and merchant refund without physical principal' USING ERRCODE='23514'; END IF;
        RETURN;
    END IF;
    SELECT * INTO p FROM reg_crypto_custody_effect_postings WHERE id=b.posting_id;
    IF b.posting_id IS NULL OR b.request_id<>r.id OR b.line_id IS DISTINCT FROM l.id OR p.recorder_type<>'CryptoWithdrawal' OR p.recorder_id<>d.id OR p.recorder_version<>1 OR p.generation<>1
       OR EXISTS(SELECT 1 FROM doc_treasury_withdrawals prior JOIN cat_wallets pw ON pw.id=prior.source_wallet_id JOIN cat_tokens pt ON pt.id=prior.token_id
           JOIN cat_tokens dt ON dt.id=d.token_id JOIN cat_blockchain_networks n ON n.id=w.network_id
           WHERE prior.id<>d.id AND prior.accounting_model_version=1 AND prior.posted AND pw.network_id=w.network_id AND pw.address_key=w.address_key AND prior.amount=d.amount
             AND regexp_replace(lower(prior.tx_hash),'^0x','')=regexp_replace(lower(d.tx_hash),'^0x','')
             AND CASE WHEN n.chain_family='evm' THEN lower(COALESCE(pt.contract_address,''))=lower(COALESCE(dt.contract_address,'')) AND lower(prior.dest_address)=lower(d.dest_address)
                 ELSE COALESCE(pt.contract_address,'')=COALESCE(dt.contract_address,'') AND prior.dest_address=d.dest_address END)
       OR EXISTS(SELECT 1 FROM doc_crypto_withdrawals prior JOIN cat_wallets pw ON pw.id=prior.source_wallet_id JOIN cat_tokens pt ON pt.id=prior.token_id
           JOIN cat_tokens dt ON dt.id=d.token_id JOIN cat_blockchain_networks n ON n.id=w.network_id
           WHERE prior.id<>d.id AND prior.accounting_model_version=1 AND prior.posted AND pw.network_id=w.network_id AND pw.address_key=w.address_key AND prior.amount=d.amount
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
       OR (SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=d.id)<>1
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_recognitions rc JOIN reg_crypto_balance_movements m ON m.line_id=rc.movement_line_id WHERE rc.posting_id=p.id AND m.amount=d.amount AND m.period=b.period)
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_merchant_balance_movements WHERE line_id=b.merchant_debit_line_id AND recorder_id=r.id AND recorder_type='WithdrawalRequest' AND recorder_version=1 AND record_type='expense' AND merchant_id=r.merchant_id AND token_id=r.token_id AND amount=r.amount)
       OR EXISTS(SELECT 1 FROM cat_wallets dst JOIN cat_blockchain_networks n ON n.id=w.network_id WHERE dst.network_id=n.id AND dst.address_key=CASE WHEN n.chain_family='evm' THEN lower(d.dest_address) ELSE d.dest_address END)
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_accounting_effect_occurrences o JOIN reg_crypto_accounting_effects e ON e.id=o.effect_id JOIN cat_blockchain_networks n ON n.id=e.network_id
           WHERE o.id=p.occurrence_id AND e.network_id=w.network_id AND e.tx_hash=regexp_replace(lower(d.tx_hash),'^0x','') AND o.source_address=w.address_key AND o.amount=d.amount
             AND o.destination_address=CASE WHEN n.chain_family='evm' THEN lower(d.dest_address) ELSE d.dest_address END AND o.block_hash=regexp_replace(lower(c.block_hash),'^0x','') AND o.block_number=c.block_height)
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations a WHERE a.id=b.signing_authorization_id AND a.basis_type=basis AND a.basis_id=owner_basis_id AND a.attempt=1 AND a.signed_envelope IS NOT NULL
             AND regexp_replace(lower(a.tx_hash),'^0x','')=regexp_replace(lower(d.tx_hash),'^0x',''))
       OR (SELECT count(*) FROM sys_outbox WHERE aggregate_type='CryptoWithdrawal' AND aggregate_id=d.id AND event_type='custody_recognized')<>1
       OR NOT EXISTS(SELECT 1 FROM sys_outbox WHERE aggregate_type='CryptoWithdrawal' AND aggregate_id=d.id AND event_type='custody_recognized' AND payload->>'postingId'=p.id::text
             AND payload->>'requestId'=r.id::text AND payload->>'entityId'=d.id::text AND payload->>'entityType'='CryptoWithdrawal' AND payload->>'action'='custody_recognized' AND payload->>'generation'='1') THEN
        RAISE EXCEPTION 'successful withdrawal requires one proven outgoing owner and original merchant debit' USING ERRCODE='23514'; END IF;
END;
$$;

CREATE OR REPLACE VIEW crypto_aml_valid_withdrawal_facts AS
 SELECT f.line_id FROM reg_crypto_aml_withdrawal_facts f JOIN reg_crypto_aml_withdrawal_lines l ON l.id=f.line_id AND l.wallet_id=f.wallet_id AND l.token_id=f.token_id AND l.amount=f.amount
 JOIN reg_crypto_aml_withdrawal_plans p ON p.request_id=l.request_id
 JOIN doc_crypto_withdrawals w ON w.id=l.withdrawal_id AND w.basis_type='CryptoAMLWithdrawalLine' AND w.basis_id=l.id AND w.merchant_id=p.merchant_id AND w.source_wallet_id=l.wallet_id AND w.token_id=l.token_id AND w.amount=l.amount AND w.posted AND w.posted_version=1
 JOIN reg_crypto_signing_authorizations a ON a.basis_type='CryptoAMLWithdrawalLine' AND a.basis_id=l.id AND a.generation=1 AND a.attempt=1 AND a.signed_envelope IS NOT NULL AND a.tx_hash=w.tx_hash AND a.source_address=l.source_address AND a.vault_key_id=l.source_vault_key_id AND a.amount_minor_units=l.amount
 JOIN reg_crypto_aml_first_dispatches dispatched ON dispatched.authorization_id=a.id
 JOIN reg_crypto_chain_settlements s ON s.id=f.settlement_id AND s.network_id=p.network_id AND s.basis_type='CryptoAMLWithdrawalLine' AND s.basis_id=l.id AND s.tx_hash=w.tx_hash AND (s.status='succeeded')=f.succeeded
 JOIN reg_crypto_balance_movements m ON m.line_id=f.expense_movement_id AND m.recorder_type='CryptoWithdrawal' AND m.recorder_id=w.id AND m.recorder_version=1 AND m.record_type='expense' AND m.wallet_id=f.wallet_id AND m.token_id=f.token_id AND m.amount=f.amount
 LEFT JOIN reg_crypto_balance_movements refund ON refund.line_id=f.refund_movement_id AND refund.line_id=l.id AND refund.recorder_type='AMLWithdrawalRevert' AND refund.recorder_id=l.id AND refund.recorder_version=1 AND refund.record_type='receipt' AND refund.wallet_id=f.wallet_id AND refund.token_id=f.token_id AND refund.amount=f.amount
 WHERE (f.succeeded OR (refund.line_id IS NOT NULL AND EXISTS(SELECT 1 FROM reg_crypto_merchant_balance_movements mr JOIN doc_withdrawal_requests request ON request.id=l.request_id WHERE mr.line_id=l.id AND mr.recorder_id=l.request_id AND mr.recorder_type='WithdrawalRequest' AND mr.recorder_version=request.posted_version+1 AND mr.record_type='receipt' AND mr.merchant_id=p.merchant_id AND mr.token_id=l.token_id AND mr.amount=l.amount))) AND (SELECT COUNT(*) FROM reg_crypto_balance_movements WHERE recorder_id=w.id AND recorder_type='CryptoWithdrawal')=1 AND w.accounting_model_version=1
UNION ALL

SELECT f.line_id FROM reg_crypto_aml_withdrawal_facts f
 JOIN reg_crypto_aml_withdrawal_lines l ON l.id=f.line_id AND l.wallet_id=f.wallet_id AND l.token_id=f.token_id AND l.amount=f.amount
 JOIN reg_crypto_aml_withdrawal_plans plan ON plan.request_id=l.request_id
 JOIN doc_crypto_withdrawals w ON w.id=l.withdrawal_id AND w.accounting_model_version=2 AND w.basis_type='CryptoAMLWithdrawalLine' AND w.basis_id=l.id
    AND w.merchant_id=plan.merchant_id AND w.source_wallet_id=l.wallet_id AND w.token_id=l.token_id AND w.amount=l.amount AND w.posted AND w.posted_version=1
    AND w.settlement_status='recorded' AND w.status=CASE WHEN f.succeeded THEN 'confirmed' ELSE 'failed' END
 JOIN reg_crypto_signing_authorizations a ON a.basis_type='CryptoAMLWithdrawalLine' AND a.basis_id=l.id AND a.generation=1 AND a.attempt=1
    AND a.signed_envelope IS NOT NULL AND a.tx_hash=w.tx_hash AND a.source_address=l.source_address AND a.vault_key_id=l.source_vault_key_id AND a.amount_minor_units=l.amount
 JOIN reg_crypto_aml_first_dispatches dispatched ON dispatched.authorization_id=a.id
 JOIN reg_crypto_chain_settlements settlement ON settlement.id=f.settlement_id AND settlement.network_id=plan.network_id AND settlement.basis_type='CryptoAMLWithdrawalLine'
    AND settlement.basis_id=l.id AND settlement.tx_hash=w.tx_hash AND (settlement.status='succeeded')=f.succeeded
 JOIN reg_crypto_balance_reservations reserve ON reserve.id=l.reservation_id AND reserve.status=CASE WHEN f.succeeded THEN 'consumed' ELSE 'released' END
 LEFT JOIN reg_crypto_withdrawal_custody_bindings binding ON binding.withdrawal_id=w.id AND binding.request_id=l.request_id AND binding.line_id=l.id AND binding.signing_authorization_id=a.id
 LEFT JOIN reg_crypto_custody_effect_postings posting ON posting.id=binding.posting_id AND posting.recorder_type='CryptoWithdrawal' AND posting.recorder_id=w.id AND posting.generation=1 AND posting.recorder_version=1
 LEFT JOIN reg_crypto_custody_effect_recognitions recognized ON recognized.posting_id=posting.id AND recognized.record_type='expense' AND recognized.wallet_id=l.wallet_id AND recognized.token_id=l.token_id
 LEFT JOIN reg_crypto_balance_movements spent ON spent.line_id=f.expense_movement_id AND spent.line_id=recognized.movement_line_id AND spent.recorder_type='CryptoWithdrawal'
    AND spent.recorder_id=w.id AND spent.recorder_version=1 AND spent.record_type='expense' AND spent.wallet_id=f.wallet_id AND spent.token_id=f.token_id AND spent.amount=f.amount AND spent.reversal_of_line_id IS NULL
 LEFT JOIN reg_crypto_transfer_verifications proof ON proof.id=posting.verification_id
 WHERE f.refund_movement_id IS NULL AND (
 (f.succeeded AND spent.line_id IS NOT NULL AND proof.evidence->'event'->>'canonicality'='canonical'
    AND NOT EXISTS(SELECT 1 FROM reg_crypto_transfer_verifications invalid WHERE invalid.network_id=proof.network_id AND invalid.tx_hash=proof.tx_hash
        AND invalid.event_kind=proof.event_kind AND invalid.event_index=proof.event_index AND invalid.verified_at>=proof.verified_at AND invalid.evidence->'event'->>'canonicality'='orphaned')
    AND NOT EXISTS(SELECT 1 FROM reg_crypto_accounting_effect_evidence ae JOIN reg_crypto_accounting_effect_occurrences occurrence ON occurrence.id=ae.occurrence_id
        JOIN reg_crypto_transfer_verifications newer ON newer.id=ae.verification_id WHERE occurrence.effect_id=posting.effect_id AND occurrence.id<>posting.occurrence_id AND newer.verified_at>=proof.verified_at))
 OR (NOT f.succeeded AND f.expense_movement_id IS NULL AND binding.posting_id IS NULL AND NOT EXISTS(SELECT 1 FROM reg_crypto_balance_movements WHERE recorder_id=w.id)
    AND EXISTS(SELECT 1 FROM reg_crypto_merchant_balance_movements mr JOIN doc_withdrawal_requests request ON request.id=l.request_id
        WHERE mr.line_id=l.id AND mr.recorder_id=l.request_id AND mr.recorder_type='WithdrawalRequest' AND mr.recorder_version=request.posted_version+1 AND mr.record_type='receipt'
          AND mr.merchant_id=plan.merchant_id AND mr.token_id=l.token_id AND mr.amount=l.amount))
 );

CREATE FUNCTION fn_crypto_withdrawal_binding_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings WHERE id=NEW.posting_id AND recorder_type='CryptoWithdrawal' AND recorder_id=NEW.withdrawal_id)
       OR (SELECT count(*) FROM reg_crypto_custody_effect_recognitions rc JOIN reg_crypto_balance_movements m ON m.line_id=rc.movement_line_id WHERE rc.posting_id=NEW.posting_id AND m.posting_txid=txid_current())<>1 THEN
        RAISE EXCEPTION 'withdrawal requires a new principal line in this transaction' USING ERRCODE='23514'; END IF;
    PERFORM fn_crypto_assert_withdrawal(NEW.withdrawal_id); RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER trg_withdrawal_binding_complete AFTER INSERT ON reg_crypto_withdrawal_custody_bindings DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_withdrawal_binding_complete();
CREATE FUNCTION fn_crypto_withdrawal_posting_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target UUID;
BEGIN
    IF NEW.recorder_type<>'CryptoWithdrawal' THEN RETURN NULL; END IF;
    SELECT withdrawal_id INTO target FROM reg_crypto_withdrawal_custody_bindings WHERE posting_id=NEW.id;
    IF NOT FOUND THEN RAISE EXCEPTION 'withdrawal principal requires its binding' USING ERRCODE='23514'; END IF;
    PERFORM fn_crypto_assert_withdrawal(target); RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER trg_withdrawal_posting_complete AFTER INSERT ON reg_crypto_custody_effect_postings DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_withdrawal_posting_complete();

CREATE FUNCTION fn_crypto_withdrawal_financial_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target UUID;
BEGIN
    FOR target IN SELECT id FROM doc_crypto_withdrawals WHERE accounting_model_version=2 AND (id=NEW.recorder_id OR basis_id=NEW.recorder_id) AND (TG_TABLE_NAME<>'reg_crypto_merchant_balance_movements' OR id=NEW.recorder_id OR settlement_status='recorded') LOOP
        PERFORM fn_crypto_assert_withdrawal(target);
    END LOOP;
    IF TG_TABLE_NAME='reg_crypto_merchant_balance_movements' AND EXISTS(SELECT 1 FROM doc_withdrawal_requests WHERE id=NEW.recorder_id) THEN PERFORM fn_crypto_assert_withdrawal_parent(NEW.recorder_id); END IF;
    IF NEW.reversal_of_line_id IS NOT NULL AND EXISTS(SELECT 1 FROM reg_crypto_custody_effect_recognitions rc JOIN reg_crypto_withdrawal_custody_bindings b ON b.posting_id=rc.posting_id WHERE rc.movement_line_id=NEW.reversal_of_line_id) THEN
        RAISE EXCEPTION 'recognized withdrawal requires explicit compensation workflow' USING ERRCODE='23514'; END IF;
    RETURN NULL;
END;
$$;
DO $$ DECLARE target TEXT; BEGIN
    FOREACH target IN ARRAY ARRAY['reg_crypto_balance_movements','reg_crypto_merchant_balance_movements','reg_crypto_fee_movements','reg_crypto_platform_cost_movements','reg_crypto_provider_balance_movements'] LOOP
        EXECUTE format('CREATE CONSTRAINT TRIGGER %I AFTER INSERT ON %I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_withdrawal_financial_complete()','trg_withdrawal_complete_'||target,target);
    END LOOP;
END; $$;
CREATE FUNCTION fn_crypto_withdrawal_settlement_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target UUID;
BEGIN
    IF TG_TABLE_NAME='doc_crypto_withdrawals' THEN
        IF NEW.settlement_status<>'recorded' THEN RETURN NULL; END IF; target:=NEW.id;
    ELSE
        IF NEW.basis_type NOT IN ('WithdrawalRequest','CryptoAMLWithdrawalLine') THEN RETURN NULL; END IF;
        IF TG_TABLE_NAME='reg_crypto_fee_reservations' AND NEW.status<>'settled' THEN RETURN NULL; END IF;
        SELECT id INTO target FROM doc_crypto_withdrawals WHERE basis_type=NEW.basis_type AND basis_id=NEW.basis_id AND accounting_model_version=2;
    END IF;
    IF EXISTS(SELECT 1 FROM doc_crypto_withdrawals WHERE id=target AND accounting_model_version=2) THEN PERFORM fn_crypto_assert_withdrawal(target); END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER trg_withdrawal_document_complete AFTER INSERT OR UPDATE ON doc_crypto_withdrawals DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_withdrawal_settlement_complete();
CREATE CONSTRAINT TRIGGER trg_withdrawal_chain_complete AFTER INSERT ON reg_crypto_chain_settlements DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_withdrawal_settlement_complete();
CREATE CONSTRAINT TRIGGER trg_withdrawal_fee_complete AFTER INSERT OR UPDATE ON reg_crypto_fee_reservations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_withdrawal_settlement_complete();

CREATE FUNCTION fn_crypto_withdrawal_basis_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME='doc_withdrawal_requests' THEN
        IF EXISTS(SELECT 1 FROM doc_crypto_withdrawals c WHERE c.accounting_model_version=2 AND ((c.basis_type='WithdrawalRequest' AND c.basis_id=OLD.id)
            OR EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_lines WHERE withdrawal_id=c.id AND request_id=OLD.id))) THEN
            IF TG_OP='DELETE' THEN RAISE EXCEPTION 'withdrawal request history is immutable' USING ERRCODE='23514'; END IF;
            IF (NEW.merchant_id,NEW.token_id,NEW.amount,NEW.dest_address,NEW.address_id,NEW.date,NEW.posted,NEW.posted_version) IS DISTINCT FROM
               (OLD.merchant_id,OLD.token_id,OLD.amount,OLD.dest_address,OLD.address_id,OLD.date,OLD.posted,OLD.posted_version) OR (NEW.withdrawal_id IS DISTINCT FROM OLD.withdrawal_id AND NOT (OLD.withdrawal_id IS NULL AND EXISTS(SELECT 1 FROM doc_crypto_withdrawals WHERE id=NEW.withdrawal_id AND basis_type='WithdrawalRequest' AND basis_id=OLD.id AND accounting_model_version=2))) OR NEW.deletion_mark OR NEW._deleted_at IS NOT NULL THEN
                RAISE EXCEPTION 'withdrawal request financial basis is immutable' USING ERRCODE='23514'; END IF;
        END IF;
        IF TG_OP='DELETE' THEN RETURN OLD; END IF; RETURN NEW;
    END IF;
    IF TG_OP='DELETE' THEN
        IF OLD.accounting_model_version=2 THEN RAISE EXCEPTION 'withdrawal execution history is immutable' USING ERRCODE='23514'; END IF; RETURN OLD;
    END IF;
    IF NEW.accounting_model_version<>OLD.accounting_model_version THEN RAISE EXCEPTION 'withdrawal accounting model is immutable' USING ERRCODE='23514'; END IF;
    IF OLD.accounting_model_version<>2 THEN RETURN NEW; END IF;
    IF (NEW.merchant_id,NEW.token_id,NEW.source_wallet_id,NEW.dest_address,NEW.amount,NEW.date,NEW.basis_type,NEW.basis_id) IS DISTINCT FROM
       (OLD.merchant_id,OLD.token_id,OLD.source_wallet_id,OLD.dest_address,OLD.amount,OLD.date,OLD.basis_type,OLD.basis_id) OR NEW.deletion_mark OR NEW._deleted_at IS NOT NULL OR NEW.posted_version>1 OR (OLD.posted AND NOT NEW.posted) THEN
        RAISE EXCEPTION 'withdrawal execution basis is immutable' USING ERRCODE='23514'; END IF;
    IF OLD.settlement_status='recorded' AND (NEW.tx_hash,NEW.status,NEW.settlement_status,NEW.chain_result,NEW.network_fee,NEW.network_fee_token_id) IS DISTINCT FROM
        (OLD.tx_hash,OLD.status,OLD.settlement_status,OLD.chain_result,OLD.network_fee,OLD.network_fee_token_id) THEN RAISE EXCEPTION 'withdrawal final outcome is immutable' USING ERRCODE='23514'; END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_withdrawal_basis BEFORE UPDATE OR DELETE ON doc_crypto_withdrawals FOR EACH ROW EXECUTE FUNCTION fn_crypto_withdrawal_basis_guard();
CREATE TRIGGER trg_withdrawal_request_basis BEFORE UPDATE OR DELETE ON doc_withdrawal_requests FOR EACH ROW EXECUTE FUNCTION fn_crypto_withdrawal_basis_guard();

CREATE FUNCTION fn_crypto_withdrawal_reservation_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE d doc_crypto_withdrawals; balance NUMERIC; reserved NUMERIC; fees NUMERIC;
BEGIN
    IF TG_OP='INSERT' THEN
        IF NEW.document_type NOT IN ('WithdrawalRequest','CryptoAMLWithdrawalLine') THEN RETURN NEW; END IF;
        IF EXISTS(SELECT 1 FROM doc_crypto_withdrawals WHERE accounting_model_version=1 AND basis_type=NEW.document_type AND basis_id=NEW.document_id) THEN RETURN NEW; END IF;
        PERFORM fn_crypto_lock_receipt_balance(NEW.wallet_id,NEW.token_id);
        SELECT amount INTO STRICT balance FROM reg_crypto_balance_balances WHERE wallet_id=NEW.wallet_id AND token_id=NEW.token_id;
        SELECT COALESCE(sum(amount),0) INTO reserved FROM reg_crypto_balance_reservations WHERE wallet_id=NEW.wallet_id AND token_id=NEW.token_id AND status='active';
        SELECT COALESCE(sum(reserved_principal_minor+CASE WHEN strategy='resource_rental' THEN 0 ELSE max_fee_minor END),0) INTO fees FROM reg_crypto_fee_reservations
            WHERE payer_wallet_id=NEW.wallet_id AND native_token_id=NEW.token_id AND status IN ('reserved','broadcast','held');
        IF NEW.status<>'active' OR NEW.amount>balance-reserved-fees THEN RAISE EXCEPTION 'withdrawal reservation exceeds available principal' USING ERRCODE='23514'; END IF;
        RETURN NEW;
    END IF;
    SELECT * INTO d FROM doc_crypto_withdrawals WHERE accounting_model_version=2 AND basis_type=OLD.document_type AND basis_id=OLD.document_id;
    IF NOT FOUND AND TG_OP='UPDATE' THEN
        SELECT * INTO d FROM doc_crypto_withdrawals WHERE accounting_model_version=2 AND basis_type=NEW.document_type AND basis_id=NEW.document_id;
    END IF;
    IF NOT FOUND THEN IF TG_OP='DELETE' THEN RETURN OLD; END IF; RETURN NEW; END IF;
    IF TG_OP='DELETE' THEN RAISE EXCEPTION 'withdrawal reservation history is immutable' USING ERRCODE='23514'; END IF;
    IF (to_jsonb(NEW)-ARRAY['status','updated_at']) IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['status','updated_at']) OR (OLD.status<>'active' AND NEW.status<>OLD.status) THEN
        RAISE EXCEPTION 'withdrawal reservation basis and terminal status are immutable' USING ERRCODE='23514'; END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_withdrawal_reservation_basis BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_balance_reservations FOR EACH ROW EXECUTE FUNCTION fn_crypto_withdrawal_reservation_guard();
CREATE FUNCTION fn_crypto_withdrawal_reservation_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE d doc_crypto_withdrawals;
BEGIN
    SELECT * INTO d FROM doc_crypto_withdrawals WHERE accounting_model_version=2 AND basis_type=NEW.document_type AND basis_id=NEW.document_id;
    IF FOUND AND NEW.status<>'active' AND (d.tx_hash<>'' OR EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations WHERE basis_type=d.basis_type AND basis_id=d.basis_id AND (signed_envelope IS NOT NULL OR status<>'failed'))) THEN PERFORM fn_crypto_assert_withdrawal(d.id); END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER trg_withdrawal_reservation_complete AFTER INSERT OR UPDATE ON reg_crypto_balance_reservations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_withdrawal_reservation_complete();
CREATE FUNCTION fn_crypto_withdrawal_fee_release_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status='released' AND EXISTS(SELECT 1 FROM doc_crypto_withdrawals d WHERE d.accounting_model_version=2 AND d.basis_type=NEW.basis_type AND d.basis_id=NEW.basis_id)
       AND EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations WHERE basis_type=NEW.basis_type AND basis_id=NEW.basis_id AND signed_envelope IS NOT NULL) THEN
        RAISE EXCEPTION 'signed withdrawal fee requires a final outcome' USING ERRCODE='23514'; END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_withdrawal_fee_release BEFORE UPDATE ON reg_crypto_fee_reservations FOR EACH ROW EXECUTE FUNCTION fn_crypto_withdrawal_fee_release_guard();
CREATE FUNCTION fn_crypto_withdrawal_outflow_owner_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.recorder_type IN ('TreasuryWithdrawal','CryptoWithdrawal') AND EXISTS(
        WITH candidate AS (
            SELECT token_id,source_wallet_id,tx_hash,dest_address,amount FROM doc_treasury_withdrawals WHERE id=NEW.recorder_id AND NEW.recorder_type='TreasuryWithdrawal'
            UNION ALL SELECT token_id,source_wallet_id,tx_hash,dest_address,amount FROM doc_crypto_withdrawals WHERE id=NEW.recorder_id AND NEW.recorder_type='CryptoWithdrawal'
        ) SELECT 1 FROM candidate c JOIN cat_tokens t ON t.id=c.token_id JOIN cat_wallets w ON w.id=c.source_wallet_id JOIN cat_blockchain_networks n ON n.id=t.network_id
        JOIN reg_crypto_expected_chain_effects x ON x.network_id=n.id AND regexp_replace(lower(x.tx_hash),'^0x','')=regexp_replace(lower(c.tx_hash),'^0x','')
            AND x.source_address_key=w.address_key AND x.destination_address_key=CASE WHEN n.chain_family='evm' THEN lower(c.dest_address) ELSE c.dest_address END
            AND x.contract_address_key=CASE WHEN n.chain_family='evm' THEN lower(COALESCE(t.contract_address,'')) ELSE COALESCE(t.contract_address,'') END AND x.amount=c.amount
        JOIN reg_crypto_signing_authorizations a ON a.id=x.signing_authorization_id AND a.basis_type IN ('WithdrawalRequest','CryptoAMLWithdrawalLine') AND a.signed_envelope IS NOT NULL
        JOIN doc_crypto_withdrawals d ON d.basis_type=a.basis_type AND d.basis_id=a.basis_id AND d.accounting_model_version=2
        WHERE NEW.recorder_type<>'CryptoWithdrawal' OR NEW.recorder_id<>d.id) THEN
        RAISE EXCEPTION 'expected withdrawal principal requires its settlement owner' USING ERRCODE='23514';
    END IF;
    IF NEW.recorder_type='CryptoCustodyAdjustment' AND EXISTS(
        SELECT 1 FROM doc_crypto_custody_adjustments adjustment JOIN reg_crypto_external_outflows o ON o.id=adjustment.external_outflow_id
        JOIN reg_crypto_expected_chain_effects x ON x.network_id=o.network_id AND regexp_replace(lower(x.tx_hash),'^0x','')=regexp_replace(lower(o.tx_hash),'^0x','')
            AND x.event_kind=o.event_kind AND x.source_address_key=o.source_address_key AND x.destination_address_key=o.destination_address_key AND x.contract_address_key=o.contract_address_key AND x.amount=o.amount
        JOIN reg_crypto_signing_authorizations a ON a.id=x.signing_authorization_id AND a.basis_type IN ('WithdrawalRequest','CryptoAMLWithdrawalLine') AND a.signed_envelope IS NOT NULL
        JOIN doc_crypto_withdrawals d ON d.basis_type=a.basis_type AND d.basis_id=a.basis_id AND d.accounting_model_version=2 WHERE adjustment.id=NEW.recorder_id) THEN
        RAISE EXCEPTION 'expected withdrawal principal requires its settlement owner' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_withdrawal_outflow_owner BEFORE INSERT ON reg_crypto_balance_movements FOR EACH ROW EXECUTE FUNCTION fn_crypto_withdrawal_outflow_owner_guard();
CREATE FUNCTION fn_crypto_withdrawal_request_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    PERFORM fn_crypto_assert_withdrawal_parent(NEW.id);
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER trg_withdrawal_request_complete AFTER INSERT OR UPDATE ON doc_withdrawal_requests DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_withdrawal_request_complete();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN IF EXISTS(SELECT 1 FROM doc_crypto_withdrawals WHERE accounting_model_version=2) THEN RAISE EXCEPTION 'withdrawal accounting v2 requires forward migration'; END IF; END; $$;
CREATE OR REPLACE VIEW crypto_aml_valid_withdrawal_facts AS
 SELECT f.line_id FROM reg_crypto_aml_withdrawal_facts f JOIN reg_crypto_aml_withdrawal_lines l ON l.id=f.line_id AND l.wallet_id=f.wallet_id AND l.token_id=f.token_id AND l.amount=f.amount
 JOIN reg_crypto_aml_withdrawal_plans p ON p.request_id=l.request_id
 JOIN doc_crypto_withdrawals w ON w.id=l.withdrawal_id AND w.basis_type='CryptoAMLWithdrawalLine' AND w.basis_id=l.id AND w.merchant_id=p.merchant_id AND w.source_wallet_id=l.wallet_id AND w.token_id=l.token_id AND w.amount=l.amount AND w.posted AND w.posted_version=1
 JOIN reg_crypto_signing_authorizations a ON a.basis_type='CryptoAMLWithdrawalLine' AND a.basis_id=l.id AND a.generation=1 AND a.attempt=1 AND a.signed_envelope IS NOT NULL AND a.tx_hash=w.tx_hash AND a.source_address=l.source_address AND a.vault_key_id=l.source_vault_key_id AND a.amount_minor_units=l.amount
 JOIN reg_crypto_aml_first_dispatches dispatched ON dispatched.authorization_id=a.id
 JOIN reg_crypto_chain_settlements s ON s.id=f.settlement_id AND s.network_id=p.network_id AND s.basis_type='CryptoAMLWithdrawalLine' AND s.basis_id=l.id AND s.tx_hash=w.tx_hash AND (s.status='succeeded')=f.succeeded
 JOIN reg_crypto_balance_movements m ON m.line_id=f.expense_movement_id AND m.recorder_type='CryptoWithdrawal' AND m.recorder_id=w.id AND m.recorder_version=1 AND m.record_type='expense' AND m.wallet_id=f.wallet_id AND m.token_id=f.token_id AND m.amount=f.amount
 LEFT JOIN reg_crypto_balance_movements refund ON refund.line_id=f.refund_movement_id AND refund.line_id=l.id AND refund.recorder_type='AMLWithdrawalRevert' AND refund.recorder_id=l.id AND refund.recorder_version=1 AND refund.record_type='receipt' AND refund.wallet_id=f.wallet_id AND refund.token_id=f.token_id AND refund.amount=f.amount
 WHERE (f.succeeded OR (refund.line_id IS NOT NULL AND EXISTS(SELECT 1 FROM reg_crypto_merchant_balance_movements mr JOIN doc_withdrawal_requests request ON request.id=l.request_id WHERE mr.line_id=l.id AND mr.recorder_id=l.request_id AND mr.recorder_type='WithdrawalRequest' AND mr.recorder_version=request.posted_version+1 AND mr.record_type='receipt' AND mr.merchant_id=p.merchant_id AND mr.token_id=l.token_id AND mr.amount=l.amount))) AND (SELECT COUNT(*) FROM reg_crypto_balance_movements WHERE recorder_id=w.id AND recorder_type='CryptoWithdrawal')=1;
DROP TRIGGER trg_withdrawal_outflow_owner ON reg_crypto_balance_movements;
DROP FUNCTION fn_crypto_withdrawal_outflow_owner_guard();
DROP TRIGGER trg_withdrawal_request_complete ON doc_withdrawal_requests;
DROP FUNCTION fn_crypto_withdrawal_request_complete();
DROP TRIGGER trg_withdrawal_fee_release ON reg_crypto_fee_reservations;
DROP TRIGGER trg_withdrawal_reservation_complete ON reg_crypto_balance_reservations;
DROP TRIGGER trg_withdrawal_reservation_basis ON reg_crypto_balance_reservations;
DROP TRIGGER trg_withdrawal_request_basis ON doc_withdrawal_requests;
DROP TRIGGER trg_withdrawal_basis ON doc_crypto_withdrawals;
DROP TRIGGER trg_withdrawal_document_complete ON doc_crypto_withdrawals;
DROP TRIGGER trg_withdrawal_chain_complete ON reg_crypto_chain_settlements;
DROP TRIGGER trg_withdrawal_fee_complete ON reg_crypto_fee_reservations;
DROP TRIGGER trg_withdrawal_posting_complete ON reg_crypto_custody_effect_postings;
DO $$ DECLARE target TEXT; BEGIN
    FOREACH target IN ARRAY ARRAY['reg_crypto_balance_movements','reg_crypto_merchant_balance_movements','reg_crypto_fee_movements','reg_crypto_platform_cost_movements','reg_crypto_provider_balance_movements'] LOOP
        EXECUTE format('DROP TRIGGER %I ON %I','trg_withdrawal_complete_'||target,target);
    END LOOP;
END; $$;
DROP TABLE reg_crypto_withdrawal_custody_bindings;
DROP FUNCTION fn_crypto_withdrawal_fee_release_guard();
DROP FUNCTION fn_crypto_withdrawal_reservation_complete();
DROP FUNCTION fn_crypto_withdrawal_reservation_guard();
DROP FUNCTION fn_crypto_withdrawal_basis_guard();
DROP FUNCTION fn_crypto_withdrawal_settlement_complete();
DROP FUNCTION fn_crypto_withdrawal_financial_complete();
DROP FUNCTION fn_crypto_withdrawal_posting_complete();
DROP FUNCTION fn_crypto_withdrawal_binding_complete();
DROP FUNCTION fn_crypto_assert_withdrawal(UUID);
DROP FUNCTION fn_crypto_assert_withdrawal_parent(UUID);
ALTER TABLE reg_crypto_aml_withdrawal_facts DROP CONSTRAINT reg_crypto_aml_withdrawal_facts_check;
ALTER TABLE reg_crypto_aml_withdrawal_facts ALTER COLUMN expense_movement_id SET NOT NULL;
ALTER TABLE reg_crypto_aml_withdrawal_facts ADD CONSTRAINT reg_crypto_aml_withdrawal_facts_check CHECK(succeeded=(refund_movement_id IS NULL));
CREATE OR REPLACE FUNCTION fn_crypto_payment_posting_transaction() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    NEW.posting_txid:=CASE WHEN NEW.recorder_type IN ('CryptoPayment','CryptoNativeFunding','CryptoSweep','TreasuryWithdrawal') AND NEW.reversal_of_line_id IS NULL THEN txid_current() ELSE NULL END;
    RETURN NEW;
END;
$$;
ALTER TABLE doc_crypto_withdrawals DROP COLUMN accounting_model_version;
-- +goose StatementEnd
