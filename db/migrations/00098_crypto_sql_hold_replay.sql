-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

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
CREATE TRIGGER trg_crypto_network_hold_insert_replay BEFORE INSERT ON reg_crypto_network_holds FOR EACH ROW EXECUTE FUNCTION fn_crypto_sql_hold_guard();
CREATE TRIGGER trg_crypto_wallet_hold_insert_replay BEFORE INSERT ON reg_crypto_wallet_allocation_holds FOR EACH ROW EXECUTE FUNCTION fn_crypto_sql_hold_guard();
DROP TRIGGER trg_crypto_network_hold_owner_guard ON reg_crypto_network_holds;
DROP TRIGGER trg_crypto_wallet_hold_owner_guard ON reg_crypto_wallet_allocation_holds;
CREATE TRIGGER trg_crypto_network_hold_owner_guard BEFORE UPDATE OR DELETE ON reg_crypto_network_holds FOR EACH ROW EXECUTE FUNCTION fn_crypto_sql_hold_guard();
CREATE TRIGGER trg_crypto_wallet_hold_owner_guard BEFORE UPDATE OR DELETE ON reg_crypto_wallet_allocation_holds FOR EACH ROW EXECUTE FUNCTION fn_crypto_sql_hold_guard();

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
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
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
-- +goose StatementEnd
