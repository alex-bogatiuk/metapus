-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE reg_crypto_external_outflows ADD COLUMN IF NOT EXISTS classification_basis JSONB, ADD COLUMN IF NOT EXISTS resolution_basis JSONB;
ALTER TABLE doc_crypto_custody_adjustments ADD COLUMN IF NOT EXISTS decision_basis JSONB;
ALTER TABLE reg_crypto_external_outflow_actions ADD COLUMN IF NOT EXISTS decision_basis JSONB;

CREATE FUNCTION fn_crypto_outflow_decision_insert_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='reg_crypto_external_outflows' THEN
  IF NEW.classification_basis IS NOT NULL OR NEW.resolution_basis IS NOT NULL OR NEW.classified_by IS NOT NULL OR NEW.classified_at IS NOT NULL
    OR NEW.resolved_by IS NOT NULL OR NEW.resolved_at IS NOT NULL THEN
   RAISE EXCEPTION 'new outflow cannot start with a decision' USING ERRCODE='23514';
  END IF;
 ELSE
  IF NEW.decision_basis IS NOT NULL OR NEW.decision_by IS NOT NULL OR NEW.decided_at IS NOT NULL OR NEW.decision_reason<>'' OR NEW.posted_version<>0 THEN
   RAISE EXCEPTION 'new custody adjustment cannot start with a decision' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER trg_crypto_outflow_initial_decision BEFORE INSERT ON reg_crypto_external_outflows FOR EACH ROW EXECUTE FUNCTION fn_crypto_outflow_decision_insert_guard();
CREATE TRIGGER trg_crypto_custody_initial_decision BEFORE INSERT ON doc_crypto_custody_adjustments FOR EACH ROW EXECUTE FUNCTION fn_crypto_outflow_decision_insert_guard();

CREATE FUNCTION fn_crypto_outflow_decision_command(kind TEXT,target UUID,actor UUID,reason TEXT,classification TEXT DEFAULT NULL,basis_kind TEXT DEFAULT NULL,basis UUID DEFAULT NULL,evidence JSONB DEFAULT NULL)
RETURNS JSONB LANGUAGE sql IMMUTABLE AS $$
 SELECT jsonb_build_object('kind',kind,'targetId',target,'actorId',actor,'reason',reason) ||
 CASE WHEN kind='classify' THEN jsonb_build_object('classification',classification,'basisType',basis_kind,'basisId',basis,'evidence',COALESCE(evidence,'{}'::jsonb)) ELSE '{}'::jsonb END
$$;
CREATE FUNCTION fn_crypto_outflow_decision_seed(basis JSONB) RETURNS UUID LANGUAGE plpgsql AS $$
DECLARE value UUID;
BEGIN
 IF basis IS NULL OR jsonb_typeof(basis)<>'object' OR (basis-'id')<>'{}'::jsonb OR basis->>'id' IS NULL THEN
  RAISE EXCEPTION 'new outflow decision requires a generated decision ID' USING ERRCODE='23514';
 END IF;
 value:=(basis->>'id')::uuid;
 IF value='00000000-0000-0000-0000-000000000000'::uuid THEN RAISE EXCEPTION 'outflow decision ID is required' USING ERRCODE='23514'; END IF;
 RETURN value;
END; $$;
CREATE FUNCTION fn_crypto_outflow_decision_source(target UUID) RETURNS JSONB LANGUAGE sql STABLE AS $$
 SELECT jsonb_build_object('outflowId',id,'networkId',network_id,'walletId',source_wallet_id,'tokenId',token_id,
  'txHash',tx_hash,'eventKind',event_kind,'eventIndex',event_index,'sourceKey',source_address_key,'destinationKey',destination_address_key,
  'contractKey',contract_address_key,'amount',amount::text,'blockNumber',block_number,'blockHash',block_hash,'confirmations',confirmations,
  'requiredConfs',required_confs,'finality',finality_status,'previousStatus',status,'initialBasis',initial_basis,
  'previousClassificationId',classification_basis->>'id') FROM reg_crypto_external_outflows WHERE id=target
$$;

CREATE FUNCTION fn_crypto_outflow_decision_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE decision UUID; command JSONB; source JSONB; check_basis JSONB; hold_basis JSONB; incident UUID; at TIMESTAMPTZ:=clock_timestamp();
BEGIN
 -- The common 103 guard obtains the same nonblocking resource prefix first.
 IF (NEW.classification,NEW.matched_basis_type,NEW.matched_basis_id,NEW.classification_reason,NEW.classification_evidence,NEW.classified_by)
  IS NOT DISTINCT FROM (OLD.classification,OLD.matched_basis_type,OLD.matched_basis_id,OLD.classification_reason,OLD.classification_evidence,OLD.classified_by) THEN
  IF NEW.classification_basis IS DISTINCT FROM OLD.classification_basis THEN
   IF OLD.classification_basis IS NULL THEN RAISE EXCEPTION 'legacy classification basis is unknown' USING ERRCODE='23514'; END IF;
   PERFORM fn_crypto_outflow_decision_seed(NEW.classification_basis);
   NEW.classification_basis:=OLD.classification_basis;
  END IF;
  IF NEW.classified_at IS DISTINCT FROM OLD.classified_at THEN
   IF OLD.classification_basis IS NULL THEN RAISE EXCEPTION 'classification clock is immutable' USING ERRCODE='23514'; END IF;
   NEW.classified_at:=OLD.classified_at;
  END IF;
 ELSE
  IF NEW.status IS DISTINCT FROM 'classified' OR NEW.classified_by IS NULL OR btrim(NEW.classification_reason)='' OR jsonb_typeof(NEW.classification_evidence)<>'object'
    OR NOT (OLD.status='pending_review' OR (OLD.status='classified' AND OLD.classification='unauthorized_loss' AND NEW.classification IN ('matched_basis','false_positive_or_reorg')
       AND EXISTS(SELECT 1 FROM doc_crypto_custody_adjustments WHERE external_outflow_id=OLD.id AND status='rejected'))) THEN
   RAISE EXCEPTION 'outflow classification decision cannot be rewritten' USING ERRCODE='23514';
  END IF;
  decision:=fn_crypto_outflow_decision_seed(NEW.classification_basis);
  IF decision::text=OLD.classification_basis->>'id' THEN RAISE EXCEPTION 'classification requires a new decision ID' USING ERRCODE='23514'; END IF;
  command:=fn_crypto_outflow_decision_command('classify',OLD.id,NEW.classified_by,NEW.classification_reason,NEW.classification,NEW.matched_basis_type,NEW.matched_basis_id,NEW.classification_evidence);
  source:=fn_crypto_outflow_decision_source(OLD.id);
  NEW.classified_at:=at;
  NEW.classification_basis:=jsonb_build_object('id',decision,'command',command,'source',source,'decidedAt',at);
 END IF;
 IF OLD.status='resolved' THEN
  IF (NEW.status,NEW.resolved_by,NEW.resolved_at,NEW.resolution,NEW.resolution_basis) IS DISTINCT FROM (OLD.status,OLD.resolved_by,OLD.resolved_at,OLD.resolution,OLD.resolution_basis) THEN
   RAISE EXCEPTION 'outflow resolution decision is immutable' USING ERRCODE='23514';
  END IF;
 ELSIF NEW.status='resolved' THEN
  IF OLD.status<>'classified' OR NEW.resolved_by IS NULL OR NEW.resolved_by=OLD.classified_by OR btrim(NEW.resolution)='' THEN
   RAISE EXCEPTION 'outflow resolution requires its independent decision' USING ERRCODE='23514';
  END IF;
  decision:=fn_crypto_outflow_decision_seed(NEW.resolution_basis);
  IF NOT fn_crypto_try_lock_operational_hold_owner('external_outflow',OLD.id::text) THEN
   RAISE EXCEPTION 'external outflow hold owner is busy; retry the command' USING ERRCODE='55P03';
  END IF;
  SELECT COALESCE(jsonb_agg(payload ORDER BY scope,id),'[]'::jsonb) INTO hold_basis FROM (
   SELECT 'network'::text AS scope,id,jsonb_build_object('scope','network','id',id,'targetId',network_id,'reason',reason,'details',details,'heldBy',held_by,'heldAt',held_at) AS payload
    FROM reg_crypto_network_holds WHERE owner_type='external_outflow' AND owner_id=OLD.id::text
   UNION ALL SELECT 'wallet',id,jsonb_build_object('scope','wallet','id',id,'targetId',wallet_id,'reason',reason,'details',details,'heldBy',held_by,'heldAt',held_at)
    FROM reg_crypto_wallet_allocation_holds WHERE owner_type='external_outflow' AND owner_id=OLD.id::text
  ) acquired;
  SELECT jsonb_build_object('checkId',c.id,'runId',r.id,'completedAt',r.completed_at,'walletId',c.wallet_id,'tokenId',c.token_id,
    'ledgerAmount',c.ledger_amount::text,'chainAmount',c.chain_amount::text,'deltaAmount',c.delta_amount::text)
   INTO check_basis FROM reg_crypto_reconciliation_checks c JOIN reg_crypto_reconciliation_runs r ON r.id=c.run_id
   WHERE c.network_id=OLD.network_id AND r.network_id=OLD.network_id AND c.wallet_id=OLD.source_wallet_id AND c.token_id IS NOT DISTINCT FROM OLD.token_id
    AND r.status='completed' AND c.delta_amount=0 AND r.completed_at>GREATEST(OLD.classified_at,
      COALESCE((SELECT decided_at FROM doc_crypto_custody_adjustments WHERE external_outflow_id=OLD.id AND status='posted'),OLD.classified_at))
    AND NOT EXISTS(SELECT 1 FROM reg_crypto_reconciliation_items i WHERE i.wallet_id=c.wallet_id AND i.token_id IS NOT DISTINCT FROM c.token_id
      AND i.kind IN ('token_balance_mismatch','native_fee_balance_mismatch') AND i.status IN ('open','acknowledged'))
   ORDER BY r.completed_at DESC,c.id LIMIT 1 FOR SHARE OF c,r NOWAIT;
  IF check_basis IS NULL THEN RAISE EXCEPTION 'outflow resolution requires its exact fresh reconciliation' USING ERRCODE='23514'; END IF;
  SELECT id INTO incident FROM reg_crypto_reconciliation_items WHERE network_id=OLD.network_id AND wallet_id=OLD.source_wallet_id AND token_id IS NOT DISTINCT FROM OLD.token_id
   AND kind='external_outflow' AND tx_hash=OLD.tx_hash AND event_kind=OLD.event_kind AND event_index=OLD.event_index AND status IN ('open','acknowledged')
   ORDER BY id LIMIT 1 FOR UPDATE NOWAIT;
  IF incident IS NULL THEN RAISE EXCEPTION 'outflow resolution requires its source incident' USING ERRCODE='23514'; END IF;
  source:=fn_crypto_outflow_decision_source(OLD.id)||jsonb_build_object('reconciliation',check_basis,'incidentId',incident,'holds',hold_basis,
   'adjustmentDecisionId',(SELECT decision_basis->>'id' FROM doc_crypto_custody_adjustments WHERE external_outflow_id=OLD.id));
  NEW.resolved_at:=at;
  NEW.resolution_basis:=jsonb_build_object('id',decision,'command',fn_crypto_outflow_decision_command('resolve',OLD.id,NEW.resolved_by,NEW.resolution),'source',source,'decidedAt',at);
 ELSIF (NEW.resolved_by,NEW.resolved_at,NEW.resolution,NEW.resolution_basis) IS DISTINCT FROM (OLD.resolved_by,OLD.resolved_at,OLD.resolution,OLD.resolution_basis) THEN
  RAISE EXCEPTION 'resolution evidence requires its transition' USING ERRCODE='23514';
 END IF;
 IF (to_jsonb(NEW)-'updated_at') IS NOT DISTINCT FROM (to_jsonb(OLD)-'updated_at') THEN RETURN NULL; END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER trg_crypto_external_outflow_decision BEFORE UPDATE ON reg_crypto_external_outflows FOR EACH ROW EXECUTE FUNCTION fn_crypto_outflow_decision_guard();

CREATE FUNCTION fn_crypto_custody_decision_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE decision UUID; kind TEXT; at TIMESTAMPTZ:=clock_timestamp(); source JSONB;
BEGIN
 IF NOT fn_crypto_try_lock_external_outflow((SELECT network_id FROM reg_crypto_external_outflows WHERE id=OLD.external_outflow_id),
   (SELECT tx_hash FROM reg_crypto_external_outflows WHERE id=OLD.external_outflow_id),OLD.wallet_id) THEN
  RAISE EXCEPTION 'external outflow resources are busy; retry the command' USING ERRCODE='55P03';
 END IF;
 IF OLD.status<>'pending_approval' THEN
  IF (to_jsonb(NEW)-'updated_at') IS NOT DISTINCT FROM (to_jsonb(OLD)-'updated_at') THEN RETURN NULL; END IF;
  RAISE EXCEPTION 'custody adjustment terminal decision is immutable' USING ERRCODE='23514';
 END IF;
 IF NEW.status=OLD.status THEN
  IF (NEW.decision_by,NEW.decision_reason,NEW.decided_at,NEW.decision_basis) IS DISTINCT FROM (OLD.decision_by,OLD.decision_reason,OLD.decided_at,OLD.decision_basis) THEN
   RAISE EXCEPTION 'custody decision requires its transition' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF NEW.status NOT IN ('posted','rejected') OR NEW.decision_by IS NULL OR NEW.decision_by=OLD.maker_id OR btrim(NEW.decision_reason)='' THEN
  RAISE EXCEPTION 'custody decision requires its independent checker and reason' USING ERRCODE='23514';
 END IF;
 decision:=fn_crypto_outflow_decision_seed(NEW.decision_basis);
 kind:=CASE WHEN NEW.status='posted' THEN 'approve' ELSE 'reject' END;
 source:=fn_crypto_outflow_decision_source(OLD.external_outflow_id)||jsonb_build_object('adjustment',jsonb_build_object('id',OLD.id,'walletId',OLD.wallet_id,
  'tokenId',OLD.token_id,'amount',OLD.amount::text,'makerId',OLD.maker_id,'accountingModelVersion',OLD.accounting_model_version,'postedVersion',OLD.posted_version));
 NEW.decided_at:=at;
 NEW.decision_basis:=jsonb_build_object('id',decision,'command',fn_crypto_outflow_decision_command(kind,OLD.id,NEW.decision_by,NEW.decision_reason),'source',source,'decidedAt',at);
 RETURN NEW;
END; $$;
CREATE TRIGGER trg_crypto_custody_decision BEFORE UPDATE ON doc_crypto_custody_adjustments FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_decision_guard();

CREATE FUNCTION fn_crypto_outflow_decision_audit_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE basis JSONB; evidence JSONB; expected_action TEXT; previous TEXT; current TEXT;
BEGIN
 IF NEW.action NOT IN ('classified','reclassified','resolved','custody_adjustment_posted','custody_adjustment_rejected') THEN
  IF NEW.decision_basis IS NOT NULL THEN RAISE EXCEPTION 'non-decision action cannot carry a decision basis' USING ERRCODE='23514'; END IF;
  RETURN NEW;
 END IF;
 IF NEW.action IN ('classified','reclassified') THEN
  SELECT classification_basis,classification_evidence INTO basis,evidence FROM reg_crypto_external_outflows WHERE id=NEW.external_outflow_id;
  previous:=basis->'source'->>'previousStatus'; current:='classified';
  expected_action:=CASE WHEN previous='pending_review' THEN 'classified' ELSE 'reclassified' END;
 ELSIF NEW.action='resolved' THEN
  SELECT resolution_basis INTO basis FROM reg_crypto_external_outflows WHERE id=NEW.external_outflow_id;
  previous:='classified'; current:='resolved'; evidence:='{}'::jsonb; expected_action:='resolved';
 ELSE
  SELECT a.decision_basis,CASE WHEN a.status='posted' THEN jsonb_build_object('adjustmentId',a.id,'postedVersion',1) ELSE jsonb_build_object('adjustmentId',a.id) END,
   CASE WHEN a.status='posted' THEN 'custody_adjustment_posted' ELSE 'custody_adjustment_rejected' END,a.status
   INTO basis,evidence,expected_action,current FROM doc_crypto_custody_adjustments a
   WHERE a.external_outflow_id=NEW.external_outflow_id AND a.id=(NEW.decision_basis->'command'->>'targetId')::uuid;
  previous:='pending_approval';
 END IF;
 IF basis IS NULL OR NEW.decision_basis IS DISTINCT FROM basis OR NEW.id::text IS DISTINCT FROM basis->>'id' OR NEW.actor_type<>'user'
  OR NEW.actor_id IS DISTINCT FROM basis->'command'->>'actorId' OR NEW.reason IS DISTINCT FROM basis->'command'->>'reason'
  OR (NEW.action,NEW.old_state,NEW.new_state,NEW.evidence) IS DISTINCT FROM (expected_action,previous,current,evidence) THEN
  RAISE EXCEPTION 'outflow decision audit does not match its saved decision' USING ERRCODE='23514';
 END IF;
 NEW.created_at:=(basis->>'decidedAt')::timestamptz;
 RETURN NEW;
END; $$;
CREATE TRIGGER trg_crypto_outflow_decision_audit_insert BEFORE INSERT ON reg_crypto_external_outflow_actions FOR EACH ROW EXECUTE FUNCTION fn_crypto_outflow_decision_audit_guard();

CREATE FUNCTION fn_crypto_outflow_append_decision_audit() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE basis JSONB; action TEXT; before_state TEXT; after_state TEXT; payload JSONB; target UUID;
BEGIN
 IF TG_TABLE_NAME='reg_crypto_external_outflows' THEN
  target:=NEW.id;
  IF NEW.classification_basis IS DISTINCT FROM OLD.classification_basis AND NEW.classification_basis IS NOT NULL THEN
   basis:=NEW.classification_basis; action:=CASE WHEN OLD.status='pending_review' THEN 'classified' ELSE 'reclassified' END;
   before_state:=OLD.status; after_state:='classified'; payload:=NEW.classification_evidence;
  ELSIF NEW.resolution_basis IS DISTINCT FROM OLD.resolution_basis AND NEW.resolution_basis IS NOT NULL THEN
   basis:=NEW.resolution_basis; action:='resolved'; before_state:='classified'; after_state:='resolved'; payload:='{}'::jsonb;
  END IF;
 ELSE
  target:=NEW.external_outflow_id;
  IF NEW.decision_basis IS DISTINCT FROM OLD.decision_basis AND NEW.decision_basis IS NOT NULL THEN
   basis:=NEW.decision_basis; action:=CASE WHEN NEW.status='posted' THEN 'custody_adjustment_posted' ELSE 'custody_adjustment_rejected' END;
   before_state:='pending_approval'; after_state:=NEW.status;
   payload:=CASE WHEN NEW.status='posted' THEN jsonb_build_object('adjustmentId',NEW.id,'postedVersion',1) ELSE jsonb_build_object('adjustmentId',NEW.id) END;
  END IF;
 END IF;
 IF basis IS NOT NULL THEN
  INSERT INTO reg_crypto_external_outflow_actions(id,external_outflow_id,actor_type,actor_id,action,old_state,new_state,reason,evidence,decision_basis)
  VALUES((basis->>'id')::uuid,target,'user',basis->'command'->>'actorId',action,before_state,after_state,basis->'command'->>'reason',payload,basis);
 END IF;
 RETURN NULL;
END; $$;
CREATE TRIGGER trg_crypto_outflow_decision_audit AFTER UPDATE ON reg_crypto_external_outflows FOR EACH ROW EXECUTE FUNCTION fn_crypto_outflow_append_decision_audit();
CREATE TRIGGER trg_crypto_custody_decision_audit AFTER UPDATE ON doc_crypto_custody_adjustments FOR EACH ROW EXECUTE FUNCTION fn_crypto_outflow_append_decision_audit();

CREATE FUNCTION fn_crypto_outflow_decision_audit_complete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE bases JSONB[]; basis JSONB; target UUID;
BEGIN
 IF TG_TABLE_NAME='reg_crypto_external_outflows' THEN bases:=ARRAY[NEW.classification_basis,NEW.resolution_basis]; target:=NEW.id;
 ELSE bases:=ARRAY[NEW.decision_basis]; target:=NEW.external_outflow_id; END IF;
 FOREACH basis IN ARRAY bases LOOP
  IF basis IS NOT NULL AND NOT EXISTS(SELECT 1 FROM reg_crypto_external_outflow_actions a WHERE a.id=(basis->>'id')::uuid AND a.decision_basis=basis) THEN
   RAISE EXCEPTION 'outflow decision requires its immutable audit in the same transaction' USING ERRCODE='23514';
  END IF;
  IF basis->'command'->>'kind'='resolve' THEN
   IF NOT EXISTS(SELECT 1 FROM reg_crypto_reconciliation_checks c JOIN reg_crypto_reconciliation_runs r ON r.id=c.run_id
     WHERE c.id=(basis->'source'->'reconciliation'->>'checkId')::uuid AND r.status='completed'
      AND jsonb_build_object('checkId',c.id,'runId',r.id,'completedAt',r.completed_at,'walletId',c.wallet_id,'tokenId',c.token_id,
       'ledgerAmount',c.ledger_amount::text,'chainAmount',c.chain_amount::text,'deltaAmount',c.delta_amount::text)=basis->'source'->'reconciliation') THEN
    RAISE EXCEPTION 'outflow resolution reconciliation basis changed before commit' USING ERRCODE='23514';
   END IF;
   IF NOT EXISTS(SELECT 1 FROM reg_crypto_reconciliation_items i WHERE i.id=(basis->'source'->>'incidentId')::uuid AND i.status='resolved'
      AND i.resolved_by::text=basis->'command'->>'actorId' AND i.resolution_reason=basis->'command'->>'reason') THEN
    RAISE EXCEPTION 'resolved external outflow requires its exact resolved reconciliation incident' USING ERRCODE='23514';
   END IF;
   IF jsonb_array_length(basis->'source'->'holds')=0
    OR EXISTS(SELECT 1 FROM reg_crypto_network_holds WHERE owner_type='external_outflow' AND owner_id=target::text
      AND (status<>'resolved' OR resolved_by IS DISTINCT FROM basis->'command'->>'actorId' OR resolution IS DISTINCT FROM basis->'command'->>'reason'))
    OR EXISTS(SELECT 1 FROM reg_crypto_wallet_allocation_holds WHERE owner_type='external_outflow' AND owner_id=target::text
      AND (status<>'resolved' OR resolved_by IS DISTINCT FROM basis->'command'->>'actorId' OR resolution IS DISTINCT FROM basis->'command'->>'reason')) THEN
    RAISE EXCEPTION 'outflow resolution requires its complete original hold cohort' USING ERRCODE='23514';
   END IF;
  END IF;
 END LOOP;
 RETURN NULL;
END; $$;
CREATE CONSTRAINT TRIGGER trg_crypto_outflow_decision_complete AFTER INSERT OR UPDATE ON reg_crypto_external_outflows DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_outflow_decision_audit_complete();
CREATE CONSTRAINT TRIGGER trg_crypto_custody_decision_complete AFTER INSERT OR UPDATE ON doc_crypto_custody_adjustments DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_outflow_decision_audit_complete();

-- Nullable legacy columns are intentionally not backfilled.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM reg_crypto_external_outflows o WHERE
   (classification_basis IS NOT NULL AND (classification_basis->'command' IS DISTINCT FROM fn_crypto_outflow_decision_command('classify',id,classified_by,classification_reason,classification,matched_basis_type,matched_basis_id,classification_evidence)
    OR (classification_basis->>'decidedAt')::timestamptz IS DISTINCT FROM classified_at))
   OR (resolution_basis IS NOT NULL AND (resolution_basis->'command' IS DISTINCT FROM fn_crypto_outflow_decision_command('resolve',id,resolved_by,resolution)
    OR (resolution_basis->>'decidedAt')::timestamptz IS DISTINCT FROM resolved_at)))
 OR EXISTS(SELECT 1 FROM doc_crypto_custody_adjustments a WHERE decision_basis IS NOT NULL AND
   (decision_basis->'command' IS DISTINCT FROM fn_crypto_outflow_decision_command(CASE WHEN status='posted' THEN 'approve' ELSE 'reject' END,id,decision_by,decision_reason)
    OR (decision_basis->>'decidedAt')::timestamptz IS DISTINCT FROM decided_at)) THEN
  RAISE EXCEPTION 'outflow decision history requires review before upgrade' USING ERRCODE='23514';
 END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM reg_crypto_external_outflows WHERE classification_basis IS NOT NULL OR resolution_basis IS NOT NULL)
 OR EXISTS(SELECT 1 FROM doc_crypto_custody_adjustments WHERE decision_basis IS NOT NULL)
 OR EXISTS(SELECT 1 FROM reg_crypto_external_outflow_actions WHERE decision_basis IS NOT NULL) THEN
  RAISE EXCEPTION 'outflow decision history requires forward recovery' USING ERRCODE='23514';
 END IF;
END $$;
DROP TRIGGER trg_crypto_outflow_decision_complete ON reg_crypto_external_outflows;
DROP TRIGGER trg_crypto_custody_decision_complete ON doc_crypto_custody_adjustments;
DROP FUNCTION fn_crypto_outflow_decision_audit_complete();
DROP TRIGGER trg_crypto_custody_decision_audit ON doc_crypto_custody_adjustments;
DROP TRIGGER trg_crypto_outflow_decision_audit ON reg_crypto_external_outflows;
DROP FUNCTION fn_crypto_outflow_append_decision_audit();
DROP TRIGGER trg_crypto_outflow_decision_audit_insert ON reg_crypto_external_outflow_actions;
DROP FUNCTION fn_crypto_outflow_decision_audit_guard();
DROP TRIGGER trg_crypto_custody_decision ON doc_crypto_custody_adjustments;
DROP FUNCTION fn_crypto_custody_decision_guard();
DROP TRIGGER trg_crypto_external_outflow_decision ON reg_crypto_external_outflows;
DROP FUNCTION fn_crypto_outflow_decision_guard();
DROP FUNCTION fn_crypto_outflow_decision_source(UUID);
DROP FUNCTION fn_crypto_outflow_decision_seed(JSONB);
DROP FUNCTION fn_crypto_outflow_decision_command(TEXT,UUID,UUID,TEXT,TEXT,TEXT,UUID,JSONB);
DROP TRIGGER trg_crypto_outflow_initial_decision ON reg_crypto_external_outflows;
DROP TRIGGER trg_crypto_custody_initial_decision ON doc_crypto_custody_adjustments;
DROP FUNCTION fn_crypto_outflow_decision_insert_guard();
-- Keep additive nullable audit columns.
-- +goose StatementEnd
