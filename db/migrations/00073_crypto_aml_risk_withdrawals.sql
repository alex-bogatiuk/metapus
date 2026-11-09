-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
CREATE TABLE reg_crypto_aml_withdrawal_plans (
 request_id UUID PRIMARY KEY REFERENCES doc_withdrawal_requests(id), merchant_id UUID NOT NULL REFERENCES cat_merchants(id), network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id), token_id UUID NOT NULL REFERENCES cat_tokens(id),
 risk_class TEXT NOT NULL CHECK(risk_class IN ('low','medium','high','unknown')), risk_accepted BOOLEAN NOT NULL, source_count INT NOT NULL CHECK(source_count BETWEEN 1 AND 1000),
 request_digest TEXT NOT NULL CHECK(request_digest ~ '^[a-f0-9]{64}$'), created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
 CHECK(risk_class NOT IN ('high','unknown') OR risk_accepted), FOREIGN KEY(merchant_id,network_id,token_id) REFERENCES sys_crypto_aml_routing_scopes(merchant_id,network_id,token_id)
);
CREATE TRIGGER trg_crypto_aml_withdrawal_plan_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_withdrawal_plans FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();
CREATE TABLE reg_crypto_aml_withdrawal_lines (
 id UUID PRIMARY KEY, request_id UUID NOT NULL REFERENCES reg_crypto_aml_withdrawal_plans(request_id), wallet_id UUID NOT NULL REFERENCES cat_wallets(id),
 token_id UUID NOT NULL REFERENCES cat_tokens(id), amount NUMERIC(78,0) NOT NULL CHECK(amount>0), source_revision BIGINT NOT NULL CHECK(source_revision>0), risk_revision BIGINT NOT NULL CHECK(risk_revision>0), scope_version BIGINT NOT NULL CHECK(scope_version>0),
 source_address TEXT NOT NULL CHECK(source_address<>''), source_vault_key_id TEXT NOT NULL CHECK(source_vault_key_id<>''), evidence_hash TEXT NOT NULL,
 withdrawal_id UUID UNIQUE REFERENCES doc_crypto_withdrawals(id), reservation_id UUID NOT NULL UNIQUE REFERENCES reg_crypto_balance_reservations(id),
 status TEXT NOT NULL DEFAULT 'reserved' CHECK(status IN ('reserved','held','broadcast','confirmed','failed','cancelled')),
 next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), lease_token UUID, lease_expires_at TIMESTAMPTZ, error_code TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL), UNIQUE(request_id,wallet_id)
);
CREATE INDEX idx_crypto_aml_withdrawal_due ON reg_crypto_aml_withdrawal_lines(next_attempt_at,id) WHERE status IN ('reserved','held','broadcast');
CREATE INDEX idx_crypto_aml_withdrawal_source ON reg_crypto_aml_withdrawal_lines(wallet_id,token_id) WHERE status IN ('reserved','held','broadcast');
CREATE TRIGGER trg_crypto_aml_withdrawal_line_updated BEFORE UPDATE ON reg_crypto_aml_withdrawal_lines FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_crypto_aml_withdrawal_line_txid BEFORE UPDATE ON reg_crypto_aml_withdrawal_lines FOR EACH ROW EXECUTE FUNCTION update_txid_column();
CREATE TABLE reg_crypto_aml_withdrawal_cancellations (
 line_id UUID PRIMARY KEY REFERENCES reg_crypto_aml_withdrawal_lines(id), reason TEXT NOT NULL CHECK(length(btrim(reason)) BETWEEN 3 AND 500), actor TEXT NOT NULL CHECK(btrim(actor)<>''),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL)
);
CREATE TRIGGER trg_crypto_aml_withdrawal_cancel_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_withdrawal_cancellations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();
CREATE TABLE reg_crypto_aml_withdrawal_facts (
 line_id UUID PRIMARY KEY REFERENCES reg_crypto_aml_withdrawal_lines(id), wallet_id UUID NOT NULL REFERENCES cat_wallets(id), token_id UUID NOT NULL REFERENCES cat_tokens(id),
 amount NUMERIC(78,0) NOT NULL CHECK(amount>0), succeeded BOOLEAN NOT NULL, settlement_id UUID NOT NULL UNIQUE REFERENCES reg_crypto_chain_settlements(id),
 expense_movement_id UUID NOT NULL UNIQUE REFERENCES reg_crypto_balance_movements(line_id), refund_movement_id UUID UNIQUE REFERENCES reg_crypto_balance_movements(line_id),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL), CHECK(succeeded=(refund_movement_id IS NULL))
);
CREATE TRIGGER trg_crypto_aml_withdrawal_fact_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_withdrawal_facts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE FUNCTION fn_crypto_aml_withdrawal_line_guard() RETURNS TRIGGER AS $$
DECLARE plan reg_crypto_aml_withdrawal_plans; available NUMERIC; inventory reg_crypto_aml_inventory_positions;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'withdrawal source evidence cannot be deleted' USING ERRCODE='23514'; END IF;
 SELECT * INTO STRICT plan FROM reg_crypto_aml_withdrawal_plans WHERE request_id=NEW.request_id;
 PERFORM 1 FROM sys_crypto_aml_routing_scopes s WHERE s.merchant_id=plan.merchant_id AND s.network_id=plan.network_id ORDER BY s.merchant_id,s.network_id,s.token_id FOR UPDATE;
 IF TG_OP='UPDATE' THEN
   IF (NEW.id,NEW.request_id,NEW.wallet_id,NEW.token_id,NEW.amount,NEW.source_revision,NEW.risk_revision,NEW.scope_version,NEW.source_address,NEW.source_vault_key_id,NEW.evidence_hash,NEW.reservation_id,NEW.created_at)
      IS DISTINCT FROM (OLD.id,OLD.request_id,OLD.wallet_id,OLD.token_id,OLD.amount,OLD.source_revision,OLD.risk_revision,OLD.scope_version,OLD.source_address,OLD.source_vault_key_id,OLD.evidence_hash,OLD.reservation_id,OLD.created_at)
      OR (OLD.withdrawal_id IS NOT NULL AND NEW.withdrawal_id IS DISTINCT FROM OLD.withdrawal_id)
      OR (OLD.status IN ('confirmed','failed','cancelled') AND NEW.status<>OLD.status) THEN RAISE EXCEPTION 'withdrawal line identity and terminal outcome are immutable' USING ERRCODE='23514'; END IF;
   IF NEW.status='cancelled' AND (NOT EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_cancellations WHERE line_id=NEW.id) OR EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations WHERE basis_type='CryptoAMLWithdrawalLine' AND basis_id=NEW.id AND status <> 'failed')) THEN RAISE EXCEPTION 'potentially signed withdrawal cannot be cancelled' USING ERRCODE='23514'; END IF;
   IF NEW.status IN ('confirmed','failed') AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_facts WHERE line_id=NEW.id AND succeeded=(NEW.status='confirmed')) THEN RAISE EXCEPTION 'withdrawal completion requires final evidence' USING ERRCODE='23514'; END IF;
   RETURN NEW;
 END IF;
 SELECT * INTO inventory FROM reg_crypto_aml_inventory_positions WHERE wallet_id=NEW.wallet_id AND token_id=NEW.token_id FOR UPDATE;
 available:=inventory.principal-COALESCE((SELECT SUM(principal) FROM reg_crypto_aml_internal_allocations WHERE source_wallet_id=NEW.wallet_id AND token_id=NEW.token_id AND status='reserved'),0)
   -COALESCE((SELECT SUM(amount) FROM reg_crypto_aml_withdrawal_lines WHERE wallet_id=NEW.wallet_id AND token_id=NEW.token_id AND status IN ('reserved','held','broadcast')),0);
 IF NEW.status<>'reserved' OR NEW.withdrawal_id IS NOT NULL OR NEW.token_id<>plan.token_id OR inventory.revision IS DISTINCT FROM NEW.source_revision OR available IS NULL OR available<NEW.amount
   OR NOT EXISTS(SELECT 1 FROM doc_withdrawal_requests r WHERE r.id=NEW.request_id AND r.merchant_id=plan.merchant_id AND r.token_id=plan.token_id AND r.risk_class=plan.risk_class AND r.posted AND r.posted_version=1 AND r.status='pending_signature')
   OR NOT EXISTS(SELECT 1 FROM cat_wallets w WHERE w.id=NEW.wallet_id AND w.custody_merchant_id=plan.merchant_id AND w.network_id=plan.network_id AND w.wallet_role IN ('merchant_pool','liquidity_hot') AND w.is_active AND NOT w.deletion_mark AND w.chain_activation_status='active' AND w.address=NEW.source_address AND w.vault_key_id=NEW.source_vault_key_id)
   OR NOT EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes WHERE merchant_id=plan.merchant_id AND network_id=plan.network_id AND token_id=plan.token_id AND enabled AND version=NEW.scope_version)
   OR NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_risk_states risk WHERE wallet_id=NEW.wallet_id AND token_id=NEW.token_id AND revision=NEW.risk_revision AND scope_version=NEW.scope_version AND risk_class=plan.risk_class AND evidence_hash=NEW.evidence_hash AND (plan.risk_class IN ('high','unknown') OR (inventory_covered AND expires_at>NOW())))
   OR NOT fn_crypto_aml_total_inventory_covered(NEW.wallet_id,NEW.token_id)
   OR NOT EXISTS(SELECT 1 FROM reg_crypto_balance_reservations reserve WHERE reserve.id=NEW.reservation_id AND reserve.wallet_id=NEW.wallet_id AND reserve.token_id=NEW.token_id AND reserve.document_type='CryptoAMLWithdrawalLine' AND reserve.document_id=NEW.id AND reserve.amount=NEW.amount AND reserve.status='active') THEN RAISE EXCEPTION 'withdrawal source must reserve exact covered owned principal in requested class' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_withdrawal_line_guard BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_aml_withdrawal_lines FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_withdrawal_line_guard();

CREATE VIEW crypto_aml_valid_withdrawal_facts AS
 SELECT f.line_id FROM reg_crypto_aml_withdrawal_facts f JOIN reg_crypto_aml_withdrawal_lines l ON l.id=f.line_id AND l.wallet_id=f.wallet_id AND l.token_id=f.token_id AND l.amount=f.amount
 JOIN reg_crypto_aml_withdrawal_plans p ON p.request_id=l.request_id
 JOIN doc_crypto_withdrawals w ON w.id=l.withdrawal_id AND w.basis_type='CryptoAMLWithdrawalLine' AND w.basis_id=l.id AND w.merchant_id=p.merchant_id AND w.source_wallet_id=l.wallet_id AND w.token_id=l.token_id AND w.amount=l.amount AND w.posted AND w.posted_version=1
 JOIN reg_crypto_signing_authorizations a ON a.basis_type='CryptoAMLWithdrawalLine' AND a.basis_id=l.id AND a.generation=1 AND a.attempt=1 AND a.signed_envelope IS NOT NULL AND a.tx_hash=w.tx_hash AND a.source_address=l.source_address AND a.vault_key_id=l.source_vault_key_id AND a.amount_minor_units=l.amount
 JOIN reg_crypto_aml_first_dispatches dispatched ON dispatched.authorization_id=a.id
 JOIN reg_crypto_chain_settlements s ON s.id=f.settlement_id AND s.network_id=p.network_id AND s.basis_type='CryptoAMLWithdrawalLine' AND s.basis_id=l.id AND s.tx_hash=w.tx_hash AND (s.status='succeeded')=f.succeeded
 JOIN reg_crypto_balance_movements m ON m.line_id=f.expense_movement_id AND m.recorder_type='CryptoWithdrawal' AND m.recorder_id=w.id AND m.recorder_version=1 AND m.record_type='expense' AND m.wallet_id=f.wallet_id AND m.token_id=f.token_id AND m.amount=f.amount
 LEFT JOIN reg_crypto_balance_movements refund ON refund.line_id=f.refund_movement_id AND refund.line_id=l.id AND refund.recorder_type='AMLWithdrawalRevert' AND refund.recorder_id=l.id AND refund.recorder_version=1 AND refund.record_type='receipt' AND refund.wallet_id=f.wallet_id AND refund.token_id=f.token_id AND refund.amount=f.amount
 WHERE (f.succeeded OR (refund.line_id IS NOT NULL AND EXISTS(SELECT 1 FROM reg_crypto_merchant_balance_movements mr JOIN doc_withdrawal_requests request ON request.id=l.request_id WHERE mr.line_id=l.id AND mr.recorder_id=l.request_id AND mr.recorder_type='WithdrawalRequest' AND mr.recorder_version=request.posted_version+1 AND mr.record_type='receipt' AND mr.merchant_id=p.merchant_id AND mr.token_id=l.token_id AND mr.amount=l.amount))) AND (SELECT COUNT(*) FROM reg_crypto_balance_movements WHERE recorder_id=w.id AND recorder_type='CryptoWithdrawal')=1;
CREATE FUNCTION fn_crypto_aml_withdrawal_fact_guard() RETURNS TRIGGER AS $$
BEGIN
 PERFORM 1 FROM reg_crypto_aml_withdrawal_plans p JOIN reg_crypto_aml_withdrawal_lines l ON l.request_id=p.request_id JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=p.merchant_id AND s.network_id=p.network_id WHERE l.id=NEW.line_id ORDER BY s.merchant_id,s.network_id,s.token_id FOR UPDATE OF s;
 IF EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_facts WHERE line_id=NEW.line_id) THEN
   IF EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_facts f WHERE f.line_id=NEW.line_id AND (f.wallet_id,f.token_id,f.amount,f.succeeded,f.settlement_id,f.expense_movement_id,f.refund_movement_id) IS NOT DISTINCT FROM (NEW.wallet_id,NEW.token_id,NEW.amount,NEW.succeeded,NEW.settlement_id,NEW.expense_movement_id,NEW.refund_movement_id)) THEN RETURN NEW; END IF;
   RAISE EXCEPTION 'withdrawal outcome fact differs from saved evidence' USING ERRCODE='23514';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_lines WHERE id=NEW.line_id AND wallet_id=NEW.wallet_id AND token_id=NEW.token_id AND amount=NEW.amount AND status IN ('reserved','held','broadcast')) THEN RAISE EXCEPTION 'withdrawal fact has no outstanding source line' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_withdrawal_fact_guard BEFORE INSERT ON reg_crypto_aml_withdrawal_facts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_withdrawal_fact_guard();
CREATE FUNCTION fn_crypto_aml_withdrawal_fact_apply() RETURNS TRIGGER AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM crypto_aml_valid_withdrawal_facts WHERE line_id=NEW.line_id) THEN RAISE EXCEPTION 'withdrawal fact requires exact final chain, signing and physical evidence' USING ERRCODE='23514'; END IF;
 UPDATE reg_crypto_aml_inventory_positions SET principal=principal-CASE WHEN NEW.succeeded THEN NEW.amount ELSE 0 END,revision=revision+1 WHERE wallet_id=NEW.wallet_id AND token_id=NEW.token_id;
 UPDATE reg_crypto_aml_withdrawal_lines SET status=CASE WHEN NEW.succeeded THEN 'confirmed' ELSE 'failed' END,lease_token=NULL,lease_expires_at=NULL WHERE id=NEW.line_id;
 PERFORM fn_crypto_aml_invalidate_wallet(NEW.wallet_id,NEW.token_id,'withdrawal_settled');
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_withdrawal_fact_apply AFTER INSERT ON reg_crypto_aml_withdrawal_facts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_withdrawal_fact_apply();

CREATE FUNCTION fn_crypto_aml_withdrawal_plan_complete() RETURNS TRIGGER AS $$
DECLARE parent UUID; expected_count INT; expected_amount NUMERIC;
BEGIN
 parent:=NEW.request_id;
 SELECT p.source_count,r.amount INTO expected_count,expected_amount FROM reg_crypto_aml_withdrawal_plans p JOIN doc_withdrawal_requests r ON r.id=p.request_id WHERE p.request_id=parent;
 IF (SELECT COUNT(*) FROM reg_crypto_merchant_balance_movements WHERE recorder_id=parent AND recorder_type='WithdrawalRequest' AND record_type='expense')<>1 OR (SELECT SUM(CASE WHEN record_type='expense' THEN amount ELSE -amount END) FROM reg_crypto_merchant_balance_movements WHERE recorder_id=parent AND recorder_type='WithdrawalRequest') IS DISTINCT FROM expected_amount THEN RAISE EXCEPTION 'withdrawal plan requires its single exact merchant debit' USING ERRCODE='23514'; END IF;
 IF (SELECT COUNT(*) FROM reg_crypto_aml_withdrawal_lines WHERE request_id=parent)<>expected_count OR (SELECT SUM(amount) FROM reg_crypto_aml_withdrawal_lines WHERE request_id=parent) IS DISTINCT FROM expected_amount THEN RAISE EXCEPTION 'withdrawal requires its complete immutable source plan' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE CONSTRAINT TRIGGER trg_crypto_aml_withdrawal_plan_complete AFTER INSERT ON reg_crypto_aml_withdrawal_plans DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_withdrawal_plan_complete();
CREATE CONSTRAINT TRIGGER trg_crypto_aml_withdrawal_lines_complete AFTER INSERT ON reg_crypto_aml_withdrawal_lines DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_withdrawal_plan_complete();

CREATE FUNCTION fn_crypto_aml_check_withdrawal_dispatch(a reg_crypto_signing_authorizations,phase TEXT) RETURNS VOID AS $$
DECLARE line reg_crypto_aml_withdrawal_lines; plan reg_crypto_aml_withdrawal_plans;
BEGIN
 IF a.purpose<>'withdrawal' THEN RETURN; END IF;
 SELECT * INTO line FROM reg_crypto_aml_withdrawal_lines WHERE id=a.basis_id AND a.basis_type='CryptoAMLWithdrawalLine';
 IF NOT FOUND THEN RAISE EXCEPTION 'AML withdrawal requires an immutable approved source line' USING ERRCODE='23514'; END IF;
 SELECT * INTO STRICT plan FROM reg_crypto_aml_withdrawal_plans WHERE request_id=line.request_id;
 IF line.status NOT IN ('reserved','held','broadcast') OR a.merchant_id IS DISTINCT FROM plan.merchant_id OR a.network_id<>plan.network_id OR a.source_address<>line.source_address OR a.vault_key_id<>line.source_vault_key_id OR a.amount_minor_units<>line.amount OR a.generation<>1 OR a.attempt<>1
   OR NOT EXISTS(SELECT 1 FROM doc_withdrawal_requests r WHERE r.id=plan.request_id AND r.status IN ('approved','signing','broadcast','held') AND r.dest_address=a.destination_address AND r.token_id=plan.token_id AND r.merchant_id=plan.merchant_id)
   OR NOT EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes WHERE merchant_id=plan.merchant_id AND network_id=plan.network_id AND token_id=plan.token_id AND enabled AND version=line.scope_version)
   OR NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_risk_states WHERE wallet_id=line.wallet_id AND token_id=line.token_id AND (revision=line.risk_revision OR (phase='broadcast' AND a.signed_envelope IS NOT NULL)) AND scope_version=line.scope_version AND risk_class=plan.risk_class AND (evidence_hash=line.evidence_hash OR (phase='broadcast' AND a.signed_envelope IS NOT NULL)) AND (plan.risk_class IN ('high','unknown') OR (inventory_covered AND expires_at>NOW())))
   OR EXISTS(SELECT 1 FROM reg_crypto_wallet_allocation_holds WHERE wallet_id=line.wallet_id AND status='open' AND (plan.risk_class IN ('low','medium') OR reason NOT IN ('aml_pool_high_risk')))
   OR NOT fn_crypto_aml_total_inventory_covered(line.wallet_id,line.token_id)
   OR EXISTS(SELECT 1 FROM cat_wallets w WHERE w.network_id=plan.network_id AND w.address_key=CASE WHEN a.chain_family='evm' THEN LOWER(a.destination_address) ELSE a.destination_address END) THEN RAISE EXCEPTION 'withdrawal source risk, scope or destination changed' USING ERRCODE='23514'; END IF;
END;
$$ LANGUAGE plpgsql;
CREATE FUNCTION fn_crypto_aml_withdrawal_signing_guard() RETURNS TRIGGER AS $$
BEGIN
 IF NEW.basis_type='CryptoAMLWithdrawalLine' THEN PERFORM fn_crypto_aml_check_withdrawal_dispatch(NEW,'authorize'); END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_withdrawal_signing_guard BEFORE INSERT ON reg_crypto_signing_authorizations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_withdrawal_signing_guard();

CREATE FUNCTION fn_crypto_aml_withdrawal_parent_guard() RETURNS TRIGGER AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_plans WHERE request_id=OLD.id) THEN
   IF OLD.status IN ('confirmed','failed','rejected','partially_completed') AND NEW.status IS DISTINCT FROM OLD.status THEN RAISE EXCEPTION 'terminal AML withdrawal cannot be reopened' USING ERRCODE='23514'; END IF;
   IF NEW.status IN ('confirmed','failed','rejected','partially_completed') AND (
     EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_lines WHERE request_id=OLD.id AND status IN ('reserved','held','broadcast'))
     OR (NEW.status='confirmed' AND EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_lines WHERE request_id=OLD.id AND status<>'confirmed'))
     OR (NEW.status IN ('failed','rejected') AND EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_lines WHERE request_id=OLD.id AND status='confirmed'))
     OR (NEW.status='rejected' AND EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_lines WHERE request_id=OLD.id AND status<>'cancelled'))
     OR (NEW.status='partially_completed' AND (NOT EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_lines WHERE request_id=OLD.id AND status='confirmed') OR NOT EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_lines WHERE request_id=OLD.id AND status IN ('failed','cancelled'))))
     OR (SELECT COALESCE(SUM(CASE WHEN record_type='expense' THEN amount ELSE -amount END),0) FROM reg_crypto_merchant_balance_movements WHERE recorder_id=OLD.id AND recorder_type='WithdrawalRequest') IS DISTINCT FROM (SELECT COALESCE(SUM(amount),0) FROM reg_crypto_aml_withdrawal_lines WHERE request_id=OLD.id AND status='confirmed')) THEN RAISE EXCEPTION 'terminal AML withdrawal requires exact completed lines and refunds' USING ERRCODE='23514'; END IF;
 END IF;
 IF EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_plans WHERE request_id=OLD.id) AND (TG_OP='DELETE' OR (NEW.id,NEW.merchant_id,NEW.token_id,NEW.amount,NEW.dest_address,NEW.address_id,NEW.posted_version,NEW.posted,NEW.withdrawal_id,NEW.risk_class) IS DISTINCT FROM (OLD.id,OLD.merchant_id,OLD.token_id,OLD.amount,OLD.dest_address,OLD.address_id,OLD.posted_version,OLD.posted,OLD.withdrawal_id,OLD.risk_class)) THEN RAISE EXCEPTION 'AML withdrawal parent financial identity is immutable' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_withdrawal_parent_guard BEFORE UPDATE OR DELETE ON doc_withdrawal_requests FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_withdrawal_parent_guard();
CREATE FUNCTION fn_crypto_aml_refresh_withdrawal(parent UUID) RETURNS VOID AS $$
BEGIN
 UPDATE doc_withdrawal_requests r SET status=CASE
   WHEN EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_lines WHERE request_id=parent AND status='held') THEN 'held'
   WHEN EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_lines WHERE request_id=parent AND status='reserved') THEN CASE WHEN r.status='held' THEN 'held' ELSE 'signing' END
   WHEN EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_lines WHERE request_id=parent AND status='broadcast') THEN 'broadcast'
   WHEN NOT EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_lines WHERE request_id=parent AND status<>'confirmed') THEN 'confirmed'
   WHEN NOT EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_lines WHERE request_id=parent AND status='confirmed') THEN 'failed'
   ELSE 'partially_completed' END,updated_at=NOW(),version=version+1 WHERE r.id=parent;
END;
$$ LANGUAGE plpgsql;
ALTER TABLE doc_withdrawal_requests DROP CONSTRAINT doc_withdrawal_requests_status_check;
ALTER TABLE doc_withdrawal_requests ADD CONSTRAINT doc_withdrawal_requests_status_check CHECK(status IN ('pending_signature','pending_approval','approved','signing','broadcast','confirmed','held','rejected','failed','partially_completed'));

CREATE OR REPLACE FUNCTION fn_crypto_aml_inventory_covered(target UUID,asset UUID) RETURNS BOOLEAN AS $$
 WITH RECURSIVE ancestors(wallet_id) AS (
    SELECT target UNION SELECT e.source_wallet_id FROM crypto_aml_risk_edges e JOIN ancestors a ON a.wallet_id=e.destination_wallet_id WHERE e.token_id=asset
 ), inventory AS (
    SELECT p.*,b.created_at baseline_at,COALESCE(physical.amount,0) physical_amount,
      COALESCE((SELECT SUM(CASE WHEN f.destination_wallet_id=p.wallet_id THEN f.principal ELSE -f.principal END) FROM reg_crypto_aml_inventory_facts f WHERE f.token_id=asset AND (f.destination_wallet_id=p.wallet_id OR f.source_wallet_id=p.wallet_id)),0)-COALESCE((SELECT SUM(amount) FROM reg_crypto_aml_withdrawal_facts wf WHERE wf.wallet_id=p.wallet_id AND wf.token_id=asset AND wf.succeeded),0) journal_principal,
      COALESCE((SELECT SUM(CASE WHEN f.destination_wallet_id=p.wallet_id THEN f.service_fee ELSE -f.service_fee END) FROM reg_crypto_aml_inventory_facts f WHERE f.token_id=asset AND (f.destination_wallet_id=p.wallet_id OR f.source_wallet_id=p.wallet_id)),0) journal_fee
    FROM ancestors a LEFT JOIN reg_crypto_aml_inventory_positions p ON p.wallet_id=a.wallet_id AND p.token_id=asset
    LEFT JOIN reg_crypto_aml_wallet_baselines b ON b.wallet_id=p.wallet_id AND b.token_id=asset
    LEFT JOIN reg_crypto_balance_balances physical ON physical.wallet_id=p.wallet_id AND physical.token_id=asset
 ) SELECT NOT EXISTS(SELECT 1 FROM inventory i WHERE i.wallet_id IS NULL OR i.baseline_at IS NULL OR i.principal<>i.journal_principal OR i.service_fee<>i.journal_fee OR i.principal+i.service_fee<>i.physical_amount)
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_inventory_facts f JOIN crypto_aml_inventory_fact_validity v ON v.id=f.id
    WHERE f.token_id=asset AND (f.destination_wallet_id IN (SELECT wallet_id FROM ancestors) OR f.source_wallet_id IN (SELECT wallet_id FROM ancestors)) AND NOT v.valid)
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_facts wf WHERE wf.wallet_id IN (SELECT wallet_id FROM ancestors) AND wf.token_id=asset AND NOT EXISTS(SELECT 1 FROM crypto_aml_valid_withdrawal_facts v WHERE v.line_id=wf.line_id))
 AND NOT EXISTS(SELECT 1 FROM inventory i JOIN reg_crypto_balance_movements m ON m.wallet_id=i.wallet_id AND m.token_id=asset AND m.created_at>=i.baseline_at
    WHERE CASE WHEN m.record_type='receipt' THEN COALESCE((SELECT SUM(wf.amount) FROM reg_crypto_aml_withdrawal_facts wf WHERE wf.refund_movement_id=m.line_id),0)+ COALESCE((SELECT SUM(f.gross) FROM reg_crypto_aml_inventory_facts f WHERE f.destination_movement_id=m.line_id),0)
       ELSE COALESCE((SELECT SUM(wf.amount) FROM reg_crypto_aml_withdrawal_facts wf WHERE wf.expense_movement_id=m.line_id),0)+COALESCE((SELECT SUM(f.gross) FROM reg_crypto_aml_inventory_facts f WHERE f.source_movement_id=m.line_id),0) END <>m.amount)
 AND NOT EXISTS(SELECT 1 FROM inventory i JOIN reg_crypto_aml_wallet_baselines b ON b.wallet_id=i.wallet_id AND b.token_id=asset
    JOIN reg_crypto_aml_effects e ON e.wallet_id=i.wallet_id AND e.token_id=asset AND e.block_number>b.block_number
    JOIN reg_crypto_aml_effect_chain_states cs ON cs.effect_id=e.id AND cs.canonicality='canonical'
    WHERE NOT EXISTS(SELECT 1 FROM reg_crypto_aml_inventory_facts f WHERE f.effect_id=e.id));
$$ LANGUAGE SQL STABLE;

CREATE OR REPLACE FUNCTION fn_crypto_aml_total_inventory_covered(target UUID,asset UUID) RETURNS BOOLEAN AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM cat_tokens WHERE id=asset AND token_standard='native') THEN RETURN fn_crypto_aml_inventory_covered(target,asset); END IF;
 RETURN (WITH RECURSIVE ancestors(wallet_id) AS (
    SELECT target UNION SELECT e.source_wallet_id FROM crypto_aml_risk_edges e JOIN ancestors a ON a.wallet_id=e.destination_wallet_id WHERE e.token_id=asset
 ), inventory AS (
    SELECT a.wallet_id,p.principal,p.service_fee,r.amount resource_amount,b.created_at baseline_at,b.block_number,COALESCE(physical.amount,0) physical_amount,
      COALESCE((SELECT SUM(CASE WHEN f.destination_wallet_id=a.wallet_id THEN f.principal ELSE -f.principal END) FROM reg_crypto_aml_inventory_facts f WHERE f.token_id=asset AND (f.destination_wallet_id=a.wallet_id OR f.source_wallet_id=a.wallet_id)),0)-COALESCE((SELECT SUM(amount) FROM reg_crypto_aml_withdrawal_facts wf WHERE wf.wallet_id=a.wallet_id AND wf.token_id=asset AND wf.succeeded),0) journal_principal,
      COALESCE((SELECT SUM(CASE WHEN f.destination_wallet_id=a.wallet_id THEN f.service_fee ELSE -f.service_fee END) FROM reg_crypto_aml_inventory_facts f WHERE f.token_id=asset AND (f.destination_wallet_id=a.wallet_id OR f.source_wallet_id=a.wallet_id)),0) journal_fee,
      COALESCE((SELECT SUM(CASE WHEN f.destination_wallet_id=a.wallet_id THEN f.amount ELSE -f.amount END) FROM reg_crypto_aml_resource_facts f WHERE f.token_id=asset AND (f.destination_wallet_id=a.wallet_id OR f.source_wallet_id=a.wallet_id)),0) journal_resource
    FROM ancestors a LEFT JOIN reg_crypto_aml_inventory_positions p ON p.wallet_id=a.wallet_id AND p.token_id=asset
    LEFT JOIN reg_crypto_aml_resource_positions r ON r.wallet_id=a.wallet_id AND r.token_id=asset
    LEFT JOIN reg_crypto_aml_wallet_baselines b ON b.wallet_id=a.wallet_id AND b.token_id=asset
    LEFT JOIN reg_crypto_balance_balances physical ON physical.wallet_id=a.wallet_id AND physical.token_id=asset
 ) SELECT NOT EXISTS(SELECT 1 FROM inventory i WHERE i.principal IS NULL OR i.resource_amount IS NULL OR i.baseline_at IS NULL
     OR i.principal<>i.journal_principal OR i.service_fee<>i.journal_fee OR i.resource_amount<>i.journal_resource OR i.principal+i.service_fee+i.resource_amount<>i.physical_amount)
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_inventory_facts f JOIN crypto_aml_inventory_fact_validity v ON v.id=f.id
     WHERE f.token_id=asset AND (f.destination_wallet_id IN (SELECT wallet_id FROM ancestors) OR f.source_wallet_id IN (SELECT wallet_id FROM ancestors)) AND NOT v.valid)
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_resource_facts f JOIN crypto_aml_resource_fact_validity v ON v.id=f.id
     WHERE f.token_id=asset AND (f.destination_wallet_id IN (SELECT wallet_id FROM ancestors) OR f.source_wallet_id IN (SELECT wallet_id FROM ancestors)) AND NOT v.valid)
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_facts wf WHERE wf.wallet_id IN (SELECT wallet_id FROM ancestors) AND wf.token_id=asset AND NOT EXISTS(SELECT 1 FROM crypto_aml_valid_withdrawal_facts v WHERE v.line_id=wf.line_id))
 AND NOT EXISTS(SELECT 1 FROM inventory i JOIN reg_crypto_balance_movements m ON m.wallet_id=i.wallet_id AND m.token_id=asset AND m.created_at>=i.baseline_at
     WHERE CASE WHEN m.record_type='receipt' THEN COALESCE((SELECT SUM(wf.amount) FROM reg_crypto_aml_withdrawal_facts wf WHERE wf.refund_movement_id=m.line_id),0)+ COALESCE((SELECT SUM(f.gross) FROM reg_crypto_aml_inventory_facts f WHERE f.destination_movement_id=m.line_id),0)+COALESCE((SELECT SUM(f.amount) FROM reg_crypto_aml_resource_facts f WHERE f.destination_movement_id=m.line_id),0)
       ELSE COALESCE((SELECT SUM(wf.amount) FROM reg_crypto_aml_withdrawal_facts wf WHERE wf.expense_movement_id=m.line_id),0)+COALESCE((SELECT SUM(f.gross) FROM reg_crypto_aml_inventory_facts f WHERE f.source_movement_id=m.line_id),0)+COALESCE((SELECT SUM(f.amount) FROM reg_crypto_aml_resource_facts f WHERE f.source_movement_id=m.line_id),0) END<>m.amount)
 AND NOT EXISTS(SELECT 1 FROM inventory i JOIN reg_crypto_aml_effects e ON e.wallet_id=i.wallet_id AND e.token_id=asset AND e.block_number>i.block_number
     JOIN reg_crypto_aml_effect_chain_states cs ON cs.effect_id=e.id AND cs.canonicality='canonical'
     WHERE NOT EXISTS(SELECT 1 FROM reg_crypto_aml_inventory_facts f WHERE f.effect_id=e.id) AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_resource_facts f WHERE f.effect_id=e.id)));
END;
$$ LANGUAGE plpgsql STABLE;

CREATE OR REPLACE FUNCTION fn_crypto_aml_allocation_guard() RETURNS TRIGGER AS $$
DECLARE scoped sys_crypto_aml_routing_scopes; source reg_crypto_aml_inventory_positions; available_principal NUMERIC; available_fee NUMERIC; destination_empty BOOLEAN;
BEGIN
    IF TG_OP='DELETE' THEN RAISE EXCEPTION 'AML allocation evidence cannot be deleted' USING ERRCODE='23514'; END IF;
    SELECT * INTO scoped FROM sys_crypto_aml_routing_scopes WHERE merchant_id=NEW.merchant_id AND network_id=NEW.network_id AND token_id=NEW.token_id FOR UPDATE;
    IF TG_OP='UPDATE' THEN
        IF (NEW.id,NEW.sweep_attempt_id,NEW.merchant_id,NEW.network_id,NEW.token_id,NEW.source_wallet_id,NEW.destination_wallet_id,NEW.principal,NEW.service_fee,NEW.source_revision,NEW.risk_revision,NEW.scope_version,NEW.risk_class,NEW.request_digest,NEW.created_at)
           IS DISTINCT FROM (OLD.id,OLD.sweep_attempt_id,OLD.merchant_id,OLD.network_id,OLD.token_id,OLD.source_wallet_id,OLD.destination_wallet_id,OLD.principal,OLD.service_fee,OLD.source_revision,OLD.risk_revision,OLD.scope_version,OLD.risk_class,OLD.request_digest,OLD.created_at)
           OR (NEW.status<>OLD.status AND (OLD.status<>'reserved' OR NEW.status NOT IN ('applied','cancelled'))) THEN
            RAISE EXCEPTION 'AML allocation identity/status is immutable' USING ERRCODE='23514';
        END IF;
        IF NEW.status='applied' AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_inventory_facts WHERE allocation_id=NEW.id) THEN
            RAISE EXCEPTION 'AML allocation requires an applied chain-backed fact' USING ERRCODE='23514';
        END IF;
        IF NEW.status='cancelled' AND (NOT EXISTS(SELECT 1 FROM reg_crypto_aml_allocation_cancellations WHERE allocation_id=NEW.id)
            OR EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations WHERE basis_type='CryptoSweepAttempt' AND basis_id=NEW.sweep_attempt_id AND status <> 'failed')) THEN
            RAISE EXCEPTION 'cannot release an allocation after authorization began' USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.status<>'reserved' THEN RAISE EXCEPTION 'AML allocation must begin reserved' USING ERRCODE='23514'; END IF;
    PERFORM 1 FROM reg_crypto_aml_inventory_positions WHERE token_id=NEW.token_id AND wallet_id IN (NEW.source_wallet_id,NEW.destination_wallet_id) ORDER BY wallet_id FOR UPDATE;
    SELECT * INTO source FROM reg_crypto_aml_inventory_positions WHERE wallet_id=NEW.source_wallet_id AND token_id=NEW.token_id;
    SELECT source.principal-COALESCE(SUM(principal),0)-COALESCE((SELECT SUM(amount) FROM reg_crypto_aml_withdrawal_lines WHERE wallet_id=NEW.source_wallet_id AND token_id=NEW.token_id AND status IN ('reserved','held','broadcast')),0),source.service_fee-COALESCE(SUM(service_fee),0) INTO available_principal,available_fee
        FROM reg_crypto_aml_internal_allocations WHERE source_wallet_id=NEW.source_wallet_id AND token_id=NEW.token_id AND status='reserved';
    IF NOT scoped.enabled OR scoped.version<>NEW.scope_version OR source.wallet_id IS NULL OR source.revision<>NEW.source_revision
       OR NOT fn_crypto_aml_inventory_covered(NEW.source_wallet_id,NEW.token_id) OR NOT fn_crypto_aml_inventory_covered(NEW.destination_wallet_id,NEW.token_id)
       OR available_principal<0 OR available_fee<0 OR available_principal+available_fee<NEW.principal+NEW.service_fee
       OR NEW.principal<>div((NEW.principal+NEW.service_fee)*available_principal,NULLIF(available_principal+available_fee,0)) THEN
        RAISE EXCEPTION 'AML allocation inventory/revision mismatch' USING ERRCODE='23514';
    END IF;
    IF NOT EXISTS(SELECT 1 FROM reg_crypto_sweep_attempts attempt JOIN cat_wallets sw ON sw.id=attempt.wallet_id JOIN cat_wallets dw ON dw.id=attempt.hot_wallet_id
        JOIN cat_tokens token ON token.id=attempt.token_id JOIN cat_blockchain_networks network ON network.id=attempt.network_id
        WHERE attempt.id=NEW.sweep_attempt_id AND attempt.network_id=NEW.network_id AND attempt.token_id=NEW.token_id AND token.token_standard<>'native'
        AND attempt.wallet_id=NEW.source_wallet_id AND attempt.hot_wallet_id=NEW.destination_wallet_id AND attempt.amount=NEW.principal+NEW.service_fee
        AND attempt.status IN ('claimed','resource_ready') AND attempt.expires_at>NOW() AND sw.wallet_role='merchant_pool' AND dw.wallet_role='liquidity_hot'
        AND sw.custody_merchant_id=NEW.merchant_id AND dw.custody_merchant_id=NEW.merchant_id AND sw.is_active AND dw.is_active AND NOT sw.deletion_mark AND NOT dw.deletion_mark
        AND sw.address_key=CASE WHEN network.chain_family='evm' THEN LOWER(attempt.source_address) ELSE attempt.source_address END
        AND dw.address_key=CASE WHEN network.chain_family='evm' THEN LOWER(attempt.destination_address) ELSE attempt.destination_address END)
       OR EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations WHERE basis_type='CryptoSweepAttempt' AND basis_id=NEW.sweep_attempt_id)
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_risk_states WHERE wallet_id=NEW.source_wallet_id AND token_id=NEW.token_id
           AND risk_class=NEW.risk_class AND revision=NEW.risk_revision AND scope_version=NEW.scope_version AND inventory_covered AND expires_at>NOW()) THEN
        RAISE EXCEPTION 'AML allocation requires a fresh unsigned owned token sweep' USING ERRCODE='23514';
    END IF;
    SELECT principal+service_fee=0 INTO destination_empty FROM reg_crypto_aml_inventory_positions WHERE wallet_id=NEW.destination_wallet_id AND token_id=NEW.token_id;
    IF NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_risk_states WHERE wallet_id=NEW.destination_wallet_id AND token_id=NEW.token_id AND scope_version=NEW.scope_version
        AND risk_class<>'high' AND (destination_empty OR (risk_class=NEW.risk_class AND inventory_covered AND expires_at>NOW())))
       OR EXISTS(SELECT 1 FROM reg_crypto_aml_internal_allocations WHERE destination_wallet_id=NEW.destination_wallet_id AND token_id=NEW.token_id AND status='reserved' AND risk_class<>NEW.risk_class) THEN
        RAISE EXCEPTION 'AML allocation destination risk mismatch' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_crypto_aml_inventory_fact_guard() RETURNS TRIGGER AS $$
BEGIN
    PERFORM 1 FROM sys_crypto_aml_routing_scopes WHERE merchant_id=NEW.merchant_id AND network_id=NEW.network_id AND token_id=NEW.token_id AND enabled FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION 'AML inventory requires an enabled owned scope' USING ERRCODE='23514'; END IF;
    PERFORM 1 FROM reg_crypto_aml_inventory_positions WHERE token_id=NEW.token_id AND wallet_id IN (NEW.source_wallet_id,NEW.destination_wallet_id) ORDER BY wallet_id FOR UPDATE;
    IF NEW.kind='payment' THEN
        IF NOT EXISTS(SELECT 1 FROM crypto_aml_allocatable_payments p WHERE p.payment_id=NEW.payment_id AND p.payment_version=NEW.payment_version
            AND p.merchant_id=NEW.merchant_id AND p.network_id=NEW.network_id AND p.token_id=NEW.token_id AND p.wallet_id=NEW.destination_wallet_id
            AND p.principal=NEW.principal AND p.service_fee=NEW.service_fee AND p.effect_id=NEW.effect_id AND p.movement_id=NEW.destination_movement_id) THEN
            RAISE EXCEPTION 'AML payment allocation does not match financial/chain evidence' USING ERRCODE='23514';
        END IF;
    ELSE
        IF NOT EXISTS(SELECT 1 FROM crypto_aml_allocatable_transfers t JOIN reg_crypto_aml_internal_allocations plan ON plan.id=t.allocation_id AND plan.status='reserved'
            WHERE t.allocation_id=NEW.allocation_id AND t.merchant_id=NEW.merchant_id AND t.network_id=NEW.network_id AND t.token_id=NEW.token_id
              AND t.source_wallet_id=NEW.source_wallet_id AND t.destination_wallet_id=NEW.destination_wallet_id AND t.principal=NEW.principal AND t.service_fee=NEW.service_fee
              AND t.effect_id=NEW.effect_id AND t.source_movement_id=NEW.source_movement_id AND t.destination_movement_id=NEW.destination_movement_id) THEN
            RAISE EXCEPTION 'AML transfer allocation does not match reserved plan and posted chain effect' USING ERRCODE='23514';
        END IF;
    END IF;
    IF NEW.principal+NEW.service_fee+COALESCE((SELECT SUM(gross) FROM reg_crypto_aml_inventory_facts WHERE destination_movement_id=NEW.destination_movement_id),0)
       >(SELECT amount FROM reg_crypto_balance_movements WHERE line_id=NEW.destination_movement_id) THEN
        RAISE EXCEPTION 'AML receipt allocation exceeds physical movement' USING ERRCODE='23514';
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

ALTER TABLE reg_crypto_aml_resource_budgets DROP CONSTRAINT reg_crypto_aml_resource_budgets_risk_class_check;
ALTER TABLE reg_crypto_aml_resource_budgets ADD CONSTRAINT reg_crypto_aml_resource_budgets_risk_class_check CHECK(risk_class IN ('low','medium','high','unknown'));
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
    OR NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_risk_states WHERE wallet_id=NEW.wallet_id AND token_id=NEW.token_id AND revision=NEW.risk_revision AND scope_version=NEW.scope_version AND risk_class=NEW.risk_class AND (inventory_covered AND expires_at>NOW() OR (NEW.risk_class IN ('high','unknown') AND fee.purpose='withdrawal' AND EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_lines l JOIN reg_crypto_aml_withdrawal_plans p ON p.request_id=l.request_id WHERE l.id=fee.basis_id AND fee.basis_type='CryptoAMLWithdrawalLine' AND l.wallet_id=NEW.wallet_id AND p.risk_class=NEW.risk_class AND p.risk_accepted))))
    OR NOT fn_crypto_aml_total_inventory_covered(NEW.wallet_id,NEW.token_id)
    OR EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations WHERE basis_type=fee.basis_type AND basis_id=fee.basis_id) THEN
   RAISE EXCEPTION 'resource budget requires fresh owned operating inventory before authorization' USING ERRCODE='23514';
 END IF;
 IF EXISTS(SELECT 1 FROM reg_crypto_fee_reservations other WHERE other.payer_wallet_id=NEW.wallet_id AND other.native_token_id=NEW.token_id AND other.id<>fee.id AND other.status IN ('reserved','broadcast','held')
      AND (other.status='held' OR NOT EXISTS(SELECT 1 FROM reg_crypto_aml_resource_budgets b WHERE b.fee_reservation_id=other.id))) THEN
   RAISE EXCEPTION 'unallocated or held historical fee reservations block new resource spending' USING ERRCODE='23514';
 END IF;
 IF NEW.risk_class IN ('high','unknown') AND (fee.purpose<>'withdrawal' OR NOT EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_lines l JOIN reg_crypto_aml_withdrawal_plans p ON p.request_id=l.request_id WHERE l.id=fee.basis_id AND fee.basis_type='CryptoAMLWithdrawalLine' AND l.wallet_id=NEW.wallet_id AND p.risk_class=NEW.risk_class AND p.risk_accepted)) THEN RAISE EXCEPTION 'high/unknown operating risk requires explicit same-class withdrawal export' USING ERRCODE='23514'; END IF;
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
CREATE OR REPLACE FUNCTION fn_crypto_aml_resource_signing_guard() RETURNS TRIGGER AS $$
BEGIN PERFORM fn_crypto_aml_check_resource_dispatch(NEW,'authorize'); RETURN NEW; END;
$$ LANGUAGE plpgsql;

-- Debit-first principal can only return against a cancelled unsigned line or
-- the final revert of that exact signed line. Full-parent storno after a
-- partial success is rejected even if an old application path calls it.
CREATE FUNCTION fn_crypto_aml_withdrawal_merchant_movement_guard() RETURNS TRIGGER AS $$
DECLARE plan reg_crypto_aml_withdrawal_plans; request doc_withdrawal_requests;
BEGIN
 IF NEW.recorder_type<>'WithdrawalRequest' THEN RETURN NEW; END IF;
 SELECT * INTO plan FROM reg_crypto_aml_withdrawal_plans WHERE request_id=NEW.recorder_id;
 IF NOT FOUND THEN RETURN NEW; END IF;
 PERFORM 1 FROM sys_crypto_aml_routing_scopes WHERE merchant_id=plan.merchant_id AND network_id=plan.network_id ORDER BY merchant_id,network_id,token_id FOR UPDATE;
 SELECT * INTO STRICT request FROM doc_withdrawal_requests WHERE id=plan.request_id;
 IF NEW.record_type<>'receipt' OR NEW.merchant_id<>plan.merchant_id OR NEW.token_id<>plan.token_id OR NEW.recorder_version<>request.posted_version+1 THEN RAISE EXCEPTION 'AML withdrawal financial identity cannot be reposted' USING ERRCODE='23514'; END IF;
 IF EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_lines l WHERE l.id=NEW.line_id AND l.request_id=plan.request_id AND l.amount=NEW.amount) THEN
   IF EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_cancellations WHERE line_id=NEW.line_id)
      AND NOT EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations WHERE basis_type='CryptoAMLWithdrawalLine' AND basis_id=NEW.line_id) THEN RETURN NEW; END IF;
   IF EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_lines l JOIN doc_crypto_withdrawals w ON w.id=l.withdrawal_id AND w.posted AND w.posted_version=1 AND w.amount=l.amount AND w.source_wallet_id=l.wallet_id
      JOIN reg_crypto_chain_settlements settlement ON settlement.network_id=plan.network_id AND settlement.tx_hash=w.tx_hash AND settlement.basis_type='CryptoAMLWithdrawalLine' AND settlement.basis_id=l.id AND settlement.status='reverted'
      WHERE l.id=NEW.line_id) THEN RETURN NEW; END IF;
 END IF;
 IF NEW.amount=request.amount AND request.status IN ('pending_signature','pending_approval')
    AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_lines l WHERE l.request_id=plan.request_id AND (l.status<>'cancelled' OR EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations WHERE basis_type='CryptoAMLWithdrawalLine' AND basis_id=l.id)))
    AND NOT EXISTS(SELECT 1 FROM reg_crypto_merchant_balance_movements WHERE recorder_id=plan.request_id AND recorder_type='WithdrawalRequest' AND record_type='receipt') THEN RETURN NEW; END IF;
 RAISE EXCEPTION 'AML withdrawal refund requires exact cancellation or final revert; full storno would overcredit' USING ERRCODE='23514';
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_withdrawal_merchant_movement BEFORE INSERT ON reg_crypto_merchant_balance_movements FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_withdrawal_merchant_movement_guard();

CREATE TABLE reg_crypto_aml_withdrawal_events (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(), request_id UUID NOT NULL REFERENCES reg_crypto_aml_withdrawal_plans(request_id), merchant_id UUID NOT NULL REFERENCES cat_merchants(id),
 resource_version INT NOT NULL CHECK(resource_version>0), event_type TEXT NOT NULL CHECK(event_type IN ('withdrawal.confirmed','withdrawal.failed','withdrawal.partially_completed')),
 snapshot JSONB NOT NULL CHECK(jsonb_typeof(snapshot)='object'), created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL), UNIQUE(request_id,resource_version)
);
CREATE TRIGGER trg_crypto_aml_withdrawal_event_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_withdrawal_events FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();
CREATE FUNCTION fn_crypto_aml_withdrawal_terminal_event() RETURNS TRIGGER AS $$
DECLARE plan reg_crypto_aml_withdrawal_plans;
BEGIN
 IF NEW.status IS NOT DISTINCT FROM OLD.status OR NEW.status NOT IN ('confirmed','failed','rejected','partially_completed') THEN RETURN NEW; END IF;
 SELECT * INTO plan FROM reg_crypto_aml_withdrawal_plans WHERE request_id=NEW.id;IF NOT FOUND THEN RETURN NEW;END IF;
 INSERT INTO reg_crypto_aml_withdrawal_events(request_id,merchant_id,resource_version,event_type,snapshot)
 VALUES(NEW.id,NEW.merchant_id,NEW.version,CASE NEW.status WHEN 'confirmed' THEN 'withdrawal.confirmed' WHEN 'partially_completed' THEN 'withdrawal.partially_completed' ELSE 'withdrawal.failed' END,
 jsonb_build_object('id',NEW.id,'withdrawalRequestId',NEW.id,'merchantId',NEW.merchant_id,'tokenId',NEW.token_id,'networkId',plan.network_id,'amount',NEW.amount::text,'status',NEW.status,'resourceVersion',NEW.version,'riskClass',plan.risk_class,'riskAccepted',plan.risk_accepted,
   'completedAmount',COALESCE((SELECT SUM(amount) FROM reg_crypto_aml_withdrawal_lines WHERE request_id=NEW.id AND status='confirmed'),0)::text,
   'refundedAmount',COALESCE((SELECT SUM(amount) FROM reg_crypto_aml_withdrawal_lines WHERE request_id=NEW.id AND status IN ('failed','cancelled')),0)::text,
   'sources',(SELECT jsonb_agg(jsonb_build_object('id',l.id,'walletId',l.wallet_id,'address',l.source_address,'amount',l.amount::text,'status',l.status,'txHash',COALESCE(a.tx_hash,''),'requestedClass',plan.risk_class,'dispatchedClass',CASE WHEN first.authorization_id IS NULL THEN NULL ELSE plan.risk_class END,'dispatchedAt',first.created_at) ORDER BY l.id)
     FROM reg_crypto_aml_withdrawal_lines l LEFT JOIN reg_crypto_signing_authorizations a ON a.basis_type='CryptoAMLWithdrawalLine' AND a.basis_id=l.id AND a.attempt=1 AND a.generation=1
     LEFT JOIN reg_crypto_aml_first_dispatches first ON first.authorization_id=a.id WHERE l.request_id=NEW.id)));
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_withdrawal_terminal_event AFTER UPDATE ON doc_withdrawal_requests FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_withdrawal_terminal_event();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN IF EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_plans) THEN RAISE EXCEPTION 'cannot discard risk withdrawal source history'; END IF; END $$;
CREATE OR REPLACE FUNCTION fn_crypto_aml_inventory_covered(target UUID,asset UUID) RETURNS BOOLEAN AS $$
 WITH RECURSIVE ancestors(wallet_id) AS (
    SELECT target UNION SELECT e.source_wallet_id FROM crypto_aml_risk_edges e JOIN ancestors a ON a.wallet_id=e.destination_wallet_id WHERE e.token_id=asset
 ), inventory AS (
    SELECT p.*,b.created_at baseline_at,COALESCE(physical.amount,0) physical_amount,
      COALESCE((SELECT SUM(CASE WHEN f.destination_wallet_id=p.wallet_id THEN f.principal ELSE -f.principal END) FROM reg_crypto_aml_inventory_facts f WHERE f.token_id=asset AND (f.destination_wallet_id=p.wallet_id OR f.source_wallet_id=p.wallet_id)),0) journal_principal,
      COALESCE((SELECT SUM(CASE WHEN f.destination_wallet_id=p.wallet_id THEN f.service_fee ELSE -f.service_fee END) FROM reg_crypto_aml_inventory_facts f WHERE f.token_id=asset AND (f.destination_wallet_id=p.wallet_id OR f.source_wallet_id=p.wallet_id)),0) journal_fee
    FROM ancestors a LEFT JOIN reg_crypto_aml_inventory_positions p ON p.wallet_id=a.wallet_id AND p.token_id=asset
    LEFT JOIN reg_crypto_aml_wallet_baselines b ON b.wallet_id=p.wallet_id AND b.token_id=asset
    LEFT JOIN reg_crypto_balance_balances physical ON physical.wallet_id=p.wallet_id AND physical.token_id=asset
 ) SELECT NOT EXISTS(SELECT 1 FROM inventory i WHERE i.wallet_id IS NULL OR i.baseline_at IS NULL OR i.principal<>i.journal_principal OR i.service_fee<>i.journal_fee OR i.principal+i.service_fee<>i.physical_amount)
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_inventory_facts f JOIN crypto_aml_inventory_fact_validity v ON v.id=f.id
    WHERE f.token_id=asset AND (f.destination_wallet_id IN (SELECT wallet_id FROM ancestors) OR f.source_wallet_id IN (SELECT wallet_id FROM ancestors)) AND NOT v.valid)
 AND NOT EXISTS(SELECT 1 FROM inventory i JOIN reg_crypto_balance_movements m ON m.wallet_id=i.wallet_id AND m.token_id=asset AND m.created_at>=i.baseline_at
    WHERE CASE WHEN m.record_type='receipt' THEN COALESCE((SELECT SUM(f.gross) FROM reg_crypto_aml_inventory_facts f WHERE f.destination_movement_id=m.line_id),0)
       ELSE COALESCE((SELECT SUM(f.gross) FROM reg_crypto_aml_inventory_facts f WHERE f.source_movement_id=m.line_id),0) END <>m.amount)
 AND NOT EXISTS(SELECT 1 FROM inventory i JOIN reg_crypto_aml_wallet_baselines b ON b.wallet_id=i.wallet_id AND b.token_id=asset
    JOIN reg_crypto_aml_effects e ON e.wallet_id=i.wallet_id AND e.token_id=asset AND e.block_number>b.block_number
    JOIN reg_crypto_aml_effect_chain_states cs ON cs.effect_id=e.id AND cs.canonicality='canonical'
    WHERE NOT EXISTS(SELECT 1 FROM reg_crypto_aml_inventory_facts f WHERE f.effect_id=e.id));
$$ LANGUAGE SQL STABLE;
CREATE OR REPLACE FUNCTION fn_crypto_aml_total_inventory_covered(target UUID,asset UUID) RETURNS BOOLEAN AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM cat_tokens WHERE id=asset AND token_standard='native') THEN RETURN fn_crypto_aml_inventory_covered(target,asset); END IF;
 RETURN (WITH RECURSIVE ancestors(wallet_id) AS (
    SELECT target UNION SELECT e.source_wallet_id FROM crypto_aml_risk_edges e JOIN ancestors a ON a.wallet_id=e.destination_wallet_id WHERE e.token_id=asset
 ), inventory AS (
    SELECT a.wallet_id,p.principal,p.service_fee,r.amount resource_amount,b.created_at baseline_at,b.block_number,COALESCE(physical.amount,0) physical_amount,
      COALESCE((SELECT SUM(CASE WHEN f.destination_wallet_id=a.wallet_id THEN f.principal ELSE -f.principal END) FROM reg_crypto_aml_inventory_facts f WHERE f.token_id=asset AND (f.destination_wallet_id=a.wallet_id OR f.source_wallet_id=a.wallet_id)),0) journal_principal,
      COALESCE((SELECT SUM(CASE WHEN f.destination_wallet_id=a.wallet_id THEN f.service_fee ELSE -f.service_fee END) FROM reg_crypto_aml_inventory_facts f WHERE f.token_id=asset AND (f.destination_wallet_id=a.wallet_id OR f.source_wallet_id=a.wallet_id)),0) journal_fee,
      COALESCE((SELECT SUM(CASE WHEN f.destination_wallet_id=a.wallet_id THEN f.amount ELSE -f.amount END) FROM reg_crypto_aml_resource_facts f WHERE f.token_id=asset AND (f.destination_wallet_id=a.wallet_id OR f.source_wallet_id=a.wallet_id)),0) journal_resource
    FROM ancestors a LEFT JOIN reg_crypto_aml_inventory_positions p ON p.wallet_id=a.wallet_id AND p.token_id=asset
    LEFT JOIN reg_crypto_aml_resource_positions r ON r.wallet_id=a.wallet_id AND r.token_id=asset
    LEFT JOIN reg_crypto_aml_wallet_baselines b ON b.wallet_id=a.wallet_id AND b.token_id=asset
    LEFT JOIN reg_crypto_balance_balances physical ON physical.wallet_id=a.wallet_id AND physical.token_id=asset
 ) SELECT NOT EXISTS(SELECT 1 FROM inventory i WHERE i.principal IS NULL OR i.resource_amount IS NULL OR i.baseline_at IS NULL
     OR i.principal<>i.journal_principal OR i.service_fee<>i.journal_fee OR i.resource_amount<>i.journal_resource OR i.principal+i.service_fee+i.resource_amount<>i.physical_amount)
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_inventory_facts f JOIN crypto_aml_inventory_fact_validity v ON v.id=f.id
     WHERE f.token_id=asset AND (f.destination_wallet_id IN (SELECT wallet_id FROM ancestors) OR f.source_wallet_id IN (SELECT wallet_id FROM ancestors)) AND NOT v.valid)
 AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_resource_facts f JOIN crypto_aml_resource_fact_validity v ON v.id=f.id
     WHERE f.token_id=asset AND (f.destination_wallet_id IN (SELECT wallet_id FROM ancestors) OR f.source_wallet_id IN (SELECT wallet_id FROM ancestors)) AND NOT v.valid)
 AND NOT EXISTS(SELECT 1 FROM inventory i JOIN reg_crypto_balance_movements m ON m.wallet_id=i.wallet_id AND m.token_id=asset AND m.created_at>=i.baseline_at
     WHERE CASE WHEN m.record_type='receipt' THEN COALESCE((SELECT SUM(f.gross) FROM reg_crypto_aml_inventory_facts f WHERE f.destination_movement_id=m.line_id),0)+COALESCE((SELECT SUM(f.amount) FROM reg_crypto_aml_resource_facts f WHERE f.destination_movement_id=m.line_id),0)
       ELSE COALESCE((SELECT SUM(f.gross) FROM reg_crypto_aml_inventory_facts f WHERE f.source_movement_id=m.line_id),0)+COALESCE((SELECT SUM(f.amount) FROM reg_crypto_aml_resource_facts f WHERE f.source_movement_id=m.line_id),0) END<>m.amount)
 AND NOT EXISTS(SELECT 1 FROM inventory i JOIN reg_crypto_aml_effects e ON e.wallet_id=i.wallet_id AND e.token_id=asset AND e.block_number>i.block_number
     JOIN reg_crypto_aml_effect_chain_states cs ON cs.effect_id=e.id AND cs.canonicality='canonical'
     WHERE NOT EXISTS(SELECT 1 FROM reg_crypto_aml_inventory_facts f WHERE f.effect_id=e.id) AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_resource_facts f WHERE f.effect_id=e.id)));
END;
$$ LANGUAGE plpgsql STABLE;
CREATE OR REPLACE FUNCTION fn_crypto_aml_allocation_guard() RETURNS TRIGGER AS $$
DECLARE scoped sys_crypto_aml_routing_scopes; source reg_crypto_aml_inventory_positions; available_principal NUMERIC; available_fee NUMERIC; destination_empty BOOLEAN;
BEGIN
    IF TG_OP='DELETE' THEN RAISE EXCEPTION 'AML allocation evidence cannot be deleted' USING ERRCODE='23514'; END IF;
    SELECT * INTO scoped FROM sys_crypto_aml_routing_scopes WHERE merchant_id=NEW.merchant_id AND network_id=NEW.network_id AND token_id=NEW.token_id FOR UPDATE;
    IF TG_OP='UPDATE' THEN
        IF (NEW.id,NEW.sweep_attempt_id,NEW.merchant_id,NEW.network_id,NEW.token_id,NEW.source_wallet_id,NEW.destination_wallet_id,NEW.principal,NEW.service_fee,NEW.source_revision,NEW.risk_revision,NEW.scope_version,NEW.risk_class,NEW.request_digest,NEW.created_at)
           IS DISTINCT FROM (OLD.id,OLD.sweep_attempt_id,OLD.merchant_id,OLD.network_id,OLD.token_id,OLD.source_wallet_id,OLD.destination_wallet_id,OLD.principal,OLD.service_fee,OLD.source_revision,OLD.risk_revision,OLD.scope_version,OLD.risk_class,OLD.request_digest,OLD.created_at)
           OR (NEW.status<>OLD.status AND (OLD.status<>'reserved' OR NEW.status NOT IN ('applied','cancelled'))) THEN
            RAISE EXCEPTION 'AML allocation identity/status is immutable' USING ERRCODE='23514';
        END IF;
        IF NEW.status='applied' AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_inventory_facts WHERE allocation_id=NEW.id) THEN
            RAISE EXCEPTION 'AML allocation requires an applied chain-backed fact' USING ERRCODE='23514';
        END IF;
        IF NEW.status='cancelled' AND (NOT EXISTS(SELECT 1 FROM reg_crypto_aml_allocation_cancellations WHERE allocation_id=NEW.id)
            OR EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations WHERE basis_type='CryptoSweepAttempt' AND basis_id=NEW.sweep_attempt_id AND status <> 'failed')) THEN
            RAISE EXCEPTION 'cannot release an allocation after authorization began' USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.status<>'reserved' THEN RAISE EXCEPTION 'AML allocation must begin reserved' USING ERRCODE='23514'; END IF;
    PERFORM 1 FROM reg_crypto_aml_inventory_positions WHERE token_id=NEW.token_id AND wallet_id IN (NEW.source_wallet_id,NEW.destination_wallet_id) ORDER BY wallet_id FOR UPDATE;
    SELECT * INTO source FROM reg_crypto_aml_inventory_positions WHERE wallet_id=NEW.source_wallet_id AND token_id=NEW.token_id;
    SELECT source.principal-COALESCE(SUM(principal),0),source.service_fee-COALESCE(SUM(service_fee),0) INTO available_principal,available_fee
        FROM reg_crypto_aml_internal_allocations WHERE source_wallet_id=NEW.source_wallet_id AND token_id=NEW.token_id AND status='reserved';
    IF NOT scoped.enabled OR scoped.version<>NEW.scope_version OR source.wallet_id IS NULL OR source.revision<>NEW.source_revision
       OR NOT fn_crypto_aml_inventory_covered(NEW.source_wallet_id,NEW.token_id) OR NOT fn_crypto_aml_inventory_covered(NEW.destination_wallet_id,NEW.token_id)
       OR available_principal<0 OR available_fee<0 OR available_principal+available_fee<NEW.principal+NEW.service_fee
       OR NEW.principal<>div((NEW.principal+NEW.service_fee)*available_principal,NULLIF(available_principal+available_fee,0)) THEN
        RAISE EXCEPTION 'AML allocation inventory/revision mismatch' USING ERRCODE='23514';
    END IF;
    IF NOT EXISTS(SELECT 1 FROM reg_crypto_sweep_attempts attempt JOIN cat_wallets sw ON sw.id=attempt.wallet_id JOIN cat_wallets dw ON dw.id=attempt.hot_wallet_id
        JOIN cat_tokens token ON token.id=attempt.token_id JOIN cat_blockchain_networks network ON network.id=attempt.network_id
        WHERE attempt.id=NEW.sweep_attempt_id AND attempt.network_id=NEW.network_id AND attempt.token_id=NEW.token_id AND token.token_standard<>'native'
        AND attempt.wallet_id=NEW.source_wallet_id AND attempt.hot_wallet_id=NEW.destination_wallet_id AND attempt.amount=NEW.principal+NEW.service_fee
        AND attempt.status IN ('claimed','resource_ready') AND attempt.expires_at>NOW() AND sw.wallet_role='merchant_pool' AND dw.wallet_role='liquidity_hot'
        AND sw.custody_merchant_id=NEW.merchant_id AND dw.custody_merchant_id=NEW.merchant_id AND sw.is_active AND dw.is_active AND NOT sw.deletion_mark AND NOT dw.deletion_mark
        AND sw.address_key=CASE WHEN network.chain_family='evm' THEN LOWER(attempt.source_address) ELSE attempt.source_address END
        AND dw.address_key=CASE WHEN network.chain_family='evm' THEN LOWER(attempt.destination_address) ELSE attempt.destination_address END)
       OR EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations WHERE basis_type='CryptoSweepAttempt' AND basis_id=NEW.sweep_attempt_id)
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_risk_states WHERE wallet_id=NEW.source_wallet_id AND token_id=NEW.token_id
           AND risk_class=NEW.risk_class AND revision=NEW.risk_revision AND scope_version=NEW.scope_version AND inventory_covered AND expires_at>NOW()) THEN
        RAISE EXCEPTION 'AML allocation requires a fresh unsigned owned token sweep' USING ERRCODE='23514';
    END IF;
    SELECT principal+service_fee=0 INTO destination_empty FROM reg_crypto_aml_inventory_positions WHERE wallet_id=NEW.destination_wallet_id AND token_id=NEW.token_id;
    IF NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_risk_states WHERE wallet_id=NEW.destination_wallet_id AND token_id=NEW.token_id AND scope_version=NEW.scope_version
        AND risk_class<>'high' AND (destination_empty OR (risk_class=NEW.risk_class AND inventory_covered AND expires_at>NOW())))
       OR EXISTS(SELECT 1 FROM reg_crypto_aml_internal_allocations WHERE destination_wallet_id=NEW.destination_wallet_id AND token_id=NEW.token_id AND status='reserved' AND risk_class<>NEW.risk_class) THEN
        RAISE EXCEPTION 'AML allocation destination risk mismatch' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE OR REPLACE FUNCTION fn_crypto_aml_inventory_fact_guard() RETURNS TRIGGER AS $$
BEGIN
    PERFORM 1 FROM sys_crypto_aml_routing_scopes WHERE merchant_id=NEW.merchant_id AND network_id=NEW.network_id AND token_id=NEW.token_id AND enabled FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION 'AML inventory requires an enabled owned scope' USING ERRCODE='23514'; END IF;
    PERFORM 1 FROM reg_crypto_aml_inventory_positions WHERE token_id=NEW.token_id AND wallet_id IN (NEW.source_wallet_id,NEW.destination_wallet_id) ORDER BY wallet_id FOR UPDATE;
    IF NEW.kind='payment' THEN
        IF NOT EXISTS(SELECT 1 FROM crypto_aml_allocatable_payments p WHERE p.payment_id=NEW.payment_id AND p.payment_version=NEW.payment_version
            AND p.merchant_id=NEW.merchant_id AND p.network_id=NEW.network_id AND p.token_id=NEW.token_id AND p.wallet_id=NEW.destination_wallet_id
            AND p.principal=NEW.principal AND p.service_fee=NEW.service_fee AND p.effect_id=NEW.effect_id AND p.movement_id=NEW.destination_movement_id) THEN
            RAISE EXCEPTION 'AML payment allocation does not match financial/chain evidence' USING ERRCODE='23514';
        END IF;
    ELSE
        IF NOT EXISTS(SELECT 1 FROM crypto_aml_allocatable_transfers t JOIN reg_crypto_aml_internal_allocations plan ON plan.id=t.allocation_id AND plan.status='reserved'
            WHERE t.allocation_id=NEW.allocation_id AND t.merchant_id=NEW.merchant_id AND t.network_id=NEW.network_id AND t.token_id=NEW.token_id
              AND t.source_wallet_id=NEW.source_wallet_id AND t.destination_wallet_id=NEW.destination_wallet_id AND t.principal=NEW.principal AND t.service_fee=NEW.service_fee
              AND t.effect_id=NEW.effect_id AND t.source_movement_id=NEW.source_movement_id AND t.destination_movement_id=NEW.destination_movement_id) THEN
            RAISE EXCEPTION 'AML transfer allocation does not match reserved plan and posted chain effect' USING ERRCODE='23514';
        END IF;
    END IF;
    IF NEW.principal+NEW.service_fee+COALESCE((SELECT SUM(gross) FROM reg_crypto_aml_inventory_facts WHERE destination_movement_id=NEW.destination_movement_id),0)
       >(SELECT amount FROM reg_crypto_balance_movements WHERE line_id=NEW.destination_movement_id) THEN
        RAISE EXCEPTION 'AML receipt allocation exceeds physical movement' USING ERRCODE='23514';
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
 ELSE FALSE END;
$$ LANGUAGE SQL STABLE;
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
CREATE OR REPLACE FUNCTION fn_crypto_aml_resource_signing_guard() RETURNS TRIGGER AS $$
DECLARE wallet UUID; owner UUID; native UUID; budget reg_crypto_aml_resource_budgets; fee reg_crypto_fee_reservations;
BEGIN
 SELECT w.id,w.custody_merchant_id,t.id INTO wallet,owner,native FROM cat_wallets w JOIN cat_blockchain_networks n ON n.id=w.network_id
   JOIN cat_tokens t ON t.network_id=w.network_id AND t.token_standard='native'
   WHERE w.network_id=NEW.network_id AND w.address_key=CASE WHEN n.chain_family='evm' THEN LOWER(NEW.source_address) ELSE NEW.source_address END;
 IF owner IS NULL OR NOT EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes WHERE merchant_id=owner AND network_id=NEW.network_id AND token_id=native AND enabled) THEN RETURN NEW; END IF;
 PERFORM 1 FROM sys_crypto_aml_routing_scopes s WHERE s.merchant_id=owner AND s.network_id=NEW.network_id ORDER BY s.merchant_id,s.network_id,s.token_id FOR UPDATE;
 SELECT b.* INTO budget FROM reg_crypto_aml_resource_budgets b JOIN reg_crypto_fee_reservations f ON f.id=b.fee_reservation_id
   WHERE f.basis_type=NEW.basis_type AND f.basis_id=NEW.basis_id AND f.purpose=NEW.purpose AND f.attempt=NEW.generation AND f.payer_wallet_id=wallet AND f.native_token_id=native;
 IF NOT FOUND THEN RAISE EXCEPTION 'AML native scope requires an operating resource budget' USING ERRCODE='23514'; END IF;
 SELECT * INTO fee FROM reg_crypto_fee_reservations WHERE id=budget.fee_reservation_id;
 IF budget.status<>'reserved' OR fee.status<>'reserved' OR fee.tx_hash<>'' OR NEW.merchant_id IS DISTINCT FROM owner OR NEW.max_network_fee_minor>budget.max_fee
    OR NOT EXISTS(SELECT 1 FROM cat_wallets WHERE id=wallet AND vault_key_id=NEW.vault_key_id AND is_active AND NOT deletion_mark)
    OR NOT EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes WHERE merchant_id=owner AND network_id=NEW.network_id AND token_id=native AND enabled AND version=budget.scope_version)
    OR NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_risk_states WHERE wallet_id=wallet AND token_id=native AND revision=budget.risk_revision AND scope_version=budget.scope_version AND risk_class=budget.risk_class AND inventory_covered AND expires_at>NOW())
    OR NOT fn_crypto_aml_total_inventory_covered(wallet,native)
    OR NOT EXISTS(SELECT 1 FROM cat_tokens t WHERE t.id=fee.operation_token_id AND t.network_id=NEW.network_id AND t.token_standard=NEW.asset_standard
       AND CASE WHEN NEW.chain_family='evm' THEN LOWER(COALESCE(t.contract_address,''))=LOWER(NEW.contract_address) ELSE COALESCE(t.contract_address,'')=NEW.contract_address END)
    OR EXISTS(SELECT 1 FROM reg_crypto_fee_reservations f WHERE f.payer_wallet_id=wallet AND f.native_token_id=native AND f.id<>fee.id AND f.status IN ('reserved','broadcast','held')
       AND (f.status='held' OR NOT EXISTS(SELECT 1 FROM reg_crypto_aml_resource_budgets b WHERE b.fee_reservation_id=f.id AND b.status='reserved'))) THEN
   RAISE EXCEPTION 'operating resource authorization is cancelled or stale' USING ERRCODE='23514';
 END IF;
 IF budget.funding_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM doc_crypto_native_fundings f JOIN cat_wallets d ON d.id=f.destination_wallet_id AND d.custody_merchant_id=owner
     JOIN reg_crypto_wallet_risk_states risk ON risk.wallet_id=d.id AND risk.token_id=native AND risk.scope_version=budget.scope_version AND risk.risk_class<>'high'
     JOIN reg_crypto_aml_inventory_positions p ON p.wallet_id=d.id AND p.token_id=native JOIN reg_crypto_aml_resource_positions r ON r.wallet_id=d.id AND r.token_id=native
     WHERE f.id=budget.funding_id AND f.id=NEW.basis_id AND f.amount_minor=NEW.amount_minor_units AND d.address_key=CASE WHEN NEW.chain_family='evm' THEN LOWER(NEW.destination_address) ELSE NEW.destination_address END
       AND f.status IN ('created','reserved') AND fn_crypto_aml_total_inventory_covered(d.id,native)
       AND (p.principal+p.service_fee+r.amount=0 OR (risk.risk_class=budget.risk_class AND risk.inventory_covered AND risk.expires_at>NOW()))) THEN
   RAISE EXCEPTION 'native resource destination changed before authorization' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
ALTER TABLE reg_crypto_aml_resource_budgets DROP CONSTRAINT reg_crypto_aml_resource_budgets_risk_class_check;
ALTER TABLE reg_crypto_aml_resource_budgets ADD CONSTRAINT reg_crypto_aml_resource_budgets_risk_class_check CHECK(risk_class IN ('low','medium'));
DROP TRIGGER trg_crypto_aml_withdrawal_terminal_event ON doc_withdrawal_requests;
DROP FUNCTION fn_crypto_aml_withdrawal_terminal_event();
DROP TABLE reg_crypto_aml_withdrawal_events;
DROP TRIGGER trg_crypto_aml_withdrawal_signing_guard ON reg_crypto_signing_authorizations;
DROP FUNCTION fn_crypto_aml_withdrawal_signing_guard();
DROP FUNCTION fn_crypto_aml_check_withdrawal_dispatch(reg_crypto_signing_authorizations,TEXT);
DROP TRIGGER trg_crypto_aml_withdrawal_merchant_movement ON reg_crypto_merchant_balance_movements;
DROP FUNCTION fn_crypto_aml_withdrawal_merchant_movement_guard();
DROP TRIGGER trg_crypto_aml_withdrawal_parent_guard ON doc_withdrawal_requests;
DROP FUNCTION fn_crypto_aml_withdrawal_parent_guard();
DROP FUNCTION fn_crypto_aml_refresh_withdrawal(UUID);
DROP VIEW crypto_aml_valid_withdrawal_facts;
DROP TABLE reg_crypto_aml_withdrawal_facts;
DROP FUNCTION fn_crypto_aml_withdrawal_fact_apply();
DROP FUNCTION fn_crypto_aml_withdrawal_fact_guard();
DROP TABLE reg_crypto_aml_withdrawal_cancellations;
DROP TABLE reg_crypto_aml_withdrawal_lines;
DROP TABLE reg_crypto_aml_withdrawal_plans;
DROP FUNCTION fn_crypto_aml_withdrawal_line_guard();
DROP FUNCTION fn_crypto_aml_withdrawal_plan_complete();
ALTER TABLE doc_withdrawal_requests DROP CONSTRAINT doc_withdrawal_requests_status_check;
ALTER TABLE doc_withdrawal_requests ADD CONSTRAINT doc_withdrawal_requests_status_check CHECK(status IN ('pending_signature','pending_approval','approved','signing','broadcast','confirmed','held','rejected','failed'));
-- +goose StatementEnd
