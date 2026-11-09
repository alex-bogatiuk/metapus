-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- NULL means a legacy decision whose original assessment clock is unknown.
-- Down retains this additive column so that no assessment evidence is lost.
ALTER TABLE reg_crypto_aml_decisions ADD COLUMN IF NOT EXISTS evaluated_at TIMESTAMPTZ;
CREATE FUNCTION fn_crypto_aml_decision_replay_guard() RETURNS TRIGGER AS $$
DECLARE prior reg_crypto_aml_decisions%ROWTYPE;
BEGIN
 IF current_setting('transaction_isolation')<>'read committed' THEN
  RAISE EXCEPTION 'AML decisions require read committed isolation' USING ERRCODE='23514';
 END IF;
 IF NEW.id IS NULL OR NEW.subject_id IS NULL OR NEW.report_id IS NULL OR NEW.generation IS NULL OR NEW.generation<=0
    OR NEW.policy_version IS NULL OR btrim(NEW.policy_version)='' OR NEW.risk_class IS NULL OR NEW.risk_class NOT IN ('low','medium','high','unknown')
    OR NEW.reason IS NULL OR btrim(NEW.reason)='' OR NEW._deleted_at IS NOT NULL THEN
  RAISE EXCEPTION 'AML decision requires its complete assessment' USING ERRCODE='23514';
 END IF;
 PERFORM 1 FROM reg_crypto_aml_reports r JOIN reg_crypto_aml_subjects s ON s.id=r.subject_id
 JOIN sys_crypto_aml_routing_scopes scope ON scope.merchant_id=s.merchant_id AND scope.network_id=s.network_id AND scope.token_id=s.token_id
 WHERE r.id=NEW.report_id AND s.id=NEW.subject_id FOR UPDATE OF scope NOWAIT;
 IF NOT EXISTS(SELECT 1 FROM reg_crypto_aml_reports r JOIN reg_crypto_aml_provider_checks c ON c.id=r.provider_check_id
   JOIN reg_crypto_aml_jobs j ON j.id=c.job_id JOIN sys_crypto_aml_risk_policies p ON p.version=NEW.policy_version
   WHERE r.id=NEW.report_id AND r.subject_id=NEW.subject_id AND j.subject_id=NEW.subject_id AND j.generation=NEW.generation) THEN
  RAISE EXCEPTION 'AML decision report, generation or policy changed' USING ERRCODE='23514';
 END IF;
 SELECT * INTO prior FROM reg_crypto_aml_decisions WHERE subject_id=NEW.subject_id AND generation=NEW.generation AND report_id=NEW.report_id AND policy_version=NEW.policy_version;
 IF FOUND THEN
  IF (prior.risk_class,prior.reason,prior.expires_at,prior.evaluated_at) IS DISTINCT FROM (NEW.risk_class,NEW.reason,NEW.expires_at,NEW.evaluated_at) THEN
   RAISE EXCEPTION 'AML decision replay differs from its original assessment' USING ERRCODE='23514';
  END IF;
  RETURN NULL;
 END IF;
 IF NEW.evaluated_at IS NULL OR NOT isfinite(NEW.evaluated_at) THEN
  RAISE EXCEPTION 'new AML decision requires an assessment clock' USING ERRCODE='23514';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM reg_crypto_aml_subjects s JOIN sys_crypto_aml_routing_scopes scope
   ON scope.merchant_id=s.merchant_id AND scope.network_id=s.network_id AND scope.token_id=s.token_id
   WHERE s.id=NEW.subject_id AND scope.enabled AND scope.policy_version=NEW.policy_version) THEN
  RAISE EXCEPTION 'AML decision routing policy changed' USING ERRCODE='23514';
 END IF;
 IF (NEW.risk_class='unknown' AND NEW.expires_at IS NOT NULL) OR (NEW.risk_class<>'unknown' AND NOT EXISTS(
   SELECT 1 FROM sys_crypto_aml_risk_policies p JOIN reg_crypto_aml_reports r ON r.id=NEW.report_id
   WHERE p.version=NEW.policy_version AND NEW.expires_at IS NOT NULL AND isfinite(NEW.expires_at)
   AND NEW.expires_at>NEW.evaluated_at AND NEW.expires_at<=NEW.evaluated_at+make_interval(secs=>p.decision_ttl_seconds)
   AND NEW.expires_at<=r.completed_at+make_interval(secs=>p.max_report_age_seconds))) THEN
  RAISE EXCEPTION 'AML decision expiry differs from its assessment bounds' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_decision_replay BEFORE INSERT ON reg_crypto_aml_decisions FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_decision_replay_guard();

ALTER TABLE sys_invoice_reconciliation_drift
 ADD COLUMN episode_id UUID,
 ADD COLUMN initial_details JSONB CHECK(initial_details IS NULL OR jsonb_typeof(initial_details)='object'),
 ADD COLUMN incident_id UUID REFERENCES reg_crypto_security_incidents(id);
CREATE UNIQUE INDEX uq_crypto_invoice_drift_episode ON sys_invoice_reconciliation_drift(episode_id) WHERE episode_id IS NOT NULL;
CREATE UNIQUE INDEX uq_crypto_invoice_drift_incident ON sys_invoice_reconciliation_drift(incident_id) WHERE incident_id IS NOT NULL;

-- A trigger already owns its target row. Never wait for the parent invoice
-- from there: this same nonblocking gate is used by the application first.
CREATE FUNCTION fn_crypto_lock_invoice_drift(invoice UUID) RETURNS UUID AS $$
DECLARE network UUID;
BEGIN
 IF current_setting('transaction_isolation')<>'read committed' THEN
  RAISE EXCEPTION 'invoice drift requires read committed isolation' USING ERRCODE='23514';
 END IF;
 SELECT t.network_id INTO network FROM doc_crypto_invoices i JOIN cat_tokens t ON t.id=i.token_id WHERE i.id=invoice FOR UPDATE OF i NOWAIT;
 IF NOT FOUND THEN RAISE EXCEPTION 'invoice drift invoice is missing' USING ERRCODE='23503'; END IF;
 RETURN network;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_invoice_drift_details(invoice UUID) RETURNS JSONB AS $$
 SELECT jsonb_build_object('episodeId',episode_id,'firstSeenAt',first_seen_at,'firstDetails',initial_details,
   'consecutiveCycles',consecutive_cycles,'reason',details->>'error')
 FROM sys_invoice_reconciliation_drift WHERE invoice_id=invoice
$$ LANGUAGE sql STABLE;

CREATE FUNCTION fn_crypto_invoice_drift_guard() RETURNS TRIGGER AS $$
DECLARE network UUID; at TIMESTAMPTZ := clock_timestamp();
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'invoice drift history cannot be deleted' USING ERRCODE='23514'; END IF;
 network := fn_crypto_lock_invoice_drift(NEW.invoice_id);
 IF TG_OP='INSERT' THEN
  IF NEW.episode_id IS NULL OR NEW.initial_details IS NULL OR NEW.initial_details IS DISTINCT FROM NEW.details
     OR NEW.consecutive_cycles IS DISTINCT FROM 1 OR NEW.resolved_at IS NOT NULL OR NEW.incident_id IS NOT NULL THEN
   RAISE EXCEPTION 'new invoice drift requires its first observation' USING ERRCODE='23514';
  END IF;
  NEW.first_seen_at := at; NEW.last_seen_at := at;
  RETURN NEW;
 END IF;
 IF NEW.invoice_id IS DISTINCT FROM OLD.invoice_id THEN RAISE EXCEPTION 'invoice drift identity is immutable' USING ERRCODE='23514'; END IF;
 IF OLD.resolved_at IS NOT NULL THEN
  IF NEW IS NOT DISTINCT FROM OLD THEN RETURN NULL; END IF;
  IF NEW.resolved_at IS NOT NULL OR NEW.episode_id IS NULL OR NEW.episode_id IS NOT DISTINCT FROM OLD.episode_id
     OR NEW.initial_details IS NULL OR NEW.initial_details IS DISTINCT FROM NEW.details OR NEW.incident_id IS NOT NULL OR NEW.consecutive_cycles IS DISTINCT FROM 1 THEN
   RAISE EXCEPTION 'resolved invoice drift requires a new episode' USING ERRCODE='23514';
  END IF;
  NEW.first_seen_at := at; NEW.last_seen_at := at;
  RETURN NEW;
 END IF;
 IF (NEW.episode_id,NEW.initial_details,NEW.first_seen_at) IS DISTINCT FROM (OLD.episode_id,OLD.initial_details,OLD.first_seen_at)
    OR (OLD.incident_id IS NOT NULL AND NEW.incident_id IS DISTINCT FROM OLD.incident_id) THEN
  RAISE EXCEPTION 'invoice drift first observation is immutable' USING ERRCODE='23514';
 END IF;
 IF NEW.resolved_at IS NOT NULL THEN
  IF (to_jsonb(NEW)-'resolved_at') IS DISTINCT FROM (to_jsonb(OLD)-'resolved_at') THEN
   RAISE EXCEPTION 'invoice drift resolution cannot change its observation' USING ERRCODE='23514';
  END IF;
  NEW.resolved_at := at;
 ELSE
  IF NEW.consecutive_cycles=OLD.consecutive_cycles+1 THEN
   NEW.last_seen_at := at;
  ELSIF (to_jsonb(NEW)-'incident_id') IS DISTINCT FROM (to_jsonb(OLD)-'incident_id') THEN
   RAISE EXCEPTION 'invoice drift update requires another observation' USING ERRCODE='23514';
  END IF;
 END IF;
 IF NEW.incident_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM reg_crypto_security_incidents incident
   WHERE incident.id=NEW.incident_id AND incident.network_id=network AND incident.invoice_id=NEW.invoice_id AND incident.payment_id IS NULL
   AND incident.incident_kind='invoice_projection_drift' AND incident.event_kind='native_value' AND incident.event_index=0
   AND incident.tx_hash=NEW.invoice_id::text||CASE WHEN NEW.episode_id IS NULL THEN '' ELSE ':'||NEW.episode_id::text END
   AND ((NEW.resolved_at IS NULL AND incident.status IN ('open','acknowledged')) OR (NEW.resolved_at IS NOT NULL AND incident.status='resolved'))) THEN
  RAISE EXCEPTION 'invoice drift incident link changed' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_invoice_drift_guard BEFORE INSERT OR UPDATE OR DELETE ON sys_invoice_reconciliation_drift FOR EACH ROW EXECUTE FUNCTION fn_crypto_invoice_drift_guard();

CREATE FUNCTION fn_crypto_invoice_drift_incident_guard() RETURNS TRIGGER AS $$
DECLARE network UUID; prior reg_crypto_security_incidents%ROWTYPE;
BEGIN
 IF TG_OP='DELETE' THEN
  IF OLD.incident_kind='invoice_projection_drift' THEN RAISE EXCEPTION 'invoice drift incident cannot be deleted' USING ERRCODE='23514'; END IF;
  RETURN OLD;
 END IF;
 IF NEW.incident_kind<>'invoice_projection_drift' AND (TG_OP='INSERT' OR OLD.incident_kind<>'invoice_projection_drift') THEN RETURN NEW; END IF;
 network := fn_crypto_lock_invoice_drift(NEW.invoice_id);
 IF NEW.id IS NULL OR NEW.tx_hash IS NULL OR NEW.network_id IS DISTINCT FROM network OR NEW.payment_id IS NOT NULL OR NEW.event_kind IS DISTINCT FROM 'native_value'
    OR NEW.event_index IS DISTINCT FROM 0 OR NEW.incident_kind IS DISTINCT FROM 'invoice_projection_drift' OR NEW.details IS NULL OR jsonb_typeof(NEW.details)<>'object' THEN
  RAISE EXCEPTION 'invoice drift incident identity changed' USING ERRCODE='23514';
 END IF;
 IF TG_OP='INSERT' THEN
  SELECT * INTO prior FROM reg_crypto_security_incidents WHERE network_id=NEW.network_id AND incident_kind=NEW.incident_kind
    AND tx_hash=NEW.tx_hash AND event_kind=NEW.event_kind AND event_index=NEW.event_index;
  IF FOUND THEN
   IF (prior.invoice_id,prior.payment_id,prior.details) IS DISTINCT FROM (NEW.invoice_id,NEW.payment_id,NEW.details) THEN
    RAISE EXCEPTION 'invoice drift incident replay differs from its first observation' USING ERRCODE='23514';
   END IF;
   RETURN NULL;
  END IF;
  IF NEW.status IS DISTINCT FROM 'open' OR NEW.acknowledged_at IS NOT NULL OR NEW.resolved_at IS NOT NULL OR NOT EXISTS(
    SELECT 1 FROM sys_invoice_reconciliation_drift d WHERE d.invoice_id=NEW.invoice_id AND d.resolved_at IS NULL AND d.consecutive_cycles>=2
    AND NEW.tx_hash=d.invoice_id::text||CASE WHEN d.episode_id IS NULL THEN '' ELSE ':'||d.episode_id::text END
    AND NEW.details=fn_crypto_invoice_drift_details(d.invoice_id)) THEN
   RAISE EXCEPTION 'invoice drift incident requires its current observation' USING ERRCODE='23514';
  END IF;
  NEW.created_at := clock_timestamp();
  RETURN NEW;
 END IF;
 IF (to_jsonb(NEW)-ARRAY['status','acknowledged_at','resolved_at']) IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['status','acknowledged_at','resolved_at']) THEN
  RAISE EXCEPTION 'invoice drift incident first observation is immutable' USING ERRCODE='23514';
 END IF;
 IF OLD.status='resolved' THEN
  IF NEW.status<>'resolved' THEN RAISE EXCEPTION 'resolved invoice drift incident cannot reopen' USING ERRCODE='23514'; END IF;
  RETURN NULL;
 END IF;
 IF NEW.status=OLD.status THEN RETURN NULL; END IF;
 IF NEW.status='resolved' THEN
  NEW.acknowledged_at := OLD.acknowledged_at; NEW.resolved_at := clock_timestamp();
 ELSIF OLD.status='open' AND NEW.status='acknowledged' THEN
  NEW.acknowledged_at := clock_timestamp(); NEW.resolved_at := NULL;
 ELSE RAISE EXCEPTION 'invoice drift incident transition is invalid' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_invoice_drift_incident_guard BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_security_incidents FOR EACH ROW EXECUTE FUNCTION fn_crypto_invoice_drift_incident_guard();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM sys_invoice_reconciliation_drift WHERE episode_id IS NOT NULL OR initial_details IS NOT NULL OR incident_id IS NOT NULL)
    OR EXISTS(SELECT 1 FROM reg_crypto_security_incidents WHERE incident_kind='invoice_projection_drift' AND details ? 'firstDetails') THEN
  RAISE EXCEPTION 'invoice drift episodes require forward recovery' USING ERRCODE='23514';
 END IF;
END $$;
DROP TRIGGER trg_crypto_invoice_drift_incident_guard ON reg_crypto_security_incidents;
DROP FUNCTION fn_crypto_invoice_drift_incident_guard();
DROP TRIGGER trg_crypto_invoice_drift_guard ON sys_invoice_reconciliation_drift;
DROP FUNCTION fn_crypto_invoice_drift_guard();
DROP FUNCTION fn_crypto_invoice_drift_details(UUID);
DROP FUNCTION fn_crypto_lock_invoice_drift(UUID);
ALTER TABLE sys_invoice_reconciliation_drift DROP COLUMN incident_id,DROP COLUMN initial_details,DROP COLUMN episode_id;
DROP TRIGGER trg_crypto_aml_decision_replay ON reg_crypto_aml_decisions;
DROP FUNCTION fn_crypto_aml_decision_replay_guard();
-- evaluated_at deliberately remains: old binaries ignore it, Up reuses it.
-- +goose StatementEnd
