-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
-- Explicit synthetic opening inventory; never a vendor report or money movement.
CREATE TABLE reg_crypto_aml_testnet_bootstraps (
 id UUID PRIMARY KEY, merchant_id UUID NOT NULL REFERENCES cat_merchants(id), network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id),
 block_number BIGINT NOT NULL CHECK(block_number>=0), block_hash TEXT NOT NULL CHECK(block_hash<>''),
 expires_at TIMESTAMPTZ NOT NULL, actor TEXT NOT NULL CHECK(btrim(actor)<>''), request_digest TEXT NOT NULL CHECK(request_digest ~ '^[0-9a-f]{64}$'),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
 UNIQUE(merchant_id,network_id), CHECK(expires_at>created_at AND expires_at<=created_at+interval '365 days')
);
CREATE TABLE reg_crypto_aml_testnet_openings (
 bootstrap_id UUID NOT NULL REFERENCES reg_crypto_aml_testnet_bootstraps(id), wallet_id UUID NOT NULL, token_id UUID NOT NULL,
 merchant_id UUID NOT NULL, network_id UUID NOT NULL, reconciliation_check_id UUID NOT NULL REFERENCES reg_crypto_reconciliation_checks(id),
 principal NUMERIC(78,0) NOT NULL CHECK(principal>=0), service_fee NUMERIC(78,0) NOT NULL CHECK(service_fee>=0), operating NUMERIC(78,0) NOT NULL CHECK(operating>=0),
 amount NUMERIC(78,0) GENERATED ALWAYS AS (principal+service_fee+operating) STORED,
 ledger_hash TEXT NOT NULL CHECK(ledger_hash ~ '^[0-9a-f]{64}$'),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
 PRIMARY KEY(wallet_id,token_id), FOREIGN KEY(wallet_id,network_id,merchant_id) REFERENCES cat_wallets(id,network_id,custody_merchant_id),
 FOREIGN KEY(merchant_id,network_id,token_id) REFERENCES sys_crypto_aml_routing_scopes(merchant_id,network_id,token_id)
);
CREATE INDEX idx_crypto_aml_testnet_opening_bootstrap ON reg_crypto_aml_testnet_openings(bootstrap_id);
CREATE TRIGGER trg_crypto_aml_testnet_bootstrap_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_testnet_bootstraps FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();
CREATE TRIGGER trg_crypto_aml_testnet_opening_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_testnet_openings FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();
CREATE FUNCTION fn_crypto_aml_ledger_hash(wallet UUID,asset UUID,before_time TIMESTAMPTZ) RETURNS TEXT AS $$
 SELECT encode(sha256(convert_to(COALESCE(string_agg(concat_ws(':',line_id,recorder_type,recorder_id,recorder_version,record_type,amount),',' ORDER BY line_id),''),'UTF8')),'hex')
 FROM reg_crypto_balance_movements WHERE wallet_id=wallet AND token_id=asset AND created_at<before_time;
$$ LANGUAGE SQL STABLE;
CREATE FUNCTION fn_crypto_aml_testnet_bootstrap_guard() RETURNS TRIGGER AS $$
BEGIN
 PERFORM 1 FROM sys_crypto_network_policies WHERE network_id=NEW.network_id AND NOT enabled FOR UPDATE;
 IF NOT FOUND OR NOT EXISTS(SELECT 1 FROM cat_blockchain_networks n WHERE n.id=NEW.network_id AND fn_crypto_aml_is_emulation_network(n.chain_family,n.network_identifier))
 OR NOT EXISTS(SELECT 1 FROM sys_crypto_aml_provisioning_identity WHERE id AND database_name=current_database())
 OR EXISTS(SELECT 1 FROM reg_crypto_merchant_balance_balances b JOIN cat_tokens t ON t.id=b.token_id WHERE t.network_id=NEW.network_id AND b.merchant_id<>NEW.merchant_id AND b.amount<>0)
 OR EXISTS(SELECT 1 FROM doc_withdrawal_requests r JOIN cat_tokens t ON t.id=r.token_id WHERE t.network_id=NEW.network_id AND r.status NOT IN ('confirmed','failed','rejected','partially_completed')) THEN
 RAISE EXCEPTION 'testnet bootstrap requires enrolled, paused, single-merchant inventory without pending withdrawals' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_testnet_bootstrap_guard BEFORE INSERT ON reg_crypto_aml_testnet_bootstraps FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_testnet_bootstrap_guard();
CREATE FUNCTION fn_crypto_aml_testnet_opening_guard() RETURNS TRIGGER AS $$
DECLARE physical NUMERIC; recorded NUMERIC;
BEGIN
 PERFORM 1 FROM reg_crypto_aml_testnet_bootstraps h JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=h.merchant_id AND s.network_id=h.network_id AND s.token_id=NEW.token_id AND s.enabled
 JOIN cat_integration_accounts a ON a.id=s.provider_account_id AND a.provider='testnet_aml'
 JOIN cat_tokens t ON t.id=s.token_id JOIN cat_wallets w ON w.id=NEW.wallet_id AND w.custody_merchant_id=s.merchant_id AND w.network_id=s.network_id
 JOIN sys_crypto_network_policies p ON p.network_id=h.network_id AND NOT p.enabled
 WHERE h.id=NEW.bootstrap_id AND h.merchant_id=NEW.merchant_id AND h.network_id=NEW.network_id AND (t.token_standard='native' OR NEW.operating=0)
 AND (w.wallet_role IN ('merchant_pool','liquidity_hot') OR NEW.principal+NEW.service_fee=0) FOR UPDATE OF s,w,p;
 IF NOT FOUND OR EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_baselines WHERE wallet_id=NEW.wallet_id AND token_id=NEW.token_id)
 OR EXISTS(SELECT 1 FROM reg_crypto_aml_effects WHERE wallet_id=NEW.wallet_id AND token_id=NEW.token_id)
 OR EXISTS(SELECT 1 FROM reg_crypto_balance_reservations WHERE wallet_id=NEW.wallet_id AND token_id=NEW.token_id AND status='active')
 OR EXISTS(SELECT 1 FROM reg_crypto_fee_reservations WHERE payer_wallet_id=NEW.wallet_id AND native_token_id=NEW.token_id AND status IN ('reserved','broadcast','held')) THEN
 RAISE EXCEPTION 'testnet opening requires untouched emulator scope and unreserved owned wallet' USING ERRCODE='23514'; END IF;
 SELECT COALESCE(amount,0) INTO physical FROM reg_crypto_balance_balances WHERE wallet_id=NEW.wallet_id AND token_id=NEW.token_id;
 SELECT COALESCE(SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END),0) INTO recorded FROM reg_crypto_balance_movements WHERE wallet_id=NEW.wallet_id AND token_id=NEW.token_id;
 IF COALESCE(physical,0)<>NEW.principal+NEW.service_fee+NEW.operating OR recorded<>COALESCE(physical,0)
 OR NEW.ledger_hash<>fn_crypto_aml_ledger_hash(NEW.wallet_id,NEW.token_id,NOW())
 OR NOT EXISTS(SELECT 1 FROM reg_crypto_reconciliation_checks c WHERE c.id=NEW.reconciliation_check_id AND c.wallet_id=NEW.wallet_id AND c.token_id=NEW.token_id AND c.network_id=NEW.network_id AND c.delta_amount=0 AND c.ledger_amount=recorded AND c.chain_amount=recorded AND c.observed_at BETWEEN NOW()-interval '10 minutes' AND NOW()) THEN
 RAISE EXCEPTION 'testnet opening requires matching physical, journal and fresh chain reconciliation evidence' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_testnet_opening_guard BEFORE INSERT ON reg_crypto_aml_testnet_openings FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_testnet_opening_guard();
CREATE FUNCTION fn_crypto_aml_testnet_opening_valid(wallet UUID,asset UUID) RETURNS BOOLEAN AS $$
 SELECT NOT EXISTS(SELECT 1 FROM reg_crypto_aml_testnet_openings o JOIN reg_crypto_aml_wallet_baselines b ON b.wallet_id=o.wallet_id AND b.token_id=o.token_id
 JOIN reg_crypto_aml_testnet_bootstraps h ON h.id=o.bootstrap_id JOIN cat_blockchain_networks n ON n.id=h.network_id
 JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=o.merchant_id AND s.network_id=o.network_id AND s.token_id=o.token_id JOIN cat_integration_accounts a ON a.id=s.provider_account_id
 WHERE o.wallet_id=wallet AND o.token_id=asset AND (NOT fn_crypto_aml_is_emulation_network(n.chain_family,n.network_identifier) OR a.provider<>'testnet_aml' OR b.evidence_reference<>h.id::text OR b.verified_by<>'testnet-bootstrap'
 OR o.ledger_hash<>fn_crypto_aml_ledger_hash(wallet,asset,b.created_at)));
$$ LANGUAGE SQL STABLE;
CREATE FUNCTION fn_crypto_aml_testnet_opening_apply() RETURNS TRIGGER AS $$
BEGIN
 UPDATE reg_crypto_aml_inventory_positions p SET principal=o.principal,service_fee=o.service_fee,revision=p.revision+1 FROM reg_crypto_aml_testnet_openings o WHERE o.wallet_id=NEW.wallet_id AND o.token_id=NEW.token_id AND p.wallet_id=o.wallet_id AND p.token_id=o.token_id;
 UPDATE reg_crypto_aml_resource_positions p SET amount=o.operating,revision=p.revision+1 FROM reg_crypto_aml_testnet_openings o WHERE o.wallet_id=NEW.wallet_id AND o.token_id=NEW.token_id AND p.wallet_id=o.wallet_id AND p.token_id=o.token_id;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_z_testnet_opening AFTER INSERT ON reg_crypto_aml_wallet_baselines FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_testnet_opening_apply();
CREATE OR REPLACE FUNCTION fn_crypto_aml_baseline_guard() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.verified_by='testnet-bootstrap' AND EXISTS(SELECT 1 FROM reg_crypto_aml_testnet_openings o JOIN reg_crypto_aml_testnet_bootstraps h ON h.id=o.bootstrap_id WHERE o.wallet_id=NEW.wallet_id AND o.token_id=NEW.token_id AND o.merchant_id=NEW.merchant_id AND o.network_id=NEW.network_id AND o.reconciliation_check_id=NEW.reconciliation_check_id AND h.id::text=NEW.evidence_reference AND h.block_number=NEW.block_number AND h.block_hash=NEW.block_hash) THEN RETURN NEW; END IF;
    IF NOT EXISTS(SELECT 1 FROM reg_crypto_reconciliation_checks c WHERE c.id=NEW.reconciliation_check_id
        AND c.wallet_id=NEW.wallet_id AND c.token_id=NEW.token_id AND c.network_id=NEW.network_id
        AND c.ledger_amount=0 AND c.chain_amount=0 AND c.delta_amount=0 AND c.observed_at>=NOW()-interval '1 minute' AND c.observed_at<=NOW()) OR
       EXISTS(SELECT 1 FROM reg_crypto_balance_balances WHERE wallet_id=NEW.wallet_id AND token_id=NEW.token_id AND amount<>0) OR
       EXISTS(SELECT 1 FROM reg_crypto_balance_reservations WHERE wallet_id=NEW.wallet_id AND token_id=NEW.token_id AND status='active') OR
       EXISTS(SELECT 1 FROM reg_crypto_fee_reservations WHERE payer_wallet_id=NEW.wallet_id AND native_token_id=NEW.token_id AND status IN ('reserved','broadcast','held')) THEN
        RAISE EXCEPTION 'AML baseline requires an evidenced empty unreserved wallet' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE OR REPLACE FUNCTION fn_crypto_aml_inventory_covered(target UUID,asset UUID) RETURNS BOOLEAN AS $$
 WITH RECURSIVE ancestors(wallet_id) AS (
    SELECT target UNION SELECT e.source_wallet_id FROM crypto_aml_risk_edges e JOIN ancestors a ON a.wallet_id=e.destination_wallet_id WHERE e.token_id=asset
 ), inventory AS (
    SELECT p.*,b.created_at baseline_at,COALESCE(physical.amount,0) physical_amount,
      COALESCE((SELECT SUM(CASE WHEN f.destination_wallet_id=p.wallet_id THEN f.principal ELSE -f.principal END) FROM reg_crypto_aml_inventory_facts f WHERE f.token_id=asset AND (f.destination_wallet_id=p.wallet_id OR f.source_wallet_id=p.wallet_id)),0)-COALESCE((SELECT SUM(amount) FROM reg_crypto_aml_withdrawal_facts wf WHERE wf.wallet_id=p.wallet_id AND wf.token_id=asset AND wf.succeeded),0)+COALESCE((SELECT o.principal FROM reg_crypto_aml_testnet_openings o WHERE o.wallet_id=p.wallet_id AND o.token_id=asset),0) journal_principal,
      COALESCE((SELECT SUM(CASE WHEN f.destination_wallet_id=p.wallet_id THEN f.service_fee ELSE -f.service_fee END) FROM reg_crypto_aml_inventory_facts f WHERE f.token_id=asset AND (f.destination_wallet_id=p.wallet_id OR f.source_wallet_id=p.wallet_id)),0)+COALESCE((SELECT o.service_fee FROM reg_crypto_aml_testnet_openings o WHERE o.wallet_id=p.wallet_id AND o.token_id=asset),0) journal_fee
    FROM ancestors a LEFT JOIN reg_crypto_aml_inventory_positions p ON p.wallet_id=a.wallet_id AND p.token_id=asset
    LEFT JOIN reg_crypto_aml_wallet_baselines b ON b.wallet_id=p.wallet_id AND b.token_id=asset
    LEFT JOIN reg_crypto_balance_balances physical ON physical.wallet_id=p.wallet_id AND physical.token_id=asset
 ) SELECT NOT EXISTS(SELECT 1 FROM inventory i WHERE i.wallet_id IS NULL OR i.baseline_at IS NULL OR NOT fn_crypto_aml_testnet_opening_valid(i.wallet_id,asset) OR i.principal<>i.journal_principal OR i.service_fee<>i.journal_fee OR i.principal+i.service_fee<>i.physical_amount)
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
      COALESCE((SELECT SUM(CASE WHEN f.destination_wallet_id=a.wallet_id THEN f.principal ELSE -f.principal END) FROM reg_crypto_aml_inventory_facts f WHERE f.token_id=asset AND (f.destination_wallet_id=a.wallet_id OR f.source_wallet_id=a.wallet_id)),0)-COALESCE((SELECT SUM(amount) FROM reg_crypto_aml_withdrawal_facts wf WHERE wf.wallet_id=a.wallet_id AND wf.token_id=asset AND wf.succeeded),0)+COALESCE((SELECT o.principal FROM reg_crypto_aml_testnet_openings o WHERE o.wallet_id=a.wallet_id AND o.token_id=asset),0) journal_principal,
      COALESCE((SELECT SUM(CASE WHEN f.destination_wallet_id=a.wallet_id THEN f.service_fee ELSE -f.service_fee END) FROM reg_crypto_aml_inventory_facts f WHERE f.token_id=asset AND (f.destination_wallet_id=a.wallet_id OR f.source_wallet_id=a.wallet_id)),0)+COALESCE((SELECT o.service_fee FROM reg_crypto_aml_testnet_openings o WHERE o.wallet_id=a.wallet_id AND o.token_id=asset),0) journal_fee,
      COALESCE((SELECT SUM(CASE WHEN f.destination_wallet_id=a.wallet_id THEN f.amount ELSE -f.amount END) FROM reg_crypto_aml_resource_facts f WHERE f.token_id=asset AND (f.destination_wallet_id=a.wallet_id OR f.source_wallet_id=a.wallet_id)),0)+COALESCE((SELECT o.operating FROM reg_crypto_aml_testnet_openings o WHERE o.wallet_id=a.wallet_id AND o.token_id=asset),0) journal_resource
    FROM ancestors a LEFT JOIN reg_crypto_aml_inventory_positions p ON p.wallet_id=a.wallet_id AND p.token_id=asset
    LEFT JOIN reg_crypto_aml_resource_positions r ON r.wallet_id=a.wallet_id AND r.token_id=asset
    LEFT JOIN reg_crypto_aml_wallet_baselines b ON b.wallet_id=a.wallet_id AND b.token_id=asset
    LEFT JOIN reg_crypto_balance_balances physical ON physical.wallet_id=a.wallet_id AND physical.token_id=asset
 ) SELECT NOT EXISTS(SELECT 1 FROM inventory i WHERE i.principal IS NULL OR i.resource_amount IS NULL OR i.baseline_at IS NULL OR NOT fn_crypto_aml_testnet_opening_valid(i.wallet_id,asset)
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
CREATE FUNCTION fn_crypto_aml_testnet_bootstrap_complete() RETURNS TRIGGER AS $$
BEGIN
 IF NOT (SELECT NOT EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes s LEFT JOIN reg_crypto_merchant_balance_balances b ON b.merchant_id=s.merchant_id AND b.token_id=s.token_id WHERE s.merchant_id=NEW.merchant_id AND s.network_id=NEW.network_id AND s.enabled AND (COALESCE(b.amount,0)<>COALESCE((SELECT SUM(o.principal) FROM reg_crypto_aml_testnet_openings o WHERE o.bootstrap_id=NEW.id AND o.token_id=s.token_id),0) OR COALESCE((SELECT SUM(CASE WHEN f.record_type='receipt' THEN f.amount ELSE -f.amount END) FROM reg_crypto_fee_movements f WHERE f.token_id=s.token_id),0)<>COALESCE((SELECT SUM(o.service_fee) FROM reg_crypto_aml_testnet_openings o WHERE o.bootstrap_id=NEW.id AND o.token_id=s.token_id),0))) AND NOT EXISTS(SELECT 1 FROM cat_wallets w JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=NEW.merchant_id AND s.network_id=w.network_id AND s.enabled LEFT JOIN reg_crypto_balance_balances b ON b.wallet_id=w.id AND b.token_id=s.token_id WHERE w.network_id=NEW.network_id AND (w.is_active AND NOT w.deletion_mark OR COALESCE(b.amount,0)<>0) AND (w.custody_merchant_id IS DISTINCT FROM NEW.merchant_id OR NOT EXISTS(SELECT 1 FROM reg_crypto_aml_testnet_openings o WHERE o.bootstrap_id=NEW.id AND o.wallet_id=w.id AND o.token_id=s.token_id)))) OR EXISTS(SELECT 1 FROM reg_crypto_aml_testnet_openings o WHERE o.bootstrap_id=NEW.id AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_baselines b WHERE b.wallet_id=o.wallet_id AND b.token_id=o.token_id AND b.evidence_reference=NEW.id::text)) THEN
 RAISE EXCEPTION 'testnet bootstrap must preserve exact merchant/fee claims and cover every wallet and baseline atomically' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE CONSTRAINT TRIGGER trg_crypto_aml_testnet_bootstrap_complete AFTER INSERT ON reg_crypto_aml_testnet_bootstraps DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_testnet_bootstrap_complete();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN IF EXISTS(SELECT 1 FROM reg_crypto_aml_testnet_bootstraps) THEN RAISE EXCEPTION 'cannot discard testnet opening inventory history; use forward migration'; END IF; END $$;
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
CREATE OR REPLACE FUNCTION fn_crypto_aml_baseline_guard() RETURNS TRIGGER AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM reg_crypto_reconciliation_checks c WHERE c.id=NEW.reconciliation_check_id
        AND c.wallet_id=NEW.wallet_id AND c.token_id=NEW.token_id AND c.network_id=NEW.network_id
        AND c.ledger_amount=0 AND c.chain_amount=0 AND c.delta_amount=0 AND c.observed_at>=NOW()-interval '1 minute' AND c.observed_at<=NOW()) OR
       EXISTS(SELECT 1 FROM reg_crypto_balance_balances WHERE wallet_id=NEW.wallet_id AND token_id=NEW.token_id AND amount<>0) OR
       EXISTS(SELECT 1 FROM reg_crypto_balance_reservations WHERE wallet_id=NEW.wallet_id AND token_id=NEW.token_id AND status='active') OR
       EXISTS(SELECT 1 FROM reg_crypto_fee_reservations WHERE payer_wallet_id=NEW.wallet_id AND native_token_id=NEW.token_id AND status IN ('reserved','broadcast','held')) THEN
        RAISE EXCEPTION 'AML baseline requires an evidenced empty unreserved wallet' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER trg_crypto_aml_z_testnet_opening ON reg_crypto_aml_wallet_baselines;
DROP FUNCTION fn_crypto_aml_testnet_opening_apply();
DROP FUNCTION fn_crypto_aml_testnet_opening_valid(UUID,UUID);
DROP TABLE reg_crypto_aml_testnet_openings;
DROP FUNCTION fn_crypto_aml_testnet_opening_guard();
DROP TABLE reg_crypto_aml_testnet_bootstraps;
DROP FUNCTION fn_crypto_aml_testnet_bootstrap_guard();
DROP FUNCTION fn_crypto_aml_testnet_bootstrap_complete();
DROP FUNCTION fn_crypto_aml_ledger_hash(UUID,UUID,TIMESTAMPTZ);
-- +goose StatementEnd
