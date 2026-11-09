-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE sys_crypto_aml_pool_policies ADD COLUMN automatic_baseline BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE reg_crypto_aml_pool_policy_versions ADD COLUMN automatic_baseline BOOLEAN NOT NULL DEFAULT FALSE;
CREATE FUNCTION fn_crypto_aml_pool_preflight_policy_guard() RETURNS TRIGGER AS $$
BEGIN
 IF NEW.automatic_baseline IS DISTINCT FROM OLD.automatic_baseline AND NEW.version<>OLD.version+1 THEN
   RAISE EXCEPTION 'automatic baseline change requires next policy version' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_pool_preflight_policy BEFORE UPDATE ON sys_crypto_aml_pool_policies FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_pool_preflight_policy_guard();
CREATE OR REPLACE FUNCTION fn_crypto_aml_pool_policy_audit() RETURNS TRIGGER AS $$
BEGIN
 IF TG_OP='INSERT' OR NEW.version<>OLD.version THEN
   INSERT INTO reg_crypto_aml_pool_policy_versions(policy_id,version,enabled,low_watermark,target_ready,max_total,max_provision_batch,configured_by,automatic_baseline)
     VALUES(NEW.id,NEW.version,NEW.enabled,NEW.low_watermark,NEW.target_ready,NEW.max_total,NEW.max_provision_batch,NEW.configured_by,NEW.automatic_baseline);
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TABLE reg_crypto_aml_watch_coverage (
 wallet_id UUID NOT NULL REFERENCES cat_wallets(id), token_id UUID NOT NULL REFERENCES cat_tokens(id), network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id),
 watcher_key TEXT NOT NULL CHECK(watcher_key<>''), lease_owner TEXT NOT NULL CHECK(lease_owner<>''), fencing_token BIGINT NOT NULL CHECK(fencing_token>0), start_height BIGINT NOT NULL CHECK(start_height>0), acknowledged_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL), PRIMARY KEY(wallet_id,token_id,watcher_key),
 FOREIGN KEY(token_id,network_id) REFERENCES cat_tokens(id,network_id)
);
-- Only the current leased watcher can attest that its address set was updated.
-- Persisted evidence remains valid across later leases because owned addresses
-- stay monitored and checkpoint progress is fenced independently.
CREATE FUNCTION fn_crypto_aml_watch_coverage_guard() RETURNS TRIGGER AS $$
BEGIN
 PERFORM 1 FROM sys_chain_watcher_state state JOIN cat_wallets w ON w.id=NEW.wallet_id AND w.network_id=state.network_id
 JOIN cat_tokens t ON t.id=NEW.token_id AND t.network_id=w.network_id JOIN cat_blockchain_networks n ON n.id=w.network_id AND n.chain_family='evm'
 WHERE state.network_id=NEW.network_id AND state.watcher_key=NEW.watcher_key AND state.lease_owner=NEW.lease_owner AND state.fencing_token=NEW.fencing_token AND state.lease_expires_at>NOW()
 AND NEW.watcher_key=CASE WHEN t.token_standard='native' THEN 'native_eth' ELSE 'erc20:'||LOWER(t.contract_address) END FOR SHARE OF state;
 IF NOT FOUND THEN RAISE EXCEPTION 'address coverage requires the current watcher lease' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_watch_coverage_guard BEFORE INSERT ON reg_crypto_aml_watch_coverage FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_watch_coverage_guard();
CREATE TRIGGER trg_crypto_aml_watch_coverage_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_watch_coverage FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TABLE reg_crypto_aml_preflight_jobs (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(), wallet_id UUID NOT NULL REFERENCES cat_wallets(id), token_id UUID NOT NULL REFERENCES cat_tokens(id),
 policy_id UUID NOT NULL REFERENCES sys_crypto_aml_pool_policies(id), policy_version BIGINT NOT NULL, scope_version BIGINT NOT NULL,
 status TEXT NOT NULL DEFAULT 'retry_wait' CHECK(status IN ('retry_wait','processing','completed','needs_attention')),
 attempts INT NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 120), next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 lease_token UUID, lease_expires_at TIMESTAMPTZ, worker_id TEXT NOT NULL DEFAULT '', error_code TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
 UNIQUE(wallet_id,token_id), FOREIGN KEY(policy_id,policy_version) REFERENCES reg_crypto_aml_pool_policy_versions(policy_id,version),
 CHECK((status='processing')=(lease_token IS NOT NULL AND lease_expires_at IS NOT NULL))
);
CREATE INDEX idx_crypto_aml_preflight_due ON reg_crypto_aml_preflight_jobs(next_attempt_at,id) WHERE status IN ('retry_wait','processing');
CREATE TRIGGER trg_crypto_aml_preflight_updated BEFORE UPDATE ON reg_crypto_aml_preflight_jobs FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_crypto_aml_preflight_txid BEFORE UPDATE ON reg_crypto_aml_preflight_jobs FOR EACH ROW EXECUTE FUNCTION update_txid_column();
CREATE TABLE reg_crypto_aml_preflight_observations (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(), job_id UUID NOT NULL REFERENCES reg_crypto_aml_preflight_jobs(id), lease_token UUID NOT NULL,
 wallet_id UUID NOT NULL REFERENCES cat_wallets(id), token_id UUID NOT NULL REFERENCES cat_tokens(id), network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id),
 block_number BIGINT NOT NULL CHECK(block_number>=0), block_hash TEXT NOT NULL CHECK(block_hash ~ '^(0x)?[a-f0-9]{64}$'),
 chain_amount NUMERIC(78,0) NOT NULL CHECK(chain_amount>=0), proof JSONB NOT NULL CHECK(jsonb_typeof(proof)='object'),
 observed_at TIMESTAMPTZ NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL), UNIQUE(job_id,lease_token)
);
CREATE TRIGGER trg_crypto_aml_preflight_evidence BEFORE UPDATE OR DELETE ON reg_crypto_aml_preflight_observations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();
-- The baseline cannot erase any observed physical or financial history. The
-- watcher must already cover this checkpoint; later inbox arrivals stay visible.
CREATE FUNCTION fn_crypto_aml_automatic_baseline_guard() RETURNS TRIGGER AS $$
BEGIN
 IF NEW.verified_by='aml-preflight' AND NOT EXISTS(
   SELECT 1 FROM reg_crypto_aml_preflight_observations o JOIN reg_crypto_aml_preflight_jobs j ON j.id=o.job_id
   JOIN sys_crypto_aml_pool_policies p ON p.id=j.policy_id AND p.enabled AND p.automatic_baseline AND p.version=j.policy_version
   JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=p.merchant_id AND s.network_id=p.network_id AND s.token_id=j.token_id AND s.enabled AND s.version=j.scope_version
   JOIN reg_crypto_aml_wallet_provisions v ON v.wallet_id=j.wallet_id AND v.status='completed'
   JOIN reg_crypto_aml_pool_intents i ON i.id=v.id AND i.policy_id=p.id JOIN cat_wallets w ON w.id=v.wallet_id AND w.is_active AND NOT w.deletion_mark AND w.custody_merchant_id=p.merchant_id AND w.wallet_role=p.wallet_role
   JOIN cat_blockchain_networks network ON network.id=w.network_id JOIN cat_tokens asset ON asset.id=j.token_id
   LEFT JOIN sys_crypto_watch_targets watch ON watch.network_id=w.network_id AND watch.target_kind='address' AND watch.owner_id=w.id AND watch._deleted_at IS NULL AND watch.start_height<=o.block_number
   JOIN reg_crypto_reconciliation_checks c ON c.id=NEW.reconciliation_check_id AND c.observed_at=o.observed_at
   WHERE o.id::text=NEW.evidence_reference AND o.wallet_id=NEW.wallet_id AND o.token_id=NEW.token_id AND o.network_id=NEW.network_id
     AND ((network.chain_family='tron' AND watch.owner_id IS NOT NULL) OR (network.chain_family='evm' AND EXISTS(SELECT 1 FROM reg_crypto_aml_watch_coverage coverage WHERE coverage.wallet_id=w.id AND coverage.token_id=j.token_id AND coverage.network_id=w.network_id AND coverage.start_height<=o.block_number AND coverage.watcher_key=CASE WHEN asset.token_standard='native' THEN 'native_eth' ELSE 'erc20:'||LOWER(asset.contract_address) END)))
     AND o.block_number=NEW.block_number AND o.block_hash=NEW.block_hash AND o.chain_amount=0
     AND o.observed_at BETWEEN NOW()-interval '1 minute' AND NOW() AND j.status='processing' AND j.lease_token=o.lease_token AND j.lease_expires_at>NOW()
     AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_effects WHERE wallet_id=w.id AND token_id=j.token_id)
     AND NOT EXISTS(SELECT 1 FROM reg_crypto_balance_movements WHERE wallet_id=w.id AND token_id=j.token_id)
     AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_preflight_observations previous WHERE previous.wallet_id=w.id AND previous.token_id=j.token_id AND previous.chain_amount<>0)
 ) THEN RAISE EXCEPTION 'automatic AML baseline requires leased independent empty checkpoint and no prior inventory' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_automatic_baseline BEFORE INSERT ON reg_crypto_aml_wallet_baselines FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_automatic_baseline_guard();
CREATE OR REPLACE FUNCTION fn_crypto_aml_pool_baselines_ready(intent UUID) RETURNS BOOLEAN AS $$
 SELECT EXISTS(SELECT 1 FROM reg_crypto_aml_pool_intents i JOIN sys_crypto_aml_pool_policies p ON p.id=i.policy_id AND p.wallet_role IN ('merchant_pool','liquidity_hot','network_fee')
   JOIN reg_crypto_aml_wallet_provisions v ON v.id=i.id AND v.status='completed' JOIN cat_wallets w ON w.id=v.wallet_id AND w.custody_merchant_id=p.merchant_id AND w.network_id=p.network_id AND w.wallet_role=p.wallet_role
   WHERE i.id=intent AND EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes s WHERE s.merchant_id=p.merchant_id AND s.network_id=p.network_id AND s.enabled AND (p.wallet_role<>'network_fee' OR EXISTS(SELECT 1 FROM cat_tokens native WHERE native.id=s.token_id AND native.token_standard='native')) AND (p.token_id IS NULL OR s.token_id=p.token_id OR (p.automatic_baseline AND EXISTS(SELECT 1 FROM cat_tokens native WHERE native.id=s.token_id AND native.token_standard='native'))))
   AND NOT EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes s LEFT JOIN reg_crypto_aml_wallet_baselines baseline ON baseline.wallet_id=w.id AND baseline.token_id=s.token_id
     LEFT JOIN reg_crypto_aml_inventory_positions inventory ON inventory.wallet_id=w.id AND inventory.token_id=s.token_id
     WHERE s.merchant_id=p.merchant_id AND s.network_id=p.network_id AND s.enabled AND (p.wallet_role<>'network_fee' OR EXISTS(SELECT 1 FROM cat_tokens native WHERE native.id=s.token_id AND native.token_standard='native')) AND (p.token_id IS NULL OR s.token_id=p.token_id OR (p.automatic_baseline AND EXISTS(SELECT 1 FROM cat_tokens native WHERE native.id=s.token_id AND native.token_standard='native')))
       AND (baseline.wallet_id IS NULL OR inventory.wallet_id IS NULL OR inventory.principal+inventory.service_fee<>0 OR NOT fn_crypto_aml_total_inventory_covered(w.id,s.token_id))));
$$ LANGUAGE SQL STABLE;

CREATE FUNCTION fn_crypto_aml_preflight_observation_guard() RETURNS TRIGGER AS $$
DECLARE w cat_wallets; asset cat_tokens; source_a JSONB; source_b JSONB; family TEXT;
BEGIN
 SELECT * INTO STRICT w FROM cat_wallets WHERE id=NEW.wallet_id;
 SELECT * INTO STRICT asset FROM cat_tokens WHERE id=NEW.token_id AND network_id=NEW.network_id;
 SELECT chain_family INTO STRICT family FROM cat_blockchain_networks WHERE id=NEW.network_id;
 IF NOT EXISTS(SELECT 1 FROM reg_crypto_aml_preflight_jobs j JOIN sys_crypto_aml_pool_policies p ON p.id=j.policy_id AND p.enabled AND p.automatic_baseline AND p.version=j.policy_version
   WHERE j.id=NEW.job_id AND j.wallet_id=NEW.wallet_id AND j.token_id=NEW.token_id AND j.status='processing' AND j.lease_token=NEW.lease_token AND j.lease_expires_at>NOW() AND p.network_id=NEW.network_id AND p.merchant_id=w.custody_merchant_id) THEN RAISE EXCEPTION 'preflight observation requires current lease and custody policy' USING ERRCODE='23514'; END IF;
 source_a:=NEW.proof->'sources'->0;source_b:=NEW.proof->'sources'->1;
 IF (jsonb_typeof(NEW.proof->'sources')='array' AND jsonb_array_length(NEW.proof->'sources')=2
   AND NEW.proof->>'networkId'=NEW.network_id::text AND NEW.proof->'asset'->>'tokenId'=NEW.token_id::text
   AND NEW.proof->'asset'->>'standard'=asset.token_standard AND (NEW.proof->'asset'->>'decimals')::int=asset.decimal_places
   AND CASE WHEN family='evm' THEN LOWER(COALESCE(NEW.proof->'asset'->>'contractAddress',''))=LOWER(COALESCE(asset.contract_address,'')) ELSE COALESCE(NEW.proof->'asset'->>'contractAddress','')=COALESCE(asset.contract_address,'') END
   AND NEW.proof->>'address'=w.address AND (NEW.proof->>'height')::bigint=NEW.block_number AND NEW.proof->>'hash'=NEW.block_hash
   AND ABS(EXTRACT(EPOCH FROM ((NEW.proof->>'observedAt')::timestamptz-NEW.observed_at)))<=5.000001
   AND (NEW.proof->>'observedAt')::timestamptz BETWEEN NOW()-interval '1 minute' AND NOW()+interval '5 seconds'
   AND NEW.observed_at BETWEEN NOW()-interval '1 minute' AND NOW()
   AND (source_a->>'height')::bigint=NEW.block_number AND (source_b->>'height')::bigint=NEW.block_number
   AND LOWER(source_a->>'hash')=NEW.block_hash AND LOWER(source_b->>'hash')=NEW.block_hash
   AND source_a->>'balance'=NEW.chain_amount::text AND source_b->>'balance'=NEW.chain_amount::text
   AND btrim(source_a->>'providerId')<>'' AND btrim(source_b->>'providerId')<>'' AND LOWER(btrim(source_a->>'providerId'))<>LOWER(btrim(source_b->>'providerId'))
   AND btrim(source_a->>'trustGroup')<>'' AND btrim(source_b->>'trustGroup')<>'' AND LOWER(btrim(source_a->>'trustGroup'))<>LOWER(btrim(source_b->>'trustGroup'))
   AND source_a->>'method'=CASE WHEN family='tron' THEN 'solidified_bracket' ELSE 'finalized_block_bracket' END
   AND source_b->>'method'=source_a->>'method') IS NOT TRUE THEN RAISE EXCEPTION 'preflight requires two independent pinned final state observations' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_preflight_observation BEFORE INSERT ON reg_crypto_aml_preflight_observations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_preflight_observation_guard();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN IF EXISTS(SELECT 1 FROM reg_crypto_aml_preflight_observations) OR EXISTS(SELECT 1 FROM reg_crypto_aml_preflight_jobs) OR EXISTS(SELECT 1 FROM reg_crypto_aml_watch_coverage) THEN RAISE EXCEPTION 'cannot discard AML preflight evidence'; END IF; END $$;
DROP TRIGGER trg_crypto_aml_automatic_baseline ON reg_crypto_aml_wallet_baselines;
DROP FUNCTION fn_crypto_aml_automatic_baseline_guard();
DROP TABLE reg_crypto_aml_preflight_observations;
DROP FUNCTION fn_crypto_aml_preflight_observation_guard();
DROP TABLE reg_crypto_aml_preflight_jobs;
DROP TABLE reg_crypto_aml_watch_coverage;
DROP FUNCTION fn_crypto_aml_watch_coverage_guard();
DROP TRIGGER trg_crypto_aml_pool_preflight_policy ON sys_crypto_aml_pool_policies;
DROP FUNCTION fn_crypto_aml_pool_preflight_policy_guard();
CREATE OR REPLACE FUNCTION fn_crypto_aml_pool_policy_audit() RETURNS TRIGGER AS $$
BEGIN
 IF TG_OP='INSERT' OR NEW.version<>OLD.version THEN
   INSERT INTO reg_crypto_aml_pool_policy_versions(policy_id,version,enabled,low_watermark,target_ready,max_total,max_provision_batch,configured_by)
     VALUES(NEW.id,NEW.version,NEW.enabled,NEW.low_watermark,NEW.target_ready,NEW.max_total,NEW.max_provision_batch,NEW.configured_by);
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE OR REPLACE FUNCTION fn_crypto_aml_pool_baselines_ready(intent UUID) RETURNS BOOLEAN AS $$
 SELECT EXISTS(SELECT 1 FROM reg_crypto_aml_pool_intents i JOIN sys_crypto_aml_pool_policies p ON p.id=i.policy_id AND p.wallet_role IN ('merchant_pool','liquidity_hot')
   JOIN reg_crypto_aml_wallet_provisions v ON v.id=i.id AND v.status='completed' JOIN cat_wallets w ON w.id=v.wallet_id AND w.custody_merchant_id=p.merchant_id AND w.network_id=p.network_id AND w.wallet_role=p.wallet_role
   WHERE i.id=intent AND EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes s WHERE s.merchant_id=p.merchant_id AND s.network_id=p.network_id AND s.enabled AND (p.token_id IS NULL OR s.token_id=p.token_id))
   AND NOT EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes s LEFT JOIN reg_crypto_aml_wallet_baselines baseline ON baseline.wallet_id=w.id AND baseline.token_id=s.token_id
     LEFT JOIN reg_crypto_aml_inventory_positions inventory ON inventory.wallet_id=w.id AND inventory.token_id=s.token_id
     WHERE s.merchant_id=p.merchant_id AND s.network_id=p.network_id AND s.enabled AND (p.token_id IS NULL OR s.token_id=p.token_id)
       AND (baseline.wallet_id IS NULL OR inventory.wallet_id IS NULL OR inventory.principal+inventory.service_fee<>0 OR NOT fn_crypto_aml_total_inventory_covered(w.id,s.token_id))));
$$ LANGUAGE SQL STABLE;
ALTER TABLE reg_crypto_aml_pool_policy_versions DROP COLUMN automatic_baseline;
ALTER TABLE sys_crypto_aml_pool_policies DROP COLUMN automatic_baseline;
-- +goose StatementEnd
