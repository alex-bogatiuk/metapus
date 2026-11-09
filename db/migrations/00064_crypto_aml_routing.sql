-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

CREATE TABLE sys_crypto_aml_risk_policies (
    version TEXT PRIMARY KEY CHECK(version<>''),
    low_max_bp INT NOT NULL CHECK(low_max_bp BETWEEN 0 AND 9999),
    medium_max_bp INT NOT NULL CHECK(medium_max_bp BETWEEN 1 AND 10000 AND medium_max_bp>low_max_bp),
    max_report_age_seconds INT NOT NULL CHECK(max_report_age_seconds>0),
    decision_ttl_seconds INT NOT NULL CHECK(decision_ttl_seconds>0),
    accepted_signals TEXT[] NOT NULL CHECK(cardinality(accepted_signals)>0),
    critical_signals TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
    CHECK(critical_signals <@ accepted_signals)
);
CREATE TABLE sys_crypto_aml_routing_scopes (
    merchant_id UUID NOT NULL REFERENCES cat_merchants(id),
    network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    token_id UUID NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    policy_version TEXT NOT NULL REFERENCES sys_crypto_aml_risk_policies(version),
    provider_account_id UUID NOT NULL REFERENCES cat_integration_accounts(id),
    mapping_version TEXT NOT NULL,
    job_deadline_seconds INT NOT NULL DEFAULT 86400 CHECK(job_deadline_seconds>0),
    version BIGINT NOT NULL DEFAULT 1 CHECK(version>0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
    PRIMARY KEY(merchant_id,network_id,token_id),
    FOREIGN KEY(network_id,token_id,mapping_version) REFERENCES sys_crypto_aml_provider_mappings(network_id,token_id,version),
    FOREIGN KEY(token_id,network_id) REFERENCES cat_tokens(id,network_id)
);
CREATE INDEX idx_crypto_aml_enabled_scope ON sys_crypto_aml_routing_scopes(network_id,token_id,merchant_id) WHERE enabled;
CREATE TABLE reg_crypto_aml_decision_queue (
    report_id UUID NOT NULL REFERENCES reg_crypto_aml_reports(id),
    policy_version TEXT NOT NULL REFERENCES sys_crypto_aml_risk_policies(version),
    PRIMARY KEY(report_id,policy_version)
);
CREATE FUNCTION fn_crypto_aml_routing_scope_guard() RETURNS TRIGGER AS $$
DECLARE executing BOOLEAN;
BEGIN
    IF TG_OP='DELETE' THEN RAISE EXCEPTION 'AML routing scope cannot be deleted' USING ERRCODE='23514'; END IF;
    IF TG_OP='UPDATE' THEN
        IF (NEW.merchant_id,NEW.network_id,NEW.token_id) IS DISTINCT FROM (OLD.merchant_id,OLD.network_id,OLD.token_id)
           OR NEW.version<>OLD.version+1 OR (OLD.enabled AND NOT NEW.enabled) THEN
            RAISE EXCEPTION 'AML routing scope identity/version cannot be downgraded' USING ERRCODE='23514';
        END IF;
    END IF;
    IF NOT EXISTS(SELECT 1 FROM cat_integration_accounts WHERE id=NEW.provider_account_id AND provider='getblock_net') THEN
        RAISE EXCEPTION 'AML routing requires a Getblock.net account' USING ERRCODE='23514';
    END IF;
    IF NEW.enabled THEN
        SELECT enabled INTO executing FROM sys_crypto_network_policies WHERE network_id=NEW.network_id FOR UPDATE;
        IF executing THEN RAISE EXCEPTION 'pause network before changing enabled AML routing' USING ERRCODE='23514'; END IF;
        IF NOT EXISTS(SELECT 1 FROM sys_crypto_custody_policies WHERE network_id=NEW.network_id AND enforced) OR
           NOT EXISTS(SELECT 1 FROM sys_crypto_aml_provider_mappings mapping JOIN cat_tokens token ON token.id=mapping.token_id AND token.network_id=mapping.network_id
                      JOIN cat_blockchain_networks network ON network.id=mapping.network_id
                      WHERE mapping.network_id=NEW.network_id AND mapping.token_id=NEW.token_id AND mapping.version=NEW.mapping_version AND mapping.capability_accepted
                      AND mapping.decimal_places=token.decimal_places AND mapping.native=(token.token_standard='native')
                      AND CASE WHEN network.chain_family='evm' THEN LOWER(mapping.contract_address)=LOWER(COALESCE(token.contract_address,'')) ELSE mapping.contract_address=COALESCE(token.contract_address,'') END) THEN
            RAISE EXCEPTION 'AML routing requires merchant custody and accepted mapping' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_routing_scope_guard BEFORE INSERT OR UPDATE OR DELETE ON sys_crypto_aml_routing_scopes
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_routing_scope_guard();
CREATE TRIGGER trg_crypto_aml_routing_scope_updated BEFORE UPDATE ON sys_crypto_aml_routing_scopes
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_crypto_aml_routing_scope_txid BEFORE UPDATE ON sys_crypto_aml_routing_scopes
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();
CREATE TRIGGER trg_crypto_aml_risk_policy_immutable BEFORE UPDATE OR DELETE ON sys_crypto_aml_risk_policies
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

-- No legacy balance is implicitly declared clean. A trusted preflight must
-- explicitly bind an empty checkpoint before a wallet can become covered.
CREATE TABLE reg_crypto_aml_wallet_baselines (
    wallet_id UUID NOT NULL,
    token_id UUID NOT NULL,
    merchant_id UUID NOT NULL,
    network_id UUID NOT NULL,
    reconciliation_check_id UUID NOT NULL REFERENCES reg_crypto_reconciliation_checks(id),
    block_number BIGINT NOT NULL CHECK(block_number>=0),
    block_hash TEXT NOT NULL CHECK(block_hash<>''),
    evidence_reference TEXT NOT NULL CHECK(evidence_reference<>''),
    verified_by TEXT NOT NULL CHECK(verified_by<>''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
    PRIMARY KEY(wallet_id,token_id),
    FOREIGN KEY(wallet_id,network_id,merchant_id) REFERENCES cat_wallets(id,network_id,custody_merchant_id),
    FOREIGN KEY(merchant_id,network_id,token_id) REFERENCES sys_crypto_aml_routing_scopes(merchant_id,network_id,token_id)
);
CREATE FUNCTION fn_crypto_aml_baseline_guard() RETURNS TRIGGER AS $$
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
CREATE TRIGGER trg_crypto_aml_baseline_guard BEFORE INSERT ON reg_crypto_aml_wallet_baselines
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_baseline_guard();
CREATE TRIGGER trg_crypto_aml_baseline_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_wallet_baselines
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TABLE reg_crypto_aml_effects (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    wallet_id UUID NOT NULL,
    merchant_id UUID NOT NULL,
    network_id UUID NOT NULL,
    token_id UUID NOT NULL,
    verification_id UUID NOT NULL REFERENCES reg_crypto_transfer_verifications(id),
    tx_hash TEXT NOT NULL,
    event_kind TEXT NOT NULL CHECK(event_kind IN ('native_value','contract_log','internal_value')),
    event_index BIGINT NOT NULL CHECK(event_index>=0),
    block_number BIGINT NOT NULL CHECK(block_number>=0),
    block_hash TEXT NOT NULL,
    economics_hash TEXT NOT NULL,
    amount NUMERIC(78,0) NOT NULL CHECK(amount>0),
    source_wallet_id UUID,
    expected_effect_id UUID REFERENCES reg_crypto_expected_chain_effects(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
    FOREIGN KEY(wallet_id,network_id,merchant_id) REFERENCES cat_wallets(id,network_id,custody_merchant_id),
    FOREIGN KEY(source_wallet_id,network_id,merchant_id) REFERENCES cat_wallets(id,network_id,custody_merchant_id),
    FOREIGN KEY(merchant_id,network_id,token_id) REFERENCES sys_crypto_aml_routing_scopes(merchant_id,network_id,token_id),
    CHECK((source_wallet_id IS NULL)=(expected_effect_id IS NULL)),
    UNIQUE(network_id,tx_hash,event_kind,event_index,block_hash,economics_hash,wallet_id)
);
CREATE INDEX idx_crypto_aml_effect_wallet ON reg_crypto_aml_effects(wallet_id,token_id,block_number);
CREATE INDEX idx_crypto_aml_effect_source ON reg_crypto_aml_effects(source_wallet_id,token_id,wallet_id) WHERE source_wallet_id IS NOT NULL;
CREATE TABLE reg_crypto_aml_effect_checks (
    effect_id UUID PRIMARY KEY REFERENCES reg_crypto_aml_effects(id),
    subject_id UUID NOT NULL REFERENCES reg_crypto_aml_subjects(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL)
);
CREATE TABLE reg_crypto_aml_effect_chain_states (
    effect_id UUID PRIMARY KEY REFERENCES reg_crypto_aml_effects(id),
    canonicality TEXT NOT NULL CHECK(canonicality IN ('canonical','orphaned')),
    coverage_complete BOOLEAN NOT NULL DEFAULT FALSE,
    verification_id UUID NOT NULL REFERENCES reg_crypto_transfer_verifications(id),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL)
);
CREATE TRIGGER trg_crypto_aml_effect_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_effects
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();
CREATE TRIGGER trg_crypto_aml_effect_checks_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_effect_checks
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TABLE reg_crypto_aml_rescreen_requests (
    job_id UUID PRIMARY KEY REFERENCES reg_crypto_aml_jobs(id),
    effect_id UUID NOT NULL REFERENCES reg_crypto_aml_effects(id),
    previous_generation BIGINT NOT NULL CHECK(previous_generation>0),
    generation BIGINT NOT NULL CHECK(generation=previous_generation+1),
    reason TEXT NOT NULL CHECK(length(reason) BETWEEN 3 AND 500),
    requested_by TEXT NOT NULL CHECK(requested_by<>''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL)
);
CREATE TRIGGER trg_crypto_aml_rescreen_request_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_rescreen_requests
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

ALTER TABLE reg_crypto_wallet_risk_states ADD COLUMN scope_version BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN evidence_hash TEXT NOT NULL DEFAULT '', ADD COLUMN reason TEXT NOT NULL DEFAULT 'unclassified',
    ADD COLUMN inventory_covered BOOLEAN NOT NULL DEFAULT FALSE;
CREATE TABLE reg_crypto_aml_projection_queue (
    wallet_id UUID NOT NULL,
    token_id UUID NOT NULL,
    merchant_id UUID NOT NULL,
    network_id UUID NOT NULL,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY(wallet_id,token_id),
    FOREIGN KEY(wallet_id,network_id,merchant_id) REFERENCES cat_wallets(id,network_id,custody_merchant_id),
    FOREIGN KEY(merchant_id,network_id,token_id) REFERENCES sys_crypto_aml_routing_scopes(merchant_id,network_id,token_id)
);
CREATE INDEX idx_crypto_aml_projection_due ON reg_crypto_aml_projection_queue(next_attempt_at,wallet_id,token_id);
CREATE TABLE reg_crypto_aml_risk_changes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    wallet_id UUID NOT NULL,
    token_id UUID NOT NULL,
    merchant_id UUID NOT NULL,
    network_id UUID NOT NULL,
    previous_class TEXT NOT NULL,
    risk_class TEXT NOT NULL,
    revision BIGINT NOT NULL CHECK(revision>0),
    scope_version BIGINT NOT NULL,
    reason TEXT NOT NULL,
    expires_at TIMESTAMPTZ,
    inventory_covered BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
    UNIQUE(wallet_id,token_id,revision),
    FOREIGN KEY(wallet_id,network_id,merchant_id) REFERENCES cat_wallets(id,network_id,custody_merchant_id)
);
CREATE TRIGGER trg_crypto_aml_risk_changes_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_risk_changes
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

-- Both observed internal effects and potentially executable signed sweeps
-- carry source risk into downstream fungible pools of the SAME merchant.
CREATE VIEW crypto_aml_risk_edges AS
    SELECT e.merchant_id,e.network_id,e.token_id,e.source_wallet_id,e.wallet_id AS destination_wallet_id
    FROM reg_crypto_aml_effects e WHERE e.source_wallet_id IS NOT NULL
    UNION
    SELECT sw.custody_merchant_id,a.network_id,a.token_id,a.wallet_id,a.hot_wallet_id
    FROM reg_crypto_sweep_attempts a JOIN cat_wallets sw ON sw.id=a.wallet_id AND sw.network_id=a.network_id
    JOIN cat_tokens token ON token.id=a.token_id AND token.network_id=a.network_id
    JOIN cat_wallets dw ON dw.id=a.hot_wallet_id AND dw.network_id=sw.network_id AND dw.custody_merchant_id=sw.custody_merchant_id
    WHERE sw.custody_merchant_id IS NOT NULL AND EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations sa
        WHERE sa.basis_type='CryptoSweepAttempt' AND sa.basis_id=a.id AND sa.signed_envelope IS NOT NULL
          AND sa.network_id=a.network_id AND sa.vault_key_id=sw.vault_key_id
          AND (sa.merchant_id=sw.custody_merchant_id OR sa.merchant_id IS NULL)
          AND sw.address_key=CASE WHEN sa.chain_family='evm' THEN LOWER(sa.source_address) ELSE sa.source_address END
          AND dw.address_key=CASE WHEN sa.chain_family='evm' THEN LOWER(sa.destination_address) ELSE sa.destination_address END
          AND CASE WHEN sa.chain_family='evm' THEN LOWER(sa.contract_address)=LOWER(COALESCE(token.contract_address,'')) ELSE sa.contract_address=COALESCE(token.contract_address,'') END);

CREATE FUNCTION fn_crypto_aml_invalidate_wallet(root_wallet UUID, asset UUID, why TEXT) RETURNS VOID AS $$
DECLARE scope_row sys_crypto_aml_routing_scopes%ROWTYPE; affected UUID; prior TEXT; rev BIGINT;
BEGIN
    SELECT s.* INTO scope_row FROM sys_crypto_aml_routing_scopes s JOIN cat_wallets w
        ON w.custody_merchant_id=s.merchant_id AND w.network_id=s.network_id
        WHERE w.id=root_wallet AND s.token_id=asset AND s.enabled FOR UPDATE OF s;
    IF NOT FOUND THEN RETURN; END IF;
    FOR affected IN WITH RECURSIVE reach(wallet_id) AS (
        SELECT root_wallet UNION SELECT e.destination_wallet_id FROM crypto_aml_risk_edges e JOIN reach r ON e.source_wallet_id=r.wallet_id
        WHERE e.merchant_id=scope_row.merchant_id AND e.network_id=scope_row.network_id AND e.token_id=asset
    ) SELECT wallet_id FROM reach ORDER BY wallet_id LOOP
        SELECT risk_class INTO prior FROM reg_crypto_wallet_risk_states WHERE wallet_id=affected AND token_id=asset FOR UPDATE;
        prior:=COALESCE(prior,'unknown');
        INSERT INTO reg_crypto_wallet_risk_states(wallet_id,merchant_id,network_id,token_id,risk_class,revision,scope_version,reason)
            VALUES(affected,scope_row.merchant_id,scope_row.network_id,asset,CASE WHEN prior='high' THEN 'high' ELSE 'unknown' END,1,scope_row.version,why)
        ON CONFLICT(wallet_id,token_id) DO UPDATE SET risk_class=CASE WHEN reg_crypto_wallet_risk_states.risk_class='high' THEN 'high' ELSE 'unknown' END,
            revision=reg_crypto_wallet_risk_states.revision+1,scope_version=scope_row.version,evidence_hash='',reason=why,expires_at=NULL,inventory_covered=FALSE
        RETURNING revision INTO rev;
        INSERT INTO reg_crypto_aml_risk_changes(wallet_id,token_id,merchant_id,network_id,previous_class,risk_class,revision,scope_version,reason,inventory_covered)
            SELECT affected,asset,scope_row.merchant_id,scope_row.network_id,prior,risk_class,rev,scope_row.version,why,FALSE
            FROM reg_crypto_wallet_risk_states WHERE wallet_id=affected AND token_id=asset;
        INSERT INTO reg_crypto_aml_projection_queue(wallet_id,token_id,merchant_id,network_id) VALUES(affected,asset,scope_row.merchant_id,scope_row.network_id)
            ON CONFLICT(wallet_id,token_id) DO UPDATE SET next_attempt_at=NOW();
    END LOOP;
END;
$$ LANGUAGE plpgsql;
CREATE FUNCTION fn_crypto_aml_balance_risk_guard() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP='DELETE' THEN PERFORM fn_crypto_aml_invalidate_wallet(OLD.wallet_id,OLD.token_id,'balance_changed'); RETURN OLD; END IF;
    IF TG_OP='UPDATE' AND (OLD.wallet_id,OLD.token_id) IS DISTINCT FROM (NEW.wallet_id,NEW.token_id) THEN
        PERFORM fn_crypto_aml_invalidate_wallet(OLD.wallet_id,OLD.token_id,'balance_changed');
    END IF;
    PERFORM fn_crypto_aml_invalidate_wallet(NEW.wallet_id,NEW.token_id,'balance_changed'); RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_balance_risk_guard AFTER INSERT OR UPDATE OR DELETE ON reg_crypto_balance_movements
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_balance_risk_guard();

ALTER TABLE doc_withdrawal_requests ADD COLUMN risk_class TEXT CHECK(risk_class IN ('low','medium','high','unknown'));
CREATE FUNCTION fn_crypto_aml_withdrawal_contract_guard() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP='UPDATE' AND OLD.risk_class IS DISTINCT FROM NEW.risk_class THEN
        RAISE EXCEPTION 'withdrawal risk class is immutable; create a new approved request' USING ERRCODE='23514';
    END IF;
    IF TG_OP='INSERT' AND NEW.risk_class IS NULL AND EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes s JOIN cat_tokens t ON t.id=NEW.token_id
        WHERE s.merchant_id=NEW.merchant_id AND s.network_id=t.network_id AND s.token_id=t.id AND s.enabled) THEN
        RAISE EXCEPTION 'AML withdrawal requires an explicit risk class' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_withdrawal_contract_guard BEFORE INSERT OR UPDATE ON doc_withdrawal_requests
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_withdrawal_contract_guard();
CREATE FUNCTION fn_crypto_aml_effect_subject_guard() RETURNS TRIGGER AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM reg_crypto_aml_effects e JOIN reg_crypto_aml_subjects s ON s.id=NEW.subject_id
        AND (s.wallet_id,s.merchant_id,s.network_id,s.token_id)=(e.wallet_id,e.merchant_id,e.network_id,e.token_id)
        AND s.subject_kind='transfer' AND s.request_payload->>'txHash'=e.tx_hash
        AND s.request_payload->>'eventKind'=e.event_kind AND s.request_payload->>'eventIndex'=e.event_index::text
        AND s.request_payload->>'blockHash'=e.block_hash AND s.request_payload->>'economicsHash'=e.economics_hash
        WHERE e.id=NEW.effect_id AND e.source_wallet_id IS NULL) THEN
        RAISE EXCEPTION 'AML effect/check identity mismatch' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_effect_subject_guard BEFORE INSERT ON reg_crypto_aml_effect_checks
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_effect_subject_guard();

CREATE FUNCTION fn_crypto_aml_baseline_check_guard() RETURNS TRIGGER AS $$
BEGIN
    IF EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_baselines WHERE reconciliation_check_id=OLD.id) THEN
        RAISE EXCEPTION 'AML baseline check evidence is immutable' USING ERRCODE='23514';
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_baseline_check_guard BEFORE UPDATE OR DELETE ON reg_crypto_reconciliation_checks
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_baseline_check_guard();

CREATE FUNCTION fn_crypto_aml_effect_proof_guard() RETURNS TRIGGER AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM reg_crypto_transfer_verifications v JOIN cat_wallets w ON w.id=NEW.wallet_id
        JOIN cat_blockchain_networks n ON n.id=w.network_id JOIN cat_tokens t ON t.id=NEW.token_id
        WHERE v.id=NEW.verification_id AND (v.network_id,v.tx_hash,v.event_kind,v.event_index,v.block_number,v.block_hash,v.economics_hash)
          =(NEW.network_id,NEW.tx_hash,NEW.event_kind,NEW.event_index,NEW.block_number,NEW.block_hash,NEW.economics_hash)
        AND v.evidence->'event'->>'amount'=NEW.amount::text AND v.evidence->>'outcome'='verified'
        AND v.evidence->>'executionStatus'='succeeded' AND v.evidence->'event'->>'canonicality'='canonical'
        AND w.address_key=CASE WHEN n.chain_family='evm' THEN LOWER(v.evidence->'event'->>'toAddress') ELSE v.evidence->'event'->>'toAddress' END
        AND CASE WHEN n.chain_family='evm' THEN LOWER(COALESCE(t.contract_address,''))=LOWER(v.evidence->'event'->>'tokenContract') ELSE COALESCE(t.contract_address,'')=v.evidence->'event'->>'tokenContract' END) THEN
        RAISE EXCEPTION 'AML effect does not match verified chain evidence' USING ERRCODE='23514';
    END IF;
    IF NEW.source_wallet_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM reg_crypto_expected_chain_effects x
        JOIN reg_crypto_signing_authorizations a ON a.id=x.signing_authorization_id AND a.signed_envelope IS NOT NULL AND a.merchant_id=NEW.merchant_id
        JOIN cat_wallets sw ON sw.id=NEW.source_wallet_id AND sw.address_key=x.source_address_key
        JOIN cat_wallets dw ON dw.id=NEW.wallet_id AND dw.address_key=x.destination_address_key
        JOIN cat_blockchain_networks n ON n.id=NEW.network_id JOIN reg_crypto_transfer_verifications v ON v.id=NEW.verification_id
        WHERE x.id=NEW.expected_effect_id AND (x.network_id,x.token_id,x.tx_hash,x.event_kind,x.amount)=(NEW.network_id,NEW.token_id,NEW.tx_hash,NEW.event_kind,NEW.amount)
        AND x.source_address_key=CASE WHEN n.chain_family='evm' THEN LOWER(v.evidence->'event'->>'fromAddress') ELSE v.evidence->'event'->>'fromAddress' END) THEN
        RAISE EXCEPTION 'AML internal provenance does not match signed effect' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_effect_proof_guard BEFORE INSERT ON reg_crypto_aml_effects
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_effect_proof_guard();

CREATE FUNCTION fn_crypto_aml_scope_changed() RETURNS TRIGGER AS $$
BEGIN
    IF NOT NEW.enabled THEN RETURN NEW; END IF;
    WITH prior AS MATERIALIZED(SELECT w.id,COALESCE(r.risk_class,'unknown') AS previous_class
        FROM cat_wallets w LEFT JOIN reg_crypto_wallet_risk_states r ON r.wallet_id=w.id AND r.token_id=NEW.token_id
        WHERE w.custody_merchant_id=NEW.merchant_id AND w.network_id=NEW.network_id),
    changed AS (INSERT INTO reg_crypto_wallet_risk_states(wallet_id,token_id,merchant_id,network_id,risk_class,scope_version,reason)
        SELECT id,NEW.token_id,NEW.merchant_id,NEW.network_id,CASE WHEN previous_class='high' THEN 'high' ELSE 'unknown' END,NEW.version,'routing_scope_changed' FROM prior
        ON CONFLICT(wallet_id,token_id) DO UPDATE SET risk_class=CASE WHEN reg_crypto_wallet_risk_states.risk_class='high' THEN 'high' ELSE 'unknown' END,
            revision=reg_crypto_wallet_risk_states.revision+1,scope_version=NEW.version,evidence_hash='',reason='routing_scope_changed',expires_at=NULL,inventory_covered=FALSE
        RETURNING wallet_id,risk_class,revision)
    INSERT INTO reg_crypto_aml_risk_changes(wallet_id,token_id,merchant_id,network_id,previous_class,risk_class,revision,scope_version,reason,inventory_covered)
        SELECT c.wallet_id,NEW.token_id,NEW.merchant_id,NEW.network_id,p.previous_class,c.risk_class,c.revision,NEW.version,'routing_scope_changed',FALSE
        FROM changed c JOIN prior p ON p.id=c.wallet_id;
    DELETE FROM reg_crypto_aml_decision_queue q USING reg_crypto_aml_reports report,reg_crypto_aml_subjects subject
        WHERE q.report_id=report.id AND report.subject_id=subject.id AND subject.merchant_id=NEW.merchant_id AND subject.network_id=NEW.network_id AND subject.token_id=NEW.token_id AND q.policy_version<>NEW.policy_version;
    INSERT INTO reg_crypto_aml_decision_queue(report_id,policy_version)
        SELECT report.id,NEW.policy_version FROM reg_crypto_aml_reports report JOIN reg_crypto_aml_subjects subject ON subject.id=report.subject_id
        WHERE subject.merchant_id=NEW.merchant_id AND subject.network_id=NEW.network_id AND subject.token_id=NEW.token_id ON CONFLICT DO NOTHING;
    INSERT INTO reg_crypto_aml_projection_queue(wallet_id,token_id,merchant_id,network_id)
        SELECT id,NEW.token_id,NEW.merchant_id,NEW.network_id FROM cat_wallets WHERE custody_merchant_id=NEW.merchant_id AND network_id=NEW.network_id
        ON CONFLICT(wallet_id,token_id) DO UPDATE SET next_attempt_at=NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_scope_changed AFTER INSERT OR UPDATE ON sys_crypto_aml_routing_scopes
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_scope_changed();

CREATE FUNCTION fn_crypto_aml_token_identity_guard() RETURNS TRIGGER AS $$
BEGIN
    IF (NEW.network_id,NEW.contract_address,NEW.decimal_places,NEW.token_standard) IS DISTINCT FROM
       (OLD.network_id,OLD.contract_address,OLD.decimal_places,OLD.token_standard) AND
       EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes WHERE token_id=OLD.id AND enabled) THEN
        RAISE EXCEPTION 'active AML token identity is immutable' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_token_identity_guard BEFORE UPDATE ON cat_tokens
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_token_identity_guard();

-- A direct balance repair cannot retain an old covered risk projection. The
-- movement trigger already invalidates normal posting, avoiding duplicate
-- invalidations on its derived balance write.
CREATE FUNCTION fn_crypto_aml_balance_projection_guard() RETURNS TRIGGER AS $$
DECLARE target_wallet UUID; asset UUID;
BEGIN
    IF TG_OP='UPDATE' AND (OLD.wallet_id,OLD.token_id) IS DISTINCT FROM (NEW.wallet_id,NEW.token_id) AND
       EXISTS(SELECT 1 FROM reg_crypto_wallet_risk_states WHERE wallet_id=OLD.wallet_id AND token_id=OLD.token_id AND inventory_covered) THEN
        PERFORM fn_crypto_aml_invalidate_wallet(OLD.wallet_id,OLD.token_id,'balance_projection_changed');
    END IF;
    IF TG_OP='DELETE' THEN target_wallet:=OLD.wallet_id; asset:=OLD.token_id;
    ELSE target_wallet:=NEW.wallet_id; asset:=NEW.token_id; END IF;
    IF EXISTS(SELECT 1 FROM reg_crypto_wallet_risk_states WHERE wallet_id=target_wallet AND token_id=asset AND inventory_covered) THEN
        PERFORM fn_crypto_aml_invalidate_wallet(target_wallet,asset,'balance_projection_changed');
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_balance_projection_guard AFTER INSERT OR UPDATE OR DELETE ON reg_crypto_balance_balances
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_balance_projection_guard();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM reg_crypto_aml_effects) OR EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_baselines)
       OR EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes WHERE enabled) THEN
        RAISE EXCEPTION 'cannot roll back active AML routing evidence; use a forward migration';
    END IF;
END $$;
DROP TRIGGER trg_crypto_aml_balance_projection_guard ON reg_crypto_balance_balances;
DROP FUNCTION fn_crypto_aml_balance_projection_guard();
DROP TRIGGER trg_crypto_aml_token_identity_guard ON cat_tokens;
DROP FUNCTION fn_crypto_aml_token_identity_guard();
DROP TRIGGER trg_crypto_aml_scope_changed ON sys_crypto_aml_routing_scopes;
DROP FUNCTION fn_crypto_aml_scope_changed();
DROP TRIGGER trg_crypto_aml_effect_proof_guard ON reg_crypto_aml_effects;
DROP FUNCTION fn_crypto_aml_effect_proof_guard();
DROP TRIGGER trg_crypto_aml_baseline_check_guard ON reg_crypto_reconciliation_checks;
DROP FUNCTION fn_crypto_aml_baseline_check_guard();
DROP TRIGGER trg_crypto_aml_withdrawal_contract_guard ON doc_withdrawal_requests;
DROP FUNCTION fn_crypto_aml_withdrawal_contract_guard();
ALTER TABLE doc_withdrawal_requests DROP COLUMN risk_class;
DROP TRIGGER trg_crypto_aml_effect_subject_guard ON reg_crypto_aml_effect_checks;
DROP FUNCTION fn_crypto_aml_effect_subject_guard();
DROP TRIGGER trg_crypto_aml_balance_risk_guard ON reg_crypto_balance_movements;
DROP FUNCTION fn_crypto_aml_balance_risk_guard();
DROP FUNCTION fn_crypto_aml_invalidate_wallet(UUID,UUID,TEXT);
DROP VIEW crypto_aml_risk_edges;
DROP TABLE reg_crypto_aml_risk_changes,reg_crypto_aml_projection_queue,reg_crypto_aml_decision_queue;
ALTER TABLE reg_crypto_wallet_risk_states DROP COLUMN scope_version,DROP COLUMN evidence_hash,DROP COLUMN reason,DROP COLUMN inventory_covered;
DROP TABLE reg_crypto_aml_rescreen_requests;
DROP TABLE reg_crypto_aml_effect_chain_states,reg_crypto_aml_effect_checks,reg_crypto_aml_effects;
DROP TABLE reg_crypto_aml_wallet_baselines;
DROP FUNCTION fn_crypto_aml_baseline_guard();
DROP TABLE sys_crypto_aml_routing_scopes;
DROP FUNCTION fn_crypto_aml_routing_scope_guard();
DROP TABLE sys_crypto_aml_risk_policies;
-- +goose StatementEnd
