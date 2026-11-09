-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- A component projection partitions existing physical money. It never creates
-- another merchant credit or replaces either financial register.
CREATE TABLE reg_crypto_aml_inventory_positions (
    wallet_id UUID NOT NULL,
    token_id UUID NOT NULL,
    merchant_id UUID NOT NULL,
    network_id UUID NOT NULL,
    principal NUMERIC(78,0) NOT NULL DEFAULT 0 CHECK(principal>=0),
    service_fee NUMERIC(78,0) NOT NULL DEFAULT 0 CHECK(service_fee>=0),
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
CREATE INDEX idx_crypto_aml_inventory_scope ON reg_crypto_aml_inventory_positions(merchant_id,network_id,token_id,wallet_id);
INSERT INTO reg_crypto_aml_inventory_positions(wallet_id,token_id,merchant_id,network_id)
    SELECT wallet_id,token_id,merchant_id,network_id FROM reg_crypto_aml_wallet_baselines;
CREATE TRIGGER trg_crypto_aml_inventory_updated BEFORE UPDATE ON reg_crypto_aml_inventory_positions FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_crypto_aml_inventory_txid BEFORE UPDATE ON reg_crypto_aml_inventory_positions FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TABLE reg_crypto_aml_internal_allocations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    sweep_attempt_id UUID NOT NULL UNIQUE REFERENCES reg_crypto_sweep_attempts(id),
    merchant_id UUID NOT NULL,
    network_id UUID NOT NULL,
    token_id UUID NOT NULL,
    source_wallet_id UUID NOT NULL,
    destination_wallet_id UUID NOT NULL,
    principal NUMERIC(78,0) NOT NULL CHECK(principal>=0),
    service_fee NUMERIC(78,0) NOT NULL CHECK(service_fee>=0),
    gross NUMERIC(78,0) GENERATED ALWAYS AS (principal+service_fee) STORED,
    source_revision BIGINT NOT NULL CHECK(source_revision>0),
    risk_revision BIGINT NOT NULL CHECK(risk_revision>0),
    scope_version BIGINT NOT NULL CHECK(scope_version>0),
    risk_class TEXT NOT NULL CHECK(risk_class IN ('low','medium')),
    request_digest TEXT NOT NULL CHECK(request_digest<>''),
    status TEXT NOT NULL DEFAULT 'reserved' CHECK(status IN ('reserved','applied','cancelled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
    FOREIGN KEY(merchant_id,network_id,token_id) REFERENCES sys_crypto_aml_routing_scopes(merchant_id,network_id,token_id),
    FOREIGN KEY(source_wallet_id,network_id,merchant_id) REFERENCES cat_wallets(id,network_id,custody_merchant_id),
    FOREIGN KEY(destination_wallet_id,network_id,merchant_id) REFERENCES cat_wallets(id,network_id,custody_merchant_id),
    CHECK(source_wallet_id<>destination_wallet_id AND principal+service_fee>0)
);
CREATE INDEX idx_crypto_aml_allocation_source ON reg_crypto_aml_internal_allocations(source_wallet_id,token_id) WHERE status='reserved';
CREATE INDEX idx_crypto_aml_allocation_destination ON reg_crypto_aml_internal_allocations(destination_wallet_id,token_id) WHERE status='reserved';
CREATE TRIGGER trg_crypto_aml_allocation_updated BEFORE UPDATE ON reg_crypto_aml_internal_allocations FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_crypto_aml_allocation_txid BEFORE UPDATE ON reg_crypto_aml_internal_allocations FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TABLE reg_crypto_aml_allocation_cancellations (
    allocation_id UUID PRIMARY KEY REFERENCES reg_crypto_aml_internal_allocations(id),
    reason TEXT NOT NULL CHECK(length(reason) BETWEEN 3 AND 500),
    cancelled_by TEXT NOT NULL CHECK(cancelled_by<>''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL)
);
CREATE TRIGGER trg_crypto_aml_allocation_cancellation_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_allocation_cancellations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TABLE reg_crypto_aml_inventory_facts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    kind TEXT NOT NULL CHECK(kind IN ('payment','transfer')),
    merchant_id UUID NOT NULL,
    network_id UUID NOT NULL,
    token_id UUID NOT NULL,
    source_wallet_id UUID,
    destination_wallet_id UUID NOT NULL,
    principal NUMERIC(78,0) NOT NULL CHECK(principal>=0),
    service_fee NUMERIC(78,0) NOT NULL CHECK(service_fee>=0),
    gross NUMERIC(78,0) GENERATED ALWAYS AS (principal+service_fee) STORED,
    payment_id UUID UNIQUE REFERENCES doc_crypto_payments(id),
    payment_version INT,
    allocation_id UUID UNIQUE REFERENCES reg_crypto_aml_internal_allocations(id),
    effect_id UUID NOT NULL UNIQUE REFERENCES reg_crypto_aml_effects(id),
    source_movement_id UUID UNIQUE REFERENCES reg_crypto_balance_movements(line_id),
    destination_movement_id UUID NOT NULL REFERENCES reg_crypto_balance_movements(line_id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
    FOREIGN KEY(merchant_id,network_id,token_id) REFERENCES sys_crypto_aml_routing_scopes(merchant_id,network_id,token_id),
    FOREIGN KEY(source_wallet_id,network_id,merchant_id) REFERENCES cat_wallets(id,network_id,custody_merchant_id),
    FOREIGN KEY(destination_wallet_id,network_id,merchant_id) REFERENCES cat_wallets(id,network_id,custody_merchant_id),
    CHECK(principal+service_fee>0),
    CHECK((kind='payment' AND payment_id IS NOT NULL AND payment_version>0 AND allocation_id IS NULL AND source_wallet_id IS NULL AND source_movement_id IS NULL)
       OR (kind='transfer' AND payment_id IS NULL AND payment_version IS NULL AND allocation_id IS NOT NULL AND source_wallet_id IS NOT NULL AND source_wallet_id<>destination_wallet_id AND source_movement_id IS NOT NULL))
);
CREATE INDEX idx_crypto_aml_inventory_fact_destination ON reg_crypto_aml_inventory_facts(destination_wallet_id,token_id);
CREATE INDEX idx_crypto_aml_inventory_fact_source ON reg_crypto_aml_inventory_facts(source_wallet_id,token_id) WHERE source_wallet_id IS NOT NULL;
CREATE INDEX idx_crypto_aml_inventory_fact_receipt ON reg_crypto_aml_inventory_facts(destination_movement_id);
CREATE TRIGGER trg_crypto_aml_inventory_fact_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_inventory_facts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TABLE reg_crypto_aml_inventory_queue (
    kind TEXT NOT NULL CHECK(kind IN ('payment','transfer')),
    basis_id UUID NOT NULL,
    merchant_id UUID NOT NULL,
    network_id UUID NOT NULL,
    token_id UUID NOT NULL,
    enqueued_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    attempts INT NOT NULL DEFAULT 0 CHECK(attempts>=0),
    last_error TEXT NOT NULL DEFAULT '',
    PRIMARY KEY(kind,basis_id),
    FOREIGN KEY(merchant_id,network_id,token_id) REFERENCES sys_crypto_aml_routing_scopes(merchant_id,network_id,token_id)
);
CREATE INDEX idx_crypto_aml_inventory_queue_age ON reg_crypto_aml_inventory_queue(next_attempt_at,enqueued_at,kind,basis_id);

CREATE VIEW crypto_aml_allocatable_payments AS
 SELECT p.id payment_id,p.posted_version payment_version,p.merchant_id,p.network_id,p.token_id,p.wallet_id,
        credit.amount principal,fees.amount service_fee,e.id effect_id,physical.line_id movement_id
 FROM doc_crypto_payments p JOIN reg_crypto_aml_wallet_baselines baseline ON baseline.wallet_id=p.wallet_id AND baseline.token_id=p.token_id AND p.block_number>baseline.block_number
 JOIN cat_wallets w ON w.id=p.wallet_id AND w.custody_merchant_id=p.merchant_id AND w.network_id=p.network_id
 JOIN cat_tokens token ON token.id=p.token_id AND token.network_id=p.network_id
 JOIN reg_crypto_aml_effects e ON e.wallet_id=p.wallet_id AND e.token_id=p.token_id AND e.network_id=p.network_id
   AND e.tx_hash=p.tx_hash AND e.event_kind=p.event_kind AND e.event_index=p.event_index AND e.block_hash=p.block_hash AND e.amount=p.amount AND e.source_wallet_id IS NULL
 JOIN reg_crypto_aml_effect_chain_states cs ON cs.effect_id=e.id AND cs.canonicality='canonical'
 JOIN reg_crypto_transfer_verifications proof ON proof.id=p.verification_id AND proof.network_id=e.network_id AND proof.tx_hash=e.tx_hash
   AND proof.event_kind=e.event_kind AND proof.event_index=e.event_index AND proof.block_hash=e.block_hash AND proof.economics_hash=e.economics_hash
 JOIN LATERAL(SELECT COALESCE(SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END),0) amount,
    COALESCE(SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END) FILTER(WHERE recorder_version=p.posted_version),0) current_amount
    FROM reg_crypto_merchant_balance_movements WHERE recorder_id=p.id AND recorder_type='CryptoPayment' AND merchant_id=p.merchant_id AND token_id=p.token_id) credit ON TRUE
 JOIN LATERAL(SELECT COALESCE(SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END),0) amount,
    COALESCE(SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END) FILTER(WHERE recorder_version=p.posted_version),0) current_amount
    FROM reg_crypto_fee_movements WHERE recorder_id=p.id AND recorder_type='CryptoPayment' AND merchant_id=p.merchant_id AND token_id=p.token_id AND fee_type='processing') fees ON TRUE
 JOIN reg_crypto_balance_movements physical ON physical.recorder_id=p.id AND physical.recorder_type='CryptoPayment' AND physical.recorder_version=p.posted_version
    AND physical.wallet_id=p.wallet_id AND physical.token_id=p.token_id AND physical.record_type='receipt' AND physical.amount=p.amount AND physical.created_at>=baseline.created_at
 WHERE p.posted AND p.status='settled' AND p.canonicality='canonical'
   AND credit.amount>=0 AND fees.amount>=0 AND credit.amount=credit.current_amount AND fees.amount=fees.current_amount AND credit.amount+fees.amount=p.amount
   AND (SELECT COUNT(*) FROM reg_crypto_balance_movements m WHERE m.recorder_id=p.id AND m.recorder_type='CryptoPayment' AND m.recorder_version=p.posted_version AND m.wallet_id=p.wallet_id AND m.token_id=p.token_id)=1;

-- Each transfer retains both exact physical bases. One aggregate sweep receipt
-- may be covered by several independent source lines, never above its amount.
CREATE VIEW crypto_aml_allocatable_transfers AS
 SELECT plan.id allocation_id,plan.merchant_id,plan.network_id,plan.token_id,plan.source_wallet_id,plan.destination_wallet_id,
        plan.principal,plan.service_fee,e.id effect_id,spent.line_id source_movement_id,received.line_id destination_movement_id
 FROM reg_crypto_aml_internal_allocations plan
 JOIN doc_crypto_sweep_lines line ON line.line_id=plan.sweep_attempt_id AND line.wallet_id=plan.source_wallet_id AND line.amount=plan.gross AND line.tx_hash<>''
 JOIN doc_crypto_sweeps sweep ON sweep.id=line.document_id AND sweep.posted AND sweep.hot_wallet_id=plan.destination_wallet_id AND sweep.token_id=plan.token_id
 JOIN reg_crypto_aml_effects e ON e.wallet_id=plan.destination_wallet_id AND e.source_wallet_id=plan.source_wallet_id AND e.token_id=plan.token_id AND e.network_id=plan.network_id AND e.tx_hash=line.tx_hash AND e.amount=plan.gross
 JOIN reg_crypto_aml_effect_chain_states cs ON cs.effect_id=e.id AND cs.canonicality='canonical'
 JOIN reg_crypto_expected_chain_effects expected ON expected.id=e.expected_effect_id
 JOIN reg_crypto_signing_authorizations auth ON auth.id=expected.signing_authorization_id AND auth.basis_type='CryptoSweepAttempt' AND auth.basis_id=plan.sweep_attempt_id AND auth.signed_envelope IS NOT NULL
 JOIN reg_crypto_balance_movements spent ON spent.recorder_id=sweep.id AND spent.recorder_type='CryptoSweep' AND spent.recorder_version=sweep.posted_version
    AND spent.wallet_id=plan.source_wallet_id AND spent.token_id=plan.token_id AND spent.record_type='expense' AND spent.amount=plan.gross
 JOIN reg_crypto_balance_movements received ON received.recorder_id=sweep.id AND received.recorder_type='CryptoSweep' AND received.recorder_version=sweep.posted_version
    AND received.wallet_id=plan.destination_wallet_id AND received.token_id=plan.token_id AND received.record_type='receipt' AND received.amount=sweep.total_amount
 WHERE plan.status IN ('reserved','applied')
   AND sweep.total_amount=(SELECT SUM(amount) FROM doc_crypto_sweep_lines WHERE document_id=sweep.id AND tx_hash<>'')
   AND spent.created_at>=(SELECT created_at FROM reg_crypto_aml_wallet_baselines WHERE wallet_id=plan.source_wallet_id AND token_id=plan.token_id)
   AND received.created_at>=(SELECT created_at FROM reg_crypto_aml_wallet_baselines WHERE wallet_id=plan.destination_wallet_id AND token_id=plan.token_id)
   AND (SELECT COUNT(*) FROM reg_crypto_balance_movements m WHERE m.recorder_id=sweep.id AND m.recorder_type='CryptoSweep' AND m.recorder_version=sweep.posted_version AND m.wallet_id=plan.source_wallet_id AND m.token_id=plan.token_id AND m.record_type='expense')=1
   AND (SELECT COUNT(*) FROM reg_crypto_balance_movements m WHERE m.recorder_id=sweep.id AND m.recorder_type='CryptoSweep' AND m.recorder_version=sweep.posted_version AND m.wallet_id=plan.destination_wallet_id AND m.token_id=plan.token_id AND m.record_type='receipt')=1;

CREATE VIEW crypto_aml_inventory_fact_validity AS
 SELECT fact.id,CASE WHEN fact.kind='payment' THEN EXISTS(SELECT 1 FROM crypto_aml_allocatable_payments p
   WHERE p.payment_id=fact.payment_id AND p.payment_version=fact.payment_version AND p.effect_id=fact.effect_id AND p.movement_id=fact.destination_movement_id
     AND p.principal=fact.principal AND p.service_fee=fact.service_fee AND p.wallet_id=fact.destination_wallet_id)
 ELSE EXISTS(SELECT 1 FROM crypto_aml_allocatable_transfers t JOIN reg_crypto_aml_internal_allocations plan ON plan.id=t.allocation_id AND plan.status='applied'
   WHERE t.allocation_id=fact.allocation_id AND t.effect_id=fact.effect_id AND t.source_movement_id=fact.source_movement_id AND t.destination_movement_id=fact.destination_movement_id
     AND t.principal=fact.principal AND t.service_fee=fact.service_fee AND t.source_wallet_id=fact.source_wallet_id AND t.destination_wallet_id=fact.destination_wallet_id) END valid
 FROM reg_crypto_aml_inventory_facts fact;

CREATE FUNCTION fn_crypto_aml_inventory_covered(target UUID,asset UUID) RETURNS BOOLEAN AS $$
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

CREATE FUNCTION fn_crypto_aml_allocation_guard() RETURNS TRIGGER AS $$
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
CREATE TRIGGER trg_crypto_aml_allocation_guard BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_aml_internal_allocations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_allocation_guard();

CREATE FUNCTION fn_crypto_aml_inventory_fact_guard() RETURNS TRIGGER AS $$
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
CREATE TRIGGER trg_crypto_aml_inventory_fact_guard BEFORE INSERT ON reg_crypto_aml_inventory_facts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_inventory_fact_guard();

CREATE FUNCTION fn_crypto_aml_inventory_apply() RETURNS TRIGGER AS $$
DECLARE target UUID; direction INT;
BEGIN
    FOR target IN SELECT wallet_id FROM reg_crypto_aml_inventory_positions WHERE token_id=NEW.token_id AND wallet_id IN (NEW.source_wallet_id,NEW.destination_wallet_id) ORDER BY wallet_id LOOP
        direction:=CASE WHEN target=NEW.destination_wallet_id THEN 1 ELSE -1 END;
        UPDATE reg_crypto_aml_inventory_positions SET principal=principal+direction*NEW.principal,service_fee=service_fee+direction*NEW.service_fee,revision=revision+1
            WHERE wallet_id=target AND token_id=NEW.token_id;
    END LOOP;
    IF NEW.allocation_id IS NOT NULL THEN UPDATE reg_crypto_aml_internal_allocations SET status='applied' WHERE id=NEW.allocation_id; END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_inventory_apply AFTER INSERT ON reg_crypto_aml_inventory_facts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_inventory_apply();
CREATE FUNCTION fn_crypto_aml_allocation_revision() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP='INSERT' OR (OLD.status='reserved' AND NEW.status='cancelled') THEN
        UPDATE reg_crypto_aml_inventory_positions SET revision=revision+1 WHERE token_id=NEW.token_id AND wallet_id IN (NEW.source_wallet_id,NEW.destination_wallet_id);
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_allocation_revision AFTER INSERT OR UPDATE ON reg_crypto_aml_internal_allocations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_allocation_revision();

CREATE FUNCTION fn_crypto_aml_allocated_attempt_guard() RETURNS TRIGGER AS $$
BEGIN
    IF (OLD.id,OLD.wallet_id,OLD.hot_wallet_id,OLD.network_id,OLD.token_id,OLD.amount,OLD.source_address,OLD.destination_address)
       IS DISTINCT FROM (NEW.id,NEW.wallet_id,NEW.hot_wallet_id,NEW.network_id,NEW.token_id,NEW.amount,NEW.source_address,NEW.destination_address)
       AND EXISTS(SELECT 1 FROM reg_crypto_aml_internal_allocations WHERE sweep_attempt_id=OLD.id) THEN
        RAISE EXCEPTION 'allocated sweep identity is immutable' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_allocated_attempt_guard BEFORE UPDATE ON reg_crypto_sweep_attempts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_allocated_attempt_guard();

-- Serializes authorization admission with pre-signing cancellation. Existing
-- signed operations remain recoverable; this adds no permission to dispatch.
CREATE FUNCTION fn_crypto_aml_allocation_signing_guard() RETURNS TRIGGER AS $$
DECLARE plan reg_crypto_aml_internal_allocations;
BEGIN
    IF NEW.basis_type<>'CryptoSweepAttempt' THEN RETURN NEW; END IF;
    PERFORM 1 FROM sys_crypto_aml_routing_scopes scope JOIN reg_crypto_aml_internal_allocations p ON p.merchant_id=scope.merchant_id AND p.network_id=scope.network_id AND p.token_id=scope.token_id
        WHERE p.sweep_attempt_id=NEW.basis_id FOR UPDATE OF scope;
    SELECT * INTO plan FROM reg_crypto_aml_internal_allocations WHERE sweep_attempt_id=NEW.basis_id FOR UPDATE;
    IF NOT FOUND THEN RETURN NEW; END IF;
    IF plan.status<>'reserved' OR EXISTS(SELECT 1 FROM reg_crypto_aml_allocation_cancellations WHERE allocation_id=plan.id)
       OR NEW.network_id<>plan.network_id OR NEW.merchant_id IS DISTINCT FROM plan.merchant_id
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_sweep_attempts WHERE id=plan.sweep_attempt_id AND status IN ('claimed','resource_ready','signing') AND expires_at>NOW())
       OR NEW.amount_minor_units<>plan.gross OR NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_risk_states WHERE wallet_id=plan.source_wallet_id AND token_id=plan.token_id AND revision=plan.risk_revision AND risk_class=plan.risk_class AND scope_version=plan.scope_version AND inventory_covered AND expires_at>NOW())
       OR NOT EXISTS(SELECT 1 FROM cat_wallets sw JOIN cat_wallets dw ON dw.id=plan.destination_wallet_id JOIN cat_tokens t ON t.id=plan.token_id
          WHERE sw.id=plan.source_wallet_id AND NEW.vault_key_id=sw.vault_key_id AND NEW.asset_standard=t.token_standard
          AND sw.address_key=CASE WHEN NEW.chain_family='evm' THEN LOWER(NEW.source_address) ELSE NEW.source_address END
          AND dw.address_key=CASE WHEN NEW.chain_family='evm' THEN LOWER(NEW.destination_address) ELSE NEW.destination_address END
          AND CASE WHEN NEW.chain_family='evm' THEN LOWER(NEW.contract_address)=LOWER(COALESCE(t.contract_address,'')) ELSE NEW.contract_address=COALESCE(t.contract_address,'') END)
       OR NOT fn_crypto_aml_inventory_covered(plan.source_wallet_id,plan.token_id) OR NOT fn_crypto_aml_inventory_covered(plan.destination_wallet_id,plan.token_id)
       OR NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_risk_states risk JOIN reg_crypto_aml_inventory_positions inventory ON inventory.wallet_id=risk.wallet_id AND inventory.token_id=risk.token_id
          WHERE risk.wallet_id=plan.destination_wallet_id AND risk.token_id=plan.token_id AND risk.scope_version=plan.scope_version AND risk.risk_class<>'high'
            AND (inventory.principal+inventory.service_fee=0 OR (risk.risk_class=plan.risk_class AND risk.inventory_covered AND risk.expires_at>NOW()))) THEN
        RAISE EXCEPTION 'AML allocation authorization is cancelled or stale' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_allocation_signing_guard BEFORE INSERT ON reg_crypto_signing_authorizations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_allocation_signing_guard();

CREATE FUNCTION fn_crypto_aml_queue_inventory(basis_kind TEXT,basis UUID) RETURNS VOID AS $$
BEGIN
    IF basis_kind='CryptoPayment' THEN
        -- Serialize the wakeup with capture+ack. An ON CONFLICT no-op alone
        -- could lose the last fee/merchant update while a worker clears an
        -- older queue row after observing incomplete evidence.
        PERFORM 1 FROM doc_crypto_payments p JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=p.merchant_id AND s.network_id=p.network_id AND s.token_id=p.token_id AND s.enabled
            WHERE p.id=basis FOR UPDATE OF s;
        INSERT INTO reg_crypto_aml_inventory_queue(kind,basis_id,merchant_id,network_id,token_id)
            SELECT 'payment',p.id,p.merchant_id,p.network_id,p.token_id FROM doc_crypto_payments p JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=p.merchant_id AND s.network_id=p.network_id AND s.token_id=p.token_id AND s.enabled
            WHERE p.id=basis AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_inventory_facts WHERE payment_id=p.id) ON CONFLICT DO NOTHING;
    ELSIF basis_kind='CryptoSweep' THEN
        PERFORM 1 FROM doc_crypto_sweep_lines line JOIN reg_crypto_aml_internal_allocations plan ON plan.sweep_attempt_id=line.line_id AND plan.status='reserved'
            JOIN sys_crypto_aml_routing_scopes scope ON scope.merchant_id=plan.merchant_id AND scope.network_id=plan.network_id AND scope.token_id=plan.token_id
            WHERE line.document_id=basis ORDER BY scope.merchant_id,scope.network_id,scope.token_id FOR UPDATE OF scope;
        INSERT INTO reg_crypto_aml_inventory_queue(kind,basis_id,merchant_id,network_id,token_id)
            SELECT 'transfer',plan.id,plan.merchant_id,plan.network_id,plan.token_id FROM doc_crypto_sweep_lines line JOIN reg_crypto_aml_internal_allocations plan ON plan.sweep_attempt_id=line.line_id AND plan.status='reserved'
            WHERE line.document_id=basis ON CONFLICT DO NOTHING;
    END IF;
END;
$$ LANGUAGE plpgsql;
CREATE FUNCTION fn_crypto_aml_inventory_movement_queue() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP<>'INSERT' THEN PERFORM fn_crypto_aml_queue_inventory(OLD.recorder_type,OLD.recorder_id); END IF;
    IF TG_OP<>'DELETE' THEN PERFORM fn_crypto_aml_queue_inventory(NEW.recorder_type,NEW.recorder_id); RETURN NEW; END IF;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_inventory_physical_queue AFTER INSERT OR UPDATE OR DELETE ON reg_crypto_balance_movements FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_inventory_movement_queue();
CREATE TRIGGER trg_crypto_aml_inventory_merchant_queue AFTER INSERT OR UPDATE OR DELETE ON reg_crypto_merchant_balance_movements FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_inventory_movement_queue();
CREATE TRIGGER trg_crypto_aml_inventory_fee_queue AFTER INSERT OR UPDATE OR DELETE ON reg_crypto_fee_movements FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_inventory_movement_queue();
CREATE FUNCTION fn_crypto_aml_inventory_document_queue() RETURNS TRIGGER AS $$
BEGIN
    IF TG_TABLE_NAME='doc_crypto_payments' THEN PERFORM fn_crypto_aml_queue_inventory('CryptoPayment',NEW.id);
    ELSE PERFORM fn_crypto_aml_queue_inventory('CryptoSweep',NEW.id); END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_inventory_payment_queue AFTER INSERT OR UPDATE ON doc_crypto_payments FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_inventory_document_queue();
CREATE TRIGGER trg_crypto_aml_inventory_sweep_queue AFTER INSERT OR UPDATE ON doc_crypto_sweeps FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_inventory_document_queue();
CREATE FUNCTION fn_crypto_aml_inventory_baseline() RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO reg_crypto_aml_inventory_positions(wallet_id,token_id,merchant_id,network_id) VALUES(NEW.wallet_id,NEW.token_id,NEW.merchant_id,NEW.network_id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_inventory_baseline AFTER INSERT ON reg_crypto_aml_wallet_baselines FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_inventory_baseline();
CREATE FUNCTION fn_crypto_aml_inventory_scope_queue() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.enabled THEN
        INSERT INTO reg_crypto_aml_inventory_queue(kind,basis_id,merchant_id,network_id,token_id)
        SELECT 'payment',p.id,p.merchant_id,p.network_id,p.token_id FROM doc_crypto_payments p WHERE p.merchant_id=NEW.merchant_id AND p.network_id=NEW.network_id AND p.token_id=NEW.token_id
          AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_inventory_facts WHERE payment_id=p.id) ON CONFLICT DO NOTHING;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_inventory_scope_queue AFTER INSERT OR UPDATE ON sys_crypto_aml_routing_scopes FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_inventory_scope_queue();
CREATE FUNCTION fn_crypto_aml_inventory_effect_queue() RETURNS TRIGGER AS $$
BEGIN
    PERFORM 1 FROM reg_crypto_aml_effects e JOIN sys_crypto_aml_routing_scopes scope ON scope.merchant_id=e.merchant_id AND scope.network_id=e.network_id AND scope.token_id=e.token_id
        WHERE e.id=NEW.effect_id FOR UPDATE OF scope;
    INSERT INTO reg_crypto_aml_inventory_queue(kind,basis_id,merchant_id,network_id,token_id)
      SELECT 'payment',p.id,p.merchant_id,p.network_id,p.token_id FROM reg_crypto_aml_effects e JOIN doc_crypto_payments p ON p.wallet_id=e.wallet_id AND p.token_id=e.token_id AND p.tx_hash=e.tx_hash AND p.event_kind=e.event_kind AND p.event_index=e.event_index AND p.block_hash=e.block_hash
      WHERE e.id=NEW.effect_id AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_inventory_facts WHERE payment_id=p.id) ON CONFLICT DO NOTHING;
    INSERT INTO reg_crypto_aml_inventory_queue(kind,basis_id,merchant_id,network_id,token_id)
      SELECT 'transfer',plan.id,plan.merchant_id,plan.network_id,plan.token_id FROM reg_crypto_aml_effects e JOIN reg_crypto_expected_chain_effects x ON x.id=e.expected_effect_id
        JOIN reg_crypto_signing_authorizations a ON a.id=x.signing_authorization_id AND a.basis_type='CryptoSweepAttempt'
        JOIN reg_crypto_aml_internal_allocations plan ON plan.sweep_attempt_id=a.basis_id AND plan.status='reserved' WHERE e.id=NEW.effect_id ON CONFLICT DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_inventory_effect_queue AFTER INSERT OR UPDATE ON reg_crypto_aml_effect_chain_states FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_inventory_effect_queue();

INSERT INTO reg_crypto_aml_inventory_queue(kind,basis_id,merchant_id,network_id,token_id)
 SELECT 'payment',p.id,p.merchant_id,p.network_id,p.token_id FROM doc_crypto_payments p JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=p.merchant_id AND s.network_id=p.network_id AND s.token_id=p.token_id AND s.enabled;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM reg_crypto_aml_inventory_facts) OR EXISTS(SELECT 1 FROM reg_crypto_aml_internal_allocations) THEN
   RAISE EXCEPTION 'cannot roll back AML inventory evidence; use a forward migration';
 END IF;
END $$;
DROP TRIGGER trg_crypto_aml_inventory_effect_queue ON reg_crypto_aml_effect_chain_states;
DROP FUNCTION fn_crypto_aml_inventory_effect_queue();
DROP TRIGGER trg_crypto_aml_inventory_scope_queue ON sys_crypto_aml_routing_scopes;
DROP FUNCTION fn_crypto_aml_inventory_scope_queue();
DROP TRIGGER trg_crypto_aml_inventory_baseline ON reg_crypto_aml_wallet_baselines;
DROP FUNCTION fn_crypto_aml_inventory_baseline();
DROP TRIGGER trg_crypto_aml_inventory_sweep_queue ON doc_crypto_sweeps;
DROP TRIGGER trg_crypto_aml_inventory_payment_queue ON doc_crypto_payments;
DROP FUNCTION fn_crypto_aml_inventory_document_queue();
DROP TRIGGER trg_crypto_aml_inventory_fee_queue ON reg_crypto_fee_movements;
DROP TRIGGER trg_crypto_aml_inventory_merchant_queue ON reg_crypto_merchant_balance_movements;
DROP TRIGGER trg_crypto_aml_inventory_physical_queue ON reg_crypto_balance_movements;
DROP FUNCTION fn_crypto_aml_inventory_movement_queue();
DROP FUNCTION fn_crypto_aml_queue_inventory(TEXT,UUID);
DROP TRIGGER trg_crypto_aml_allocation_signing_guard ON reg_crypto_signing_authorizations;
DROP FUNCTION fn_crypto_aml_allocation_signing_guard();
DROP TRIGGER trg_crypto_aml_allocated_attempt_guard ON reg_crypto_sweep_attempts;
DROP FUNCTION fn_crypto_aml_allocated_attempt_guard();
DROP TRIGGER trg_crypto_aml_allocation_revision ON reg_crypto_aml_internal_allocations;
DROP FUNCTION fn_crypto_aml_allocation_revision();
DROP TRIGGER trg_crypto_aml_inventory_apply ON reg_crypto_aml_inventory_facts;
DROP FUNCTION fn_crypto_aml_inventory_apply();
DROP TRIGGER trg_crypto_aml_inventory_fact_guard ON reg_crypto_aml_inventory_facts;
DROP FUNCTION fn_crypto_aml_inventory_fact_guard();
DROP TRIGGER trg_crypto_aml_allocation_guard ON reg_crypto_aml_internal_allocations;
DROP FUNCTION fn_crypto_aml_allocation_guard();
DROP FUNCTION fn_crypto_aml_inventory_covered(UUID,UUID);
DROP VIEW crypto_aml_inventory_fact_validity;
DROP VIEW crypto_aml_allocatable_transfers;
DROP VIEW crypto_aml_allocatable_payments;
DROP TABLE reg_crypto_aml_inventory_queue,reg_crypto_aml_inventory_facts,reg_crypto_aml_allocation_cancellations,reg_crypto_aml_internal_allocations,reg_crypto_aml_inventory_positions;
-- +goose StatementEnd
