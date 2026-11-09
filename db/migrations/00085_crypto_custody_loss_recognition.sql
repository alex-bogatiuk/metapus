-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE doc_crypto_custody_adjustments ADD COLUMN accounting_model_version INT NOT NULL DEFAULT 1 CHECK(accounting_model_version IN (1,2));
ALTER TABLE doc_crypto_custody_adjustments ALTER COLUMN accounting_model_version SET DEFAULT 2;
CREATE TABLE reg_crypto_custody_loss_bindings (
    posting_id UUID PRIMARY KEY REFERENCES reg_crypto_custody_effect_postings(id),
    adjustment_id UUID NOT NULL UNIQUE REFERENCES doc_crypto_custody_adjustments(id),
    external_outflow_id UUID NOT NULL UNIQUE REFERENCES reg_crypto_external_outflows(id),
    maker_id UUID NOT NULL,
    checker_id UUID NOT NULL CHECK(checker_id<>maker_id),
    period TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TRIGGER trg_custody_loss_binding_immutable BEFORE UPDATE OR DELETE ON reg_crypto_custody_loss_bindings FOR EACH ROW EXECUTE FUNCTION fn_crypto_financial_movement_immutable();
CREATE OR REPLACE FUNCTION fn_crypto_payment_posting_transaction() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    NEW.posting_txid:=CASE WHEN NEW.recorder_type IN ('CryptoPayment','CryptoNativeFunding','CryptoSweep','TreasuryWithdrawal','CryptoWithdrawal','CryptoCustodyAdjustment') AND NEW.reversal_of_line_id IS NULL THEN txid_current() ELSE NULL END;
    RETURN NEW;
END;
$$;

CREATE FUNCTION fn_crypto_lock_outflow_economics(network UUID, hash TEXT, source TEXT, destination TEXT, contract TEXT, amount NUMERIC) RETURNS VOID LANGUAGE sql AS $$
    SELECT pg_advisory_xact_lock(hashtextextended(jsonb_build_array('custody-outflow',network,regexp_replace(lower(hash),'^0x',''),source,destination,contract,amount::text)::text,0));
$$;

CREATE FUNCTION fn_crypto_custody_loss_conflicting_owner(target UUID, adjustment UUID) RETURNS BOOLEAN LANGUAGE sql AS $$
    SELECT (EXISTS(SELECT 1 FROM cat_wallets recipient WHERE recipient.network_id=o.network_id AND recipient.address_key=o.destination_address_key)
       OR EXISTS(SELECT 1 FROM reg_crypto_expected_chain_effects x JOIN reg_crypto_signing_authorizations a ON a.id=x.signing_authorization_id
           WHERE x.network_id=o.network_id AND regexp_replace(lower(x.tx_hash),'^0x','')=regexp_replace(lower(o.tx_hash),'^0x','') AND x.event_kind=o.event_kind
             AND x.source_address_key=o.source_address_key AND x.destination_address_key=o.destination_address_key AND x.contract_address_key=o.contract_address_key AND x.amount=o.amount
             AND a.signed_envelope IS NOT NULL)
       OR EXISTS(SELECT 1 FROM doc_crypto_withdrawals prior JOIN cat_wallets w ON w.id=prior.source_wallet_id JOIN cat_tokens t ON t.id=prior.token_id JOIN cat_blockchain_networks n ON n.id=w.network_id
           WHERE prior.posted AND w.network_id=o.network_id AND w.address_key=o.source_address_key AND prior.amount=o.amount
             AND regexp_replace(lower(prior.tx_hash),'^0x','')=regexp_replace(lower(o.tx_hash),'^0x','')
             AND o.destination_address_key=CASE WHEN n.chain_family='evm' THEN lower(prior.dest_address) ELSE prior.dest_address END
             AND o.contract_address_key=CASE WHEN n.chain_family='evm' THEN lower(COALESCE(t.contract_address,'')) ELSE COALESCE(t.contract_address,'') END)
       OR EXISTS(SELECT 1 FROM doc_treasury_withdrawals prior JOIN cat_wallets w ON w.id=prior.source_wallet_id JOIN cat_tokens t ON t.id=prior.token_id JOIN cat_blockchain_networks n ON n.id=w.network_id
           WHERE prior.posted AND w.network_id=o.network_id AND w.address_key=o.source_address_key AND prior.amount=o.amount
             AND regexp_replace(lower(prior.tx_hash),'^0x','')=regexp_replace(lower(o.tx_hash),'^0x','')
             AND o.destination_address_key=CASE WHEN n.chain_family='evm' THEN lower(prior.dest_address) ELSE prior.dest_address END
             AND o.contract_address_key=CASE WHEN n.chain_family='evm' THEN lower(COALESCE(t.contract_address,'')) ELSE COALESCE(t.contract_address,'') END)
       OR EXISTS(SELECT 1 FROM doc_crypto_custody_adjustments prior JOIN reg_crypto_external_outflows old ON old.id=prior.external_outflow_id
           WHERE prior.id<>adjustment AND prior.status='posted' AND old.network_id=o.network_id AND regexp_replace(lower(old.tx_hash),'^0x','')=regexp_replace(lower(o.tx_hash),'^0x','')
             AND old.source_address_key=o.source_address_key AND old.destination_address_key=o.destination_address_key AND old.contract_address_key=o.contract_address_key AND old.amount=o.amount)) FROM reg_crypto_external_outflows o WHERE o.id=target;
$$;

CREATE FUNCTION fn_crypto_assert_custody_loss(target UUID) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE d doc_crypto_custody_adjustments; o reg_crypto_external_outflows; b reg_crypto_custody_loss_bindings; p reg_crypto_custody_effect_postings;
BEGIN
    SELECT * INTO STRICT d FROM doc_crypto_custody_adjustments WHERE id=target;
    IF d.accounting_model_version<>2 THEN RETURN; END IF;
    SELECT * INTO STRICT o FROM reg_crypto_external_outflows WHERE id=d.external_outflow_id;
    SELECT * INTO b FROM reg_crypto_custody_loss_bindings WHERE adjustment_id=d.id;
    SELECT * INTO p FROM reg_crypto_custody_effect_postings WHERE id=b.posting_id;
    IF d.status<>'posted' OR d.posted_version<>1 OR b.posting_id IS NULL OR b.external_outflow_id<>o.id OR b.maker_id<>d.maker_id OR b.checker_id IS DISTINCT FROM d.decision_by
       OR d.decision_by=d.maker_id OR b.period IS NULL OR d.decided_at IS NULL OR btrim(d.decision_reason)=''
       OR d.network_id<>o.network_id OR d.wallet_id IS DISTINCT FROM o.source_wallet_id OR d.token_id IS DISTINCT FROM o.token_id OR d.amount<>o.amount
       OR o.status NOT IN ('classified','resolved') OR o.classification<>'unauthorized_loss' OR o.finality_status NOT IN ('confirmed','finalized')
       OR o.confirmations<GREATEST(o.required_confs,(SELECT confirmations_needed FROM cat_blockchain_networks WHERE id=d.network_id))
       OR p.recorder_type<>'CryptoCustodyAdjustment' OR p.recorder_id<>d.id OR p.recorder_version<>1 OR p.generation<>1
       OR (SELECT count(*) FROM reg_crypto_custody_effect_recognitions WHERE posting_id=p.id)<>1
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_recognitions r WHERE r.posting_id=p.id AND r.wallet_id=d.wallet_id AND r.token_id=d.token_id AND r.record_type='expense')
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_accounting_effects effect JOIN reg_crypto_accounting_effect_occurrences occurrence ON occurrence.effect_id=effect.id
           JOIN reg_crypto_transfer_verifications proof ON proof.id=p.verification_id JOIN cat_wallets w ON w.id=d.wallet_id JOIN cat_tokens t ON t.id=d.token_id
           JOIN cat_blockchain_networks n ON n.id=d.network_id
           WHERE occurrence.id=p.occurrence_id AND effect.network_id=d.network_id AND effect.tx_hash=regexp_replace(lower(o.tx_hash),'^0x','')
             AND effect.event_kind=o.event_kind AND proof.event_index=o.event_index AND occurrence.source_address=o.source_address_key AND occurrence.destination_address=o.destination_address_key
             AND occurrence.contract_address=o.contract_address_key AND occurrence.amount=d.amount AND occurrence.block_hash=regexp_replace(lower(o.block_hash),'^0x','') AND occurrence.block_number=o.block_number
             AND w.network_id=d.network_id AND t.network_id=d.network_id AND w.address_key=o.source_address_key AND w.vault_key_id<>''
             AND o.contract_address_key=CASE WHEN n.chain_family='evm' THEN lower(COALESCE(t.contract_address,'')) ELSE COALESCE(t.contract_address,'') END
             AND proof.confirmations>=GREATEST(o.required_confs,n.confirmations_needed))
       OR fn_crypto_custody_loss_conflicting_owner(o.id,d.id)
       OR (SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=d.id)<>1
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_balance_movements m JOIN reg_crypto_custody_effect_recognitions r ON r.movement_line_id=m.line_id
           WHERE r.posting_id=p.id AND m.recorder_id=d.id AND m.recorder_type='CryptoCustodyAdjustment' AND m.recorder_version=1 AND m.record_type='expense'
             AND m.wallet_id=d.wallet_id AND m.token_id=d.token_id AND m.amount=d.amount AND m.period=b.period AND m.reversal_of_line_id IS NULL)
       OR (SELECT count(*) FROM reg_crypto_platform_cost_movements WHERE recorder_id=d.id)<>1
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_platform_cost_movements WHERE recorder_id=d.id AND recorder_type='CryptoCustodyAdjustment' AND recorder_version=1 AND record_type='expense'
           AND network_id=d.network_id AND operation_token_id=d.token_id AND cost_token_id=d.token_id AND cost_type='custody_loss' AND purpose='custody_loss'
           AND provider='custody' AND external_order_id=o.id::text AND amount=d.amount AND period=b.period AND reversal_of_line_id IS NULL)
       OR EXISTS(SELECT 1 FROM reg_crypto_merchant_balance_movements WHERE recorder_id=d.id)
       OR EXISTS(SELECT 1 FROM reg_crypto_fee_movements WHERE recorder_id=d.id)
       OR EXISTS(SELECT 1 FROM reg_crypto_provider_balance_movements WHERE recorder_id=d.id)
       OR (SELECT count(*) FROM reg_crypto_external_outflow_actions WHERE external_outflow_id=o.id AND action='custody_adjustment_posted')<>1
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_external_outflow_actions WHERE external_outflow_id=o.id AND actor_type='user' AND actor_id=d.decision_by::text
           AND action='custody_adjustment_posted' AND old_state='pending_approval' AND new_state='posted' AND reason=d.decision_reason
           AND evidence->>'adjustmentId'=d.id::text AND evidence->>'postedVersion'='1')
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_reconciliation_requests WHERE source_type='custody_adjustment' AND source_id=d.id AND network_id=d.network_id AND wallet_id=d.wallet_id AND token_id=d.token_id)
       OR (SELECT count(*) FROM sys_outbox WHERE aggregate_type='CryptoCustodyAdjustment' AND aggregate_id=d.id AND event_type='custody_recognized')<>1
       OR NOT EXISTS(SELECT 1 FROM sys_outbox WHERE aggregate_type='CryptoCustodyAdjustment' AND aggregate_id=d.id AND event_type='custody_recognized'
           AND payload->>'entityId'=d.id::text AND payload->>'entityType'='CryptoCustodyAdjustment' AND payload->>'action'='custody_recognized'
           AND payload->>'externalOutflowId'=o.id::text AND payload->>'postingId'=p.id::text AND payload->>'generation'='1') THEN
        RAISE EXCEPTION 'custody loss requires one proven external expense, checker, loss cost, audit, reconciliation and outbox' USING ERRCODE='23514';
    END IF;
END;
$$;

CREATE FUNCTION fn_crypto_custody_loss_binding_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings WHERE id=NEW.posting_id AND recorder_type='CryptoCustodyAdjustment' AND recorder_id=NEW.adjustment_id)
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_recognitions r JOIN reg_crypto_balance_movements m ON m.line_id=r.movement_line_id
           WHERE r.posting_id=NEW.posting_id AND m.posting_txid=txid_current()) THEN
        RAISE EXCEPTION 'custody loss requires its new principal in the same transaction' USING ERRCODE='23514'; END IF;
    PERFORM fn_crypto_assert_custody_loss(NEW.adjustment_id); RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER trg_custody_loss_binding_complete AFTER INSERT ON reg_crypto_custody_loss_bindings DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_loss_binding_complete();
CREATE FUNCTION fn_crypto_custody_loss_posting_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target UUID;
BEGIN
    IF NEW.recorder_type<>'CryptoCustodyAdjustment' THEN RETURN NULL; END IF;
    SELECT adjustment_id INTO target FROM reg_crypto_custody_loss_bindings WHERE posting_id=NEW.id;
    IF NOT FOUND THEN RAISE EXCEPTION 'custody loss principal requires its recognition binding' USING ERRCODE='23514'; END IF;
    PERFORM fn_crypto_assert_custody_loss(target); RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER trg_custody_loss_posting_complete AFTER INSERT ON reg_crypto_custody_effect_postings DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_loss_posting_complete();
CREATE FUNCTION fn_crypto_custody_loss_financial_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS(SELECT 1 FROM doc_crypto_custody_adjustments WHERE id=NEW.recorder_id AND accounting_model_version=2) THEN PERFORM fn_crypto_assert_custody_loss(NEW.recorder_id); END IF;
    IF NEW.reversal_of_line_id IS NOT NULL AND (EXISTS(SELECT 1 FROM reg_crypto_custody_effect_recognitions r JOIN reg_crypto_custody_loss_bindings b ON b.posting_id=r.posting_id WHERE r.movement_line_id=NEW.reversal_of_line_id)
        OR (TG_TABLE_NAME='reg_crypto_platform_cost_movements' AND EXISTS(SELECT 1 FROM reg_crypto_platform_cost_movements m JOIN reg_crypto_custody_loss_bindings b ON b.adjustment_id=m.recorder_id WHERE m.line_id=NEW.reversal_of_line_id))) THEN
        RAISE EXCEPTION 'custody loss requires explicit compensation workflow' USING ERRCODE='23514'; END IF;
    RETURN NULL;
END;
$$;
DO $$ DECLARE target TEXT; BEGIN
    FOREACH target IN ARRAY ARRAY['reg_crypto_balance_movements','reg_crypto_platform_cost_movements','reg_crypto_merchant_balance_movements','reg_crypto_fee_movements','reg_crypto_provider_balance_movements'] LOOP
        EXECUTE format('CREATE CONSTRAINT TRIGGER %I AFTER INSERT ON %I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_loss_financial_complete()','trg_custody_loss_complete_'||target,target);
    END LOOP;
END; $$;
CREATE FUNCTION fn_crypto_custody_loss_document_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.accounting_model_version=2 AND NEW.status='posted' THEN PERFORM fn_crypto_assert_custody_loss(NEW.id); END IF; RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER trg_custody_loss_document_complete AFTER INSERT OR UPDATE ON doc_crypto_custody_adjustments DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_loss_document_complete();
CREATE FUNCTION fn_crypto_custody_loss_basis_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.accounting_model_version<>OLD.accounting_model_version OR (OLD.accounting_model_version=2 AND NEW.created_at<>OLD.created_at) THEN
        RAISE EXCEPTION 'custody loss accounting model and basis are immutable' USING ERRCODE='23514'; END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_custody_loss_basis BEFORE UPDATE ON doc_crypto_custody_adjustments FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_loss_basis_guard();

-- Serialize equivalent economics independently of catalog aliases and recorder.
CREATE FUNCTION fn_crypto_custody_loss_outflow_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE network UUID; hash TEXT; source TEXT; destination TEXT; contract TEXT; owner_amount NUMERIC;
BEGIN
    IF NEW.recorder_type NOT IN ('TreasuryWithdrawal','CryptoWithdrawal','CryptoCustodyAdjustment') THEN RETURN NEW; END IF;
    IF NEW.recorder_type='CryptoCustodyAdjustment' THEN
        SELECT o.network_id,o.tx_hash,o.source_address_key,o.destination_address_key,o.contract_address_key,o.amount INTO network,hash,source,destination,contract,owner_amount
            FROM doc_crypto_custody_adjustments a JOIN reg_crypto_external_outflows o ON o.id=a.external_outflow_id WHERE a.id=NEW.recorder_id;
    ELSE
        WITH candidate AS (
            SELECT token_id,source_wallet_id,tx_hash,dest_address,amount FROM doc_treasury_withdrawals WHERE id=NEW.recorder_id AND NEW.recorder_type='TreasuryWithdrawal'
            UNION ALL SELECT token_id,source_wallet_id,tx_hash,dest_address,amount FROM doc_crypto_withdrawals WHERE id=NEW.recorder_id AND NEW.recorder_type='CryptoWithdrawal'
        ) SELECT w.network_id,c.tx_hash,w.address_key,CASE WHEN n.chain_family='evm' THEN lower(c.dest_address) ELSE c.dest_address END,
            CASE WHEN n.chain_family='evm' THEN lower(COALESCE(t.contract_address,'')) ELSE COALESCE(t.contract_address,'') END,c.amount INTO network,hash,source,destination,contract,owner_amount
            FROM candidate c JOIN cat_wallets w ON w.id=c.source_wallet_id JOIN cat_tokens t ON t.id=c.token_id JOIN cat_blockchain_networks n ON n.id=w.network_id;
    END IF;
    IF network IS NULL THEN RETURN NEW; END IF;
    PERFORM fn_crypto_lock_outflow_economics(network,hash,source,destination,contract,owner_amount);
    IF EXISTS(SELECT 1 FROM reg_crypto_custody_loss_bindings binding JOIN reg_crypto_custody_effect_postings p ON p.id=binding.posting_id
        JOIN reg_crypto_accounting_effects e ON e.id=p.effect_id JOIN reg_crypto_accounting_effect_occurrences o ON o.id=p.occurrence_id
        WHERE e.network_id=network AND e.tx_hash=regexp_replace(lower(hash),'^0x','') AND o.source_address=source AND o.destination_address=destination AND o.contract_address=contract AND o.amount=owner_amount
          AND (NEW.recorder_type<>'CryptoCustodyAdjustment' OR NEW.recorder_id<>binding.adjustment_id)) THEN
        RAISE EXCEPTION 'recognized custody loss already owns this outgoing principal' USING ERRCODE='23514'; END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_custody_loss_outflow_owner BEFORE INSERT ON reg_crypto_balance_movements FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_loss_outflow_guard();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN IF EXISTS(SELECT 1 FROM doc_crypto_custody_adjustments WHERE accounting_model_version=2) THEN RAISE EXCEPTION 'custody loss recognition v2 requires forward migration'; END IF; END; $$;
DROP TRIGGER trg_custody_loss_outflow_owner ON reg_crypto_balance_movements;
DROP TRIGGER trg_custody_loss_basis ON doc_crypto_custody_adjustments;
DROP TRIGGER trg_custody_loss_document_complete ON doc_crypto_custody_adjustments;
DROP TRIGGER trg_custody_loss_posting_complete ON reg_crypto_custody_effect_postings;
DO $$ DECLARE target TEXT; BEGIN
    FOREACH target IN ARRAY ARRAY['reg_crypto_balance_movements','reg_crypto_platform_cost_movements','reg_crypto_merchant_balance_movements','reg_crypto_fee_movements','reg_crypto_provider_balance_movements'] LOOP
        EXECUTE format('DROP TRIGGER %I ON %I','trg_custody_loss_complete_'||target,target);
    END LOOP;
END; $$;
DROP TABLE reg_crypto_custody_loss_bindings;
DROP FUNCTION fn_crypto_custody_loss_outflow_guard();
DROP FUNCTION fn_crypto_custody_loss_basis_guard();
DROP FUNCTION fn_crypto_custody_loss_document_complete();
DROP FUNCTION fn_crypto_custody_loss_financial_complete();
DROP FUNCTION fn_crypto_custody_loss_posting_complete();
DROP FUNCTION fn_crypto_custody_loss_binding_complete();
DROP FUNCTION fn_crypto_assert_custody_loss(UUID);
DROP FUNCTION fn_crypto_custody_loss_conflicting_owner(UUID,UUID);
DROP FUNCTION fn_crypto_lock_outflow_economics(UUID,TEXT,TEXT,TEXT,TEXT,NUMERIC);
CREATE OR REPLACE FUNCTION fn_crypto_payment_posting_transaction() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    NEW.posting_txid:=CASE WHEN NEW.recorder_type IN ('CryptoPayment','CryptoNativeFunding','CryptoSweep','TreasuryWithdrawal','CryptoWithdrawal') AND NEW.reversal_of_line_id IS NULL THEN txid_current() ELSE NULL END;
    RETURN NEW;
END;
$$;
ALTER TABLE doc_crypto_custody_adjustments DROP COLUMN accounting_model_version;
-- +goose StatementEnd
