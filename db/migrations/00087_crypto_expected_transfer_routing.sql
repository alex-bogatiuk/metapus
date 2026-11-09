-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

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
CREATE TRIGGER trg_expected_request_basis BEFORE INSERT ON reg_crypto_expected_transfer_requests FOR EACH ROW EXECUTE FUNCTION fn_crypto_expected_transfer_guard();
CREATE TRIGGER trg_expected_route_basis BEFORE INSERT ON reg_crypto_expected_transfer_routes FOR EACH ROW EXECUTE FUNCTION fn_crypto_expected_transfer_guard();
CREATE TRIGGER trg_expected_route_evidence_basis BEFORE INSERT ON reg_crypto_expected_transfer_evidence FOR EACH ROW EXECUTE FUNCTION fn_crypto_expected_transfer_guard();
DO $$ DECLARE target TEXT; BEGIN
 FOREACH target IN ARRAY ARRAY['reg_crypto_expected_transfer_requests','reg_crypto_expected_transfer_routes','reg_crypto_expected_transfer_evidence'] LOOP
  EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION fn_crypto_financial_movement_immutable()','trg_immutable_'||target,target);
 END LOOP; END; $$;
CREATE FUNCTION fn_crypto_expected_request_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF (SELECT count(*) FROM sys_outbox WHERE aggregate_type='CryptoExpectedTransfer' AND aggregate_id=NEW.expected_effect_id AND event_type='settlement_requested')<>1
 OR (SELECT count(*) FROM sys_outbox WHERE aggregate_type='CryptoExpectedTransfer' AND aggregate_id=NEW.expected_effect_id AND event_type='settlement_requested'
    AND payload->>'ownerType'=NEW.owner_type AND payload->>'ownerId'=NEW.owner_id::text AND payload->>'expectedEffectId'=NEW.expected_effect_id::text)<>1 THEN
  RAISE EXCEPTION 'expected settlement request requires its original-owner outbox' USING ERRCODE='23514'; END IF; RETURN NULL;
END; $$;
CREATE CONSTRAINT TRIGGER trg_expected_request_complete AFTER INSERT ON reg_crypto_expected_transfer_requests DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_expected_request_complete();

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
CREATE TRIGGER trg_expected_posting_owner BEFORE INSERT ON reg_crypto_custody_effect_postings FOR EACH ROW EXECUTE FUNCTION fn_crypto_expected_posting_owner_guard();
CREATE FUNCTION fn_crypto_expected_external_payment_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE p doc_crypto_payments; blocked BOOLEAN;
BEGIN
 IF TG_TABLE_NAME='doc_crypto_payments' THEN p:=NEW;
 ELSE
  IF NEW.recorder_type<>'CryptoPayment' OR NEW.record_type<>'receipt' OR NEW.reversal_of_line_id IS NOT NULL THEN RETURN NEW; END IF;
  SELECT * INTO STRICT p FROM doc_crypto_payments WHERE id=NEW.recorder_id;
 END IF;
 SELECT EXISTS(SELECT 1 FROM cat_wallets w JOIN cat_tokens t ON t.id=p.token_id
  CROSS JOIN LATERAL fn_crypto_expected_transfer_candidates(p.network_id,p.tx_hash,p.event_kind,p.from_address,w.address,COALESCE(t.contract_address,''),p.amount) c WHERE w.id=p.wallet_id) INTO blocked;
 IF blocked THEN RAISE EXCEPTION 'expected transfer cannot create an external payment receipt' USING ERRCODE='23514'; END IF; RETURN NEW;
END; $$;
CREATE TRIGGER trg_expected_payment BEFORE INSERT ON doc_crypto_payments FOR EACH ROW EXECUTE FUNCTION fn_crypto_expected_external_payment_guard();
CREATE TRIGGER trg_expected_payment_movement BEFORE INSERT ON reg_crypto_balance_movements FOR EACH ROW EXECUTE FUNCTION fn_crypto_expected_external_payment_guard();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN IF EXISTS(SELECT 1 FROM reg_crypto_expected_transfer_requests) OR EXISTS(SELECT 1 FROM reg_crypto_expected_transfer_routes) THEN
 RAISE EXCEPTION 'expected transfer routing requires forward migration'; END IF; END; $$;
DROP TRIGGER trg_expected_payment_movement ON reg_crypto_balance_movements;
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
-- +goose StatementEnd
