-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE reg_crypto_external_outflows ADD COLUMN IF NOT EXISTS initial_basis JSONB CHECK(initial_basis IS NULL OR jsonb_typeof(initial_basis)='object');
ALTER TABLE reg_crypto_external_outflow_observations ADD COLUMN IF NOT EXISTS incident_id UUID REFERENCES reg_crypto_reconciliation_items(id);
-- Historical projections are not evidence of their original observation.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM reg_crypto_external_outflows o WHERE initial_basis IS NOT NULL AND
   (initial_basis->>'networkId',initial_basis->>'walletId',initial_basis->>'tokenId',initial_basis->>'txHash',initial_basis->>'eventKind',initial_basis->>'eventIndex',
    initial_basis->>'sourceKey',initial_basis->>'destinationKey',initial_basis->>'contractKey',initial_basis->>'amount') IS DISTINCT FROM
   (network_id::text,source_wallet_id::text,token_id::text,tx_hash,event_kind,event_index::text,source_address_key,destination_address_key,contract_address_key,amount::text)) THEN
  RAISE EXCEPTION 'external outflow initial history requires review before upgrade' USING ERRCODE='23514';
 END IF;
END $$;

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
CREATE TRIGGER trg_crypto_external_outflow_basis BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_external_outflows FOR EACH ROW EXECUTE FUNCTION fn_crypto_external_outflow_basis_guard();

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
CREATE TRIGGER trg_crypto_external_outflow_observation_source BEFORE INSERT ON reg_crypto_external_outflow_observations FOR EACH ROW EXECUTE FUNCTION fn_crypto_external_outflow_observation_source();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
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
-- +goose StatementEnd
