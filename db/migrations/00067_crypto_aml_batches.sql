-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

CREATE TABLE sys_crypto_aml_batch_policies (
 merchant_id UUID NOT NULL, network_id UUID NOT NULL, token_id UUID NOT NULL,
 risk_class TEXT NOT NULL CHECK(risk_class IN ('low','medium')),
 version BIGINT NOT NULL CHECK(version>0),
 max_gross NUMERIC(78,0) NOT NULL CHECK(max_gross>0),
 max_age_seconds INT NOT NULL CHECK(max_age_seconds BETWEEN 1 AND 2592000),
 configured_by TEXT NOT NULL CHECK(btrim(configured_by)<>''),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
 PRIMARY KEY(merchant_id,network_id,token_id,risk_class,version),
 FOREIGN KEY(merchant_id,network_id,token_id) REFERENCES sys_crypto_aml_routing_scopes(merchant_id,network_id,token_id)
);
CREATE TRIGGER trg_crypto_aml_batch_policy_immutable BEFORE UPDATE OR DELETE ON sys_crypto_aml_batch_policies FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TABLE reg_crypto_aml_batches (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
 merchant_id UUID NOT NULL, network_id UUID NOT NULL, token_id UUID NOT NULL,
 risk_class TEXT NOT NULL CHECK(risk_class IN ('low','medium')),
 policy_version BIGINT NOT NULL, scope_version BIGINT NOT NULL CHECK(scope_version>0),
 collector_wallet_id UUID NOT NULL UNIQUE,
 max_gross NUMERIC(78,0) NOT NULL CHECK(max_gross>0),
 opened_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), intake_closes_at TIMESTAMPTZ NOT NULL,
 intake_closed_at TIMESTAMPTZ, close_reason TEXT NOT NULL DEFAULT '',
 execution_held BOOLEAN NOT NULL DEFAULT FALSE, hold_reason TEXT NOT NULL DEFAULT '',
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 execution_revision BIGINT NOT NULL DEFAULT 1 CHECK(execution_revision>0),
 request_digest TEXT NOT NULL CHECK(request_digest<>''),
 created_by TEXT NOT NULL CHECK(btrim(created_by)<>''), last_actor TEXT NOT NULL CHECK(btrim(last_actor)<>''),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
 FOREIGN KEY(merchant_id,network_id,token_id,risk_class,policy_version) REFERENCES sys_crypto_aml_batch_policies(merchant_id,network_id,token_id,risk_class,version),
 FOREIGN KEY(collector_wallet_id,network_id,merchant_id) REFERENCES cat_wallets(id,network_id,custody_merchant_id),
 CHECK(intake_closes_at>opened_at),
 CHECK((intake_closed_at IS NULL AND close_reason='') OR (intake_closed_at IS NOT NULL AND close_reason<>'')),
 CHECK(execution_held=(hold_reason<>'')),
 CHECK(NOT execution_held OR intake_closed_at IS NOT NULL)
);
CREATE INDEX idx_crypto_aml_batches_open ON reg_crypto_aml_batches(merchant_id,network_id,token_id,risk_class,intake_closes_at,id) WHERE intake_closed_at IS NULL;
CREATE TRIGGER trg_crypto_aml_batch_updated BEFORE UPDATE ON reg_crypto_aml_batches FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_crypto_aml_batch_txid BEFORE UPDATE ON reg_crypto_aml_batches FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TABLE reg_crypto_aml_batch_events (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(), batch_id UUID NOT NULL REFERENCES reg_crypto_aml_batches(id),
 event_kind TEXT NOT NULL CHECK(event_kind IN ('opened','closed','held','reserved','applied','cancelled')),
 allocation_id UUID REFERENCES reg_crypto_aml_internal_allocations(id),
 reason TEXT NOT NULL CHECK(reason<>''), recorded_by TEXT NOT NULL CHECK(btrim(recorded_by)<>''),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
 UNIQUE(batch_id,event_kind,allocation_id)
);
CREATE TRIGGER trg_crypto_aml_batch_event_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_batch_events FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

ALTER TABLE reg_crypto_aml_internal_allocations ADD COLUMN batch_id UUID REFERENCES reg_crypto_aml_batches(id),
 ADD COLUMN batch_revision BIGINT, ADD COLUMN batch_execution_revision BIGINT,
 ADD CONSTRAINT chk_crypto_aml_allocation_batch CHECK((batch_id IS NULL AND batch_revision IS NULL AND batch_execution_revision IS NULL)
   OR (batch_id IS NOT NULL AND batch_revision>0 AND batch_execution_revision>0));
CREATE INDEX idx_crypto_aml_allocation_batch ON reg_crypto_aml_internal_allocations(batch_id,status) WHERE batch_id IS NOT NULL;

CREATE VIEW crypto_aml_batch_usage AS
 SELECT b.id,COALESCE(plans.committed,0) committed,COALESCE(plans.reserved,0) reserved,COALESCE(unplanned.amount,0) unplanned,
        COALESCE(plans.committed,0)+COALESCE(plans.reserved,0)+COALESCE(unplanned.amount,0) used
 FROM reg_crypto_aml_batches b
 LEFT JOIN LATERAL(SELECT SUM(gross) FILTER(WHERE status='applied') committed,SUM(gross) FILTER(WHERE status='reserved') reserved
   FROM reg_crypto_aml_internal_allocations WHERE batch_id=b.id) plans ON TRUE
 LEFT JOIN LATERAL(SELECT SUM(e.amount) amount FROM reg_crypto_aml_effects e JOIN reg_crypto_aml_effect_chain_states cs ON cs.effect_id=e.id AND cs.canonicality='canonical'
   WHERE e.wallet_id=b.collector_wallet_id AND e.token_id=b.token_id AND NOT EXISTS(
     SELECT 1 FROM reg_crypto_expected_chain_effects expected JOIN reg_crypto_signing_authorizations auth ON auth.id=expected.signing_authorization_id AND auth.basis_type='CryptoSweepAttempt'
     JOIN reg_crypto_aml_internal_allocations plan ON plan.sweep_attempt_id=auth.basis_id AND plan.batch_id=b.id AND plan.status IN ('reserved','applied')
     WHERE expected.id=e.expected_effect_id AND plan.source_wallet_id=e.source_wallet_id AND plan.destination_wallet_id=e.wallet_id AND plan.token_id=e.token_id AND plan.gross=e.amount)) unplanned ON TRUE;

CREATE FUNCTION fn_crypto_aml_batch_policy_guard() RETURNS TRIGGER AS $$
BEGIN
 PERFORM 1 FROM sys_crypto_aml_routing_scopes s JOIN cat_tokens t ON t.id=s.token_id AND t.token_standard<>'native'
   JOIN sys_crypto_custody_policies custody ON custody.network_id=s.network_id AND custody.enforced
   JOIN sys_crypto_network_policies network ON network.network_id=s.network_id AND NOT network.enabled
   WHERE s.merchant_id=NEW.merchant_id AND s.network_id=NEW.network_id AND s.token_id=NEW.token_id AND s.enabled FOR UPDATE OF s;
 IF NOT FOUND OR NEW.version<>(SELECT COALESCE(MAX(version),0)+1 FROM sys_crypto_aml_batch_policies WHERE merchant_id=NEW.merchant_id AND network_id=NEW.network_id AND token_id=NEW.token_id AND risk_class=NEW.risk_class)
    OR EXISTS(SELECT 1 FROM reg_crypto_aml_internal_allocations WHERE merchant_id=NEW.merchant_id AND network_id=NEW.network_id AND token_id=NEW.token_id AND risk_class=NEW.risk_class AND batch_id IS NULL AND status='reserved') THEN
   RAISE EXCEPTION 'batch policy requires paused owned scope, next version and drained legacy allocations' USING ERRCODE='23514';
 END IF;
 UPDATE reg_crypto_aml_batches SET intake_closed_at=COALESCE(intake_closed_at,NOW()),close_reason=CASE WHEN intake_closed_at IS NULL THEN 'policy_changed' ELSE close_reason END,
   execution_held=TRUE,hold_reason='policy_changed',last_actor=NEW.configured_by
 WHERE merchant_id=NEW.merchant_id AND network_id=NEW.network_id AND token_id=NEW.token_id AND risk_class=NEW.risk_class AND NOT execution_held;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_batch_policy_guard BEFORE INSERT ON sys_crypto_aml_batch_policies FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_batch_policy_guard();

CREATE FUNCTION fn_crypto_aml_batch_guard() RETURNS TRIGGER AS $$
DECLARE policy sys_crypto_aml_batch_policies; scoped sys_crypto_aml_routing_scopes;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'batch evidence cannot be deleted' USING ERRCODE='23514'; END IF;
 SELECT * INTO scoped FROM sys_crypto_aml_routing_scopes WHERE merchant_id=NEW.merchant_id AND network_id=NEW.network_id AND token_id=NEW.token_id FOR UPDATE;
 IF TG_OP='UPDATE' THEN
   IF (NEW.id,NEW.merchant_id,NEW.network_id,NEW.token_id,NEW.risk_class,NEW.policy_version,NEW.scope_version,NEW.collector_wallet_id,NEW.max_gross,NEW.opened_at,NEW.intake_closes_at,NEW.request_digest,NEW.created_by,NEW.created_at)
      IS DISTINCT FROM (OLD.id,OLD.merchant_id,OLD.network_id,OLD.token_id,OLD.risk_class,OLD.policy_version,OLD.scope_version,OLD.collector_wallet_id,OLD.max_gross,OLD.opened_at,OLD.intake_closes_at,OLD.request_digest,OLD.created_by,OLD.created_at)
      OR (OLD.intake_closed_at IS NOT NULL AND (NEW.intake_closed_at,NEW.close_reason) IS DISTINCT FROM (OLD.intake_closed_at,OLD.close_reason))
      OR (OLD.execution_held AND (NOT NEW.execution_held OR NEW.hold_reason<>OLD.hold_reason)) THEN
     RAISE EXCEPTION 'batch identity and closed/held history are immutable' USING ERRCODE='23514';
   END IF;
   NEW.revision:=OLD.revision+1;
   NEW.execution_revision:=OLD.execution_revision+CASE WHEN NOT OLD.execution_held AND NEW.execution_held THEN 1 ELSE 0 END;
   RETURN NEW;
 END IF;
 SELECT * INTO policy FROM sys_crypto_aml_batch_policies WHERE merchant_id=NEW.merchant_id AND network_id=NEW.network_id AND token_id=NEW.token_id AND risk_class=NEW.risk_class ORDER BY version DESC LIMIT 1;
 IF policy.version IS NULL OR NOT scoped.enabled OR scoped.version<>NEW.scope_version OR policy.version<>NEW.policy_version OR policy.max_gross<>NEW.max_gross
    OR NEW.opened_at IS DISTINCT FROM transaction_timestamp() OR NEW.intake_closes_at<>NEW.opened_at+make_interval(secs=>policy.max_age_seconds)
    OR NEW.intake_closed_at IS NOT NULL OR NEW.execution_held OR NEW.revision<>1 OR NEW.execution_revision<>1
    OR NOT EXISTS(SELECT 1 FROM cat_wallets w JOIN reg_crypto_aml_inventory_positions inventory ON inventory.wallet_id=w.id AND inventory.token_id=NEW.token_id
       JOIN reg_crypto_wallet_risk_states risk ON risk.wallet_id=w.id AND risk.token_id=NEW.token_id AND risk.scope_version=NEW.scope_version AND risk.risk_class<>'high'
       WHERE w.id=NEW.collector_wallet_id AND w.wallet_role='liquidity_hot' AND w.custody_merchant_id=NEW.merchant_id AND w.network_id=NEW.network_id AND w.is_active AND NOT w.deletion_mark AND w.chain_activation_status='active'
         AND inventory.principal+inventory.service_fee=0 AND fn_crypto_aml_total_inventory_covered(w.id,NEW.token_id)
         AND NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_allocation_holds WHERE wallet_id=w.id AND status='open'))
    OR EXISTS(SELECT 1 FROM reg_crypto_aml_effects WHERE wallet_id=NEW.collector_wallet_id AND token_id=NEW.token_id)
    OR EXISTS(SELECT 1 FROM reg_crypto_balance_movements WHERE wallet_id=NEW.collector_wallet_id AND token_id=NEW.token_id)
    OR EXISTS(SELECT 1 FROM reg_crypto_aml_effects e JOIN cat_tokens t ON t.id=e.token_id AND t.token_standard<>'native' WHERE e.wallet_id=NEW.collector_wallet_id AND e.token_id<>NEW.token_id)
    OR EXISTS(SELECT 1 FROM reg_crypto_balance_balances balance JOIN cat_tokens t ON t.id=balance.token_id AND t.token_standard<>'native' WHERE balance.wallet_id=NEW.collector_wallet_id AND balance.token_id<>NEW.token_id AND balance.amount<>0)
    OR EXISTS(SELECT 1 FROM reg_crypto_aml_internal_allocations WHERE destination_wallet_id=NEW.collector_wallet_id OR source_wallet_id=NEW.collector_wallet_id) THEN
   RAISE EXCEPTION 'batch requires current policy and a fresh empty owned collector' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_batch_guard BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_aml_batches FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_batch_guard();

CREATE FUNCTION fn_crypto_aml_batch_audit() RETURNS TRIGGER AS $$
BEGIN
 IF TG_OP='INSERT' THEN
   INSERT INTO reg_crypto_aml_batch_events(batch_id,event_kind,reason,recorded_by) VALUES(NEW.id,'opened','collector_enrolled',NEW.created_by);
 ELSE
   IF OLD.intake_closed_at IS NULL AND NEW.intake_closed_at IS NOT NULL THEN
     INSERT INTO reg_crypto_aml_batch_events(batch_id,event_kind,reason,recorded_by) VALUES(NEW.id,'closed',NEW.close_reason,NEW.last_actor);
   END IF;
   IF NOT OLD.execution_held AND NEW.execution_held THEN
     INSERT INTO reg_crypto_aml_batch_events(batch_id,event_kind,reason,recorded_by) VALUES(NEW.id,'held',NEW.hold_reason,NEW.last_actor);
   END IF;
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_batch_audit AFTER INSERT OR UPDATE ON reg_crypto_aml_batches FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_batch_audit();

CREATE FUNCTION fn_crypto_aml_batch_allocation_guard() RETURNS TRIGGER AS $$
DECLARE batch reg_crypto_aml_batches; used NUMERIC;
BEGIN
 PERFORM 1 FROM sys_crypto_aml_routing_scopes WHERE merchant_id=NEW.merchant_id AND network_id=NEW.network_id AND token_id=NEW.token_id FOR UPDATE;
 IF TG_OP='UPDATE' THEN
   IF (NEW.batch_id,NEW.batch_revision,NEW.batch_execution_revision) IS DISTINCT FROM (OLD.batch_id,OLD.batch_revision,OLD.batch_execution_revision) THEN
     RAISE EXCEPTION 'allocation batch identity is immutable' USING ERRCODE='23514';
   END IF;
   RETURN NEW;
 END IF;
 IF NEW.batch_id IS NULL THEN
   IF EXISTS(SELECT 1 FROM reg_crypto_aml_batches WHERE collector_wallet_id=NEW.destination_wallet_id OR collector_wallet_id=NEW.source_wallet_id)
      OR EXISTS(SELECT 1 FROM sys_crypto_aml_batch_policies WHERE merchant_id=NEW.merchant_id AND network_id=NEW.network_id AND token_id=NEW.token_id AND risk_class=NEW.risk_class) THEN
     RAISE EXCEPTION 'AML consolidation requires an explicit batch' USING ERRCODE='23514';
   END IF;
   RETURN NEW;
 END IF;
 SELECT * INTO batch FROM reg_crypto_aml_batches WHERE id=NEW.batch_id FOR UPDATE;
 SELECT usage.used INTO used FROM crypto_aml_batch_usage usage WHERE usage.id=NEW.batch_id;
 IF batch.id IS NULL OR (batch.merchant_id,batch.network_id,batch.token_id,batch.risk_class,batch.collector_wallet_id,batch.scope_version,batch.revision,batch.execution_revision)
    IS DISTINCT FROM (NEW.merchant_id,NEW.network_id,NEW.token_id,NEW.risk_class,NEW.destination_wallet_id,NEW.scope_version,NEW.batch_revision,NEW.batch_execution_revision)
    OR batch.intake_closed_at IS NOT NULL OR batch.intake_closes_at<=clock_timestamp() OR batch.execution_held
    OR used+NEW.principal+NEW.service_fee>batch.max_gross THEN
   RAISE EXCEPTION 'batch is closed, stale, held or lacks capacity' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_00_batch_allocation_guard BEFORE INSERT OR UPDATE ON reg_crypto_aml_internal_allocations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_batch_allocation_guard();

CREATE FUNCTION fn_crypto_aml_batch_allocation_audit() RETURNS TRIGGER AS $$
DECLARE event TEXT; usage NUMERIC;
BEGIN
 IF NEW.batch_id IS NULL THEN RETURN NEW; END IF;
 IF TG_OP='UPDATE' AND NEW.status=OLD.status THEN RETURN NEW; END IF;
 event:=CASE WHEN TG_OP='INSERT' THEN 'reserved' ELSE NEW.status END;
 INSERT INTO reg_crypto_aml_batch_events(batch_id,event_kind,allocation_id,reason,recorded_by) VALUES(NEW.batch_id,event,NEW.id,'allocation_'||event,'aml-inventory');
 SELECT used INTO usage FROM crypto_aml_batch_usage WHERE id=NEW.batch_id;
 IF usage>= (SELECT max_gross FROM reg_crypto_aml_batches WHERE id=NEW.batch_id) THEN
   UPDATE reg_crypto_aml_batches SET intake_closed_at=NOW(),close_reason='capacity',last_actor='aml-inventory' WHERE id=NEW.batch_id AND intake_closed_at IS NULL;
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_batch_allocation_audit AFTER INSERT OR UPDATE ON reg_crypto_aml_internal_allocations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_batch_allocation_audit();

CREATE FUNCTION fn_crypto_aml_batch_signing_guard() RETURNS TRIGGER AS $$
DECLARE plan reg_crypto_aml_internal_allocations; batch reg_crypto_aml_batches;
BEGIN
 IF NEW.basis_type<>'CryptoSweepAttempt' THEN RETURN NEW; END IF;
 SELECT * INTO plan FROM reg_crypto_aml_internal_allocations WHERE sweep_attempt_id=NEW.basis_id;
 IF plan.batch_id IS NULL THEN RETURN NEW; END IF;
 PERFORM 1 FROM sys_crypto_aml_routing_scopes WHERE merchant_id=plan.merchant_id AND network_id=plan.network_id AND token_id=plan.token_id FOR UPDATE;
 SELECT * INTO batch FROM reg_crypto_aml_batches WHERE id=plan.batch_id FOR UPDATE;
 IF NEW.purpose<>'sweep' OR batch.execution_held OR batch.execution_revision<>plan.batch_execution_revision
    OR NOT EXISTS(SELECT 1 FROM cat_wallets source JOIN cat_wallets collector ON collector.id=batch.collector_wallet_id
       WHERE source.id=plan.source_wallet_id AND source.wallet_role='merchant_pool' AND source.custody_merchant_id=batch.merchant_id AND source.network_id=batch.network_id AND source.is_active AND NOT source.deletion_mark
         AND collector.wallet_role='liquidity_hot' AND collector.custody_merchant_id=batch.merchant_id AND collector.network_id=batch.network_id AND collector.is_active AND NOT collector.deletion_mark AND collector.chain_activation_status='active'
         AND NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_allocation_holds hold WHERE hold.wallet_id IN(source.id,collector.id) AND hold.status='open'))
    OR EXISTS(SELECT 1 FROM reg_crypto_aml_effects e JOIN reg_crypto_aml_effect_chain_states cs ON cs.effect_id=e.id AND cs.canonicality='canonical'
       WHERE e.wallet_id=batch.collector_wallet_id AND e.token_id=batch.token_id AND e.source_wallet_id IS NULL)
    OR NOT EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes WHERE merchant_id=batch.merchant_id AND network_id=batch.network_id AND token_id=batch.token_id AND version=batch.scope_version AND enabled) THEN
   RAISE EXCEPTION 'batch execution is held or stale' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_01_batch_signing_guard BEFORE INSERT ON reg_crypto_signing_authorizations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_batch_signing_guard();

CREATE FUNCTION fn_crypto_aml_batch_observation() RETURNS TRIGGER AS $$
DECLARE target UUID; asset UUID; why TEXT;
BEGIN
 IF TG_TABLE_NAME='reg_crypto_wallet_risk_states' THEN
   IF NEW.risk_class<>'high' THEN RETURN NEW; END IF;
   target:=NEW.wallet_id; asset:=NEW.token_id; why:='late_high';
 ELSIF TG_TABLE_NAME='sys_crypto_aml_routing_scopes' THEN
   UPDATE reg_crypto_aml_batches SET intake_closed_at=COALESCE(intake_closed_at,NOW()),close_reason=CASE WHEN intake_closed_at IS NULL THEN 'scope_changed' ELSE close_reason END,
     execution_held=TRUE,hold_reason='scope_changed',last_actor='aml-routing' WHERE merchant_id=NEW.merchant_id AND network_id=NEW.network_id AND token_id=NEW.token_id AND scope_version<>NEW.version AND NOT execution_held;
   RETURN NEW;
 ELSE
   SELECT e.wallet_id,e.token_id INTO target,asset FROM reg_crypto_aml_effects e WHERE e.id=NEW.effect_id;
   IF NEW.canonicality<>'canonical' THEN why:='chain_changed';
   ELSIF NOT EXISTS(SELECT 1 FROM crypto_aml_batch_usage usage JOIN reg_crypto_aml_batches b ON b.id=usage.id WHERE b.collector_wallet_id=target AND b.token_id=asset AND usage.unplanned>0) THEN RETURN NEW;
   ELSE why:='unplanned_incoming'; END IF;
 END IF;
 PERFORM 1 FROM cat_wallets w JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=w.custody_merchant_id AND s.network_id=w.network_id AND s.token_id=asset WHERE w.id=target FOR UPDATE OF s;
 UPDATE reg_crypto_aml_batches b SET intake_closed_at=COALESCE(intake_closed_at,NOW()),close_reason=CASE WHEN intake_closed_at IS NULL THEN why ELSE close_reason END,
   execution_held=TRUE,hold_reason=why,last_actor='aml-routing' WHERE b.token_id=asset AND NOT b.execution_held AND
   (b.collector_wallet_id=target OR EXISTS(SELECT 1 FROM reg_crypto_aml_internal_allocations plan WHERE plan.batch_id=b.id AND plan.source_wallet_id=target AND plan.status IN ('reserved','applied')));
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_batch_risk AFTER INSERT OR UPDATE ON reg_crypto_wallet_risk_states FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_batch_observation();
CREATE TRIGGER trg_crypto_aml_batch_scope AFTER UPDATE ON sys_crypto_aml_routing_scopes FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_batch_observation();
CREATE TRIGGER trg_crypto_aml_batch_chain AFTER INSERT OR UPDATE ON reg_crypto_aml_effect_chain_states FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_batch_observation();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN IF EXISTS(SELECT 1 FROM reg_crypto_aml_batches) OR EXISTS(SELECT 1 FROM sys_crypto_aml_batch_policies) THEN RAISE EXCEPTION 'cannot roll back AML batch evidence; use a forward migration'; END IF; END $$;
DROP TRIGGER trg_crypto_aml_batch_chain ON reg_crypto_aml_effect_chain_states;
DROP TRIGGER trg_crypto_aml_batch_scope ON sys_crypto_aml_routing_scopes;
DROP TRIGGER trg_crypto_aml_batch_risk ON reg_crypto_wallet_risk_states;
DROP FUNCTION fn_crypto_aml_batch_observation();
DROP TRIGGER trg_crypto_aml_01_batch_signing_guard ON reg_crypto_signing_authorizations;
DROP FUNCTION fn_crypto_aml_batch_signing_guard();
DROP TRIGGER trg_crypto_aml_batch_allocation_audit ON reg_crypto_aml_internal_allocations;
DROP FUNCTION fn_crypto_aml_batch_allocation_audit();
DROP TRIGGER trg_crypto_aml_00_batch_allocation_guard ON reg_crypto_aml_internal_allocations;
DROP FUNCTION fn_crypto_aml_batch_allocation_guard();
DROP TRIGGER trg_crypto_aml_batch_policy_guard ON sys_crypto_aml_batch_policies;
DROP FUNCTION fn_crypto_aml_batch_policy_guard();
DROP VIEW crypto_aml_batch_usage;
ALTER TABLE reg_crypto_aml_internal_allocations DROP COLUMN batch_id,DROP COLUMN batch_revision,DROP COLUMN batch_execution_revision;
DROP TABLE reg_crypto_aml_batch_events;
DROP TABLE reg_crypto_aml_batches;
DROP FUNCTION fn_crypto_aml_batch_audit();
DROP FUNCTION fn_crypto_aml_batch_guard();
DROP TABLE sys_crypto_aml_batch_policies;
-- +goose StatementEnd
