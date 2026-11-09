-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

CREATE OR REPLACE FUNCTION fn_crypto_aml_resource_budget_guard() RETURNS TRIGGER AS $$
DECLARE fee reg_crypto_fee_reservations; available NUMERIC;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'resource budget evidence cannot be deleted' USING ERRCODE='23514'; END IF;
 PERFORM 1 FROM reg_crypto_fee_reservations f JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=NEW.merchant_id AND s.network_id=f.network_id AND (TG_OP='INSERT' OR s.token_id=NEW.token_id)
   WHERE f.id=NEW.fee_reservation_id ORDER BY s.merchant_id,s.network_id,s.token_id FOR UPDATE OF s;
 IF TG_OP='INSERT' THEN
   SELECT * INTO fee FROM reg_crypto_fee_reservations WHERE id=NEW.fee_reservation_id FOR UPDATE;
 ELSE
   SELECT * INTO fee FROM reg_crypto_fee_reservations WHERE id=NEW.fee_reservation_id;
 END IF;
 IF TG_OP='UPDATE' THEN
   IF (NEW.id,NEW.fee_reservation_id,NEW.merchant_id,NEW.network_id,NEW.token_id,NEW.wallet_id,NEW.funding_id,NEW.funding_amount,NEW.max_fee,NEW.resource_revision,NEW.risk_revision,NEW.scope_version,NEW.risk_class,NEW.request_digest,NEW.created_at)
      IS DISTINCT FROM (OLD.id,OLD.fee_reservation_id,OLD.merchant_id,OLD.network_id,OLD.token_id,OLD.wallet_id,OLD.funding_id,OLD.funding_amount,OLD.max_fee,OLD.resource_revision,OLD.risk_revision,OLD.scope_version,OLD.risk_class,OLD.request_digest,OLD.created_at)
      OR (OLD.status<>'reserved' AND (NEW.status,NEW.settlement_id,NEW.actual_fee) IS DISTINCT FROM (OLD.status,OLD.settlement_id,OLD.actual_fee)) THEN
     RAISE EXCEPTION 'resource budget identity/outcome is immutable' USING ERRCODE='23514';
   END IF;
   IF NEW.status='cancelled' AND (fee.status NOT IN ('reserved','held') OR fee.tx_hash<>''
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_aml_resource_cancellations WHERE budget_id=NEW.id)
       OR EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations WHERE basis_type=fee.basis_type AND basis_id=fee.basis_id AND status <> 'failed')) THEN
     RAISE EXCEPTION 'resource budget cannot be released after authorization began' USING ERRCODE='23514';
   END IF;
   IF NEW.status IN ('settled','failed') AND NOT EXISTS(SELECT 1 FROM crypto_aml_ready_resource_budgets ready WHERE ready.id=NEW.id AND ready.settlement_id=NEW.settlement_id AND ready.actual_fee=NEW.actual_fee
       AND NEW.status=CASE WHEN ready.chain_status='succeeded' THEN 'settled' ELSE 'failed' END
       AND (ready.actual_fee=0 OR EXISTS(SELECT 1 FROM reg_crypto_aml_resource_facts f WHERE f.budget_id=NEW.id AND f.kind='cost' AND f.amount=ready.actual_fee AND f.source_movement_id=ready.cost_movement_id))
       AND (ready.chain_status='reverted' OR ready.funding_id IS NULL OR EXISTS(SELECT 1 FROM reg_crypto_aml_resource_facts f WHERE f.budget_id=NEW.id AND f.kind='funding' AND f.effect_id=ready.effect_id AND f.amount=ready.funding_amount))) THEN
     RAISE EXCEPTION 'resource outcome requires final settlement and exact applied resource facts' USING ERRCODE='23514';
   END IF;
   RETURN NEW;
 END IF;
 IF NEW.status<>'reserved' OR fee.id IS NULL OR fee.status<>'reserved' OR fee.tx_hash<>'' OR fee.purpose NOT IN ('sweep','withdrawal','native_funding','activation')
    OR (fee.network_id,fee.native_token_id,fee.payer_wallet_id,fee.max_fee_minor) IS DISTINCT FROM (NEW.network_id,NEW.token_id,NEW.wallet_id,NEW.max_fee)
    OR NOT EXISTS(SELECT 1 FROM cat_tokens WHERE id=NEW.token_id AND network_id=NEW.network_id AND token_standard='native')
    OR NOT EXISTS(SELECT 1 FROM cat_wallets WHERE id=NEW.wallet_id AND network_id=NEW.network_id AND custody_merchant_id=NEW.merchant_id AND is_active AND NOT deletion_mark)
    OR NOT EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes WHERE merchant_id=NEW.merchant_id AND network_id=NEW.network_id AND token_id=NEW.token_id AND enabled AND version=NEW.scope_version)
    OR NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_risk_states WHERE wallet_id=NEW.wallet_id AND token_id=NEW.token_id AND revision=NEW.risk_revision AND scope_version=NEW.scope_version AND risk_class=NEW.risk_class AND inventory_covered AND expires_at>NOW())
    OR NOT fn_crypto_aml_total_inventory_covered(NEW.wallet_id,NEW.token_id)
    OR EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations WHERE basis_type=fee.basis_type AND basis_id=fee.basis_id) THEN
   RAISE EXCEPTION 'resource budget requires fresh owned operating inventory before authorization' USING ERRCODE='23514';
 END IF;
 IF EXISTS(SELECT 1 FROM reg_crypto_fee_reservations other WHERE other.payer_wallet_id=NEW.wallet_id AND other.native_token_id=NEW.token_id AND other.id<>fee.id AND other.status IN ('reserved','broadcast','held')
      AND (other.status='held' OR NOT EXISTS(SELECT 1 FROM reg_crypto_aml_resource_budgets b WHERE b.fee_reservation_id=other.id))) THEN
   RAISE EXCEPTION 'unallocated or held historical fee reservations block new resource spending' USING ERRCODE='23514';
 END IF;
 IF fee.purpose='activation' AND (fee.basis_type<>'CryptoAccountActivationTask' OR NEW.funding_id IS NOT NULL OR NEW.funding_amount<>0 OR NOT EXISTS(SELECT 1 FROM reg_crypto_account_activations task JOIN cat_wallets target ON target.id=task.wallet_id AND target.custody_merchant_id=NEW.merchant_id AND target.network_id=NEW.network_id WHERE task.id=fee.basis_id AND task.network_id=NEW.network_id AND task.status IN ('claimed','signing'))) THEN RAISE EXCEPTION 'activation requires its owned task and fee-only resource budget' USING ERRCODE='23514'; END IF;
 IF fee.purpose='native_funding' THEN
   IF NOT EXISTS(SELECT 1 FROM doc_crypto_native_fundings f JOIN cat_wallets sw ON sw.id=f.source_wallet_id AND sw.wallet_role='network_fee'
       JOIN cat_wallets dw ON dw.id=f.destination_wallet_id AND dw.custody_merchant_id=NEW.merchant_id AND dw.is_active AND NOT dw.deletion_mark
       JOIN reg_crypto_aml_inventory_positions inventory ON inventory.wallet_id=dw.id AND inventory.token_id=f.native_token_id
       JOIN reg_crypto_aml_resource_positions resource ON resource.wallet_id=dw.id AND resource.token_id=f.native_token_id
       JOIN reg_crypto_wallet_risk_states risk ON risk.wallet_id=dw.id AND risk.token_id=f.native_token_id AND risk.scope_version=NEW.scope_version AND risk.risk_class<>'high'
       WHERE f.id=NEW.funding_id AND f.id=fee.basis_id AND fee.basis_type='CryptoNativeFunding' AND f.fee_reservation_id=fee.id
         AND f.network_id=NEW.network_id AND f.native_token_id=NEW.token_id AND f.source_wallet_id=NEW.wallet_id AND f.amount_minor=NEW.funding_amount AND f.amount_minor=fee.reserved_principal_minor
         AND f.status IN ('created','reserved') AND f.signing_authorization_id IS NULL AND f.tx_hash=''
         AND (inventory.principal+inventory.service_fee+resource.amount=0 OR (risk.risk_class=NEW.risk_class AND risk.inventory_covered AND risk.expires_at>NOW()))
         AND fn_crypto_aml_total_inventory_covered(dw.id,f.native_token_id)
         AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_resource_budgets peer JOIN doc_crypto_native_fundings pf ON pf.id=peer.funding_id WHERE pf.destination_wallet_id=dw.id AND peer.status='reserved' AND peer.risk_class<>NEW.risk_class)) THEN
     RAISE EXCEPTION 'native funding resource destination or risk mismatch' USING ERRCODE='23514';
   END IF;
 ELSIF NEW.funding_id IS NOT NULL OR NEW.funding_amount<>0 THEN RAISE EXCEPTION 'non-funding operation cannot reserve a resource transfer' USING ERRCODE='23514'; END IF;
 PERFORM 1 FROM reg_crypto_aml_resource_positions WHERE wallet_id=NEW.wallet_id AND token_id=NEW.token_id FOR UPDATE;
 SELECT p.amount-COALESCE((SELECT SUM(CASE WHEN f.strategy<>'resource_rental' THEN b.max_fee ELSE 0 END + b.funding_amount)
   FROM reg_crypto_aml_resource_budgets b JOIN reg_crypto_fee_reservations f ON f.id=b.fee_reservation_id
   WHERE b.wallet_id=NEW.wallet_id AND b.token_id=NEW.token_id AND b.status='reserved'),0)
   INTO available FROM reg_crypto_aml_resource_positions p WHERE p.wallet_id=NEW.wallet_id AND p.token_id=NEW.token_id AND p.revision=NEW.resource_revision;
 IF available IS NULL OR available<(CASE WHEN fee.strategy<>'resource_rental' THEN NEW.max_fee ELSE 0 END + NEW.funding_amount) THEN
   RAISE EXCEPTION 'insufficient operating funds or stale resource revision; merchant principal cannot fund gas' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE VIEW crypto_aml_ready_resource_budgets AS
 SELECT b.id,b.merchant_id,b.network_id,b.token_id,b.wallet_id,b.funding_id,b.funding_amount,b.max_fee,
        settlement.id settlement_id,settlement.status chain_status,settlement.total_fee_minor actual_fee,
        cost.line_id cost_movement_id,spent.line_id funding_source_movement_id,received.line_id funding_destination_movement_id,
        funding.destination_wallet_id,e.id effect_id
 FROM reg_crypto_aml_resource_budgets b JOIN reg_crypto_fee_reservations fee ON fee.id=b.fee_reservation_id
 JOIN reg_crypto_chain_settlements settlement ON settlement.network_id=b.network_id AND settlement.tx_hash=fee.tx_hash AND settlement.native_token_id=b.token_id
   AND settlement.total_fee_minor=fee.actual_fee_minor
   AND ((fee.purpose<>'activation' AND settlement.basis_type=fee.basis_type AND settlement.basis_id=fee.basis_id)
     OR (fee.purpose='activation' AND settlement.basis_type='CryptoNetworkOperation' AND EXISTS(SELECT 1 FROM reg_crypto_account_activations task JOIN doc_crypto_network_operations operation ON operation.id=task.network_operation_id WHERE task.id=fee.basis_id AND operation.id=settlement.basis_id AND operation.chain_settlement_id=settlement.id AND operation.tx_hash=fee.tx_hash AND operation.source_wallet_id=b.wallet_id AND operation.target_wallet_id=task.wallet_id AND operation.cost_token_id=b.token_id AND operation.cost_amount=settlement.total_fee_minor AND operation.posted AND operation.posted_version=1 AND operation.status=CASE WHEN settlement.status='succeeded' THEN 'succeeded' ELSE 'reverted' END)))
 LEFT JOIN reg_crypto_balance_movements cost ON ((fee.purpose<>'activation' AND cost.line_id=fee.id AND cost.recorder_type='ChainSettlement' AND cost.recorder_id=fee.basis_id) OR (fee.purpose='activation' AND cost.recorder_type='CryptoNetworkOperation' AND cost.recorder_id=settlement.basis_id AND (SELECT COUNT(*) FROM reg_crypto_balance_movements m WHERE m.recorder_id=settlement.basis_id AND m.recorder_type='CryptoNetworkOperation')=1)) AND cost.recorder_version=1
   AND cost.record_type='expense' AND cost.wallet_id=b.wallet_id AND cost.token_id=b.token_id AND cost.amount=settlement.total_fee_minor
 LEFT JOIN doc_crypto_native_fundings funding ON funding.id=b.funding_id AND funding.fee_reservation_id=fee.id AND funding.tx_hash=fee.tx_hash
   AND funding.source_wallet_id=b.wallet_id AND funding.amount_minor=b.funding_amount AND funding.network_id=b.network_id AND funding.native_token_id=b.token_id
 LEFT JOIN reg_crypto_aml_effects e ON e.wallet_id=funding.destination_wallet_id AND e.source_wallet_id=b.wallet_id AND e.network_id=b.network_id AND e.token_id=b.token_id
   AND e.tx_hash=funding.tx_hash AND e.amount=b.funding_amount AND e.block_hash=settlement.block_hash AND e.block_number=settlement.block_height
 LEFT JOIN reg_crypto_aml_effect_chain_states cs ON cs.effect_id=e.id AND cs.canonicality='canonical'
 LEFT JOIN reg_crypto_expected_chain_effects expected ON expected.id=e.expected_effect_id
 LEFT JOIN reg_crypto_signing_authorizations auth ON auth.id=expected.signing_authorization_id AND auth.basis_type='CryptoNativeFunding' AND auth.basis_id=funding.id AND auth.signed_envelope IS NOT NULL
 LEFT JOIN reg_crypto_balance_movements spent ON spent.recorder_id=funding.id AND spent.recorder_type='CryptoNativeFunding' AND spent.recorder_version=1 AND spent.wallet_id=b.wallet_id AND spent.token_id=b.token_id AND spent.record_type='expense' AND spent.amount=b.funding_amount
 LEFT JOIN reg_crypto_balance_movements received ON received.recorder_id=funding.id AND received.recorder_type='CryptoNativeFunding' AND received.recorder_version=1 AND received.wallet_id=funding.destination_wallet_id AND received.token_id=b.token_id AND received.record_type='receipt' AND received.amount=b.funding_amount
 WHERE b.status IN ('reserved','settled','failed') AND fee.status='settled' AND fee.native_token_id=b.token_id AND fee.network_id=b.network_id AND fee.payer_wallet_id=b.wallet_id
   AND fee.max_fee_minor=b.max_fee AND settlement.total_fee_minor<=b.max_fee AND (settlement.total_fee_minor=0 OR cost.line_id IS NOT NULL)
   AND (b.funding_id IS NULL OR (settlement.status='reverted' AND funding.status='failed'
      AND NOT EXISTS(SELECT 1 FROM reg_crypto_balance_movements m WHERE m.recorder_id=funding.id AND m.recorder_type='CryptoNativeFunding')
      AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_effects transferred JOIN reg_crypto_aml_effect_chain_states state ON state.effect_id=transferred.id AND state.canonicality='canonical'
         WHERE transferred.network_id=b.network_id AND transferred.tx_hash=fee.tx_hash AND transferred.wallet_id=funding.destination_wallet_id AND transferred.token_id=b.token_id))
      OR (settlement.status='succeeded' AND funding.status='settled' AND cs.effect_id IS NOT NULL AND auth.id IS NOT NULL AND spent.line_id IS NOT NULL AND received.line_id IS NOT NULL
          AND (SELECT COUNT(*) FROM reg_crypto_balance_movements m WHERE m.recorder_id=funding.id AND m.recorder_type='CryptoNativeFunding')=2));

CREATE OR REPLACE FUNCTION fn_crypto_aml_resource_queue_trigger() RETURNS TRIGGER AS $$
DECLARE reservation UUID;
BEGIN
 IF TG_TABLE_NAME='reg_crypto_fee_reservations' THEN reservation:=NEW.id;
 ELSIF TG_TABLE_NAME='doc_crypto_native_fundings' THEN reservation:=NEW.fee_reservation_id;
 ELSIF TG_TABLE_NAME='reg_crypto_chain_settlements' THEN SELECT id INTO reservation FROM reg_crypto_fee_reservations WHERE network_id=NEW.network_id AND tx_hash=NEW.tx_hash;
 ELSIF TG_TABLE_NAME='doc_crypto_network_operations' THEN SELECT fee.id INTO reservation FROM reg_crypto_account_activations task JOIN reg_crypto_fee_reservations fee ON fee.basis_type='CryptoAccountActivationTask' AND fee.basis_id=task.id AND fee.purpose='activation' WHERE task.network_operation_id=NEW.id;
 ELSIF TG_TABLE_NAME='reg_crypto_balance_movements' THEN
    IF NEW.recorder_type='CryptoNetworkOperation' THEN SELECT fee.id INTO reservation FROM reg_crypto_account_activations task JOIN reg_crypto_fee_reservations fee ON fee.basis_type='CryptoAccountActivationTask' AND fee.basis_id=task.id AND fee.purpose='activation' WHERE task.network_operation_id=NEW.recorder_id;
    ELSIF NEW.recorder_type='ChainSettlement' THEN reservation:=NEW.line_id;
    ELSIF NEW.recorder_type='CryptoNativeFunding' THEN SELECT fee_reservation_id INTO reservation FROM doc_crypto_native_fundings WHERE id=NEW.recorder_id; END IF;
 ELSE
    SELECT b.fee_reservation_id INTO reservation FROM reg_crypto_aml_effects e JOIN doc_crypto_native_fundings f ON f.network_id=e.network_id AND f.tx_hash=e.tx_hash AND f.destination_wallet_id=e.wallet_id
      JOIN reg_crypto_aml_resource_budgets b ON b.funding_id=f.id WHERE e.id=NEW.effect_id;
 END IF;
 IF reservation IS NOT NULL THEN PERFORM fn_crypto_aml_queue_resource_budget(reservation); END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_resource_activation_queue AFTER INSERT OR UPDATE ON doc_crypto_network_operations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_queue_trigger();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN IF EXISTS(SELECT 1 FROM reg_crypto_aml_resource_budgets b JOIN reg_crypto_fee_reservations f ON f.id=b.fee_reservation_id WHERE f.purpose='activation') THEN RAISE EXCEPTION 'cannot discard activation resource evidence'; END IF; END $$;
DROP TRIGGER trg_crypto_aml_resource_activation_queue ON doc_crypto_network_operations;
CREATE OR REPLACE FUNCTION fn_crypto_aml_resource_budget_guard() RETURNS TRIGGER AS $$
DECLARE fee reg_crypto_fee_reservations; available NUMERIC;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'resource budget evidence cannot be deleted' USING ERRCODE='23514'; END IF;
 PERFORM 1 FROM reg_crypto_fee_reservations f JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=NEW.merchant_id AND s.network_id=f.network_id AND (TG_OP='INSERT' OR s.token_id=NEW.token_id)
   WHERE f.id=NEW.fee_reservation_id ORDER BY s.merchant_id,s.network_id,s.token_id FOR UPDATE OF s;
 IF TG_OP='INSERT' THEN
   SELECT * INTO fee FROM reg_crypto_fee_reservations WHERE id=NEW.fee_reservation_id FOR UPDATE;
 ELSE
   SELECT * INTO fee FROM reg_crypto_fee_reservations WHERE id=NEW.fee_reservation_id;
 END IF;
 IF TG_OP='UPDATE' THEN
   IF (NEW.id,NEW.fee_reservation_id,NEW.merchant_id,NEW.network_id,NEW.token_id,NEW.wallet_id,NEW.funding_id,NEW.funding_amount,NEW.max_fee,NEW.resource_revision,NEW.risk_revision,NEW.scope_version,NEW.risk_class,NEW.request_digest,NEW.created_at)
      IS DISTINCT FROM (OLD.id,OLD.fee_reservation_id,OLD.merchant_id,OLD.network_id,OLD.token_id,OLD.wallet_id,OLD.funding_id,OLD.funding_amount,OLD.max_fee,OLD.resource_revision,OLD.risk_revision,OLD.scope_version,OLD.risk_class,OLD.request_digest,OLD.created_at)
      OR (OLD.status<>'reserved' AND (NEW.status,NEW.settlement_id,NEW.actual_fee) IS DISTINCT FROM (OLD.status,OLD.settlement_id,OLD.actual_fee)) THEN
     RAISE EXCEPTION 'resource budget identity/outcome is immutable' USING ERRCODE='23514';
   END IF;
   IF NEW.status='cancelled' AND (fee.status NOT IN ('reserved','held') OR fee.tx_hash<>''
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_aml_resource_cancellations WHERE budget_id=NEW.id)
       OR EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations WHERE basis_type=fee.basis_type AND basis_id=fee.basis_id AND status <> 'failed')) THEN
     RAISE EXCEPTION 'resource budget cannot be released after authorization began' USING ERRCODE='23514';
   END IF;
   IF NEW.status IN ('settled','failed') AND NOT EXISTS(SELECT 1 FROM crypto_aml_ready_resource_budgets ready WHERE ready.id=NEW.id AND ready.settlement_id=NEW.settlement_id AND ready.actual_fee=NEW.actual_fee
       AND NEW.status=CASE WHEN ready.chain_status='succeeded' THEN 'settled' ELSE 'failed' END
       AND (ready.actual_fee=0 OR EXISTS(SELECT 1 FROM reg_crypto_aml_resource_facts f WHERE f.budget_id=NEW.id AND f.kind='cost' AND f.amount=ready.actual_fee AND f.source_movement_id=ready.cost_movement_id))
       AND (ready.chain_status='reverted' OR ready.funding_id IS NULL OR EXISTS(SELECT 1 FROM reg_crypto_aml_resource_facts f WHERE f.budget_id=NEW.id AND f.kind='funding' AND f.effect_id=ready.effect_id AND f.amount=ready.funding_amount))) THEN
     RAISE EXCEPTION 'resource outcome requires final settlement and exact applied resource facts' USING ERRCODE='23514';
   END IF;
   RETURN NEW;
 END IF;
 IF NEW.status<>'reserved' OR fee.id IS NULL OR fee.status<>'reserved' OR fee.tx_hash<>'' OR fee.purpose NOT IN ('sweep','withdrawal','native_funding')
    OR (fee.network_id,fee.native_token_id,fee.payer_wallet_id,fee.max_fee_minor) IS DISTINCT FROM (NEW.network_id,NEW.token_id,NEW.wallet_id,NEW.max_fee)
    OR NOT EXISTS(SELECT 1 FROM cat_tokens WHERE id=NEW.token_id AND network_id=NEW.network_id AND token_standard='native')
    OR NOT EXISTS(SELECT 1 FROM cat_wallets WHERE id=NEW.wallet_id AND network_id=NEW.network_id AND custody_merchant_id=NEW.merchant_id AND is_active AND NOT deletion_mark)
    OR NOT EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes WHERE merchant_id=NEW.merchant_id AND network_id=NEW.network_id AND token_id=NEW.token_id AND enabled AND version=NEW.scope_version)
    OR NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_risk_states WHERE wallet_id=NEW.wallet_id AND token_id=NEW.token_id AND revision=NEW.risk_revision AND scope_version=NEW.scope_version AND risk_class=NEW.risk_class AND inventory_covered AND expires_at>NOW())
    OR NOT fn_crypto_aml_total_inventory_covered(NEW.wallet_id,NEW.token_id)
    OR EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations WHERE basis_type=fee.basis_type AND basis_id=fee.basis_id) THEN
   RAISE EXCEPTION 'resource budget requires fresh owned operating inventory before authorization' USING ERRCODE='23514';
 END IF;
 IF EXISTS(SELECT 1 FROM reg_crypto_fee_reservations other WHERE other.payer_wallet_id=NEW.wallet_id AND other.native_token_id=NEW.token_id AND other.id<>fee.id AND other.status IN ('reserved','broadcast','held')
      AND (other.status='held' OR NOT EXISTS(SELECT 1 FROM reg_crypto_aml_resource_budgets b WHERE b.fee_reservation_id=other.id))) THEN
   RAISE EXCEPTION 'unallocated or held historical fee reservations block new resource spending' USING ERRCODE='23514';
 END IF;
 IF fee.purpose='native_funding' THEN
   IF NOT EXISTS(SELECT 1 FROM doc_crypto_native_fundings f JOIN cat_wallets sw ON sw.id=f.source_wallet_id AND sw.wallet_role='network_fee'
       JOIN cat_wallets dw ON dw.id=f.destination_wallet_id AND dw.custody_merchant_id=NEW.merchant_id AND dw.is_active AND NOT dw.deletion_mark
       JOIN reg_crypto_aml_inventory_positions inventory ON inventory.wallet_id=dw.id AND inventory.token_id=f.native_token_id
       JOIN reg_crypto_aml_resource_positions resource ON resource.wallet_id=dw.id AND resource.token_id=f.native_token_id
       JOIN reg_crypto_wallet_risk_states risk ON risk.wallet_id=dw.id AND risk.token_id=f.native_token_id AND risk.scope_version=NEW.scope_version AND risk.risk_class<>'high'
       WHERE f.id=NEW.funding_id AND f.id=fee.basis_id AND fee.basis_type='CryptoNativeFunding' AND f.fee_reservation_id=fee.id
         AND f.network_id=NEW.network_id AND f.native_token_id=NEW.token_id AND f.source_wallet_id=NEW.wallet_id AND f.amount_minor=NEW.funding_amount AND f.amount_minor=fee.reserved_principal_minor
         AND f.status IN ('created','reserved') AND f.signing_authorization_id IS NULL AND f.tx_hash=''
         AND (inventory.principal+inventory.service_fee+resource.amount=0 OR (risk.risk_class=NEW.risk_class AND risk.inventory_covered AND risk.expires_at>NOW()))
         AND fn_crypto_aml_total_inventory_covered(dw.id,f.native_token_id)
         AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_resource_budgets peer JOIN doc_crypto_native_fundings pf ON pf.id=peer.funding_id WHERE pf.destination_wallet_id=dw.id AND peer.status='reserved' AND peer.risk_class<>NEW.risk_class)) THEN
     RAISE EXCEPTION 'native funding resource destination or risk mismatch' USING ERRCODE='23514';
   END IF;
 ELSIF NEW.funding_id IS NOT NULL OR NEW.funding_amount<>0 THEN RAISE EXCEPTION 'non-funding operation cannot reserve a resource transfer' USING ERRCODE='23514'; END IF;
 PERFORM 1 FROM reg_crypto_aml_resource_positions WHERE wallet_id=NEW.wallet_id AND token_id=NEW.token_id FOR UPDATE;
 SELECT p.amount-COALESCE((SELECT SUM(CASE WHEN f.strategy<>'resource_rental' THEN b.max_fee ELSE 0 END + b.funding_amount)
   FROM reg_crypto_aml_resource_budgets b JOIN reg_crypto_fee_reservations f ON f.id=b.fee_reservation_id
   WHERE b.wallet_id=NEW.wallet_id AND b.token_id=NEW.token_id AND b.status='reserved'),0)
   INTO available FROM reg_crypto_aml_resource_positions p WHERE p.wallet_id=NEW.wallet_id AND p.token_id=NEW.token_id AND p.revision=NEW.resource_revision;
 IF available IS NULL OR available<(CASE WHEN fee.strategy<>'resource_rental' THEN NEW.max_fee ELSE 0 END + NEW.funding_amount) THEN
   RAISE EXCEPTION 'insufficient operating funds or stale resource revision; merchant principal cannot fund gas' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE OR REPLACE VIEW crypto_aml_ready_resource_budgets AS
 SELECT b.id,b.merchant_id,b.network_id,b.token_id,b.wallet_id,b.funding_id,b.funding_amount,b.max_fee,
        settlement.id settlement_id,settlement.status chain_status,settlement.total_fee_minor actual_fee,
        cost.line_id cost_movement_id,spent.line_id funding_source_movement_id,received.line_id funding_destination_movement_id,
        funding.destination_wallet_id,e.id effect_id
 FROM reg_crypto_aml_resource_budgets b JOIN reg_crypto_fee_reservations fee ON fee.id=b.fee_reservation_id
 JOIN reg_crypto_chain_settlements settlement ON settlement.network_id=b.network_id AND settlement.tx_hash=fee.tx_hash AND settlement.native_token_id=b.token_id
   AND settlement.basis_type=fee.basis_type AND settlement.basis_id=fee.basis_id AND settlement.total_fee_minor=fee.actual_fee_minor
 LEFT JOIN reg_crypto_balance_movements cost ON cost.line_id=fee.id AND cost.recorder_type='ChainSettlement' AND cost.recorder_id=fee.basis_id AND cost.recorder_version=1
   AND cost.record_type='expense' AND cost.wallet_id=b.wallet_id AND cost.token_id=b.token_id AND cost.amount=settlement.total_fee_minor
 LEFT JOIN doc_crypto_native_fundings funding ON funding.id=b.funding_id AND funding.fee_reservation_id=fee.id AND funding.tx_hash=fee.tx_hash
   AND funding.source_wallet_id=b.wallet_id AND funding.amount_minor=b.funding_amount AND funding.network_id=b.network_id AND funding.native_token_id=b.token_id
 LEFT JOIN reg_crypto_aml_effects e ON e.wallet_id=funding.destination_wallet_id AND e.source_wallet_id=b.wallet_id AND e.network_id=b.network_id AND e.token_id=b.token_id
   AND e.tx_hash=funding.tx_hash AND e.amount=b.funding_amount AND e.block_hash=settlement.block_hash AND e.block_number=settlement.block_height
 LEFT JOIN reg_crypto_aml_effect_chain_states cs ON cs.effect_id=e.id AND cs.canonicality='canonical'
 LEFT JOIN reg_crypto_expected_chain_effects expected ON expected.id=e.expected_effect_id
 LEFT JOIN reg_crypto_signing_authorizations auth ON auth.id=expected.signing_authorization_id AND auth.basis_type='CryptoNativeFunding' AND auth.basis_id=funding.id AND auth.signed_envelope IS NOT NULL
 LEFT JOIN reg_crypto_balance_movements spent ON spent.recorder_id=funding.id AND spent.recorder_type='CryptoNativeFunding' AND spent.recorder_version=1 AND spent.wallet_id=b.wallet_id AND spent.token_id=b.token_id AND spent.record_type='expense' AND spent.amount=b.funding_amount
 LEFT JOIN reg_crypto_balance_movements received ON received.recorder_id=funding.id AND received.recorder_type='CryptoNativeFunding' AND received.recorder_version=1 AND received.wallet_id=funding.destination_wallet_id AND received.token_id=b.token_id AND received.record_type='receipt' AND received.amount=b.funding_amount
 WHERE b.status IN ('reserved','settled','failed') AND fee.status='settled' AND fee.native_token_id=b.token_id AND fee.network_id=b.network_id AND fee.payer_wallet_id=b.wallet_id
   AND fee.max_fee_minor=b.max_fee AND settlement.total_fee_minor<=b.max_fee AND (settlement.total_fee_minor=0 OR cost.line_id IS NOT NULL)
   AND (b.funding_id IS NULL OR (settlement.status='reverted' AND funding.status='failed'
      AND NOT EXISTS(SELECT 1 FROM reg_crypto_balance_movements m WHERE m.recorder_id=funding.id AND m.recorder_type='CryptoNativeFunding')
      AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_effects transferred JOIN reg_crypto_aml_effect_chain_states state ON state.effect_id=transferred.id AND state.canonicality='canonical'
         WHERE transferred.network_id=b.network_id AND transferred.tx_hash=fee.tx_hash AND transferred.wallet_id=funding.destination_wallet_id AND transferred.token_id=b.token_id))
      OR (settlement.status='succeeded' AND funding.status='settled' AND cs.effect_id IS NOT NULL AND auth.id IS NOT NULL AND spent.line_id IS NOT NULL AND received.line_id IS NOT NULL
          AND (SELECT COUNT(*) FROM reg_crypto_balance_movements m WHERE m.recorder_id=funding.id AND m.recorder_type='CryptoNativeFunding')=2));
CREATE OR REPLACE FUNCTION fn_crypto_aml_resource_queue_trigger() RETURNS TRIGGER AS $$
DECLARE reservation UUID;
BEGIN
 IF TG_TABLE_NAME='reg_crypto_fee_reservations' THEN reservation:=NEW.id;
 ELSIF TG_TABLE_NAME='doc_crypto_native_fundings' THEN reservation:=NEW.fee_reservation_id;
 ELSIF TG_TABLE_NAME='reg_crypto_chain_settlements' THEN SELECT id INTO reservation FROM reg_crypto_fee_reservations WHERE network_id=NEW.network_id AND tx_hash=NEW.tx_hash;
 ELSIF TG_TABLE_NAME='reg_crypto_balance_movements' THEN
    IF NEW.recorder_type='ChainSettlement' THEN reservation:=NEW.line_id;
    ELSIF NEW.recorder_type='CryptoNativeFunding' THEN SELECT fee_reservation_id INTO reservation FROM doc_crypto_native_fundings WHERE id=NEW.recorder_id; END IF;
 ELSE
    SELECT b.fee_reservation_id INTO reservation FROM reg_crypto_aml_effects e JOIN doc_crypto_native_fundings f ON f.network_id=e.network_id AND f.tx_hash=e.tx_hash AND f.destination_wallet_id=e.wallet_id
      JOIN reg_crypto_aml_resource_budgets b ON b.funding_id=f.id WHERE e.id=NEW.effect_id;
 END IF;
 IF reservation IS NOT NULL THEN PERFORM fn_crypto_aml_queue_resource_budget(reservation); END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
