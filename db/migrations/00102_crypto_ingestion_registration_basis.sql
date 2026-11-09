-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
-- Existing rows retain unknown original shadow/activation evidence. Down keeps
-- these additive columns so that retained registration history is not erased.
ALTER TABLE sys_crypto_block_ingestion ADD COLUMN IF NOT EXISTS registration_basis JSONB;
ALTER TABLE sys_crypto_block_ingestion ADD COLUMN IF NOT EXISTS activation_basis JSONB;

DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM sys_crypto_block_ingestion WHERE mode IN ('solidified','hybrid') AND activated_at IS NULL) THEN
  RAISE EXCEPTION 'financial ingestion without activation time requires review before upgrade' USING ERRCODE='23514';
 END IF;
 IF EXISTS(SELECT 1 FROM sys_crypto_block_ingestion i WHERE
   (activation_basis IS NOT NULL AND (activation_basis->>'height' IS DISTINCT FROM start_height::text OR activation_basis->>'hash' IS DISTINCT FROM start_hash
     OR activation_basis->>'timestamp' IS DISTINCT FROM start_timestamp::text OR (activation_basis->>'activatedAt')::timestamptz IS DISTINCT FROM activated_at))
   OR (registration_basis IS NOT NULL AND activated_at IS NULL AND (registration_basis->>'height' IS DISTINCT FROM start_height::text
     OR registration_basis->>'hash' IS DISTINCT FROM start_hash OR registration_basis->>'timestamp' IS DISTINCT FROM start_timestamp::text))) THEN
  RAISE EXCEPTION 'ingestion boundary history requires review before upgrade' USING ERRCODE='23514';
 END IF;
 IF EXISTS(SELECT 1 FROM sys_crypto_watch_targets t WHERE
   (target_kind='address' AND NOT EXISTS(SELECT 1 FROM cat_wallets w WHERE w.id=t.owner_id AND w.network_id=t.network_id AND w.address=t.target_key))
   OR (target_kind='asset' AND NOT EXISTS(SELECT 1 FROM cat_tokens a WHERE a.id=t.owner_id AND a.network_id=t.network_id AND a.contract_address=t.target_key))) THEN
  RAISE EXCEPTION 'watch target owner requires review before upgrade' USING ERRCODE='23514';
 END IF;
END $$;

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
CREATE TRIGGER trg_crypto_block_ingestion_basis BEFORE INSERT OR UPDATE OR DELETE ON sys_crypto_block_ingestion FOR EACH ROW EXECUTE FUNCTION fn_crypto_ingestion_basis_guard();

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
CREATE TRIGGER trg_crypto_watch_target_guard BEFORE INSERT OR UPDATE OR DELETE ON sys_crypto_watch_targets FOR EACH ROW EXECUTE FUNCTION fn_crypto_watch_target_guard();

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
CREATE TRIGGER trg_crypto_watched_wallet_identity BEFORE UPDATE OR DELETE ON cat_wallets FOR EACH ROW EXECUTE FUNCTION fn_crypto_watched_catalog_guard();
CREATE TRIGGER trg_crypto_watched_asset_identity BEFORE UPDATE OR DELETE ON cat_tokens FOR EACH ROW EXECUTE FUNCTION fn_crypto_watched_catalog_guard();

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
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
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
-- +goose StatementEnd
