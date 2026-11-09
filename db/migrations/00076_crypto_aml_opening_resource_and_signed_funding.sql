-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

CREATE OR REPLACE FUNCTION fn_crypto_aml_resource_fact_guard() RETURNS TRIGGER AS $$
BEGIN
 PERFORM 1 FROM sys_crypto_aml_routing_scopes WHERE merchant_id=NEW.merchant_id AND network_id=NEW.network_id AND token_id=NEW.token_id AND enabled FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'resource facts require an enabled owned scope' USING ERRCODE='23514'; END IF;
 PERFORM 1 FROM reg_crypto_aml_resource_positions WHERE token_id=NEW.token_id AND wallet_id IN (NEW.source_wallet_id,NEW.destination_wallet_id) ORDER BY wallet_id FOR UPDATE;
 IF (NEW.source_wallet_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_resource_positions WHERE wallet_id=NEW.source_wallet_id AND token_id=NEW.token_id))
    OR (NEW.destination_wallet_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_resource_positions WHERE wallet_id=NEW.destination_wallet_id AND token_id=NEW.token_id)) THEN
   RAISE EXCEPTION 'resource fact requires baseline positions for both endpoints' USING ERRCODE='23514';
 END IF;
 IF EXISTS(SELECT 1 FROM reg_crypto_aml_resource_positions p WHERE p.token_id=NEW.token_id AND p.wallet_id IN (NEW.source_wallet_id,NEW.destination_wallet_id)
    AND p.amount-COALESCE((SELECT o.operating FROM reg_crypto_aml_testnet_openings o WHERE o.wallet_id=p.wallet_id AND o.token_id=p.token_id),0)<>COALESCE((SELECT SUM(CASE WHEN f.destination_wallet_id=p.wallet_id THEN f.amount ELSE -f.amount END) FROM reg_crypto_aml_resource_facts f WHERE f.token_id=p.token_id AND (f.source_wallet_id=p.wallet_id OR f.destination_wallet_id=p.wallet_id)),0)) THEN
   RAISE EXCEPTION 'operating resource projection disagrees with immutable facts' USING ERRCODE='23514';
 END IF;
 IF EXISTS(SELECT 1 FROM reg_crypto_aml_inventory_facts WHERE effect_id=NEW.effect_id) THEN RAISE EXCEPTION 'chain effect already allocated to principal/fee' USING ERRCODE='23514'; END IF;
 IF NEW.kind='seed' THEN
   IF NOT EXISTS(SELECT 1 FROM reg_crypto_aml_operating_receipts r JOIN reg_crypto_aml_effect_chain_states cs ON cs.effect_id=r.effect_id AND cs.canonicality='canonical'
       JOIN reg_crypto_balance_movements m ON m.line_id=r.movement_id AND m.recorder_id=r.id AND m.recorder_type='AMLOperatingReceipt' AND m.recorder_version=1 AND m.record_type='receipt'
       WHERE r.id=NEW.receipt_id AND r.effect_id=NEW.effect_id AND r.merchant_id=NEW.merchant_id AND r.network_id=NEW.network_id AND r.token_id=NEW.token_id AND r.wallet_id=NEW.destination_wallet_id
         AND r.amount=NEW.amount AND r.movement_id=NEW.destination_movement_id AND m.amount=r.amount AND m.wallet_id=r.wallet_id AND m.token_id=r.token_id) THEN
     RAISE EXCEPTION 'operating resource seed does not match verified physical receipt' USING ERRCODE='23514';
   END IF;
 ELSE
   IF NOT EXISTS(SELECT 1 FROM crypto_aml_ready_resource_budgets ready JOIN reg_crypto_aml_resource_budgets b ON b.id=ready.id AND b.status='reserved'
       WHERE b.id=NEW.budget_id AND b.merchant_id=NEW.merchant_id AND b.network_id=NEW.network_id AND b.token_id=NEW.token_id AND b.wallet_id=NEW.source_wallet_id
         AND ((NEW.kind='cost' AND NEW.amount=ready.actual_fee AND NEW.source_movement_id=ready.cost_movement_id)
           OR (NEW.kind='funding' AND ready.chain_status='succeeded' AND NEW.amount=ready.funding_amount AND NEW.destination_wallet_id=ready.destination_wallet_id AND NEW.effect_id=ready.effect_id
             AND NEW.source_movement_id=ready.funding_source_movement_id AND NEW.destination_movement_id=ready.funding_destination_movement_id))) THEN
     RAISE EXCEPTION 'resource fact requires its reserved budget and final chain/physical evidence' USING ERRCODE='23514';
   END IF;
 END IF;
 IF NEW.source_wallet_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_resource_positions WHERE wallet_id=NEW.source_wallet_id AND token_id=NEW.token_id AND amount>=NEW.amount) THEN
   RAISE EXCEPTION 'operating resource debit cannot consume merchant principal' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_crypto_aml_spend_plan_matches(basis TEXT, basis_id UUID, purpose TEXT, source UUID, asset UUID, destination TEXT, amount NUMERIC) RETURNS BOOLEAN AS $$
 SELECT CASE $3
 WHEN 'sweep' THEN $1='CryptoSweepAttempt' AND EXISTS(SELECT 1 FROM reg_crypto_aml_internal_allocations p JOIN cat_wallets d ON d.id=p.destination_wallet_id JOIN cat_blockchain_networks n ON n.id=p.network_id WHERE p.sweep_attempt_id=$2 AND p.source_wallet_id=$4 AND p.token_id=$5 AND p.gross=$7 AND p.status='reserved' AND d.address_key=CASE WHEN n.chain_family='evm' THEN LOWER($6) ELSE $6 END)
 WHEN 'native_funding' THEN $1='CryptoNativeFunding' AND EXISTS(SELECT 1 FROM doc_crypto_native_fundings f JOIN cat_wallets d ON d.id=f.destination_wallet_id JOIN cat_blockchain_networks n ON n.id=f.network_id WHERE f.id=$2 AND f.source_wallet_id=$4 AND f.native_token_id=$5 AND f.amount_minor=$7 AND f.status IN ('created','reserved','signed','broadcast') AND d.address_key=CASE WHEN n.chain_family='evm' THEN LOWER($6) ELSE $6 END)
 WHEN 'activation' THEN $1='CryptoAccountActivationTask' AND $7=0 AND EXISTS(SELECT 1 FROM reg_crypto_account_activations task JOIN cat_wallets d ON d.id=task.wallet_id JOIN cat_wallets sw ON sw.id=$4 AND sw.network_id=d.network_id AND sw.custody_merchant_id=d.custody_merchant_id AND sw.wallet_role='network_fee' JOIN cat_tokens t ON t.id=$5 AND t.network_id=task.network_id AND t.token_standard='native' JOIN cat_blockchain_networks n ON n.id=task.network_id WHERE task.id=$2 AND (NOT EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_provisions av JOIN reg_crypto_aml_pool_intents ai ON ai.id=av.id WHERE av.wallet_id=d.id)
   OR EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_provisions av JOIN reg_crypto_aml_pool_intents ai ON ai.id=av.id JOIN sys_crypto_aml_pool_policies ap ON ap.id=ai.policy_id AND ap.enabled AND ap.automatic_baseline JOIN crypto_aml_pool_runtime runtime ON runtime.id=ap.id WHERE av.wallet_id=d.id AND av.status='completed' AND fn_crypto_aml_pool_baselines_ready(ai.id))) AND task.status IN ('claimed','signing','broadcast','held') AND d.address_key=CASE WHEN n.chain_family='evm' THEN LOWER($6) ELSE $6 END)
 WHEN 'withdrawal' THEN $1='CryptoAMLWithdrawalLine' AND EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_lines l JOIN reg_crypto_aml_withdrawal_plans p ON p.request_id=l.request_id JOIN doc_withdrawal_requests r ON r.id=p.request_id JOIN cat_blockchain_networks n ON n.id=p.network_id WHERE l.id=$2 AND l.wallet_id=$4 AND l.token_id=$5 AND l.amount=$7 AND l.status IN ('reserved','held','broadcast') AND CASE WHEN n.chain_family='evm' THEN LOWER(r.dest_address)=LOWER($6) ELSE r.dest_address=$6 END)
 ELSE FALSE END;
$$ LANGUAGE SQL STABLE;

CREATE OR REPLACE FUNCTION fn_crypto_aml_check_resource_dispatch(a reg_crypto_signing_authorizations, phase TEXT) RETURNS VOID AS $$
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
    OR NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_risk_states WHERE wallet_id=wallet AND token_id=native AND (revision=budget.risk_revision OR (phase='broadcast' AND a.signed_envelope IS NOT NULL)) AND scope_version=budget.scope_version AND risk_class=budget.risk_class AND (inventory_covered AND expires_at>NOW() OR (budget.risk_class IN ('high','unknown') AND a.purpose='withdrawal' AND EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_lines l JOIN reg_crypto_aml_withdrawal_plans p ON p.request_id=l.request_id WHERE l.id=a.basis_id AND a.basis_type='CryptoAMLWithdrawalLine' AND p.risk_class=budget.risk_class AND p.risk_accepted))))
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
       AND f.status IN ('created','reserved','signed','broadcast') AND fn_crypto_aml_total_inventory_covered(d.id,native)
       AND (p.principal+p.service_fee+r.amount=0 OR (risk.risk_class=budget.risk_class AND risk.inventory_covered AND risk.expires_at>NOW()))) THEN
   RAISE EXCEPTION 'native resource destination changed before authorization' USING ERRCODE='23514';
 END IF;
 RETURN;
END;
$$ LANGUAGE plpgsql;
CREATE TABLE reg_crypto_aml_authorization_renewals (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(), authorization_id UUID NOT NULL UNIQUE REFERENCES reg_crypto_signing_authorizations(id),
 expires_at TIMESTAMPTZ NOT NULL, reason TEXT NOT NULL CHECK(length(reason) BETWEEN 3 AND 500), authorized_by TEXT NOT NULL CHECK(length(btrim(authorized_by)) BETWEEN 1 AND 100),
 authorization_jws TEXT NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL)
);
CREATE FUNCTION fn_crypto_aml_authorization_renewal_guard() RETURNS TRIGGER AS $$
DECLARE a reg_crypto_signing_authorizations;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'authorization renewal history is immutable' USING ERRCODE='23514'; END IF;
 IF TG_OP='UPDATE' THEN
  IF (NEW.id,NEW.authorization_id,NEW.expires_at,NEW.reason,NEW.authorized_by,NEW.created_at,NEW._deleted_at) IS DISTINCT FROM (OLD.id,OLD.authorization_id,OLD.expires_at,OLD.reason,OLD.authorized_by,OLD.created_at,OLD._deleted_at)
    OR (OLD.authorization_jws<>'' AND NEW.authorization_jws<>OLD.authorization_jws) OR btrim(NEW.authorization_jws)='' THEN
   RAISE EXCEPTION 'authorization renewal identity and signature are immutable' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 SELECT * INTO a FROM reg_crypto_signing_authorizations WHERE id=NEW.authorization_id FOR UPDATE;
 IF NOT FOUND OR a.basis_type<>'CryptoSweepAttempt' OR a.purpose<>'sweep' OR a.status<>'created' OR a.signed_envelope IS NOT NULL OR a.tx_hash<>'' OR a.authorization_jws='' OR a.unsigned_envelope IS NULL
   OR a.expires_at>NOW() OR NEW.expires_at<=NOW() OR NEW.expires_at>NOW()+interval '10 minutes' OR NEW.authorization_jws<>''
   OR EXISTS(SELECT 1 FROM reg_crypto_aml_dispatch_starts WHERE authorization_id=a.id AND phase='broadcast')
   OR NOT EXISTS(SELECT 1 FROM reg_crypto_aml_internal_allocations plan
    JOIN cat_wallets source ON source.id=plan.source_wallet_id AND source.is_active AND NOT source.deletion_mark
    JOIN sys_crypto_aml_routing_scopes scope ON scope.merchant_id=plan.merchant_id AND scope.network_id=plan.network_id AND scope.token_id=plan.token_id AND scope.enabled AND scope.version=plan.scope_version
    JOIN sys_crypto_network_policies policy ON policy.network_id=plan.network_id AND policy.enabled
    JOIN reg_crypto_wallet_risk_states risk ON risk.wallet_id=source.id AND risk.token_id=plan.token_id AND risk.revision=plan.risk_revision AND risk.scope_version=plan.scope_version AND risk.inventory_covered AND risk.expires_at>NOW() AND risk.risk_class=plan.risk_class
    JOIN reg_crypto_aml_batches batch ON batch.id=plan.batch_id AND NOT batch.execution_held AND batch.execution_revision=plan.batch_execution_revision
    JOIN reg_crypto_fee_reservations fee ON fee.basis_type=a.basis_type AND fee.basis_id=a.basis_id AND fee.purpose=a.purpose AND fee.attempt=a.attempt AND fee.status='reserved' AND fee.tx_hash='' AND fee.unsigned_fingerprint=a.unsigned_fingerprint
    WHERE plan.sweep_attempt_id=a.basis_id AND plan.status='reserved' AND plan.risk_class IN ('low','medium') AND plan.gross=a.amount_minor_units AND plan.merchant_id=a.merchant_id AND source.custody_merchant_id=a.merchant_id AND source.vault_key_id=a.vault_key_id
      AND fn_crypto_aml_total_inventory_covered(source.id,plan.token_id) AND fn_crypto_aml_total_inventory_covered(plan.destination_wallet_id,plan.token_id)
      AND NOT EXISTS(SELECT 1 FROM reg_crypto_network_holds h WHERE h.network_id=plan.network_id AND h.status='open')
      AND NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_allocation_holds h WHERE h.wallet_id=source.id AND h.status='open')
      AND NOT EXISTS(SELECT 1 FROM reg_crypto_reconciliation_items i WHERE i.wallet_id=source.id AND i.status IN ('open','acknowledged') AND i.kind IN ('token_balance_mismatch','native_fee_balance_mismatch'))) THEN
  RAISE EXCEPTION 'exact renewal requires expired unsigned owned AML sweep, unchanged fresh risk and original fee/envelope' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_authorization_renewal_txid BEFORE UPDATE ON reg_crypto_aml_authorization_renewals FOR EACH ROW EXECUTE FUNCTION update_txid_column();
CREATE TRIGGER trg_crypto_aml_authorization_renewal_guard BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_aml_authorization_renewals FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_authorization_renewal_guard();

CREATE OR REPLACE FUNCTION fn_crypto_aml_begin_dispatch(auth_id UUID, dispatch_phase TEXT) RETURNS UUID AS $$
DECLARE a reg_crypto_signing_authorizations; source cat_wallets; asset UUID; native UUID; start_id UUID; snapshot JSONB; budget UUID; aml_enabled BOOLEAN;
BEGIN
 IF dispatch_phase NOT IN ('authorize','sign','broadcast') THEN RAISE EXCEPTION 'invalid AML dispatch phase' USING ERRCODE='23514'; END IF;
 SELECT * INTO STRICT a FROM reg_crypto_signing_authorizations WHERE id=auth_id;
 IF a.signed_envelope IS NULL THEN
  SELECT COALESCE(MAX(r.expires_at),a.expires_at) INTO a.expires_at FROM reg_crypto_aml_authorization_renewals r WHERE r.authorization_id=a.id AND r.expires_at>NOW();
 END IF;
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
 PERFORM fn_crypto_aml_check_withdrawal_dispatch(a,dispatch_phase);
 PERFORM fn_crypto_aml_check_resource_dispatch(a,dispatch_phase);
 PERFORM fn_crypto_aml_check_allocation_dispatch(a,dispatch_phase);
 PERFORM fn_crypto_aml_check_batch_dispatch(a,dispatch_phase);
 snapshot:=jsonb_build_object('authorizationId',a.authorization_id,'signingRecordId',a.id,'basisType',a.basis_type,'basisId',a.basis_id,'purpose',a.purpose,'generation',a.generation,'unsignedFingerprint',a.unsigned_fingerprint,'sourceWalletId',source.id,'destination',a.destination_address,'tokenId',asset,'amountMinor',a.amount_minor_units::text,'txHash',a.tx_hash,
   'watermarks',(SELECT jsonb_agg(to_jsonb(watermark) ORDER BY watcher_key) FROM sys_chain_watcher_state watermark WHERE watermark.network_id=a.network_id),
   'risks',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.token_id) FROM reg_crypto_wallet_risk_states r WHERE r.wallet_id=source.id AND r.token_id IN(asset,native)),
   'resourceBudget',(SELECT to_jsonb(b) FROM reg_crypto_aml_resource_budgets b WHERE b.id=budget),
   'withdrawalLine',(SELECT to_jsonb(l) FROM reg_crypto_aml_withdrawal_lines l WHERE l.id=a.basis_id),
   'withdrawalPlan',(SELECT to_jsonb(p) FROM reg_crypto_aml_withdrawal_plans p JOIN reg_crypto_aml_withdrawal_lines l ON l.request_id=p.request_id WHERE l.id=a.basis_id),
   'allocation',(SELECT to_jsonb(p) FROM reg_crypto_aml_internal_allocations p WHERE p.sweep_attempt_id=a.basis_id));
 INSERT INTO reg_crypto_aml_dispatch_starts(authorization_id,phase,snapshot) VALUES(auth_id,dispatch_phase,snapshot) RETURNING id INTO start_id;
 IF dispatch_phase='broadcast' THEN INSERT INTO reg_crypto_aml_first_dispatches(authorization_id,start_id,snapshot) VALUES(auth_id,start_id,snapshot) ON CONFLICT(authorization_id) DO NOTHING; END IF;
 RETURN start_id;
END;
$$ LANGUAGE plpgsql;
CREATE OR REPLACE FUNCTION fn_crypto_aml_pool_counts(pool UUID) RETURNS TABLE(ready INT,pending INT,queued INT,total INT,started INT) AS $$
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
     w.wallet_role=p.wallet_role AND w.is_active AND NOT w.deletion_mark AND
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
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM reg_crypto_aml_authorization_renewals) THEN RAISE EXCEPTION 'cannot roll back authorization renewal history'; END IF;
 IF EXISTS(SELECT 1 FROM reg_crypto_aml_resource_facts f JOIN reg_crypto_aml_testnet_openings o ON o.wallet_id=f.source_wallet_id AND o.token_id=f.token_id WHERE o.operating>0)
 OR EXISTS(SELECT 1 FROM doc_crypto_native_fundings WHERE status='signed') THEN
  RAISE EXCEPTION 'cannot roll back active opening resource or signed funding repair; use a forward migration';
 END IF;
END $$;
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
CREATE OR REPLACE FUNCTION fn_crypto_aml_resource_fact_guard() RETURNS TRIGGER AS $$
BEGIN
 PERFORM 1 FROM sys_crypto_aml_routing_scopes WHERE merchant_id=NEW.merchant_id AND network_id=NEW.network_id AND token_id=NEW.token_id AND enabled FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'resource facts require an enabled owned scope' USING ERRCODE='23514'; END IF;
 PERFORM 1 FROM reg_crypto_aml_resource_positions WHERE token_id=NEW.token_id AND wallet_id IN (NEW.source_wallet_id,NEW.destination_wallet_id) ORDER BY wallet_id FOR UPDATE;
 IF (NEW.source_wallet_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_resource_positions WHERE wallet_id=NEW.source_wallet_id AND token_id=NEW.token_id))
    OR (NEW.destination_wallet_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_resource_positions WHERE wallet_id=NEW.destination_wallet_id AND token_id=NEW.token_id)) THEN
   RAISE EXCEPTION 'resource fact requires baseline positions for both endpoints' USING ERRCODE='23514';
 END IF;
 IF EXISTS(SELECT 1 FROM reg_crypto_aml_resource_positions p WHERE p.token_id=NEW.token_id AND p.wallet_id IN (NEW.source_wallet_id,NEW.destination_wallet_id)
    AND p.amount<>COALESCE((SELECT SUM(CASE WHEN f.destination_wallet_id=p.wallet_id THEN f.amount ELSE -f.amount END) FROM reg_crypto_aml_resource_facts f WHERE f.token_id=p.token_id AND (f.source_wallet_id=p.wallet_id OR f.destination_wallet_id=p.wallet_id)),0)) THEN
   RAISE EXCEPTION 'operating resource projection disagrees with immutable facts' USING ERRCODE='23514';
 END IF;
 IF EXISTS(SELECT 1 FROM reg_crypto_aml_inventory_facts WHERE effect_id=NEW.effect_id) THEN RAISE EXCEPTION 'chain effect already allocated to principal/fee' USING ERRCODE='23514'; END IF;
 IF NEW.kind='seed' THEN
   IF NOT EXISTS(SELECT 1 FROM reg_crypto_aml_operating_receipts r JOIN reg_crypto_aml_effect_chain_states cs ON cs.effect_id=r.effect_id AND cs.canonicality='canonical'
       JOIN reg_crypto_balance_movements m ON m.line_id=r.movement_id AND m.recorder_id=r.id AND m.recorder_type='AMLOperatingReceipt' AND m.recorder_version=1 AND m.record_type='receipt'
       WHERE r.id=NEW.receipt_id AND r.effect_id=NEW.effect_id AND r.merchant_id=NEW.merchant_id AND r.network_id=NEW.network_id AND r.token_id=NEW.token_id AND r.wallet_id=NEW.destination_wallet_id
         AND r.amount=NEW.amount AND r.movement_id=NEW.destination_movement_id AND m.amount=r.amount AND m.wallet_id=r.wallet_id AND m.token_id=r.token_id) THEN
     RAISE EXCEPTION 'operating resource seed does not match verified physical receipt' USING ERRCODE='23514';
   END IF;
 ELSE
   IF NOT EXISTS(SELECT 1 FROM crypto_aml_ready_resource_budgets ready JOIN reg_crypto_aml_resource_budgets b ON b.id=ready.id AND b.status='reserved'
       WHERE b.id=NEW.budget_id AND b.merchant_id=NEW.merchant_id AND b.network_id=NEW.network_id AND b.token_id=NEW.token_id AND b.wallet_id=NEW.source_wallet_id
         AND ((NEW.kind='cost' AND NEW.amount=ready.actual_fee AND NEW.source_movement_id=ready.cost_movement_id)
           OR (NEW.kind='funding' AND ready.chain_status='succeeded' AND NEW.amount=ready.funding_amount AND NEW.destination_wallet_id=ready.destination_wallet_id AND NEW.effect_id=ready.effect_id
             AND NEW.source_movement_id=ready.funding_source_movement_id AND NEW.destination_movement_id=ready.funding_destination_movement_id))) THEN
     RAISE EXCEPTION 'resource fact requires its reserved budget and final chain/physical evidence' USING ERRCODE='23514';
   END IF;
 END IF;
 IF NEW.source_wallet_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_resource_positions WHERE wallet_id=NEW.source_wallet_id AND token_id=NEW.token_id AND amount>=NEW.amount) THEN
   RAISE EXCEPTION 'operating resource debit cannot consume merchant principal' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_crypto_aml_spend_plan_matches(basis TEXT, basis_id UUID, purpose TEXT, source UUID, asset UUID, destination TEXT, amount NUMERIC) RETURNS BOOLEAN AS $$
 SELECT CASE $3
 WHEN 'sweep' THEN $1='CryptoSweepAttempt' AND EXISTS(SELECT 1 FROM reg_crypto_aml_internal_allocations p JOIN cat_wallets d ON d.id=p.destination_wallet_id JOIN cat_blockchain_networks n ON n.id=p.network_id WHERE p.sweep_attempt_id=$2 AND p.source_wallet_id=$4 AND p.token_id=$5 AND p.gross=$7 AND p.status='reserved' AND d.address_key=CASE WHEN n.chain_family='evm' THEN LOWER($6) ELSE $6 END)
 WHEN 'native_funding' THEN $1='CryptoNativeFunding' AND EXISTS(SELECT 1 FROM doc_crypto_native_fundings f JOIN cat_wallets d ON d.id=f.destination_wallet_id JOIN cat_blockchain_networks n ON n.id=f.network_id WHERE f.id=$2 AND f.source_wallet_id=$4 AND f.native_token_id=$5 AND f.amount_minor=$7 AND f.status IN ('created','reserved','broadcast') AND d.address_key=CASE WHEN n.chain_family='evm' THEN LOWER($6) ELSE $6 END)
 WHEN 'activation' THEN $1='CryptoAccountActivationTask' AND $7=0 AND EXISTS(SELECT 1 FROM reg_crypto_account_activations task JOIN cat_wallets d ON d.id=task.wallet_id JOIN cat_wallets sw ON sw.id=$4 AND sw.network_id=d.network_id AND sw.custody_merchant_id=d.custody_merchant_id AND sw.wallet_role='network_fee' JOIN cat_tokens t ON t.id=$5 AND t.network_id=task.network_id AND t.token_standard='native' JOIN cat_blockchain_networks n ON n.id=task.network_id WHERE task.id=$2 AND (NOT EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_provisions av JOIN reg_crypto_aml_pool_intents ai ON ai.id=av.id WHERE av.wallet_id=d.id)
   OR EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_provisions av JOIN reg_crypto_aml_pool_intents ai ON ai.id=av.id JOIN sys_crypto_aml_pool_policies ap ON ap.id=ai.policy_id AND ap.enabled AND ap.automatic_baseline JOIN crypto_aml_pool_runtime runtime ON runtime.id=ap.id WHERE av.wallet_id=d.id AND av.status='completed' AND fn_crypto_aml_pool_baselines_ready(ai.id))) AND task.status IN ('claimed','signing','broadcast','held') AND d.address_key=CASE WHEN n.chain_family='evm' THEN LOWER($6) ELSE $6 END)
 WHEN 'withdrawal' THEN $1='CryptoAMLWithdrawalLine' AND EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_lines l JOIN reg_crypto_aml_withdrawal_plans p ON p.request_id=l.request_id JOIN doc_withdrawal_requests r ON r.id=p.request_id JOIN cat_blockchain_networks n ON n.id=p.network_id WHERE l.id=$2 AND l.wallet_id=$4 AND l.token_id=$5 AND l.amount=$7 AND l.status IN ('reserved','held','broadcast') AND CASE WHEN n.chain_family='evm' THEN LOWER(r.dest_address)=LOWER($6) ELSE r.dest_address=$6 END)
 ELSE FALSE END;
$$ LANGUAGE SQL STABLE;

CREATE OR REPLACE FUNCTION fn_crypto_aml_check_resource_dispatch(a reg_crypto_signing_authorizations, phase TEXT) RETURNS VOID AS $$
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
    OR NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_risk_states WHERE wallet_id=wallet AND token_id=native AND (revision=budget.risk_revision OR (phase='broadcast' AND a.signed_envelope IS NOT NULL)) AND scope_version=budget.scope_version AND risk_class=budget.risk_class AND (inventory_covered AND expires_at>NOW() OR (budget.risk_class IN ('high','unknown') AND a.purpose='withdrawal' AND EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_lines l JOIN reg_crypto_aml_withdrawal_plans p ON p.request_id=l.request_id WHERE l.id=a.basis_id AND a.basis_type='CryptoAMLWithdrawalLine' AND p.risk_class=budget.risk_class AND p.risk_accepted))))
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
CREATE OR REPLACE FUNCTION fn_crypto_aml_begin_dispatch(auth_id UUID, dispatch_phase TEXT) RETURNS UUID AS $$
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
 PERFORM fn_crypto_aml_check_withdrawal_dispatch(a,dispatch_phase);
 PERFORM fn_crypto_aml_check_resource_dispatch(a,dispatch_phase);
 PERFORM fn_crypto_aml_check_allocation_dispatch(a,dispatch_phase);
 PERFORM fn_crypto_aml_check_batch_dispatch(a,dispatch_phase);
 snapshot:=jsonb_build_object('authorizationId',a.authorization_id,'signingRecordId',a.id,'basisType',a.basis_type,'basisId',a.basis_id,'purpose',a.purpose,'generation',a.generation,'unsignedFingerprint',a.unsigned_fingerprint,'sourceWalletId',source.id,'destination',a.destination_address,'tokenId',asset,'amountMinor',a.amount_minor_units::text,'txHash',a.tx_hash,
   'watermarks',(SELECT jsonb_agg(to_jsonb(watermark) ORDER BY watcher_key) FROM sys_chain_watcher_state watermark WHERE watermark.network_id=a.network_id),
   'risks',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.token_id) FROM reg_crypto_wallet_risk_states r WHERE r.wallet_id=source.id AND r.token_id IN(asset,native)),
   'resourceBudget',(SELECT to_jsonb(b) FROM reg_crypto_aml_resource_budgets b WHERE b.id=budget),
   'withdrawalLine',(SELECT to_jsonb(l) FROM reg_crypto_aml_withdrawal_lines l WHERE l.id=a.basis_id),
   'withdrawalPlan',(SELECT to_jsonb(p) FROM reg_crypto_aml_withdrawal_plans p JOIN reg_crypto_aml_withdrawal_lines l ON l.request_id=p.request_id WHERE l.id=a.basis_id),
   'allocation',(SELECT to_jsonb(p) FROM reg_crypto_aml_internal_allocations p WHERE p.sweep_attempt_id=a.basis_id));
 INSERT INTO reg_crypto_aml_dispatch_starts(authorization_id,phase,snapshot) VALUES(auth_id,dispatch_phase,snapshot) RETURNING id INTO start_id;
 IF dispatch_phase='broadcast' THEN INSERT INTO reg_crypto_aml_first_dispatches(authorization_id,start_id,snapshot) VALUES(auth_id,start_id,snapshot) ON CONFLICT(authorization_id) DO NOTHING; END IF;
 RETURN start_id;
END;
$$ LANGUAGE plpgsql;
DROP TABLE reg_crypto_aml_authorization_renewals;
DROP FUNCTION fn_crypto_aml_authorization_renewal_guard();
CREATE OR REPLACE FUNCTION fn_crypto_aml_pool_counts(pool UUID) RETURNS TABLE(ready INT,pending INT,queued INT,total INT,started INT) AS $$
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
-- +goose StatementEnd
