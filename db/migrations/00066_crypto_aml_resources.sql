-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- Service operating capital is deliberately separate from merchant principal
-- AND processing fee income. It cannot be created by a risk classification.
CREATE TABLE reg_crypto_aml_resource_positions (
    wallet_id UUID NOT NULL,
    token_id UUID NOT NULL,
    merchant_id UUID NOT NULL,
    network_id UUID NOT NULL,
    amount NUMERIC(78,0) NOT NULL DEFAULT 0 CHECK(amount>=0),
    revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
    PRIMARY KEY(wallet_id,token_id),
    FOREIGN KEY(wallet_id,token_id) REFERENCES reg_crypto_aml_wallet_baselines(wallet_id,token_id),
    FOREIGN KEY(wallet_id,network_id,merchant_id) REFERENCES cat_wallets(id,network_id,custody_merchant_id),
    FOREIGN KEY(token_id,network_id) REFERENCES cat_tokens(id,network_id)
);
CREATE INDEX idx_crypto_aml_resource_scope ON reg_crypto_aml_resource_positions(merchant_id,network_id,token_id,wallet_id);
INSERT INTO reg_crypto_aml_resource_positions(wallet_id,token_id,merchant_id,network_id)
 SELECT b.wallet_id,b.token_id,b.merchant_id,b.network_id FROM reg_crypto_aml_wallet_baselines b JOIN cat_tokens t ON t.id=b.token_id AND t.token_standard='native';
CREATE TRIGGER trg_crypto_aml_resource_updated BEFORE UPDATE ON reg_crypto_aml_resource_positions FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_crypto_aml_resource_txid BEFORE UPDATE ON reg_crypto_aml_resource_positions FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TABLE reg_crypto_aml_operating_receipts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    effect_id UUID NOT NULL UNIQUE REFERENCES reg_crypto_aml_effects(id),
    merchant_id UUID NOT NULL,
    network_id UUID NOT NULL,
    token_id UUID NOT NULL,
    wallet_id UUID NOT NULL,
    tx_hash TEXT NOT NULL,
    event_kind TEXT NOT NULL,
    event_index BIGINT NOT NULL,
    amount NUMERIC(78,0) NOT NULL CHECK(amount>0),
    movement_id UUID NOT NULL UNIQUE REFERENCES reg_crypto_balance_movements(line_id) DEFERRABLE INITIALLY DEFERRED,
    evidence_reference TEXT NOT NULL CHECK(evidence_reference<>''),
    recorded_by TEXT NOT NULL CHECK(recorded_by<>''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
    UNIQUE(network_id,tx_hash,event_kind,event_index,wallet_id),
    FOREIGN KEY(wallet_id,network_id,merchant_id) REFERENCES cat_wallets(id,network_id,custody_merchant_id),
    FOREIGN KEY(merchant_id,network_id,token_id) REFERENCES sys_crypto_aml_routing_scopes(merchant_id,network_id,token_id)
);
CREATE TRIGGER trg_crypto_aml_operating_receipt_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_operating_receipts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TABLE reg_crypto_aml_resource_budgets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    fee_reservation_id UUID NOT NULL UNIQUE REFERENCES reg_crypto_fee_reservations(id),
    merchant_id UUID NOT NULL,
    network_id UUID NOT NULL,
    token_id UUID NOT NULL,
    wallet_id UUID NOT NULL,
    funding_id UUID UNIQUE REFERENCES doc_crypto_native_fundings(id),
    funding_amount NUMERIC(78,0) NOT NULL DEFAULT 0 CHECK(funding_amount>=0),
    max_fee NUMERIC(78,0) NOT NULL CHECK(max_fee>0),
    resource_revision BIGINT NOT NULL CHECK(resource_revision>0),
    risk_revision BIGINT NOT NULL CHECK(risk_revision>0),
    scope_version BIGINT NOT NULL CHECK(scope_version>0),
    risk_class TEXT NOT NULL CHECK(risk_class IN ('low','medium')),
    request_digest TEXT NOT NULL CHECK(request_digest<>''),
    status TEXT NOT NULL DEFAULT 'reserved' CHECK(status IN ('reserved','settled','failed','cancelled')),
    settlement_id UUID REFERENCES reg_crypto_chain_settlements(id),
    actual_fee NUMERIC(78,0) NOT NULL DEFAULT 0 CHECK(actual_fee>=0 AND actual_fee<=max_fee),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
    FOREIGN KEY(wallet_id,network_id,merchant_id) REFERENCES cat_wallets(id,network_id,custody_merchant_id),
    FOREIGN KEY(merchant_id,network_id,token_id) REFERENCES sys_crypto_aml_routing_scopes(merchant_id,network_id,token_id),
    CHECK((funding_id IS NULL AND funding_amount=0) OR (funding_id IS NOT NULL AND funding_amount>0)),
    CHECK((status IN ('reserved','cancelled') AND settlement_id IS NULL AND actual_fee=0) OR (status IN ('settled','failed') AND settlement_id IS NOT NULL))
);
CREATE INDEX idx_crypto_aml_resource_active ON reg_crypto_aml_resource_budgets(wallet_id,token_id) WHERE status='reserved';
CREATE TRIGGER trg_crypto_aml_resource_budget_updated BEFORE UPDATE ON reg_crypto_aml_resource_budgets FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_crypto_aml_resource_budget_txid BEFORE UPDATE ON reg_crypto_aml_resource_budgets FOR EACH ROW EXECUTE FUNCTION update_txid_column();
CREATE TABLE reg_crypto_aml_resource_cancellations (
    budget_id UUID PRIMARY KEY REFERENCES reg_crypto_aml_resource_budgets(id),
    reason TEXT NOT NULL CHECK(length(reason) BETWEEN 3 AND 500),
    cancelled_by TEXT NOT NULL CHECK(cancelled_by<>''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL)
);
CREATE TRIGGER trg_crypto_aml_resource_cancellation_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_resource_cancellations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TABLE reg_crypto_aml_resource_facts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    kind TEXT NOT NULL CHECK(kind IN ('seed','funding','cost')),
    merchant_id UUID NOT NULL,
    network_id UUID NOT NULL,
    token_id UUID NOT NULL,
    source_wallet_id UUID,
    destination_wallet_id UUID,
    amount NUMERIC(78,0) NOT NULL CHECK(amount>0),
    receipt_id UUID UNIQUE REFERENCES reg_crypto_aml_operating_receipts(id),
    budget_id UUID REFERENCES reg_crypto_aml_resource_budgets(id),
    effect_id UUID UNIQUE REFERENCES reg_crypto_aml_effects(id),
    source_movement_id UUID UNIQUE REFERENCES reg_crypto_balance_movements(line_id),
    destination_movement_id UUID UNIQUE REFERENCES reg_crypto_balance_movements(line_id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
    UNIQUE(kind,budget_id),
    FOREIGN KEY(source_wallet_id,network_id,merchant_id) REFERENCES cat_wallets(id,network_id,custody_merchant_id),
    FOREIGN KEY(destination_wallet_id,network_id,merchant_id) REFERENCES cat_wallets(id,network_id,custody_merchant_id),
    FOREIGN KEY(merchant_id,network_id,token_id) REFERENCES sys_crypto_aml_routing_scopes(merchant_id,network_id,token_id),
    CHECK((kind='seed' AND source_wallet_id IS NULL AND source_movement_id IS NULL AND destination_wallet_id IS NOT NULL AND destination_movement_id IS NOT NULL AND effect_id IS NOT NULL AND receipt_id IS NOT NULL AND budget_id IS NULL)
       OR (kind='funding' AND source_wallet_id IS NOT NULL AND destination_wallet_id IS NOT NULL AND source_wallet_id<>destination_wallet_id AND source_movement_id IS NOT NULL AND destination_movement_id IS NOT NULL AND effect_id IS NOT NULL AND receipt_id IS NULL AND budget_id IS NOT NULL)
       OR (kind='cost' AND source_wallet_id IS NOT NULL AND destination_wallet_id IS NULL AND source_movement_id IS NOT NULL AND destination_movement_id IS NULL AND effect_id IS NULL AND receipt_id IS NULL AND budget_id IS NOT NULL))
);
CREATE INDEX idx_crypto_aml_resource_fact_source ON reg_crypto_aml_resource_facts(source_wallet_id,token_id) WHERE source_wallet_id IS NOT NULL;
CREATE INDEX idx_crypto_aml_resource_fact_destination ON reg_crypto_aml_resource_facts(destination_wallet_id,token_id) WHERE destination_wallet_id IS NOT NULL;
CREATE TRIGGER trg_crypto_aml_resource_fact_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_resource_facts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE VIEW crypto_aml_ready_resource_budgets AS
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

CREATE VIEW crypto_aml_resource_fact_validity AS
 SELECT fact.id,CASE WHEN fact.kind='seed' THEN EXISTS(SELECT 1 FROM reg_crypto_aml_operating_receipts receipt JOIN reg_crypto_aml_effects e ON e.id=receipt.effect_id
    JOIN reg_crypto_aml_effect_chain_states cs ON cs.effect_id=e.id AND cs.canonicality='canonical'
    JOIN reg_crypto_balance_movements physical ON physical.line_id=receipt.movement_id AND physical.recorder_id=receipt.id AND physical.recorder_type='AMLOperatingReceipt' AND physical.recorder_version=1
       AND physical.record_type='receipt' AND physical.wallet_id=receipt.wallet_id AND physical.token_id=receipt.token_id AND physical.amount=receipt.amount
    WHERE receipt.id=fact.receipt_id AND receipt.effect_id=fact.effect_id AND receipt.movement_id=fact.destination_movement_id AND receipt.amount=fact.amount AND receipt.wallet_id=fact.destination_wallet_id
       AND e.wallet_id=receipt.wallet_id AND e.network_id=receipt.network_id AND e.token_id=receipt.token_id AND e.amount=receipt.amount)
 ELSE EXISTS(SELECT 1 FROM crypto_aml_ready_resource_budgets ready JOIN reg_crypto_aml_resource_budgets b ON b.id=ready.id AND b.status IN ('settled','failed') AND b.settlement_id=ready.settlement_id AND b.actual_fee=ready.actual_fee
    WHERE b.id=fact.budget_id AND fact.source_wallet_id=b.wallet_id AND fact.network_id=b.network_id AND fact.token_id=b.token_id AND fact.merchant_id=b.merchant_id
      AND ((fact.kind='cost' AND fact.amount=ready.actual_fee AND fact.source_movement_id=ready.cost_movement_id)
        OR (fact.kind='funding' AND ready.chain_status='succeeded' AND fact.amount=ready.funding_amount AND fact.destination_wallet_id=ready.destination_wallet_id AND fact.effect_id=ready.effect_id
          AND fact.source_movement_id=ready.funding_source_movement_id AND fact.destination_movement_id=ready.funding_destination_movement_id))) END valid
 FROM reg_crypto_aml_resource_facts fact;

CREATE FUNCTION fn_crypto_aml_total_inventory_covered(target UUID,asset UUID) RETURNS BOOLEAN AS $$
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

CREATE FUNCTION fn_crypto_aml_lock_resource_scopes(reservation UUID) RETURNS VOID AS $$
BEGIN
 PERFORM 1 FROM reg_crypto_fee_reservations fee JOIN cat_wallets w ON w.id=fee.payer_wallet_id
   JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=w.custody_merchant_id AND s.network_id=fee.network_id
   WHERE fee.id=reservation AND (EXISTS(SELECT 1 FROM reg_crypto_aml_resource_budgets b WHERE b.fee_reservation_id=fee.id)
      OR EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes native WHERE native.merchant_id=w.custody_merchant_id AND native.network_id=fee.network_id AND native.token_id=fee.native_token_id AND native.enabled))
   ORDER BY s.merchant_id,s.network_id,s.token_id FOR UPDATE OF s;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_lock_fee_wallet_scopes(payer UUID,asset UUID) RETURNS VOID AS $$
BEGIN
 PERFORM 1 FROM cat_wallets w JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=w.custody_merchant_id AND s.network_id=w.network_id
 WHERE w.id=payer AND EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes native WHERE native.merchant_id=w.custody_merchant_id AND native.network_id=w.network_id AND native.token_id=asset AND native.enabled)
 ORDER BY s.merchant_id,s.network_id,s.token_id FOR UPDATE OF s;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_resource_fee_insert_guard() RETURNS TRIGGER AS $$
BEGIN
 PERFORM fn_crypto_aml_lock_fee_wallet_scopes(NEW.payer_wallet_id,NEW.native_token_id);
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_resource_fee_insert_guard BEFORE INSERT ON reg_crypto_fee_reservations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_fee_insert_guard();

CREATE FUNCTION fn_crypto_aml_operating_receipt_guard() RETURNS TRIGGER AS $$
BEGIN
 PERFORM 1 FROM sys_crypto_aml_routing_scopes WHERE merchant_id=NEW.merchant_id AND network_id=NEW.network_id AND token_id=NEW.token_id AND enabled FOR UPDATE;
 IF NOT FOUND OR NOT EXISTS(SELECT 1 FROM reg_crypto_aml_effects e JOIN reg_crypto_aml_effect_chain_states cs ON cs.effect_id=e.id AND cs.canonicality='canonical'
    JOIN cat_wallets w ON w.id=e.wallet_id AND w.wallet_role='network_fee' AND w.custody_merchant_id=NEW.merchant_id
    JOIN cat_tokens t ON t.id=e.token_id AND t.network_id=e.network_id AND t.token_standard='native'
    JOIN cat_blockchain_networks n ON n.id=e.network_id JOIN reg_crypto_transfer_verifications proof ON proof.id=e.verification_id
    JOIN reg_crypto_aml_wallet_baselines baseline ON baseline.wallet_id=e.wallet_id AND baseline.token_id=e.token_id AND e.block_number>baseline.block_number
    WHERE e.id=NEW.effect_id AND (e.merchant_id,e.network_id,e.token_id,e.wallet_id,e.tx_hash,e.event_kind,e.event_index,e.amount)
      =(NEW.merchant_id,NEW.network_id,NEW.token_id,NEW.wallet_id,NEW.tx_hash,NEW.event_kind,NEW.event_index,NEW.amount) AND e.source_wallet_id IS NULL
      AND NOT EXISTS(SELECT 1 FROM cat_wallets sender WHERE sender.network_id=e.network_id AND sender.address_key=CASE WHEN n.chain_family='evm' THEN LOWER(proof.evidence->'event'->>'fromAddress') ELSE proof.evidence->'event'->>'fromAddress' END))
    OR EXISTS(SELECT 1 FROM doc_crypto_payments p WHERE p.network_id=NEW.network_id AND p.wallet_id=NEW.wallet_id AND p.tx_hash=NEW.tx_hash AND p.event_kind=NEW.event_kind AND p.event_index=NEW.event_index)
    OR EXISTS(SELECT 1 FROM reg_crypto_aml_inventory_facts WHERE effect_id=NEW.effect_id) THEN
   RAISE EXCEPTION 'operating receipt requires a verified external native funding of its own fee wallet' USING ERRCODE='23514';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM reg_crypto_aml_inventory_positions p JOIN reg_crypto_aml_resource_positions r ON r.wallet_id=p.wallet_id AND r.token_id=p.token_id
     LEFT JOIN reg_crypto_balance_balances physical ON physical.wallet_id=p.wallet_id AND physical.token_id=p.token_id
     WHERE p.wallet_id=NEW.wallet_id AND p.token_id=NEW.token_id AND p.principal+p.service_fee+r.amount=COALESCE(physical.amount,0)) THEN
   RAISE EXCEPTION 'operating receipt cannot relabel unallocated legacy balances' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_operating_receipt_guard BEFORE INSERT ON reg_crypto_aml_operating_receipts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_operating_receipt_guard();

CREATE FUNCTION fn_crypto_aml_resource_budget_guard() RETURNS TRIGGER AS $$
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
CREATE TRIGGER trg_crypto_aml_resource_budget_guard BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_aml_resource_budgets FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_budget_guard();
CREATE FUNCTION fn_crypto_aml_resource_budget_revision() RETURNS TRIGGER AS $$
BEGIN
 IF TG_OP='INSERT' OR (OLD.status='reserved' AND NEW.status='cancelled') THEN
   UPDATE reg_crypto_aml_resource_positions SET revision=revision+1 WHERE wallet_id=NEW.wallet_id AND token_id=NEW.token_id;
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_resource_budget_revision AFTER INSERT OR UPDATE ON reg_crypto_aml_resource_budgets FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_budget_revision();

CREATE FUNCTION fn_crypto_aml_resource_fact_guard() RETURNS TRIGGER AS $$
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
CREATE TRIGGER trg_crypto_aml_resource_fact_guard BEFORE INSERT ON reg_crypto_aml_resource_facts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_fact_guard();
CREATE FUNCTION fn_crypto_aml_resource_fact_apply() RETURNS TRIGGER AS $$
DECLARE target UUID;
BEGIN
 FOR target IN SELECT wallet_id FROM reg_crypto_aml_resource_positions WHERE token_id=NEW.token_id AND wallet_id IN (NEW.source_wallet_id,NEW.destination_wallet_id) ORDER BY wallet_id LOOP
   UPDATE reg_crypto_aml_resource_positions SET amount=amount+CASE WHEN target=NEW.destination_wallet_id THEN NEW.amount ELSE -NEW.amount END,revision=revision+1 WHERE wallet_id=target AND token_id=NEW.token_id;
 END LOOP;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_resource_fact_apply AFTER INSERT ON reg_crypto_aml_resource_facts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_fact_apply();
CREATE FUNCTION fn_crypto_aml_principal_resource_exclusion() RETURNS TRIGGER AS $$
BEGIN
 PERFORM 1 FROM sys_crypto_aml_routing_scopes WHERE merchant_id=NEW.merchant_id AND network_id=NEW.network_id AND token_id=NEW.token_id FOR UPDATE;
 IF EXISTS(SELECT 1 FROM reg_crypto_aml_resource_facts WHERE effect_id=NEW.effect_id) THEN RAISE EXCEPTION 'chain effect already allocated to operating resources' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_00_resource_exclusion BEFORE INSERT ON reg_crypto_aml_inventory_facts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_principal_resource_exclusion();

CREATE FUNCTION fn_crypto_aml_resource_baseline() RETURNS TRIGGER AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM cat_tokens WHERE id=NEW.token_id AND token_standard='native') THEN
   INSERT INTO reg_crypto_aml_resource_positions(wallet_id,token_id,merchant_id,network_id) VALUES(NEW.wallet_id,NEW.token_id,NEW.merchant_id,NEW.network_id);
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_resource_baseline AFTER INSERT ON reg_crypto_aml_wallet_baselines FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_baseline();

CREATE FUNCTION fn_crypto_aml_resource_basis_guard() RETURNS TRIGGER AS $$
BEGIN
 IF TG_TABLE_NAME='reg_crypto_fee_reservations' THEN
   IF NEW.status='released' AND EXISTS(SELECT 1 FROM reg_crypto_aml_resource_budgets WHERE fee_reservation_id=OLD.id AND status<>'cancelled') THEN
     RAISE EXCEPTION 'resource-backed fee release requires audited resource cancellation' USING ERRCODE='23514';
   END IF;
   IF EXISTS(SELECT 1 FROM reg_crypto_aml_resource_budgets WHERE fee_reservation_id=OLD.id) AND
      ((OLD.id,OLD.network_id,OLD.payer_wallet_id,OLD.native_token_id,OLD.operation_token_id,OLD.reserved_principal_minor,OLD.basis_type,OLD.basis_id,OLD.purpose,OLD.attempt,OLD.max_fee_minor)
       IS DISTINCT FROM (NEW.id,NEW.network_id,NEW.payer_wallet_id,NEW.native_token_id,NEW.operation_token_id,NEW.reserved_principal_minor,NEW.basis_type,NEW.basis_id,NEW.purpose,NEW.attempt,NEW.max_fee_minor)
       OR (OLD.tx_hash<>'' AND OLD.tx_hash IS DISTINCT FROM NEW.tx_hash) OR (OLD.unsigned_fingerprint<>'' AND OLD.unsigned_fingerprint IS DISTINCT FROM NEW.unsigned_fingerprint)) THEN
     RAISE EXCEPTION 'resource-backed fee reservation identity is immutable' USING ERRCODE='23514';
   END IF;
 ELSE
   IF EXISTS(SELECT 1 FROM reg_crypto_aml_resource_budgets WHERE funding_id=OLD.id) AND
      ((OLD.id,OLD.basis_type,OLD.basis_id,OLD.attempt,OLD.network_id,OLD.native_token_id,OLD.source_wallet_id,OLD.destination_wallet_id,OLD.amount_minor,OLD.fee_reservation_id)
       IS DISTINCT FROM (NEW.id,NEW.basis_type,NEW.basis_id,NEW.attempt,NEW.network_id,NEW.native_token_id,NEW.source_wallet_id,NEW.destination_wallet_id,NEW.amount_minor,NEW.fee_reservation_id)
       OR (OLD.tx_hash<>'' AND OLD.tx_hash IS DISTINCT FROM NEW.tx_hash)) THEN
     RAISE EXCEPTION 'resource-backed native funding identity is immutable' USING ERRCODE='23514';
   END IF;
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_resource_fee_identity BEFORE UPDATE ON reg_crypto_fee_reservations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_basis_guard();
CREATE TRIGGER trg_crypto_aml_resource_funding_identity BEFORE UPDATE ON doc_crypto_native_fundings FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_basis_guard();

CREATE FUNCTION fn_crypto_aml_resource_signing_guard() RETURNS TRIGGER AS $$
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
CREATE TRIGGER trg_crypto_aml_00_resource_signing_guard BEFORE INSERT ON reg_crypto_signing_authorizations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_signing_guard();

ALTER TABLE reg_crypto_aml_inventory_queue DROP CONSTRAINT reg_crypto_aml_inventory_queue_kind_check;
ALTER TABLE reg_crypto_aml_inventory_queue ADD CONSTRAINT reg_crypto_aml_inventory_queue_kind_check CHECK(kind IN ('payment','transfer','resource'));
CREATE FUNCTION fn_crypto_aml_queue_resource_budget(reservation UUID) RETURNS VOID AS $$
BEGIN
 PERFORM 1 FROM reg_crypto_aml_resource_budgets b JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=b.merchant_id AND s.network_id=b.network_id AND s.token_id=b.token_id WHERE b.fee_reservation_id=reservation FOR UPDATE OF s;
 INSERT INTO reg_crypto_aml_inventory_queue(kind,basis_id,merchant_id,network_id,token_id)
    SELECT 'resource',id,merchant_id,network_id,token_id FROM reg_crypto_aml_resource_budgets WHERE fee_reservation_id=reservation AND status='reserved' ON CONFLICT DO NOTHING;
END;
$$ LANGUAGE plpgsql;
CREATE FUNCTION fn_crypto_aml_resource_queue_trigger() RETURNS TRIGGER AS $$
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
CREATE TRIGGER trg_crypto_aml_resource_fee_queue AFTER INSERT OR UPDATE ON reg_crypto_fee_reservations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_queue_trigger();
CREATE TRIGGER trg_crypto_aml_resource_funding_queue AFTER INSERT OR UPDATE ON doc_crypto_native_fundings FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_queue_trigger();
CREATE TRIGGER trg_crypto_aml_resource_settlement_queue AFTER INSERT ON reg_crypto_chain_settlements FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_queue_trigger();
CREATE TRIGGER trg_crypto_aml_resource_movement_queue AFTER INSERT OR UPDATE ON reg_crypto_balance_movements FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_queue_trigger();
CREATE TRIGGER trg_crypto_aml_resource_effect_queue AFTER INSERT OR UPDATE ON reg_crypto_aml_effect_chain_states FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_queue_trigger();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM reg_crypto_aml_operating_receipts) OR EXISTS(SELECT 1 FROM reg_crypto_aml_resource_budgets) OR EXISTS(SELECT 1 FROM reg_crypto_aml_resource_facts) THEN
   RAISE EXCEPTION 'cannot roll back AML resource evidence; use a forward migration';
 END IF;
END $$;
DROP TRIGGER trg_crypto_aml_resource_effect_queue ON reg_crypto_aml_effect_chain_states;
DROP TRIGGER trg_crypto_aml_resource_movement_queue ON reg_crypto_balance_movements;
DROP TRIGGER trg_crypto_aml_resource_settlement_queue ON reg_crypto_chain_settlements;
DROP TRIGGER trg_crypto_aml_resource_funding_queue ON doc_crypto_native_fundings;
DROP TRIGGER trg_crypto_aml_resource_fee_queue ON reg_crypto_fee_reservations;
DROP FUNCTION fn_crypto_aml_resource_queue_trigger();
DROP FUNCTION fn_crypto_aml_queue_resource_budget(UUID);
DELETE FROM reg_crypto_aml_inventory_queue WHERE kind='resource';
ALTER TABLE reg_crypto_aml_inventory_queue DROP CONSTRAINT reg_crypto_aml_inventory_queue_kind_check;
ALTER TABLE reg_crypto_aml_inventory_queue ADD CONSTRAINT reg_crypto_aml_inventory_queue_kind_check CHECK(kind IN ('payment','transfer'));
DROP TRIGGER trg_crypto_aml_00_resource_signing_guard ON reg_crypto_signing_authorizations;
DROP FUNCTION fn_crypto_aml_resource_signing_guard();
DROP TRIGGER trg_crypto_aml_resource_funding_identity ON doc_crypto_native_fundings;
DROP TRIGGER trg_crypto_aml_resource_fee_identity ON reg_crypto_fee_reservations;
DROP FUNCTION fn_crypto_aml_resource_basis_guard();
DROP TRIGGER trg_crypto_aml_resource_baseline ON reg_crypto_aml_wallet_baselines;
DROP FUNCTION fn_crypto_aml_resource_baseline();
DROP TRIGGER trg_crypto_aml_00_resource_exclusion ON reg_crypto_aml_inventory_facts;
DROP FUNCTION fn_crypto_aml_principal_resource_exclusion();
DROP TRIGGER trg_crypto_aml_resource_fact_apply ON reg_crypto_aml_resource_facts;
DROP FUNCTION fn_crypto_aml_resource_fact_apply();
DROP TRIGGER trg_crypto_aml_resource_fact_guard ON reg_crypto_aml_resource_facts;
DROP FUNCTION fn_crypto_aml_resource_fact_guard();
DROP TRIGGER trg_crypto_aml_resource_budget_revision ON reg_crypto_aml_resource_budgets;
DROP FUNCTION fn_crypto_aml_resource_budget_revision();
DROP TRIGGER trg_crypto_aml_resource_budget_guard ON reg_crypto_aml_resource_budgets;
DROP FUNCTION fn_crypto_aml_resource_budget_guard();
DROP TRIGGER trg_crypto_aml_operating_receipt_guard ON reg_crypto_aml_operating_receipts;
DROP FUNCTION fn_crypto_aml_operating_receipt_guard();
DROP FUNCTION fn_crypto_aml_lock_resource_scopes(UUID);
DROP TRIGGER trg_crypto_aml_resource_fee_insert_guard ON reg_crypto_fee_reservations;
DROP FUNCTION fn_crypto_aml_resource_fee_insert_guard();
DROP FUNCTION fn_crypto_aml_lock_fee_wallet_scopes(UUID,UUID);
DROP FUNCTION fn_crypto_aml_total_inventory_covered(UUID,UUID);
DROP VIEW crypto_aml_resource_fact_validity;
DROP VIEW crypto_aml_ready_resource_budgets;
DROP TABLE reg_crypto_aml_resource_facts,reg_crypto_aml_resource_cancellations,reg_crypto_aml_resource_budgets,reg_crypto_aml_operating_receipts,reg_crypto_aml_resource_positions;
-- +goose StatementEnd
