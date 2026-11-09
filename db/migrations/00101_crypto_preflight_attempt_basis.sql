-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
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
CREATE TRIGGER trg_crypto_aml_preflight_attempt_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_preflight_attempts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

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
CREATE TRIGGER trg_crypto_aml_preflight_job_identity BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_aml_preflight_jobs FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_preflight_job_guard();

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
CREATE TRIGGER trg_crypto_aml_preflight_attempt_guard BEFORE INSERT ON reg_crypto_aml_preflight_attempts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_preflight_attempt_guard();

CREATE FUNCTION fn_crypto_aml_preflight_claim_committed() RETURNS TRIGGER AS $$
BEGIN
 IF NEW.status='processing' AND (TG_OP='INSERT' OR OLD.lease_token IS DISTINCT FROM NEW.lease_token)
   AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_preflight_attempts WHERE job_id=NEW.id AND lease_token=NEW.lease_token) THEN
  RAISE EXCEPTION 'preflight claim requires immutable attempt evidence' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE CONSTRAINT TRIGGER trg_crypto_aml_preflight_claim_committed AFTER INSERT OR UPDATE ON reg_crypto_aml_preflight_jobs
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_preflight_claim_committed();

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
CREATE TRIGGER trg_crypto_aml_preflight_attempt_observation BEFORE INSERT ON reg_crypto_aml_preflight_observations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_preflight_attempt_observation_guard();
ALTER TABLE reg_crypto_aml_preflight_observations ADD CONSTRAINT fk_crypto_preflight_observation_attempt
 FOREIGN KEY(job_id,lease_token) REFERENCES reg_crypto_aml_preflight_attempts(job_id,lease_token) NOT VALID;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
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
-- +goose StatementEnd
