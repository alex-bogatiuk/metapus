-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

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
CREATE TRIGGER trg_crypto_wallet_funding_audit_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_wallet_funding_audit
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_financial_movement_immutable();

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
CREATE TRIGGER trg_crypto_wallet_funding_admission BEFORE INSERT ON reg_crypto_wallet_fundings
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_wallet_funding_admission();

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
CREATE TRIGGER trg_crypto_wallet_funding_payment_guard BEFORE INSERT OR UPDATE ON doc_crypto_payments
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_wallet_funding_payment_guard();

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
        RAISE EXCEPTION 'wallet funding overlaps existing accounting; explicit adoption required' USING ERRCODE='23514';
    END IF;
    IF EXISTS(SELECT 1 FROM reg_crypto_balance_movements m
        WHERE m.wallet_id=f.wallet_id AND m.token_id=f.token_id AND m.record_type='receipt'
          AND m.recorder_type IN ('CryptoAdjustment','CryptoCustodyAdjustment') AND m.reversal_of_line_id IS NULL
          AND NOT EXISTS(SELECT 1 FROM reg_crypto_custody_effect_recognitions r WHERE r.movement_line_id=m.line_id)
          AND m.amount>COALESCE((SELECT SUM(x.amount) FROM reg_crypto_balance_movements x WHERE x.reversal_of_line_id=m.line_id),0)) THEN
        RAISE EXCEPTION 'manual custody correction requires explicit funding adoption' USING ERRCODE='23514';
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
CREATE CONSTRAINT TRIGGER trg_crypto_wallet_funding_complete AFTER INSERT ON reg_crypto_wallet_fundings
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_wallet_funding_complete();
CREATE CONSTRAINT TRIGGER trg_crypto_wallet_funding_posting_complete AFTER INSERT ON reg_crypto_custody_effect_postings
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_wallet_funding_complete();
CREATE CONSTRAINT TRIGGER trg_crypto_wallet_funding_movement_complete AFTER INSERT ON reg_crypto_balance_movements
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_wallet_funding_complete();

CREATE FUNCTION fn_crypto_wallet_funding_movement_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE payment doc_crypto_payments; operating reg_crypto_aml_operating_receipts;
BEGIN
    PERFORM fn_crypto_lock_receipt_balance(NEW.wallet_id,NEW.token_id);
    IF NEW.recorder_type='CryptoPayment' THEN
        SELECT * INTO payment FROM doc_crypto_payments WHERE id=NEW.recorder_id;
        IF FOUND AND fn_crypto_wallet_funding_payment_conflict(payment.network_id,NEW.wallet_id,NEW.token_id,
            payment.tx_hash,payment.event_kind,payment.verification_id) THEN
            RAISE EXCEPTION 'payment principal already belongs to wallet funding' USING ERRCODE='23514';
        END IF;
    ELSIF NEW.recorder_type='AMLOperatingReceipt' THEN
        SELECT * INTO operating FROM reg_crypto_aml_operating_receipts WHERE id=NEW.recorder_id;
        IF FOUND AND fn_crypto_wallet_funding_payment_conflict(operating.network_id,NEW.wallet_id,NEW.token_id,
            operating.tx_hash,operating.event_kind,NULL) THEN
            RAISE EXCEPTION 'operating principal already belongs to wallet funding' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_crypto_wallet_funding_movement_guard BEFORE INSERT ON reg_crypto_balance_movements
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_wallet_funding_movement_guard();

CREATE FUNCTION fn_crypto_wallet_funding_no_attribution() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.recorder_type='CryptoWalletFunding' OR EXISTS(SELECT 1 FROM reg_crypto_wallet_fundings WHERE id=NEW.recorder_id AND posting_id IS NOT NULL) THEN
        RAISE EXCEPTION 'wallet funding cannot create attributed financial movements' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
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
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
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
-- +goose StatementEnd
