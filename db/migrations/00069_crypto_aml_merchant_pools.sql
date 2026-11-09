-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

CREATE TABLE sys_crypto_aml_pool_policies (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(), merchant_id UUID NOT NULL REFERENCES cat_merchants(id),
 network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id),
 wallet_role TEXT NOT NULL CHECK(wallet_role IN ('merchant_pool','liquidity_hot','network_fee')),
 token_id UUID, risk_class TEXT,
 enabled BOOLEAN NOT NULL DEFAULT FALSE, version BIGINT NOT NULL DEFAULT 1 CHECK(version>0),
 low_watermark INT NOT NULL, target_ready INT NOT NULL, max_total INT NOT NULL, max_provision_batch INT NOT NULL,
 configured_by TEXT NOT NULL CHECK(btrim(configured_by)<>''),
 next_reconcile_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), last_reconcile_error TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
 UNIQUE NULLS NOT DISTINCT(merchant_id,network_id,wallet_role,token_id,risk_class),
 FOREIGN KEY(token_id,network_id) REFERENCES cat_tokens(id,network_id),
 CHECK((wallet_role='liquidity_hot' AND token_id IS NOT NULL AND risk_class IN ('low','medium')) OR (wallet_role<>'liquidity_hot' AND token_id IS NULL AND risk_class IS NULL)),
 CHECK(low_watermark>=0 AND low_watermark<target_ready AND target_ready<=max_total AND max_total<=100000 AND max_provision_batch BETWEEN 1 AND 50),
 CHECK(wallet_role<>'network_fee' OR (low_watermark=0 AND target_ready=1 AND max_provision_batch=1))
);
CREATE INDEX idx_crypto_aml_pool_due ON sys_crypto_aml_pool_policies(next_reconcile_at,id) WHERE enabled;
CREATE TRIGGER trg_crypto_aml_pool_updated BEFORE UPDATE ON sys_crypto_aml_pool_policies FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_crypto_aml_pool_txid BEFORE UPDATE ON sys_crypto_aml_pool_policies FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TABLE reg_crypto_aml_pool_policy_versions (
 policy_id UUID NOT NULL REFERENCES sys_crypto_aml_pool_policies(id), version BIGINT NOT NULL,
 enabled BOOLEAN NOT NULL, low_watermark INT NOT NULL, target_ready INT NOT NULL, max_total INT NOT NULL, max_provision_batch INT NOT NULL,
 configured_by TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
 PRIMARY KEY(policy_id,version)
);
CREATE TRIGGER trg_crypto_aml_pool_version_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_pool_policy_versions FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TABLE reg_crypto_aml_pool_intents (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(), policy_id UUID NOT NULL, policy_version BIGINT NOT NULL,
 plan_id UUID NOT NULL, ordinal INT NOT NULL CHECK(ordinal BETWEEN 1 AND 50),
 next_batch_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), batch_attempts INT NOT NULL DEFAULT 0 CHECK(batch_attempts>=0), batch_error TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
 UNIQUE(policy_id,plan_id,ordinal), FOREIGN KEY(policy_id,policy_version) REFERENCES reg_crypto_aml_pool_policy_versions(policy_id,version)
);
CREATE INDEX idx_crypto_aml_pool_intent_due ON reg_crypto_aml_pool_intents(next_batch_attempt_at,created_at,id);
CREATE INDEX idx_crypto_aml_pool_intent_policy ON reg_crypto_aml_pool_intents(policy_id,id);
CREATE INDEX idx_crypto_aml_provision_pending_scope ON reg_crypto_aml_wallet_provisions(merchant_id,network_id,wallet_role) WHERE wallet_id IS NULL;
CREATE TRIGGER trg_crypto_aml_pool_intent_updated BEFORE UPDATE ON reg_crypto_aml_pool_intents FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_crypto_aml_pool_intent_txid BEFORE UPDATE ON reg_crypto_aml_pool_intents FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TABLE reg_crypto_aml_pool_intent_cancellations (
 intent_id UUID PRIMARY KEY REFERENCES reg_crypto_aml_pool_intents(id), reason TEXT NOT NULL CHECK(length(btrim(reason)) BETWEEN 3 AND 500),
 cancelled_by TEXT NOT NULL CHECK(btrim(cancelled_by)<>''), created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL)
);
CREATE TRIGGER trg_crypto_aml_pool_cancel_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_pool_intent_cancellations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE VIEW crypto_aml_pool_runtime AS
 SELECT p.id FROM sys_crypto_aml_pool_policies p
 JOIN sys_crypto_custody_policies custody ON custody.network_id=p.network_id AND custody.enforced
 JOIN cat_blockchain_networks n ON n.id=p.network_id AND n.is_active AND NOT n.deletion_mark AND n.chain_family IN ('tron','evm')
 JOIN sys_crypto_wallet_pool_policies base ON base.network_id=p.network_id AND base.enabled AND NOT base.needs_review AND base.lease_blocked_reason=''
 JOIN sys_crypto_wallet_derivation_cursors cursor ON cursor.network_id=p.network_id AND NOT cursor.needs_review AND cursor.derivation_namespace=base.derivation_namespace AND cursor.derivation_namespace<>''
 WHERE p.enabled AND EXISTS(SELECT 1 FROM sys_crypto_aml_provisioning_identity WHERE id AND database_name=current_database())
   AND NOT EXISTS(SELECT 1 FROM reg_crypto_network_holds hold WHERE hold.network_id=p.network_id AND hold.status='open');

-- Ready is usable address capacity, never a claim that a fee wallet has gas.
-- Pending includes mandatory preflight so an RPC/activation outage cannot
-- create a new address every reconciliation cycle.
CREATE FUNCTION fn_crypto_aml_pool_counts(pool UUID) RETURNS TABLE(ready INT,pending INT,queued INT,total INT,started INT) AS $$
 WITH policy AS (SELECT * FROM sys_crypto_aml_pool_policies WHERE id=pool), wallets AS (
   SELECT w.*,b.id batch_id,b.intake_closed_at,b.intake_closes_at,b.execution_held,b.scope_version,b.max_gross,usage.used,
     risk.risk_class,risk.inventory_covered,risk.expires_at,inventory.principal,inventory.service_fee,scope.version current_scope,
     (EXISTS(SELECT 1 FROM reg_crypto_wallet_allocation_holds h WHERE h.wallet_id=w.id AND h.status='open') OR EXISTS(SELECT 1 FROM reg_crypto_reconciliation_items ri WHERE ri.wallet_id=w.id AND ri.kind IN ('token_balance_mismatch','native_fee_balance_mismatch') AND ri.status IN ('open','acknowledged'))) held,
     (EXISTS(SELECT 1 FROM reg_crypto_wallet_allocation_holds h WHERE h.wallet_id=w.id AND h.status='open' AND h.reason NOT IN ('chain_balance_check_pending','aml_baseline_pending')) OR EXISTS(SELECT 1 FROM reg_crypto_reconciliation_items ri WHERE ri.wallet_id=w.id AND ri.kind IN ('token_balance_mismatch','native_fee_balance_mismatch') AND ri.status IN ('open','acknowledged'))) quarantined
   FROM policy p JOIN cat_wallets w ON w.network_id=p.network_id AND w.custody_merchant_id=p.merchant_id
   LEFT JOIN reg_crypto_aml_batches b ON b.collector_wallet_id=w.id AND b.token_id=p.token_id AND b.risk_class=p.risk_class
   LEFT JOIN crypto_aml_batch_usage usage ON usage.id=b.id
   LEFT JOIN reg_crypto_wallet_risk_states risk ON risk.wallet_id=w.id AND risk.token_id=p.token_id
   LEFT JOIN reg_crypto_aml_inventory_positions inventory ON inventory.wallet_id=w.id AND inventory.token_id=p.token_id
   LEFT JOIN sys_crypto_aml_routing_scopes scope ON scope.merchant_id=p.merchant_id AND scope.network_id=p.network_id AND scope.token_id=p.token_id
   WHERE (p.wallet_role<>'liquidity_hot' AND w.wallet_role=p.wallet_role) OR b.id IS NOT NULL OR EXISTS(SELECT 1 FROM reg_crypto_aml_pool_intents i JOIN reg_crypto_aml_wallet_provisions v ON v.id=i.id AND v.wallet_id=w.id WHERE i.policy_id=p.id)
 ), classified AS (
   SELECT w.id,w.wallet_role=p.wallet_role AND w.is_active AND NOT w.deletion_mark AND w.chain_activation_status='active' AND NOT w.held AND
     CASE p.wallet_role WHEN 'merchant_pool' THEN w.status='free' AND w.allocation_mode='transient' AND (w.cooldown_until IS NULL OR w.cooldown_until<NOW())
       WHEN 'network_fee' THEN TRUE
       ELSE w.batch_id IS NOT NULL AND w.intake_closed_at IS NULL AND w.intake_closes_at>NOW() AND NOT w.execution_held AND w.scope_version=w.current_scope AND w.used<w.max_gross
         AND w.risk_class<>'high' AND fn_crypto_aml_total_inventory_covered(w.id,p.token_id)
         AND (w.principal+w.service_fee=0 OR (w.risk_class=p.risk_class AND w.inventory_covered AND w.expires_at>NOW())) END usable,
     w.wallet_role=p.wallet_role AND w.is_active AND NOT w.deletion_mark AND w.chain_activation_status NOT IN ('held','failed') AND
     CASE p.wallet_role WHEN 'network_fee' THEN TRUE
       WHEN 'merchant_pool' THEN w.status='free' AND w.allocation_mode='transient' AND NOT w.quarantined
       ELSE NOT w.quarantined AND ((w.batch_id IS NOT NULL AND w.intake_closed_at IS NULL AND w.intake_closes_at>NOW() AND NOT w.execution_held AND w.used<w.max_gross)
         OR (w.batch_id IS NULL AND w.risk_class IS DISTINCT FROM 'high' AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_effects e JOIN cat_tokens t ON t.id=e.token_id AND t.token_standard<>'native' WHERE e.wallet_id=w.id)
           AND NOT EXISTS(SELECT 1 FROM reg_crypto_balance_movements m JOIN cat_tokens t ON t.id=m.token_id AND t.token_standard<>'native' WHERE m.wallet_id=w.id))) END potential
   FROM wallets w CROSS JOIN policy p
 ), unfinished AS (
   SELECT COUNT(*)::INT amount FROM policy p JOIN reg_crypto_aml_wallet_provisions v ON v.merchant_id=p.merchant_id AND v.network_id=p.network_id AND v.wallet_role=p.wallet_role AND v.wallet_id IS NULL
   WHERE p.wallet_role<>'liquidity_hot' OR EXISTS(SELECT 1 FROM reg_crypto_aml_pool_intents i WHERE i.id=v.id AND i.policy_id=p.id)
 ), unstarted AS (
   SELECT COUNT(*)::INT amount FROM reg_crypto_aml_pool_intents i WHERE i.policy_id=pool
     AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_provisions v WHERE v.id=i.id)
     AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_pool_intent_cancellations c WHERE c.intent_id=i.id)
 ) SELECT COUNT(*) FILTER(WHERE usable)::INT,COUNT(*) FILTER(WHERE potential AND NOT COALESCE(usable,FALSE))::INT+(SELECT amount FROM unfinished),
   (SELECT amount FROM unstarted),COUNT(*)::INT+(SELECT amount FROM unfinished)+(SELECT amount FROM unstarted),COUNT(*)::INT+(SELECT amount FROM unfinished)
 FROM classified;
$$ LANGUAGE SQL STABLE;

CREATE FUNCTION fn_crypto_aml_network_wallet_commitments(network UUID,excluded UUID) RETURNS BIGINT AS $$
 SELECT (SELECT COUNT(*) FROM cat_wallets WHERE network_id=network)
   +(SELECT COUNT(*) FROM sys_crypto_wallet_provisioning_jobs WHERE network_id=network AND wallet_id IS NULL)
   +(SELECT COUNT(*) FROM reg_crypto_aml_wallet_provisions WHERE network_id=network AND wallet_id IS NULL)
   +(SELECT COUNT(*) FROM reg_crypto_aml_pool_intents i JOIN sys_crypto_aml_pool_policies p ON p.id=i.policy_id AND p.network_id=network
      WHERE i.id IS DISTINCT FROM excluded AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_provisions v WHERE v.id=i.id)
        AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_pool_intent_cancellations c WHERE c.intent_id=i.id));
$$ LANGUAGE SQL STABLE;

CREATE FUNCTION fn_crypto_aml_pool_baselines_ready(intent UUID) RETURNS BOOLEAN AS $$
 SELECT EXISTS(SELECT 1 FROM reg_crypto_aml_pool_intents i JOIN sys_crypto_aml_pool_policies p ON p.id=i.policy_id AND p.wallet_role IN ('merchant_pool','liquidity_hot')
   JOIN reg_crypto_aml_wallet_provisions v ON v.id=i.id AND v.status='completed' JOIN cat_wallets w ON w.id=v.wallet_id AND w.custody_merchant_id=p.merchant_id AND w.network_id=p.network_id AND w.wallet_role=p.wallet_role
   WHERE i.id=intent AND EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes s WHERE s.merchant_id=p.merchant_id AND s.network_id=p.network_id AND s.enabled AND (p.token_id IS NULL OR s.token_id=p.token_id))
   AND NOT EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes s LEFT JOIN reg_crypto_aml_wallet_baselines baseline ON baseline.wallet_id=w.id AND baseline.token_id=s.token_id
     LEFT JOIN reg_crypto_aml_inventory_positions inventory ON inventory.wallet_id=w.id AND inventory.token_id=s.token_id
     WHERE s.merchant_id=p.merchant_id AND s.network_id=p.network_id AND s.enabled AND (p.token_id IS NULL OR s.token_id=p.token_id)
       AND (baseline.wallet_id IS NULL OR inventory.wallet_id IS NULL OR inventory.principal+inventory.service_fee<>0 OR NOT fn_crypto_aml_total_inventory_covered(w.id,s.token_id))));
$$ LANGUAGE SQL STABLE;

CREATE FUNCTION fn_crypto_aml_pool_scope_hold() RETURNS TRIGGER AS $$
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
CREATE TRIGGER trg_crypto_aml_pool_scope_hold AFTER INSERT OR UPDATE ON sys_crypto_aml_routing_scopes FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_pool_scope_hold();

CREATE FUNCTION fn_crypto_aml_pool_high_hold() RETURNS TRIGGER AS $$
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
CREATE TRIGGER trg_crypto_aml_pool_high_hold AFTER INSERT OR UPDATE ON reg_crypto_wallet_risk_states FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_pool_high_hold();

CREATE FUNCTION fn_crypto_aml_pool_policy_guard() RETURNS TRIGGER AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'merchant pool policies cannot be deleted' USING ERRCODE='23514'; END IF;
 PERFORM 1 FROM sys_crypto_custody_policies WHERE network_id=NEW.network_id AND enforced FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'merchant pools require enforced custody' USING ERRCODE='23514'; END IF;
 IF TG_OP='UPDATE' THEN
   IF (NEW.id,NEW.merchant_id,NEW.network_id,NEW.wallet_role,NEW.token_id,NEW.risk_class,NEW.created_at) IS DISTINCT FROM (OLD.id,OLD.merchant_id,OLD.network_id,OLD.wallet_role,OLD.token_id,OLD.risk_class,OLD.created_at) THEN
     RAISE EXCEPTION 'merchant pool scope is immutable' USING ERRCODE='23514';
   END IF;
   IF (NEW.enabled,NEW.low_watermark,NEW.target_ready,NEW.max_total,NEW.max_provision_batch,NEW.configured_by) IS NOT DISTINCT FROM (OLD.enabled,OLD.low_watermark,OLD.target_ready,OLD.max_total,OLD.max_provision_batch,OLD.configured_by) AND NEW.version=OLD.version THEN RETURN NEW; END IF;
   IF NEW.version<>OLD.version+1 THEN RAISE EXCEPTION 'merchant pool policy requires next version' USING ERRCODE='23514'; END IF;
 ELSIF NEW.version<>1 THEN RAISE EXCEPTION 'merchant pool policy must start at version one' USING ERRCODE='23514'; END IF;
 IF NEW.wallet_role='liquidity_hot' AND NOT EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes s JOIN sys_crypto_aml_batch_policies b ON b.merchant_id=s.merchant_id AND b.network_id=s.network_id AND b.token_id=s.token_id AND b.risk_class=NEW.risk_class
    WHERE s.merchant_id=NEW.merchant_id AND s.network_id=NEW.network_id AND s.token_id=NEW.token_id AND s.enabled) THEN
   RAISE EXCEPTION 'collector pool requires enabled AML scope and batch policy' USING ERRCODE='23514';
 END IF;
 IF NEW.enabled AND NEW.wallet_role='merchant_pool' AND NOT EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes WHERE merchant_id=NEW.merchant_id AND network_id=NEW.network_id AND enabled) THEN
   RAISE EXCEPTION 'deposit pool requires an enabled AML asset scope' USING ERRCODE='23514';
 END IF;
 IF NEW.enabled AND (NOT EXISTS(SELECT 1 FROM sys_crypto_aml_provisioning_identity WHERE id AND database_name=current_database())
   OR NOT EXISTS(SELECT 1 FROM sys_crypto_wallet_pool_policies p JOIN sys_crypto_wallet_derivation_cursors cursor ON cursor.network_id=p.network_id
     JOIN cat_blockchain_networks n ON n.id=p.network_id AND n.is_active AND NOT n.deletion_mark AND n.chain_family IN ('tron','evm')
     WHERE p.network_id=NEW.network_id AND p.enabled AND NOT p.needs_review AND NOT cursor.needs_review AND p.derivation_namespace<>'' AND p.derivation_namespace=cursor.derivation_namespace)) THEN
   RAISE EXCEPTION 'merchant pool requires enrolled database and ready network derivation policy' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_pool_policy_guard BEFORE INSERT OR UPDATE OR DELETE ON sys_crypto_aml_pool_policies FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_pool_policy_guard();
CREATE FUNCTION fn_crypto_aml_pool_policy_audit() RETURNS TRIGGER AS $$
BEGIN
 IF TG_OP='INSERT' OR NEW.version<>OLD.version THEN
   INSERT INTO reg_crypto_aml_pool_policy_versions(policy_id,version,enabled,low_watermark,target_ready,max_total,max_provision_batch,configured_by)
     VALUES(NEW.id,NEW.version,NEW.enabled,NEW.low_watermark,NEW.target_ready,NEW.max_total,NEW.max_provision_batch,NEW.configured_by);
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_pool_policy_audit AFTER INSERT OR UPDATE ON sys_crypto_aml_pool_policies FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_pool_policy_audit();

CREATE FUNCTION fn_crypto_aml_pool_intent_guard() RETURNS TRIGGER AS $$
DECLARE policy sys_crypto_aml_pool_policies; usage RECORD; network_max INT;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'merchant pool intents cannot be deleted' USING ERRCODE='23514'; END IF;
 IF TG_OP='UPDATE' THEN
   IF (NEW.id,NEW.policy_id,NEW.policy_version,NEW.plan_id,NEW.ordinal,NEW.created_at) IS DISTINCT FROM (OLD.id,OLD.policy_id,OLD.policy_version,OLD.plan_id,OLD.ordinal,OLD.created_at) OR NEW.batch_attempts<OLD.batch_attempts THEN
     RAISE EXCEPTION 'merchant pool intent identity is immutable' USING ERRCODE='23514';
   END IF;
   RETURN NEW;
 END IF;
 PERFORM 1 FROM sys_crypto_aml_pool_policies p JOIN sys_crypto_custody_policies c ON c.network_id=p.network_id AND c.enforced WHERE p.id=NEW.policy_id FOR UPDATE OF c;
 SELECT * INTO policy FROM sys_crypto_aml_pool_policies WHERE id=NEW.policy_id FOR UPDATE;
 SELECT * INTO usage FROM fn_crypto_aml_pool_counts(NEW.policy_id);
 SELECT max_total_wallets INTO network_max FROM sys_crypto_wallet_pool_policies WHERE network_id=policy.network_id AND enabled AND NOT needs_review FOR UPDATE;
 IF NOT EXISTS(SELECT 1 FROM crypto_aml_pool_runtime WHERE id=policy.id) OR policy.version<>NEW.policy_version OR NEW.ordinal>policy.max_provision_batch OR usage.ready>policy.low_watermark
    OR usage.ready+usage.pending+usage.queued>=policy.target_ready OR usage.total>=policy.max_total OR network_max IS NULL
    OR fn_crypto_aml_network_wallet_commitments(policy.network_id,NULL)>=network_max THEN
   RAISE EXCEPTION 'merchant pool intent exceeds readiness, policy or network budget' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_pool_intent_guard BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_aml_pool_intents FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_pool_intent_guard();

CREATE FUNCTION fn_crypto_aml_pool_cancel_guard() RETURNS TRIGGER AS $$
BEGIN
 PERFORM 1 FROM reg_crypto_aml_pool_intents i JOIN sys_crypto_aml_pool_policies p ON p.id=i.policy_id JOIN sys_crypto_custody_policies c ON c.network_id=p.network_id WHERE i.id=NEW.intent_id FOR UPDATE OF c;
 PERFORM 1 FROM reg_crypto_aml_pool_intents WHERE id=NEW.intent_id FOR UPDATE;
 IF EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_provisions WHERE id=NEW.intent_id) THEN RAISE EXCEPTION 'cannot cancel pool intent after derivation reservation' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_pool_cancel_guard BEFORE INSERT ON reg_crypto_aml_pool_intent_cancellations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_pool_cancel_guard();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN IF EXISTS(SELECT 1 FROM sys_crypto_aml_pool_policies) OR EXISTS(SELECT 1 FROM reg_crypto_aml_pool_intents) THEN RAISE EXCEPTION 'cannot roll back merchant pool history; use a forward migration'; END IF; END $$;
DROP TABLE reg_crypto_aml_pool_intent_cancellations;
DROP FUNCTION fn_crypto_aml_pool_cancel_guard();
DROP TRIGGER trg_crypto_aml_pool_intent_guard ON reg_crypto_aml_pool_intents;
DROP FUNCTION fn_crypto_aml_pool_intent_guard();
DROP TRIGGER trg_crypto_aml_pool_policy_audit ON sys_crypto_aml_pool_policies;
DROP FUNCTION fn_crypto_aml_pool_policy_audit();
DROP TRIGGER trg_crypto_aml_pool_policy_guard ON sys_crypto_aml_pool_policies;
DROP FUNCTION fn_crypto_aml_pool_policy_guard();
DROP FUNCTION fn_crypto_aml_network_wallet_commitments(UUID,UUID);
DROP FUNCTION fn_crypto_aml_pool_counts(UUID);
DROP TRIGGER trg_crypto_aml_pool_scope_hold ON sys_crypto_aml_routing_scopes;
DROP FUNCTION fn_crypto_aml_pool_scope_hold();
DROP TRIGGER trg_crypto_aml_pool_high_hold ON reg_crypto_wallet_risk_states;
DROP FUNCTION fn_crypto_aml_pool_high_hold();
DROP FUNCTION fn_crypto_aml_pool_baselines_ready(UUID);
DROP VIEW crypto_aml_pool_runtime;
DROP INDEX idx_crypto_aml_provision_pending_scope;
DROP TABLE reg_crypto_aml_pool_intents,reg_crypto_aml_pool_policy_versions,sys_crypto_aml_pool_policies;
-- +goose StatementEnd
