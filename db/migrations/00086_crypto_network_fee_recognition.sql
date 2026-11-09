-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE reg_crypto_fee_reservations ADD COLUMN accounting_model_version INT NOT NULL DEFAULT 1 CHECK(accounting_model_version IN (1,2));
ALTER TABLE reg_crypto_fee_reservations ALTER COLUMN accounting_model_version SET DEFAULT 2;
ALTER TABLE reg_crypto_fee_reservation_events ADD COLUMN transition_txid BIGINT;
CREATE FUNCTION fn_crypto_fee_transition_transaction() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    NEW.transition_txid:=CASE WHEN EXISTS(SELECT 1 FROM reg_crypto_fee_reservations WHERE id=NEW.reservation_id AND accounting_model_version=2) THEN txid_current() ELSE NULL END;
    RETURN NEW;
END;
$$;
CREATE TRIGGER trg_network_fee_transition_txid BEFORE INSERT ON reg_crypto_fee_reservation_events FOR EACH ROW EXECUTE FUNCTION fn_crypto_fee_transition_transaction();
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
CREATE TRIGGER trg_network_fee_proof_immutable BEFORE UPDATE OR DELETE ON reg_crypto_network_fee_proofs FOR EACH ROW EXECUTE FUNCTION fn_crypto_financial_movement_immutable();
CREATE TRIGGER trg_network_fee_binding_immutable BEFORE UPDATE OR DELETE ON reg_crypto_network_fee_bindings FOR EACH ROW EXECUTE FUNCTION fn_crypto_financial_movement_immutable();

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
CREATE TRIGGER trg_network_fee_proof_profile BEFORE INSERT ON reg_crypto_network_fee_proofs FOR EACH ROW EXECUTE FUNCTION fn_crypto_network_fee_proof_guard();

CREATE OR REPLACE FUNCTION fn_crypto_payment_posting_transaction() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    NEW.posting_txid:=CASE WHEN NEW.recorder_type IN ('CryptoPayment','CryptoNativeFunding','CryptoSweep','TreasuryWithdrawal','CryptoWithdrawal','CryptoCustodyAdjustment','ChainSettlement','CryptoNetworkOperation') AND NEW.reversal_of_line_id IS NULL THEN txid_current() ELSE NULL END;
    RETURN NEW;
END;
$$;
CREATE FUNCTION fn_crypto_assert_network_fee(target UUID) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE f reg_crypto_fee_reservations; b reg_crypto_network_fee_bindings; p reg_crypto_network_fee_proofs; c reg_crypto_chain_settlements; d doc_crypto_network_operations; fee_purpose TEXT;
BEGIN
    SELECT * INTO STRICT f FROM reg_crypto_fee_reservations WHERE id=target;
    IF f.accounting_model_version<>2 THEN RAISE EXCEPTION 'network fee recognition requires model 2' USING ERRCODE='23514'; END IF;
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
           AND w.address_key=p.payer_address_key AND w.vault_key_id<>'' AND t.id=f.native_token_id AND t.token_standard='native' AND COALESCE(t.contract_address,'')='')
       OR EXISTS(SELECT 1 FROM reg_crypto_fee_reservations prior WHERE prior.id<>f.id AND prior.accounting_model_version=1 AND prior.network_id=f.network_id
           AND regexp_replace(lower(prior.tx_hash),'^0x','')=p.tx_hash AND prior.status IN ('broadcast','held','settled')) THEN
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
CREATE CONSTRAINT TRIGGER trg_network_fee_binding_complete AFTER INSERT ON reg_crypto_network_fee_bindings DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_network_fee_binding_complete();
CREATE FUNCTION fn_crypto_network_fee_state_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target UUID;
BEGIN
    IF TG_TABLE_NAME='reg_crypto_fee_reservations' THEN
        IF NEW.accounting_model_version<>2 OR NEW.status<>'settled' THEN RETURN NULL; END IF; target:=NEW.id;
    ELSIF TG_TABLE_NAME='reg_crypto_chain_settlement_components' THEN
        SELECT network_fee_reservation_id INTO target FROM reg_crypto_chain_settlements WHERE id=NEW.settlement_id;
    ELSE target:=NEW.network_fee_reservation_id; END IF;
    IF target IS NOT NULL THEN PERFORM fn_crypto_assert_network_fee(target); END IF; RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER trg_network_fee_reservation_complete AFTER INSERT OR UPDATE ON reg_crypto_fee_reservations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_network_fee_state_complete();
CREATE CONSTRAINT TRIGGER trg_network_fee_settlement_complete AFTER INSERT ON reg_crypto_chain_settlements DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_network_fee_state_complete();
CREATE CONSTRAINT TRIGGER trg_network_fee_component_complete AFTER INSERT ON reg_crypto_chain_settlement_components DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_network_fee_state_complete();
CREATE CONSTRAINT TRIGGER trg_network_fee_document_complete AFTER INSERT OR UPDATE ON doc_crypto_network_operations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_network_fee_state_complete();
CREATE FUNCTION fn_crypto_network_fee_financial_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE target UUID;
BEGIN
    IF NEW.reversal_of_line_id IS NOT NULL AND EXISTS(SELECT 1 FROM reg_crypto_network_fee_bindings WHERE movement_line_id=NEW.reversal_of_line_id OR cost_line_id=NEW.reversal_of_line_id) THEN
        RAISE EXCEPTION 'recognized network fee requires explicit compensation workflow' USING ERRCODE='23514'; END IF;
    IF NEW.recorder_type='ChainSettlement' AND EXISTS(SELECT 1 FROM reg_crypto_fee_reservations WHERE basis_id=NEW.recorder_id AND accounting_model_version=2)
       AND NOT EXISTS(SELECT 1 FROM reg_crypto_network_fee_bindings WHERE recorder_type=NEW.recorder_type AND recorder_id=NEW.recorder_id AND (movement_line_id=NEW.line_id OR cost_line_id=NEW.line_id)) THEN
        RAISE EXCEPTION 'network fee movement has no exact recognition owner' USING ERRCODE='23514'; END IF;
    FOR target IN SELECT b.reservation_id FROM reg_crypto_network_fee_bindings b WHERE (b.recorder_id=NEW.recorder_id AND b.recorder_type=NEW.recorder_type) OR b.movement_line_id=NEW.line_id OR b.cost_line_id=NEW.line_id
        UNION SELECT f.id FROM reg_crypto_fee_reservations f WHERE f.id=NEW.line_id AND f.accounting_model_version=2 LOOP
        PERFORM fn_crypto_assert_network_fee(target);
    END LOOP; RETURN NULL;
END;
$$;
DO $$ DECLARE target TEXT; BEGIN
    FOREACH target IN ARRAY ARRAY['reg_crypto_balance_movements','reg_crypto_platform_cost_movements','reg_crypto_merchant_balance_movements','reg_crypto_fee_movements','reg_crypto_provider_balance_movements'] LOOP
        EXECUTE format('CREATE CONSTRAINT TRIGGER %I AFTER INSERT ON %I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_network_fee_financial_complete()','trg_network_fee_complete_'||target,target);
    END LOOP;
END; $$;
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
        IF OLD.accounting_model_version=2 THEN RAISE EXCEPTION 'network fee reservation history is immutable' USING ERRCODE='23514'; END IF; RETURN OLD;
    END IF;
    IF NEW.accounting_model_version<>OLD.accounting_model_version THEN RAISE EXCEPTION 'network fee accounting model is immutable' USING ERRCODE='23514'; END IF;
    IF OLD.accounting_model_version<>2 THEN RETURN NEW; END IF;
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
CREATE TRIGGER trg_network_fee_basis BEFORE UPDATE OR DELETE ON reg_crypto_fee_reservations FOR EACH ROW EXECUTE FUNCTION fn_crypto_network_fee_basis_guard();
CREATE TRIGGER trg_network_fee_document_basis BEFORE UPDATE OR DELETE ON doc_crypto_network_operations FOR EACH ROW EXECUTE FUNCTION fn_crypto_network_fee_basis_guard();

CREATE FUNCTION fn_crypto_network_fee_replacement_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.accounting_model_version=2 AND ((OLD.tx_hash<>'' AND NEW.tx_hash<>OLD.tx_hash) OR (OLD.unsigned_fingerprint<>'' AND NEW.unsigned_fingerprint<>OLD.unsigned_fingerprint))
       AND NOT EXISTS(SELECT 1 FROM reg_crypto_fee_reservation_events e WHERE e.reservation_id=NEW.id AND e.transition_txid=txid_current()
           AND e.metadata->>'oldTxHash'=OLD.tx_hash AND e.metadata->>'newTxHash'=NEW.tx_hash
           AND e.metadata->>'oldFingerprint'=OLD.unsigned_fingerprint AND e.metadata->>'newFingerprint'=NEW.unsigned_fingerprint) THEN
        RAISE EXCEPTION 'network fee replacement requires an immutable transition event in this transaction' USING ERRCODE='23514'; END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER trg_network_fee_replacement_complete AFTER UPDATE ON reg_crypto_fee_reservations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_network_fee_replacement_complete();

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
CREATE TRIGGER trg_network_fee_receipt_owner BEFORE INSERT ON reg_crypto_chain_settlements FOR EACH ROW EXECUTE FUNCTION fn_crypto_network_fee_legacy_owner_guard();
CREATE TRIGGER trg_network_fee_movement_owner BEFORE INSERT ON reg_crypto_balance_movements FOR EACH ROW EXECUTE FUNCTION fn_crypto_network_fee_legacy_owner_guard();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN IF EXISTS(SELECT 1 FROM reg_crypto_fee_reservations WHERE accounting_model_version=2)
    OR EXISTS(SELECT 1 FROM reg_crypto_network_fee_proofs) OR EXISTS(SELECT 1 FROM reg_crypto_network_fee_bindings)
    THEN RAISE EXCEPTION 'network fee recognition v2 requires forward migration'; END IF; END; $$;
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
ALTER TABLE reg_crypto_fee_reservations DROP COLUMN accounting_model_version;
CREATE OR REPLACE FUNCTION fn_crypto_payment_posting_transaction() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    NEW.posting_txid:=CASE WHEN NEW.recorder_type IN ('CryptoPayment','CryptoNativeFunding','CryptoSweep','TreasuryWithdrawal','CryptoWithdrawal','CryptoCustodyAdjustment') AND NEW.reversal_of_line_id IS NULL THEN txid_current() ELSE NULL END;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
