-- Crypto custody accounting: effect identity, receipt/suspense, recognition registry,
-- final-settlement models for outbound writers, network fee proofs, expected routing,
-- resource-ordering helpers and immutable command bases.
-- Consolidated from former migrations 00077-00104 (no production data existed).

-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- Function bodies are validated when first executed; definitions precede the tables they use.

SET LOCAL check_function_bodies = off;



-- ---- Functions ----

-- Independent of transfer evidence/signing byte schemas. The preimage matches
-- AccountingEffectIdentity.Key and is protected by cross-language golden tests.
CREATE FUNCTION fn_crypto_accounting_identity_bytes(
    family TEXT, network UUID, tx TEXT, kind TEXT, ordinal BIGINT, trace TEXT
) RETURNS TEXT LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT format(
        '{"schemaVersion":1,"family":"%s","networkId":"%s","txHash":"%s","eventKind":"%s","ordinal":%s%s}',
        family,network,tx,kind,ordinal,
        CASE WHEN trace='' THEN '' ELSE format(',"traceHash":"%s"',trace) END
    );
$$;

CREATE FUNCTION fn_crypto_accounting_immutable() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'accounting effect evidence is immutable' USING ERRCODE='23514';
END;
$$;

CREATE FUNCTION fn_crypto_accounting_evidence_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    proof reg_crypto_transfer_verifications;
    occurrence reg_crypto_accounting_effect_occurrences;
    effect reg_crypto_accounting_effects;
    identity JSONB;
    event JSONB;
    from_key TEXT;
    to_key TEXT;
    contract_key TEXT;
BEGIN
    SELECT * INTO STRICT proof FROM reg_crypto_transfer_verifications WHERE id=NEW.verification_id;
    SELECT * INTO STRICT occurrence FROM reg_crypto_accounting_effect_occurrences WHERE id=NEW.occurrence_id;
    SELECT * INTO STRICT effect FROM reg_crypto_accounting_effects WHERE id=occurrence.effect_id FOR UPDATE;
    identity:=proof.evidence->'accountingIdentity';
    event:=proof.evidence->'event';
    from_key:=event->>'fromAddress';
    to_key:=event->>'toAddress';
    contract_key:=event->>'tokenContract';
    IF effect.family='evm' THEN
        from_key:=lower(from_key);
        to_key:=lower(to_key);
        contract_key:=lower(contract_key);
    END IF;
    IF identity IS NULL OR jsonb_typeof(identity)<>'object'
       OR identity IS DISTINCT FROM
          (jsonb_build_object('schemaVersion',1,'family',effect.family,'networkId',effect.network_id,
             'txHash',effect.tx_hash,'eventKind',effect.event_kind,'ordinal',effect.effect_ordinal)
           || CASE WHEN effect.trace_hash='' THEN '{}'::jsonb ELSE jsonb_build_object('traceHash',effect.trace_hash) END)
       OR proof.network_id<>effect.network_id
       OR regexp_replace(lower(proof.tx_hash),'^0x','')<>effect.tx_hash
       OR proof.event_kind<>effect.event_kind
       OR proof.block_number<>occurrence.block_number
       OR regexp_replace(lower(proof.block_hash),'^0x','')<>occurrence.block_hash
       OR (event->>'networkId') IS DISTINCT FROM effect.network_id::text
       OR regexp_replace(lower(event->>'txHash'),'^0x','') IS DISTINCT FROM effect.tx_hash
       OR (event->>'eventKind') IS DISTINCT FROM effect.event_kind
       OR (event->>'eventIndex')::bigint IS DISTINCT FROM proof.event_index
       OR (event->>'blockNumber')::bigint IS DISTINCT FROM occurrence.block_number
       OR regexp_replace(lower(event->>'blockHash'),'^0x','') IS DISTINCT FROM occurrence.block_hash
       OR (event->>'amount')::numeric IS DISTINCT FROM occurrence.amount
       OR from_key IS DISTINCT FROM occurrence.source_address
       OR to_key IS DISTINCT FROM occurrence.destination_address
       OR contract_key IS DISTINCT FROM occurrence.contract_address
       OR occurrence.economics_hash<>encode(digest(format(
          '{"schemaVersion":1,"identity":%s,"from":%s,"to":%s,"contract":%s,"amount":%s}',
          fn_crypto_accounting_identity_bytes(effect.family,effect.network_id,effect.tx_hash,
             effect.event_kind,effect.effect_ordinal,effect.trace_hash),
          to_jsonb(from_key)::text,to_jsonb(to_key)::text,to_jsonb(contract_key)::text,
          to_jsonb(occurrence.amount::text)::text),'sha256'),'hex')
       OR (event->>'canonicality') IS DISTINCT FROM 'canonical'
       OR (event->>'finality') IS DISTINCT FROM 'solidified'
       OR (proof.evidence->>'outcome') IS DISTINCT FROM 'verified'
       OR (proof.evidence->>'executionStatus') IS DISTINCT FROM 'succeeded'
       OR EXISTS(SELECT 1 FROM reg_crypto_accounting_effect_evidence prior
          JOIN reg_crypto_transfer_verifications previous ON previous.id=prior.verification_id
          WHERE prior.occurrence_id=NEW.occurrence_id AND previous.event_index<>proof.event_index)
       OR NOT EXISTS(SELECT 1 FROM cat_blockchain_networks n WHERE n.id=effect.network_id AND n.chain_family=effect.family)
       OR (effect.event_kind='contract_log')<>(occurrence.contract_address<>'')
       OR (effect.family='tron' AND
            ((proof.evidence->>'decoderVersion')::int IS DISTINCT FROM 2
             OR (effect.event_kind<>'internal_value' AND proof.event_index<>effect.effect_ordinal)
             OR (effect.event_kind='internal_value' AND
                 (regexp_replace(lower(event->>'traceHash'),'^0x','') IS DISTINCT FROM effect.trace_hash
                  OR (event->>'callValueIndex')::bigint IS DISTINCT FROM effect.effect_ordinal))))
       OR (effect.family='evm' AND
            ((proof.evidence->>'decoderVersion')::int IS DISTINCT FROM 1
             OR proof.event_index<effect.effect_ordinal
             OR (effect.event_kind='native_value' AND proof.event_index<>0))) THEN
        RAISE EXCEPTION 'accounting identity does not match verified occurrence' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

-- No detached, unproven identity/occurrence can survive a commit.
CREATE FUNCTION fn_crypto_accounting_coverage_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME='reg_crypto_accounting_effects' THEN
        IF NOT EXISTS(SELECT 1 FROM reg_crypto_accounting_effect_occurrences o
            JOIN reg_crypto_accounting_effect_evidence e ON e.occurrence_id=o.id WHERE o.effect_id=NEW.id) THEN
            RAISE EXCEPTION 'accounting effect requires verified occurrence' USING ERRCODE='23514';
        END IF;
    ELSIF NOT EXISTS(SELECT 1 FROM reg_crypto_accounting_effect_evidence e WHERE e.occurrence_id=NEW.id) THEN
        RAISE EXCEPTION 'accounting occurrence requires verified evidence' USING ERRCODE='23514';
    END IF;
    RETURN NULL;
END;
$$;

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

-- The receipt adds its gross once; attributions (manual credits and payments)
-- consume it, and a payment compensation returns its consumption.
CREATE FUNCTION fn_crypto_receipt_suspense_apply() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE original reg_crypto_receipt_suspense_movements; delta NUMERIC;
BEGIN
    IF NEW.reversal_of_line_id IS NOT NULL THEN
        SELECT * INTO original FROM reg_crypto_receipt_suspense_movements WHERE line_id=NEW.reversal_of_line_id FOR UPDATE;
        IF NOT FOUND OR original.recorder_type<>'CryptoPayment' OR original.reversal_of_line_id IS NOT NULL
           OR original.receipt_id<>NEW.receipt_id OR original.token_id<>NEW.token_id OR original.record_type=NEW.record_type
           OR original.amount<(SELECT SUM(amount) FROM reg_crypto_receipt_suspense_movements WHERE reversal_of_line_id=original.line_id) THEN
            RAISE EXCEPTION 'suspense reversal requires an available payment attribution' USING ERRCODE='23514';
        END IF;
        delta:=NEW.amount;
    ELSIF NEW.recorder_type='CryptoCustodyReceipt' THEN
        IF NOT EXISTS(SELECT 1 FROM doc_crypto_custody_receipts d WHERE d.id=NEW.receipt_id
            AND d.id=NEW.recorder_id AND d.token_id=NEW.token_id AND d.amount=NEW.amount
            AND d.suspense_line_id=NEW.line_id AND d.date=NEW.period) THEN
            RAISE EXCEPTION 'suspense movement requires its exact receipt basis' USING ERRCODE='23514';
        END IF;
        INSERT INTO reg_crypto_receipt_suspense_balances(receipt_id,token_id,amount,last_movement_at)
        VALUES(NEW.receipt_id,NEW.token_id,NEW.amount,NEW.period);
        RETURN NEW;
    ELSIF NEW.recorder_type='CryptoMerchantCredit' THEN
        IF NOT EXISTS(SELECT 1 FROM doc_crypto_merchant_credits c WHERE c.id=NEW.recorder_id
            AND c.receipt_id=NEW.receipt_id AND c.token_id=NEW.token_id AND c.amount=NEW.amount
            AND c.suspense_line_id=NEW.line_id AND c.date=NEW.period) THEN
            RAISE EXCEPTION 'suspense consumption requires its exact merchant credit' USING ERRCODE='23514';
        END IF;
        delta:=-NEW.amount;
    ELSE
        -- The payment receives its receipt in the posting callback; the
        -- deferred attribution assertion checks the exact basis at commit.
        delta:=-NEW.amount;
    END IF;
    IF delta<0 AND EXISTS(SELECT 1 FROM reg_crypto_custody_receipt_audits WHERE receipt_id=NEW.receipt_id AND status='orphaned') THEN
        RAISE EXCEPTION 'orphaned receipt cannot be attributed' USING ERRCODE='23514';
    END IF;
    -- The CHECK(amount>=0) on the balance rejects attribution above the remainder.
    UPDATE reg_crypto_receipt_suspense_balances SET amount=amount+delta,last_movement_at=NEW.period,updated_at=NOW()
        WHERE receipt_id=NEW.receipt_id AND token_id=NEW.token_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'suspense consumption requires a recorded receipt' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;


CREATE FUNCTION fn_crypto_custody_receipt_audit_seed() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO reg_crypto_custody_receipt_audits(receipt_id) VALUES(NEW.id);
    RETURN NULL;
END;
$$;

CREATE FUNCTION fn_crypto_custody_receipt_audit_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' OR OLD.status='orphaned' OR NEW.receipt_id<>OLD.receipt_id THEN
        RAISE EXCEPTION 'receipt canonicality outcome is immutable' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

-- The fee is the merchant tariff snapshot; the approval requirement follows
-- the token policy. The custody owner of a merchant-owned wallet is the only
-- merchant that may receive its receipt.
CREATE FUNCTION fn_crypto_merchant_credit_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE r doc_crypto_custody_receipts; fee NUMERIC; threshold NUMERIC;
BEGIN
    IF TG_OP='DELETE' THEN RAISE EXCEPTION 'merchant credit history is immutable' USING ERRCODE='23514'; END IF;
    IF TG_OP='UPDATE' THEN
        IF OLD.status<>'prepared' OR NEW.status='prepared'
           OR (to_jsonb(NEW)-ARRAY['status','posted','posted_version','decided_by','decided_at','decision_reason','version','updated_at','updated_by','_txid'])
              IS DISTINCT FROM
              (to_jsonb(OLD)-ARRAY['status','posted','posted_version','decided_by','decided_at','decision_reason','version','updated_at','updated_by','_txid']) THEN
            RAISE EXCEPTION 'merchant credit only permits its single decision' USING ERRCODE='23514';
        END IF;
        IF NEW.status='posted' AND EXISTS(SELECT 1 FROM reg_crypto_custody_receipt_audits WHERE receipt_id=NEW.receipt_id AND status='orphaned') THEN
            RAISE EXCEPTION 'orphaned receipt cannot be attributed' USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;
    SELECT * INTO r FROM doc_crypto_custody_receipts WHERE id=NEW.receipt_id;
    IF EXISTS(SELECT 1 FROM reg_crypto_custody_receipt_audits WHERE receipt_id=NEW.receipt_id AND status='orphaned') THEN
        RAISE EXCEPTION 'orphaned receipt cannot be attributed' USING ERRCODE='23514';
    END IF;
    IF NOT FOUND OR NOT r.posted OR r.token_id<>NEW.token_id
       OR (r.custody_merchant_id IS NOT NULL AND r.custody_merchant_id<>NEW.merchant_id)
       OR (NEW.invoice_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM doc_crypto_invoices i
            WHERE i.id=NEW.invoice_id AND i.merchant_id=NEW.merchant_id AND i.token_id=NEW.token_id)) THEN
        RAISE EXCEPTION 'merchant credit requires its receipt, custody owner and invoice' USING ERRCODE='23514';
    END IF;
    fee:=NEW.fee_fixed+trunc(NEW.amount*NEW.fee_percent_bp/10000);
    IF NEW.fee_min>0 THEN fee:=greatest(fee,NEW.fee_min); END IF;
    IF NEW.fee_max>0 THEN fee:=least(fee,NEW.fee_max); END IF;
    IF fee<>NEW.processing_fee THEN
        RAISE EXCEPTION 'merchant credit fee must follow its tariff snapshot' USING ERRCODE='23514';
    END IF;
    SELECT dual_approval_threshold INTO threshold FROM sys_crypto_receipt_credit_policies WHERE token_id=NEW.token_id;
    IF NEW.approval_required IS DISTINCT FROM (threshold IS NULL OR NEW.amount>threshold) THEN
        RAISE EXCEPTION 'merchant credit approval requirement follows the token policy' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION fn_crypto_assert_merchant_credit(target UUID) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE c doc_crypto_merchant_credits;
BEGIN
    SELECT * INTO c FROM doc_crypto_merchant_credits WHERE id=target;
    IF NOT FOUND THEN RAISE EXCEPTION 'merchant credit movements require their document' USING ERRCODE='23514'; END IF;
    IF NOT c.posted THEN
        IF EXISTS(SELECT 1 FROM reg_crypto_receipt_suspense_movements WHERE recorder_id=c.id)
           OR EXISTS(SELECT 1 FROM reg_crypto_merchant_balance_movements WHERE recorder_id=c.id)
           OR EXISTS(SELECT 1 FROM reg_crypto_fee_movements WHERE recorder_id=c.id) THEN
            RAISE EXCEPTION 'unposted merchant credit cannot move money' USING ERRCODE='23514';
        END IF;
        RETURN;
    END IF;
    IF (SELECT count(*) FROM reg_crypto_receipt_suspense_movements WHERE recorder_id=c.id)<>1
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_receipt_suspense_movements WHERE line_id=c.suspense_line_id AND recorder_id=c.id
            AND recorder_type='CryptoMerchantCredit' AND recorder_version=1 AND record_type='expense'
            AND receipt_id=c.receipt_id AND token_id=c.token_id AND amount=c.amount)
       OR (SELECT count(*) FROM reg_crypto_merchant_balance_movements WHERE recorder_id=c.id)<>(CASE WHEN c.net_amount>0 THEN 1 ELSE 0 END)
       OR (c.net_amount>0 AND NOT EXISTS(SELECT 1 FROM reg_crypto_merchant_balance_movements WHERE line_id=c.merchant_line_id
            AND recorder_id=c.id AND recorder_type='CryptoMerchantCredit' AND recorder_version=1 AND record_type='receipt'
            AND merchant_id=c.merchant_id AND token_id=c.token_id AND amount=c.net_amount))
       OR (SELECT count(*) FROM reg_crypto_fee_movements WHERE recorder_id=c.id)<>(CASE WHEN c.processing_fee>0 THEN 1 ELSE 0 END)
       OR (c.processing_fee>0 AND NOT EXISTS(SELECT 1 FROM reg_crypto_fee_movements WHERE line_id=c.fee_line_id
            AND recorder_id=c.id AND recorder_type='CryptoMerchantCredit' AND recorder_version=1 AND record_type='receipt'
            AND merchant_id=c.merchant_id AND token_id=c.token_id AND fee_type='processing' AND amount=c.processing_fee))
       OR EXISTS(SELECT 1 FROM reg_crypto_balance_movements WHERE recorder_id=c.id)
       OR EXISTS(SELECT 1 FROM reg_crypto_platform_cost_movements WHERE recorder_id=c.id)
       OR EXISTS(SELECT 1 FROM reg_crypto_provider_balance_movements WHERE recorder_id=c.id) THEN
        RAISE EXCEPTION 'merchant credit requires its complete attribution set' USING ERRCODE='23514';
    END IF;
END;
$$;

CREATE FUNCTION fn_crypto_merchant_credit_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME='doc_crypto_merchant_credits' THEN
        PERFORM fn_crypto_assert_merchant_credit(NEW.id);
    ELSIF NEW.recorder_type='CryptoMerchantCredit' THEN
        PERFORM fn_crypto_assert_merchant_credit(NEW.recorder_id);
    END IF;
    RETURN NULL;
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

CREATE FUNCTION fn_crypto_lock_receipt_balance(wallet UUID,asset UUID) RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO reg_crypto_balance_balances(wallet_id,token_id,amount) VALUES(wallet,asset,0)
        ON CONFLICT(wallet_id,token_id) DO NOTHING;
    PERFORM wallet_id FROM reg_crypto_balance_balances WHERE wallet_id=wallet AND token_id=asset FOR UPDATE;
END;
$$;

CREATE FUNCTION fn_crypto_assert_custody_receipt(target UUID) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE d doc_crypto_custody_receipts;
BEGIN
    SELECT * INTO STRICT d FROM doc_crypto_custody_receipts WHERE id=target;
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
    IF EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings p
        JOIN reg_crypto_accounting_effect_occurrences o ON o.id=p.occurrence_id
        JOIN reg_crypto_accounting_effects e ON e.id=p.effect_id
        JOIN cat_wallets source ON source.network_id=e.network_id AND source.address_key=o.source_address
        WHERE p.id=d.posting_id) THEN
        RAISE EXCEPTION 'internal transfer requires its settlement owner' USING ERRCODE='23514';
    END IF;
    -- One chain effect has one custody owner; payments only attribute the receipt.
    IF EXISTS(SELECT 1 FROM reg_crypto_wallet_fundings other
          JOIN reg_crypto_custody_effect_postings p ON p.id=d.posting_id
          JOIN reg_crypto_accounting_effects e ON e.id=p.effect_id
          WHERE other.network_id=e.network_id AND regexp_replace(lower(other.tx_hash),'^0x','')=e.tx_hash)
       OR EXISTS(SELECT 1 FROM reg_crypto_aml_operating_receipts other
          JOIN reg_crypto_custody_effect_postings p ON p.id=d.posting_id
          JOIN reg_crypto_accounting_effects e ON e.id=p.effect_id
          WHERE other.network_id=e.network_id AND regexp_replace(lower(other.tx_hash),'^0x','')=e.tx_hash) THEN
        RAISE EXCEPTION 'chain effect already has another accounting owner' USING ERRCODE='23514';
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

CREATE FUNCTION fn_crypto_custody_receipt_no_attribution() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.recorder_type='CryptoCustodyReceipt' OR EXISTS(SELECT 1 FROM doc_crypto_custody_receipts WHERE id=NEW.recorder_id) THEN
        RAISE EXCEPTION 'custody receipt cannot create attributed financial movements' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION fn_crypto_wallet_funding_admission() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    PERFORM fn_crypto_lock_receipt_balance(NEW.wallet_id,NEW.token_id);
    IF NEW.occurrence_id IS NULL OR NEW.posting_id IS NULL OR NEW.verification_id IS NULL THEN
        RAISE EXCEPTION 'wallet funding requires indexed custody recognition' USING ERRCODE='23514';
    END IF;
    IF NOT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings p
        JOIN reg_crypto_accounting_effect_occurrences o ON o.id=p.occurrence_id
        JOIN reg_crypto_accounting_effects e ON e.id=p.effect_id
        JOIN reg_crypto_transfer_verifications v ON v.id=p.verification_id
        JOIN cat_wallets w ON w.id=NEW.wallet_id
        WHERE p.id=NEW.posting_id AND p.occurrence_id=NEW.occurrence_id
          AND p.verification_id=NEW.verification_id AND p.recorder_id=NEW.id
          AND p.recorder_type='CryptoWalletFunding' AND p.recorder_version=1
          AND e.network_id=NEW.network_id AND w.network_id=e.network_id
          AND (e.family<>'evm' OR e.event_kind<>'native_value' OR v.evidence->>'matchingEffects'='1')
          AND w.custody_merchant_id IS NULL AND w.vault_key_id<>''
          AND regexp_replace(lower(NEW.tx_hash),'^0x','')=e.tx_hash
          AND (CASE WHEN e.family='evm' THEN lower(NEW.from_address) ELSE NEW.from_address END)=o.source_address
          AND (CASE WHEN e.family='evm' THEN lower(NEW.to_address) ELSE NEW.to_address END)=o.destination_address
          AND NEW.amount=o.amount AND NEW.block_number=o.block_number
          AND NEW.confirmations=v.confirmations
          AND NEW.block_timestamp=(v.evidence->'event'->>'timestamp')::timestamptz) THEN
        RAISE EXCEPTION 'wallet funding requires exact platform-owned receipt evidence' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

-- Payment aliases with an indexed identity are compared by logical effect.
-- An unindexed legacy payment is conservatively ambiguous within the same
-- transaction/kind/destination/asset and must be investigated before funding.
CREATE FUNCTION fn_crypto_wallet_funding_payment_conflict(
    network UUID, wallet UUID, asset UUID, hash TEXT, kind TEXT, verification UUID
) RETURNS BOOLEAN LANGUAGE sql VOLATILE AS $$
    SELECT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings p
        JOIN reg_crypto_accounting_effects e ON e.id=p.effect_id
        JOIN reg_crypto_accounting_effect_occurrences o ON o.id=p.occurrence_id
        JOIN cat_wallets w ON w.id=wallet
        JOIN cat_tokens t ON t.id=asset
        WHERE p.recorder_type='CryptoWalletFunding' AND e.network_id=network
          AND e.tx_hash=regexp_replace(lower(hash),'^0x','') AND e.event_kind=kind
          AND o.destination_address=w.address_key
          AND o.contract_address=CASE WHEN e.family='evm' THEN lower(COALESCE(t.contract_address,'')) ELSE COALESCE(t.contract_address,'') END
          AND (NOT EXISTS(SELECT 1 FROM reg_crypto_accounting_effect_evidence WHERE verification_id=verification)
            OR EXISTS(SELECT 1 FROM reg_crypto_accounting_effect_evidence v
                JOIN reg_crypto_accounting_effect_occurrences x ON x.id=v.occurrence_id
                WHERE v.verification_id=verification AND x.effect_id=e.id)))
$$;

CREATE FUNCTION fn_crypto_wallet_funding_payment_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    PERFORM fn_crypto_lock_receipt_balance(NEW.wallet_id,NEW.token_id);
    IF fn_crypto_wallet_funding_payment_conflict(NEW.network_id,NEW.wallet_id,NEW.token_id,NEW.tx_hash,NEW.event_kind,NEW.verification_id) THEN
        RAISE EXCEPTION 'payment principal already belongs to wallet funding' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION fn_crypto_assert_wallet_funding(target UUID) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE f reg_crypto_wallet_fundings;
BEGIN
    SELECT * INTO STRICT f FROM reg_crypto_wallet_fundings WHERE id=target;
    IF f.posting_id IS NULL THEN RETURN; END IF;
    PERFORM fn_crypto_assert_custody_posting(f.posting_id);
    IF (SELECT count(*) FROM reg_crypto_custody_effect_recognitions WHERE posting_id=f.posting_id)<>1
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_recognitions r
            WHERE r.posting_id=f.posting_id AND r.wallet_id=f.wallet_id AND r.token_id=f.token_id
              AND r.record_type='receipt' AND r.movement_line_id=f.id)
       OR (SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=f.id)<>1
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_balance_movements m WHERE m.line_id=f.id AND m.period=f.block_timestamp)
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_funding_audit a WHERE a.funding_id=f.id
            AND a.verification_id=f.verification_id AND a.posting_id=f.posting_id)
       OR (SELECT count(*) FROM sys_outbox WHERE aggregate_id=f.id AND aggregate_type='CryptoWalletFunding' AND event_type='recorded')<>1
       OR NOT EXISTS(SELECT 1 FROM sys_outbox WHERE aggregate_id=f.id AND aggregate_type='CryptoWalletFunding'
            AND event_type='recorded' AND payload->>'entityId'=f.id::text
            AND payload->>'entityType'='CryptoWalletFunding' AND payload->>'action'='recorded')
       OR EXISTS(SELECT 1 FROM reg_crypto_merchant_balance_movements WHERE recorder_id=f.id)
       OR EXISTS(SELECT 1 FROM reg_crypto_fee_movements WHERE recorder_id=f.id)
       OR EXISTS(SELECT 1 FROM reg_crypto_platform_cost_movements WHERE recorder_id=f.id)
       OR EXISTS(SELECT 1 FROM reg_crypto_provider_balance_movements WHERE recorder_id=f.id) THEN
        RAISE EXCEPTION 'wallet funding requires complete atomic accounting' USING ERRCODE='23514';
    END IF;
    IF EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings p
        JOIN reg_crypto_accounting_effects e ON e.id=p.effect_id
        JOIN reg_crypto_accounting_effect_occurrences o ON o.id=p.occurrence_id
        JOIN cat_wallets source ON source.network_id=e.network_id AND source.address_key=o.source_address
        WHERE p.id=f.posting_id)
       OR EXISTS(SELECT 1 FROM reg_crypto_expected_chain_effects x
        WHERE x.network_id=f.network_id AND regexp_replace(lower(x.tx_hash),'^0x','')=regexp_replace(lower(f.tx_hash),'^0x','')) THEN
        RAISE EXCEPTION 'internal funding requires its settlement owner' USING ERRCODE='23514';
    END IF;
    IF EXISTS(SELECT 1 FROM doc_crypto_payments d
        WHERE fn_crypto_wallet_funding_payment_conflict(d.network_id,d.wallet_id,d.token_id,d.tx_hash,d.event_kind,d.verification_id)
          AND d.network_id=f.network_id AND d.wallet_id=f.wallet_id
          AND regexp_replace(lower(d.tx_hash),'^0x','')=regexp_replace(lower(f.tx_hash),'^0x',''))
       OR EXISTS(SELECT 1 FROM reg_crypto_wallet_fundings old WHERE old.id<>f.id AND old.posting_id IS NULL
          AND old.network_id=f.network_id AND regexp_replace(lower(old.tx_hash),'^0x','')=regexp_replace(lower(f.tx_hash),'^0x',''))
       OR EXISTS(SELECT 1 FROM reg_crypto_aml_operating_receipts old WHERE old.network_id=f.network_id
          AND regexp_replace(lower(old.tx_hash),'^0x','')=regexp_replace(lower(f.tx_hash),'^0x','')) THEN
        RAISE EXCEPTION 'wallet funding overlaps another accounting owner of the transaction' USING ERRCODE='23514';
    END IF;
    IF EXISTS(SELECT 1 FROM reg_crypto_balance_movements m
        WHERE m.wallet_id=f.wallet_id AND m.token_id=f.token_id AND m.record_type='receipt'
          AND m.recorder_type IN ('CryptoAdjustment','CryptoCustodyAdjustment') AND m.reversal_of_line_id IS NULL
          AND NOT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_recognitions r WHERE r.movement_line_id=m.line_id)
          AND m.amount>COALESCE((SELECT SUM(x.amount) FROM reg_crypto_balance_movements x WHERE x.reversal_of_line_id=m.line_id),0)) THEN
        RAISE EXCEPTION 'wallet funding conflicts with an unreconciled manual custody correction' USING ERRCODE='23514';
    END IF;
    IF (SELECT amount FROM reg_crypto_balance_balances WHERE wallet_id=f.wallet_id AND token_id=f.token_id)
       IS DISTINCT FROM (SELECT COALESCE(SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END),0)
            FROM reg_crypto_balance_movements WHERE wallet_id=f.wallet_id AND token_id=f.token_id) THEN
        RAISE EXCEPTION 'custody projection requires reconciliation before funding' USING ERRCODE='23514';
    END IF;
END;
$$;

CREATE FUNCTION fn_crypto_wallet_funding_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME='reg_crypto_wallet_fundings' THEN
        PERFORM fn_crypto_assert_wallet_funding(NEW.id);
    ELSIF NEW.recorder_type='CryptoWalletFunding' THEN
        IF NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_fundings WHERE id=NEW.recorder_id AND posting_id IS NOT NULL) THEN
            RAISE EXCEPTION 'funding principal requires its recognized receipt' USING ERRCODE='23514';
        END IF;
        PERFORM fn_crypto_assert_wallet_funding(NEW.recorder_id);
    END IF;
    RETURN NULL;
END;
$$;

CREATE FUNCTION fn_crypto_wallet_funding_movement_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE operating reg_crypto_aml_operating_receipts;
BEGIN
    PERFORM fn_crypto_lock_receipt_balance(NEW.wallet_id,NEW.token_id);
    IF NEW.recorder_type='AMLOperatingReceipt' THEN
        SELECT * INTO operating FROM reg_crypto_aml_operating_receipts WHERE id=NEW.recorder_id;
        IF FOUND AND fn_crypto_wallet_funding_payment_conflict(operating.network_id,NEW.wallet_id,NEW.token_id,
            operating.tx_hash,operating.event_kind,NULL) THEN
            RAISE EXCEPTION 'operating principal already belongs to wallet funding' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;


CREATE FUNCTION fn_crypto_wallet_funding_no_attribution() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.recorder_type='CryptoWalletFunding' OR EXISTS(SELECT 1 FROM reg_crypto_wallet_fundings WHERE id=NEW.recorder_id AND posting_id IS NOT NULL) THEN
        RAISE EXCEPTION 'wallet funding cannot create attributed financial movements' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

-- Once a payment attributes a receipt, its chain identity, receipt and tariff
-- snapshot are history; only lifecycle and confirmation metadata may change.
CREATE FUNCTION fn_crypto_payment_frozen_basis_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.receipt_id IS NULL THEN
        IF TG_OP='DELETE' THEN RETURN OLD; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP='DELETE' OR NEW.deletion_mark OR NEW._deleted_at IS NOT NULL
       OR (NEW.receipt_id,NEW.invoice_id,NEW.merchant_id,NEW.network_id,NEW.wallet_id,NEW.token_id,NEW.amount,
           NEW.tx_hash,NEW.event_kind,NEW.fee_fixed,NEW.fee_percent_bp,NEW.fee_min,NEW.fee_max)
          IS DISTINCT FROM (OLD.receipt_id,OLD.invoice_id,OLD.merchant_id,OLD.network_id,OLD.wallet_id,OLD.token_id,OLD.amount,
           OLD.tx_hash,OLD.event_kind,OLD.fee_fixed,OLD.fee_percent_bp,OLD.fee_min,OLD.fee_max) THEN
        RAISE EXCEPTION 'attributed payment basis and fee snapshot are frozen' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

-- A payment is an attribution of one custody receipt: it consumes the receipt
-- suspense and credits the merchant net of the snapshotted processing fee.
-- Superseded generations remain only as fully compensated history.
CREATE FUNCTION fn_crypto_assert_payment_attribution(target UUID) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE d doc_crypto_payments; fee NUMERIC;
BEGIN
    SELECT * INTO d FROM doc_crypto_payments WHERE id=target;
    IF NOT FOUND THEN RAISE EXCEPTION 'payment attribution requires its document' USING ERRCODE='23514'; END IF;
    IF EXISTS(SELECT 1 FROM reg_crypto_balance_movements WHERE recorder_id=d.id)
       OR EXISTS(SELECT 1 FROM reg_crypto_platform_cost_movements WHERE recorder_id=d.id)
       OR EXISTS(SELECT 1 FROM reg_crypto_provider_balance_movements WHERE recorder_id=d.id) THEN
        RAISE EXCEPTION 'payment attributes a receipt and never moves custody' USING ERRCODE='23514';
    END IF;
    IF EXISTS(SELECT 1 FROM reg_crypto_receipt_suspense_movements m WHERE m.recorder_id=d.id AND m.recorder_type='CryptoPayment'
            AND (NOT d.posted OR m.recorder_version<>d.posted_version)
            AND m.amount<>(SELECT COALESCE(SUM(x.amount),0) FROM reg_crypto_receipt_suspense_movements x WHERE x.reversal_of_line_id=m.line_id))
       OR EXISTS(SELECT 1 FROM reg_crypto_merchant_balance_movements m WHERE m.recorder_id=d.id AND m.recorder_type='CryptoPayment'
            AND (NOT d.posted OR m.recorder_version<>d.posted_version)
            AND m.amount<>(SELECT COALESCE(SUM(x.amount),0) FROM reg_crypto_merchant_balance_movements x WHERE x.reversal_of_line_id=m.line_id))
       OR EXISTS(SELECT 1 FROM reg_crypto_fee_movements m WHERE m.recorder_id=d.id AND m.recorder_type='CryptoPayment'
            AND (NOT d.posted OR m.recorder_version<>d.posted_version)
            AND m.amount<>(SELECT COALESCE(SUM(x.amount),0) FROM reg_crypto_fee_movements x WHERE x.reversal_of_line_id=m.line_id)) THEN
        RAISE EXCEPTION 'superseded payment attribution is not fully compensated' USING ERRCODE='23514';
    END IF;
    IF NOT d.posted THEN RETURN; END IF;
    fee:=d.fee_fixed+trunc(d.amount*d.fee_percent_bp/10000);
    IF d.fee_min>0 THEN fee:=greatest(fee,d.fee_min); END IF;
    IF d.fee_max>0 THEN fee:=least(fee,d.fee_max); END IF;
    IF fee>d.amount OR d.status NOT IN ('confirmed','settled')
       OR NOT EXISTS(SELECT 1 FROM doc_crypto_custody_receipts r
            JOIN reg_crypto_custody_effect_postings p ON p.id=r.posting_id
            JOIN reg_crypto_accounting_effects e ON e.id=p.effect_id
            WHERE r.id=d.receipt_id AND r.posted AND r.network_id=d.network_id AND r.wallet_id=d.wallet_id
              AND r.token_id=d.token_id AND r.amount=d.amount
              AND e.tx_hash=regexp_replace(lower(d.tx_hash),'^0x','') AND e.event_kind=d.event_kind)
       OR (SELECT count(*) FROM reg_crypto_receipt_suspense_movements WHERE recorder_id=d.id AND recorder_type='CryptoPayment' AND recorder_version=d.posted_version)<>1
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_receipt_suspense_movements WHERE recorder_id=d.id AND recorder_type='CryptoPayment'
            AND recorder_version=d.posted_version AND record_type='expense' AND receipt_id=d.receipt_id AND token_id=d.token_id AND amount=d.amount)
       OR (SELECT count(*) FROM reg_crypto_merchant_balance_movements WHERE recorder_id=d.id AND recorder_type='CryptoPayment' AND recorder_version=d.posted_version)
            <>(CASE WHEN d.amount>fee THEN 1 ELSE 0 END)
       OR EXISTS(SELECT 1 FROM reg_crypto_merchant_balance_movements WHERE recorder_id=d.id AND recorder_type='CryptoPayment' AND recorder_version=d.posted_version
            AND (record_type<>'receipt' OR merchant_id<>d.merchant_id OR token_id<>d.token_id OR amount<>d.amount-fee OR reversal_of_line_id IS NOT NULL))
       OR (SELECT count(*) FROM reg_crypto_fee_movements WHERE recorder_id=d.id AND recorder_type='CryptoPayment' AND recorder_version=d.posted_version)
            <>(CASE WHEN fee>0 THEN 1 ELSE 0 END)
       OR EXISTS(SELECT 1 FROM reg_crypto_fee_movements WHERE recorder_id=d.id AND recorder_type='CryptoPayment' AND recorder_version=d.posted_version
            AND (record_type<>'receipt' OR merchant_id<>d.merchant_id OR token_id<>d.token_id OR amount<>fee OR fee_type<>'processing' OR reversal_of_line_id IS NOT NULL)) THEN
        RAISE EXCEPTION 'payment requires its exact receipt attribution set' USING ERRCODE='23514';
    END IF;
END;
$$;

CREATE FUNCTION fn_crypto_payment_attribution_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME='doc_crypto_payments' THEN
        PERFORM fn_crypto_assert_payment_attribution(NEW.id);
    ELSIF NEW.recorder_type='CryptoPayment' THEN
        PERFORM fn_crypto_assert_payment_attribution(NEW.recorder_id);
    END IF;
    RETURN NULL;
END;
$$;


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

CREATE FUNCTION fn_crypto_assert_sweep_line(target UUID) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE l doc_crypto_sweep_lines; s doc_crypto_sweeps; b reg_crypto_sweep_custody_bindings; p reg_crypto_custody_effect_postings;
DECLARE src cat_wallets; dst cat_wallets; f reg_crypto_fee_reservations; c reg_crypto_chain_settlements;
BEGIN
    SELECT * INTO STRICT l FROM doc_crypto_sweep_lines WHERE line_id=target;
    SELECT * INTO STRICT s FROM doc_crypto_sweeps WHERE id=l.document_id;
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

CREATE FUNCTION fn_crypto_sweep_financial_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target UUID;
BEGIN
    IF TG_TABLE_NAME IN ('reg_crypto_merchant_balance_movements','reg_crypto_fee_movements','reg_crypto_provider_balance_movements')
       AND EXISTS(SELECT 1 FROM doc_crypto_sweeps WHERE id=NEW.recorder_id) THEN
        RAISE EXCEPTION 'sweep cannot create merchant credit, revenue or provider principal' USING ERRCODE='23514';
    END IF;
    IF TG_TABLE_NAME='reg_crypto_balance_movements'
       AND EXISTS(SELECT 1 FROM doc_crypto_sweeps WHERE id=NEW.recorder_id) THEN
        IF NEW.recorder_type<>'CryptoSweep' THEN RAISE EXCEPTION 'sweep principal cannot relabel its recorder' USING ERRCODE='23514'; END IF;
        SELECT b.line_id INTO target FROM reg_crypto_custody_effect_recognitions r
            JOIN reg_crypto_sweep_custody_bindings b ON b.posting_id=r.posting_id WHERE r.movement_line_id=NEW.line_id AND b.sweep_id=NEW.recorder_id;
        IF NOT FOUND THEN RAISE EXCEPTION 'sweep principal cannot be posted at broadcast' USING ERRCODE='23514'; END IF;
        PERFORM fn_crypto_assert_sweep_line(target);
    END IF;
    SELECT l.line_id INTO target FROM doc_crypto_sweep_lines l JOIN doc_crypto_sweeps s ON s.id=l.document_id
        WHERE l.line_id=NEW.recorder_id;
    IF FOUND THEN PERFORM fn_crypto_assert_sweep_line(target); END IF;
    IF NEW.reversal_of_line_id IS NOT NULL AND EXISTS(SELECT 1 FROM reg_crypto_custody_effect_recognitions r
        JOIN reg_crypto_sweep_custody_bindings b ON b.posting_id=r.posting_id WHERE r.movement_line_id=NEW.reversal_of_line_id) THEN
        RAISE EXCEPTION 'recognized sweep requires explicit compensation workflow' USING ERRCODE='23514';
    END IF;
    RETURN NULL;
END;
$$;

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
    IF EXISTS(SELECT 1 FROM doc_crypto_sweep_lines l JOIN doc_crypto_sweeps s ON s.id=l.document_id WHERE l.line_id=target) THEN
        PERFORM fn_crypto_assert_sweep_line(target);
    END IF;
    RETURN NULL;
END;
$$;

CREATE FUNCTION fn_crypto_sweep_basis_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE model INT;
BEGIN
    IF TG_TABLE_NAME='doc_crypto_sweeps' THEN
        IF TG_OP='DELETE' THEN
            IF EXISTS(SELECT 1 FROM doc_crypto_sweep_lines l WHERE l.document_id=OLD.id AND
                (l.tx_hash<>'' OR EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations a WHERE a.basis_type='CryptoSweepAttempt' AND a.basis_id=l.line_id))) THEN
                RAISE EXCEPTION 'broadcast sweep history is immutable' USING ERRCODE='23514'; END IF;
            RETURN OLD;
        END IF;
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

CREATE FUNCTION fn_crypto_assert_treasury(target UUID) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE d doc_treasury_withdrawals; w cat_wallets; f reg_crypto_fee_reservations; c reg_crypto_chain_settlements;
DECLARE b reg_crypto_treasury_custody_bindings; p reg_crypto_custody_effect_postings; success BOOLEAN;
BEGIN
    SELECT * INTO STRICT d FROM doc_treasury_withdrawals WHERE id=target;
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

CREATE FUNCTION fn_crypto_treasury_financial_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS(SELECT 1 FROM doc_treasury_withdrawals WHERE id=NEW.recorder_id) THEN
        PERFORM fn_crypto_assert_treasury(NEW.recorder_id);
    END IF;
    IF NEW.reversal_of_line_id IS NOT NULL AND EXISTS(SELECT 1 FROM reg_crypto_custody_effect_recognitions r
        JOIN reg_crypto_treasury_custody_bindings b ON b.posting_id=r.posting_id WHERE r.movement_line_id=NEW.reversal_of_line_id) THEN
        RAISE EXCEPTION 'recognized treasury requires explicit compensation workflow' USING ERRCODE='23514';
    END IF;
    RETURN NULL;
END;
$$;

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
    IF EXISTS(SELECT 1 FROM doc_treasury_withdrawals WHERE id=target) THEN PERFORM fn_crypto_assert_treasury(target); END IF;
    RETURN NULL;
END;
$$;

CREATE FUNCTION fn_crypto_treasury_basis_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        IF (OLD.tx_hash<>'' OR EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations WHERE basis_type='TreasuryWithdrawal' AND basis_id=OLD.id)) THEN
            RAISE EXCEPTION 'signed treasury history is immutable' USING ERRCODE='23514'; END IF;
        RETURN OLD;
    END IF;
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

CREATE FUNCTION fn_crypto_treasury_reservation_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target UUID; d doc_treasury_withdrawals; balance NUMERIC; reserved NUMERIC; fees NUMERIC;
BEGIN
    IF TG_OP IN ('UPDATE','DELETE') THEN target:=OLD.document_id; ELSE target:=NEW.document_id; END IF;
    SELECT * INTO d FROM doc_treasury_withdrawals WHERE id=target;
    IF NOT FOUND AND TG_OP='UPDATE' THEN
        SELECT * INTO d FROM doc_treasury_withdrawals WHERE id=NEW.document_id;
    END IF;
    IF NOT FOUND THEN
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
    IF EXISTS(SELECT 1 FROM doc_treasury_withdrawals WHERE id=NEW.document_id) AND
       (EXISTS(SELECT 1 FROM doc_treasury_withdrawals WHERE id=NEW.document_id AND (tx_hash<>'' OR settlement_status='recorded'))
        OR (NEW.status<>'active' AND EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations WHERE basis_type='TreasuryWithdrawal' AND basis_id=NEW.document_id AND signed_envelope IS NOT NULL AND status<>'failed'))) THEN
        IF NEW.status<>'active' OR EXISTS(SELECT 1 FROM doc_treasury_withdrawals WHERE id=NEW.document_id AND settlement_status='recorded') THEN
            PERFORM fn_crypto_assert_treasury(NEW.document_id);
        END IF;
    END IF;
    RETURN NULL;
END;
$$;

CREATE FUNCTION fn_crypto_treasury_fee_release_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.basis_type='TreasuryWithdrawal' AND NEW.status='released' AND EXISTS(SELECT 1 FROM doc_treasury_withdrawals WHERE id=NEW.basis_id)
       AND EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations WHERE basis_type='TreasuryWithdrawal' AND basis_id=NEW.basis_id AND signed_envelope IS NOT NULL AND status<>'failed') THEN
        RAISE EXCEPTION 'signed treasury fee reserve requires a proven final outcome' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

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
        JOIN doc_treasury_withdrawals d ON d.id=a.basis_id
        WHERE NEW.recorder_type<>'TreasuryWithdrawal' OR NEW.recorder_id<>d.id) THEN
        RAISE EXCEPTION 'expected treasury principal requires its settlement owner' USING ERRCODE='23514';
    END IF;
    IF NEW.recorder_type='CryptoCustodyAdjustment' AND EXISTS(
        SELECT 1 FROM doc_crypto_custody_adjustments adjustment JOIN reg_crypto_external_outflows o ON o.id=adjustment.external_outflow_id
        JOIN reg_crypto_expected_chain_effects x ON x.network_id=o.network_id AND regexp_replace(lower(x.tx_hash),'^0x','')=regexp_replace(lower(o.tx_hash),'^0x','')
            AND x.event_kind=o.event_kind AND x.source_address_key=o.source_address_key AND x.destination_address_key=o.destination_address_key AND x.contract_address_key=o.contract_address_key AND x.amount=o.amount
        JOIN reg_crypto_signing_authorizations a ON a.id=x.signing_authorization_id AND a.basis_type='TreasuryWithdrawal' AND a.signed_envelope IS NOT NULL
        JOIN doc_treasury_withdrawals d ON d.id=a.basis_id WHERE adjustment.id=NEW.recorder_id) THEN
        RAISE EXCEPTION 'expected treasury principal requires its settlement owner' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION fn_crypto_assert_withdrawal_parent(target UUID) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE r doc_withdrawal_requests; d doc_crypto_withdrawals;
BEGIN
    SELECT * INTO STRICT r FROM doc_withdrawal_requests WHERE id=target;
    IF NOT EXISTS(SELECT 1 FROM doc_crypto_withdrawals c WHERE ((c.basis_type='WithdrawalRequest' AND c.basis_id=r.id)
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

CREATE FUNCTION fn_crypto_withdrawal_binding_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings WHERE id=NEW.posting_id AND recorder_type='CryptoWithdrawal' AND recorder_id=NEW.withdrawal_id)
       OR (SELECT count(*) FROM reg_crypto_custody_effect_recognitions rc JOIN reg_crypto_balance_movements m ON m.line_id=rc.movement_line_id WHERE rc.posting_id=NEW.posting_id AND m.posting_txid=txid_current())<>1 THEN
        RAISE EXCEPTION 'withdrawal requires a new principal line in this transaction' USING ERRCODE='23514'; END IF;
    PERFORM fn_crypto_assert_withdrawal(NEW.withdrawal_id); RETURN NULL;
END;
$$;

CREATE FUNCTION fn_crypto_withdrawal_posting_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target UUID;
BEGIN
    IF NEW.recorder_type<>'CryptoWithdrawal' THEN RETURN NULL; END IF;
    SELECT withdrawal_id INTO target FROM reg_crypto_withdrawal_custody_bindings WHERE posting_id=NEW.id;
    IF NOT FOUND THEN RAISE EXCEPTION 'withdrawal principal requires its binding' USING ERRCODE='23514'; END IF;
    PERFORM fn_crypto_assert_withdrawal(target); RETURN NULL;
END;
$$;

CREATE FUNCTION fn_crypto_withdrawal_financial_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target UUID;
BEGIN
    FOR target IN SELECT id FROM doc_crypto_withdrawals WHERE (id=NEW.recorder_id OR basis_id=NEW.recorder_id) AND (TG_TABLE_NAME<>'reg_crypto_merchant_balance_movements' OR id=NEW.recorder_id OR settlement_status='recorded') LOOP
        PERFORM fn_crypto_assert_withdrawal(target);
    END LOOP;
    IF TG_TABLE_NAME='reg_crypto_merchant_balance_movements' AND EXISTS(SELECT 1 FROM doc_withdrawal_requests WHERE id=NEW.recorder_id) THEN PERFORM fn_crypto_assert_withdrawal_parent(NEW.recorder_id); END IF;
    IF NEW.reversal_of_line_id IS NOT NULL AND EXISTS(SELECT 1 FROM reg_crypto_custody_effect_recognitions rc JOIN reg_crypto_withdrawal_custody_bindings b ON b.posting_id=rc.posting_id WHERE rc.movement_line_id=NEW.reversal_of_line_id) THEN
        RAISE EXCEPTION 'recognized withdrawal requires explicit compensation workflow' USING ERRCODE='23514'; END IF;
    RETURN NULL;
END;
$$;

CREATE FUNCTION fn_crypto_withdrawal_settlement_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target UUID;
BEGIN
    IF TG_TABLE_NAME='doc_crypto_withdrawals' THEN
        IF NEW.settlement_status<>'recorded' THEN RETURN NULL; END IF; target:=NEW.id;
    ELSE
        IF NEW.basis_type NOT IN ('WithdrawalRequest','CryptoAMLWithdrawalLine') THEN RETURN NULL; END IF;
        IF TG_TABLE_NAME='reg_crypto_fee_reservations' AND NEW.status<>'settled' THEN RETURN NULL; END IF;
        SELECT id INTO target FROM doc_crypto_withdrawals WHERE basis_type=NEW.basis_type AND basis_id=NEW.basis_id;
    END IF;
    IF EXISTS(SELECT 1 FROM doc_crypto_withdrawals WHERE id=target) THEN PERFORM fn_crypto_assert_withdrawal(target); END IF;
    RETURN NULL;
END;
$$;

CREATE FUNCTION fn_crypto_withdrawal_basis_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME='doc_withdrawal_requests' THEN
        IF EXISTS(SELECT 1 FROM doc_crypto_withdrawals c WHERE ((c.basis_type='WithdrawalRequest' AND c.basis_id=OLD.id)
            OR EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_lines WHERE withdrawal_id=c.id AND request_id=OLD.id))) THEN
            IF TG_OP='DELETE' THEN RAISE EXCEPTION 'withdrawal request history is immutable' USING ERRCODE='23514'; END IF;
            IF (NEW.merchant_id,NEW.token_id,NEW.amount,NEW.dest_address,NEW.address_id,NEW.date,NEW.posted,NEW.posted_version) IS DISTINCT FROM
               (OLD.merchant_id,OLD.token_id,OLD.amount,OLD.dest_address,OLD.address_id,OLD.date,OLD.posted,OLD.posted_version) OR (NEW.withdrawal_id IS DISTINCT FROM OLD.withdrawal_id AND NOT (OLD.withdrawal_id IS NULL AND EXISTS(SELECT 1 FROM doc_crypto_withdrawals WHERE id=NEW.withdrawal_id AND basis_type='WithdrawalRequest' AND basis_id=OLD.id))) OR NEW.deletion_mark OR NEW._deleted_at IS NOT NULL THEN
                RAISE EXCEPTION 'withdrawal request financial basis is immutable' USING ERRCODE='23514'; END IF;
        END IF;
        IF TG_OP='DELETE' THEN RETURN OLD; END IF; RETURN NEW;
    END IF;
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'withdrawal execution history is immutable' USING ERRCODE='23514';
    END IF;
    IF (NEW.merchant_id,NEW.token_id,NEW.source_wallet_id,NEW.dest_address,NEW.amount,NEW.date,NEW.basis_type,NEW.basis_id) IS DISTINCT FROM
       (OLD.merchant_id,OLD.token_id,OLD.source_wallet_id,OLD.dest_address,OLD.amount,OLD.date,OLD.basis_type,OLD.basis_id) OR NEW.deletion_mark OR NEW._deleted_at IS NOT NULL OR NEW.posted_version>1 OR (OLD.posted AND NOT NEW.posted) THEN
        RAISE EXCEPTION 'withdrawal execution basis is immutable' USING ERRCODE='23514'; END IF;
    IF OLD.settlement_status='recorded' AND (NEW.tx_hash,NEW.status,NEW.settlement_status,NEW.chain_result,NEW.network_fee,NEW.network_fee_token_id) IS DISTINCT FROM
        (OLD.tx_hash,OLD.status,OLD.settlement_status,OLD.chain_result,OLD.network_fee,OLD.network_fee_token_id) THEN RAISE EXCEPTION 'withdrawal final outcome is immutable' USING ERRCODE='23514'; END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION fn_crypto_withdrawal_reservation_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE d doc_crypto_withdrawals; balance NUMERIC; reserved NUMERIC; fees NUMERIC;
BEGIN
    IF TG_OP='INSERT' THEN
        IF NEW.document_type NOT IN ('WithdrawalRequest','CryptoAMLWithdrawalLine') THEN RETURN NEW; END IF;
        PERFORM fn_crypto_lock_receipt_balance(NEW.wallet_id,NEW.token_id);
        SELECT amount INTO STRICT balance FROM reg_crypto_balance_balances WHERE wallet_id=NEW.wallet_id AND token_id=NEW.token_id;
        SELECT COALESCE(sum(amount),0) INTO reserved FROM reg_crypto_balance_reservations WHERE wallet_id=NEW.wallet_id AND token_id=NEW.token_id AND status='active';
        SELECT COALESCE(sum(reserved_principal_minor+CASE WHEN strategy='resource_rental' THEN 0 ELSE max_fee_minor END),0) INTO fees FROM reg_crypto_fee_reservations
            WHERE payer_wallet_id=NEW.wallet_id AND native_token_id=NEW.token_id AND status IN ('reserved','broadcast','held');
        IF NEW.status<>'active' OR NEW.amount>balance-reserved-fees THEN RAISE EXCEPTION 'withdrawal reservation exceeds available principal' USING ERRCODE='23514'; END IF;
        RETURN NEW;
    END IF;
    SELECT * INTO d FROM doc_crypto_withdrawals WHERE basis_type=OLD.document_type AND basis_id=OLD.document_id;
    IF NOT FOUND AND TG_OP='UPDATE' THEN
        SELECT * INTO d FROM doc_crypto_withdrawals WHERE basis_type=NEW.document_type AND basis_id=NEW.document_id;
    END IF;
    IF NOT FOUND THEN IF TG_OP='DELETE' THEN RETURN OLD; END IF; RETURN NEW; END IF;
    IF TG_OP='DELETE' THEN RAISE EXCEPTION 'withdrawal reservation history is immutable' USING ERRCODE='23514'; END IF;
    IF (to_jsonb(NEW)-ARRAY['status','updated_at']) IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['status','updated_at']) OR (OLD.status<>'active' AND NEW.status<>OLD.status) THEN
        RAISE EXCEPTION 'withdrawal reservation basis and terminal status are immutable' USING ERRCODE='23514'; END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION fn_crypto_withdrawal_reservation_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE d doc_crypto_withdrawals;
BEGIN
    SELECT * INTO d FROM doc_crypto_withdrawals WHERE basis_type=NEW.document_type AND basis_id=NEW.document_id;
    IF FOUND AND NEW.status<>'active' AND (d.tx_hash<>'' OR EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations WHERE basis_type=d.basis_type AND basis_id=d.basis_id AND (signed_envelope IS NOT NULL OR status<>'failed'))) THEN PERFORM fn_crypto_assert_withdrawal(d.id); END IF;
    RETURN NULL;
END;
$$;

CREATE FUNCTION fn_crypto_withdrawal_fee_release_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status='released' AND EXISTS(SELECT 1 FROM doc_crypto_withdrawals d WHERE d.basis_type=NEW.basis_type AND d.basis_id=NEW.basis_id)
       AND EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations WHERE basis_type=NEW.basis_type AND basis_id=NEW.basis_id AND signed_envelope IS NOT NULL) THEN
        RAISE EXCEPTION 'signed withdrawal fee requires a final outcome' USING ERRCODE='23514'; END IF;
    RETURN NEW;
END;
$$;

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
        JOIN doc_crypto_withdrawals d ON d.basis_type=a.basis_type AND d.basis_id=a.basis_id
        WHERE NEW.recorder_type<>'CryptoWithdrawal' OR NEW.recorder_id<>d.id) THEN
        RAISE EXCEPTION 'expected withdrawal principal requires its settlement owner' USING ERRCODE='23514';
    END IF;
    IF NEW.recorder_type='CryptoCustodyAdjustment' AND EXISTS(
        SELECT 1 FROM doc_crypto_custody_adjustments adjustment JOIN reg_crypto_external_outflows o ON o.id=adjustment.external_outflow_id
        JOIN reg_crypto_expected_chain_effects x ON x.network_id=o.network_id AND regexp_replace(lower(x.tx_hash),'^0x','')=regexp_replace(lower(o.tx_hash),'^0x','')
            AND x.event_kind=o.event_kind AND x.source_address_key=o.source_address_key AND x.destination_address_key=o.destination_address_key AND x.contract_address_key=o.contract_address_key AND x.amount=o.amount
        JOIN reg_crypto_signing_authorizations a ON a.id=x.signing_authorization_id AND a.basis_type IN ('WithdrawalRequest','CryptoAMLWithdrawalLine') AND a.signed_envelope IS NOT NULL
        JOIN doc_crypto_withdrawals d ON d.basis_type=a.basis_type AND d.basis_id=a.basis_id WHERE adjustment.id=NEW.recorder_id) THEN
        RAISE EXCEPTION 'expected withdrawal principal requires its settlement owner' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION fn_crypto_withdrawal_request_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    PERFORM fn_crypto_assert_withdrawal_parent(NEW.id);
    RETURN NULL;
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

CREATE FUNCTION fn_crypto_custody_loss_posting_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target UUID;
BEGIN
    IF NEW.recorder_type<>'CryptoCustodyAdjustment' THEN RETURN NULL; END IF;
    SELECT adjustment_id INTO target FROM reg_crypto_custody_loss_bindings WHERE posting_id=NEW.id;
    IF NOT FOUND THEN RAISE EXCEPTION 'custody loss principal requires its recognition binding' USING ERRCODE='23514'; END IF;
    PERFORM fn_crypto_assert_custody_loss(target); RETURN NULL;
END;
$$;

CREATE FUNCTION fn_crypto_custody_loss_financial_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS(SELECT 1 FROM doc_crypto_custody_adjustments WHERE id=NEW.recorder_id) THEN PERFORM fn_crypto_assert_custody_loss(NEW.recorder_id); END IF;
    IF NEW.reversal_of_line_id IS NOT NULL AND (EXISTS(SELECT 1 FROM reg_crypto_custody_effect_recognitions r JOIN reg_crypto_custody_loss_bindings b ON b.posting_id=r.posting_id WHERE r.movement_line_id=NEW.reversal_of_line_id)
        OR (TG_TABLE_NAME='reg_crypto_platform_cost_movements' AND EXISTS(SELECT 1 FROM reg_crypto_platform_cost_movements m JOIN reg_crypto_custody_loss_bindings b ON b.adjustment_id=m.recorder_id WHERE m.line_id=NEW.reversal_of_line_id))) THEN
        RAISE EXCEPTION 'custody loss requires explicit compensation workflow' USING ERRCODE='23514'; END IF;
    RETURN NULL;
END;
$$;

CREATE FUNCTION fn_crypto_custody_loss_document_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status='posted' THEN PERFORM fn_crypto_assert_custody_loss(NEW.id); END IF; RETURN NULL;
END;
$$;

CREATE FUNCTION fn_crypto_custody_loss_basis_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.created_at<>OLD.created_at THEN
        RAISE EXCEPTION 'custody loss accounting model and basis are immutable' USING ERRCODE='23514'; END IF;
    RETURN NEW;
END;
$$;

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

CREATE FUNCTION fn_crypto_fee_transition_transaction() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    NEW.transition_txid:=CASE WHEN EXISTS(SELECT 1 FROM reg_crypto_fee_reservations WHERE id=NEW.reservation_id) THEN txid_current() ELSE NULL END;
    RETURN NEW;
END;
$$;

CREATE FUNCTION fn_crypto_network_fee_key(family TEXT, network UUID, hash TEXT) RETURNS TEXT LANGUAGE sql IMMUTABLE AS $$
    SELECT encode(digest(concat_ws(E'\n','network-fee.v1',family,network::text,regexp_replace(lower(hash),'^0x','')),'sha256'),'hex');
$$;

CREATE FUNCTION fn_crypto_network_fee_proof_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE body JSONB; components JSONB; sources JSONB; component_count INT; total NUMERIC; canonical TEXT;
BEGIN
    body:=NEW.evidence; components:=body->'components'; sources:=body->'sources';
    IF jsonb_typeof(components) IS DISTINCT FROM 'array' OR jsonb_typeof(sources) IS DISTINCT FROM 'array' THEN RAISE EXCEPTION 'network fee requires structured independent evidence' USING ERRCODE='23514'; END IF;
    component_count:=jsonb_array_length(components);
    IF component_count<1 OR component_count>8 OR jsonb_array_length(sources)<>2
       OR body->>'schemaVersion' IS DISTINCT FROM '1' OR body->>'family' IS DISTINCT FROM NEW.family
       OR body->>'networkId' IS DISTINCT FROM NEW.network_id::text OR regexp_replace(lower(body->>'txHash'),'^0x','') IS DISTINCT FROM NEW.tx_hash
       OR body->>'nativeTokenId' IS DISTINCT FROM NEW.native_token_id::text
       OR (CASE WHEN NEW.family='evm' THEN lower(body->>'payerAddress') ELSE body->>'payerAddress' END) IS DISTINCT FROM NEW.payer_address_key
       OR regexp_replace(lower(body->>'blockHash'),'^0x','') IS DISTINCT FROM NEW.block_hash OR (body->>'blockNumber')::bigint IS DISTINCT FROM NEW.block_number
       OR body->>'status' IS DISTINCT FROM NEW.status OR body->>'totalFee' IS DISTINCT FROM NEW.total_fee_minor::text
       OR body->>'canonicality' IS DISTINCT FROM 'canonical' OR COALESCE(body->>'finality','') NOT IN ('solidified','finalized')
       OR (body->>'confirmations')::int IS DISTINCT FROM NEW.confirmations OR (body->>'verifiedAt')::timestamptz IS DISTINCT FROM NEW.verified_at
       OR NEW.effect_key<>fn_crypto_network_fee_key(NEW.family,NEW.network_id,NEW.tx_hash)
       OR (NEW.family='evm' AND NEW.payer_address_key!~'^0x[0-9a-f]{40}$') OR (NEW.family='tron' AND NEW.payer_address_key!~'^T[1-9A-HJ-NP-Za-km-z]{33}$')
       OR NOT EXISTS(SELECT 1 FROM cat_blockchain_networks n JOIN cat_tokens t ON t.network_id=n.id WHERE n.id=NEW.network_id AND n.chain_family=NEW.family
           AND t.id=NEW.native_token_id AND t.token_standard='native' AND COALESCE(t.contract_address,'')='' AND NEW.confirmations>=n.confirmations_needed)
       OR (SELECT count(DISTINCT value->>'name') FROM jsonb_array_elements(components))<>component_count
       OR EXISTS(SELECT 1 FROM jsonb_array_elements(components) c WHERE COALESCE(c->>'amount','') !~'^(0|[1-9][0-9]*)$'
           OR COALESCE(c->>'quantity','0') !~'^(0|[1-9][0-9]*)$' OR COALESCE(c->>'quantity','0')::numeric>9223372036854775807 OR c->>'name' IS NULL
           OR (NEW.family='evm' AND COALESCE(c->>'quantity','0')::numeric=0)
           OR (NEW.family='evm' AND c->>'name' NOT IN ('execution_gas','blob_gas')) OR (NEW.family='tron' AND c->>'name' NOT IN ('energy_fee','bandwidth_fee','other_chain_fee')))
       OR (NEW.family='evm' AND NOT components@>'[{"name":"execution_gas"}]')
       OR (NEW.family='tron' AND (NOT components@>'[{"name":"energy_fee"}]' OR NOT components@>'[{"name":"bandwidth_fee"}]')) THEN
        RAISE EXCEPTION 'network fee proof profile or economics mismatch' USING ERRCODE='23514'; END IF;
    SELECT sum((c->>'amount')::numeric) INTO total FROM jsonb_array_elements(components) c;
    SELECT concat_ws(E'\n','network-fee-proof.v1',NEW.family,NEW.network_id::text,NEW.tx_hash,NEW.payer_address_key,NEW.native_token_id::text,NEW.status,
        NEW.block_number::text,NEW.block_hash,NEW.total_fee_minor::text,string_agg((c->>'name')||':'||(c->>'amount')||':'||COALESCE(c->>'quantity','0'),E'\n' ORDER BY c->>'name'))
        INTO canonical FROM jsonb_array_elements(components) c;
    IF total IS DISTINCT FROM NEW.total_fee_minor OR NEW.proof_digest<>encode(digest(canonical,'sha256'),'hex')
       OR (SELECT count(DISTINCT s->>'providerId') FROM jsonb_array_elements(sources) s)<>2
       OR (SELECT count(DISTINCT s->>'trustGroup') FROM jsonb_array_elements(sources) s)<>2
       OR (SELECT count(DISTINCT regexp_replace(lower(s->>'headHash'),'^0x','')) FROM jsonb_array_elements(sources) s)<>1
       OR (SELECT count(DISTINCT s->>'headHeight') FROM jsonb_array_elements(sources) s)<>1
       OR EXISTS(SELECT 1 FROM jsonb_array_elements(sources) s WHERE COALESCE(btrim(s->>'providerId'),'')='' OR COALESCE(btrim(s->>'trustGroup'),'')=''
           OR s->>'digest' IS DISTINCT FROM NEW.proof_digest OR COALESCE(s->>'sourceDigest','')!~'^[0-9a-f]{64}$'
           OR COALESCE(regexp_replace(lower(s->>'headHash'),'^0x',''),'')!~'^[0-9a-f]{64}$'
           OR (s->>'headHeight')::bigint-NEW.block_number+1 IS DISTINCT FROM NEW.confirmations::bigint) THEN
        RAISE EXCEPTION 'network fee requires exact component total and independent common-head quorum' USING ERRCODE='23514'; END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION fn_crypto_balance_posting_transaction() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.recorder_type='CryptoPayment' THEN
        RAISE EXCEPTION 'payment attributes a receipt and never moves custody' USING ERRCODE='23514';
    END IF;
    NEW.posting_txid:=CASE WHEN NEW.recorder_type IN ('CryptoNativeFunding','CryptoSweep','TreasuryWithdrawal','CryptoWithdrawal','CryptoCustodyAdjustment','ChainSettlement','CryptoNetworkOperation') AND NEW.reversal_of_line_id IS NULL THEN txid_current() ELSE NULL END;
    RETURN NEW;
END;
$$;


CREATE FUNCTION fn_crypto_assert_network_fee(target UUID) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE f reg_crypto_fee_reservations; b reg_crypto_network_fee_bindings; p reg_crypto_network_fee_proofs; c reg_crypto_chain_settlements; d doc_crypto_network_operations; fee_purpose TEXT;
BEGIN
    SELECT * INTO STRICT f FROM reg_crypto_fee_reservations WHERE id=target;
    SELECT * INTO b FROM reg_crypto_network_fee_bindings WHERE reservation_id=f.id;
    SELECT * INTO p FROM reg_crypto_network_fee_proofs WHERE id=b.proof_id;
    SELECT * INTO c FROM reg_crypto_chain_settlements WHERE id=b.settlement_id;
    fee_purpose:=CASE WHEN f.purpose='treasury_withdrawal' THEN 'treasury' ELSE f.purpose END;
    IF f.status<>'settled' OR b.reservation_id IS NULL OR p.id IS NULL OR c.id IS NULL OR b.generation<>1 OR b.effect_key<>p.effect_key
       OR f.network_id<>p.network_id OR regexp_replace(lower(f.tx_hash),'^0x','')<>p.tx_hash OR f.native_token_id<>p.native_token_id
       OR f.actual_fee_minor<>p.total_fee_minor OR p.total_fee_minor>f.max_fee_minor
       OR c.network_fee_reservation_id IS DISTINCT FROM f.id OR c.network_id<>f.network_id OR regexp_replace(lower(c.tx_hash),'^0x','')<>p.tx_hash
       OR c.status<>p.status OR regexp_replace(lower(c.block_hash),'^0x','')<>p.block_hash OR c.block_height<>p.block_number
       OR c.native_token_id<>f.native_token_id OR c.total_fee_minor<>f.actual_fee_minor OR c.finality_status IS DISTINCT FROM p.evidence->>'finality' OR c.observed_at<>b.period
       OR NOT EXISTS(SELECT 1 FROM cat_wallets w JOIN cat_tokens t ON t.network_id=w.network_id WHERE w.id=f.payer_wallet_id AND w.network_id=f.network_id
           AND w.address_key=p.payer_address_key AND w.vault_key_id<>'' AND t.id=f.native_token_id AND t.token_standard='native' AND COALESCE(t.contract_address,'')='') THEN
        RAISE EXCEPTION 'network fee requires its exact final proof, reservation and normalized settlement' USING ERRCODE='23514'; END IF;
    IF (SELECT count(*) FROM reg_crypto_chain_settlement_components WHERE settlement_id=c.id)<>jsonb_array_length(p.evidence->'components')
       OR EXISTS(SELECT 1 FROM jsonb_array_elements(p.evidence->'components') AS part(value) WHERE NOT EXISTS(
           SELECT 1 FROM reg_crypto_chain_settlement_components stored WHERE stored.settlement_id=c.id AND stored.component=part.value->>'name'
           AND stored.amount_minor=(part.value->>'amount')::numeric AND stored.quantity=COALESCE(part.value->>'quantity','0')::bigint)) THEN
        RAISE EXCEPTION 'network fee requires its exact normalized fee components' USING ERRCODE='23514'; END IF;
    IF b.recorder_type='ChainSettlement' THEN
        IF f.purpose='activation' OR b.recorder_id<>f.basis_id OR c.basis_type<>f.basis_type OR c.basis_id<>f.basis_id
           OR (p.total_fee_minor>0 AND (b.movement_line_id IS DISTINCT FROM f.id OR b.cost_line_id IS DISTINCT FROM f.id)) THEN
            RAISE EXCEPTION 'network fee belongs to its original settlement owner' USING ERRCODE='23514'; END IF;
    ELSE
        SELECT * INTO d FROM doc_crypto_network_operations WHERE id=b.recorder_id;
        IF d.id IS NULL OR f.purpose<>'activation' OR d.network_fee_reservation_id IS DISTINCT FROM f.id OR d.chain_settlement_id IS DISTINCT FROM c.id
           OR d.network_id<>f.network_id OR d.source_wallet_id<>f.payer_wallet_id OR d.cost_token_id<>f.native_token_id OR d.cost_amount<>p.total_fee_minor
           OR NOT d.posted OR d.posted_version<>1 OR d.finalized_at IS DISTINCT FROM b.period OR d.status<>p.status
           OR d.basis_type<>f.basis_type OR d.basis_id IS DISTINCT FROM f.basis_id OR c.basis_type<>'CryptoNetworkOperation' OR c.basis_id<>d.id
           OR (SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=d.id)<>(CASE WHEN p.total_fee_minor>0 THEN 1 ELSE 0 END)
           OR (SELECT count(*) FROM reg_crypto_platform_cost_movements WHERE recorder_id=d.id)<>(CASE WHEN p.total_fee_minor>0 THEN 1 ELSE 0 END) THEN
            RAISE EXCEPTION 'network operation requires its one proven network fee outcome' USING ERRCODE='23514'; END IF;
    END IF;
    IF p.total_fee_minor=0 THEN
        IF b.movement_line_id IS NOT NULL OR b.cost_line_id IS NOT NULL
           OR EXISTS(SELECT 1 FROM reg_crypto_balance_movements WHERE line_id=f.id) OR EXISTS(SELECT 1 FROM reg_crypto_platform_cost_movements WHERE line_id=f.id) THEN
            RAISE EXCEPTION 'zero network fee cannot create money movements' USING ERRCODE='23514'; END IF;
    ELSIF NOT EXISTS(SELECT 1 FROM reg_crypto_balance_movements WHERE line_id=b.movement_line_id AND recorder_type=b.recorder_type AND recorder_id=b.recorder_id AND recorder_version=1
            AND record_type='expense' AND period=b.period AND wallet_id=f.payer_wallet_id AND token_id=f.native_token_id AND amount=f.actual_fee_minor AND reversal_of_line_id IS NULL)
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_platform_cost_movements WHERE line_id=b.cost_line_id AND recorder_type=b.recorder_type AND recorder_id=b.recorder_id AND recorder_version=1
            AND record_type='expense' AND period=b.period AND network_id=f.network_id AND cost_token_id=f.native_token_id AND cost_type='chain_network_fee'
            AND operation_token_id IS NOT DISTINCT FROM CASE WHEN b.recorder_type='CryptoNetworkOperation' THEN NULL::uuid ELSE f.operation_token_id END
            AND purpose=fee_purpose AND provider='chain' AND external_order_id=f.tx_hash AND amount=f.actual_fee_minor AND reversal_of_line_id IS NULL) THEN
        RAISE EXCEPTION 'network fee requires one exact native expense and equal platform cost' USING ERRCODE='23514'; END IF;
    IF EXISTS(SELECT 1 FROM reg_crypto_fee_movements WHERE line_id=f.id OR (recorder_type=b.recorder_type AND recorder_id=b.recorder_id))
       OR EXISTS(SELECT 1 FROM reg_crypto_merchant_balance_movements WHERE line_id=f.id OR (recorder_type=b.recorder_type AND recorder_id=b.recorder_id))
       OR EXISTS(SELECT 1 FROM reg_crypto_provider_balance_movements WHERE line_id=f.id OR (recorder_type=b.recorder_type AND recorder_id=b.recorder_id))
       OR (SELECT count(*) FROM sys_outbox WHERE aggregate_type='CryptoNetworkFee' AND aggregate_id=f.id AND event_type='fee_recognized')<>1
       OR NOT EXISTS(SELECT 1 FROM sys_outbox WHERE aggregate_type='CryptoNetworkFee' AND aggregate_id=f.id AND event_type='fee_recognized' AND payload->>'entityId'=f.id::text
            AND payload->>'entityType'='CryptoNetworkFee' AND payload->>'action'='fee_recognized' AND payload->>'effectKey'=b.effect_key AND payload->>'proofId'=p.id::text AND payload->>'generation'='1') THEN
        RAISE EXCEPTION 'network fee cannot change merchant liability, revenue or provider balance and requires outbox' USING ERRCODE='23514'; END IF;
END;
$$;

CREATE FUNCTION fn_crypto_network_fee_binding_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.movement_line_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM reg_crypto_balance_movements WHERE line_id=NEW.movement_line_id AND posting_txid=txid_current()) THEN
        RAISE EXCEPTION 'network fee requires a new expense in this transaction' USING ERRCODE='23514'; END IF;
    PERFORM fn_crypto_assert_network_fee(NEW.reservation_id); RETURN NULL;
END;
$$;

CREATE FUNCTION fn_crypto_network_fee_state_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target UUID;
BEGIN
    IF TG_TABLE_NAME='reg_crypto_fee_reservations' THEN
        IF NEW.status<>'settled' THEN RETURN NULL; END IF; target:=NEW.id;
    ELSIF TG_TABLE_NAME='reg_crypto_chain_settlement_components' THEN
        SELECT network_fee_reservation_id INTO target FROM reg_crypto_chain_settlements WHERE id=NEW.settlement_id;
    ELSE target:=NEW.network_fee_reservation_id; END IF;
    IF target IS NOT NULL THEN PERFORM fn_crypto_assert_network_fee(target); END IF; RETURN NULL;
END;
$$;

CREATE FUNCTION fn_crypto_network_fee_financial_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target UUID;
BEGIN
    IF NEW.reversal_of_line_id IS NOT NULL AND EXISTS(SELECT 1 FROM reg_crypto_network_fee_bindings WHERE movement_line_id=NEW.reversal_of_line_id OR cost_line_id=NEW.reversal_of_line_id) THEN
        RAISE EXCEPTION 'recognized network fee requires explicit compensation workflow' USING ERRCODE='23514'; END IF;
    IF NEW.recorder_type='ChainSettlement' AND EXISTS(SELECT 1 FROM reg_crypto_fee_reservations WHERE basis_id=NEW.recorder_id)
       AND NOT EXISTS(SELECT 1 FROM reg_crypto_network_fee_bindings WHERE recorder_type=NEW.recorder_type AND recorder_id=NEW.recorder_id AND (movement_line_id=NEW.line_id OR cost_line_id=NEW.line_id)) THEN
        RAISE EXCEPTION 'network fee movement has no exact recognition owner' USING ERRCODE='23514'; END IF;
    FOR target IN SELECT b.reservation_id FROM reg_crypto_network_fee_bindings b WHERE (b.recorder_id=NEW.recorder_id AND b.recorder_type=NEW.recorder_type) OR b.movement_line_id=NEW.line_id OR b.cost_line_id=NEW.line_id
        UNION SELECT f.id FROM reg_crypto_fee_reservations f WHERE f.id=NEW.line_id LOOP
        PERFORM fn_crypto_assert_network_fee(target);
    END LOOP; RETURN NULL;
END;
$$;

CREATE FUNCTION fn_crypto_network_fee_basis_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME='doc_crypto_network_operations' THEN
        IF TG_OP='DELETE' THEN
            IF OLD.network_fee_reservation_id IS NOT NULL THEN RAISE EXCEPTION 'recognized network fee history is immutable' USING ERRCODE='23514'; END IF; RETURN OLD;
        END IF;
        IF OLD.network_fee_reservation_id IS NOT NULL AND (NEW.network_fee_reservation_id IS DISTINCT FROM OLD.network_fee_reservation_id
            OR (NEW.network_id,NEW.source_wallet_id,NEW.cost_token_id,NEW.cost_amount,NEW.tx_hash,NEW.chain_settlement_id,NEW.finalized_at,NEW.status,NEW.basis_type,NEW.basis_id,NEW.target_wallet_id,NEW.target_address,NEW.date)
               IS DISTINCT FROM (OLD.network_id,OLD.source_wallet_id,OLD.cost_token_id,OLD.cost_amount,OLD.tx_hash,OLD.chain_settlement_id,OLD.finalized_at,OLD.status,OLD.basis_type,OLD.basis_id,OLD.target_wallet_id,OLD.target_address,OLD.date)
            OR NEW.deletion_mark OR NEW._deleted_at IS NOT NULL
            OR ((NEW.posted,NEW.posted_version) IS DISTINCT FROM (OLD.posted,OLD.posted_version) AND NOT (NOT OLD.posted AND OLD.posted_version=0 AND NEW.posted AND NEW.posted_version=1))) THEN
            RAISE EXCEPTION 'recognized network operation basis and outcome are immutable' USING ERRCODE='23514'; END IF; RETURN NEW;
    END IF;
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'network fee reservation history is immutable' USING ERRCODE='23514';
    END IF;
    IF (NEW.network_id,NEW.payer_wallet_id,NEW.native_token_id,NEW.operation_token_id,NEW.reserved_principal_minor,NEW.basis_type,NEW.basis_id,NEW.purpose,NEW.attempt,NEW.strategy,NEW.reserved_at)
       IS DISTINCT FROM (OLD.network_id,OLD.payer_wallet_id,OLD.native_token_id,OLD.operation_token_id,OLD.reserved_principal_minor,OLD.basis_type,OLD.basis_id,OLD.purpose,OLD.attempt,OLD.strategy,OLD.reserved_at)
       OR (OLD.status='settled' AND (to_jsonb(NEW)-'updated_at') IS DISTINCT FROM (to_jsonb(OLD)-'updated_at')) THEN
        RAISE EXCEPTION 'network fee reservation basis and settled outcome are immutable' USING ERRCODE='23514'; END IF;
    IF ((OLD.tx_hash<>'' AND NEW.tx_hash<>OLD.tx_hash) OR (OLD.unsigned_fingerprint<>'' AND NEW.unsigned_fingerprint<>OLD.unsigned_fingerprint)) AND NOT EXISTS(
        SELECT 1 FROM reg_crypto_signing_authorizations a JOIN cat_wallets w ON w.id=NEW.payer_wallet_id JOIN cat_blockchain_networks n ON n.id=w.network_id
        WHERE a.basis_type=NEW.basis_type AND a.basis_id=NEW.basis_id AND a.network_id=NEW.network_id AND a.purpose=NEW.purpose AND a.signed_envelope IS NOT NULL
          AND a.unsigned_fingerprint=NEW.unsigned_fingerprint AND regexp_replace(lower(a.tx_hash),'^0x','')=regexp_replace(lower(NEW.tx_hash),'^0x','')
          AND w.address_key=CASE WHEN n.chain_family='evm' THEN lower(a.source_address) ELSE a.source_address END
          AND w.address_key=CASE WHEN n.chain_family='evm' THEN lower(a.fee_payer_address) ELSE a.fee_payer_address END) THEN
        RAISE EXCEPTION 'network fee replacement requires an exact persisted signed authorization' USING ERRCODE='23514'; END IF;
    IF NEW.status='released' AND (NEW.tx_hash<>'' OR EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations WHERE basis_type=NEW.basis_type AND basis_id=NEW.basis_id AND (signed_envelope IS NOT NULL OR status<>'failed'))) THEN
        RAISE EXCEPTION 'network fee for an unknown signing outcome cannot be released' USING ERRCODE='23514'; END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION fn_crypto_network_fee_replacement_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF ((OLD.tx_hash<>'' AND NEW.tx_hash<>OLD.tx_hash) OR (OLD.unsigned_fingerprint<>'' AND NEW.unsigned_fingerprint<>OLD.unsigned_fingerprint))
       AND NOT EXISTS(SELECT 1 FROM reg_crypto_fee_reservation_events e WHERE e.reservation_id=NEW.id AND e.transition_txid=txid_current()
           AND e.metadata->>'oldTxHash'=OLD.tx_hash AND e.metadata->>'newTxHash'=NEW.tx_hash
           AND e.metadata->>'oldFingerprint'=OLD.unsigned_fingerprint AND e.metadata->>'newFingerprint'=NEW.unsigned_fingerprint) THEN
        RAISE EXCEPTION 'network fee replacement requires an immutable transition event in this transaction' USING ERRCODE='23514'; END IF;
    RETURN NULL;
END;
$$;

-- Serialize legacy normalized receipts before they can hold a receipt row while
-- awaiting a balance held by a new principal/fee settlement.
CREATE FUNCTION fn_crypto_network_fee_legacy_owner_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE reservation reg_crypto_fee_reservations; network UUID; hash TEXT; family TEXT; owner UUID; asset UUID; effect TEXT;
BEGIN
    IF TG_TABLE_NAME='reg_crypto_chain_settlements' THEN
        network:=NEW.network_id; hash:=NEW.tx_hash;
        IF NEW.network_fee_reservation_id IS NOT NULL THEN SELECT * INTO STRICT reservation FROM reg_crypto_fee_reservations WHERE id=NEW.network_fee_reservation_id;
        ELSE
            SELECT * INTO reservation FROM reg_crypto_fee_reservations f WHERE f.network_id=network AND regexp_replace(lower(f.tx_hash),'^0x','')=regexp_replace(lower(hash),'^0x','')
                AND ((f.basis_type=NEW.basis_type AND f.basis_id=NEW.basis_id) OR EXISTS(SELECT 1 FROM doc_crypto_network_operations d WHERE NEW.basis_type='CryptoNetworkOperation' AND d.id=NEW.basis_id AND d.basis_type=f.basis_type AND d.basis_id=f.basis_id)) LIMIT 1;
        END IF;
        owner:=reservation.payer_wallet_id;asset:=reservation.native_token_id;
    ELSE
        IF NEW.recorder_type='ChainSettlement' THEN SELECT * INTO reservation FROM reg_crypto_fee_reservations WHERE id=NEW.line_id;
            network:=reservation.network_id;hash:=reservation.tx_hash;owner:=reservation.payer_wallet_id;asset:=reservation.native_token_id;
        ELSIF NEW.recorder_type='CryptoNetworkOperation' THEN SELECT d.network_id,d.tx_hash,d.source_wallet_id,d.cost_token_id INTO network,hash,owner,asset FROM doc_crypto_network_operations d WHERE id=NEW.recorder_id;
        ELSE RETURN NEW; END IF;
    END IF;
    IF network IS NULL OR owner IS NULL THEN RETURN NEW; END IF;
    SELECT chain_family INTO STRICT family FROM cat_blockchain_networks WHERE id=network;
    PERFORM fn_crypto_lock_receipt_balance(owner,asset);
    effect:=fn_crypto_network_fee_key(family,network,hash);
    PERFORM pg_advisory_xact_lock(hashtextextended('network-fee:'||effect,0));
    IF TG_TABLE_NAME='reg_crypto_chain_settlements' THEN
        IF EXISTS(SELECT 1 FROM reg_crypto_network_fee_bindings b WHERE b.effect_key=effect AND b.reservation_id IS DISTINCT FROM NEW.network_fee_reservation_id) THEN
            RAISE EXCEPTION 'network fee already has a different accounting owner' USING ERRCODE='23514'; END IF;
    ELSE
        IF EXISTS(SELECT 1 FROM reg_crypto_network_fee_bindings b WHERE b.effect_key=effect AND (b.recorder_type<>NEW.recorder_type OR b.recorder_id<>NEW.recorder_id OR b.movement_line_id IS DISTINCT FROM NEW.line_id)) THEN
            RAISE EXCEPTION 'network fee already has a different accounting owner' USING ERRCODE='23514'; END IF;
    END IF;
    RETURN NEW;
END;
$$;

-- Candidate lookup is economic, never a tx-hash-only classification. Retired
-- catalog aliases and signed/held/failed authorizations retain their history.
CREATE FUNCTION fn_crypto_expected_transfer_candidates(network UUID, hash TEXT, kind TEXT, source TEXT, destination TEXT, contract TEXT, quantity NUMERIC)
RETURNS TABLE(expected_id UUID,authorization_id UUID,owner_type TEXT,owner_id UUID,intent_valid BOOLEAN) LANGUAGE sql STABLE AS $$
 SELECT x.id,a.id,a.basis_type,a.basis_id,
   COALESCE(a.network_id=x.network_id AND a.chain_family=n.chain_family AND a.network_identifier=n.network_identifier AND x.ordinal=0
    AND (SELECT count(*) FROM reg_crypto_expected_chain_effects other WHERE other.signing_authorization_id=a.id)=1
    AND ((a.purpose='native_funding' AND a.basis_type='CryptoNativeFunding') OR (a.purpose='sweep' AND a.basis_type='CryptoSweepAttempt')
      OR (a.purpose='withdrawal' AND a.basis_type IN ('WithdrawalRequest','CryptoAMLWithdrawalLine')) OR (a.purpose='treasury_withdrawal' AND a.basis_type='TreasuryWithdrawal'))
    AND regexp_replace(lower(a.tx_hash),'^0x','')=regexp_replace(lower(x.tx_hash),'^0x','')
    AND x.source_address_key=CASE WHEN n.chain_family='evm' THEN lower(a.source_address) ELSE a.source_address END
    AND x.destination_address_key=CASE WHEN n.chain_family='evm' THEN lower(a.destination_address) ELSE a.destination_address END
    AND x.contract_address_key=CASE WHEN n.chain_family='evm' THEN lower(a.contract_address) ELSE a.contract_address END
    AND x.amount=a.amount_minor_units AND x.event_kind=CASE WHEN a.contract_address='' THEN 'native_value' ELSE 'contract_log' END
    AND lower(replace(t.token_standard,'-',''))=lower(replace(a.asset_standard,'-',''))
    AND n.chain_family IN ('tron','evm')
    AND a.signed_envelope->>'family'=a.chain_family AND a.unsigned_envelope->>'family'=a.chain_family
    AND a.signed_envelope->>'schemaVersion'='1' AND a.unsigned_envelope->>'schemaVersion'=a.signed_envelope->>'schemaVersion'
    AND a.signed_envelope->>'generation'=a.generation::text AND a.unsigned_envelope->>'generation'=a.generation::text
    AND a.signed_envelope->>'unsignedFingerprint'=a.unsigned_fingerprint AND a.unsigned_envelope->>'unsignedFingerprint'=a.unsigned_fingerprint
    AND (a.schema_version=1 OR (a.network_sequence<>'' AND a.signed_envelope->>'networkSequence'=a.network_sequence AND a.unsigned_envelope->>'networkSequence'=a.network_sequence))
    AND regexp_replace(lower(a.signed_envelope->>'txHash'),'^0x','')=regexp_replace(lower(a.tx_hash),'^0x','')
    AND a.signed_envelope->'payload' IS NOT NULL AND a.signed_envelope->'payload'<>'null'::jsonb
    AND EXISTS(SELECT 1 FROM cat_wallets w WHERE w.network_id=x.network_id AND w.address_key=x.source_address_key AND w.vault_key_id<>''),false)
 FROM reg_crypto_expected_chain_effects x JOIN reg_crypto_signing_authorizations a ON a.id=x.signing_authorization_id
 JOIN cat_blockchain_networks n ON n.id=x.network_id JOIN cat_tokens t ON t.id=x.token_id AND t.network_id=x.network_id
 WHERE x.network_id=network AND regexp_replace(lower(x.tx_hash),'^0x','')=regexp_replace(lower(hash),'^0x','') AND x.event_kind=kind
 AND x.source_address_key=CASE WHEN n.chain_family='evm' THEN lower(source) ELSE source END
 AND x.destination_address_key=CASE WHEN n.chain_family='evm' THEN lower(destination) ELSE destination END
 AND x.contract_address_key=CASE WHEN n.chain_family='evm' THEN lower(contract) ELSE contract END
 AND x.contract_address_key=CASE WHEN n.chain_family='evm' THEN lower(COALESCE(t.contract_address,'')) ELSE COALESCE(t.contract_address,'') END
 AND x.amount=quantity AND quantity>0 AND a.signed_envelope IS NOT NULL;
$$;

CREATE FUNCTION fn_crypto_expected_owner_matches(expected UUID,recorder TEXT,document UUID) RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
 SELECT COALESCE(EXISTS(SELECT 1 FROM reg_crypto_expected_chain_effects x JOIN reg_crypto_signing_authorizations a ON a.id=x.signing_authorization_id WHERE x.id=expected AND (
   (a.basis_type='CryptoNativeFunding' AND recorder='CryptoNativeFunding' AND document=a.basis_id)
   OR (a.basis_type='CryptoSweepAttempt' AND recorder='CryptoSweep' AND EXISTS(SELECT 1 FROM reg_crypto_sweep_attempts s WHERE s.id=a.basis_id AND s.sweep_id=document))
   OR (a.basis_type='TreasuryWithdrawal' AND recorder='TreasuryWithdrawal' AND document=a.basis_id)
   OR (a.basis_type IN ('WithdrawalRequest','CryptoAMLWithdrawalLine') AND recorder='CryptoWithdrawal' AND EXISTS(SELECT 1 FROM doc_crypto_withdrawals d WHERE d.id=document AND d.basis_type=a.basis_type AND d.basis_id=a.basis_id))
 )),false);
$$;

CREATE FUNCTION fn_crypto_validate_expected_route(expected UUID,proof UUID) RETURNS UUID LANGUAGE plpgsql AS $$
DECLARE v reg_crypto_transfer_verifications; o reg_crypto_accounting_effect_occurrences; e reg_crypto_accounting_effects; matches INT; found UUID; valid BOOLEAN;
BEGIN
 SELECT * INTO STRICT v FROM reg_crypto_transfer_verifications WHERE id=proof;
 SELECT occurrence.* INTO STRICT o FROM reg_crypto_accounting_effect_evidence link JOIN reg_crypto_accounting_effect_occurrences occurrence ON occurrence.id=link.occurrence_id WHERE link.verification_id=v.id;
 SELECT * INTO STRICT e FROM reg_crypto_accounting_effects WHERE id=o.effect_id;
 IF v.evidence->>'outcome' IS DISTINCT FROM 'verified' OR v.evidence->>'executionStatus' IS DISTINCT FROM 'succeeded'
    OR v.evidence#>>'{event,canonicality}' IS DISTINCT FROM 'canonical' OR v.evidence#>>'{event,finality}' IS DISTINCT FROM 'solidified'
    OR v.evidence->>'matchingEffects' IS DISTINCT FROM '1'
    OR NOT EXISTS(SELECT 1 FROM cat_blockchain_networks n WHERE n.id=v.network_id AND v.confirmations>=n.confirmations_needed) THEN
  RAISE EXCEPTION 'expected route requires unambiguous independent final transfer proof' USING ERRCODE='23514'; END IF;
 SELECT count(*),(array_agg(c.expected_id))[1],COALESCE(bool_and(c.intent_valid),false) INTO matches,found,valid FROM fn_crypto_expected_transfer_candidates(e.network_id,e.tx_hash,e.event_kind,o.source_address,o.destination_address,o.contract_address,o.amount) c;
 IF matches<>1 OR NOT valid OR found IS DISTINCT FROM expected THEN RAISE EXCEPTION 'expected route conflicts with its exact signed intent' USING ERRCODE='23514'; END IF;
 IF EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings p
   JOIN reg_crypto_accounting_effects known ON known.id=p.effect_id
   JOIN reg_crypto_accounting_effect_occurrences occurrence ON occurrence.id=p.occurrence_id
   WHERE known.network_id=e.network_id AND known.tx_hash=e.tx_hash AND known.event_kind=e.event_kind
    AND occurrence.source_address=o.source_address AND occurrence.destination_address=o.destination_address
    AND occurrence.contract_address=o.contract_address AND occurrence.amount=o.amount
    AND (p.effect_id<>e.id OR NOT fn_crypto_expected_owner_matches(expected,p.recorder_type,p.recorder_id))) THEN
  RAISE EXCEPTION 'expected transfer already has another accounting owner' USING ERRCODE='23514'; END IF;
 RETURN e.id;
END; $$;

CREATE FUNCTION fn_crypto_expected_transfer_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE a reg_crypto_signing_authorizations; effect UUID; actual UUID;
BEGIN
 IF TG_TABLE_NAME='reg_crypto_expected_transfer_requests' THEN
  SELECT auth.* INTO STRICT a FROM reg_crypto_expected_chain_effects x JOIN reg_crypto_signing_authorizations auth ON auth.id=x.signing_authorization_id WHERE x.id=NEW.expected_effect_id;
  IF NEW.owner_type<>a.basis_type OR NEW.owner_id<>a.basis_id OR NOT EXISTS(
    SELECT 1 FROM fn_crypto_expected_transfer_candidates((NEW.first_event->>'networkId')::uuid,NEW.first_event->>'txHash',NEW.first_event->>'eventKind',NEW.first_event->>'fromAddress',NEW.first_event->>'toAddress',COALESCE(NEW.first_event->>'tokenContract',''),(NEW.first_event->>'amount')::numeric) c WHERE c.expected_id=NEW.expected_effect_id) THEN
   RAISE EXCEPTION 'expected transfer request requires its exact economic observation and original owner' USING ERRCODE='23514'; END IF;
 ELSIF TG_TABLE_NAME='reg_crypto_expected_transfer_routes' THEN
  effect:=fn_crypto_validate_expected_route(NEW.expected_effect_id,NEW.first_verification_id);
  IF NEW.effect_id<>effect OR NOT EXISTS(SELECT 1 FROM reg_crypto_expected_transfer_requests r WHERE r.expected_effect_id=NEW.expected_effect_id AND r.owner_type=NEW.owner_type AND r.owner_id=NEW.owner_id) THEN
   RAISE EXCEPTION 'expected transfer route owner is immutable' USING ERRCODE='23514'; END IF;
 ELSE
  SELECT expected_effect_id INTO STRICT actual FROM reg_crypto_expected_transfer_routes WHERE effect_id=NEW.effect_id;
  effect:=fn_crypto_validate_expected_route(actual,NEW.verification_id);
  IF NEW.effect_id<>effect THEN RAISE EXCEPTION 'expected route evidence cannot change logical identity' USING ERRCODE='23514'; END IF;
 END IF;RETURN NEW;
END; $$;

CREATE FUNCTION fn_crypto_expected_request_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF (SELECT count(*) FROM sys_outbox WHERE aggregate_type='CryptoExpectedTransfer' AND aggregate_id=NEW.expected_effect_id AND event_type='settlement_requested')<>1
 OR (SELECT count(*) FROM sys_outbox WHERE aggregate_type='CryptoExpectedTransfer' AND aggregate_id=NEW.expected_effect_id AND event_type='settlement_requested'
    AND payload->>'ownerType'=NEW.owner_type AND payload->>'ownerId'=NEW.owner_id::text AND payload->>'expectedEffectId'=NEW.expected_effect_id::text)<>1 THEN
  RAISE EXCEPTION 'expected settlement request requires its original-owner outbox' USING ERRCODE='23514'; END IF; RETURN NULL;
END; $$;

-- Pending ownership forbids fallback even before the observer has a final
-- proof, and even when the signing/workflow label says failed or expired.
CREATE FUNCTION fn_crypto_expected_posting_owner_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE o reg_crypto_accounting_effect_occurrences; e reg_crypto_accounting_effects; expected UUID;
BEGIN
 SELECT * INTO STRICT o FROM reg_crypto_accounting_effect_occurrences WHERE id=NEW.occurrence_id;
 SELECT * INTO STRICT e FROM reg_crypto_accounting_effects WHERE id=o.effect_id;
 FOR expected IN SELECT c.expected_id FROM fn_crypto_expected_transfer_candidates(e.network_id,e.tx_hash,e.event_kind,o.source_address,o.destination_address,o.contract_address,o.amount) c LOOP
  IF NOT fn_crypto_expected_owner_matches(expected,NEW.recorder_type,NEW.recorder_id) THEN
   RAISE EXCEPTION 'expected transfer belongs to its original settlement owner' USING ERRCODE='23514'; END IF;
  IF EXISTS(SELECT 1 FROM reg_crypto_expected_transfer_routes r WHERE r.expected_effect_id=expected AND r.effect_id<>e.id) THEN
   RAISE EXCEPTION 'expected transfer already has another logical route' USING ERRCODE='23514'; END IF;
 END LOOP; RETURN NEW;
END; $$;

CREATE FUNCTION fn_crypto_expected_external_payment_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE blocked BOOLEAN;
BEGIN
    SELECT EXISTS(SELECT 1 FROM cat_wallets w JOIN cat_tokens t ON t.id=NEW.token_id
        CROSS JOIN LATERAL fn_crypto_expected_transfer_candidates(NEW.network_id,NEW.tx_hash,NEW.event_kind,NEW.from_address,
            w.address,COALESCE(t.contract_address,''),NEW.amount) c WHERE w.id=NEW.wallet_id) INTO blocked;
    IF blocked THEN RAISE EXCEPTION 'expected transfer cannot create an external payment' USING ERRCODE='23514'; END IF;
    RETURN NEW;
END;
$$;


CREATE FUNCTION fn_crypto_adjustment_accounting_basis() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE d doc_crypto_payments;
BEGIN
    IF TG_OP='DELETE' THEN RAISE EXCEPTION 'crypto adjustment history is immutable' USING ERRCODE='23514'; END IF;
    IF TG_OP='INSERT' THEN
        IF NEW.status<>'pending_approval' THEN
            RAISE EXCEPTION 'new crypto adjustment requires model 2 proposal' USING ERRCODE='23514';
        END IF;
        NEW.proposal_txid:=txid_current();
    ELSIF (OLD.basis_kind,OLD.proposal_txid,OLD.created_at) IS DISTINCT FROM (NEW.basis_kind,NEW.proposal_txid,NEW.created_at) THEN
        RAISE EXCEPTION 'crypto adjustment accounting basis is immutable' USING ERRCODE='23514';
    END IF;
    IF NEW.mode<>'payment_reversal' OR (TG_OP='UPDATE' AND NEW.status<>'posted') THEN RETURN NEW; END IF;
    SELECT * INTO STRICT d FROM doc_crypto_payments WHERE id=NEW.payment_id;
    IF d.status<>'settled' OR NOT d.posted OR d.posted_version<>NEW.posted_version OR d.receipt_id IS NULL THEN
        RAISE EXCEPTION 'crypto adjustment requires current settled payment generation' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;


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

CREATE FUNCTION fn_crypto_assert_adjustment_accounting(target UUID) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE a doc_crypto_adjustments; kind TEXT; source_table TEXT; selected_count INT; movement_count INT; exact_count INT;
DECLARE suspense NUMERIC; merchant NUMERIC; fee NUMERIC;
BEGIN
    SELECT * INTO a FROM doc_crypto_adjustments WHERE id=target;
    IF NOT FOUND THEN RETURN; END IF;
    IF a.mode='payment_reversal' THEN
        SELECT COALESCE(SUM(amount) FILTER(WHERE source_register='suspense'),0),
            COALESCE(SUM(amount) FILTER(WHERE source_register='merchant'),0),
            COALESCE(SUM(amount) FILTER(WHERE source_register='fee'),0)
            INTO suspense,merchant,fee FROM reg_crypto_adjustment_sources WHERE adjustment_id=a.id;
        IF suspense<=0 OR suspense<>merchant+fee THEN
            RAISE EXCEPTION 'crypto adjustment requires balanced source selection' USING ERRCODE='23514';
        END IF;
    ELSIF EXISTS(SELECT 1 FROM reg_crypto_adjustment_sources WHERE adjustment_id=a.id) THEN
        RAISE EXCEPTION 'wallet variance cannot select payment sources' USING ERRCODE='23514';
    END IF;
    FOREACH kind IN ARRAY ARRAY['custody','suspense','merchant','fee'] LOOP
        source_table:=CASE kind WHEN 'custody' THEN 'reg_crypto_balance_movements' WHEN 'suspense' THEN 'reg_crypto_receipt_suspense_movements'
            WHEN 'merchant' THEN 'reg_crypto_merchant_balance_movements' ELSE 'reg_crypto_fee_movements' END;
        SELECT count(*) INTO selected_count FROM reg_crypto_adjustment_sources WHERE adjustment_id=a.id AND source_register=kind;
        EXECUTE format('SELECT count(*) FROM %I WHERE recorder_id=$1',source_table) INTO movement_count USING a.id;
        IF a.status<>'posted' THEN
            IF movement_count<>0 THEN RAISE EXCEPTION 'undecided adjustment cannot have financial movements' USING ERRCODE='23514'; END IF;
        ELSIF a.mode='payment_reversal' THEN
            EXECUTE format('SELECT count(*) FROM %1$I m JOIN reg_crypto_adjustment_sources s ON s.adjustment_id=$1 AND s.source_register=$2 AND s.source_line_id=m.reversal_of_line_id
                JOIN %1$I original ON original.line_id=s.source_line_id
                WHERE m.recorder_id=$1 AND m.recorder_type=''CryptoAdjustment'' AND m.recorder_version=1
                AND m.record_type<>original.record_type AND m.amount=s.amount AND m.reversal_reason=''manual_financial_correction''
                AND (to_jsonb(m)->>''receipt_id'',to_jsonb(m)->>''merchant_id'',m.token_id,to_jsonb(m)->>''fee_type'')
                    IS NOT DISTINCT FROM (to_jsonb(original)->>''receipt_id'',to_jsonb(original)->>''merchant_id'',original.token_id,to_jsonb(original)->>''fee_type'')',source_table)
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
            RAISE EXCEPTION 'wallet variance cannot change merchant, fee or suspense balances' USING ERRCODE='23514';
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
                AND payload->>'basisKind'=a.basis_kind
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

CREATE FUNCTION fn_crypto_adjustment_cannot_claim_effect() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.recorder_type='CryptoAdjustment' OR EXISTS(SELECT 1 FROM doc_crypto_adjustments WHERE id=NEW.recorder_id) THEN
        RAISE EXCEPTION 'manual correction references source posting and cannot claim a chain effect' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;

-- Fee/resource and AML withdrawal writers already lock an entire owner/network.
-- Custody admission must use the same granularity before touching any wallet,
-- including disabled token scopes that can be enabled concurrently.
CREATE FUNCTION fn_crypto_lock_custody_scopes(wallets UUID[]) RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
    PERFORM s.version FROM sys_crypto_aml_routing_scopes s
    WHERE EXISTS(SELECT 1 FROM cat_wallets w WHERE w.id=ANY(wallets)
        AND (w.custody_merchant_id,w.network_id)=(s.merchant_id,s.network_id))
    ORDER BY s.merchant_id,s.network_id,s.token_id FOR UPDATE OF s;
END;
$$;

CREATE FUNCTION fn_crypto_lock_custody_wallets(wallets UUID[]) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE found_count INT; expected_count INT;
BEGIN
    IF array_position(wallets,NULL) IS NOT NULL THEN
        RAISE EXCEPTION 'custody wallet lock requires valid wallet identifiers' USING ERRCODE='23514';
    END IF;
    PERFORM fn_crypto_lock_custody_scopes(wallets);
    PERFORM id FROM cat_wallets WHERE id=ANY(wallets) ORDER BY id FOR UPDATE;
    GET DIAGNOSTICS found_count=ROW_COUNT;
    SELECT count(DISTINCT value) INTO expected_count FROM unnest(wallets) value;
    IF found_count<>expected_count THEN
        RAISE EXCEPTION 'custody wallet lock target is missing' USING ERRCODE='23514';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION fn_crypto_aml_lock_resource_scopes(reservation UUID) RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
    PERFORM fn_crypto_lock_custody_scopes(ARRAY(SELECT payer_wallet_id FROM reg_crypto_fee_reservations WHERE id=reservation));
END;
$$;

CREATE OR REPLACE FUNCTION fn_crypto_aml_lock_fee_wallet_scopes(payer UUID,asset UUID) RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
    PERFORM fn_crypto_lock_custody_scopes(ARRAY[payer]);
END;
$$;

-- Candidate selection must skip a busy *complete* footprint, without keeping
-- a prefix of its scopes/wallets and subsequently waiting in another order.
-- The exception block is a subtransaction: NOWAIT failure releases its locks.
CREATE FUNCTION fn_crypto_try_lock_custody_wallets(wallets UUID[],scope_merchant UUID DEFAULT NULL,scope_network UUID DEFAULT NULL) RETURNS BOOLEAN LANGUAGE plpgsql AS $$
DECLARE found_count INT; expected_count INT;
BEGIN
    IF wallets IS NULL OR cardinality(wallets)=0 OR array_position(wallets,NULL) IS NOT NULL
        OR (scope_merchant IS NULL)<>(scope_network IS NULL) THEN
        RAISE EXCEPTION 'custody selection requires valid wallet identifiers' USING ERRCODE='23514';
    END IF;
    PERFORM s.version FROM sys_crypto_aml_routing_scopes s
        WHERE EXISTS(SELECT 1 FROM cat_wallets w WHERE w.id=ANY(wallets)
            AND (w.custody_merchant_id,w.network_id)=(s.merchant_id,s.network_id))
            OR (s.merchant_id=scope_merchant AND s.network_id=scope_network)
        ORDER BY s.merchant_id,s.network_id,s.token_id FOR UPDATE OF s NOWAIT;
    PERFORM id FROM cat_wallets WHERE id=ANY(wallets) ORDER BY id FOR UPDATE NOWAIT;
    GET DIAGNOSTICS found_count=ROW_COUNT;
    SELECT count(DISTINCT value) INTO expected_count FROM unnest(wallets) value;
    IF found_count<>expected_count THEN
        RAISE EXCEPTION 'custody selection wallet is missing' USING ERRCODE='23514';
    END IF;
    RETURN TRUE;
EXCEPTION WHEN lock_not_available THEN
    RETURN FALSE;
END;
$$;

-- A withdrawal also owns its request merchant/network scope when legacy
-- platform wallets or retained fee/funding resources have other owners.
-- Acquire the union in one order, never merchant scope followed by a new owner.
CREATE FUNCTION fn_crypto_lock_withdrawal_wallets(wallets UUID[],scope_merchant UUID,scope_network UUID) RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
    IF scope_merchant IS NULL OR scope_network IS NULL THEN
        RAISE EXCEPTION 'withdrawal resources require their request scope' USING ERRCODE='23514';
    END IF;
    PERFORM s.version FROM sys_crypto_aml_routing_scopes s
        WHERE EXISTS(SELECT 1 FROM cat_wallets w WHERE w.id=ANY(wallets)
            AND (w.custody_merchant_id,w.network_id)=(s.merchant_id,s.network_id))
            OR (s.merchant_id=scope_merchant AND s.network_id=scope_network)
        ORDER BY s.merchant_id,s.network_id,s.token_id FOR UPDATE OF s;
    PERFORM fn_crypto_lock_custody_wallets(wallets);
END;
$$;

-- Queue triggers also lock custody scopes on lease-only UPDATEs. A selector
-- must acquire the whole footprint before its funding row, and release all
-- newly acquired locks if any resource (including the funding row) is busy.
CREATE FUNCTION fn_crypto_try_lock_funding_confirmation(funding UUID) RETURNS BOOLEAN LANGUAGE plpgsql AS $$
DECLARE wallets UUID[]; locked UUID;
BEGIN
    SELECT ARRAY(
        SELECT source_wallet_id FROM doc_crypto_native_fundings WHERE id=funding
        UNION SELECT destination_wallet_id FROM doc_crypto_native_fundings WHERE id=funding
        UNION SELECT fee.payer_wallet_id FROM doc_crypto_native_fundings f
            JOIN reg_crypto_fee_reservations fee ON fee.id=f.fee_reservation_id WHERE f.id=funding
    ) INTO wallets;
    IF cardinality(wallets)=0 THEN RETURN FALSE; END IF;
    IF NOT fn_crypto_try_lock_custody_wallets(wallets) THEN RETURN FALSE; END IF;
    SELECT id INTO locked FROM doc_crypto_native_fundings WHERE id=funding FOR UPDATE NOWAIT;
    RETURN locked IS NOT NULL;
EXCEPTION WHEN lock_not_available THEN
    RETURN FALSE;
END;
$$;

-- Identity belongs to the immutable producer fact; leases/results are an
-- operational projection. Never repair an old conflicting target implicitly.
CREATE FUNCTION fn_crypto_reconciliation_request_identity_valid(network UUID,wallet UUID,token UUID,source_kind TEXT,source UUID) RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM cat_wallets w JOIN cat_tokens t ON t.network_id=w.network_id
     WHERE w.id=wallet AND t.id=token AND w.network_id=network)
 AND CASE source_kind
     WHEN 'native_funding_source' THEN EXISTS(SELECT 1 FROM doc_crypto_native_fundings f
         WHERE f.id=source AND f.network_id=network AND f.source_wallet_id=wallet AND f.native_token_id=token)
     WHEN 'native_funding_destination' THEN EXISTS(SELECT 1 FROM doc_crypto_native_fundings f
         WHERE f.id=source AND f.network_id=network AND f.destination_wallet_id=wallet AND f.native_token_id=token)
     WHEN 'custody_adjustment' THEN EXISTS(SELECT 1 FROM doc_crypto_custody_adjustments d
         WHERE d.id=source AND d.network_id=network AND d.wallet_id=wallet AND d.token_id=token)
     ELSE TRUE END;
$$;

CREATE FUNCTION fn_crypto_reconciliation_request_identity_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'reconciliation request identity cannot be deleted' USING ERRCODE='23514'; END IF;
 IF TG_OP='UPDATE' THEN
     IF (NEW.id,NEW.network_id,NEW.wallet_id,NEW.token_id,NEW.source_type,NEW.source_id,NEW.requested_at)
         IS DISTINCT FROM (OLD.id,OLD.network_id,OLD.wallet_id,OLD.token_id,OLD.source_type,OLD.source_id,OLD.requested_at) THEN
         RAISE EXCEPTION 'reconciliation request identity is immutable' USING ERRCODE='23514'; END IF;
 ELSE
     IF NOT fn_crypto_reconciliation_request_identity_valid(NEW.network_id,NEW.wallet_id,NEW.token_id,NEW.source_type,NEW.source_id) THEN
         RAISE EXCEPTION 'reconciliation request does not match its source or asset network' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NEW;
END;
$$;

CREATE FUNCTION fn_crypto_activation_identity_valid(task UUID,wallet UUID,network UUID,operation UUID) RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM cat_wallets w WHERE w.id=wallet AND w.network_id=network)
 AND (operation IS NULL OR EXISTS(SELECT 1 FROM doc_crypto_network_operations o WHERE o.id=operation AND o.network_id=network AND o.target_wallet_id=wallet AND o.basis_type='CryptoAccountActivationTask' AND o.basis_id=task));
$$;

CREATE FUNCTION fn_crypto_activation_identity_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'activation task identity cannot be deleted' USING ERRCODE='23514'; END IF;
 IF TG_OP='UPDATE' AND ((NEW.id,NEW.wallet_id,NEW.network_id,NEW.created_at) IS DISTINCT FROM (OLD.id,OLD.wallet_id,OLD.network_id,OLD.created_at)
 OR (OLD.network_operation_id IS NOT NULL AND NEW.network_operation_id IS DISTINCT FROM OLD.network_operation_id)) THEN
  RAISE EXCEPTION 'activation task identity is immutable' USING ERRCODE='23514';
 END IF;
 IF NOT fn_crypto_activation_identity_valid(NEW.id,NEW.wallet_id,NEW.network_id,NEW.network_operation_id) THEN
  RAISE EXCEPTION 'activation task identity does not match its wallet or operation' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END; $$;

CREATE FUNCTION fn_crypto_activation_dispatch_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'activation dispatch facts are immutable' USING ERRCODE='23514'; END IF;
 IF NOT EXISTS(SELECT 1 FROM reg_crypto_account_activations a JOIN reg_crypto_signing_authorizations s ON s.basis_id=a.id AND s.basis_type='CryptoAccountActivationTask' AND s.purpose='activation'
 JOIN cat_wallets target ON target.id=a.wallet_id AND s.destination_address=target.address
 JOIN cat_wallets payer ON payer.network_id=a.network_id AND payer.address=s.source_address AND payer.address=s.fee_payer_address AND payer.vault_key_id=s.vault_key_id
 JOIN reg_crypto_fee_reservations f ON f.basis_type=s.basis_type AND f.basis_id=s.basis_id AND f.purpose=s.purpose AND f.attempt=s.attempt AND f.network_id=a.network_id
 AND f.payer_wallet_id=payer.id AND f.unsigned_fingerprint=s.unsigned_fingerprint AND f.max_fee_minor=s.max_network_fee_minor
 JOIN cat_tokens native ON native.id=f.native_token_id AND native.network_id=a.network_id AND native.token_standard='native' AND native.id=f.operation_token_id
 WHERE a.id=NEW.activation_id AND a.activation_claim_token=NEW.claim_token AND a.activation_lease_expires_at>clock_timestamp()
 AND a.activation_claimed_by<>'' AND a.status IN ('claimed','held','signing','broadcast','confirmed')
 AND s.authorization_id=NEW.authorization_id AND s.network_id=a.network_id AND s.network_id=NEW.network_id
 AND s.unsigned_fingerprint=NEW.unsigned_fingerprint AND s.attempt=1 AND s.generation=1 AND s.amount_minor_units=0 AND s.asset_standard='native' AND s.contract_address=''
 AND (NEW.phase<>'broadcast' OR (s.signed_envelope IS NOT NULL AND s.tx_hash<>'' AND s.signed_envelope->>'txHash'=s.tx_hash))) THEN
  RAISE EXCEPTION 'activation dispatch requires its current claim and exact authorization' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END; $$;

CREATE FUNCTION fn_crypto_reorg_compensation_immutable() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.request_basis IS NULL OR NEW.request_basis->'schemaVersion' IS DISTINCT FROM '1'::jsonb THEN
   RAISE EXCEPTION 'new financial reorg compensation requires request basis version 1' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 RAISE EXCEPTION 'financial reorg compensation facts are immutable' USING ERRCODE='23514';
END; $$;

-- Match the compact JSON tuple used by the previous Go owner gate exactly.
CREATE FUNCTION fn_crypto_hold_owner_key(kind TEXT,owner TEXT) RETURNS TEXT LANGUAGE sql IMMUTABLE STRICT AS $$
 SELECT '["operational-hold",'||to_json(kind)::text||','||to_json(owner)::text||']'
$$;

CREATE FUNCTION fn_crypto_try_lock_operational_hold_owner(kind TEXT,owner TEXT) RETURNS BOOLEAN LANGUAGE plpgsql AS $$
BEGIN
 IF current_setting('transaction_isolation')<>'read committed' THEN
  RAISE EXCEPTION 'operational hold commands require read committed isolation' USING ERRCODE='22023';
 END IF;
 IF kind IS NULL OR owner IS NULL OR btrim(kind)='' OR btrim(owner)='' THEN
  RAISE EXCEPTION 'operational hold owner is required' USING ERRCODE='22023';
 END IF;
 RETURN pg_try_advisory_xact_lock(hashtextextended(fn_crypto_hold_owner_key(kind,owner),0));
END; $$;

CREATE FUNCTION fn_crypto_sql_hold_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target UUID; column_name TEXT; count_rows BIGINT; exact BOOLEAN;
BEGIN
 IF NOT fn_crypto_try_lock_operational_hold_owner(CASE WHEN TG_OP='INSERT' THEN NEW.owner_type ELSE OLD.owner_type END,
      CASE WHEN TG_OP='INSERT' THEN NEW.owner_id ELSE OLD.owner_id END) THEN
  RAISE EXCEPTION 'operational hold owner is busy; retry the command' USING ERRCODE='55P03';
 END IF;
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'operational hold history is immutable' USING ERRCODE='23514'; END IF;
 column_name:=CASE TG_TABLE_NAME WHEN 'reg_crypto_network_holds' THEN 'network_id' WHEN 'reg_crypto_wallet_allocation_holds' THEN 'wallet_id' END;
 IF column_name IS NULL THEN RAISE EXCEPTION 'unsupported operational hold table'; END IF;
 target:=(to_jsonb(NEW)->>column_name)::uuid;
 IF TG_OP='INSERT' THEN
  IF target IS NULL OR NEW.reason IS NULL OR btrim(NEW.reason)='' OR NEW.held_by IS NULL OR btrim(NEW.held_by)=''
    OR NEW.details IS NULL OR jsonb_typeof(NEW.details)<>'object' THEN
   RAISE EXCEPTION 'operational hold target, reason, actor and object evidence are required' USING ERRCODE='23514';
  END IF;
  IF NEW.status IS DISTINCT FROM 'open' OR NEW.resolved_at IS NOT NULL OR NEW.resolved_by IS DISTINCT FROM '' OR NEW.resolution IS DISTINCT FROM '' THEN
   RAISE EXCEPTION 'new operational hold must start open' USING ERRCODE='23514';
  END IF;
  EXECUTE format('SELECT count(*),COALESCE(bool_and(reason=$4 AND held_by=$5 AND details=$6),TRUE) FROM %I WHERE %I=$1 AND owner_type=$2 AND owner_id=$3',TG_TABLE_NAME,column_name)
   INTO count_rows,exact USING target,NEW.owner_type,NEW.owner_id,NEW.reason,NEW.held_by,NEW.details;
  IF count_rows>0 THEN
   IF NOT exact THEN RAISE EXCEPTION 'operational hold replay differs from its original acquisition' USING ERRCODE='23514'; END IF;
   RETURN NULL;
  END IF;
  IF EXISTS(SELECT 1 FROM reg_crypto_network_holds WHERE owner_type=NEW.owner_type AND owner_id=NEW.owner_id AND status='resolved')
    OR EXISTS(SELECT 1 FROM reg_crypto_wallet_allocation_holds WHERE owner_type=NEW.owner_type AND owner_id=NEW.owner_id AND status='resolved') THEN
   RAISE EXCEPTION 'resolved operational hold owner cannot acquire another target' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF (to_jsonb(NEW)-ARRAY['status','resolved_at','resolved_by','resolution']) IS DISTINCT FROM
    (to_jsonb(OLD)-ARRAY['status','resolved_at','resolved_by','resolution']) THEN
  RAISE EXCEPTION 'operational hold acquisition identity is immutable' USING ERRCODE='23514';
 END IF;
 IF OLD.status='resolved' THEN
  IF NEW.status='resolved' AND (NEW.resolved_by,NEW.resolution) IS NOT DISTINCT FROM (OLD.resolved_by,OLD.resolution) THEN RETURN NULL; END IF;
  RAISE EXCEPTION 'operational hold resolution replay differs from its original actor or reason' USING ERRCODE='23514';
 END IF;
 IF NEW.status='open' THEN
  IF (NEW.resolved_at,NEW.resolved_by,NEW.resolution) IS DISTINCT FROM (OLD.resolved_at,OLD.resolved_by,OLD.resolution) THEN
   RAISE EXCEPTION 'open operational hold cannot change resolution audit' USING ERRCODE='23514';
  END IF;
  RETURN NULL;
 END IF;
 IF NEW.status IS DISTINCT FROM 'resolved' OR NEW.resolved_by IS NULL OR NEW.resolution IS NULL OR btrim(NEW.resolved_by)='' OR btrim(NEW.resolution)='' THEN
  RAISE EXCEPTION 'operational hold resolver and reason are required' USING ERRCODE='23514';
 END IF;
 IF EXISTS(SELECT 1 FROM reg_crypto_network_holds WHERE owner_type=OLD.owner_type AND owner_id=OLD.owner_id AND status='resolved'
    AND (resolved_by,resolution) IS DISTINCT FROM (NEW.resolved_by,NEW.resolution))
 OR EXISTS(SELECT 1 FROM reg_crypto_wallet_allocation_holds WHERE owner_type=OLD.owner_type AND owner_id=OLD.owner_id AND status='resolved'
    AND (resolved_by,resolution) IS DISTINCT FROM (NEW.resolved_by,NEW.resolution)) THEN
  RAISE EXCEPTION 'operational hold resolution replay differs from its original actor or reason' USING ERRCODE='23514';
 END IF;
 NEW.resolved_at:=clock_timestamp();
 RETURN NEW;
END; $$;

-- Pool observations update their own source records. An open hold keeps its
-- initial source version; a later high-risk finding gets a distinct lifecycle.
CREATE FUNCTION fn_crypto_hold_aml_pool_wallet(wallet UUID,token UUID,kind TEXT,source_version BIGINT) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE previous reg_crypto_wallet_allocation_holds; owner TEXT; payload JSONB; reason_code TEXT; version_key TEXT;
BEGIN
 IF kind NOT IN ('aml_pool_scope_baseline','aml_pool_risk') OR source_version<=0 THEN RAISE EXCEPTION 'invalid AML hold source' USING ERRCODE='22023'; END IF;
 IF NOT EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_provisions v JOIN reg_crypto_aml_pool_intents i ON i.id=v.id
 JOIN sys_crypto_aml_pool_policies p ON p.id=i.policy_id JOIN cat_wallets w ON w.id=v.wallet_id
 WHERE v.wallet_id=wallet AND v.status='completed' AND p.wallet_role='merchant_pool' AND w.wallet_role='merchant_pool') THEN
  RAISE EXCEPTION 'AML hold requires its provisioned deposit wallet' USING ERRCODE='23514';
 END IF;
 IF kind='aml_pool_scope_baseline' THEN
  IF NOT EXISTS(SELECT 1 FROM cat_wallets w JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=w.custody_merchant_id AND s.network_id=w.network_id
    WHERE w.id=wallet AND s.token_id=token AND s.enabled AND s.version=source_version) THEN RAISE EXCEPTION 'AML hold scope source changed' USING ERRCODE='23514'; END IF;
  owner:=wallet::text||':'||token::text; reason_code:='aml_scope_baseline_missing'; version_key:='scopeVersion';
 ELSE
  IF NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_risk_states WHERE wallet_id=wallet AND token_id=token AND risk_class='high' AND revision=source_version) THEN
   RAISE EXCEPTION 'AML hold risk source changed' USING ERRCODE='23514'; END IF;
  owner:=wallet::text||':'||token::text||':'||source_version::text; reason_code:='aml_pool_high_risk'; version_key:='riskRevision';
 END IF;
 SELECT * INTO previous FROM reg_crypto_wallet_allocation_holds h WHERE h.wallet_id=wallet AND h.owner_type=kind
  AND (h.owner_id=token::text OR h.owner_id=owner OR (kind='aml_pool_risk' AND h.owner_id LIKE wallet::text||':'||token::text||':%'))
  AND (h.status='open' OR kind='aml_pool_scope_baseline' OR h.owner_id=owner OR h.details->>version_key=source_version::text)
  ORDER BY (h.status='open') DESC,h.held_at,h.id LIMIT 1;
 IF FOUND THEN
  IF previous.details->>'tokenId' IS DISTINCT FROM token::text OR COALESCE(previous.details->>version_key,'')!~'^[1-9][0-9]*$'
    OR (previous.details->>version_key)::numeric>source_version THEN RAISE EXCEPTION 'AML hold original source is inconsistent' USING ERRCODE='23514'; END IF;
  owner:=previous.owner_id; payload:=previous.details;
 ELSE payload:=jsonb_build_object('tokenId',token::text,version_key,source_version);
 END IF;
 INSERT INTO reg_crypto_wallet_allocation_holds(wallet_id,reason,owner_type,owner_id,details,held_by)
 VALUES(wallet,reason_code,kind,owner,payload,'aml-pool-controller');
END; $$;

CREATE OR REPLACE FUNCTION fn_crypto_aml_pool_scope_hold() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE wallet UUID;
BEGIN
 IF NEW.enabled THEN
  FOR wallet IN SELECT v.wallet_id FROM reg_crypto_aml_pool_intents i JOIN sys_crypto_aml_pool_policies p ON p.id=i.policy_id AND p.wallet_role='merchant_pool'
   JOIN reg_crypto_aml_wallet_provisions v ON v.id=i.id AND v.status='completed'
   WHERE p.merchant_id=NEW.merchant_id AND p.network_id=NEW.network_id
   AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_baselines WHERE wallet_id=v.wallet_id AND token_id=NEW.token_id) ORDER BY v.wallet_id LOOP
   PERFORM fn_crypto_hold_aml_pool_wallet(wallet,NEW.token_id,'aml_pool_scope_baseline',NEW.version);
  END LOOP;
 END IF;
 RETURN NEW;
END; $$;

CREATE OR REPLACE FUNCTION fn_crypto_aml_pool_high_hold() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.risk_class='high' AND EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_provisions v JOIN reg_crypto_aml_pool_intents i ON i.id=v.id
  JOIN sys_crypto_aml_pool_policies p ON p.id=i.policy_id AND p.wallet_role='merchant_pool' WHERE v.wallet_id=NEW.wallet_id AND v.status='completed') THEN
  PERFORM fn_crypto_hold_aml_pool_wallet(NEW.wallet_id,NEW.token_id,'aml_pool_risk',NEW.revision);
 END IF;
 RETURN NEW;
END; $$;

CREATE FUNCTION fn_crypto_hold_aml_preflight(wallet UUID,token UUID,observation UUID) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE previous reg_crypto_wallet_allocation_holds; owner TEXT; payload JSONB;
BEGIN
 IF NOT EXISTS(SELECT 1 FROM reg_crypto_aml_preflight_observations WHERE id=observation AND wallet_id=wallet AND token_id=token) THEN
  RAISE EXCEPTION 'preflight hold requires its original observation' USING ERRCODE='23514';
 END IF;
 owner:=wallet::text||':'||token::text||':'||observation::text;
 SELECT * INTO previous FROM reg_crypto_wallet_allocation_holds h WHERE h.wallet_id=wallet AND h.owner_type='aml_preflight'
  AND (h.owner_id=token::text OR h.owner_id LIKE wallet::text||':'||token::text||':%')
  AND (h.status='open' OR h.details->>'observationId'=observation::text) ORDER BY (h.status='open') DESC,h.held_at,h.id LIMIT 1;
 IF FOUND THEN
  IF NOT EXISTS(SELECT 1 FROM reg_crypto_aml_preflight_observations o WHERE o.id::text=previous.details->>'observationId' AND o.wallet_id=wallet AND o.token_id=token) THEN
   RAISE EXCEPTION 'preflight hold original observation is inconsistent' USING ERRCODE='23514';
  END IF;
  owner:=previous.owner_id; payload:=previous.details;
 ELSE payload:=jsonb_build_object('observationId',observation::text);
 END IF;
 INSERT INTO reg_crypto_wallet_allocation_holds(wallet_id,reason,owner_type,owner_id,details,held_by)
 VALUES(wallet,'aml_preflight_inventory_detected','aml_preflight',owner,payload,'aml-preflight');
END; $$;

-- An intent has one immutable cancellation command, including its first actor.
-- Compare before ON CONFLICT can hide a different command or skip constraints.
CREATE OR REPLACE FUNCTION fn_crypto_aml_pool_cancel_guard() RETURNS TRIGGER AS $$
DECLARE
 network UUID;
 prior reg_crypto_aml_pool_intent_cancellations%ROWTYPE;
BEGIN
 IF current_setting('transaction_isolation')<>'read committed' THEN
  RAISE EXCEPTION 'pool cancellation requires read committed isolation' USING ERRCODE='23514';
 END IF;
 IF NEW.intent_id IS NULL OR NEW.reason IS NULL OR length(btrim(NEW.reason)) NOT BETWEEN 3 AND 500
    OR NEW.cancelled_by IS NULL OR btrim(NEW.cancelled_by)='' OR NEW._deleted_at IS NOT NULL THEN
  RAISE EXCEPTION 'pool cancellation requires intent, reason and actor' USING ERRCODE='23514';
 END IF;
 SELECT p.network_id INTO network FROM reg_crypto_aml_pool_intents i JOIN sys_crypto_aml_pool_policies p ON p.id=i.policy_id WHERE i.id=NEW.intent_id;
 IF NOT FOUND THEN RAISE EXCEPTION 'pool cancellation intent is missing' USING ERRCODE='23503'; END IF;
 PERFORM 1 FROM sys_crypto_custody_policies WHERE network_id=network FOR UPDATE;
 PERFORM 1 FROM sys_crypto_aml_routing_scopes WHERE network_id=network ORDER BY merchant_id,network_id,token_id FOR UPDATE;
 PERFORM 1 FROM reg_crypto_aml_pool_intents WHERE id=NEW.intent_id FOR UPDATE;
 SELECT * INTO prior FROM reg_crypto_aml_pool_intent_cancellations WHERE intent_id=NEW.intent_id;
 IF FOUND THEN
  IF prior.reason IS DISTINCT FROM NEW.reason OR prior.cancelled_by IS DISTINCT FROM NEW.cancelled_by THEN
   RAISE EXCEPTION 'pool cancellation replay differs from its original command' USING ERRCODE='23514';
  END IF;
  RETURN NULL;
 END IF;
 IF EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_provisions WHERE id=NEW.intent_id) THEN
  RAISE EXCEPTION 'cannot cancel pool intent after derivation reservation' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Coverage is a first observation, retained across later address refreshes and
-- watcher leases. Check authority after any lock wait using the database clock.
CREATE OR REPLACE FUNCTION fn_crypto_aml_watch_coverage_guard() RETURNS TRIGGER AS $$
BEGIN
 PERFORM 1 FROM sys_chain_watcher_state state JOIN cat_wallets w ON w.id=NEW.wallet_id AND w.network_id=state.network_id
 JOIN cat_tokens t ON t.id=NEW.token_id AND t.network_id=w.network_id JOIN cat_blockchain_networks n ON n.id=w.network_id AND n.chain_family='evm'
 WHERE state.network_id=NEW.network_id AND state.watcher_key=NEW.watcher_key
 AND NEW.watcher_key=CASE WHEN t.token_standard='native' THEN 'native_eth' ELSE 'erc20:'||LOWER(t.contract_address) END FOR SHARE OF state;
 IF NOT FOUND THEN RAISE EXCEPTION 'address coverage watcher or asset changed' USING ERRCODE='23514'; END IF;
 IF NOT EXISTS(SELECT 1 FROM sys_chain_watcher_state WHERE network_id=NEW.network_id AND watcher_key=NEW.watcher_key
   AND lease_owner=NEW.lease_owner AND fencing_token=NEW.fencing_token AND lease_expires_at>clock_timestamp()) THEN
  RAISE EXCEPTION 'address coverage requires the current watcher lease' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_decision_replay_guard() RETURNS TRIGGER AS $$
DECLARE prior reg_crypto_aml_decisions%ROWTYPE;
BEGIN
 IF current_setting('transaction_isolation')<>'read committed' THEN
  RAISE EXCEPTION 'AML decisions require read committed isolation' USING ERRCODE='23514';
 END IF;
 IF NEW.id IS NULL OR NEW.subject_id IS NULL OR NEW.report_id IS NULL OR NEW.generation IS NULL OR NEW.generation<=0
    OR NEW.policy_version IS NULL OR btrim(NEW.policy_version)='' OR NEW.risk_class IS NULL OR NEW.risk_class NOT IN ('low','medium','high','unknown')
    OR NEW.reason IS NULL OR btrim(NEW.reason)='' OR NEW._deleted_at IS NOT NULL THEN
  RAISE EXCEPTION 'AML decision requires its complete assessment' USING ERRCODE='23514';
 END IF;
 PERFORM 1 FROM reg_crypto_aml_reports r JOIN reg_crypto_aml_subjects s ON s.id=r.subject_id
 JOIN sys_crypto_aml_routing_scopes scope ON scope.merchant_id=s.merchant_id AND scope.network_id=s.network_id AND scope.token_id=s.token_id
 WHERE r.id=NEW.report_id AND s.id=NEW.subject_id FOR UPDATE OF scope NOWAIT;
 IF NOT EXISTS(SELECT 1 FROM reg_crypto_aml_reports r JOIN reg_crypto_aml_provider_checks c ON c.id=r.provider_check_id
   JOIN reg_crypto_aml_jobs j ON j.id=c.job_id JOIN sys_crypto_aml_risk_policies p ON p.version=NEW.policy_version
   WHERE r.id=NEW.report_id AND r.subject_id=NEW.subject_id AND j.subject_id=NEW.subject_id AND j.generation=NEW.generation) THEN
  RAISE EXCEPTION 'AML decision report, generation or policy changed' USING ERRCODE='23514';
 END IF;
 SELECT * INTO prior FROM reg_crypto_aml_decisions WHERE subject_id=NEW.subject_id AND generation=NEW.generation AND report_id=NEW.report_id AND policy_version=NEW.policy_version;
 IF FOUND THEN
  IF (prior.risk_class,prior.reason,prior.expires_at,prior.evaluated_at) IS DISTINCT FROM (NEW.risk_class,NEW.reason,NEW.expires_at,NEW.evaluated_at) THEN
   RAISE EXCEPTION 'AML decision replay differs from its original assessment' USING ERRCODE='23514';
  END IF;
  RETURN NULL;
 END IF;
 IF NEW.evaluated_at IS NULL OR NOT isfinite(NEW.evaluated_at) THEN
  RAISE EXCEPTION 'new AML decision requires an assessment clock' USING ERRCODE='23514';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM reg_crypto_aml_subjects s JOIN sys_crypto_aml_routing_scopes scope
   ON scope.merchant_id=s.merchant_id AND scope.network_id=s.network_id AND scope.token_id=s.token_id
   WHERE s.id=NEW.subject_id AND scope.enabled AND scope.policy_version=NEW.policy_version) THEN
  RAISE EXCEPTION 'AML decision routing policy changed' USING ERRCODE='23514';
 END IF;
 IF (NEW.risk_class='unknown' AND NEW.expires_at IS NOT NULL) OR (NEW.risk_class<>'unknown' AND NOT EXISTS(
   SELECT 1 FROM sys_crypto_aml_risk_policies p JOIN reg_crypto_aml_reports r ON r.id=NEW.report_id
   WHERE p.version=NEW.policy_version AND NEW.expires_at IS NOT NULL AND isfinite(NEW.expires_at)
   AND NEW.expires_at>NEW.evaluated_at AND NEW.expires_at<=NEW.evaluated_at+make_interval(secs=>p.decision_ttl_seconds)
   AND NEW.expires_at<=r.completed_at+make_interval(secs=>p.max_report_age_seconds))) THEN
  RAISE EXCEPTION 'AML decision expiry differs from its assessment bounds' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- A trigger already owns its target row. Never wait for the parent invoice
-- from there: this same nonblocking gate is used by the application first.
CREATE FUNCTION fn_crypto_lock_invoice_drift(invoice UUID) RETURNS UUID AS $$
DECLARE network UUID;
BEGIN
 IF current_setting('transaction_isolation')<>'read committed' THEN
  RAISE EXCEPTION 'invoice drift requires read committed isolation' USING ERRCODE='23514';
 END IF;
 SELECT t.network_id INTO network FROM doc_crypto_invoices i JOIN cat_tokens t ON t.id=i.token_id WHERE i.id=invoice FOR UPDATE OF i NOWAIT;
 IF NOT FOUND THEN RAISE EXCEPTION 'invoice drift invoice is missing' USING ERRCODE='23503'; END IF;
 RETURN network;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_invoice_drift_details(invoice UUID) RETURNS JSONB AS $$
 SELECT jsonb_build_object('episodeId',episode_id,'firstSeenAt',first_seen_at,'firstDetails',initial_details,
   'consecutiveCycles',consecutive_cycles,'reason',details->>'error')
 FROM sys_invoice_reconciliation_drift WHERE invoice_id=invoice
$$ LANGUAGE sql STABLE;

CREATE FUNCTION fn_crypto_invoice_drift_guard() RETURNS TRIGGER AS $$
DECLARE network UUID; at TIMESTAMPTZ := clock_timestamp();
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'invoice drift history cannot be deleted' USING ERRCODE='23514'; END IF;
 network := fn_crypto_lock_invoice_drift(NEW.invoice_id);
 IF TG_OP='INSERT' THEN
  IF NEW.episode_id IS NULL OR NEW.initial_details IS NULL OR NEW.initial_details IS DISTINCT FROM NEW.details
     OR NEW.consecutive_cycles IS DISTINCT FROM 1 OR NEW.resolved_at IS NOT NULL OR NEW.incident_id IS NOT NULL THEN
   RAISE EXCEPTION 'new invoice drift requires its first observation' USING ERRCODE='23514';
  END IF;
  NEW.first_seen_at := at; NEW.last_seen_at := at;
  RETURN NEW;
 END IF;
 IF NEW.invoice_id IS DISTINCT FROM OLD.invoice_id THEN RAISE EXCEPTION 'invoice drift identity is immutable' USING ERRCODE='23514'; END IF;
 IF OLD.resolved_at IS NOT NULL THEN
  IF NEW IS NOT DISTINCT FROM OLD THEN RETURN NULL; END IF;
  IF NEW.resolved_at IS NOT NULL OR NEW.episode_id IS NULL OR NEW.episode_id IS NOT DISTINCT FROM OLD.episode_id
     OR NEW.initial_details IS NULL OR NEW.initial_details IS DISTINCT FROM NEW.details OR NEW.incident_id IS NOT NULL OR NEW.consecutive_cycles IS DISTINCT FROM 1 THEN
   RAISE EXCEPTION 'resolved invoice drift requires a new episode' USING ERRCODE='23514';
  END IF;
  NEW.first_seen_at := at; NEW.last_seen_at := at;
  RETURN NEW;
 END IF;
 IF (NEW.episode_id,NEW.initial_details,NEW.first_seen_at) IS DISTINCT FROM (OLD.episode_id,OLD.initial_details,OLD.first_seen_at)
    OR (OLD.incident_id IS NOT NULL AND NEW.incident_id IS DISTINCT FROM OLD.incident_id) THEN
  RAISE EXCEPTION 'invoice drift first observation is immutable' USING ERRCODE='23514';
 END IF;
 IF NEW.resolved_at IS NOT NULL THEN
  IF (to_jsonb(NEW)-'resolved_at') IS DISTINCT FROM (to_jsonb(OLD)-'resolved_at') THEN
   RAISE EXCEPTION 'invoice drift resolution cannot change its observation' USING ERRCODE='23514';
  END IF;
  NEW.resolved_at := at;
 ELSE
  IF NEW.consecutive_cycles=OLD.consecutive_cycles+1 THEN
   NEW.last_seen_at := at;
  ELSIF (to_jsonb(NEW)-'incident_id') IS DISTINCT FROM (to_jsonb(OLD)-'incident_id') THEN
   RAISE EXCEPTION 'invoice drift update requires another observation' USING ERRCODE='23514';
  END IF;
 END IF;
 IF NEW.incident_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM reg_crypto_security_incidents incident
   WHERE incident.id=NEW.incident_id AND incident.network_id=network AND incident.invoice_id=NEW.invoice_id AND incident.payment_id IS NULL
   AND incident.incident_kind='invoice_projection_drift' AND incident.event_kind='native_value' AND incident.event_index=0
   AND incident.tx_hash=NEW.invoice_id::text||CASE WHEN NEW.episode_id IS NULL THEN '' ELSE ':'||NEW.episode_id::text END
   AND ((NEW.resolved_at IS NULL AND incident.status IN ('open','acknowledged')) OR (NEW.resolved_at IS NOT NULL AND incident.status='resolved'))) THEN
  RAISE EXCEPTION 'invoice drift incident link changed' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_invoice_drift_incident_guard() RETURNS TRIGGER AS $$
DECLARE network UUID; prior reg_crypto_security_incidents%ROWTYPE;
BEGIN
 IF TG_OP='DELETE' THEN
  IF OLD.incident_kind='invoice_projection_drift' THEN RAISE EXCEPTION 'invoice drift incident cannot be deleted' USING ERRCODE='23514'; END IF;
  RETURN OLD;
 END IF;
 IF NEW.incident_kind<>'invoice_projection_drift' AND (TG_OP='INSERT' OR OLD.incident_kind<>'invoice_projection_drift') THEN RETURN NEW; END IF;
 network := fn_crypto_lock_invoice_drift(NEW.invoice_id);
 IF NEW.id IS NULL OR NEW.tx_hash IS NULL OR NEW.network_id IS DISTINCT FROM network OR NEW.payment_id IS NOT NULL OR NEW.event_kind IS DISTINCT FROM 'native_value'
    OR NEW.event_index IS DISTINCT FROM 0 OR NEW.incident_kind IS DISTINCT FROM 'invoice_projection_drift' OR NEW.details IS NULL OR jsonb_typeof(NEW.details)<>'object' THEN
  RAISE EXCEPTION 'invoice drift incident identity changed' USING ERRCODE='23514';
 END IF;
 IF TG_OP='INSERT' THEN
  SELECT * INTO prior FROM reg_crypto_security_incidents WHERE network_id=NEW.network_id AND incident_kind=NEW.incident_kind
    AND tx_hash=NEW.tx_hash AND event_kind=NEW.event_kind AND event_index=NEW.event_index;
  IF FOUND THEN
   IF (prior.invoice_id,prior.payment_id,prior.details) IS DISTINCT FROM (NEW.invoice_id,NEW.payment_id,NEW.details) THEN
    RAISE EXCEPTION 'invoice drift incident replay differs from its first observation' USING ERRCODE='23514';
   END IF;
   RETURN NULL;
  END IF;
  IF NEW.status IS DISTINCT FROM 'open' OR NEW.acknowledged_at IS NOT NULL OR NEW.resolved_at IS NOT NULL OR NOT EXISTS(
    SELECT 1 FROM sys_invoice_reconciliation_drift d WHERE d.invoice_id=NEW.invoice_id AND d.resolved_at IS NULL AND d.consecutive_cycles>=2
    AND NEW.tx_hash=d.invoice_id::text||CASE WHEN d.episode_id IS NULL THEN '' ELSE ':'||d.episode_id::text END
    AND NEW.details=fn_crypto_invoice_drift_details(d.invoice_id)) THEN
   RAISE EXCEPTION 'invoice drift incident requires its current observation' USING ERRCODE='23514';
  END IF;
  NEW.created_at := clock_timestamp();
  RETURN NEW;
 END IF;
 IF (to_jsonb(NEW)-ARRAY['status','acknowledged_at','resolved_at']) IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['status','acknowledged_at','resolved_at']) THEN
  RAISE EXCEPTION 'invoice drift incident first observation is immutable' USING ERRCODE='23514';
 END IF;
 IF OLD.status='resolved' THEN
  IF NEW.status<>'resolved' THEN RAISE EXCEPTION 'resolved invoice drift incident cannot reopen' USING ERRCODE='23514'; END IF;
  RETURN NULL;
 END IF;
 IF NEW.status=OLD.status THEN RETURN NULL; END IF;
 IF NEW.status='resolved' THEN
  NEW.acknowledged_at := OLD.acknowledged_at; NEW.resolved_at := clock_timestamp();
 ELSIF OLD.status='open' AND NEW.status='acknowledged' THEN
  NEW.acknowledged_at := clock_timestamp(); NEW.resolved_at := NULL;
 ELSE RAISE EXCEPTION 'invoice drift incident transition is invalid' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_preflight_basis(job UUID) RETURNS JSONB LANGUAGE sql STABLE AS $$
 SELECT jsonb_build_object('jobId',j.id,'walletId',j.wallet_id,'merchantId',p.merchant_id,'networkId',p.network_id,'tokenId',j.token_id,
 'policyId',p.id,'policyVersion',j.policy_version,'scopeVersion',j.scope_version,'address',w.address,
 'asset',jsonb_strip_nulls(jsonb_build_object('tokenId',t.id,'code',NULLIF(t.code,''),'name',NULLIF(t.name,''),'symbol',NULLIF(t.symbol,''),
 'standard',t.token_standard,'contractAddress',NULLIF(t.contract_address,''),'decimals',t.decimal_places)))
 FROM reg_crypto_aml_preflight_jobs j JOIN sys_crypto_aml_pool_policies p ON p.id=j.policy_id JOIN cat_wallets w ON w.id=j.wallet_id
 JOIN cat_tokens t ON t.id=j.token_id WHERE j.id=job
$$;

CREATE FUNCTION fn_crypto_aml_preflight_job_guard() RETURNS TRIGGER AS $$
DECLARE prior reg_crypto_aml_preflight_jobs%ROWTYPE;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'preflight job identity cannot be deleted' USING ERRCODE='23514'; END IF;
 IF TG_OP='UPDATE' THEN
  IF (NEW.id,NEW.wallet_id,NEW.token_id,NEW.policy_id,NEW.created_at,NEW.initial_policy_version,NEW.initial_scope_version)
   IS DISTINCT FROM (OLD.id,OLD.wallet_id,OLD.token_id,OLD.policy_id,OLD.created_at,OLD.initial_policy_version,OLD.initial_scope_version) THEN
   RAISE EXCEPTION 'preflight job identity is immutable' USING ERRCODE='23514';
  END IF;
  IF NEW.status='processing' AND NEW.lease_token IS DISTINCT FROM OLD.lease_token
     AND EXISTS(SELECT 1 FROM reg_crypto_aml_preflight_attempts WHERE lease_token=NEW.lease_token) THEN
   RAISE EXCEPTION 'preflight attempt token cannot be reused' USING ERRCODE='23514';
  END IF;
  IF OLD.status='processing' AND NEW.status='processing' THEN
   IF NEW.lease_token IS NOT DISTINCT FROM OLD.lease_token THEN
    IF (NEW.policy_version,NEW.scope_version,NEW.worker_id,NEW.lease_expires_at,NEW.attempts)
      IS DISTINCT FROM (OLD.policy_version,OLD.scope_version,OLD.worker_id,OLD.lease_expires_at,OLD.attempts) THEN
     RAISE EXCEPTION 'preflight active attempt basis is immutable' USING ERRCODE='23514';
    END IF;
   ELSIF OLD.lease_expires_at>clock_timestamp() THEN
    RAISE EXCEPTION 'preflight attempt is still leased' USING ERRCODE='23514';
   END IF;
  END IF;
  RETURN NEW;
 END IF;
 IF NEW.id IS NULL OR NEW.wallet_id IS NULL OR NEW.token_id IS NULL OR NEW.policy_id IS NULL OR NEW.policy_version IS NULL OR NEW.policy_version<=0 OR NEW.scope_version IS NULL OR NEW.scope_version<=0 THEN
  RAISE EXCEPTION 'preflight job requires its complete identity' USING ERRCODE='23514';
 END IF;
 SELECT * INTO prior FROM reg_crypto_aml_preflight_jobs WHERE wallet_id=NEW.wallet_id AND token_id=NEW.token_id;
 IF FOUND THEN
  IF prior.policy_id IS DISTINCT FROM NEW.policy_id THEN RAISE EXCEPTION 'preflight queue belongs to another policy' USING ERRCODE='23514'; END IF;
  RETURN NULL;
 END IF;
 IF NOT fn_crypto_try_lock_custody_wallets(ARRAY[NEW.wallet_id]) THEN
  RAISE EXCEPTION 'preflight wallet is busy' USING ERRCODE='55P03';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM cat_wallets w JOIN cat_tokens t ON t.id=NEW.token_id AND t.network_id=w.network_id
   JOIN sys_crypto_aml_pool_policies p ON p.id=NEW.policy_id AND p.network_id=w.network_id AND p.merchant_id=w.custody_merchant_id AND p.wallet_role=w.wallet_role
   WHERE w.id=NEW.wallet_id) OR EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_provisions v JOIN reg_crypto_aml_pool_intents i ON i.id=v.id
   WHERE v.wallet_id=NEW.wallet_id AND i.policy_id<>NEW.policy_id) THEN
  RAISE EXCEPTION 'preflight job source identity changed' USING ERRCODE='23514';
 END IF;
 NEW.initial_policy_version:=NEW.policy_version; NEW.initial_scope_version:=NEW.scope_version;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_preflight_attempt_guard() RETURNS TRIGGER AS $$
DECLARE wallet UUID;
BEGIN
 IF current_setting('transaction_isolation')<>'read committed' THEN RAISE EXCEPTION 'preflight attempts require read committed isolation' USING ERRCODE='23514'; END IF;
 SELECT wallet_id INTO wallet FROM reg_crypto_aml_preflight_jobs WHERE id=NEW.job_id;
 IF wallet IS NULL OR NOT fn_crypto_try_lock_custody_wallets(ARRAY[wallet]) THEN RAISE EXCEPTION 'preflight wallet is missing or busy' USING ERRCODE='55P03'; END IF;
 PERFORM id FROM reg_crypto_aml_preflight_jobs WHERE id=NEW.job_id FOR UPDATE NOWAIT;
 PERFORM p.id FROM sys_crypto_aml_pool_policies p JOIN reg_crypto_aml_preflight_jobs j ON j.policy_id=p.id WHERE j.id=NEW.job_id FOR SHARE OF p NOWAIT;
 IF NOT EXISTS(SELECT 1 FROM reg_crypto_aml_preflight_jobs j JOIN sys_crypto_aml_pool_policies p ON p.id=j.policy_id AND p.enabled AND p.automatic_baseline AND p.version=j.policy_version
   JOIN cat_wallets w ON w.id=j.wallet_id AND w.network_id=p.network_id AND w.custody_merchant_id=p.merchant_id AND w.wallet_role=p.wallet_role AND w.is_active AND NOT w.deletion_mark
   JOIN cat_tokens t ON t.id=j.token_id AND t.network_id=w.network_id AND t.is_active AND NOT t.deletion_mark
   JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=p.merchant_id AND s.network_id=p.network_id AND s.token_id=t.id AND s.enabled AND s.version=j.scope_version
   JOIN reg_crypto_aml_wallet_provisions v ON v.wallet_id=w.id AND v.status='completed' JOIN reg_crypto_aml_pool_intents i ON i.id=v.id AND i.policy_id=p.id
   WHERE j.id=NEW.job_id AND j.status='processing' AND j.lease_token=NEW.lease_token AND j.worker_id=NEW.worker_id
   AND j.lease_expires_at=NEW.lease_expires_at AND NEW.lease_expires_at>clock_timestamp() AND NEW.lease_expires_at<=clock_timestamp()+interval '1 minute'
   AND NEW.basis=fn_crypto_aml_preflight_basis(j.id)
   AND (p.token_id IS NULL OR p.token_id=t.id OR t.token_standard='native') AND (p.wallet_role<>'network_fee' OR t.token_standard='native')) THEN
  RAISE EXCEPTION 'preflight attempt requires its current source and lease' USING ERRCODE='23514';
 END IF;
 NEW.claimed_at:=clock_timestamp();
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_preflight_claim_committed() RETURNS TRIGGER AS $$
BEGIN
 IF NEW.status='processing' AND (TG_OP='INSERT' OR OLD.lease_token IS DISTINCT FROM NEW.lease_token)
   AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_preflight_attempts WHERE job_id=NEW.id AND lease_token=NEW.lease_token) THEN
  RAISE EXCEPTION 'preflight claim requires immutable attempt evidence' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_preflight_attempt_active(job UUID,lease UUID,current_policy BOOLEAN) RETURNS BOOLEAN AS $$
DECLARE wallet UUID; active BOOLEAN;
BEGIN
 IF current_setting('transaction_isolation')<>'read committed' THEN RAISE EXCEPTION 'preflight attempts require read committed isolation' USING ERRCODE='23514'; END IF;
 SELECT (basis->>'walletId')::uuid INTO wallet FROM reg_crypto_aml_preflight_attempts WHERE job_id=job AND lease_token=lease;
 IF wallet IS NULL THEN RETURN FALSE; END IF;
 IF NOT fn_crypto_try_lock_custody_wallets(ARRAY[wallet]) THEN RAISE EXCEPTION 'preflight wallet is busy' USING ERRCODE='55P03'; END IF;
 PERFORM id FROM reg_crypto_aml_preflight_jobs WHERE id=job FOR UPDATE NOWAIT;
 IF current_policy THEN
  PERFORM p.id FROM sys_crypto_aml_pool_policies p JOIN reg_crypto_aml_preflight_jobs j ON j.policy_id=p.id WHERE j.id=job FOR SHARE OF p NOWAIT;
 END IF;
 SELECT EXISTS(SELECT 1 FROM reg_crypto_aml_preflight_jobs j JOIN reg_crypto_aml_preflight_attempts a ON a.job_id=j.id AND a.lease_token=lease
   JOIN sys_crypto_aml_pool_policies p ON p.id=j.policy_id
   JOIN cat_wallets w ON w.id=j.wallet_id AND w.network_id=p.network_id AND w.custody_merchant_id=p.merchant_id AND w.wallet_role=p.wallet_role
   JOIN cat_tokens t ON t.id=j.token_id AND t.network_id=w.network_id
   WHERE j.id=job AND j.status='processing' AND j.lease_token=lease AND j.lease_expires_at=a.lease_expires_at AND j.lease_expires_at>clock_timestamp()
   AND j.worker_id=a.worker_id AND j.policy_id::text=a.basis->>'policyId'
   AND j.policy_version=(a.basis->>'policyVersion')::bigint AND j.scope_version=(a.basis->>'scopeVersion')::bigint
   AND j.wallet_id::text=a.basis->>'walletId' AND j.token_id::text=a.basis->>'tokenId' AND w.address=a.basis->>'address'
   AND (NOT current_policy OR (p.enabled AND p.automatic_baseline AND p.version=j.policy_version AND w.is_active AND NOT w.deletion_mark AND t.is_active AND NOT t.deletion_mark
     AND EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes s WHERE s.merchant_id=p.merchant_id AND s.network_id=p.network_id AND s.token_id=t.id AND s.enabled AND s.version=j.scope_version)))) INTO active;
 RETURN active;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_preflight_attempt_observation_guard() RETURNS TRIGGER AS $$
BEGIN
 IF NOT fn_crypto_aml_preflight_attempt_active(NEW.job_id,NEW.lease_token,TRUE) OR NOT EXISTS(
   SELECT 1 FROM reg_crypto_aml_preflight_attempts a WHERE a.job_id=NEW.job_id AND a.lease_token=NEW.lease_token
   AND NEW.wallet_id::text=a.basis->>'walletId' AND NEW.token_id::text=a.basis->>'tokenId' AND NEW.network_id::text=a.basis->>'networkId'
   AND NEW.proof->'asset'=a.basis->'asset' AND NEW.proof->>'address'=a.basis->>'address'
   AND (NEW.proof->>'observedAt')::timestamptz BETWEEN clock_timestamp()-interval '1 minute' AND clock_timestamp()+interval '5 seconds'
   AND NEW.observed_at BETWEEN clock_timestamp()-interval '1 minute' AND clock_timestamp()) THEN
  RAISE EXCEPTION 'preflight observation requires its recorded live attempt' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_try_lock_watch_network(network UUID) RETURNS BOOLEAN LANGUAGE plpgsql VOLATILE AS $$
BEGIN
 IF current_setting('transaction_isolation')<>'read committed' THEN
  RAISE EXCEPTION 'watch registration requires read committed isolation' USING ERRCODE='23514';
 END IF;
 RETURN network IS NOT NULL AND pg_try_advisory_xact_lock(hashtextextended('crypto-watch-network:'||network::text,0));
END;
$$;

CREATE FUNCTION fn_crypto_ingestion_basis_guard() RETURNS TRIGGER AS $$
DECLARE prior sys_crypto_block_ingestion%ROWTYPE; original JSONB;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'ingestion registration cannot be deleted' USING ERRCODE='23514'; END IF;
 IF NEW.network_id IS NULL OR NEW.start_height IS NULL OR NEW.start_height<0 OR NEW.start_height=9223372036854775807
    OR NEW.start_hash IS NULL OR NEW.start_hash='' OR NEW.start_timestamp IS NULL OR NEW.decoder_version IS DISTINCT FROM 2 THEN
  RAISE EXCEPTION 'ingestion registration requires its complete boundary' USING ERRCODE='23514';
 END IF;
 IF NEW.mode IN ('solidified','hybrid') AND NEW.activated_at IS NULL THEN
  RAISE EXCEPTION 'financial ingestion requires its activation time' USING ERRCODE='23514';
 END IF;
 IF NOT fn_crypto_try_lock_watch_network(NEW.network_id) THEN RAISE EXCEPTION 'ingestion registration is busy' USING ERRCODE='55P03'; END IF;
 IF TG_OP='INSERT' THEN
  SELECT * INTO prior FROM sys_crypto_block_ingestion WHERE network_id=NEW.network_id;
  IF FOUND THEN
   original:=COALESCE(prior.registration_basis,jsonb_build_object('height',prior.start_height,'hash',prior.start_hash,'timestamp',prior.start_timestamp,'mode',prior.mode,'decoderVersion',prior.decoder_version));
   IF (NEW.start_height::text,LOWER(NEW.start_hash),NEW.start_timestamp::text,NEW.mode,NEW.decoder_version::text)
     IS DISTINCT FROM (original->>'height',LOWER(original->>'hash'),original->>'timestamp',original->>'mode',original->>'decoderVersion') THEN
    RAISE EXCEPTION 'ingestion registration replay differs from its first boundary' USING ERRCODE='23514';
   END IF;
   RETURN NULL;
  END IF;
  IF NEW._deleted_at IS NOT NULL THEN RAISE EXCEPTION 'ingestion registration cannot start deleted' USING ERRCODE='23514'; END IF;
  NEW.registration_basis:=jsonb_build_object('height',NEW.start_height,'hash',NEW.start_hash,'timestamp',NEW.start_timestamp,'mode',NEW.mode,'decoderVersion',NEW.decoder_version,'registeredAt',clock_timestamp());
  NEW.activation_basis:=NULL;
  IF NEW.activated_at IS NOT NULL THEN NEW.activated_at:=clock_timestamp(); END IF;
 ELSE
  IF (NEW.network_id,NEW.created_at,NEW._deleted_at,NEW.registration_basis,NEW.decoder_version)
   IS DISTINCT FROM (OLD.network_id,OLD.created_at,OLD._deleted_at,OLD.registration_basis,OLD.decoder_version) THEN
   RAISE EXCEPTION 'ingestion registration identity is immutable' USING ERRCODE='23514';
  END IF;
  IF OLD.activated_at IS NOT NULL THEN
   IF (NEW.start_height,NEW.start_hash,NEW.start_timestamp,NEW.activated_at,NEW.activation_basis)
     IS DISTINCT FROM (OLD.start_height,OLD.start_hash,OLD.start_timestamp,OLD.activated_at,OLD.activation_basis) THEN
    RAISE EXCEPTION 'activated ingestion boundary is immutable' USING ERRCODE='23514';
   END IF;
  ELSIF NEW.activated_at IS NULL THEN
   IF (NEW.start_height,NEW.start_hash,NEW.start_timestamp,NEW.activation_basis) IS DISTINCT FROM (OLD.start_height,OLD.start_hash,OLD.start_timestamp,OLD.activation_basis) THEN
    RAISE EXCEPTION 'shadow registration boundary is immutable' USING ERRCODE='23514';
   END IF;
  ELSE
   IF NEW.start_height<OLD.start_height OR (NEW.start_height=OLD.start_height AND (LOWER(NEW.start_hash) IS DISTINCT FROM LOWER(OLD.start_hash) OR NEW.start_timestamp IS DISTINCT FROM OLD.start_timestamp)) THEN
    RAISE EXCEPTION 'financial ingestion boundary conflicts with its registration' USING ERRCODE='23514';
   END IF;
   NEW.activated_at:=clock_timestamp();
  END IF;
 END IF;
 IF NEW.activated_at IS NOT NULL AND NEW.mode='shadow' THEN RAISE EXCEPTION 'activated ingestion cannot downgrade to shadow' USING ERRCODE='23514'; END IF;
 IF NEW.activated_at IS NOT NULL AND (TG_OP='INSERT' OR OLD.activated_at IS NULL) THEN
  IF NEW.mode NOT IN ('solidified','hybrid') THEN RAISE EXCEPTION 'financial activation requires a financial mode' USING ERRCODE='23514'; END IF;
  NEW.activation_basis:=jsonb_build_object('height',NEW.start_height,'hash',NEW.start_hash,'timestamp',NEW.start_timestamp,'mode',NEW.mode,'decoderVersion',NEW.decoder_version,'activatedAt',NEW.activated_at);
 END IF;
 IF TG_OP='UPDATE' AND (to_jsonb(NEW)-ARRAY['updated_at','_txid']) IS NOT DISTINCT FROM (to_jsonb(OLD)-ARRAY['updated_at','_txid']) THEN RETURN NULL; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_watch_target_guard() RETURNS TRIGGER AS $$
DECLARE prior sys_crypto_watch_targets%ROWTYPE;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'watch target first registration is immutable' USING ERRCODE='23514'; END IF;
 IF NEW.network_id IS NULL OR NEW.owner_id IS NULL OR NEW.target_kind IS NULL OR NEW.target_kind NOT IN ('address','asset')
   OR NEW.target_key IS NULL OR NEW.target_key='' OR NEW.start_height IS NULL OR NEW.start_height<0 OR NEW.source IS NULL OR NEW.source='' OR NEW._deleted_at IS NOT NULL THEN
  RAISE EXCEPTION 'watch target requires its complete owner and boundary' USING ERRCODE='23514';
 END IF;
 IF NOT fn_crypto_try_lock_watch_network(NEW.network_id) THEN RAISE EXCEPTION 'watch registration is busy' USING ERRCODE='55P03'; END IF;
 IF (NEW.target_kind='address' AND NOT EXISTS(SELECT 1 FROM cat_wallets w WHERE w.id=NEW.owner_id AND w.network_id=NEW.network_id AND w.address=NEW.target_key))
   OR (NEW.target_kind='asset' AND NOT EXISTS(SELECT 1 FROM cat_tokens t WHERE t.id=NEW.owner_id AND t.network_id=NEW.network_id AND t.contract_address=NEW.target_key)) THEN
  RAISE EXCEPTION 'watch target does not match its catalog owner' USING ERRCODE='23514';
 END IF;
 SELECT * INTO prior FROM sys_crypto_watch_targets WHERE network_id=NEW.network_id AND target_kind=NEW.target_kind AND target_key=NEW.target_key;
 IF FOUND THEN
  IF prior.owner_id IS DISTINCT FROM NEW.owner_id THEN RAISE EXCEPTION 'watch target belongs to its first owner' USING ERRCODE='23514'; END IF;
  -- A later scan boundary/source is a refresh hint, not a new registration.
  RETURN NULL;
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_watched_catalog_guard() RETURNS TRIGGER AS $$
DECLARE network UUID; changed BOOLEAN;
BEGIN
 network:=OLD.network_id;
 IF TG_OP='DELETE' THEN changed:=TRUE;
 ELSIF TG_TABLE_NAME='cat_wallets' THEN changed:=(NEW.id,NEW.network_id,NEW.address) IS DISTINCT FROM (OLD.id,OLD.network_id,OLD.address);
 ELSE changed:=(NEW.id,NEW.network_id,NEW.contract_address,NEW.token_standard,NEW.decimal_places) IS DISTINCT FROM (OLD.id,OLD.network_id,OLD.contract_address,OLD.token_standard,OLD.decimal_places);
 END IF;
 IF NOT changed THEN RETURN NEW; END IF;
 IF network IS NULL THEN
  IF TG_OP='DELETE' THEN RETURN OLD; END IF;
  RETURN NEW;
 END IF;
 IF NOT fn_crypto_try_lock_watch_network(network) THEN RAISE EXCEPTION 'watch registration is busy' USING ERRCODE='55P03'; END IF;
 IF EXISTS(SELECT 1 FROM sys_crypto_watch_targets WHERE network_id=network AND owner_id=OLD.id AND target_kind=CASE WHEN TG_TABLE_NAME='cat_wallets' THEN 'address' ELSE 'asset' END) THEN
  RAISE EXCEPTION 'watched catalog identity cannot be deleted or changed' USING ERRCODE='23514';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
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

-- Same transaction identity gate as the expected-effect matcher. Never wait
-- for wallets/scopes from a trigger that may already own an outflow row.
CREATE FUNCTION fn_crypto_try_lock_external_outflow(network UUID,hash TEXT,wallet UUID) RETURNS BOOLEAN LANGUAGE plpgsql AS $$
BEGIN
 IF current_setting('transaction_isolation')<>'read committed' THEN
  RAISE EXCEPTION 'external outflow requires read committed isolation' USING ERRCODE='23514';
 END IF;
 IF network IS NULL OR hash IS NULL OR btrim(hash)='' THEN RAISE EXCEPTION 'external outflow identity is required' USING ERRCODE='23514'; END IF;
 IF NOT pg_try_advisory_xact_lock(hashtextextended(network::text||':'||hash,0)) THEN RETURN FALSE; END IF;
 IF wallet IS NOT NULL THEN RETURN fn_crypto_try_lock_custody_wallets(ARRAY[wallet]); END IF;
 RETURN TRUE;
END; $$;

CREATE FUNCTION fn_crypto_external_outflow_basis_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE prior reg_crypto_external_outflows%ROWTYPE; at TIMESTAMPTZ:=clock_timestamp(); family TEXT;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'external outflow history is immutable' USING ERRCODE='23514'; END IF;
 IF NOT fn_crypto_try_lock_external_outflow(NEW.network_id,NEW.tx_hash,NEW.source_wallet_id) THEN
  RAISE EXCEPTION 'external outflow resources are busy; retry the command' USING ERRCODE='55P03';
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.source_wallet_id IS NULL OR NEW.amount IS NULL OR NEW.amount<=0 OR NEW.block_number<0 OR NEW.confirmations<0 OR NEW.required_confs<0
    OR NEW.status IS DISTINCT FROM 'pending_review' OR NEW.classification IS DISTINCT FROM '' THEN
   RAISE EXCEPTION 'external outflow requires its original effect and observation' USING ERRCODE='23514';
  END IF;
  SELECT * INTO prior FROM reg_crypto_external_outflows WHERE network_id=NEW.network_id AND tx_hash=NEW.tx_hash AND event_kind=NEW.event_kind AND event_index=NEW.event_index;
  IF FOUND THEN
   IF (NEW.token_id,NEW.source_wallet_id,NEW.source_address_key,NEW.destination_address_key,NEW.contract_address_key,NEW.amount) IS DISTINCT FROM
      (prior.token_id,prior.source_wallet_id,prior.source_address_key,prior.destination_address_key,prior.contract_address_key,prior.amount) THEN
    RAISE EXCEPTION 'external outflow replay differs from its first effect' USING ERRCODE='23514';
   END IF;
   RETURN NULL;
  END IF;
  SELECT chain_family INTO STRICT family FROM cat_blockchain_networks WHERE id=NEW.network_id;
  IF NEW.source_address_key IS DISTINCT FROM (CASE WHEN family='evm' THEN lower(NEW.from_address) ELSE NEW.from_address END)
    OR NEW.destination_address_key IS DISTINCT FROM (CASE WHEN family='evm' THEN lower(NEW.to_address) ELSE NEW.to_address END)
    OR NEW.contract_address_key IS DISTINCT FROM (CASE WHEN family='evm' THEN lower(NEW.contract_address) ELSE NEW.contract_address END)
    OR NOT EXISTS(SELECT 1 FROM cat_wallets w WHERE w.id=NEW.source_wallet_id AND w.network_id=NEW.network_id
      AND w.address_key=NEW.source_address_key AND NOT w.deletion_mark)
    OR (NEW.token_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM cat_tokens t WHERE t.id=NEW.token_id AND t.network_id=NEW.network_id
      AND CASE WHEN family='evm' THEN lower(COALESCE(t.contract_address,'')) ELSE COALESCE(t.contract_address,'') END=NEW.contract_address_key AND NOT t.deletion_mark)) THEN
   RAISE EXCEPTION 'external outflow source does not match its catalog identity' USING ERRCODE='23514';
  END IF;
  NEW.detected_at:=at; NEW.created_at:=at; NEW.last_observed_at:=at;
  NEW.initial_basis:=jsonb_build_object('networkId',NEW.network_id,'walletId',NEW.source_wallet_id,'tokenId',NEW.token_id,
   'txHash',NEW.tx_hash,'eventKind',NEW.event_kind,'eventIndex',NEW.event_index,'sourceKey',NEW.source_address_key,
   'destinationKey',NEW.destination_address_key,'contractKey',NEW.contract_address_key,'amount',NEW.amount::text,
   'fromAddress',NEW.from_address,'toAddress',NEW.to_address,'contractAddress',NEW.contract_address,
   'blockNumber',NEW.block_number,'blockHash',NEW.block_hash,'confirmations',NEW.confirmations,'requiredConfs',NEW.required_confs,
   'finalityStatus',NEW.finality_status,'detectedAt',at);
  RETURN NEW;
 END IF;
 IF (NEW.id,NEW.network_id,NEW.token_id,NEW.source_wallet_id,NEW.tx_hash,NEW.event_kind,NEW.event_index,NEW.from_address,NEW.to_address,NEW.contract_address,
     NEW.source_address_key,NEW.destination_address_key,NEW.contract_address_key,NEW.amount,NEW.detected_at,NEW.created_at,NEW.initial_basis)
    IS DISTINCT FROM
    (OLD.id,OLD.network_id,OLD.token_id,OLD.source_wallet_id,OLD.tx_hash,OLD.event_kind,OLD.event_index,OLD.from_address,OLD.to_address,OLD.contract_address,
     OLD.source_address_key,OLD.destination_address_key,OLD.contract_address_key,OLD.amount,OLD.detected_at,OLD.created_at,OLD.initial_basis) THEN
  RAISE EXCEPTION 'external outflow first effect and observation are immutable' USING ERRCODE='23514';
 END IF;
 IF NEW.required_confs<OLD.required_confs OR NEW.block_number<OLD.block_number OR (OLD.block_hash<>'' AND NEW.block_hash='')
   OR (NEW.block_number<>OLD.block_number AND OLD.block_hash<>'' AND lower(NEW.block_hash)=lower(OLD.block_hash)) THEN
  RAISE EXCEPTION 'external outflow block evidence or confirmation floor is inconsistent' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END; $$;

CREATE FUNCTION fn_crypto_external_outflow_observation_source() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE source reg_crypto_external_outflows%ROWTYPE;
BEGIN
 SELECT * INTO STRICT source FROM reg_crypto_external_outflows WHERE id=NEW.external_outflow_id;
 IF NOT fn_crypto_try_lock_external_outflow(source.network_id,source.tx_hash,source.source_wallet_id) THEN
  RAISE EXCEPTION 'external outflow resources are busy; retry the command' USING ERRCODE='55P03';
 END IF;
 IF NEW.incident_id IS NULL OR NOT EXISTS(SELECT 1 FROM reg_crypto_reconciliation_items i JOIN reg_crypto_reconciliation_runs r ON r.id=NEW.run_id
   WHERE i.id=NEW.incident_id AND i.kind='external_outflow' AND r.network_id=source.network_id
    AND i.network_id=source.network_id AND i.wallet_id=source.source_wallet_id AND i.token_id IS NOT DISTINCT FROM source.token_id
    AND i.tx_hash=source.tx_hash AND i.event_kind=source.event_kind AND i.event_index=source.event_index
    AND i.chain_amount=source.amount AND i.ledger_amount=0 AND i.delta_amount=-source.amount) THEN
  RAISE EXCEPTION 'external outflow observation requires its source incident and run' USING ERRCODE='23514';
 END IF;
 NEW.observed_at:=clock_timestamp();
 RETURN NEW;
END; $$;

CREATE FUNCTION fn_crypto_outflow_decision_insert_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='reg_crypto_external_outflows' THEN
  IF NEW.classification_basis IS NOT NULL OR NEW.resolution_basis IS NOT NULL OR NEW.classified_by IS NOT NULL OR NEW.classified_at IS NOT NULL
    OR NEW.resolved_by IS NOT NULL OR NEW.resolved_at IS NOT NULL THEN
   RAISE EXCEPTION 'new outflow cannot start with a decision' USING ERRCODE='23514';
  END IF;
 ELSE
  IF NEW.decision_basis IS NOT NULL OR NEW.decision_by IS NOT NULL OR NEW.decided_at IS NOT NULL OR NEW.decision_reason<>'' OR NEW.posted_version<>0 THEN
   RAISE EXCEPTION 'new custody adjustment cannot start with a decision' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END; $$;

CREATE FUNCTION fn_crypto_outflow_decision_command(kind TEXT,target UUID,actor UUID,reason TEXT,classification TEXT DEFAULT NULL,basis_kind TEXT DEFAULT NULL,basis UUID DEFAULT NULL,evidence JSONB DEFAULT NULL)
RETURNS JSONB LANGUAGE sql IMMUTABLE AS $$
 SELECT jsonb_build_object('kind',kind,'targetId',target,'actorId',actor,'reason',reason) ||
 CASE WHEN kind='classify' THEN jsonb_build_object('classification',classification,'basisType',basis_kind,'basisId',basis,'evidence',COALESCE(evidence,'{}'::jsonb)) ELSE '{}'::jsonb END
$$;

CREATE FUNCTION fn_crypto_outflow_decision_seed(basis JSONB) RETURNS UUID LANGUAGE plpgsql AS $$
DECLARE value UUID;
BEGIN
 IF basis IS NULL OR jsonb_typeof(basis)<>'object' OR (basis-'id')<>'{}'::jsonb OR basis->>'id' IS NULL THEN
  RAISE EXCEPTION 'new outflow decision requires a generated decision ID' USING ERRCODE='23514';
 END IF;
 value:=(basis->>'id')::uuid;
 IF value='00000000-0000-0000-0000-000000000000'::uuid THEN RAISE EXCEPTION 'outflow decision ID is required' USING ERRCODE='23514'; END IF;
 RETURN value;
END; $$;

CREATE FUNCTION fn_crypto_outflow_decision_source(target UUID) RETURNS JSONB LANGUAGE sql STABLE AS $$
 SELECT jsonb_build_object('outflowId',id,'networkId',network_id,'walletId',source_wallet_id,'tokenId',token_id,
  'txHash',tx_hash,'eventKind',event_kind,'eventIndex',event_index,'sourceKey',source_address_key,'destinationKey',destination_address_key,
  'contractKey',contract_address_key,'amount',amount::text,'blockNumber',block_number,'blockHash',block_hash,'confirmations',confirmations,
  'requiredConfs',required_confs,'finality',finality_status,'previousStatus',status,'initialBasis',initial_basis,
  'previousClassificationId',classification_basis->>'id') FROM reg_crypto_external_outflows WHERE id=target
$$;

CREATE FUNCTION fn_crypto_outflow_decision_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE decision UUID; command JSONB; source JSONB; check_basis JSONB; hold_basis JSONB; incident UUID; at TIMESTAMPTZ:=clock_timestamp();
BEGIN
 -- The common 103 guard obtains the same nonblocking resource prefix first.
 IF (NEW.classification,NEW.matched_basis_type,NEW.matched_basis_id,NEW.classification_reason,NEW.classification_evidence,NEW.classified_by)
  IS NOT DISTINCT FROM (OLD.classification,OLD.matched_basis_type,OLD.matched_basis_id,OLD.classification_reason,OLD.classification_evidence,OLD.classified_by) THEN
  IF NEW.classification_basis IS DISTINCT FROM OLD.classification_basis THEN
   IF OLD.classification_basis IS NULL THEN RAISE EXCEPTION 'legacy classification basis is unknown' USING ERRCODE='23514'; END IF;
   PERFORM fn_crypto_outflow_decision_seed(NEW.classification_basis);
   NEW.classification_basis:=OLD.classification_basis;
  END IF;
  IF NEW.classified_at IS DISTINCT FROM OLD.classified_at THEN
   IF OLD.classification_basis IS NULL THEN RAISE EXCEPTION 'classification clock is immutable' USING ERRCODE='23514'; END IF;
   NEW.classified_at:=OLD.classified_at;
  END IF;
 ELSE
  IF NEW.status IS DISTINCT FROM 'classified' OR NEW.classified_by IS NULL OR btrim(NEW.classification_reason)='' OR jsonb_typeof(NEW.classification_evidence)<>'object'
    OR NOT (OLD.status='pending_review' OR (OLD.status='classified' AND OLD.classification='unauthorized_loss' AND NEW.classification IN ('matched_basis','false_positive_or_reorg')
       AND EXISTS(SELECT 1 FROM doc_crypto_custody_adjustments WHERE external_outflow_id=OLD.id AND status='rejected'))) THEN
   RAISE EXCEPTION 'outflow classification decision cannot be rewritten' USING ERRCODE='23514';
  END IF;
  decision:=fn_crypto_outflow_decision_seed(NEW.classification_basis);
  IF decision::text=OLD.classification_basis->>'id' THEN RAISE EXCEPTION 'classification requires a new decision ID' USING ERRCODE='23514'; END IF;
  command:=fn_crypto_outflow_decision_command('classify',OLD.id,NEW.classified_by,NEW.classification_reason,NEW.classification,NEW.matched_basis_type,NEW.matched_basis_id,NEW.classification_evidence);
  source:=fn_crypto_outflow_decision_source(OLD.id);
  NEW.classified_at:=at;
  NEW.classification_basis:=jsonb_build_object('id',decision,'command',command,'source',source,'decidedAt',at);
 END IF;
 IF OLD.status='resolved' THEN
  IF (NEW.status,NEW.resolved_by,NEW.resolved_at,NEW.resolution,NEW.resolution_basis) IS DISTINCT FROM (OLD.status,OLD.resolved_by,OLD.resolved_at,OLD.resolution,OLD.resolution_basis) THEN
   RAISE EXCEPTION 'outflow resolution decision is immutable' USING ERRCODE='23514';
  END IF;
 ELSIF NEW.status='resolved' THEN
  IF OLD.status<>'classified' OR NEW.resolved_by IS NULL OR NEW.resolved_by=OLD.classified_by OR btrim(NEW.resolution)='' THEN
   RAISE EXCEPTION 'outflow resolution requires its independent decision' USING ERRCODE='23514';
  END IF;
  decision:=fn_crypto_outflow_decision_seed(NEW.resolution_basis);
  IF NOT fn_crypto_try_lock_operational_hold_owner('external_outflow',OLD.id::text) THEN
   RAISE EXCEPTION 'external outflow hold owner is busy; retry the command' USING ERRCODE='55P03';
  END IF;
  SELECT COALESCE(jsonb_agg(payload ORDER BY scope,id),'[]'::jsonb) INTO hold_basis FROM (
   SELECT 'network'::text AS scope,id,jsonb_build_object('scope','network','id',id,'targetId',network_id,'reason',reason,'details',details,'heldBy',held_by,'heldAt',held_at) AS payload
    FROM reg_crypto_network_holds WHERE owner_type='external_outflow' AND owner_id=OLD.id::text
   UNION ALL SELECT 'wallet',id,jsonb_build_object('scope','wallet','id',id,'targetId',wallet_id,'reason',reason,'details',details,'heldBy',held_by,'heldAt',held_at)
    FROM reg_crypto_wallet_allocation_holds WHERE owner_type='external_outflow' AND owner_id=OLD.id::text
  ) acquired;
  SELECT jsonb_build_object('checkId',c.id,'runId',r.id,'completedAt',r.completed_at,'walletId',c.wallet_id,'tokenId',c.token_id,
    'ledgerAmount',c.ledger_amount::text,'chainAmount',c.chain_amount::text,'deltaAmount',c.delta_amount::text)
   INTO check_basis FROM reg_crypto_reconciliation_checks c JOIN reg_crypto_reconciliation_runs r ON r.id=c.run_id
   WHERE c.network_id=OLD.network_id AND r.network_id=OLD.network_id AND c.wallet_id=OLD.source_wallet_id AND c.token_id IS NOT DISTINCT FROM OLD.token_id
    AND r.status='completed' AND c.delta_amount=0 AND r.completed_at>GREATEST(OLD.classified_at,
      COALESCE((SELECT decided_at FROM doc_crypto_custody_adjustments WHERE external_outflow_id=OLD.id AND status='posted'),OLD.classified_at))
    AND NOT EXISTS(SELECT 1 FROM reg_crypto_reconciliation_items i WHERE i.wallet_id=c.wallet_id AND i.token_id IS NOT DISTINCT FROM c.token_id
      AND i.kind IN ('token_balance_mismatch','native_fee_balance_mismatch') AND i.status IN ('open','acknowledged'))
   ORDER BY r.completed_at DESC,c.id LIMIT 1 FOR SHARE OF c,r NOWAIT;
  IF check_basis IS NULL THEN RAISE EXCEPTION 'outflow resolution requires its exact fresh reconciliation' USING ERRCODE='23514'; END IF;
  SELECT id INTO incident FROM reg_crypto_reconciliation_items WHERE network_id=OLD.network_id AND wallet_id=OLD.source_wallet_id AND token_id IS NOT DISTINCT FROM OLD.token_id
   AND kind='external_outflow' AND tx_hash=OLD.tx_hash AND event_kind=OLD.event_kind AND event_index=OLD.event_index AND status IN ('open','acknowledged')
   ORDER BY id LIMIT 1 FOR UPDATE NOWAIT;
  IF incident IS NULL THEN RAISE EXCEPTION 'outflow resolution requires its source incident' USING ERRCODE='23514'; END IF;
  source:=fn_crypto_outflow_decision_source(OLD.id)||jsonb_build_object('reconciliation',check_basis,'incidentId',incident,'holds',hold_basis,
   'adjustmentDecisionId',(SELECT decision_basis->>'id' FROM doc_crypto_custody_adjustments WHERE external_outflow_id=OLD.id));
  NEW.resolved_at:=at;
  NEW.resolution_basis:=jsonb_build_object('id',decision,'command',fn_crypto_outflow_decision_command('resolve',OLD.id,NEW.resolved_by,NEW.resolution),'source',source,'decidedAt',at);
 ELSIF (NEW.resolved_by,NEW.resolved_at,NEW.resolution,NEW.resolution_basis) IS DISTINCT FROM (OLD.resolved_by,OLD.resolved_at,OLD.resolution,OLD.resolution_basis) THEN
  RAISE EXCEPTION 'resolution evidence requires its transition' USING ERRCODE='23514';
 END IF;
 IF (to_jsonb(NEW)-'updated_at') IS NOT DISTINCT FROM (to_jsonb(OLD)-'updated_at') THEN RETURN NULL; END IF;
 RETURN NEW;
END; $$;

CREATE FUNCTION fn_crypto_custody_decision_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE decision UUID; kind TEXT; at TIMESTAMPTZ:=clock_timestamp(); source JSONB;
BEGIN
 IF NOT fn_crypto_try_lock_external_outflow((SELECT network_id FROM reg_crypto_external_outflows WHERE id=OLD.external_outflow_id),
   (SELECT tx_hash FROM reg_crypto_external_outflows WHERE id=OLD.external_outflow_id),OLD.wallet_id) THEN
  RAISE EXCEPTION 'external outflow resources are busy; retry the command' USING ERRCODE='55P03';
 END IF;
 IF OLD.status<>'pending_approval' THEN
  IF (to_jsonb(NEW)-'updated_at') IS NOT DISTINCT FROM (to_jsonb(OLD)-'updated_at') THEN RETURN NULL; END IF;
  RAISE EXCEPTION 'custody adjustment terminal decision is immutable' USING ERRCODE='23514';
 END IF;
 IF NEW.status=OLD.status THEN
  IF (NEW.decision_by,NEW.decision_reason,NEW.decided_at,NEW.decision_basis) IS DISTINCT FROM (OLD.decision_by,OLD.decision_reason,OLD.decided_at,OLD.decision_basis) THEN
   RAISE EXCEPTION 'custody decision requires its transition' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF NEW.status NOT IN ('posted','rejected') OR NEW.decision_by IS NULL OR NEW.decision_by=OLD.maker_id OR btrim(NEW.decision_reason)='' THEN
  RAISE EXCEPTION 'custody decision requires its independent checker and reason' USING ERRCODE='23514';
 END IF;
 decision:=fn_crypto_outflow_decision_seed(NEW.decision_basis);
 kind:=CASE WHEN NEW.status='posted' THEN 'approve' ELSE 'reject' END;
 source:=fn_crypto_outflow_decision_source(OLD.external_outflow_id)||jsonb_build_object('adjustment',jsonb_build_object('id',OLD.id,'walletId',OLD.wallet_id,
  'tokenId',OLD.token_id,'amount',OLD.amount::text,'makerId',OLD.maker_id,'postedVersion',OLD.posted_version));
 NEW.decided_at:=at;
 NEW.decision_basis:=jsonb_build_object('id',decision,'command',fn_crypto_outflow_decision_command(kind,OLD.id,NEW.decision_by,NEW.decision_reason),'source',source,'decidedAt',at);
 RETURN NEW;
END; $$;

CREATE FUNCTION fn_crypto_outflow_decision_audit_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE basis JSONB; evidence JSONB; expected_action TEXT; previous TEXT; current TEXT;
BEGIN
 IF NEW.action NOT IN ('classified','reclassified','resolved','custody_adjustment_posted','custody_adjustment_rejected') THEN
  IF NEW.decision_basis IS NOT NULL THEN RAISE EXCEPTION 'non-decision action cannot carry a decision basis' USING ERRCODE='23514'; END IF;
  RETURN NEW;
 END IF;
 IF NEW.action IN ('classified','reclassified') THEN
  SELECT classification_basis,classification_evidence INTO basis,evidence FROM reg_crypto_external_outflows WHERE id=NEW.external_outflow_id;
  previous:=basis->'source'->>'previousStatus'; current:='classified';
  expected_action:=CASE WHEN previous='pending_review' THEN 'classified' ELSE 'reclassified' END;
 ELSIF NEW.action='resolved' THEN
  SELECT resolution_basis INTO basis FROM reg_crypto_external_outflows WHERE id=NEW.external_outflow_id;
  previous:='classified'; current:='resolved'; evidence:='{}'::jsonb; expected_action:='resolved';
 ELSE
  SELECT a.decision_basis,CASE WHEN a.status='posted' THEN jsonb_build_object('adjustmentId',a.id,'postedVersion',1) ELSE jsonb_build_object('adjustmentId',a.id) END,
   CASE WHEN a.status='posted' THEN 'custody_adjustment_posted' ELSE 'custody_adjustment_rejected' END,a.status
   INTO basis,evidence,expected_action,current FROM doc_crypto_custody_adjustments a
   WHERE a.external_outflow_id=NEW.external_outflow_id AND a.id=(NEW.decision_basis->'command'->>'targetId')::uuid;
  previous:='pending_approval';
 END IF;
 IF basis IS NULL OR NEW.decision_basis IS DISTINCT FROM basis OR NEW.id::text IS DISTINCT FROM basis->>'id' OR NEW.actor_type<>'user'
  OR NEW.actor_id IS DISTINCT FROM basis->'command'->>'actorId' OR NEW.reason IS DISTINCT FROM basis->'command'->>'reason'
  OR (NEW.action,NEW.old_state,NEW.new_state,NEW.evidence) IS DISTINCT FROM (expected_action,previous,current,evidence) THEN
  RAISE EXCEPTION 'outflow decision audit does not match its saved decision' USING ERRCODE='23514';
 END IF;
 NEW.created_at:=(basis->>'decidedAt')::timestamptz;
 RETURN NEW;
END; $$;

CREATE FUNCTION fn_crypto_outflow_append_decision_audit() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE basis JSONB; action TEXT; before_state TEXT; after_state TEXT; payload JSONB; target UUID;
BEGIN
 IF TG_TABLE_NAME='reg_crypto_external_outflows' THEN
  target:=NEW.id;
  IF NEW.classification_basis IS DISTINCT FROM OLD.classification_basis AND NEW.classification_basis IS NOT NULL THEN
   basis:=NEW.classification_basis; action:=CASE WHEN OLD.status='pending_review' THEN 'classified' ELSE 'reclassified' END;
   before_state:=OLD.status; after_state:='classified'; payload:=NEW.classification_evidence;
  ELSIF NEW.resolution_basis IS DISTINCT FROM OLD.resolution_basis AND NEW.resolution_basis IS NOT NULL THEN
   basis:=NEW.resolution_basis; action:='resolved'; before_state:='classified'; after_state:='resolved'; payload:='{}'::jsonb;
  END IF;
 ELSE
  target:=NEW.external_outflow_id;
  IF NEW.decision_basis IS DISTINCT FROM OLD.decision_basis AND NEW.decision_basis IS NOT NULL THEN
   basis:=NEW.decision_basis; action:=CASE WHEN NEW.status='posted' THEN 'custody_adjustment_posted' ELSE 'custody_adjustment_rejected' END;
   before_state:='pending_approval'; after_state:=NEW.status;
   payload:=CASE WHEN NEW.status='posted' THEN jsonb_build_object('adjustmentId',NEW.id,'postedVersion',1) ELSE jsonb_build_object('adjustmentId',NEW.id) END;
  END IF;
 END IF;
 IF basis IS NOT NULL THEN
  INSERT INTO reg_crypto_external_outflow_actions(id,external_outflow_id,actor_type,actor_id,action,old_state,new_state,reason,evidence,decision_basis)
  VALUES((basis->>'id')::uuid,target,'user',basis->'command'->>'actorId',action,before_state,after_state,basis->'command'->>'reason',payload,basis);
 END IF;
 RETURN NULL;
END; $$;

CREATE FUNCTION fn_crypto_outflow_decision_audit_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE bases JSONB[]; basis JSONB; target UUID;
BEGIN
 IF TG_TABLE_NAME='reg_crypto_external_outflows' THEN bases:=ARRAY[NEW.classification_basis,NEW.resolution_basis]; target:=NEW.id;
 ELSE bases:=ARRAY[NEW.decision_basis]; target:=NEW.external_outflow_id; END IF;
 FOREACH basis IN ARRAY bases LOOP
  IF basis IS NOT NULL AND NOT EXISTS(SELECT 1 FROM reg_crypto_external_outflow_actions a WHERE a.id=(basis->>'id')::uuid AND a.decision_basis=basis) THEN
   RAISE EXCEPTION 'outflow decision requires its immutable audit in the same transaction' USING ERRCODE='23514';
  END IF;
  IF basis->'command'->>'kind'='resolve' THEN
   IF NOT EXISTS(SELECT 1 FROM reg_crypto_reconciliation_checks c JOIN reg_crypto_reconciliation_runs r ON r.id=c.run_id
     WHERE c.id=(basis->'source'->'reconciliation'->>'checkId')::uuid AND r.status='completed'
      AND jsonb_build_object('checkId',c.id,'runId',r.id,'completedAt',r.completed_at,'walletId',c.wallet_id,'tokenId',c.token_id,
       'ledgerAmount',c.ledger_amount::text,'chainAmount',c.chain_amount::text,'deltaAmount',c.delta_amount::text)=basis->'source'->'reconciliation') THEN
    RAISE EXCEPTION 'outflow resolution reconciliation basis changed before commit' USING ERRCODE='23514';
   END IF;
   IF NOT EXISTS(SELECT 1 FROM reg_crypto_reconciliation_items i WHERE i.id=(basis->'source'->>'incidentId')::uuid AND i.status='resolved'
      AND i.resolved_by::text=basis->'command'->>'actorId' AND i.resolution_reason=basis->'command'->>'reason') THEN
    RAISE EXCEPTION 'resolved external outflow requires its exact resolved reconciliation incident' USING ERRCODE='23514';
   END IF;
   IF jsonb_array_length(basis->'source'->'holds')=0
    OR EXISTS(SELECT 1 FROM reg_crypto_network_holds WHERE owner_type='external_outflow' AND owner_id=target::text
      AND (status<>'resolved' OR resolved_by IS DISTINCT FROM basis->'command'->>'actorId' OR resolution IS DISTINCT FROM basis->'command'->>'reason'))
    OR EXISTS(SELECT 1 FROM reg_crypto_wallet_allocation_holds WHERE owner_type='external_outflow' AND owner_id=target::text
      AND (status<>'resolved' OR resolved_by IS DISTINCT FROM basis->'command'->>'actorId' OR resolution IS DISTINCT FROM basis->'command'->>'reason')) THEN
    RAISE EXCEPTION 'outflow resolution requires its complete original hold cohort' USING ERRCODE='23514';
   END IF;
  END IF;
 END LOOP;
 RETURN NULL;
END; $$;



-- ---- Tables, columns, constraints and indexes ----

CREATE TABLE reg_crypto_accounting_effects (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    schema_version INT NOT NULL DEFAULT 1 CHECK(schema_version=1),
    family TEXT NOT NULL CHECK(family IN ('tron','evm')),
    network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    tx_hash TEXT NOT NULL CHECK(tx_hash ~ '^[0-9a-f]{64}$'),
    event_kind TEXT NOT NULL CHECK(event_kind IN ('native_value','contract_log','internal_value')),
    effect_ordinal BIGINT NOT NULL CHECK(effect_ordinal>=0),
    trace_hash TEXT NOT NULL DEFAULT '',
    logical_key TEXT GENERATED ALWAYS AS (
        'accounting-effect.v1:' || encode(digest(
            fn_crypto_accounting_identity_bytes(family,network_id,tx_hash,event_kind,effect_ordinal,trace_hash),
            'sha256'),'hex')
    ) STORED UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK((event_kind='internal_value' AND family='tron' AND trace_hash ~ '^[0-9a-f]{64}$')
       OR (event_kind<>'internal_value' AND trace_hash='')),
    CHECK(NOT(family='evm' AND event_kind='native_value') OR effect_ordinal=0),
    UNIQUE(family,network_id,tx_hash,event_kind,effect_ordinal,trace_hash)
);

CREATE TABLE reg_crypto_accounting_effect_occurrences (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    effect_id UUID NOT NULL REFERENCES reg_crypto_accounting_effects(id),
    block_number BIGINT NOT NULL CHECK(block_number>=0),
    block_hash TEXT NOT NULL CHECK(block_hash ~ '^[0-9a-f]{64}$'),
    economics_hash TEXT NOT NULL CHECK(economics_hash ~ '^[0-9a-f]{64}$'),
    source_address TEXT NOT NULL CHECK(source_address<>''),
    destination_address TEXT NOT NULL CHECK(destination_address<>''),
    contract_address TEXT NOT NULL,
    amount NUMERIC(78,0) NOT NULL CHECK(amount>0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(effect_id,block_hash)
);

CREATE INDEX idx_crypto_accounting_occurrence_block
    ON reg_crypto_accounting_effect_occurrences(block_number,effect_id);

-- One persisted proof cannot be rebound to another logical effect or occurrence.
-- This is evidence indexing only: it neither admits custody nor marks a
-- canonical generation as financially recognized.
CREATE TABLE reg_crypto_accounting_effect_evidence (
    verification_id UUID PRIMARY KEY REFERENCES reg_crypto_transfer_verifications(id),
    occurrence_id UUID NOT NULL REFERENCES reg_crypto_accounting_effect_occurrences(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_crypto_accounting_evidence_occurrence
    ON reg_crypto_accounting_effect_evidence(occurrence_id,verification_id);

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
        'additional_single_payment','overlapping_windows','dust','treasury_unidentified','matched_receipt')),
    CHECK(basis_id=verification_id),
    CHECK((NOT posted AND posted_version=0) OR (posted AND posted_version=1))
);

CREATE INDEX idx_crypto_custody_receipts_wallet ON doc_crypto_custody_receipts(wallet_id,token_id,date,id);

CREATE INDEX idx_crypto_custody_receipts_owner ON doc_crypto_custody_receipts(custody_merchant_id,date,id);

CREATE INDEX idx_crypto_custody_receipts_txid ON doc_crypto_custody_receipts(_txid);

-- A final unmatched transfer is accounted once as a custody receipt. The inbox
-- row keeps the link and the schedule of independent-proof attempts.
ALTER TABLE reg_crypto_inbound_chain_events
    ADD COLUMN receipt_id UUID UNIQUE REFERENCES doc_crypto_custody_receipts(id),
    ADD COLUMN receipt_check_at TIMESTAMPTZ,
    ADD COLUMN receipt_attempts INT NOT NULL DEFAULT 0 CHECK(receipt_attempts>=0),
    ADD COLUMN receipt_error TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_crypto_inbound_receipt_candidates ON reg_crypto_inbound_chain_events(receipt_check_at NULLS FIRST,created_at,id)
    WHERE receipt_id IS NULL AND match_status IN ('unmatched','needs_review') AND wallet_id IS NOT NULL AND token_id IS NOT NULL AND _deleted_at IS NULL;

-- Operational canonicality audit of accounted receipts. An orphaned receipt is
-- never reversed automatically: it opens an incident, holds the network and can
-- no longer be attributed.
CREATE TABLE reg_crypto_custody_receipt_audits (
    receipt_id UUID PRIMARY KEY REFERENCES doc_crypto_custody_receipts(id),
    status TEXT NOT NULL DEFAULT 'canonical' CHECK(status IN ('canonical','orphaned')),
    next_check_at TIMESTAMPTZ NOT NULL DEFAULT NOW()+INTERVAL '15 minutes',
    last_checked_at TIMESTAMPTZ,
    attempts INT NOT NULL DEFAULT 0 CHECK(attempts>=0),
    last_error TEXT NOT NULL DEFAULT '',
    claimed_by TEXT NOT NULL DEFAULT '',
    lease_expires_at TIMESTAMPTZ,
    orphaned_at TIMESTAMPTZ,
    orphan_verification_id UUID REFERENCES reg_crypto_transfer_verifications(id),
    incident_id UUID REFERENCES reg_crypto_security_incidents(id),
    CHECK((status='orphaned')=(orphaned_at IS NOT NULL AND incident_id IS NOT NULL))
);
CREATE INDEX idx_crypto_custody_receipt_audit_due ON reg_crypto_custody_receipt_audits(next_check_at,receipt_id) WHERE status='canonical';

-- Operator attribution of an unidentified receipt to a merchant. Below the
-- token threshold one operator posts it; above it (or without a policy) a
-- second person approves the prepared credit.
CREATE TABLE sys_crypto_receipt_credit_policies (
    token_id UUID PRIMARY KEY REFERENCES cat_tokens(id),
    dual_approval_threshold NUMERIC(78,0) NOT NULL CHECK(dual_approval_threshold>=0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE doc_crypto_merchant_credits (
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
    basis_type TEXT NOT NULL CHECK(basis_type='CryptoCustodyReceipt'),
    basis_id UUID NOT NULL REFERENCES doc_crypto_custody_receipts(id),
    description TEXT NOT NULL DEFAULT '',
    receipt_id UUID NOT NULL REFERENCES doc_crypto_custody_receipts(id),
    merchant_id UUID NOT NULL REFERENCES cat_merchants(id),
    invoice_id UUID REFERENCES doc_crypto_invoices(id),
    token_id UUID NOT NULL REFERENCES cat_tokens(id),
    amount NUMERIC(78,0) NOT NULL CHECK(amount>0),
    net_amount NUMERIC(78,0) NOT NULL CHECK(net_amount>=0),
    processing_fee NUMERIC(78,0) NOT NULL CHECK(processing_fee>=0),
    fee_fixed NUMERIC(78,0) NOT NULL CHECK(fee_fixed>=0),
    fee_percent_bp INT NOT NULL CHECK(fee_percent_bp BETWEEN 0 AND 10000),
    fee_min NUMERIC(78,0) NOT NULL CHECK(fee_min>=0),
    fee_max NUMERIC(78,0) NOT NULL CHECK(fee_max>=0),
    status TEXT NOT NULL CHECK(status IN ('prepared','posted','rejected')),
    approval_required BOOLEAN NOT NULL,
    prepared_by UUID NOT NULL,
    prepared_at TIMESTAMPTZ NOT NULL,
    decided_by UUID,
    decided_at TIMESTAMPTZ,
    decision_reason TEXT NOT NULL DEFAULT '',
    suspense_line_id UUID NOT NULL UNIQUE,
    merchant_line_id UUID NOT NULL UNIQUE,
    fee_line_id UUID NOT NULL UNIQUE,
    CHECK(basis_id=receipt_id),
    CHECK(amount=net_amount+processing_fee),
    CHECK((status='posted')=posted AND (posted_version IN (0,1)) AND (posted=(posted_version=1))),
    CHECK((status='prepared' AND decided_by IS NULL AND decided_at IS NULL)
       OR (status<>'prepared' AND decided_by IS NOT NULL AND decided_at IS NOT NULL)),
    CHECK(status<>'posted' OR NOT approval_required OR decided_by<>prepared_by),
    CHECK(status<>'rejected' OR btrim(decision_reason)<>'')
);
CREATE UNIQUE INDEX uq_crypto_merchant_credit_open ON doc_crypto_merchant_credits(receipt_id) WHERE status='prepared';
CREATE INDEX idx_crypto_merchant_credits_merchant ON doc_crypto_merchant_credits(merchant_id,date,id);
CREATE TRIGGER trg_crypto_merchant_credit_txid BEFORE UPDATE ON doc_crypto_merchant_credits
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TABLE reg_crypto_receipt_suspense_movements (
    line_id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    recorder_id UUID NOT NULL,
    recorder_type TEXT NOT NULL CHECK(recorder_type IN
        ('CryptoCustodyReceipt','CryptoMerchantCredit','CryptoPayment','CryptoPaymentReorg','CryptoAdjustment')),
    recorder_version INT NOT NULL CHECK(recorder_version>0),
    period TIMESTAMPTZ NOT NULL,
    record_type TEXT NOT NULL CHECK(record_type IN ('receipt','expense')),
    receipt_id UUID NOT NULL REFERENCES doc_crypto_custody_receipts(id),
    token_id UUID NOT NULL REFERENCES cat_tokens(id),
    amount NUMERIC(78,0) NOT NULL CHECK(amount>0),
    reversal_of_line_id UUID REFERENCES reg_crypto_receipt_suspense_movements(line_id),
    reversal_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- A receipt adds its gross once; attributions consume it; only a payment
    -- attribution can be compensated back into the remainder.
    CHECK((recorder_type='CryptoCustodyReceipt' AND record_type='receipt' AND recorder_id=receipt_id
            AND recorder_version=1 AND reversal_of_line_id IS NULL AND reversal_reason IS NULL)
       OR (recorder_type='CryptoMerchantCredit' AND record_type='expense' AND recorder_version=1
            AND reversal_of_line_id IS NULL AND reversal_reason IS NULL)
       OR (recorder_type='CryptoPayment' AND record_type='expense' AND reversal_of_line_id IS NULL AND reversal_reason IS NULL)
       OR (recorder_type IN ('CryptoPaymentReorg','CryptoAdjustment') AND record_type='receipt' AND recorder_version=1
            AND reversal_of_line_id IS NOT NULL AND btrim(COALESCE(reversal_reason,''))<>''))
);

CREATE UNIQUE INDEX uq_crypto_receipt_suspense_origin ON reg_crypto_receipt_suspense_movements(receipt_id)
    WHERE recorder_type='CryptoCustodyReceipt';
CREATE UNIQUE INDEX uq_crypto_receipt_suspense_recorder ON reg_crypto_receipt_suspense_movements(recorder_id)
    WHERE recorder_type='CryptoMerchantCredit';
CREATE UNIQUE INDEX uq_crypto_receipt_suspense_payment ON reg_crypto_receipt_suspense_movements(recorder_id,recorder_version)
    WHERE recorder_type='CryptoPayment';
CREATE INDEX idx_crypto_receipt_suspense_reversal ON reg_crypto_receipt_suspense_movements(reversal_of_line_id)
    WHERE reversal_of_line_id IS NOT NULL;


CREATE TABLE reg_crypto_receipt_suspense_balances (
    receipt_id UUID NOT NULL REFERENCES doc_crypto_custody_receipts(id),
    token_id UUID NOT NULL REFERENCES cat_tokens(id),
    amount NUMERIC(78,0) NOT NULL CHECK(amount>=0),
    last_movement_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY(receipt_id,token_id)
);

-- An automatic invoice payment attributes the receipt of its own chain effect.
ALTER TABLE doc_crypto_payments ADD COLUMN receipt_id UUID REFERENCES doc_crypto_custody_receipts(id);
CREATE INDEX idx_crypto_payments_receipt ON doc_crypto_payments(receipt_id) WHERE receipt_id IS NOT NULL;


CREATE TABLE reg_crypto_receipt_intake_audit (
    receipt_id UUID PRIMARY KEY REFERENCES doc_crypto_custody_receipts(id),
    verification_id UUID NOT NULL REFERENCES reg_crypto_transfer_verifications(id),
    posting_id UUID NOT NULL REFERENCES reg_crypto_custody_effect_postings(id),
    actor TEXT NOT NULL CHECK(btrim(actor)<>''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Existing funding receipts remain historical. Admission of a new receipt must
-- claim an indexed effect; migration never fabricates identities or movements.
ALTER TABLE reg_crypto_wallet_fundings
    ADD COLUMN occurrence_id UUID UNIQUE REFERENCES reg_crypto_accounting_effect_occurrences(id),
    ADD COLUMN posting_id UUID UNIQUE REFERENCES reg_crypto_custody_effect_postings(id),
    ADD CONSTRAINT ck_crypto_wallet_funding_recognition_pair CHECK((occurrence_id IS NULL)=(posting_id IS NULL)),
    DROP CONSTRAINT uq_crypto_wallet_fundings_tx_hash;

CREATE UNIQUE INDEX uq_crypto_wallet_fundings_legacy_tx
    ON reg_crypto_wallet_fundings(network_id,tx_hash) WHERE posting_id IS NULL;

CREATE INDEX idx_crypto_wallet_fundings_tx ON reg_crypto_wallet_fundings(network_id,tx_hash);

CREATE TABLE reg_crypto_wallet_funding_audit (
    funding_id UUID PRIMARY KEY REFERENCES reg_crypto_wallet_fundings(id),
    verification_id UUID NOT NULL REFERENCES reg_crypto_transfer_verifications(id),
    posting_id UUID NOT NULL REFERENCES reg_crypto_custody_effect_postings(id),
    actor TEXT NOT NULL CHECK(btrim(actor)<>''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- COPY may use a subtransaction, so xmin is not a reliable top-level posting
-- identity. Stamp new payment lines explicitly; historical rows stay NULL.
ALTER TABLE reg_crypto_balance_movements ADD COLUMN posting_txid BIGINT;

CREATE UNIQUE INDEX uq_crypto_native_funding_custody ON reg_crypto_custody_effect_postings(recorder_id)
    WHERE recorder_type='CryptoNativeFunding';

CREATE TABLE reg_crypto_native_funding_custody_bindings (
    posting_id UUID PRIMARY KEY REFERENCES reg_crypto_custody_effect_postings(id),
    funding_id UUID NOT NULL UNIQUE REFERENCES doc_crypto_native_fundings(id),
    period TIMESTAMPTZ NOT NULL,
    actor TEXT NOT NULL DEFAULT 'native-funding-settlement' CHECK(actor='native-funding-settlement'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE reg_crypto_sweep_custody_bindings (
    posting_id UUID PRIMARY KEY REFERENCES reg_crypto_custody_effect_postings(id),
    sweep_id UUID NOT NULL REFERENCES doc_crypto_sweeps(id),
    line_id UUID NOT NULL UNIQUE REFERENCES doc_crypto_sweep_lines(line_id),
    period TIMESTAMPTZ NOT NULL,
    actor TEXT NOT NULL DEFAULT 'sweep-settlement' CHECK(actor='sweep-settlement'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_sweep_custody_bindings_document ON reg_crypto_sweep_custody_bindings(sweep_id);

CREATE TABLE reg_crypto_treasury_custody_bindings (
    posting_id UUID PRIMARY KEY REFERENCES reg_crypto_custody_effect_postings(id),
    treasury_id UUID NOT NULL UNIQUE REFERENCES doc_treasury_withdrawals(id),
    signing_authorization_id UUID NOT NULL REFERENCES reg_crypto_signing_authorizations(id),
    period TIMESTAMPTZ NOT NULL,
    actor TEXT NOT NULL DEFAULT 'treasury-settlement' CHECK(actor='treasury-settlement'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

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

CREATE TABLE reg_crypto_custody_loss_bindings (
    posting_id UUID PRIMARY KEY REFERENCES reg_crypto_custody_effect_postings(id),
    adjustment_id UUID NOT NULL UNIQUE REFERENCES doc_crypto_custody_adjustments(id),
    external_outflow_id UUID NOT NULL UNIQUE REFERENCES reg_crypto_external_outflows(id),
    maker_id UUID NOT NULL,
    checker_id UUID NOT NULL CHECK(checker_id<>maker_id),
    period TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE reg_crypto_fee_reservation_events ADD COLUMN transition_txid BIGINT;

ALTER TABLE reg_crypto_chain_settlements ADD COLUMN network_fee_reservation_id UUID REFERENCES reg_crypto_fee_reservations(id);

ALTER TABLE doc_crypto_network_operations ADD COLUMN network_fee_reservation_id UUID UNIQUE REFERENCES reg_crypto_fee_reservations(id);

CREATE TABLE reg_crypto_network_fee_proofs (
    id UUID PRIMARY KEY,
    effect_key TEXT NOT NULL CHECK(effect_key~'^[0-9a-f]{64}$'),
    network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    tx_hash TEXT NOT NULL CHECK(tx_hash~'^[0-9a-f]{64}$'),
    family TEXT NOT NULL CHECK(family IN ('tron','evm')),
    payer_address_key TEXT NOT NULL,
    native_token_id UUID NOT NULL REFERENCES cat_tokens(id),
    block_hash TEXT NOT NULL CHECK(block_hash~'^[0-9a-f]{64}$'),
    block_number BIGINT NOT NULL CHECK(block_number>=0),
    status TEXT NOT NULL CHECK(status IN ('succeeded','reverted')),
    total_fee_minor NUMERIC(78,0) NOT NULL CHECK(total_fee_minor>=0),
    proof_digest TEXT NOT NULL CHECK(proof_digest~'^[0-9a-f]{64}$'),
    confirmations INT NOT NULL CHECK(confirmations>0),
    evidence JSONB NOT NULL,
    verified_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_network_fee_proofs_effect ON reg_crypto_network_fee_proofs(effect_key,block_number,verified_at);

CREATE TABLE reg_crypto_network_fee_bindings (
    reservation_id UUID PRIMARY KEY REFERENCES reg_crypto_fee_reservations(id),
    effect_key TEXT NOT NULL UNIQUE,
    proof_id UUID NOT NULL UNIQUE REFERENCES reg_crypto_network_fee_proofs(id),
    settlement_id UUID NOT NULL UNIQUE REFERENCES reg_crypto_chain_settlements(id),
    recorder_type TEXT NOT NULL CHECK(recorder_type IN ('ChainSettlement','CryptoNetworkOperation')),
    recorder_id UUID NOT NULL,
    generation INT NOT NULL DEFAULT 1 CHECK(generation=1),
    movement_line_id UUID UNIQUE REFERENCES reg_crypto_balance_movements(line_id) DEFERRABLE INITIALLY DEFERRED,
    cost_line_id UUID UNIQUE REFERENCES reg_crypto_platform_cost_movements(line_id) DEFERRABLE INITIALLY DEFERRED,
    period TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_network_fee_bindings_recorder ON reg_crypto_network_fee_bindings(recorder_type,recorder_id);

CREATE INDEX idx_expected_transfer_canonical_tx ON reg_crypto_expected_chain_effects(network_id,(regexp_replace(lower(tx_hash),'^0x','')),event_kind);

CREATE TABLE reg_crypto_expected_transfer_requests (
 expected_effect_id UUID PRIMARY KEY REFERENCES reg_crypto_expected_chain_effects(id),
 owner_type TEXT NOT NULL,owner_id UUID NOT NULL,first_event JSONB NOT NULL,
 requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),_txid BIGINT NOT NULL DEFAULT txid_current(),_deleted_at TIMESTAMPTZ
);

CREATE TABLE sys_crypto_expected_transfer_deliveries (
 expected_effect_id UUID PRIMARY KEY REFERENCES reg_crypto_expected_transfer_requests(expected_effect_id),
 next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),delivered_at TIMESTAMPTZ,attempts INT NOT NULL DEFAULT 0 CHECK(attempts>=0),
 version INT NOT NULL DEFAULT 1,_txid BIGINT NOT NULL DEFAULT txid_current(),_deleted_at TIMESTAMPTZ
);

CREATE INDEX idx_expected_transfer_delivery_due ON sys_crypto_expected_transfer_deliveries(next_attempt_at) WHERE delivered_at IS NULL;

CREATE TABLE reg_crypto_expected_transfer_routes (
 effect_id UUID PRIMARY KEY REFERENCES reg_crypto_accounting_effects(id),
 expected_effect_id UUID NOT NULL UNIQUE REFERENCES reg_crypto_expected_transfer_requests(expected_effect_id),
 first_verification_id UUID NOT NULL REFERENCES reg_crypto_transfer_verifications(id),
 owner_type TEXT NOT NULL,owner_id UUID NOT NULL,created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 _txid BIGINT NOT NULL DEFAULT txid_current(),_deleted_at TIMESTAMPTZ
);

CREATE TABLE reg_crypto_expected_transfer_evidence (
 verification_id UUID PRIMARY KEY REFERENCES reg_crypto_transfer_verifications(id),
 effect_id UUID NOT NULL REFERENCES reg_crypto_expected_transfer_routes(effect_id),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),_txid BIGINT NOT NULL DEFAULT txid_current(),_deleted_at TIMESTAMPTZ
);

-- A payment reversal returns an attribution to the receipt suspense; it never
-- claims a chain effect.
ALTER TABLE doc_crypto_adjustments
    ADD COLUMN basis_kind TEXT NOT NULL,
    ADD COLUMN proposal_txid BIGINT,
    ADD CONSTRAINT ck_crypto_adjustment_accounting_basis CHECK (
        proposal_txid IS NOT NULL AND (
            (mode='wallet_balance' AND basis_kind='wallet_variance')
            OR (mode='payment_reversal' AND basis_kind='payment_attribution')));

ALTER TABLE reg_crypto_adjustment_sources
    DROP CONSTRAINT reg_crypto_adjustment_sources_source_register_check,
    ADD CONSTRAINT reg_crypto_adjustment_sources_source_register_check CHECK(source_register IN ('suspense','merchant','fee'));


ALTER TABLE doc_crypto_resource_rentals ADD COLUMN maintenance_claim_token UUID;

ALTER TABLE doc_crypto_resource_rentals ADD CONSTRAINT chk_crypto_resource_rental_claim_token
 CHECK (maintenance_claim_token IS NULL OR (claimed_by<>'' AND claimed_at IS NOT NULL AND lease_expires_at IS NOT NULL));

COMMENT ON COLUMN doc_crypto_resource_rentals.maintenance_claim_token IS 'Fresh fencing token per maintenance claim; legacy leases drain before a new token is issued.';

ALTER TABLE reg_crypto_account_activations ADD COLUMN cost_claim_token UUID;

ALTER TABLE reg_crypto_account_activations ADD CONSTRAINT chk_crypto_activation_cost_claim
 CHECK (cost_claim_token IS NULL OR (cost_status='repairing' AND cost_claimed_by<>'' AND cost_claimed_at IS NOT NULL AND cost_lease_expires_at IS NOT NULL));

COMMENT ON COLUMN reg_crypto_account_activations.cost_claim_token IS 'Fresh fencing token per cost-repair attempt; legacy repairing leases drain without rewriting history.';

CREATE TABLE reg_crypto_activation_dispatches (
 authorization_id TEXT NOT NULL REFERENCES reg_crypto_signing_authorizations(authorization_id),
 activation_id UUID NOT NULL REFERENCES reg_crypto_account_activations(id),
 phase TEXT NOT NULL CHECK(phase IN ('authorize','sign','broadcast')),
 claim_token UUID NOT NULL,
 network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id),
 unsigned_fingerprint TEXT NOT NULL CHECK(unsigned_fingerprint<>''),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 _txid BIGINT NOT NULL DEFAULT txid_current(),
 PRIMARY KEY(authorization_id,phase,claim_token)
);

CREATE UNIQUE INDEX idx_crypto_activation_single_sign_dispatch ON reg_crypto_activation_dispatches(authorization_id) WHERE phase='sign';

ALTER TABLE reg_crypto_reorg_compensations ADD COLUMN request_basis JSONB
 CHECK(request_basis IS NULL OR jsonb_typeof(request_basis)='object');

-- NULL means a legacy decision whose original assessment clock is unknown.
-- Down retains this additive column so that no assessment evidence is lost.
ALTER TABLE reg_crypto_aml_decisions ADD COLUMN IF NOT EXISTS evaluated_at TIMESTAMPTZ;

ALTER TABLE sys_invoice_reconciliation_drift
 ADD COLUMN episode_id UUID,
 ADD COLUMN initial_details JSONB CHECK(initial_details IS NULL OR jsonb_typeof(initial_details)='object'),
 ADD COLUMN incident_id UUID REFERENCES reg_crypto_security_incidents(id);

CREATE UNIQUE INDEX uq_crypto_invoice_drift_episode ON sys_invoice_reconciliation_drift(episode_id) WHERE episode_id IS NOT NULL;

CREATE UNIQUE INDEX uq_crypto_invoice_drift_incident ON sys_invoice_reconciliation_drift(incident_id) WHERE incident_id IS NOT NULL;

ALTER TABLE reg_crypto_aml_preflight_jobs
 ADD COLUMN initial_policy_version BIGINT,
 ADD COLUMN initial_scope_version BIGINT,
 ADD FOREIGN KEY(policy_id,initial_policy_version) REFERENCES reg_crypto_aml_pool_policy_versions(policy_id,version);

CREATE TABLE reg_crypto_aml_preflight_attempts (
 job_id UUID NOT NULL REFERENCES reg_crypto_aml_preflight_jobs(id),
 lease_token UUID PRIMARY KEY, basis JSONB NOT NULL CHECK(jsonb_typeof(basis)='object'),
 worker_id TEXT NOT NULL CHECK(btrim(worker_id)<>''),
 claimed_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), lease_expires_at TIMESTAMPTZ NOT NULL,
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
 UNIQUE(job_id,lease_token)
);

ALTER TABLE reg_crypto_aml_preflight_observations ADD CONSTRAINT fk_crypto_preflight_observation_attempt
 FOREIGN KEY(job_id,lease_token) REFERENCES reg_crypto_aml_preflight_attempts(job_id,lease_token) NOT VALID;

-- Existing rows retain unknown original shadow/activation evidence. Down keeps
-- these additive columns so that retained registration history is not erased.
ALTER TABLE sys_crypto_block_ingestion ADD COLUMN IF NOT EXISTS registration_basis JSONB;

ALTER TABLE sys_crypto_block_ingestion ADD COLUMN IF NOT EXISTS activation_basis JSONB;

ALTER TABLE reg_crypto_external_outflows ADD COLUMN IF NOT EXISTS initial_basis JSONB CHECK(initial_basis IS NULL OR jsonb_typeof(initial_basis)='object');

ALTER TABLE reg_crypto_external_outflow_observations ADD COLUMN IF NOT EXISTS incident_id UUID REFERENCES reg_crypto_reconciliation_items(id);

ALTER TABLE reg_crypto_external_outflows ADD COLUMN IF NOT EXISTS classification_basis JSONB, ADD COLUMN IF NOT EXISTS resolution_basis JSONB;

ALTER TABLE doc_crypto_custody_adjustments ADD COLUMN IF NOT EXISTS decision_basis JSONB;

ALTER TABLE reg_crypto_external_outflow_actions ADD COLUMN IF NOT EXISTS decision_basis JSONB;



-- ---- Triggers and views ----

CREATE TRIGGER trg_crypto_accounting_effect_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_accounting_effects
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_accounting_immutable();

CREATE TRIGGER trg_crypto_accounting_occurrence_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_accounting_effect_occurrences
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_accounting_immutable();

CREATE TRIGGER trg_crypto_accounting_evidence_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_accounting_effect_evidence
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_accounting_immutable();

CREATE TRIGGER trg_crypto_accounting_evidence_guard
    BEFORE INSERT ON reg_crypto_accounting_effect_evidence
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_accounting_evidence_guard();

CREATE CONSTRAINT TRIGGER trg_crypto_accounting_effect_coverage
    AFTER INSERT ON reg_crypto_accounting_effects DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_accounting_coverage_guard();

CREATE CONSTRAINT TRIGGER trg_crypto_accounting_occurrence_coverage
    AFTER INSERT ON reg_crypto_accounting_effect_occurrences DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_accounting_coverage_guard();

CREATE TRIGGER trg_crypto_custody_receipt_txid BEFORE UPDATE ON doc_crypto_custody_receipts
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_crypto_custody_receipt_mutation BEFORE UPDATE OR DELETE ON doc_crypto_custody_receipts
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_receipt_mutation_guard();

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

CREATE TRIGGER trg_crypto_custody_posting_identity BEFORE INSERT ON reg_crypto_custody_effect_postings
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_posting_identity_guard();

CREATE CONSTRAINT TRIGGER trg_crypto_custody_posting_complete
    AFTER INSERT ON reg_crypto_custody_effect_postings DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_posting_complete();

CREATE CONSTRAINT TRIGGER trg_crypto_custody_recognition_complete
    AFTER INSERT ON reg_crypto_custody_effect_recognitions DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_posting_complete();

CREATE CONSTRAINT TRIGGER trg_crypto_custody_receipt_complete AFTER INSERT OR UPDATE ON doc_crypto_custody_receipts
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_receipt_complete();

CREATE CONSTRAINT TRIGGER trg_crypto_custody_receipt_movement_complete AFTER INSERT ON reg_crypto_balance_movements
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_receipt_movement_complete();

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

CREATE TRIGGER trg_crypto_wallet_funding_audit_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_wallet_funding_audit
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_financial_movement_immutable();

CREATE TRIGGER trg_crypto_wallet_funding_admission BEFORE INSERT ON reg_crypto_wallet_fundings
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_wallet_funding_admission();

CREATE TRIGGER trg_crypto_wallet_funding_payment_guard BEFORE INSERT OR UPDATE ON doc_crypto_payments
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_wallet_funding_payment_guard();

CREATE CONSTRAINT TRIGGER trg_crypto_wallet_funding_complete AFTER INSERT ON reg_crypto_wallet_fundings
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_wallet_funding_complete();

CREATE CONSTRAINT TRIGGER trg_crypto_wallet_funding_posting_complete AFTER INSERT ON reg_crypto_custody_effect_postings
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_wallet_funding_complete();

CREATE CONSTRAINT TRIGGER trg_crypto_wallet_funding_movement_complete AFTER INSERT ON reg_crypto_balance_movements
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_wallet_funding_complete();

CREATE TRIGGER trg_crypto_wallet_funding_movement_guard BEFORE INSERT ON reg_crypto_balance_movements
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_wallet_funding_movement_guard();

DO $$
DECLARE target TEXT;
BEGIN
    FOREACH target IN ARRAY ARRAY['reg_crypto_merchant_balance_movements','reg_crypto_fee_movements',
        'reg_crypto_platform_cost_movements','reg_crypto_provider_balance_movements'] LOOP
        EXECUTE format('CREATE TRIGGER %I BEFORE INSERT ON %I FOR EACH ROW EXECUTE FUNCTION fn_crypto_wallet_funding_no_attribution()',
            'trg_no_funding_'||target,target);
    END LOOP;
END;
$$;

CREATE TRIGGER trg_crypto_balance_posting_transaction BEFORE INSERT ON reg_crypto_balance_movements
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_balance_posting_transaction();

CREATE TRIGGER trg_crypto_payment_frozen_basis BEFORE UPDATE OR DELETE ON doc_crypto_payments
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_payment_frozen_basis_guard();
CREATE CONSTRAINT TRIGGER trg_crypto_payment_attribution_document AFTER UPDATE ON doc_crypto_payments
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
    WHEN (OLD.posted IS DISTINCT FROM NEW.posted OR OLD.posted_version IS DISTINCT FROM NEW.posted_version)
    EXECUTE FUNCTION fn_crypto_payment_attribution_complete();
DO $$
DECLARE target TEXT;
BEGIN
    FOREACH target IN ARRAY ARRAY['reg_crypto_receipt_suspense_movements','reg_crypto_merchant_balance_movements','reg_crypto_fee_movements'] LOOP
        EXECUTE format('CREATE CONSTRAINT TRIGGER %I AFTER INSERT ON %I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_payment_attribution_complete()',
            'trg_payment_attribution_'||target,target);
    END LOOP;
END;
$$;


CREATE TRIGGER trg_native_funding_binding_immutable BEFORE UPDATE OR DELETE ON reg_crypto_native_funding_custody_bindings
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_financial_movement_immutable();

CREATE CONSTRAINT TRIGGER trg_native_funding_posting_complete AFTER INSERT ON reg_crypto_custody_effect_postings
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_native_funding_custody_complete();

CREATE CONSTRAINT TRIGGER trg_native_funding_binding_complete AFTER INSERT ON reg_crypto_native_funding_custody_bindings
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_native_funding_custody_complete();

CREATE CONSTRAINT TRIGGER trg_native_funding_chain_complete AFTER INSERT ON reg_crypto_chain_settlements
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_native_funding_settlement_complete();

CREATE CONSTRAINT TRIGGER trg_native_funding_fee_complete AFTER INSERT OR UPDATE ON reg_crypto_fee_reservations
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_native_funding_settlement_complete();

DO $$ DECLARE target TEXT; BEGIN
    FOREACH target IN ARRAY ARRAY['reg_crypto_balance_movements','reg_crypto_merchant_balance_movements','reg_crypto_fee_movements',
        'reg_crypto_platform_cost_movements','reg_crypto_provider_balance_movements'] LOOP
        EXECUTE format('CREATE CONSTRAINT TRIGGER %I AFTER INSERT ON %I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_native_funding_movement_complete()',
            'trg_native_funding_complete_'||target,target);
    END LOOP;
END; $$;

CREATE TRIGGER trg_native_funding_basis BEFORE UPDATE OR DELETE ON doc_crypto_native_fundings
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_native_funding_basis_guard();

CREATE CONSTRAINT TRIGGER trg_native_funding_document_complete AFTER INSERT OR UPDATE ON doc_crypto_native_fundings
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_native_funding_document_complete();

CREATE TRIGGER trg_payment_native_funding BEFORE INSERT OR UPDATE ON doc_crypto_payments
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_payment_native_funding_guard();

CREATE TRIGGER trg_sweep_custody_binding_immutable BEFORE UPDATE OR DELETE ON reg_crypto_sweep_custody_bindings
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_financial_movement_immutable();

CREATE CONSTRAINT TRIGGER trg_sweep_binding_complete AFTER INSERT ON reg_crypto_sweep_custody_bindings
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_sweep_binding_complete();

CREATE CONSTRAINT TRIGGER trg_sweep_posting_complete AFTER INSERT ON reg_crypto_custody_effect_postings
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_sweep_posting_complete();

DO $$ DECLARE target TEXT; BEGIN
    FOREACH target IN ARRAY ARRAY['reg_crypto_balance_movements','reg_crypto_merchant_balance_movements','reg_crypto_fee_movements',
        'reg_crypto_platform_cost_movements','reg_crypto_provider_balance_movements'] LOOP
        EXECUTE format('CREATE CONSTRAINT TRIGGER %I AFTER INSERT ON %I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_sweep_financial_complete()',
            'trg_sweep_complete_'||target,target);
    END LOOP;
END; $$;

CREATE CONSTRAINT TRIGGER trg_sweep_line_complete AFTER INSERT OR UPDATE ON doc_crypto_sweep_lines
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_sweep_settlement_complete();

CREATE CONSTRAINT TRIGGER trg_sweep_chain_complete AFTER INSERT ON reg_crypto_chain_settlements
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_sweep_settlement_complete();

CREATE CONSTRAINT TRIGGER trg_sweep_fee_complete AFTER INSERT OR UPDATE ON reg_crypto_fee_reservations
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_sweep_settlement_complete();

CREATE TRIGGER trg_sweep_header_basis BEFORE UPDATE OR DELETE ON doc_crypto_sweeps FOR EACH ROW EXECUTE FUNCTION fn_crypto_sweep_basis_guard();

CREATE TRIGGER trg_sweep_line_basis BEFORE UPDATE OR DELETE ON doc_crypto_sweep_lines FOR EACH ROW EXECUTE FUNCTION fn_crypto_sweep_basis_guard();

CREATE OR REPLACE VIEW crypto_aml_allocatable_transfers AS
 SELECT plan.id allocation_id,plan.merchant_id,plan.network_id,plan.token_id,plan.source_wallet_id,plan.destination_wallet_id,
 plan.principal,plan.service_fee,e.id effect_id,spent.line_id source_movement_id,received.line_id destination_movement_id
 FROM reg_crypto_aml_internal_allocations plan
 JOIN reg_crypto_sweep_custody_bindings b ON b.line_id=plan.sweep_attempt_id
 JOIN doc_crypto_sweep_lines line ON line.line_id=b.line_id AND line.document_id=b.sweep_id AND line.wallet_id=plan.source_wallet_id AND line.amount=plan.gross
 JOIN doc_crypto_sweeps sweep ON sweep.id=b.sweep_id AND sweep.hot_wallet_id=plan.destination_wallet_id AND sweep.token_id=plan.token_id
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

CREATE TRIGGER trg_treasury_custody_binding_immutable BEFORE UPDATE OR DELETE ON reg_crypto_treasury_custody_bindings
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_financial_movement_immutable();

CREATE CONSTRAINT TRIGGER trg_treasury_binding_complete AFTER INSERT ON reg_crypto_treasury_custody_bindings
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_treasury_binding_complete();

CREATE CONSTRAINT TRIGGER trg_treasury_posting_complete AFTER INSERT ON reg_crypto_custody_effect_postings
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_treasury_posting_complete();

DO $$ DECLARE target TEXT; BEGIN
    FOREACH target IN ARRAY ARRAY['reg_crypto_balance_movements','reg_crypto_merchant_balance_movements','reg_crypto_fee_movements',
        'reg_crypto_platform_cost_movements','reg_crypto_provider_balance_movements'] LOOP
        EXECUTE format('CREATE CONSTRAINT TRIGGER %I AFTER INSERT ON %I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_treasury_financial_complete()',
            'trg_treasury_complete_'||target,target);
    END LOOP;
END; $$;

CREATE CONSTRAINT TRIGGER trg_treasury_document_complete AFTER INSERT OR UPDATE ON doc_treasury_withdrawals
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_treasury_settlement_complete();

CREATE CONSTRAINT TRIGGER trg_treasury_chain_complete AFTER INSERT ON reg_crypto_chain_settlements
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_treasury_settlement_complete();

CREATE CONSTRAINT TRIGGER trg_treasury_fee_complete AFTER INSERT OR UPDATE ON reg_crypto_fee_reservations
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_treasury_settlement_complete();

CREATE TRIGGER trg_treasury_basis BEFORE UPDATE OR DELETE ON doc_treasury_withdrawals FOR EACH ROW EXECUTE FUNCTION fn_crypto_treasury_basis_guard();

DO $$ DECLARE target TEXT; BEGIN
    FOREACH target IN ARRAY ARRAY['reg_crypto_balance_reservations','reg_crypto_revenue_reservations'] LOOP
        EXECUTE format('CREATE TRIGGER %I BEFORE INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION fn_crypto_treasury_reservation_guard()', 'trg_treasury_basis_'||target,target);
        EXECUTE format('CREATE CONSTRAINT TRIGGER %I AFTER INSERT OR UPDATE ON %I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_treasury_reservation_complete()', 'trg_treasury_complete_'||target,target);
    END LOOP;
END; $$;

CREATE TRIGGER trg_treasury_fee_release BEFORE UPDATE ON reg_crypto_fee_reservations FOR EACH ROW EXECUTE FUNCTION fn_crypto_treasury_fee_release_guard();

CREATE TRIGGER trg_treasury_outflow_owner BEFORE INSERT ON reg_crypto_balance_movements FOR EACH ROW EXECUTE FUNCTION fn_crypto_treasury_outflow_owner_guard();

CREATE TRIGGER trg_withdrawal_custody_binding_immutable BEFORE UPDATE OR DELETE ON reg_crypto_withdrawal_custody_bindings FOR EACH ROW EXECUTE FUNCTION fn_crypto_financial_movement_immutable();

CREATE OR REPLACE VIEW crypto_aml_valid_withdrawal_facts AS
 SELECT f.line_id FROM reg_crypto_aml_withdrawal_facts f
 JOIN reg_crypto_aml_withdrawal_lines l ON l.id=f.line_id AND l.wallet_id=f.wallet_id AND l.token_id=f.token_id AND l.amount=f.amount
 JOIN reg_crypto_aml_withdrawal_plans plan ON plan.request_id=l.request_id
 JOIN doc_crypto_withdrawals w ON w.id=l.withdrawal_id AND w.basis_type='CryptoAMLWithdrawalLine' AND w.basis_id=l.id
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

CREATE CONSTRAINT TRIGGER trg_withdrawal_binding_complete AFTER INSERT ON reg_crypto_withdrawal_custody_bindings DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_withdrawal_binding_complete();

CREATE CONSTRAINT TRIGGER trg_withdrawal_posting_complete AFTER INSERT ON reg_crypto_custody_effect_postings DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_withdrawal_posting_complete();

DO $$ DECLARE target TEXT; BEGIN
    FOREACH target IN ARRAY ARRAY['reg_crypto_balance_movements','reg_crypto_merchant_balance_movements','reg_crypto_fee_movements','reg_crypto_platform_cost_movements','reg_crypto_provider_balance_movements'] LOOP
        EXECUTE format('CREATE CONSTRAINT TRIGGER %I AFTER INSERT ON %I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_withdrawal_financial_complete()','trg_withdrawal_complete_'||target,target);
    END LOOP;
END; $$;

CREATE CONSTRAINT TRIGGER trg_withdrawal_document_complete AFTER INSERT OR UPDATE ON doc_crypto_withdrawals DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_withdrawal_settlement_complete();

CREATE CONSTRAINT TRIGGER trg_withdrawal_chain_complete AFTER INSERT ON reg_crypto_chain_settlements DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_withdrawal_settlement_complete();

CREATE CONSTRAINT TRIGGER trg_withdrawal_fee_complete AFTER INSERT OR UPDATE ON reg_crypto_fee_reservations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_withdrawal_settlement_complete();

CREATE TRIGGER trg_withdrawal_basis BEFORE UPDATE OR DELETE ON doc_crypto_withdrawals FOR EACH ROW EXECUTE FUNCTION fn_crypto_withdrawal_basis_guard();

CREATE TRIGGER trg_withdrawal_request_basis BEFORE UPDATE OR DELETE ON doc_withdrawal_requests FOR EACH ROW EXECUTE FUNCTION fn_crypto_withdrawal_basis_guard();

CREATE TRIGGER trg_withdrawal_reservation_basis BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_balance_reservations FOR EACH ROW EXECUTE FUNCTION fn_crypto_withdrawal_reservation_guard();

CREATE CONSTRAINT TRIGGER trg_withdrawal_reservation_complete AFTER INSERT OR UPDATE ON reg_crypto_balance_reservations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_withdrawal_reservation_complete();

CREATE TRIGGER trg_withdrawal_fee_release BEFORE UPDATE ON reg_crypto_fee_reservations FOR EACH ROW EXECUTE FUNCTION fn_crypto_withdrawal_fee_release_guard();

CREATE TRIGGER trg_withdrawal_outflow_owner BEFORE INSERT ON reg_crypto_balance_movements FOR EACH ROW EXECUTE FUNCTION fn_crypto_withdrawal_outflow_owner_guard();

CREATE CONSTRAINT TRIGGER trg_withdrawal_request_complete AFTER INSERT OR UPDATE ON doc_withdrawal_requests DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_withdrawal_request_complete();

CREATE TRIGGER trg_custody_loss_binding_immutable BEFORE UPDATE OR DELETE ON reg_crypto_custody_loss_bindings FOR EACH ROW EXECUTE FUNCTION fn_crypto_financial_movement_immutable();

CREATE CONSTRAINT TRIGGER trg_custody_loss_binding_complete AFTER INSERT ON reg_crypto_custody_loss_bindings DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_loss_binding_complete();

CREATE CONSTRAINT TRIGGER trg_custody_loss_posting_complete AFTER INSERT ON reg_crypto_custody_effect_postings DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_loss_posting_complete();

DO $$ DECLARE target TEXT; BEGIN
    FOREACH target IN ARRAY ARRAY['reg_crypto_balance_movements','reg_crypto_platform_cost_movements','reg_crypto_merchant_balance_movements','reg_crypto_fee_movements','reg_crypto_provider_balance_movements'] LOOP
        EXECUTE format('CREATE CONSTRAINT TRIGGER %I AFTER INSERT ON %I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_loss_financial_complete()','trg_custody_loss_complete_'||target,target);
    END LOOP;
END; $$;

CREATE CONSTRAINT TRIGGER trg_custody_loss_document_complete AFTER INSERT OR UPDATE ON doc_crypto_custody_adjustments DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_loss_document_complete();

CREATE TRIGGER trg_custody_loss_basis BEFORE UPDATE ON doc_crypto_custody_adjustments FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_loss_basis_guard();

CREATE TRIGGER trg_custody_loss_outflow_owner BEFORE INSERT ON reg_crypto_balance_movements FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_loss_outflow_guard();

CREATE TRIGGER trg_network_fee_transition_txid BEFORE INSERT ON reg_crypto_fee_reservation_events FOR EACH ROW EXECUTE FUNCTION fn_crypto_fee_transition_transaction();

CREATE TRIGGER trg_network_fee_proof_immutable BEFORE UPDATE OR DELETE ON reg_crypto_network_fee_proofs FOR EACH ROW EXECUTE FUNCTION fn_crypto_financial_movement_immutable();

CREATE TRIGGER trg_network_fee_binding_immutable BEFORE UPDATE OR DELETE ON reg_crypto_network_fee_bindings FOR EACH ROW EXECUTE FUNCTION fn_crypto_financial_movement_immutable();

CREATE TRIGGER trg_network_fee_proof_profile BEFORE INSERT ON reg_crypto_network_fee_proofs FOR EACH ROW EXECUTE FUNCTION fn_crypto_network_fee_proof_guard();

CREATE CONSTRAINT TRIGGER trg_network_fee_binding_complete AFTER INSERT ON reg_crypto_network_fee_bindings DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_network_fee_binding_complete();

CREATE CONSTRAINT TRIGGER trg_network_fee_reservation_complete AFTER INSERT OR UPDATE ON reg_crypto_fee_reservations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_network_fee_state_complete();

CREATE CONSTRAINT TRIGGER trg_network_fee_settlement_complete AFTER INSERT ON reg_crypto_chain_settlements DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_network_fee_state_complete();

CREATE CONSTRAINT TRIGGER trg_network_fee_component_complete AFTER INSERT ON reg_crypto_chain_settlement_components DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_network_fee_state_complete();

CREATE CONSTRAINT TRIGGER trg_network_fee_document_complete AFTER INSERT OR UPDATE ON doc_crypto_network_operations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_network_fee_state_complete();

DO $$ DECLARE target TEXT; BEGIN
    FOREACH target IN ARRAY ARRAY['reg_crypto_balance_movements','reg_crypto_platform_cost_movements','reg_crypto_merchant_balance_movements','reg_crypto_fee_movements','reg_crypto_provider_balance_movements'] LOOP
        EXECUTE format('CREATE CONSTRAINT TRIGGER %I AFTER INSERT ON %I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_network_fee_financial_complete()','trg_network_fee_complete_'||target,target);
    END LOOP;
END; $$;

CREATE TRIGGER trg_network_fee_basis BEFORE UPDATE OR DELETE ON reg_crypto_fee_reservations FOR EACH ROW EXECUTE FUNCTION fn_crypto_network_fee_basis_guard();

CREATE TRIGGER trg_network_fee_document_basis BEFORE UPDATE OR DELETE ON doc_crypto_network_operations FOR EACH ROW EXECUTE FUNCTION fn_crypto_network_fee_basis_guard();

CREATE CONSTRAINT TRIGGER trg_network_fee_replacement_complete AFTER UPDATE ON reg_crypto_fee_reservations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_network_fee_replacement_complete();

CREATE TRIGGER trg_network_fee_receipt_owner BEFORE INSERT ON reg_crypto_chain_settlements FOR EACH ROW EXECUTE FUNCTION fn_crypto_network_fee_legacy_owner_guard();

CREATE TRIGGER trg_network_fee_movement_owner BEFORE INSERT ON reg_crypto_balance_movements FOR EACH ROW EXECUTE FUNCTION fn_crypto_network_fee_legacy_owner_guard();

CREATE TRIGGER trg_expected_request_basis BEFORE INSERT ON reg_crypto_expected_transfer_requests FOR EACH ROW EXECUTE FUNCTION fn_crypto_expected_transfer_guard();

CREATE TRIGGER trg_expected_route_basis BEFORE INSERT ON reg_crypto_expected_transfer_routes FOR EACH ROW EXECUTE FUNCTION fn_crypto_expected_transfer_guard();

CREATE TRIGGER trg_expected_route_evidence_basis BEFORE INSERT ON reg_crypto_expected_transfer_evidence FOR EACH ROW EXECUTE FUNCTION fn_crypto_expected_transfer_guard();

DO $$ DECLARE target TEXT; BEGIN
 FOREACH target IN ARRAY ARRAY['reg_crypto_expected_transfer_requests','reg_crypto_expected_transfer_routes','reg_crypto_expected_transfer_evidence'] LOOP
  EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION fn_crypto_financial_movement_immutable()','trg_immutable_'||target,target);
 END LOOP; END; $$;

CREATE CONSTRAINT TRIGGER trg_expected_request_complete AFTER INSERT ON reg_crypto_expected_transfer_requests DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_expected_request_complete();

CREATE TRIGGER trg_expected_posting_owner BEFORE INSERT ON reg_crypto_custody_effect_postings FOR EACH ROW EXECUTE FUNCTION fn_crypto_expected_posting_owner_guard();

CREATE TRIGGER trg_expected_payment BEFORE INSERT ON doc_crypto_payments FOR EACH ROW EXECUTE FUNCTION fn_crypto_expected_external_payment_guard();

CREATE TRIGGER trg_crypto_adjustment_accounting_basis BEFORE INSERT OR UPDATE OR DELETE ON doc_crypto_adjustments
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_adjustment_accounting_basis();

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

CREATE TRIGGER trg_crypto_adjustment_cannot_claim_effect BEFORE INSERT ON reg_crypto_custody_effect_postings
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_adjustment_cannot_claim_effect();

CREATE TRIGGER trg_crypto_reconciliation_request_identity BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_reconciliation_requests
 FOR EACH ROW EXECUTE FUNCTION fn_crypto_reconciliation_request_identity_guard();

CREATE TRIGGER trg_crypto_activation_identity BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_account_activations FOR EACH ROW EXECUTE FUNCTION fn_crypto_activation_identity_guard();

CREATE TRIGGER trg_crypto_activation_dispatch_guard BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_activation_dispatches FOR EACH ROW EXECUTE FUNCTION fn_crypto_activation_dispatch_guard();

CREATE TRIGGER trg_crypto_reorg_compensation_immutable BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_reorg_compensations
 FOR EACH ROW EXECUTE FUNCTION fn_crypto_reorg_compensation_immutable();

CREATE TRIGGER trg_crypto_network_hold_insert_replay BEFORE INSERT ON reg_crypto_network_holds FOR EACH ROW EXECUTE FUNCTION fn_crypto_sql_hold_guard();

CREATE TRIGGER trg_crypto_wallet_hold_insert_replay BEFORE INSERT ON reg_crypto_wallet_allocation_holds FOR EACH ROW EXECUTE FUNCTION fn_crypto_sql_hold_guard();

DROP TRIGGER trg_crypto_network_hold_owner_guard ON reg_crypto_network_holds;

DROP TRIGGER trg_crypto_wallet_hold_owner_guard ON reg_crypto_wallet_allocation_holds;

CREATE TRIGGER trg_crypto_network_hold_owner_guard BEFORE UPDATE OR DELETE ON reg_crypto_network_holds FOR EACH ROW EXECUTE FUNCTION fn_crypto_sql_hold_guard();

CREATE TRIGGER trg_crypto_wallet_hold_owner_guard BEFORE UPDATE OR DELETE ON reg_crypto_wallet_allocation_holds FOR EACH ROW EXECUTE FUNCTION fn_crypto_sql_hold_guard();

CREATE TRIGGER trg_crypto_aml_decision_replay BEFORE INSERT ON reg_crypto_aml_decisions FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_decision_replay_guard();

CREATE TRIGGER trg_crypto_invoice_drift_guard BEFORE INSERT OR UPDATE OR DELETE ON sys_invoice_reconciliation_drift FOR EACH ROW EXECUTE FUNCTION fn_crypto_invoice_drift_guard();

CREATE TRIGGER trg_crypto_invoice_drift_incident_guard BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_security_incidents FOR EACH ROW EXECUTE FUNCTION fn_crypto_invoice_drift_incident_guard();

CREATE TRIGGER trg_crypto_aml_preflight_attempt_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_preflight_attempts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_preflight_job_identity BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_aml_preflight_jobs FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_preflight_job_guard();

CREATE TRIGGER trg_crypto_aml_preflight_attempt_guard BEFORE INSERT ON reg_crypto_aml_preflight_attempts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_preflight_attempt_guard();

CREATE CONSTRAINT TRIGGER trg_crypto_aml_preflight_claim_committed AFTER INSERT OR UPDATE ON reg_crypto_aml_preflight_jobs
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_preflight_claim_committed();

CREATE TRIGGER trg_crypto_aml_preflight_attempt_observation BEFORE INSERT ON reg_crypto_aml_preflight_observations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_preflight_attempt_observation_guard();

CREATE TRIGGER trg_crypto_block_ingestion_basis BEFORE INSERT OR UPDATE OR DELETE ON sys_crypto_block_ingestion FOR EACH ROW EXECUTE FUNCTION fn_crypto_ingestion_basis_guard();

CREATE TRIGGER trg_crypto_watch_target_guard BEFORE INSERT OR UPDATE OR DELETE ON sys_crypto_watch_targets FOR EACH ROW EXECUTE FUNCTION fn_crypto_watch_target_guard();

CREATE TRIGGER trg_crypto_watched_wallet_identity BEFORE UPDATE OR DELETE ON cat_wallets FOR EACH ROW EXECUTE FUNCTION fn_crypto_watched_catalog_guard();

CREATE TRIGGER trg_crypto_watched_asset_identity BEFORE UPDATE OR DELETE ON cat_tokens FOR EACH ROW EXECUTE FUNCTION fn_crypto_watched_catalog_guard();

CREATE TRIGGER trg_crypto_external_outflow_basis BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_external_outflows FOR EACH ROW EXECUTE FUNCTION fn_crypto_external_outflow_basis_guard();

CREATE TRIGGER trg_crypto_external_outflow_observation_source BEFORE INSERT ON reg_crypto_external_outflow_observations FOR EACH ROW EXECUTE FUNCTION fn_crypto_external_outflow_observation_source();

CREATE TRIGGER trg_crypto_outflow_initial_decision BEFORE INSERT ON reg_crypto_external_outflows FOR EACH ROW EXECUTE FUNCTION fn_crypto_outflow_decision_insert_guard();

CREATE TRIGGER trg_crypto_custody_initial_decision BEFORE INSERT ON doc_crypto_custody_adjustments FOR EACH ROW EXECUTE FUNCTION fn_crypto_outflow_decision_insert_guard();

CREATE TRIGGER trg_crypto_external_outflow_decision BEFORE UPDATE ON reg_crypto_external_outflows FOR EACH ROW EXECUTE FUNCTION fn_crypto_outflow_decision_guard();

CREATE TRIGGER trg_crypto_custody_decision BEFORE UPDATE ON doc_crypto_custody_adjustments FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_decision_guard();

CREATE TRIGGER trg_crypto_outflow_decision_audit_insert BEFORE INSERT ON reg_crypto_external_outflow_actions FOR EACH ROW EXECUTE FUNCTION fn_crypto_outflow_decision_audit_guard();

CREATE TRIGGER trg_crypto_outflow_decision_audit AFTER UPDATE ON reg_crypto_external_outflows FOR EACH ROW EXECUTE FUNCTION fn_crypto_outflow_append_decision_audit();

CREATE TRIGGER trg_crypto_custody_decision_audit AFTER UPDATE ON doc_crypto_custody_adjustments FOR EACH ROW EXECUTE FUNCTION fn_crypto_outflow_append_decision_audit();

CREATE CONSTRAINT TRIGGER trg_crypto_outflow_decision_complete AFTER INSERT OR UPDATE ON reg_crypto_external_outflows DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_outflow_decision_audit_complete();

CREATE CONSTRAINT TRIGGER trg_crypto_custody_decision_complete AFTER INSERT OR UPDATE ON doc_crypto_custody_adjustments DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_outflow_decision_audit_complete();

CREATE TRIGGER trg_crypto_merchant_credit_guard BEFORE INSERT OR UPDATE OR DELETE ON doc_crypto_merchant_credits
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_merchant_credit_guard();
CREATE CONSTRAINT TRIGGER trg_crypto_merchant_credit_complete AFTER INSERT OR UPDATE ON doc_crypto_merchant_credits
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_merchant_credit_complete();
DO $$
DECLARE target TEXT;
BEGIN
    FOREACH target IN ARRAY ARRAY['reg_crypto_receipt_suspense_movements','reg_crypto_merchant_balance_movements',
        'reg_crypto_fee_movements','reg_crypto_balance_movements'] LOOP
        EXECUTE format('CREATE CONSTRAINT TRIGGER %I AFTER INSERT ON %I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_merchant_credit_complete()',
            'trg_merchant_credit_complete_'||target,target);
    END LOOP;
END;
$$;
CREATE TRIGGER trg_crypto_custody_receipt_audit_seed AFTER INSERT ON doc_crypto_custody_receipts
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_receipt_audit_seed();
CREATE TRIGGER trg_crypto_custody_receipt_audit_guard BEFORE UPDATE OR DELETE ON reg_crypto_custody_receipt_audits
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_receipt_audit_guard();

INSERT INTO auth_permissions (code,name,description,resource,action) VALUES
    ('crypto_receipt:read','Журнал невыясненных поступлений','View unidentified custody receipts','crypto_receipt','read'),
    ('crypto_receipt:credit','Зачисление невыясненных поступлений','Prepare or reject a merchant credit of an unidentified receipt','crypto_receipt','credit'),
    ('crypto_receipt:approve','Подтверждение зачисления поступлений','Approve a merchant credit prepared by another operator','crypto_receipt','approve')
ON CONFLICT (code) DO NOTHING;
-- The physical basis of an allocatable payment is the custody line of the
-- receipt it attributes; the payment itself only moves suspense and liabilities.
CREATE OR REPLACE VIEW crypto_aml_allocatable_payments AS
 SELECT p.id payment_id,p.posted_version payment_version,p.merchant_id,p.network_id,p.token_id,p.wallet_id,
        credit.amount principal,fees.amount service_fee,e.id effect_id,physical.line_id movement_id
 FROM doc_crypto_payments p JOIN reg_crypto_aml_wallet_baselines baseline ON baseline.wallet_id=p.wallet_id AND baseline.token_id=p.token_id AND p.block_number>baseline.block_number
 JOIN cat_wallets w ON w.id=p.wallet_id AND w.custody_merchant_id=p.merchant_id AND w.network_id=p.network_id
 JOIN cat_tokens token ON token.id=p.token_id AND token.network_id=p.network_id
 JOIN reg_crypto_aml_effects e ON e.wallet_id=p.wallet_id AND e.token_id=p.token_id AND e.network_id=p.network_id
   AND e.tx_hash=p.tx_hash AND e.event_kind=p.event_kind AND e.event_index=p.event_index AND e.block_hash=p.block_hash AND e.amount=p.amount AND e.source_wallet_id IS NULL
 JOIN reg_crypto_aml_effect_chain_states cs ON cs.effect_id=e.id AND cs.canonicality='canonical'
 JOIN reg_crypto_transfer_verifications proof ON proof.id=p.verification_id AND proof.network_id=e.network_id AND proof.tx_hash=e.tx_hash
   AND proof.event_kind=e.event_kind AND proof.event_index=e.event_index AND proof.block_hash=e.block_hash AND proof.economics_hash=e.economics_hash
 JOIN LATERAL(SELECT COALESCE(SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END),0) amount,
    COALESCE(SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END) FILTER(WHERE recorder_version=p.posted_version),0) current_amount
    FROM reg_crypto_merchant_balance_movements WHERE recorder_id=p.id AND recorder_type='CryptoPayment' AND merchant_id=p.merchant_id AND token_id=p.token_id) credit ON TRUE
 JOIN LATERAL(SELECT COALESCE(SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END),0) amount,
    COALESCE(SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END) FILTER(WHERE recorder_version=p.posted_version),0) current_amount
    FROM reg_crypto_fee_movements WHERE recorder_id=p.id AND recorder_type='CryptoPayment' AND merchant_id=p.merchant_id AND token_id=p.token_id AND fee_type='processing') fees ON TRUE
 JOIN doc_crypto_custody_receipts receipt ON receipt.id=p.receipt_id AND receipt.posted AND receipt.wallet_id=p.wallet_id
   AND receipt.token_id=p.token_id AND receipt.amount=p.amount
 JOIN reg_crypto_balance_movements physical ON physical.line_id=receipt.custody_line_id AND physical.recorder_id=receipt.id
    AND physical.recorder_type='CryptoCustodyReceipt' AND physical.wallet_id=p.wallet_id AND physical.token_id=p.token_id
    AND physical.record_type='receipt' AND physical.amount=p.amount AND physical.created_at>=baseline.created_at
 WHERE p.posted AND p.status='settled' AND p.canonicality='canonical'
   AND credit.amount>=0 AND fees.amount>=0 AND credit.amount=credit.current_amount AND fees.amount=fees.current_amount AND credit.amount+fees.amount=p.amount;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER trg_crypto_payment_attribution_document ON doc_crypto_payments;
DO $$
DECLARE target TEXT;
BEGIN
    FOREACH target IN ARRAY ARRAY['reg_crypto_receipt_suspense_movements','reg_crypto_merchant_balance_movements','reg_crypto_fee_movements'] LOOP
        EXECUTE format('DROP TRIGGER %I ON %I','trg_payment_attribution_'||target,target);
    END LOOP;
END;
$$;
DROP FUNCTION fn_crypto_payment_attribution_complete();
DROP FUNCTION fn_crypto_assert_payment_attribution(UUID);
CREATE OR REPLACE VIEW crypto_aml_allocatable_payments AS
 SELECT p.id payment_id,p.posted_version payment_version,p.merchant_id,p.network_id,p.token_id,p.wallet_id,
        credit.amount principal,fees.amount service_fee,e.id effect_id,physical.line_id movement_id
 FROM doc_crypto_payments p JOIN reg_crypto_aml_wallet_baselines baseline ON baseline.wallet_id=p.wallet_id AND baseline.token_id=p.token_id AND p.block_number>baseline.block_number
 JOIN cat_wallets w ON w.id=p.wallet_id AND w.custody_merchant_id=p.merchant_id AND w.network_id=p.network_id
 JOIN cat_tokens token ON token.id=p.token_id AND token.network_id=p.network_id
 JOIN reg_crypto_aml_effects e ON e.wallet_id=p.wallet_id AND e.token_id=p.token_id AND e.network_id=p.network_id
   AND e.tx_hash=p.tx_hash AND e.event_kind=p.event_kind AND e.event_index=p.event_index AND e.block_hash=p.block_hash AND e.amount=p.amount AND e.source_wallet_id IS NULL
 JOIN reg_crypto_aml_effect_chain_states cs ON cs.effect_id=e.id AND cs.canonicality='canonical'
 JOIN reg_crypto_transfer_verifications proof ON proof.id=p.verification_id AND proof.network_id=e.network_id AND proof.tx_hash=e.tx_hash
   AND proof.event_kind=e.event_kind AND proof.event_index=e.event_index AND proof.block_hash=e.block_hash AND proof.economics_hash=e.economics_hash
 JOIN LATERAL(SELECT COALESCE(SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END),0) amount,
    COALESCE(SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END) FILTER(WHERE recorder_version=p.posted_version),0) current_amount
    FROM reg_crypto_merchant_balance_movements WHERE recorder_id=p.id AND recorder_type='CryptoPayment' AND merchant_id=p.merchant_id AND token_id=p.token_id) credit ON TRUE
 JOIN LATERAL(SELECT COALESCE(SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END),0) amount,
    COALESCE(SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END) FILTER(WHERE recorder_version=p.posted_version),0) current_amount
    FROM reg_crypto_fee_movements WHERE recorder_id=p.id AND recorder_type='CryptoPayment' AND merchant_id=p.merchant_id AND token_id=p.token_id AND fee_type='processing') fees ON TRUE
 JOIN reg_crypto_balance_movements physical ON physical.recorder_id=p.id AND physical.recorder_type='CryptoPayment' AND physical.recorder_version=p.posted_version
    AND physical.wallet_id=p.wallet_id AND physical.token_id=p.token_id AND physical.record_type='receipt' AND physical.amount=p.amount AND physical.created_at>=baseline.created_at
 WHERE p.posted AND p.status='settled' AND p.canonicality='canonical'
   AND credit.amount>=0 AND fees.amount>=0 AND credit.amount=credit.current_amount AND fees.amount=fees.current_amount AND credit.amount+fees.amount=p.amount
   AND (SELECT COUNT(*) FROM reg_crypto_balance_movements m WHERE m.recorder_id=p.id AND m.recorder_type='CryptoPayment' AND m.recorder_version=p.posted_version AND m.wallet_id=p.wallet_id AND m.token_id=p.token_id)=1;

ALTER TABLE doc_crypto_payments DROP COLUMN receipt_id;DELETE FROM auth_role_permissions WHERE permission_id IN (SELECT id FROM auth_permissions WHERE resource='crypto_receipt');
DELETE FROM auth_permissions WHERE resource='crypto_receipt';DROP TRIGGER trg_crypto_custody_receipt_audit_seed ON doc_crypto_custody_receipts;
DROP TABLE reg_crypto_custody_receipt_audits;
DROP FUNCTION fn_crypto_custody_receipt_audit_guard();
DROP FUNCTION fn_crypto_custody_receipt_audit_seed();DO $$
DECLARE target TEXT;
BEGIN
    FOREACH target IN ARRAY ARRAY['reg_crypto_receipt_suspense_movements','reg_crypto_merchant_balance_movements',
        'reg_crypto_fee_movements','reg_crypto_balance_movements'] LOOP
        EXECUTE format('DROP TRIGGER %I ON %I','trg_merchant_credit_complete_'||target,target);
    END LOOP;
END;
$$;
DROP TABLE doc_crypto_merchant_credits;
DROP TABLE sys_crypto_receipt_credit_policies;
DROP FUNCTION fn_crypto_merchant_credit_complete();
DROP FUNCTION fn_crypto_assert_merchant_credit(UUID);
DROP FUNCTION fn_crypto_merchant_credit_guard();ALTER TABLE reg_crypto_inbound_chain_events DROP COLUMN receipt_error, DROP COLUMN receipt_attempts, DROP COLUMN receipt_check_at, DROP COLUMN receipt_id;-- ==== crypto_external_outflow_decisions ====
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM reg_crypto_external_outflows WHERE classification_basis IS NOT NULL OR resolution_basis IS NOT NULL)
 OR EXISTS(SELECT 1 FROM doc_crypto_custody_adjustments WHERE decision_basis IS NOT NULL)
 OR EXISTS(SELECT 1 FROM reg_crypto_external_outflow_actions WHERE decision_basis IS NOT NULL) THEN
  RAISE EXCEPTION 'outflow decision history requires forward recovery' USING ERRCODE='23514';
 END IF;
END $$;
DROP TRIGGER trg_crypto_outflow_decision_complete ON reg_crypto_external_outflows;
DROP TRIGGER trg_crypto_custody_decision_complete ON doc_crypto_custody_adjustments;
DROP FUNCTION fn_crypto_outflow_decision_audit_complete();
DROP TRIGGER trg_crypto_custody_decision_audit ON doc_crypto_custody_adjustments;
DROP TRIGGER trg_crypto_outflow_decision_audit ON reg_crypto_external_outflows;
DROP FUNCTION fn_crypto_outflow_append_decision_audit();
DROP TRIGGER trg_crypto_outflow_decision_audit_insert ON reg_crypto_external_outflow_actions;
DROP FUNCTION fn_crypto_outflow_decision_audit_guard();
DROP TRIGGER trg_crypto_custody_decision ON doc_crypto_custody_adjustments;
DROP FUNCTION fn_crypto_custody_decision_guard();
DROP TRIGGER trg_crypto_external_outflow_decision ON reg_crypto_external_outflows;
DROP FUNCTION fn_crypto_outflow_decision_guard();
DROP FUNCTION fn_crypto_outflow_decision_source(UUID);
DROP FUNCTION fn_crypto_outflow_decision_seed(JSONB);
DROP FUNCTION fn_crypto_outflow_decision_command(TEXT,UUID,UUID,TEXT,TEXT,TEXT,UUID,JSONB);
DROP TRIGGER trg_crypto_outflow_initial_decision ON reg_crypto_external_outflows;
DROP TRIGGER trg_crypto_custody_initial_decision ON doc_crypto_custody_adjustments;
DROP FUNCTION fn_crypto_outflow_decision_insert_guard();
-- Keep additive nullable audit columns.
-- ==== crypto_external_outflow_basis ====
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM reg_crypto_external_outflows WHERE initial_basis IS NOT NULL)
   OR EXISTS(SELECT 1 FROM reg_crypto_external_outflow_observations WHERE incident_id IS NOT NULL) THEN
  RAISE EXCEPTION 'external outflow source history requires forward recovery' USING ERRCODE='23514';
 END IF;
END $$;
DROP TRIGGER trg_crypto_external_outflow_observation_source ON reg_crypto_external_outflow_observations;
DROP FUNCTION fn_crypto_external_outflow_observation_source();
DROP TRIGGER trg_crypto_external_outflow_basis ON reg_crypto_external_outflows;
DROP FUNCTION fn_crypto_external_outflow_basis_guard();
DROP FUNCTION fn_crypto_try_lock_external_outflow(UUID,TEXT,UUID);
-- Keep nullable source columns; a later Up validates retained history.
-- ==== crypto_ingestion_registration_basis ====
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM sys_crypto_block_ingestion WHERE activation_basis IS NOT NULL) THEN
  RAISE EXCEPTION 'financial ingestion boundary history requires forward recovery' USING ERRCODE='23514';
 END IF;
END $$;
DROP TRIGGER trg_crypto_watched_wallet_identity ON cat_wallets;
DROP TRIGGER trg_crypto_watched_asset_identity ON cat_tokens;
DROP FUNCTION fn_crypto_watched_catalog_guard();
DROP TRIGGER trg_crypto_watch_target_guard ON sys_crypto_watch_targets;
DROP FUNCTION fn_crypto_watch_target_guard();
DROP TRIGGER trg_crypto_block_ingestion_basis ON sys_crypto_block_ingestion;
DROP FUNCTION fn_crypto_ingestion_basis_guard();
CREATE OR REPLACE FUNCTION fn_crypto_register_watch_address() RETURNS TRIGGER AS $$
DECLARE boundary BIGINT;
BEGIN
 IF NEW.vault_key_id='' OR NEW.address='' OR NOT EXISTS(SELECT 1 FROM cat_blockchain_networks WHERE id=NEW.network_id AND chain_family='tron') THEN RETURN NEW; END IF;
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
 SELECT start_height+1 INTO boundary FROM sys_crypto_block_ingestion WHERE network_id=NEW.network_id FOR UPDATE;
 IF boundary IS NULL THEN boundary:=0; END IF;
 SELECT GREATEST(boundary,COALESCE(MAX(last_block)+1,boundary)) INTO boundary FROM sys_chain_watcher_state WHERE network_id=NEW.network_id AND watcher_key='tron:solidified';
 INSERT INTO sys_crypto_watch_targets(network_id,target_kind,target_key,owner_id,start_height,source) VALUES(NEW.network_id,'asset',NEW.contract_address,NEW.id,boundary,'asset_write') ON CONFLICT(network_id,target_kind,target_key) DO NOTHING;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP FUNCTION fn_crypto_try_lock_watch_network(UUID);
-- Both basis columns deliberately remain. Earlier binaries ignore them;
-- the next Up checks any retained history against the current projection.
-- ==== crypto_preflight_attempt_basis ====
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM reg_crypto_aml_preflight_attempts) OR EXISTS(SELECT 1 FROM reg_crypto_aml_preflight_jobs WHERE initial_policy_version IS NOT NULL OR initial_scope_version IS NOT NULL) THEN
  RAISE EXCEPTION 'preflight attempt history requires forward recovery' USING ERRCODE='23514';
 END IF;
END $$;
ALTER TABLE reg_crypto_aml_preflight_observations DROP CONSTRAINT fk_crypto_preflight_observation_attempt;
DROP TRIGGER trg_crypto_aml_preflight_attempt_observation ON reg_crypto_aml_preflight_observations;
DROP FUNCTION fn_crypto_aml_preflight_attempt_observation_guard();
DROP FUNCTION fn_crypto_aml_preflight_attempt_active(UUID,UUID,BOOLEAN);
DROP TRIGGER trg_crypto_aml_preflight_claim_committed ON reg_crypto_aml_preflight_jobs;
DROP FUNCTION fn_crypto_aml_preflight_claim_committed();
DROP TRIGGER trg_crypto_aml_preflight_job_identity ON reg_crypto_aml_preflight_jobs;
DROP FUNCTION fn_crypto_aml_preflight_job_guard();
DROP TRIGGER trg_crypto_aml_preflight_attempt_guard ON reg_crypto_aml_preflight_attempts;
DROP FUNCTION fn_crypto_aml_preflight_attempt_guard();
DROP TABLE reg_crypto_aml_preflight_attempts;
DROP FUNCTION fn_crypto_aml_preflight_basis(UUID);
ALTER TABLE reg_crypto_aml_preflight_jobs DROP COLUMN initial_policy_version,DROP COLUMN initial_scope_version;
-- ==== crypto_decision_and_invoice_drift_history ====
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM sys_invoice_reconciliation_drift WHERE episode_id IS NOT NULL OR initial_details IS NOT NULL OR incident_id IS NOT NULL)
    OR EXISTS(SELECT 1 FROM reg_crypto_security_incidents WHERE incident_kind='invoice_projection_drift' AND details ? 'firstDetails') THEN
  RAISE EXCEPTION 'invoice drift episodes require forward recovery' USING ERRCODE='23514';
 END IF;
END $$;
DROP TRIGGER trg_crypto_invoice_drift_incident_guard ON reg_crypto_security_incidents;
DROP FUNCTION fn_crypto_invoice_drift_incident_guard();
DROP TRIGGER trg_crypto_invoice_drift_guard ON sys_invoice_reconciliation_drift;
DROP FUNCTION fn_crypto_invoice_drift_guard();
DROP FUNCTION fn_crypto_invoice_drift_details(UUID);
DROP FUNCTION fn_crypto_lock_invoice_drift(UUID);
ALTER TABLE sys_invoice_reconciliation_drift DROP COLUMN incident_id,DROP COLUMN initial_details,DROP COLUMN episode_id;
DROP TRIGGER trg_crypto_aml_decision_replay ON reg_crypto_aml_decisions;
DROP FUNCTION fn_crypto_aml_decision_replay_guard();
-- evaluated_at deliberately remains: old binaries ignore it, Up reuses it.
-- ==== crypto_pool_cancel_and_watch_evidence ====
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
-- No evidence is transformed by this migration in either direction.
CREATE OR REPLACE FUNCTION fn_crypto_aml_pool_cancel_guard() RETURNS TRIGGER AS $$
BEGIN
 PERFORM 1 FROM reg_crypto_aml_pool_intents i JOIN sys_crypto_aml_pool_policies p ON p.id=i.policy_id JOIN sys_crypto_custody_policies c ON c.network_id=p.network_id WHERE i.id=NEW.intent_id FOR UPDATE OF c;
 PERFORM 1 FROM reg_crypto_aml_pool_intents WHERE id=NEW.intent_id FOR UPDATE;
 IF EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_provisions WHERE id=NEW.intent_id) THEN RAISE EXCEPTION 'cannot cancel pool intent after derivation reservation' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE OR REPLACE FUNCTION fn_crypto_aml_watch_coverage_guard() RETURNS TRIGGER AS $$
BEGIN
 PERFORM 1 FROM sys_chain_watcher_state state JOIN cat_wallets w ON w.id=NEW.wallet_id AND w.network_id=state.network_id
 JOIN cat_tokens t ON t.id=NEW.token_id AND t.network_id=w.network_id JOIN cat_blockchain_networks n ON n.id=w.network_id AND n.chain_family='evm'
 WHERE state.network_id=NEW.network_id AND state.watcher_key=NEW.watcher_key AND state.lease_owner=NEW.lease_owner AND state.fencing_token=NEW.fencing_token AND state.lease_expires_at>NOW()
 AND NEW.watcher_key=CASE WHEN t.token_standard='native' THEN 'native_eth' ELSE 'erc20:'||LOWER(t.contract_address) END FOR SHARE OF state;
 IF NOT FOUND THEN RAISE EXCEPTION 'address coverage requires the current watcher lease' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- ==== crypto_sql_hold_replay ====
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM reg_crypto_wallet_allocation_holds WHERE owner_type IN ('aml_pool_scope_baseline','aml_pool_risk','aml_preflight') AND owner_id LIKE '%:%') THEN
  RAISE EXCEPTION 'SQL hold lifecycle requires forward recovery' USING ERRCODE='23514';
 END IF;
END; $$;
DROP TRIGGER trg_crypto_network_hold_insert_replay ON reg_crypto_network_holds;
DROP TRIGGER trg_crypto_wallet_hold_insert_replay ON reg_crypto_wallet_allocation_holds;
DROP TRIGGER trg_crypto_network_hold_owner_guard ON reg_crypto_network_holds;
DROP TRIGGER trg_crypto_wallet_hold_owner_guard ON reg_crypto_wallet_allocation_holds;
CREATE TRIGGER trg_crypto_network_hold_owner_guard BEFORE UPDATE OR DELETE ON reg_crypto_network_holds FOR EACH ROW EXECUTE FUNCTION fn_crypto_hold_owner_guard();
CREATE TRIGGER trg_crypto_wallet_hold_owner_guard BEFORE UPDATE OR DELETE ON reg_crypto_wallet_allocation_holds FOR EACH ROW EXECUTE FUNCTION fn_crypto_wallet_hold_owner_guard();
-- Original pool trigger implementations are restored below.

CREATE OR REPLACE FUNCTION fn_crypto_aml_pool_scope_hold() RETURNS TRIGGER AS $$
BEGIN
 IF NEW.enabled THEN
   INSERT INTO reg_crypto_wallet_allocation_holds(wallet_id,reason,owner_type,owner_id,details,held_by)
     SELECT v.wallet_id,'aml_scope_baseline_missing','aml_pool_scope_baseline',NEW.token_id::text,jsonb_build_object('scopeVersion',NEW.version,'tokenId',NEW.token_id::text),'aml-pool-controller'
     FROM reg_crypto_aml_pool_intents i JOIN sys_crypto_aml_pool_policies p ON p.id=i.policy_id AND p.wallet_role='merchant_pool'
     JOIN reg_crypto_aml_wallet_provisions v ON v.id=i.id AND v.status='completed'
     WHERE p.merchant_id=NEW.merchant_id AND p.network_id=NEW.network_id
       AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_baselines WHERE wallet_id=v.wallet_id AND token_id=NEW.token_id)
     ON CONFLICT(wallet_id,owner_type,owner_id) WHERE status='open' DO NOTHING;
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_crypto_aml_pool_high_hold() RETURNS TRIGGER AS $$
BEGIN
 IF NEW.risk_class='high' THEN
   INSERT INTO reg_crypto_wallet_allocation_holds(wallet_id,reason,owner_type,owner_id,details,held_by)
     SELECT v.wallet_id,'aml_pool_high_risk','aml_pool_risk',NEW.token_id::text,jsonb_build_object('tokenId',NEW.token_id::text,'riskRevision',NEW.revision),'aml-pool-controller'
     FROM reg_crypto_aml_wallet_provisions v JOIN reg_crypto_aml_pool_intents i ON i.id=v.id
     JOIN sys_crypto_aml_pool_policies p ON p.id=i.policy_id AND p.wallet_role='merchant_pool'
     WHERE v.wallet_id=NEW.wallet_id AND v.status='completed'
     ON CONFLICT(wallet_id,owner_type,owner_id) WHERE status='open' DO NOTHING;
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP FUNCTION fn_crypto_hold_aml_preflight(UUID,UUID,UUID);
DROP FUNCTION fn_crypto_hold_aml_pool_wallet(UUID,UUID,TEXT,BIGINT);
DROP FUNCTION fn_crypto_sql_hold_guard();
DROP FUNCTION fn_crypto_try_lock_operational_hold_owner(TEXT,TEXT);
DROP FUNCTION fn_crypto_hold_owner_key(TEXT,TEXT);
-- ==== crypto_reorg_compensation_basis ====
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM reg_crypto_reorg_compensations WHERE request_basis IS NOT NULL) THEN
  RAISE EXCEPTION 'financial reorg request basis requires forward recovery' USING ERRCODE='23514';
 END IF;
END; $$;
DROP TRIGGER trg_crypto_reorg_compensation_immutable ON reg_crypto_reorg_compensations;
DROP FUNCTION fn_crypto_reorg_compensation_immutable();
ALTER TABLE reg_crypto_reorg_compensations DROP COLUMN request_basis;
-- ==== crypto_activation_execution_dispatch ====
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM reg_crypto_activation_dispatches) THEN
  RAISE EXCEPTION 'activation dispatch history requires forward recovery' USING ERRCODE='23514';
 END IF;
END; $$;
DROP TABLE reg_crypto_activation_dispatches;
DROP FUNCTION fn_crypto_activation_dispatch_guard();
-- ==== crypto_activation_cost_claim_fencing ====
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE reg_crypto_account_activations DROP CONSTRAINT chk_crypto_activation_cost_claim;
ALTER TABLE reg_crypto_account_activations DROP COLUMN cost_claim_token;
-- ==== crypto_resource_rental_claim_fencing ====
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE doc_crypto_resource_rentals DROP CONSTRAINT chk_crypto_resource_rental_claim_token;
ALTER TABLE doc_crypto_resource_rentals DROP COLUMN maintenance_claim_token;
-- ==== crypto_activation_task_identity ====
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TRIGGER trg_crypto_activation_identity ON reg_crypto_account_activations;
DROP FUNCTION fn_crypto_activation_identity_guard();
DROP FUNCTION fn_crypto_activation_identity_valid(UUID,UUID,UUID,UUID);
-- ==== crypto_reconciliation_request_identity ====
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TRIGGER trg_crypto_reconciliation_request_identity ON reg_crypto_reconciliation_requests;
DROP FUNCTION fn_crypto_reconciliation_request_identity_guard();
DROP FUNCTION fn_crypto_reconciliation_request_identity_valid(UUID,UUID,UUID,TEXT,UUID);
-- ==== crypto_funding_confirmation_locks ====
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP FUNCTION fn_crypto_try_lock_funding_confirmation(UUID);
-- ==== crypto_selector_resource_locks ====
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP FUNCTION fn_crypto_lock_withdrawal_wallets(UUID[],UUID,UUID);
DROP FUNCTION fn_crypto_try_lock_custody_wallets(UUID[],UUID,UUID);
-- ==== crypto_accounting_resource_locks ====
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
-- Lock-only migration: no financial row, ownership or historical byte changes.
CREATE OR REPLACE FUNCTION fn_crypto_aml_lock_resource_scopes(reservation UUID) RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
 PERFORM 1 FROM reg_crypto_fee_reservations fee JOIN cat_wallets w ON w.id=fee.payer_wallet_id
   JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=w.custody_merchant_id AND s.network_id=fee.network_id
   WHERE fee.id=reservation AND (EXISTS(SELECT 1 FROM reg_crypto_aml_resource_budgets b WHERE b.fee_reservation_id=fee.id)
      OR EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes native WHERE native.merchant_id=w.custody_merchant_id AND native.network_id=fee.network_id AND native.token_id=fee.native_token_id AND native.enabled))
   ORDER BY s.merchant_id,s.network_id,s.token_id FOR UPDATE OF s;
END;
$$;
CREATE OR REPLACE FUNCTION fn_crypto_aml_lock_fee_wallet_scopes(payer UUID,asset UUID) RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
 PERFORM 1 FROM cat_wallets w JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=w.custody_merchant_id AND s.network_id=w.network_id
 WHERE w.id=payer AND EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes native WHERE native.merchant_id=w.custody_merchant_id AND native.network_id=w.network_id AND native.token_id=asset AND native.enabled)
 ORDER BY s.merchant_id,s.network_id,s.token_id FOR UPDATE OF s;
END;
$$;
DROP FUNCTION fn_crypto_lock_custody_wallets(UUID[]);
DROP FUNCTION fn_crypto_lock_custody_scopes(UUID[]);
-- ==== crypto_adjustment_accounting_basis ====
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
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
    DROP COLUMN basis_kind,DROP COLUMN proposal_txid;
ALTER TABLE reg_crypto_adjustment_sources
    DROP CONSTRAINT reg_crypto_adjustment_sources_source_register_check,
    ADD CONSTRAINT reg_crypto_adjustment_sources_source_register_check CHECK(source_register IN ('custody','merchant','fee'));
-- ==== crypto_expected_transfer_routing ====
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN IF EXISTS(SELECT 1 FROM reg_crypto_expected_transfer_requests) OR EXISTS(SELECT 1 FROM reg_crypto_expected_transfer_routes) THEN
 RAISE EXCEPTION 'expected transfer routing requires forward migration'; END IF; END; $$;
DROP TRIGGER trg_expected_payment ON doc_crypto_payments;
DROP FUNCTION fn_crypto_expected_external_payment_guard();
DROP TRIGGER trg_expected_posting_owner ON reg_crypto_custody_effect_postings;
DROP FUNCTION fn_crypto_expected_posting_owner_guard();
DROP FUNCTION fn_crypto_expected_request_complete() CASCADE;
DROP TABLE sys_crypto_expected_transfer_deliveries;
DROP TABLE reg_crypto_expected_transfer_evidence;
DROP TABLE reg_crypto_expected_transfer_routes;
DROP TABLE reg_crypto_expected_transfer_requests;
DROP FUNCTION fn_crypto_expected_transfer_guard();
DROP FUNCTION fn_crypto_validate_expected_route(UUID,UUID);
DROP FUNCTION fn_crypto_expected_owner_matches(UUID,TEXT,UUID);
DROP INDEX idx_expected_transfer_canonical_tx;
DROP FUNCTION fn_crypto_expected_transfer_candidates(UUID,TEXT,TEXT,TEXT,TEXT,TEXT,NUMERIC);
-- ==== crypto_network_fee_recognition ====
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TRIGGER trg_network_fee_transition_txid ON reg_crypto_fee_reservation_events;
DROP FUNCTION fn_crypto_fee_transition_transaction();
ALTER TABLE reg_crypto_fee_reservation_events DROP COLUMN transition_txid;
DROP TRIGGER trg_network_fee_replacement_complete ON reg_crypto_fee_reservations;
DROP FUNCTION fn_crypto_network_fee_replacement_complete();
DROP TRIGGER trg_network_fee_receipt_owner ON reg_crypto_chain_settlements;
DROP TRIGGER trg_network_fee_movement_owner ON reg_crypto_balance_movements;
DROP TRIGGER trg_network_fee_basis ON reg_crypto_fee_reservations;
DROP TRIGGER trg_network_fee_document_basis ON doc_crypto_network_operations;
DROP TRIGGER trg_network_fee_reservation_complete ON reg_crypto_fee_reservations;
DROP TRIGGER trg_network_fee_settlement_complete ON reg_crypto_chain_settlements;
DROP TRIGGER trg_network_fee_component_complete ON reg_crypto_chain_settlement_components;
DROP TRIGGER trg_network_fee_document_complete ON doc_crypto_network_operations;
DO $$ DECLARE target TEXT; BEGIN
    FOREACH target IN ARRAY ARRAY['reg_crypto_balance_movements','reg_crypto_platform_cost_movements','reg_crypto_merchant_balance_movements','reg_crypto_fee_movements','reg_crypto_provider_balance_movements'] LOOP
        EXECUTE format('DROP TRIGGER %I ON %I','trg_network_fee_complete_'||target,target);
    END LOOP;
END; $$;
DROP TABLE reg_crypto_network_fee_bindings;
DROP TABLE reg_crypto_network_fee_proofs;
DROP FUNCTION fn_crypto_network_fee_legacy_owner_guard();
DROP FUNCTION fn_crypto_network_fee_basis_guard();
DROP FUNCTION fn_crypto_network_fee_financial_complete();
DROP FUNCTION fn_crypto_network_fee_state_complete();
DROP FUNCTION fn_crypto_network_fee_binding_complete();
DROP FUNCTION fn_crypto_assert_network_fee(UUID);
DROP FUNCTION fn_crypto_network_fee_proof_guard();
DROP FUNCTION fn_crypto_network_fee_key(TEXT,UUID,TEXT);
ALTER TABLE doc_crypto_network_operations DROP COLUMN network_fee_reservation_id;
ALTER TABLE reg_crypto_chain_settlements DROP COLUMN network_fee_reservation_id;
-- ==== crypto_custody_loss_recognition ====
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
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
-- ==== crypto_withdrawal_final_custody_recognition ====
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
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
-- ==== crypto_treasury_final_custody_recognition ====
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
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
-- ==== crypto_sweep_final_custody_recognition ====
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
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
DROP FUNCTION fn_crypto_sweep_basis_guard();
DROP FUNCTION fn_crypto_sweep_settlement_complete();
DROP FUNCTION fn_crypto_sweep_financial_complete();
DROP FUNCTION fn_crypto_sweep_posting_complete();
DROP FUNCTION fn_crypto_sweep_binding_complete();
DROP FUNCTION fn_crypto_assert_sweep_line(UUID);
-- ==== crypto_native_funding_custody_recognition ====
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
DROP FUNCTION fn_crypto_payment_native_funding_guard();
DROP FUNCTION fn_crypto_native_funding_document_complete();
DROP FUNCTION fn_crypto_native_funding_basis_guard();
DROP FUNCTION fn_crypto_native_funding_movement_complete();
DROP FUNCTION fn_crypto_native_funding_custody_complete();
DROP FUNCTION fn_crypto_native_funding_settlement_complete();
DROP FUNCTION fn_crypto_assert_native_funding_custody(UUID);
-- ==== crypto_payment_custody_recognition ====
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TRIGGER trg_crypto_payment_frozen_basis ON doc_crypto_payments;
DROP TRIGGER trg_crypto_balance_posting_transaction ON reg_crypto_balance_movements;
ALTER TABLE reg_crypto_balance_movements DROP COLUMN posting_txid;
DROP FUNCTION fn_crypto_payment_frozen_basis_guard();
DROP FUNCTION fn_crypto_balance_posting_transaction();
-- ==== crypto_wallet_funding_recognition ====
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$
BEGIN
    IF EXISTS(SELECT 1 FROM reg_crypto_wallet_fundings WHERE posting_id IS NOT NULL)
       OR EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings WHERE recorder_type='CryptoWalletFunding') THEN
        RAISE EXCEPTION 'recognized wallet funding requires forward migration';
    END IF;
END;
$$;
DROP TRIGGER trg_crypto_wallet_funding_movement_guard ON reg_crypto_balance_movements;
DROP TRIGGER trg_crypto_wallet_funding_movement_complete ON reg_crypto_balance_movements;
DROP TRIGGER trg_crypto_wallet_funding_posting_complete ON reg_crypto_custody_effect_postings;
DROP TRIGGER trg_crypto_wallet_funding_payment_guard ON doc_crypto_payments;
DROP TRIGGER trg_crypto_wallet_funding_admission ON reg_crypto_wallet_fundings;
DROP TRIGGER trg_crypto_wallet_funding_complete ON reg_crypto_wallet_fundings;
DROP TRIGGER trg_no_funding_reg_crypto_merchant_balance_movements ON reg_crypto_merchant_balance_movements;
DROP TRIGGER trg_no_funding_reg_crypto_fee_movements ON reg_crypto_fee_movements;
DROP TRIGGER trg_no_funding_reg_crypto_platform_cost_movements ON reg_crypto_platform_cost_movements;
DROP TRIGGER trg_no_funding_reg_crypto_provider_balance_movements ON reg_crypto_provider_balance_movements;
DROP TABLE reg_crypto_wallet_funding_audit;
DROP INDEX uq_crypto_wallet_fundings_legacy_tx;
DROP INDEX idx_crypto_wallet_fundings_tx;
ALTER TABLE reg_crypto_wallet_fundings DROP CONSTRAINT ck_crypto_wallet_funding_recognition_pair,
    DROP COLUMN posting_id, DROP COLUMN occurrence_id,
    ADD CONSTRAINT uq_crypto_wallet_fundings_tx_hash UNIQUE(tx_hash);
DROP FUNCTION fn_crypto_wallet_funding_no_attribution();
DROP FUNCTION fn_crypto_wallet_funding_movement_guard();
DROP FUNCTION fn_crypto_wallet_funding_complete();
DROP FUNCTION fn_crypto_assert_wallet_funding(UUID);
DROP FUNCTION fn_crypto_wallet_funding_payment_guard();
DROP FUNCTION fn_crypto_wallet_funding_payment_conflict(UUID,UUID,UUID,TEXT,TEXT,UUID);
DROP FUNCTION fn_crypto_wallet_funding_admission();
-- ==== crypto_custody_receipt_suspense ====
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$
BEGIN
    IF EXISTS(SELECT 1 FROM doc_crypto_custody_receipts)
       OR EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings) THEN
        RAISE EXCEPTION 'custody recognition history requires forward migration';
    END IF;
END;
$$;
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
DROP FUNCTION fn_crypto_custody_posting_complete();
DROP FUNCTION fn_crypto_assert_custody_posting(UUID);
DROP FUNCTION fn_crypto_custody_posting_identity_guard();
DROP FUNCTION fn_crypto_receipt_suspense_apply();
DROP FUNCTION fn_crypto_custody_receipt_mutation_guard();
DROP FUNCTION fn_crypto_lock_receipt_balance(UUID,UUID);
DROP FUNCTION fn_crypto_custody_receipt_movement_complete();
DROP FUNCTION fn_crypto_custody_receipt_no_attribution();
-- ==== crypto_accounting_effect_identity ====
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$
BEGIN
    IF EXISTS(SELECT 1 FROM reg_crypto_accounting_effect_evidence) THEN
        RAISE EXCEPTION 'accounting evidence history requires a forward-compatible binary';
    END IF;
END;
$$;
DROP TABLE reg_crypto_accounting_effect_evidence;
DROP TABLE reg_crypto_accounting_effect_occurrences;
DROP TABLE reg_crypto_accounting_effects;
DROP FUNCTION fn_crypto_accounting_coverage_guard();
DROP FUNCTION fn_crypto_accounting_evidence_guard();
DROP FUNCTION fn_crypto_accounting_immutable();
DROP FUNCTION fn_crypto_accounting_identity_bytes(TEXT,UUID,TEXT,TEXT,BIGINT,TEXT);
-- +goose StatementEnd
