-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
-- Append-only starts linearize risk invalidation and external signing/sending.
-- A start is potential exposure, including an RPC timeout. It is never a release.
CREATE TABLE reg_crypto_aml_dispatch_starts (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(), authorization_id UUID NOT NULL REFERENCES reg_crypto_signing_authorizations(id),
 phase TEXT NOT NULL CHECK(phase IN ('authorize','sign','broadcast')), snapshot JSONB NOT NULL CHECK(jsonb_typeof(snapshot)='object'),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL)
);
CREATE INDEX idx_crypto_aml_dispatch_authorization ON reg_crypto_aml_dispatch_starts(authorization_id,phase,created_at,id);
CREATE TRIGGER trg_crypto_aml_dispatch_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_dispatch_starts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();
CREATE TABLE reg_crypto_aml_first_dispatches (
 authorization_id UUID PRIMARY KEY REFERENCES reg_crypto_signing_authorizations(id), start_id UUID NOT NULL UNIQUE REFERENCES reg_crypto_aml_dispatch_starts(id),
 snapshot JSONB NOT NULL CHECK(jsonb_typeof(snapshot)='object'), created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL)
);
CREATE TRIGGER trg_crypto_aml_first_dispatch_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_first_dispatches FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE FUNCTION fn_crypto_aml_spend_plan_matches(basis TEXT, basis_id UUID, purpose TEXT, source UUID, asset UUID, destination TEXT, amount NUMERIC) RETURNS BOOLEAN AS $$
 SELECT CASE $3
 WHEN 'sweep' THEN $1='CryptoSweepAttempt' AND EXISTS(SELECT 1 FROM reg_crypto_aml_internal_allocations p JOIN cat_wallets d ON d.id=p.destination_wallet_id JOIN cat_blockchain_networks n ON n.id=p.network_id WHERE p.sweep_attempt_id=$2 AND p.source_wallet_id=$4 AND p.token_id=$5 AND p.gross=$7 AND p.status='reserved' AND d.address_key=CASE WHEN n.chain_family='evm' THEN LOWER($6) ELSE $6 END)
 WHEN 'native_funding' THEN $1='CryptoNativeFunding' AND EXISTS(SELECT 1 FROM doc_crypto_native_fundings f JOIN cat_wallets d ON d.id=f.destination_wallet_id JOIN cat_blockchain_networks n ON n.id=f.network_id WHERE f.id=$2 AND f.source_wallet_id=$4 AND f.native_token_id=$5 AND f.amount_minor=$7 AND f.status IN ('created','reserved','broadcast') AND d.address_key=CASE WHEN n.chain_family='evm' THEN LOWER($6) ELSE $6 END)
 WHEN 'activation' THEN $1='CryptoAccountActivationTask' AND $7=0 AND EXISTS(SELECT 1 FROM reg_crypto_account_activations task JOIN cat_wallets d ON d.id=task.wallet_id JOIN cat_wallets sw ON sw.id=$4 AND sw.network_id=d.network_id AND sw.custody_merchant_id=d.custody_merchant_id AND sw.wallet_role='network_fee' JOIN cat_tokens t ON t.id=$5 AND t.network_id=task.network_id AND t.token_standard='native' JOIN cat_blockchain_networks n ON n.id=task.network_id WHERE task.id=$2 AND (NOT EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_provisions av JOIN reg_crypto_aml_pool_intents ai ON ai.id=av.id WHERE av.wallet_id=d.id)
   OR EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_provisions av JOIN reg_crypto_aml_pool_intents ai ON ai.id=av.id JOIN sys_crypto_aml_pool_policies ap ON ap.id=ai.policy_id AND ap.enabled AND ap.automatic_baseline JOIN crypto_aml_pool_runtime runtime ON runtime.id=ap.id WHERE av.wallet_id=d.id AND av.status='completed' AND fn_crypto_aml_pool_baselines_ready(ai.id))) AND task.status IN ('claimed','signing','broadcast','held') AND d.address_key=CASE WHEN n.chain_family='evm' THEN LOWER($6) ELSE $6 END)
 ELSE FALSE END;
$$ LANGUAGE SQL STABLE;
CREATE FUNCTION fn_crypto_aml_check_allocation_dispatch(a reg_crypto_signing_authorizations, phase TEXT) RETURNS VOID AS $$
DECLARE plan reg_crypto_aml_internal_allocations;
BEGIN
    IF a.basis_type<>'CryptoSweepAttempt' THEN RETURN; END IF;
    PERFORM 1 FROM sys_crypto_aml_routing_scopes scope JOIN reg_crypto_aml_internal_allocations p ON p.merchant_id=scope.merchant_id AND p.network_id=scope.network_id AND p.token_id=scope.token_id
        WHERE p.sweep_attempt_id=a.basis_id FOR UPDATE OF scope;
    SELECT * INTO plan FROM reg_crypto_aml_internal_allocations WHERE sweep_attempt_id=a.basis_id FOR UPDATE;
    IF NOT FOUND THEN RETURN; END IF;
    IF plan.status<>'reserved' OR EXISTS(SELECT 1 FROM reg_crypto_aml_allocation_cancellations WHERE allocation_id=plan.id)
       OR a.network_id<>plan.network_id OR a.merchant_id IS DISTINCT FROM plan.merchant_id
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_sweep_attempts WHERE id=plan.sweep_attempt_id AND status IN ('claimed','resource_ready','signing','broadcast') AND (phase='broadcast' OR expires_at>NOW()))
       OR a.amount_minor_units<>plan.gross OR NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_risk_states WHERE wallet_id=plan.source_wallet_id AND token_id=plan.token_id AND (revision=plan.risk_revision OR (phase='broadcast' AND a.signed_envelope IS NOT NULL)) AND risk_class=plan.risk_class AND scope_version=plan.scope_version AND inventory_covered AND expires_at>NOW())
       OR NOT EXISTS(SELECT 1 FROM cat_wallets sw JOIN cat_wallets dw ON dw.id=plan.destination_wallet_id JOIN cat_tokens t ON t.id=plan.token_id
          WHERE sw.id=plan.source_wallet_id AND a.vault_key_id=sw.vault_key_id AND a.asset_standard=t.token_standard
          AND sw.address_key=CASE WHEN a.chain_family='evm' THEN LOWER(a.source_address) ELSE a.source_address END
          AND dw.address_key=CASE WHEN a.chain_family='evm' THEN LOWER(a.destination_address) ELSE a.destination_address END
          AND CASE WHEN a.chain_family='evm' THEN LOWER(a.contract_address)=LOWER(COALESCE(t.contract_address,'')) ELSE a.contract_address=COALESCE(t.contract_address,'') END)
       OR NOT fn_crypto_aml_inventory_covered(plan.source_wallet_id,plan.token_id) OR NOT fn_crypto_aml_inventory_covered(plan.destination_wallet_id,plan.token_id)
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_risk_states risk JOIN reg_crypto_aml_inventory_positions inventory ON inventory.wallet_id=risk.wallet_id AND inventory.token_id=risk.token_id
          WHERE risk.wallet_id=plan.destination_wallet_id AND risk.token_id=plan.token_id AND risk.scope_version=plan.scope_version AND risk.risk_class<>'high'
            AND (inventory.principal+inventory.service_fee=0 OR (risk.risk_class=plan.risk_class AND risk.inventory_covered AND risk.expires_at>NOW()))) THEN
        RAISE EXCEPTION 'AML allocation authorization is cancelled or stale' USING ERRCODE='23514';
    END IF;
    RETURN;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_check_resource_dispatch(a reg_crypto_signing_authorizations, phase TEXT) RETURNS VOID AS $$
DECLARE wallet UUID; owner UUID; native UUID; budget reg_crypto_aml_resource_budgets; fee reg_crypto_fee_reservations;
BEGIN
 SELECT w.id,w.custody_merchant_id,t.id INTO wallet,owner,native FROM cat_wallets w JOIN cat_blockchain_networks n ON n.id=w.network_id
   JOIN cat_tokens t ON t.network_id=w.network_id AND t.token_standard='native'
   WHERE w.network_id=a.network_id AND w.address_key=CASE WHEN n.chain_family='evm' THEN LOWER(a.source_address) ELSE a.source_address END;
 IF owner IS NULL OR NOT EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes WHERE merchant_id=owner AND network_id=a.network_id AND token_id=native AND enabled) THEN RETURN; END IF;
 PERFORM 1 FROM sys_crypto_aml_routing_scopes s WHERE s.merchant_id=owner AND s.network_id=a.network_id ORDER BY s.merchant_id,s.network_id,s.token_id FOR UPDATE;
 SELECT b.* INTO budget FROM reg_crypto_aml_resource_budgets b JOIN reg_crypto_fee_reservations f ON f.id=b.fee_reservation_id
   WHERE f.basis_type=a.basis_type AND f.basis_id=a.basis_id AND f.purpose=a.purpose AND f.attempt=a.generation AND f.payer_wallet_id=wallet AND f.native_token_id=native;
 IF NOT FOUND THEN RAISE EXCEPTION 'AML native scope requires an operating resource budget' USING ERRCODE='23514'; END IF;
 SELECT * INTO fee FROM reg_crypto_fee_reservations WHERE id=budget.fee_reservation_id;
 IF budget.status<>'reserved' OR (fee.status<>'reserved' AND NOT (phase='broadcast' AND fee.status IN ('broadcast','held') AND (fee.tx_hash=a.tx_hash OR (fee.status='held' AND fee.tx_hash='' AND fee.unsigned_fingerprint=a.unsigned_fingerprint AND a.signed_envelope IS NOT NULL)))) OR (phase<>'broadcast' AND fee.tx_hash<>'') OR a.merchant_id IS DISTINCT FROM owner OR a.max_network_fee_minor>budget.max_fee
    OR NOT EXISTS(SELECT 1 FROM cat_wallets WHERE id=wallet AND vault_key_id=a.vault_key_id AND is_active AND NOT deletion_mark)
    OR NOT EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes WHERE merchant_id=owner AND network_id=a.network_id AND token_id=native AND enabled AND version=budget.scope_version)
    OR NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_risk_states WHERE wallet_id=wallet AND token_id=native AND (revision=budget.risk_revision OR (phase='broadcast' AND a.signed_envelope IS NOT NULL)) AND scope_version=budget.scope_version AND risk_class=budget.risk_class AND inventory_covered AND expires_at>NOW())
    OR NOT fn_crypto_aml_total_inventory_covered(wallet,native)
    OR NOT EXISTS(SELECT 1 FROM cat_tokens t WHERE t.id=fee.operation_token_id AND t.network_id=a.network_id AND t.token_standard=a.asset_standard
       AND CASE WHEN a.chain_family='evm' THEN LOWER(COALESCE(t.contract_address,''))=LOWER(a.contract_address) ELSE COALESCE(t.contract_address,'')=a.contract_address END)
    OR EXISTS(SELECT 1 FROM reg_crypto_fee_reservations f WHERE f.payer_wallet_id=wallet AND f.native_token_id=native AND f.id<>fee.id AND f.status IN ('reserved','broadcast','held')
       AND (f.status='held' OR NOT EXISTS(SELECT 1 FROM reg_crypto_aml_resource_budgets b WHERE b.fee_reservation_id=f.id AND b.status='reserved'))) THEN
   RAISE EXCEPTION 'operating resource authorization is cancelled or stale' USING ERRCODE='23514';
 END IF;
 IF budget.funding_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM doc_crypto_native_fundings f JOIN cat_wallets d ON d.id=f.destination_wallet_id AND d.custody_merchant_id=owner
     JOIN reg_crypto_wallet_risk_states risk ON risk.wallet_id=d.id AND risk.token_id=native AND risk.scope_version=budget.scope_version AND risk.risk_class<>'high'
     JOIN reg_crypto_aml_inventory_positions p ON p.wallet_id=d.id AND p.token_id=native JOIN reg_crypto_aml_resource_positions r ON r.wallet_id=d.id AND r.token_id=native
     WHERE f.id=budget.funding_id AND f.id=a.basis_id AND f.amount_minor=a.amount_minor_units AND d.address_key=CASE WHEN a.chain_family='evm' THEN LOWER(a.destination_address) ELSE a.destination_address END
       AND f.status IN ('created','reserved','broadcast') AND fn_crypto_aml_total_inventory_covered(d.id,native)
       AND (p.principal+p.service_fee+r.amount=0 OR (risk.risk_class=budget.risk_class AND risk.inventory_covered AND risk.expires_at>NOW()))) THEN
   RAISE EXCEPTION 'native resource destination changed before authorization' USING ERRCODE='23514';
 END IF;
 RETURN;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_check_batch_dispatch(a reg_crypto_signing_authorizations, phase TEXT) RETURNS VOID AS $$
DECLARE plan reg_crypto_aml_internal_allocations; batch reg_crypto_aml_batches;
BEGIN
 IF a.basis_type<>'CryptoSweepAttempt' THEN RETURN; END IF;
 SELECT * INTO plan FROM reg_crypto_aml_internal_allocations WHERE sweep_attempt_id=a.basis_id;
 IF plan.batch_id IS NULL THEN RETURN; END IF;
 PERFORM 1 FROM sys_crypto_aml_routing_scopes WHERE merchant_id=plan.merchant_id AND network_id=plan.network_id AND token_id=plan.token_id FOR UPDATE;
 SELECT * INTO batch FROM reg_crypto_aml_batches WHERE id=plan.batch_id FOR UPDATE;
 IF a.purpose<>'sweep' OR batch.execution_held OR batch.execution_revision<>plan.batch_execution_revision
    OR NOT EXISTS(SELECT 1 FROM cat_wallets source JOIN cat_wallets collector ON collector.id=batch.collector_wallet_id
       WHERE source.id=plan.source_wallet_id AND source.wallet_role='merchant_pool' AND source.custody_merchant_id=batch.merchant_id AND source.network_id=batch.network_id AND source.is_active AND NOT source.deletion_mark
         AND collector.wallet_role='liquidity_hot' AND collector.custody_merchant_id=batch.merchant_id AND collector.network_id=batch.network_id AND collector.is_active AND NOT collector.deletion_mark AND collector.chain_activation_status='active'
         AND NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_allocation_holds hold WHERE hold.wallet_id IN(source.id,collector.id) AND hold.status='open'))
    OR EXISTS(SELECT 1 FROM reg_crypto_aml_effects e JOIN reg_crypto_aml_effect_chain_states cs ON cs.effect_id=e.id AND cs.canonicality='canonical'
       WHERE e.wallet_id=batch.collector_wallet_id AND e.token_id=batch.token_id AND e.source_wallet_id IS NULL)
    OR NOT EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes WHERE merchant_id=batch.merchant_id AND network_id=batch.network_id AND token_id=batch.token_id AND version=batch.scope_version AND enabled) THEN
   RAISE EXCEPTION 'batch execution is held or stale' USING ERRCODE='23514';
 END IF;
 RETURN;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_has_unapplied_risk(root UUID,asset UUID) RETURNS BOOLEAN AS $$
 WITH RECURSIVE ancestors(wallet_id) AS (
   SELECT root UNION SELECT edge.source_wallet_id FROM crypto_aml_risk_edges edge JOIN ancestors a ON a.wallet_id=edge.destination_wallet_id WHERE edge.token_id=asset
 ) SELECT EXISTS(SELECT 1 FROM reg_crypto_aml_projection_queue q WHERE q.wallet_id IN(SELECT wallet_id FROM ancestors) AND q.token_id=asset AND q.next_attempt_at<=NOW())
 OR EXISTS(SELECT 1 FROM reg_crypto_aml_decision_queue q JOIN reg_crypto_aml_reports report ON report.id=q.report_id JOIN reg_crypto_aml_subjects subject ON subject.id=report.subject_id WHERE subject.wallet_id IN(SELECT wallet_id FROM ancestors) AND subject.token_id=asset);
$$ LANGUAGE SQL STABLE;

CREATE FUNCTION fn_crypto_aml_begin_dispatch(auth_id UUID, dispatch_phase TEXT) RETURNS UUID AS $$
DECLARE a reg_crypto_signing_authorizations; source cat_wallets; asset UUID; native UUID; start_id UUID; snapshot JSONB; budget UUID; aml_enabled BOOLEAN;
BEGIN
 IF dispatch_phase NOT IN ('authorize','sign','broadcast') THEN RAISE EXCEPTION 'invalid AML dispatch phase' USING ERRCODE='23514'; END IF;
 SELECT * INTO STRICT a FROM reg_crypto_signing_authorizations WHERE id=auth_id;
 SELECT w.* INTO STRICT source FROM cat_wallets w WHERE w.network_id=a.network_id AND w.address_key=CASE WHEN a.chain_family='evm' THEN LOWER(a.source_address) ELSE a.source_address END;
 SELECT EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes s JOIN cat_tokens t ON t.id=s.token_id WHERE s.merchant_id=source.custody_merchant_id AND s.network_id=a.network_id AND s.enabled AND (t.token_standard='native' OR CASE WHEN a.chain_family='evm' THEN LOWER(COALESCE(t.contract_address,''))=LOWER(a.contract_address) ELSE COALESCE(t.contract_address,'')=a.contract_address END)) INTO aml_enabled;
 IF NOT aml_enabled THEN
   IF a.basis_type='CryptoAMLWithdrawalLine' OR EXISTS(SELECT 1 FROM reg_crypto_aml_dispatch_starts WHERE authorization_id=auth_id) OR EXISTS(SELECT 1 FROM reg_crypto_aml_resource_budgets b JOIN reg_crypto_fee_reservations f ON f.id=b.fee_reservation_id WHERE f.basis_type=a.basis_type AND f.basis_id=a.basis_id) OR EXISTS(SELECT 1 FROM reg_crypto_aml_internal_allocations WHERE sweep_attempt_id=a.basis_id AND a.basis_type='CryptoSweepAttempt') THEN RAISE EXCEPTION 'AML dispatch scope was disabled' USING ERRCODE='23514'; END IF;
   RETURN NULL;
 END IF;
 SELECT t.id INTO STRICT asset FROM cat_tokens t WHERE t.network_id=a.network_id AND t.token_standard=a.asset_standard AND CASE WHEN a.chain_family='evm' THEN LOWER(COALESCE(t.contract_address,''))=LOWER(a.contract_address) ELSE COALESCE(t.contract_address,'')=a.contract_address END;
 SELECT t.id INTO native FROM cat_tokens t WHERE t.network_id=a.network_id AND t.token_standard='native';
 PERFORM 1 FROM sys_crypto_aml_routing_scopes s WHERE s.merchant_id=source.custody_merchant_id AND s.network_id=a.network_id ORDER BY s.merchant_id,s.network_id,s.token_id FOR UPDATE;
 IF NOT source.is_active OR source.deletion_mark OR source.custody_merchant_id IS DISTINCT FROM a.merchant_id OR source.vault_key_id<>a.vault_key_id
    OR (a.expires_at<=NOW() AND NOT (dispatch_phase='broadcast' AND a.signed_envelope IS NOT NULL)) OR a.status IN ('failed','expired','settled')
    OR EXISTS(SELECT 1 FROM reg_crypto_network_holds WHERE network_id=a.network_id AND status='open')
    OR NOT EXISTS(SELECT 1 FROM sys_crypto_network_policies WHERE network_id=a.network_id AND enabled)
    OR (a.chain_family='tron' AND (NOT EXISTS(SELECT 1 FROM sys_crypto_block_ingestion WHERE network_id=a.network_id AND mode IN ('hybrid','solidified') AND NOT shadow_paused AND last_error_code='')
      OR NOT EXISTS(SELECT 1 FROM sys_chain_watcher_state WHERE network_id=a.network_id AND watcher_key='tron:solidified' AND updated_at>NOW()-interval '2 minutes' AND to_timestamp(last_timestamp/1000.0)>NOW()-make_interval(secs=>GREATEST(180,COALESCE((SELECT max_watcher_lag_seconds FROM sys_crypto_wallet_pool_policies WHERE network_id=a.network_id),0))) AND lease_owner<>'' AND lease_expires_at>NOW())
      OR (EXISTS(SELECT 1 FROM sys_crypto_block_ingestion WHERE network_id=a.network_id AND mode='hybrid') AND NOT EXISTS(SELECT 1 FROM sys_chain_watcher_state WHERE network_id=a.network_id AND watcher_key='tron:latest' AND updated_at>NOW()-interval '2 minutes' AND to_timestamp(last_timestamp/1000.0)>NOW()-make_interval(secs=>GREATEST(180,COALESCE((SELECT max_watcher_lag_seconds FROM sys_crypto_wallet_pool_policies WHERE network_id=a.network_id),0))) AND lease_owner<>'' AND lease_expires_at>NOW()))))
    OR (a.chain_family='evm' AND (NOT EXISTS(SELECT 1 FROM sys_chain_watcher_state WHERE network_id=a.network_id AND watcher_key='native_eth' AND updated_at>NOW()-interval '2 minutes' AND to_timestamp(last_timestamp/1.0)>NOW()-make_interval(secs=>GREATEST(180,COALESCE((SELECT max_watcher_lag_seconds FROM sys_crypto_wallet_pool_policies WHERE network_id=a.network_id),0))) AND lease_owner<>'' AND lease_expires_at>NOW()) OR (a.asset_standard<>'native' AND NOT EXISTS(SELECT 1 FROM sys_chain_watcher_state WHERE network_id=a.network_id AND watcher_key='erc20:'||LOWER(a.contract_address) AND updated_at>NOW()-interval '2 minutes' AND to_timestamp(last_timestamp)>NOW()-make_interval(secs=>GREATEST(180,COALESCE((SELECT max_watcher_lag_seconds FROM sys_crypto_wallet_pool_policies WHERE network_id=a.network_id),0))) AND lease_owner<>'' AND lease_expires_at>NOW()))))
    OR EXISTS(SELECT 1 FROM reg_crypto_chain_observations WHERE network_id=a.network_id AND status<>'processed')
    OR fn_crypto_aml_has_unapplied_risk(source.id,asset) OR fn_crypto_aml_has_unapplied_risk(source.id,native)
    OR NOT fn_crypto_aml_spend_plan_matches(a.basis_type,a.basis_id,a.purpose,source.id,asset,a.destination_address,a.amount_minor_units)
    OR (dispatch_phase='broadcast' AND (a.signed_envelope IS NULL OR a.tx_hash=''))
    OR EXISTS(SELECT 1 FROM reg_crypto_chain_settlements WHERE network_id=a.network_id AND tx_hash=a.tx_hash AND a.tx_hash<>'') THEN
   RAISE EXCEPTION 'AML dispatch requires current exact plan, healthy ingestion and unfinalized attempt' USING ERRCODE='23514';
 END IF;
 SELECT b.id INTO budget FROM reg_crypto_aml_resource_budgets b JOIN reg_crypto_fee_reservations f ON f.id=b.fee_reservation_id WHERE f.basis_type=a.basis_type AND f.basis_id=a.basis_id AND f.purpose=a.purpose AND f.attempt=a.generation AND b.wallet_id=source.id AND b.token_id=native AND b.status='reserved' AND f.unsigned_fingerprint=a.unsigned_fingerprint;
 IF budget IS NULL THEN RAISE EXCEPTION 'AML dispatch requires an owned operating gas budget' USING ERRCODE='23514'; END IF;
 PERFORM fn_crypto_aml_check_resource_dispatch(a,dispatch_phase);
 PERFORM fn_crypto_aml_check_allocation_dispatch(a,dispatch_phase);
 PERFORM fn_crypto_aml_check_batch_dispatch(a,dispatch_phase);
 snapshot:=jsonb_build_object('authorizationId',a.authorization_id,'signingRecordId',a.id,'basisType',a.basis_type,'basisId',a.basis_id,'purpose',a.purpose,'generation',a.generation,'unsignedFingerprint',a.unsigned_fingerprint,'sourceWalletId',source.id,'destination',a.destination_address,'tokenId',asset,'amountMinor',a.amount_minor_units::text,'txHash',a.tx_hash,
   'watermarks',(SELECT jsonb_agg(to_jsonb(watermark) ORDER BY watcher_key) FROM sys_chain_watcher_state watermark WHERE watermark.network_id=a.network_id),
   'risks',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.token_id) FROM reg_crypto_wallet_risk_states r WHERE r.wallet_id=source.id AND r.token_id IN(asset,native)),
   'resourceBudget',(SELECT to_jsonb(b) FROM reg_crypto_aml_resource_budgets b WHERE b.id=budget),
   'allocation',(SELECT to_jsonb(p) FROM reg_crypto_aml_internal_allocations p WHERE p.sweep_attempt_id=a.basis_id));
 INSERT INTO reg_crypto_aml_dispatch_starts(authorization_id,phase,snapshot) VALUES(auth_id,dispatch_phase,snapshot) RETURNING id INTO start_id;
 IF dispatch_phase='broadcast' THEN INSERT INTO reg_crypto_aml_first_dispatches(authorization_id,start_id,snapshot) VALUES(auth_id,start_id,snapshot) ON CONFLICT(authorization_id) DO NOTHING; END IF;
 RETURN start_id;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN IF EXISTS(SELECT 1 FROM reg_crypto_aml_dispatch_starts) THEN RAISE EXCEPTION 'cannot discard dispatch exposure history'; END IF; END $$;
DROP FUNCTION fn_crypto_aml_begin_dispatch(UUID,TEXT);
DROP FUNCTION fn_crypto_aml_has_unapplied_risk(UUID,UUID);
DROP FUNCTION fn_crypto_aml_check_batch_dispatch(reg_crypto_signing_authorizations,TEXT);
DROP FUNCTION fn_crypto_aml_check_allocation_dispatch(reg_crypto_signing_authorizations,TEXT);
DROP FUNCTION fn_crypto_aml_check_resource_dispatch(reg_crypto_signing_authorizations,TEXT);
DROP FUNCTION fn_crypto_aml_spend_plan_matches(TEXT,UUID,TEXT,UUID,UUID,TEXT,NUMERIC);
DROP TABLE reg_crypto_aml_first_dispatches;
DROP TABLE reg_crypto_aml_dispatch_starts;
-- +goose StatementEnd
