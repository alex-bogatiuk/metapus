-- Crypto AML: providers and routing, subjects and checks, inventory, resources,
-- batches, merchant provisioning and pools, preflight, dispatch permits,
-- risk-aware withdrawals and the testnet emulator.
-- Consolidated schema: each object is declared once in its final form.

-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- Function bodies are validated when first executed; definitions precede the tables they use.
SET LOCAL check_function_bodies = off;


-- ---- Functions ----
CREATE FUNCTION fn_crypto_wallet_custody_owner_guard() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        IF OLD.custody_merchant_id IS NOT NULL THEN
            RAISE EXCEPTION 'bound wallet custody identity cannot be deleted' USING ERRCODE='23514';
        END IF;
        RETURN OLD;
    END IF;
    IF TG_OP='UPDATE' AND OLD.custody_merchant_id IS NOT NULL AND
       (OLD.custody_merchant_id,OLD.network_id,OLD.address,OLD.vault_key_id,OLD.derivation_path)
       IS DISTINCT FROM
       (NEW.custody_merchant_id,NEW.network_id,NEW.address,NEW.vault_key_id,NEW.derivation_path) THEN
        RAISE EXCEPTION 'bound wallet custody identity is immutable' USING ERRCODE='23514';
    END IF;
    IF NEW.custody_merchant_id IS NOT NULL AND NEW.merchant_id IS NOT NULL
       AND NEW.custody_merchant_id<>NEW.merchant_id THEN
        RAISE EXCEPTION 'wallet assignment crosses merchant custody boundary' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_evidence_guard() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'AML evidence is append-only' USING ERRCODE='23514';
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_job_identity_guard() RETURNS TRIGGER AS $$
BEGIN
    IF (OLD.subject_id,OLD.generation,OLD.provider,OLD.provider_account_id,OLD.request_payload,
        OLD.request_digest,OLD.mapping_version,OLD.submit_request_id,OLD.deadline_at)
       IS DISTINCT FROM
       (NEW.subject_id,NEW.generation,NEW.provider,NEW.provider_account_id,NEW.request_payload,
        NEW.request_digest,NEW.mapping_version,NEW.submit_request_id,NEW.deadline_at) THEN
        RAISE EXCEPTION 'AML job identity is immutable' USING ERRCODE='23514';
    END IF;
    IF OLD.ticket IS NOT NULL AND OLD.ticket IS DISTINCT FROM NEW.ticket THEN
        RAISE EXCEPTION 'AML ticket is immutable' USING ERRCODE='23514';
    END IF;
    IF OLD.status<>'pending_submit' AND NEW.status='pending_submit' THEN
        RAISE EXCEPTION 'paid AML submit cannot be reset; create a new generation' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_subject_identity_guard() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.generation<OLD.generation THEN
        RAISE EXCEPTION 'AML subject generation cannot decrease' USING ERRCODE='23514';
    END IF;
    IF (OLD.merchant_id,OLD.network_id,OLD.token_id,OLD.wallet_id,OLD.subject_kind,
        OLD.request_digest,OLD.request_payload,OLD.mapping_version,OLD.created_at)
       IS DISTINCT FROM
       (NEW.merchant_id,NEW.network_id,NEW.token_id,NEW.wallet_id,NEW.subject_kind,
        NEW.request_digest,NEW.request_payload,NEW.mapping_version,NEW.created_at) THEN
        RAISE EXCEPTION 'AML subject identity is immutable' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_custody_policy_guard() RETURNS TRIGGER AS $$
DECLARE execution_enabled BOOLEAN;
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'custody policy cannot be deleted' USING ERRCODE='23514';
    END IF;
    IF TG_OP='UPDATE' THEN
        IF NEW.network_id<>OLD.network_id OR NEW.version<>OLD.version+1 THEN
            RAISE EXCEPTION 'custody policy requires a new version in the same network' USING ERRCODE='23514';
        END IF;
        IF OLD.enforced AND NOT NEW.enforced THEN
            RAISE EXCEPTION 'merchant custody enforcement cannot be downgraded' USING ERRCODE='23514';
        END IF;
    END IF;
    IF NEW.enforced AND (TG_OP='INSERT' OR NOT OLD.enforced) THEN
        SELECT enabled INTO execution_enabled FROM sys_crypto_network_policies
            WHERE network_id=NEW.network_id FOR UPDATE;
        IF execution_enabled THEN
            RAISE EXCEPTION 'pause network execution before enabling merchant custody' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_mapping_scope_guard() RETURNS TRIGGER AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM cat_tokens t JOIN cat_blockchain_networks n ON n.id=t.network_id
        WHERE t.id=NEW.token_id AND t.network_id=NEW.network_id AND t.decimal_places=NEW.decimal_places
          AND (t.token_standard='native')=NEW.native
          AND ((n.chain_family='tron' AND NEW.currency='TRX' AND COALESCE(t.contract_address,'')=NEW.contract_address)
            OR (n.chain_family='evm' AND NEW.currency='ETH' AND LOWER(COALESCE(t.contract_address,''))=LOWER(NEW.contract_address)))) THEN
        RAISE EXCEPTION 'AML mapping does not match network/token/contract/decimals' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_crypto_aml_runtime_account_guard() RETURNS TRIGGER AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM cat_integration_accounts WHERE id=NEW.account_id AND provider IN ('getblock_net','testnet_aml')) THEN
        RAISE EXCEPTION 'AML runtime requires a Getblock.net account' USING ERRCODE='23514';
    END IF;
    IF TG_TABLE_NAME<>'sys_crypto_aml_account_policies' AND EXISTS(SELECT 1 FROM cat_integration_accounts WHERE id=NEW.account_id AND provider='testnet_aml') THEN RAISE EXCEPTION 'emulated AML cannot consume paid credits or admissions' USING ERRCODE='23514'; END IF;
    IF TG_TABLE_NAME='sys_crypto_aml_account_policies' AND TG_OP='UPDATE' THEN
        IF NEW.account_id<>OLD.account_id OR NEW.version<>OLD.version+1 THEN
            RAISE EXCEPTION 'AML account policy requires a new version' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_crypto_aml_routing_scope_guard() RETURNS TRIGGER AS $$
DECLARE executing BOOLEAN;
BEGIN
    IF TG_OP='DELETE' THEN RAISE EXCEPTION 'AML routing scope cannot be deleted' USING ERRCODE='23514'; END IF;
    IF TG_OP='UPDATE' THEN
        IF (NEW.merchant_id,NEW.network_id,NEW.token_id) IS DISTINCT FROM (OLD.merchant_id,OLD.network_id,OLD.token_id)
           OR NEW.version<>OLD.version+1 OR (OLD.enabled AND NOT NEW.enabled) THEN
            RAISE EXCEPTION 'AML routing scope identity/version cannot be downgraded' USING ERRCODE='23514';
        END IF;
    END IF;
    IF NOT EXISTS(SELECT 1 FROM cat_integration_accounts WHERE id=NEW.provider_account_id AND provider IN ('getblock_net','testnet_aml')) THEN
        RAISE EXCEPTION 'AML routing requires a Getblock.net account' USING ERRCODE='23514';
    END IF;
    IF EXISTS(SELECT 1 FROM cat_integration_accounts WHERE id=NEW.provider_account_id AND provider='testnet_aml') THEN
      IF NOT EXISTS(SELECT 1 FROM sys_crypto_aml_testnet_emulators e JOIN cat_blockchain_networks n ON n.id=e.network_id
          JOIN sys_crypto_aml_provider_mappings m ON m.network_id=e.network_id AND m.token_id=NEW.token_id AND m.version=NEW.mapping_version
          WHERE e.account_id=NEW.provider_account_id AND e.network_id=NEW.network_id AND fn_crypto_aml_is_emulation_network(n.chain_family,n.network_identifier)
            AND m.contract_version='aml-testnet.v1' AND m.currencylist_snapshot->'emulated'='true'::jsonb) THEN
        RAISE EXCEPTION 'emulated AML scope requires its pinned Nile/Sepolia binding and synthetic mapping' USING ERRCODE='23514';
      END IF;
    ELSIF EXISTS(SELECT 1 FROM sys_crypto_aml_provider_mappings WHERE network_id=NEW.network_id AND token_id=NEW.token_id AND version=NEW.mapping_version AND contract_version='aml-testnet.v1') THEN
      RAISE EXCEPTION 'synthetic AML mapping cannot be used by a live provider' USING ERRCODE='23514';
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

CREATE FUNCTION fn_crypto_aml_baseline_check_guard() RETURNS TRIGGER AS $$
BEGIN
    IF EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_baselines WHERE reconciliation_check_id=OLD.id) THEN
        RAISE EXCEPTION 'AML baseline check evidence is immutable' USING ERRCODE='23514';
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

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

CREATE FUNCTION fn_crypto_aml_allocation_revision() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP='INSERT' OR (OLD.status='reserved' AND NEW.status='cancelled') THEN
        UPDATE reg_crypto_aml_inventory_positions SET revision=revision+1 WHERE token_id=NEW.token_id AND wallet_id IN (NEW.source_wallet_id,NEW.destination_wallet_id);
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

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

CREATE FUNCTION fn_crypto_aml_inventory_document_queue() RETURNS TRIGGER AS $$
BEGIN
    IF TG_TABLE_NAME='doc_crypto_payments' THEN PERFORM fn_crypto_aml_queue_inventory('CryptoPayment',NEW.id);
    ELSE PERFORM fn_crypto_aml_queue_inventory('CryptoSweep',NEW.id); END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_inventory_baseline() RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO reg_crypto_aml_inventory_positions(wallet_id,token_id,merchant_id,network_id) VALUES(NEW.wallet_id,NEW.token_id,NEW.merchant_id,NEW.network_id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

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

CREATE OR REPLACE FUNCTION fn_crypto_aml_lock_resource_scopes(reservation UUID) RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
    PERFORM fn_crypto_lock_custody_scopes(ARRAY(SELECT payer_wallet_id FROM reg_crypto_fee_reservations WHERE id=reservation));
END;
$$;

CREATE OR REPLACE FUNCTION fn_crypto_aml_lock_fee_wallet_scopes(payer UUID,asset UUID) RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
    PERFORM fn_crypto_lock_custody_scopes(ARRAY[payer]);
END;
$$;

CREATE FUNCTION fn_crypto_aml_resource_fee_insert_guard() RETURNS TRIGGER AS $$
BEGIN
 PERFORM fn_crypto_aml_lock_fee_wallet_scopes(NEW.payer_wallet_id,NEW.native_token_id);
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

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

CREATE FUNCTION fn_crypto_aml_resource_budget_revision() RETURNS TRIGGER AS $$
BEGIN
 IF TG_OP='INSERT' OR (OLD.status='reserved' AND NEW.status='cancelled') THEN
   UPDATE reg_crypto_aml_resource_positions SET revision=revision+1 WHERE wallet_id=NEW.wallet_id AND token_id=NEW.token_id;
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

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

CREATE FUNCTION fn_crypto_aml_resource_fact_apply() RETURNS TRIGGER AS $$
DECLARE target UUID;
BEGIN
 FOR target IN SELECT wallet_id FROM reg_crypto_aml_resource_positions WHERE token_id=NEW.token_id AND wallet_id IN (NEW.source_wallet_id,NEW.destination_wallet_id) ORDER BY wallet_id LOOP
   UPDATE reg_crypto_aml_resource_positions SET amount=amount+CASE WHEN target=NEW.destination_wallet_id THEN NEW.amount ELSE -NEW.amount END,revision=revision+1 WHERE wallet_id=target AND token_id=NEW.token_id;
 END LOOP;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_principal_resource_exclusion() RETURNS TRIGGER AS $$
BEGIN
 PERFORM 1 FROM sys_crypto_aml_routing_scopes WHERE merchant_id=NEW.merchant_id AND network_id=NEW.network_id AND token_id=NEW.token_id FOR UPDATE;
 IF EXISTS(SELECT 1 FROM reg_crypto_aml_resource_facts WHERE effect_id=NEW.effect_id) THEN RAISE EXCEPTION 'chain effect already allocated to operating resources' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_resource_baseline() RETURNS TRIGGER AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM cat_tokens WHERE id=NEW.token_id AND token_standard='native') THEN
   INSERT INTO reg_crypto_aml_resource_positions(wallet_id,token_id,merchant_id,network_id) VALUES(NEW.wallet_id,NEW.token_id,NEW.merchant_id,NEW.network_id);
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

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

CREATE OR REPLACE FUNCTION fn_crypto_aml_resource_signing_guard() RETURNS TRIGGER AS $$
BEGIN PERFORM fn_crypto_aml_check_resource_dispatch(NEW,'authorize'); RETURN NEW; END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_queue_resource_budget(reservation UUID) RETURNS VOID AS $$
BEGIN
 PERFORM 1 FROM reg_crypto_aml_resource_budgets b JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=b.merchant_id AND s.network_id=b.network_id AND s.token_id=b.token_id WHERE b.fee_reservation_id=reservation FOR UPDATE OF s;
 INSERT INTO reg_crypto_aml_inventory_queue(kind,basis_id,merchant_id,network_id,token_id)
    SELECT 'resource',id,merchant_id,network_id,token_id FROM reg_crypto_aml_resource_budgets WHERE fee_reservation_id=reservation AND status='reserved' ON CONFLICT DO NOTHING;
END;
$$ LANGUAGE plpgsql;

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

CREATE FUNCTION fn_crypto_aml_batch_policy_guard() RETURNS TRIGGER AS $$
BEGIN
 PERFORM 1 FROM sys_crypto_aml_routing_scopes s JOIN cat_tokens t ON t.id=s.token_id AND t.token_standard<>'native'
   JOIN sys_crypto_custody_policies custody ON custody.network_id=s.network_id AND custody.enforced
   JOIN sys_crypto_network_policies network ON network.network_id=s.network_id AND NOT network.enabled
   WHERE s.merchant_id=NEW.merchant_id AND s.network_id=NEW.network_id AND s.token_id=NEW.token_id AND s.enabled FOR UPDATE OF s;
 IF NOT FOUND OR NEW.version<>(SELECT COALESCE(MAX(version),0)+1 FROM sys_crypto_aml_batch_policies WHERE merchant_id=NEW.merchant_id AND network_id=NEW.network_id AND token_id=NEW.token_id AND risk_class=NEW.risk_class)
    OR EXISTS(SELECT 1 FROM reg_crypto_aml_internal_allocations WHERE merchant_id=NEW.merchant_id AND network_id=NEW.network_id AND token_id=NEW.token_id AND risk_class=NEW.risk_class AND batch_id IS NULL AND status='reserved') THEN
   RAISE EXCEPTION 'batch policy requires paused owned scope, next version and drained legacy allocations' USING ERRCODE='23514';
 END IF;
 UPDATE reg_crypto_aml_batches SET intake_closed_at=COALESCE(intake_closed_at,NOW()),close_reason=CASE WHEN intake_closed_at IS NULL THEN 'policy_changed' ELSE close_reason END,
   execution_held=TRUE,hold_reason='policy_changed',last_actor=NEW.configured_by
 WHERE merchant_id=NEW.merchant_id AND network_id=NEW.network_id AND token_id=NEW.token_id AND risk_class=NEW.risk_class AND NOT execution_held;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_batch_guard() RETURNS TRIGGER AS $$
DECLARE policy sys_crypto_aml_batch_policies; scoped sys_crypto_aml_routing_scopes;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'batch evidence cannot be deleted' USING ERRCODE='23514'; END IF;
 SELECT * INTO scoped FROM sys_crypto_aml_routing_scopes WHERE merchant_id=NEW.merchant_id AND network_id=NEW.network_id AND token_id=NEW.token_id FOR UPDATE;
 IF TG_OP='UPDATE' THEN
   IF (NEW.id,NEW.merchant_id,NEW.network_id,NEW.token_id,NEW.risk_class,NEW.policy_version,NEW.scope_version,NEW.collector_wallet_id,NEW.max_gross,NEW.opened_at,NEW.intake_closes_at,NEW.request_digest,NEW.created_by,NEW.created_at)
      IS DISTINCT FROM (OLD.id,OLD.merchant_id,OLD.network_id,OLD.token_id,OLD.risk_class,OLD.policy_version,OLD.scope_version,OLD.collector_wallet_id,OLD.max_gross,OLD.opened_at,OLD.intake_closes_at,OLD.request_digest,OLD.created_by,OLD.created_at)
      OR (OLD.intake_closed_at IS NOT NULL AND (NEW.intake_closed_at,NEW.close_reason) IS DISTINCT FROM (OLD.intake_closed_at,OLD.close_reason))
      OR (OLD.execution_held AND (NOT NEW.execution_held OR NEW.hold_reason<>OLD.hold_reason)) THEN
     RAISE EXCEPTION 'batch identity and closed/held history are immutable' USING ERRCODE='23514';
   END IF;
   NEW.revision:=OLD.revision+1;
   NEW.execution_revision:=OLD.execution_revision+CASE WHEN NOT OLD.execution_held AND NEW.execution_held THEN 1 ELSE 0 END;
   RETURN NEW;
 END IF;
 SELECT * INTO policy FROM sys_crypto_aml_batch_policies WHERE merchant_id=NEW.merchant_id AND network_id=NEW.network_id AND token_id=NEW.token_id AND risk_class=NEW.risk_class ORDER BY version DESC LIMIT 1;
 IF policy.version IS NULL OR NOT scoped.enabled OR scoped.version<>NEW.scope_version OR policy.version<>NEW.policy_version OR policy.max_gross<>NEW.max_gross
    OR NEW.opened_at IS DISTINCT FROM transaction_timestamp() OR NEW.intake_closes_at<>NEW.opened_at+make_interval(secs=>policy.max_age_seconds)
    OR NEW.intake_closed_at IS NOT NULL OR NEW.execution_held OR NEW.revision<>1 OR NEW.execution_revision<>1
    OR NOT EXISTS(SELECT 1 FROM cat_wallets w JOIN reg_crypto_aml_inventory_positions inventory ON inventory.wallet_id=w.id AND inventory.token_id=NEW.token_id
       JOIN reg_crypto_wallet_risk_states risk ON risk.wallet_id=w.id AND risk.token_id=NEW.token_id AND risk.scope_version=NEW.scope_version AND risk.risk_class<>'high'
       WHERE w.id=NEW.collector_wallet_id AND w.wallet_role='liquidity_hot' AND w.custody_merchant_id=NEW.merchant_id AND w.network_id=NEW.network_id AND w.is_active AND NOT w.deletion_mark AND w.chain_activation_status='active'
         AND inventory.principal+inventory.service_fee=0 AND fn_crypto_aml_total_inventory_covered(w.id,NEW.token_id)
         AND NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_allocation_holds WHERE wallet_id=w.id AND status='open'))
    OR EXISTS(SELECT 1 FROM reg_crypto_aml_effects WHERE wallet_id=NEW.collector_wallet_id AND token_id=NEW.token_id)
    OR EXISTS(SELECT 1 FROM reg_crypto_balance_movements WHERE wallet_id=NEW.collector_wallet_id AND token_id=NEW.token_id)
    OR EXISTS(SELECT 1 FROM reg_crypto_aml_effects e JOIN cat_tokens t ON t.id=e.token_id AND t.token_standard<>'native' WHERE e.wallet_id=NEW.collector_wallet_id AND e.token_id<>NEW.token_id)
    OR EXISTS(SELECT 1 FROM reg_crypto_balance_balances balance JOIN cat_tokens t ON t.id=balance.token_id AND t.token_standard<>'native' WHERE balance.wallet_id=NEW.collector_wallet_id AND balance.token_id<>NEW.token_id AND balance.amount<>0)
    OR EXISTS(SELECT 1 FROM reg_crypto_aml_internal_allocations WHERE destination_wallet_id=NEW.collector_wallet_id OR source_wallet_id=NEW.collector_wallet_id) THEN
   RAISE EXCEPTION 'batch requires current policy and a fresh empty owned collector' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_batch_audit() RETURNS TRIGGER AS $$
BEGIN
 IF TG_OP='INSERT' THEN
   INSERT INTO reg_crypto_aml_batch_events(batch_id,event_kind,reason,recorded_by) VALUES(NEW.id,'opened','collector_enrolled',NEW.created_by);
 ELSE
   IF OLD.intake_closed_at IS NULL AND NEW.intake_closed_at IS NOT NULL THEN
     INSERT INTO reg_crypto_aml_batch_events(batch_id,event_kind,reason,recorded_by) VALUES(NEW.id,'closed',NEW.close_reason,NEW.last_actor);
   END IF;
   IF NOT OLD.execution_held AND NEW.execution_held THEN
     INSERT INTO reg_crypto_aml_batch_events(batch_id,event_kind,reason,recorded_by) VALUES(NEW.id,'held',NEW.hold_reason,NEW.last_actor);
   END IF;
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_batch_allocation_guard() RETURNS TRIGGER AS $$
DECLARE batch reg_crypto_aml_batches; used NUMERIC;
BEGIN
 PERFORM 1 FROM sys_crypto_aml_routing_scopes WHERE merchant_id=NEW.merchant_id AND network_id=NEW.network_id AND token_id=NEW.token_id FOR UPDATE;
 IF TG_OP='UPDATE' THEN
   IF (NEW.batch_id,NEW.batch_revision,NEW.batch_execution_revision) IS DISTINCT FROM (OLD.batch_id,OLD.batch_revision,OLD.batch_execution_revision) THEN
     RAISE EXCEPTION 'allocation batch identity is immutable' USING ERRCODE='23514';
   END IF;
   RETURN NEW;
 END IF;
 IF NEW.batch_id IS NULL THEN
   IF EXISTS(SELECT 1 FROM reg_crypto_aml_batches WHERE collector_wallet_id=NEW.destination_wallet_id OR collector_wallet_id=NEW.source_wallet_id)
      OR EXISTS(SELECT 1 FROM sys_crypto_aml_batch_policies WHERE merchant_id=NEW.merchant_id AND network_id=NEW.network_id AND token_id=NEW.token_id AND risk_class=NEW.risk_class) THEN
     RAISE EXCEPTION 'AML consolidation requires an explicit batch' USING ERRCODE='23514';
   END IF;
   RETURN NEW;
 END IF;
 SELECT * INTO batch FROM reg_crypto_aml_batches WHERE id=NEW.batch_id FOR UPDATE;
 SELECT usage.used INTO used FROM crypto_aml_batch_usage usage WHERE usage.id=NEW.batch_id;
 IF batch.id IS NULL OR (batch.merchant_id,batch.network_id,batch.token_id,batch.risk_class,batch.collector_wallet_id,batch.scope_version,batch.revision,batch.execution_revision)
    IS DISTINCT FROM (NEW.merchant_id,NEW.network_id,NEW.token_id,NEW.risk_class,NEW.destination_wallet_id,NEW.scope_version,NEW.batch_revision,NEW.batch_execution_revision)
    OR batch.intake_closed_at IS NOT NULL OR batch.intake_closes_at<=clock_timestamp() OR batch.execution_held
    OR used+NEW.principal+NEW.service_fee>batch.max_gross THEN
   RAISE EXCEPTION 'batch is closed, stale, held or lacks capacity' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_batch_allocation_audit() RETURNS TRIGGER AS $$
DECLARE event TEXT; usage NUMERIC;
BEGIN
 IF NEW.batch_id IS NULL THEN RETURN NEW; END IF;
 IF TG_OP='UPDATE' AND NEW.status=OLD.status THEN RETURN NEW; END IF;
 event:=CASE WHEN TG_OP='INSERT' THEN 'reserved' ELSE NEW.status END;
 INSERT INTO reg_crypto_aml_batch_events(batch_id,event_kind,allocation_id,reason,recorded_by) VALUES(NEW.batch_id,event,NEW.id,'allocation_'||event,'aml-inventory');
 SELECT used INTO usage FROM crypto_aml_batch_usage WHERE id=NEW.batch_id;
 IF usage>= (SELECT max_gross FROM reg_crypto_aml_batches WHERE id=NEW.batch_id) THEN
   UPDATE reg_crypto_aml_batches SET intake_closed_at=NOW(),close_reason='capacity',last_actor='aml-inventory' WHERE id=NEW.batch_id AND intake_closed_at IS NULL;
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_batch_signing_guard() RETURNS TRIGGER AS $$
DECLARE plan reg_crypto_aml_internal_allocations; batch reg_crypto_aml_batches;
BEGIN
 IF NEW.basis_type<>'CryptoSweepAttempt' THEN RETURN NEW; END IF;
 SELECT * INTO plan FROM reg_crypto_aml_internal_allocations WHERE sweep_attempt_id=NEW.basis_id;
 IF plan.batch_id IS NULL THEN RETURN NEW; END IF;
 PERFORM 1 FROM sys_crypto_aml_routing_scopes WHERE merchant_id=plan.merchant_id AND network_id=plan.network_id AND token_id=plan.token_id FOR UPDATE;
 SELECT * INTO batch FROM reg_crypto_aml_batches WHERE id=plan.batch_id FOR UPDATE;
 IF NEW.purpose<>'sweep' OR batch.execution_held OR batch.execution_revision<>plan.batch_execution_revision
    OR NOT EXISTS(SELECT 1 FROM cat_wallets source JOIN cat_wallets collector ON collector.id=batch.collector_wallet_id
       WHERE source.id=plan.source_wallet_id AND source.wallet_role='merchant_pool' AND source.custody_merchant_id=batch.merchant_id AND source.network_id=batch.network_id AND source.is_active AND NOT source.deletion_mark
         AND collector.wallet_role='liquidity_hot' AND collector.custody_merchant_id=batch.merchant_id AND collector.network_id=batch.network_id AND collector.is_active AND NOT collector.deletion_mark AND collector.chain_activation_status='active'
         AND NOT EXISTS(SELECT 1 FROM reg_crypto_wallet_allocation_holds hold WHERE hold.wallet_id IN(source.id,collector.id) AND hold.status='open'))
    OR EXISTS(SELECT 1 FROM reg_crypto_aml_effects e JOIN reg_crypto_aml_effect_chain_states cs ON cs.effect_id=e.id AND cs.canonicality='canonical'
       WHERE e.wallet_id=batch.collector_wallet_id AND e.token_id=batch.token_id AND e.source_wallet_id IS NULL)
    OR NOT EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes WHERE merchant_id=batch.merchant_id AND network_id=batch.network_id AND token_id=batch.token_id AND version=batch.scope_version AND enabled) THEN
   RAISE EXCEPTION 'batch execution is held or stale' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_batch_observation() RETURNS TRIGGER AS $$
DECLARE target UUID; asset UUID; why TEXT;
BEGIN
 IF TG_TABLE_NAME='reg_crypto_wallet_risk_states' THEN
   IF NEW.risk_class<>'high' THEN RETURN NEW; END IF;
   target:=NEW.wallet_id; asset:=NEW.token_id; why:='late_high';
 ELSIF TG_TABLE_NAME='sys_crypto_aml_routing_scopes' THEN
   UPDATE reg_crypto_aml_batches SET intake_closed_at=COALESCE(intake_closed_at,NOW()),close_reason=CASE WHEN intake_closed_at IS NULL THEN 'scope_changed' ELSE close_reason END,
     execution_held=TRUE,hold_reason='scope_changed',last_actor='aml-routing' WHERE merchant_id=NEW.merchant_id AND network_id=NEW.network_id AND token_id=NEW.token_id AND scope_version<>NEW.version AND NOT execution_held;
   RETURN NEW;
 ELSE
   SELECT e.wallet_id,e.token_id INTO target,asset FROM reg_crypto_aml_effects e WHERE e.id=NEW.effect_id;
   IF NEW.canonicality<>'canonical' THEN why:='chain_changed';
   ELSIF NOT EXISTS(SELECT 1 FROM crypto_aml_batch_usage usage JOIN reg_crypto_aml_batches b ON b.id=usage.id WHERE b.collector_wallet_id=target AND b.token_id=asset AND usage.unplanned>0) THEN RETURN NEW;
   ELSE why:='unplanned_incoming'; END IF;
 END IF;
 PERFORM 1 FROM cat_wallets w JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=w.custody_merchant_id AND s.network_id=w.network_id AND s.token_id=asset WHERE w.id=target FOR UPDATE OF s;
 UPDATE reg_crypto_aml_batches b SET intake_closed_at=COALESCE(intake_closed_at,NOW()),close_reason=CASE WHEN intake_closed_at IS NULL THEN why ELSE close_reason END,
   execution_held=TRUE,hold_reason=why,last_actor='aml-routing' WHERE b.token_id=asset AND NOT b.execution_held AND
   (b.collector_wallet_id=target OR EXISTS(SELECT 1 FROM reg_crypto_aml_internal_allocations plan WHERE plan.batch_id=b.id AND plan.source_wallet_id=target AND plan.status IN ('reserved','applied')));
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_provision_guard() RETURNS TRIGGER AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'merchant provisioning history cannot be deleted' USING ERRCODE='23514'; END IF;
 IF TG_OP='UPDATE' AND ((OLD.id,OLD.merchant_id,OLD.network_id,OLD.wallet_role,OLD.derivation_path,OLD.request_digest,OLD.requested_by,OLD.created_at)
    IS DISTINCT FROM (NEW.id,NEW.merchant_id,NEW.network_id,NEW.wallet_role,NEW.derivation_path,NEW.request_digest,NEW.requested_by,NEW.created_at) OR OLD.status='completed') THEN
   RAISE EXCEPTION 'merchant provisioning identity and completion are immutable' USING ERRCODE='23514';
 END IF;
 IF TG_OP='INSERT' AND (NEW.status<>'processing' OR NEW.attempts<>1 OR NOT EXISTS(SELECT 1 FROM sys_crypto_custody_policies WHERE network_id=NEW.network_id AND enforced)) THEN
   RAISE EXCEPTION 'merchant provisioning requires enforced custody and an initial lease' USING ERRCODE='23514';
 END IF;
 IF NEW.status='completed' AND (OLD.status<>'processing' OR OLD.lease_expires_at<=clock_timestamp() OR NOT EXISTS(
   SELECT 1 FROM cat_wallets w WHERE w.id=NEW.wallet_id AND w.custody_merchant_id=NEW.merchant_id AND w.network_id=NEW.network_id AND w.wallet_role=NEW.wallet_role AND w.derivation_path=NEW.derivation_path)) THEN
   RAISE EXCEPTION 'merchant provisioning completion lacks its owned wallet or current lease' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_legacy_provision_guard() RETURNS TRIGGER AS $$
BEGIN
 IF NEW.custody_merchant_id IS NULL AND NEW.wallet_role='merchant_pool'
    AND EXISTS(SELECT 1 FROM sys_crypto_wallet_provisioning_jobs WHERE network_id=NEW.network_id AND derivation_path=NEW.derivation_path) THEN
   PERFORM 1 FROM sys_crypto_custody_policies WHERE network_id=NEW.network_id AND enforced FOR UPDATE;
   IF FOUND THEN RAISE EXCEPTION 'owned network requires trusted merchant provisioning' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Ready is usable address capacity, never a claim that a fee wallet has gas.
-- Pending includes mandatory preflight so an RPC/activation outage cannot
-- create a new address every reconciliation cycle.
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

CREATE FUNCTION fn_crypto_aml_network_wallet_commitments(network UUID,excluded UUID) RETURNS BIGINT AS $$
 SELECT (SELECT COUNT(*) FROM cat_wallets WHERE network_id=network)
   +(SELECT COUNT(*) FROM sys_crypto_wallet_provisioning_jobs WHERE network_id=network AND wallet_id IS NULL)
   +(SELECT COUNT(*) FROM reg_crypto_aml_wallet_provisions WHERE network_id=network AND wallet_id IS NULL)
   +(SELECT COUNT(*) FROM reg_crypto_aml_pool_intents i JOIN sys_crypto_aml_pool_policies p ON p.id=i.policy_id AND p.network_id=network
      WHERE i.id IS DISTINCT FROM excluded AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_provisions v WHERE v.id=i.id)
        AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_pool_intent_cancellations c WHERE c.intent_id=i.id));
$$ LANGUAGE SQL STABLE;

CREATE OR REPLACE FUNCTION fn_crypto_aml_pool_baselines_ready(intent UUID) RETURNS BOOLEAN AS $$
 SELECT EXISTS(SELECT 1 FROM reg_crypto_aml_pool_intents i JOIN sys_crypto_aml_pool_policies p ON p.id=i.policy_id AND p.wallet_role IN ('merchant_pool','liquidity_hot','network_fee')
   JOIN reg_crypto_aml_wallet_provisions v ON v.id=i.id AND v.status='completed' JOIN cat_wallets w ON w.id=v.wallet_id AND w.custody_merchant_id=p.merchant_id AND w.network_id=p.network_id AND w.wallet_role=p.wallet_role
   WHERE i.id=intent AND EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes s WHERE s.merchant_id=p.merchant_id AND s.network_id=p.network_id AND s.enabled AND (p.wallet_role<>'network_fee' OR EXISTS(SELECT 1 FROM cat_tokens native WHERE native.id=s.token_id AND native.token_standard='native')) AND (p.token_id IS NULL OR s.token_id=p.token_id OR (p.automatic_baseline AND EXISTS(SELECT 1 FROM cat_tokens native WHERE native.id=s.token_id AND native.token_standard='native'))))
   AND NOT EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes s LEFT JOIN reg_crypto_aml_wallet_baselines baseline ON baseline.wallet_id=w.id AND baseline.token_id=s.token_id
     LEFT JOIN reg_crypto_aml_inventory_positions inventory ON inventory.wallet_id=w.id AND inventory.token_id=s.token_id
     WHERE s.merchant_id=p.merchant_id AND s.network_id=p.network_id AND s.enabled AND (p.wallet_role<>'network_fee' OR EXISTS(SELECT 1 FROM cat_tokens native WHERE native.id=s.token_id AND native.token_standard='native')) AND (p.token_id IS NULL OR s.token_id=p.token_id OR (p.automatic_baseline AND EXISTS(SELECT 1 FROM cat_tokens native WHERE native.id=s.token_id AND native.token_standard='native')))
       AND (baseline.wallet_id IS NULL OR inventory.wallet_id IS NULL OR inventory.principal+inventory.service_fee<>0 OR NOT fn_crypto_aml_total_inventory_covered(w.id,s.token_id))));
$$ LANGUAGE SQL STABLE;

CREATE OR REPLACE FUNCTION fn_crypto_aml_pool_scope_hold() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE wallet UUID;
BEGIN
 IF NEW.enabled THEN
  FOR wallet IN SELECT v.wallet_id FROM reg_crypto_aml_pool_intents i JOIN sys_crypto_aml_pool_policies p ON p.id=i.policy_id AND p.wallet_role='merchant_pool'
   JOIN reg_crypto_aml_wallet_provisions v ON v.id=i.id AND v.status='completed'
   WHERE p.merchant_id=NEW.merchant_id AND p.network_id=NEW.network_id
   AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_baselines WHERE wallet_id=v.wallet_id AND token_id=NEW.token_id) ORDER BY v.wallet_id LOOP
   PERFORM fn_crypto_hold_aml_pool_wallet(wallet,NEW.token_id,'aml_pool_scope_baseline',NEW.version);
  END LOOP;
 END IF;
 RETURN NEW;
END; $$;

CREATE OR REPLACE FUNCTION fn_crypto_aml_pool_high_hold() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.risk_class='high' AND EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_provisions v JOIN reg_crypto_aml_pool_intents i ON i.id=v.id
  JOIN sys_crypto_aml_pool_policies p ON p.id=i.policy_id AND p.wallet_role='merchant_pool' WHERE v.wallet_id=NEW.wallet_id AND v.status='completed') THEN
  PERFORM fn_crypto_hold_aml_pool_wallet(NEW.wallet_id,NEW.token_id,'aml_pool_risk',NEW.revision);
 END IF;
 RETURN NEW;
END; $$;

CREATE FUNCTION fn_crypto_aml_pool_policy_guard() RETURNS TRIGGER AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'merchant pool policies cannot be deleted' USING ERRCODE='23514'; END IF;
 PERFORM 1 FROM sys_crypto_custody_policies WHERE network_id=NEW.network_id AND enforced FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'merchant pools require enforced custody' USING ERRCODE='23514'; END IF;
 IF TG_OP='UPDATE' THEN
   IF (NEW.id,NEW.merchant_id,NEW.network_id,NEW.wallet_role,NEW.token_id,NEW.risk_class,NEW.created_at) IS DISTINCT FROM (OLD.id,OLD.merchant_id,OLD.network_id,OLD.wallet_role,OLD.token_id,OLD.risk_class,OLD.created_at) THEN
     RAISE EXCEPTION 'merchant pool scope is immutable' USING ERRCODE='23514';
   END IF;
   IF (NEW.enabled,NEW.low_watermark,NEW.target_ready,NEW.max_total,NEW.max_provision_batch,NEW.configured_by) IS NOT DISTINCT FROM (OLD.enabled,OLD.low_watermark,OLD.target_ready,OLD.max_total,OLD.max_provision_batch,OLD.configured_by) AND NEW.version=OLD.version THEN RETURN NEW; END IF;
   IF NEW.version<>OLD.version+1 THEN RAISE EXCEPTION 'merchant pool policy requires next version' USING ERRCODE='23514'; END IF;
 ELSIF NEW.version<>1 THEN RAISE EXCEPTION 'merchant pool policy must start at version one' USING ERRCODE='23514'; END IF;
 IF NEW.wallet_role='liquidity_hot' AND NOT EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes s JOIN sys_crypto_aml_batch_policies b ON b.merchant_id=s.merchant_id AND b.network_id=s.network_id AND b.token_id=s.token_id AND b.risk_class=NEW.risk_class
    WHERE s.merchant_id=NEW.merchant_id AND s.network_id=NEW.network_id AND s.token_id=NEW.token_id AND s.enabled) THEN
   RAISE EXCEPTION 'collector pool requires enabled AML scope and batch policy' USING ERRCODE='23514';
 END IF;
 IF NEW.enabled AND NEW.wallet_role='merchant_pool' AND NOT EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes WHERE merchant_id=NEW.merchant_id AND network_id=NEW.network_id AND enabled) THEN
   RAISE EXCEPTION 'deposit pool requires an enabled AML asset scope' USING ERRCODE='23514';
 END IF;
 IF NEW.enabled AND (NOT EXISTS(SELECT 1 FROM sys_crypto_aml_provisioning_identity WHERE id AND database_name=current_database())
   OR NOT EXISTS(SELECT 1 FROM sys_crypto_wallet_pool_policies p JOIN sys_crypto_wallet_derivation_cursors cursor ON cursor.network_id=p.network_id
     JOIN cat_blockchain_networks n ON n.id=p.network_id AND n.is_active AND NOT n.deletion_mark AND n.chain_family IN ('tron','evm')
     WHERE p.network_id=NEW.network_id AND p.enabled AND NOT p.needs_review AND NOT cursor.needs_review AND p.derivation_namespace<>'' AND p.derivation_namespace=cursor.derivation_namespace)) THEN
   RAISE EXCEPTION 'merchant pool requires enrolled database and ready network derivation policy' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_crypto_aml_pool_policy_audit() RETURNS TRIGGER AS $$
BEGIN
 IF TG_OP='INSERT' OR NEW.version<>OLD.version THEN
   INSERT INTO reg_crypto_aml_pool_policy_versions(policy_id,version,enabled,low_watermark,target_ready,max_total,max_provision_batch,configured_by,automatic_baseline)
     VALUES(NEW.id,NEW.version,NEW.enabled,NEW.low_watermark,NEW.target_ready,NEW.max_total,NEW.max_provision_batch,NEW.configured_by,NEW.automatic_baseline);
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_pool_intent_guard() RETURNS TRIGGER AS $$
DECLARE policy sys_crypto_aml_pool_policies; usage RECORD; network_max INT;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'merchant pool intents cannot be deleted' USING ERRCODE='23514'; END IF;
 IF TG_OP='UPDATE' THEN
   IF (NEW.id,NEW.policy_id,NEW.policy_version,NEW.plan_id,NEW.ordinal,NEW.created_at) IS DISTINCT FROM (OLD.id,OLD.policy_id,OLD.policy_version,OLD.plan_id,OLD.ordinal,OLD.created_at) OR NEW.batch_attempts<OLD.batch_attempts THEN
     RAISE EXCEPTION 'merchant pool intent identity is immutable' USING ERRCODE='23514';
   END IF;
   RETURN NEW;
 END IF;
 PERFORM 1 FROM sys_crypto_aml_pool_policies p JOIN sys_crypto_custody_policies c ON c.network_id=p.network_id AND c.enforced WHERE p.id=NEW.policy_id FOR UPDATE OF c;
 SELECT * INTO policy FROM sys_crypto_aml_pool_policies WHERE id=NEW.policy_id FOR UPDATE;
 SELECT * INTO usage FROM fn_crypto_aml_pool_counts(NEW.policy_id);
 SELECT max_total_wallets INTO network_max FROM sys_crypto_wallet_pool_policies WHERE network_id=policy.network_id AND enabled AND NOT needs_review FOR UPDATE;
 IF NOT EXISTS(SELECT 1 FROM crypto_aml_pool_runtime WHERE id=policy.id) OR policy.version<>NEW.policy_version OR NEW.ordinal>policy.max_provision_batch OR usage.ready>policy.low_watermark
    OR usage.ready+usage.pending+usage.queued>=policy.target_ready OR usage.total>=policy.max_total OR network_max IS NULL
    OR fn_crypto_aml_network_wallet_commitments(policy.network_id,NULL)>=network_max THEN
   RAISE EXCEPTION 'merchant pool intent exceeds readiness, policy or network budget' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- An intent has one immutable cancellation command, including its first actor.
-- Compare before ON CONFLICT can hide a different command or skip constraints.
CREATE OR REPLACE FUNCTION fn_crypto_aml_pool_cancel_guard() RETURNS TRIGGER AS $$
DECLARE
 network UUID;
 prior reg_crypto_aml_pool_intent_cancellations%ROWTYPE;
BEGIN
 IF current_setting('transaction_isolation')<>'read committed' THEN
  RAISE EXCEPTION 'pool cancellation requires read committed isolation' USING ERRCODE='23514';
 END IF;
 IF NEW.intent_id IS NULL OR NEW.reason IS NULL OR length(btrim(NEW.reason)) NOT BETWEEN 3 AND 500
    OR NEW.cancelled_by IS NULL OR btrim(NEW.cancelled_by)='' OR NEW._deleted_at IS NOT NULL THEN
  RAISE EXCEPTION 'pool cancellation requires intent, reason and actor' USING ERRCODE='23514';
 END IF;
 SELECT p.network_id INTO network FROM reg_crypto_aml_pool_intents i JOIN sys_crypto_aml_pool_policies p ON p.id=i.policy_id WHERE i.id=NEW.intent_id;
 IF NOT FOUND THEN RAISE EXCEPTION 'pool cancellation intent is missing' USING ERRCODE='23503'; END IF;
 PERFORM 1 FROM sys_crypto_custody_policies WHERE network_id=network FOR UPDATE;
 PERFORM 1 FROM sys_crypto_aml_routing_scopes WHERE network_id=network ORDER BY merchant_id,network_id,token_id FOR UPDATE;
 PERFORM 1 FROM reg_crypto_aml_pool_intents WHERE id=NEW.intent_id FOR UPDATE;
 SELECT * INTO prior FROM reg_crypto_aml_pool_intent_cancellations WHERE intent_id=NEW.intent_id;
 IF FOUND THEN
  IF prior.reason IS DISTINCT FROM NEW.reason OR prior.cancelled_by IS DISTINCT FROM NEW.cancelled_by THEN
   RAISE EXCEPTION 'pool cancellation replay differs from its original command' USING ERRCODE='23514';
  END IF;
  RETURN NULL;
 END IF;
 IF EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_provisions WHERE id=NEW.intent_id) THEN
  RAISE EXCEPTION 'cannot cancel pool intent after derivation reservation' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_pool_preflight_policy_guard() RETURNS TRIGGER AS $$
BEGIN
 IF NEW.automatic_baseline IS DISTINCT FROM OLD.automatic_baseline AND NEW.version<>OLD.version+1 THEN
   RAISE EXCEPTION 'automatic baseline change requires next policy version' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Coverage is a first observation, retained across later address refreshes and
-- watcher leases. Check authority after any lock wait using the database clock.
CREATE OR REPLACE FUNCTION fn_crypto_aml_watch_coverage_guard() RETURNS TRIGGER AS $$
BEGIN
 PERFORM 1 FROM sys_chain_watcher_state state JOIN cat_wallets w ON w.id=NEW.wallet_id AND w.network_id=state.network_id
 JOIN cat_tokens t ON t.id=NEW.token_id AND t.network_id=w.network_id JOIN cat_blockchain_networks n ON n.id=w.network_id AND n.chain_family='evm'
 WHERE state.network_id=NEW.network_id AND state.watcher_key=NEW.watcher_key
 AND NEW.watcher_key=CASE WHEN t.token_standard='native' THEN 'native_eth' ELSE 'erc20:'||LOWER(t.contract_address) END FOR SHARE OF state;
 IF NOT FOUND THEN RAISE EXCEPTION 'address coverage watcher or asset changed' USING ERRCODE='23514'; END IF;
 IF NOT EXISTS(SELECT 1 FROM sys_chain_watcher_state WHERE network_id=NEW.network_id AND watcher_key=NEW.watcher_key
   AND lease_owner=NEW.lease_owner AND fencing_token=NEW.fencing_token AND lease_expires_at>clock_timestamp()) THEN
  RAISE EXCEPTION 'address coverage requires the current watcher lease' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- The baseline cannot erase any observed physical or financial history. The
-- watcher must already cover this checkpoint; later inbox arrivals stay visible.
CREATE FUNCTION fn_crypto_aml_automatic_baseline_guard() RETURNS TRIGGER AS $$
BEGIN
 IF NEW.verified_by='aml-preflight' AND NOT EXISTS(
   SELECT 1 FROM reg_crypto_aml_preflight_observations o JOIN reg_crypto_aml_preflight_jobs j ON j.id=o.job_id
   JOIN sys_crypto_aml_pool_policies p ON p.id=j.policy_id AND p.enabled AND p.automatic_baseline AND p.version=j.policy_version
   JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=p.merchant_id AND s.network_id=p.network_id AND s.token_id=j.token_id AND s.enabled AND s.version=j.scope_version
   JOIN reg_crypto_aml_wallet_provisions v ON v.wallet_id=j.wallet_id AND v.status='completed'
   JOIN reg_crypto_aml_pool_intents i ON i.id=v.id AND i.policy_id=p.id JOIN cat_wallets w ON w.id=v.wallet_id AND w.is_active AND NOT w.deletion_mark AND w.custody_merchant_id=p.merchant_id AND w.wallet_role=p.wallet_role
   JOIN cat_blockchain_networks network ON network.id=w.network_id JOIN cat_tokens asset ON asset.id=j.token_id
   LEFT JOIN sys_crypto_watch_targets watch ON watch.network_id=w.network_id AND watch.target_kind='address' AND watch.owner_id=w.id AND watch._deleted_at IS NULL AND watch.start_height<=o.block_number
   JOIN reg_crypto_reconciliation_checks c ON c.id=NEW.reconciliation_check_id AND c.observed_at=o.observed_at
   WHERE o.id::text=NEW.evidence_reference AND o.wallet_id=NEW.wallet_id AND o.token_id=NEW.token_id AND o.network_id=NEW.network_id
     AND ((network.chain_family='tron' AND watch.owner_id IS NOT NULL) OR (network.chain_family='evm' AND EXISTS(SELECT 1 FROM reg_crypto_aml_watch_coverage coverage WHERE coverage.wallet_id=w.id AND coverage.token_id=j.token_id AND coverage.network_id=w.network_id AND coverage.start_height<=o.block_number AND coverage.watcher_key=CASE WHEN asset.token_standard='native' THEN 'native_eth' ELSE 'erc20:'||LOWER(asset.contract_address) END)))
     AND o.block_number=NEW.block_number AND o.block_hash=NEW.block_hash AND o.chain_amount=0
     AND o.observed_at BETWEEN NOW()-interval '1 minute' AND NOW() AND j.status='processing' AND j.lease_token=o.lease_token AND j.lease_expires_at>NOW()
     AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_effects WHERE wallet_id=w.id AND token_id=j.token_id)
     AND NOT EXISTS(SELECT 1 FROM reg_crypto_balance_movements WHERE wallet_id=w.id AND token_id=j.token_id)
     AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_preflight_observations previous WHERE previous.wallet_id=w.id AND previous.token_id=j.token_id AND previous.chain_amount<>0)
 ) THEN RAISE EXCEPTION 'automatic AML baseline requires leased independent empty checkpoint and no prior inventory' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_preflight_observation_guard() RETURNS TRIGGER AS $$
DECLARE w cat_wallets; asset cat_tokens; source_a JSONB; source_b JSONB; family TEXT;
BEGIN
 SELECT * INTO STRICT w FROM cat_wallets WHERE id=NEW.wallet_id;
 SELECT * INTO STRICT asset FROM cat_tokens WHERE id=NEW.token_id AND network_id=NEW.network_id;
 SELECT chain_family INTO STRICT family FROM cat_blockchain_networks WHERE id=NEW.network_id;
 IF NOT EXISTS(SELECT 1 FROM reg_crypto_aml_preflight_jobs j JOIN sys_crypto_aml_pool_policies p ON p.id=j.policy_id AND p.enabled AND p.automatic_baseline AND p.version=j.policy_version
   WHERE j.id=NEW.job_id AND j.wallet_id=NEW.wallet_id AND j.token_id=NEW.token_id AND j.status='processing' AND j.lease_token=NEW.lease_token AND j.lease_expires_at>NOW() AND p.network_id=NEW.network_id AND p.merchant_id=w.custody_merchant_id) THEN RAISE EXCEPTION 'preflight observation requires current lease and custody policy' USING ERRCODE='23514'; END IF;
 source_a:=NEW.proof->'sources'->0;source_b:=NEW.proof->'sources'->1;
 IF (jsonb_typeof(NEW.proof->'sources')='array' AND jsonb_array_length(NEW.proof->'sources')=2
   AND NEW.proof->>'networkId'=NEW.network_id::text AND NEW.proof->'asset'->>'tokenId'=NEW.token_id::text
   AND NEW.proof->'asset'->>'standard'=asset.token_standard AND (NEW.proof->'asset'->>'decimals')::int=asset.decimal_places
   AND CASE WHEN family='evm' THEN LOWER(COALESCE(NEW.proof->'asset'->>'contractAddress',''))=LOWER(COALESCE(asset.contract_address,'')) ELSE COALESCE(NEW.proof->'asset'->>'contractAddress','')=COALESCE(asset.contract_address,'') END
   AND NEW.proof->>'address'=w.address AND (NEW.proof->>'height')::bigint=NEW.block_number AND NEW.proof->>'hash'=NEW.block_hash
   AND ABS(EXTRACT(EPOCH FROM ((NEW.proof->>'observedAt')::timestamptz-NEW.observed_at)))<=5.000001
   AND (NEW.proof->>'observedAt')::timestamptz BETWEEN NOW()-interval '1 minute' AND NOW()+interval '5 seconds'
   AND NEW.observed_at BETWEEN NOW()-interval '1 minute' AND NOW()
   AND (source_a->>'height')::bigint=NEW.block_number AND (source_b->>'height')::bigint=NEW.block_number
   AND LOWER(source_a->>'hash')=NEW.block_hash AND LOWER(source_b->>'hash')=NEW.block_hash
   AND source_a->>'balance'=NEW.chain_amount::text AND source_b->>'balance'=NEW.chain_amount::text
   AND btrim(source_a->>'providerId')<>'' AND btrim(source_b->>'providerId')<>'' AND LOWER(btrim(source_a->>'providerId'))<>LOWER(btrim(source_b->>'providerId'))
   AND btrim(source_a->>'trustGroup')<>'' AND btrim(source_b->>'trustGroup')<>'' AND LOWER(btrim(source_a->>'trustGroup'))<>LOWER(btrim(source_b->>'trustGroup'))
   AND source_a->>'method'=CASE WHEN family='tron' THEN 'solidified_bracket' ELSE 'finalized_block_bracket' END
   AND source_b->>'method'=source_a->>'method') IS NOT TRUE THEN RAISE EXCEPTION 'preflight requires two independent pinned final state observations' USING ERRCODE='23514'; END IF;
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

CREATE FUNCTION fn_crypto_aml_withdrawal_fact_apply() RETURNS TRIGGER AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM crypto_aml_valid_withdrawal_facts WHERE line_id=NEW.line_id) THEN RAISE EXCEPTION 'withdrawal fact requires exact final chain, signing and physical evidence' USING ERRCODE='23514'; END IF;
 UPDATE reg_crypto_aml_inventory_positions SET principal=principal-CASE WHEN NEW.succeeded THEN NEW.amount ELSE 0 END,revision=revision+1 WHERE wallet_id=NEW.wallet_id AND token_id=NEW.token_id;
 UPDATE reg_crypto_aml_withdrawal_lines SET status=CASE WHEN NEW.succeeded THEN 'confirmed' ELSE 'failed' END,lease_token=NULL,lease_expires_at=NULL WHERE id=NEW.line_id;
 PERFORM fn_crypto_aml_invalidate_wallet(NEW.wallet_id,NEW.token_id,'withdrawal_settled');
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

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

CREATE FUNCTION fn_crypto_aml_is_emulation_network(family TEXT,identifier TEXT) RETURNS BOOLEAN AS $$
 SELECT (family='tron' AND identifier='nile') OR (family='evm' AND identifier='11155111');
$$ LANGUAGE SQL IMMUTABLE;

CREATE FUNCTION fn_crypto_aml_testnet_binding_guard() RETURNS TRIGGER AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM cat_integration_accounts a JOIN cat_blockchain_networks n ON n.id=NEW.network_id
   WHERE a.id=NEW.account_id AND a.provider='testnet_aml' AND fn_crypto_aml_is_emulation_network(n.chain_family,n.network_identifier)) THEN
   RAISE EXCEPTION 'AML emulation is restricted to TRON Nile and Ethereum Sepolia' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_testnet_identity_guard() RETURNS TRIGGER AS $$
BEGIN
 IF TG_TABLE_NAME='cat_blockchain_networks' THEN
   IF (NEW.chain_family,NEW.network_identifier) IS DISTINCT FROM (OLD.chain_family,OLD.network_identifier) AND EXISTS(SELECT 1 FROM sys_crypto_aml_testnet_emulators WHERE network_id=OLD.id) THEN
     RAISE EXCEPTION 'emulated network identity is immutable' USING ERRCODE='23514';
   END IF;
 ELSE
   IF NEW.provider<>OLD.provider AND (EXISTS(SELECT 1 FROM sys_crypto_aml_testnet_emulators WHERE account_id=OLD.id) OR EXISTS(SELECT 1 FROM reg_crypto_aml_jobs WHERE provider_account_id=OLD.id)) THEN
     RAISE EXCEPTION 'pinned AML account provider is immutable' USING ERRCODE='23514';
   END IF;
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_testnet_mapping_guard() RETURNS TRIGGER AS $$
BEGIN
 IF NEW.contract_version='aml-testnet.v1' AND NOT EXISTS(SELECT 1 FROM cat_blockchain_networks n WHERE n.id=NEW.network_id AND fn_crypto_aml_is_emulation_network(n.chain_family,n.network_identifier) AND NEW.currencylist_snapshot->'emulated'='true'::jsonb) THEN
   RAISE EXCEPTION 'synthetic AML mappings are restricted to Nile/Sepolia' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_job_provider_guard() RETURNS TRIGGER AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM cat_integration_accounts WHERE id=NEW.provider_account_id AND provider=NEW.provider) THEN RAISE EXCEPTION 'AML job account/provider mismatch' USING ERRCODE='23514'; END IF;
 IF NEW.provider='testnet_aml' AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_subjects s JOIN sys_crypto_aml_testnet_emulators e ON e.account_id=NEW.provider_account_id AND e.network_id=s.network_id
    JOIN cat_blockchain_networks n ON n.id=s.network_id JOIN sys_crypto_aml_provider_mappings m ON m.network_id=s.network_id AND m.token_id=s.token_id AND m.version=NEW.mapping_version
    WHERE s.id=NEW.subject_id AND fn_crypto_aml_is_emulation_network(n.chain_family,n.network_identifier) AND m.contract_version='aml-testnet.v1') THEN RAISE EXCEPTION 'emulated job requires its pinned testnet mapping' USING ERRCODE='23514'; END IF;
 IF NEW.provider='getblock_net' AND EXISTS(SELECT 1 FROM reg_crypto_aml_subjects s JOIN sys_crypto_aml_provider_mappings m ON m.network_id=s.network_id AND m.token_id=s.token_id AND m.version=NEW.mapping_version WHERE s.id=NEW.subject_id AND m.contract_version='aml-testnet.v1') THEN RAISE EXCEPTION 'synthetic mapping cannot create paid live jobs' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_check_provider_guard() RETURNS TRIGGER AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM reg_crypto_aml_jobs j WHERE j.id=NEW.job_id AND j.subject_id=NEW.subject_id AND j.provider=NEW.provider AND j.provider_account_id=NEW.provider_account_id AND j.request_digest=NEW.request_digest) THEN
   RAISE EXCEPTION 'AML provider check must match its pinned job' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_crypto_aml_report_emulation_guard() RETURNS TRIGGER AS $$
DECLARE provider_name TEXT;
BEGIN
 SELECT c.provider INTO STRICT provider_name FROM reg_crypto_aml_provider_checks c JOIN reg_crypto_aml_jobs j ON j.id=c.job_id AND j.provider=c.provider AND j.provider_account_id=c.provider_account_id WHERE c.id=NEW.provider_check_id;
 IF provider_name='testnet_aml' THEN
   IF NEW.contract_version<>'aml-testnet.v1' OR NEW.adapter_version<>'testnet-aml.v1' OR (convert_from(NEW.raw_response,'UTF8')::jsonb)->'emulated' IS DISTINCT FROM 'true'::jsonb THEN
     RAISE EXCEPTION 'emulated AML reports must retain their explicit provenance' USING ERRCODE='23514';
   END IF;
 ELSIF NEW.contract_version='aml-testnet.v1' OR NEW.adapter_version='testnet-aml.v1' THEN RAISE EXCEPTION 'emulated report cannot satisfy a live provider check' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

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

CREATE FUNCTION fn_crypto_aml_testnet_bootstrap_complete() RETURNS TRIGGER AS $$
BEGIN
 IF NOT (SELECT NOT EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes s LEFT JOIN reg_crypto_merchant_balance_balances b ON b.merchant_id=s.merchant_id AND b.token_id=s.token_id WHERE s.merchant_id=NEW.merchant_id AND s.network_id=NEW.network_id AND s.enabled AND (COALESCE(b.amount,0)<>COALESCE((SELECT SUM(o.principal) FROM reg_crypto_aml_testnet_openings o WHERE o.bootstrap_id=NEW.id AND o.token_id=s.token_id),0) OR COALESCE((SELECT SUM(CASE WHEN f.record_type='receipt' THEN f.amount ELSE -f.amount END) FROM reg_crypto_fee_movements f WHERE f.token_id=s.token_id),0)<>COALESCE((SELECT SUM(o.service_fee) FROM reg_crypto_aml_testnet_openings o WHERE o.bootstrap_id=NEW.id AND o.token_id=s.token_id),0))) AND NOT EXISTS(SELECT 1 FROM cat_wallets w JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=NEW.merchant_id AND s.network_id=w.network_id AND s.enabled LEFT JOIN reg_crypto_balance_balances b ON b.wallet_id=w.id AND b.token_id=s.token_id WHERE w.network_id=NEW.network_id AND (w.is_active AND NOT w.deletion_mark OR COALESCE(b.amount,0)<>0) AND (w.custody_merchant_id IS DISTINCT FROM NEW.merchant_id OR NOT EXISTS(SELECT 1 FROM reg_crypto_aml_testnet_openings o WHERE o.bootstrap_id=NEW.id AND o.wallet_id=w.id AND o.token_id=s.token_id)))) OR EXISTS(SELECT 1 FROM reg_crypto_aml_testnet_openings o WHERE o.bootstrap_id=NEW.id AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_baselines b WHERE b.wallet_id=o.wallet_id AND b.token_id=o.token_id AND b.evidence_reference=NEW.id::text)) THEN
 RAISE EXCEPTION 'testnet bootstrap must preserve exact merchant/fee claims and cover every wallet and baseline atomically' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

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


-- ---- Tables ----
CREATE TABLE reg_crypto_aml_subjects (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    merchant_id UUID NOT NULL REFERENCES cat_merchants(id),
    network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    token_id UUID NOT NULL,
    wallet_id UUID NOT NULL,
    subject_kind TEXT NOT NULL CHECK(subject_kind IN ('address','transfer')),
    request_digest TEXT NOT NULL CHECK(request_digest ~ '^[0-9a-f]{64}$'),
    request_payload JSONB NOT NULL CHECK(jsonb_typeof(request_payload)='object'),
    mapping_version TEXT NOT NULL CHECK(mapping_version<>''),
    generation BIGINT NOT NULL DEFAULT 1 CHECK(generation>0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ,
    FOREIGN KEY(wallet_id,network_id,merchant_id)
        REFERENCES cat_wallets(id,network_id,custody_merchant_id),
    FOREIGN KEY(token_id,network_id) REFERENCES cat_tokens(id,network_id),
    UNIQUE(merchant_id,network_id,token_id,request_digest),
    UNIQUE(id,merchant_id,network_id,token_id,wallet_id)
);

CREATE TABLE reg_crypto_aml_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    subject_id UUID NOT NULL REFERENCES reg_crypto_aml_subjects(id),
    generation BIGINT NOT NULL CHECK(generation>0),
    provider TEXT NOT NULL CHECK(provider IN ('getblock_net','testnet_aml')),
    provider_account_id UUID NOT NULL REFERENCES cat_integration_accounts(id),
    request_payload JSONB NOT NULL CHECK(jsonb_typeof(request_payload)='object'),
    request_digest TEXT NOT NULL CHECK(request_digest ~ '^[0-9a-f]{64}$'),
    mapping_version TEXT NOT NULL CHECK(mapping_version<>''),
    submit_request_id TEXT NOT NULL CHECK(length(submit_request_id) BETWEEN 1 AND 32),
    poll_request_id TEXT,
    status TEXT NOT NULL DEFAULT 'pending_submit' CHECK(status IN
        ('pending_submit','submitting','submission_unknown','awaiting_result','polling',
         'retry_wait','succeeded','needs_attention','superseded')),
    ticket JSONB CHECK(ticket IS NULL OR jsonb_typeof(ticket)='object'),
    attempts INT NOT NULL DEFAULT 0 CHECK(attempts>=0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deadline_at TIMESTAMPTZ NOT NULL,
    lease_token UUID,
    lease_expires_at TIMESTAMPTZ,
    last_error_code TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ,
    UNIQUE(subject_id,generation,provider,provider_account_id),
    UNIQUE(id,subject_id),
    CHECK(poll_request_id IS NULL OR length(poll_request_id) BETWEEN 1 AND 32),
    CHECK((lease_token IS NULL)=(lease_expires_at IS NULL)),
    CHECK(status NOT IN ('awaiting_result','polling','succeeded') OR ticket IS NOT NULL),
    CONSTRAINT uq_crypto_aml_job_account UNIQUE(id,provider_account_id),
    CONSTRAINT uq_crypto_aml_job_generation_provider UNIQUE(subject_id,generation,provider)
);
CREATE INDEX idx_crypto_aml_jobs_due ON reg_crypto_aml_jobs(next_attempt_at,id)
    WHERE status IN ('pending_submit','awaiting_result','submitting','polling') AND _deleted_at IS NULL;

CREATE TABLE reg_crypto_aml_provider_checks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    subject_id UUID NOT NULL REFERENCES reg_crypto_aml_subjects(id),
    job_id UUID NOT NULL UNIQUE,
    provider TEXT NOT NULL CHECK(provider IN ('getblock_net','testnet_aml')),
    provider_account_id UUID NOT NULL REFERENCES cat_integration_accounts(id),
    check_hash TEXT NOT NULL CHECK(check_hash<>''),
    request_digest TEXT NOT NULL CHECK(request_digest ~ '^[0-9a-f]{64}$'),
    ticket JSONB NOT NULL CHECK(jsonb_typeof(ticket)='object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ,
    UNIQUE(provider,provider_account_id,check_hash),
    FOREIGN KEY(job_id,subject_id) REFERENCES reg_crypto_aml_jobs(id,subject_id),
    UNIQUE(id,subject_id)
);

CREATE TABLE reg_crypto_aml_reports (
    id UUID PRIMARY KEY,
    subject_id UUID NOT NULL REFERENCES reg_crypto_aml_subjects(id),
    provider_check_id UUID NOT NULL UNIQUE,
    score_bp INT NOT NULL CHECK(score_bp BETWEEN 0 AND 10000),
    coverage_accepted BOOLEAN NOT NULL,
    contract_version TEXT NOT NULL CHECK(contract_version<>''),
    adapter_version TEXT NOT NULL CHECK(adapter_version<>''),
    mapping_version TEXT NOT NULL CHECK(mapping_version<>''),
    request_digest TEXT NOT NULL CHECK(request_digest ~ '^[0-9a-f]{64}$'),
    signals JSONB NOT NULL CHECK(jsonb_typeof(signals)='array'),
    raw_response BYTEA NOT NULL CHECK(octet_length(raw_response)>0),
    response_hash TEXT GENERATED ALWAYS AS (encode(sha256(raw_response),'hex')) STORED,
    completed_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    dataset_as_of TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ,
    FOREIGN KEY(provider_check_id,subject_id) REFERENCES reg_crypto_aml_provider_checks(id,subject_id),
    UNIQUE(id,subject_id)
);

CREATE TABLE reg_crypto_aml_decisions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    subject_id UUID NOT NULL REFERENCES reg_crypto_aml_subjects(id),
    report_id UUID NOT NULL,
    generation BIGINT NOT NULL CHECK(generation>0),
    policy_version TEXT NOT NULL CHECK(policy_version<>''),
    risk_class TEXT NOT NULL CHECK(risk_class IN ('low','medium','high','unknown')),
    reason TEXT NOT NULL CHECK(reason<>''),
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ,
    evaluated_at TIMESTAMPTZ,
    FOREIGN KEY(report_id,subject_id) REFERENCES reg_crypto_aml_reports(id,subject_id),
    UNIQUE(subject_id,generation,report_id,policy_version)
);

CREATE TABLE reg_crypto_wallet_risk_states (
    wallet_id UUID NOT NULL,
    merchant_id UUID NOT NULL REFERENCES cat_merchants(id),
    network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    token_id UUID NOT NULL,
    risk_class TEXT NOT NULL DEFAULT 'unknown' CHECK(risk_class IN ('low','medium','high','unknown')),
    revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
    evaluated_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ,
    scope_version BIGINT NOT NULL DEFAULT 0,
    evidence_hash TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT 'unclassified',
    inventory_covered BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY(wallet_id,token_id),
    FOREIGN KEY(wallet_id,network_id,merchant_id)
        REFERENCES cat_wallets(id,network_id,custody_merchant_id),
    FOREIGN KEY(token_id,network_id) REFERENCES cat_tokens(id,network_id)
);
CREATE INDEX idx_crypto_wallet_risk_scope
    ON reg_crypto_wallet_risk_states(merchant_id,network_id,token_id,risk_class,wallet_id);

CREATE TABLE reg_crypto_withdrawal_risk_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    request_id UUID NOT NULL REFERENCES doc_withdrawal_requests(id),
    stage TEXT NOT NULL CHECK(stage IN ('requested','dispatched','reassessed')),
    generation BIGINT NOT NULL CHECK(generation>0),
    risk_class TEXT NOT NULL CHECK(risk_class IN ('low','medium','high','unknown')),
    intent_digest TEXT NOT NULL CHECK(intent_digest ~ '^[0-9a-f]{64}$'),
    risk_accepted BOOLEAN NOT NULL DEFAULT FALSE,
    evidence JSONB NOT NULL CHECK(jsonb_typeof(evidence)='object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ,
    UNIQUE(request_id,stage,generation),
    CHECK(stage='reassessed' OR risk_class NOT IN ('high','unknown') OR risk_accepted)
);

-- Ownership enforcement is rolled out separately from AML classification.
-- An absent policy remains legacy-compatible until the coordinated switch.
CREATE TABLE sys_crypto_custody_policies (
    network_id UUID PRIMARY KEY REFERENCES cat_blockchain_networks(id),
    enforced BOOLEAN NOT NULL DEFAULT FALSE,
    version BIGINT NOT NULL DEFAULT 1 CHECK(version>0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL)
);

-- A mapping is an accepted, immutable contract snapshot. New vendor contracts
-- receive a new version; old tickets continue using their original version.
CREATE TABLE sys_crypto_aml_provider_mappings (
    network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    token_id UUID NOT NULL,
    version TEXT NOT NULL CHECK(version<>''),
    contract_version TEXT NOT NULL CHECK(contract_version<>''),
    contract_address TEXT NOT NULL DEFAULT '',
    currency TEXT NOT NULL CHECK(currency IN ('TRX','ETH')),
    provider_token_id TEXT NOT NULL DEFAULT '',
    native BOOLEAN NOT NULL,
    decimal_places INT NOT NULL CHECK(decimal_places BETWEEN 0 AND 18),
    capability_accepted BOOLEAN NOT NULL DEFAULT FALSE,
    acceptance_reference TEXT NOT NULL DEFAULT '',
    currencylist_snapshot JSONB NOT NULL CHECK(jsonb_typeof(currencylist_snapshot)='object'),
    pending_statuses TEXT[] NOT NULL DEFAULT '{}',
    failure_statuses TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
    PRIMARY KEY(network_id,token_id,version),
    FOREIGN KEY(token_id,network_id) REFERENCES cat_tokens(id,network_id),
    CHECK((native AND contract_address='' AND provider_token_id='') OR
          (NOT native AND contract_address<>'' AND provider_token_id<>'')),
    CHECK(NOT capability_accepted OR (acceptance_reference<>'' AND currencylist_snapshot<>'{}'::jsonb))
);

CREATE TABLE sys_crypto_aml_account_policies (
    account_id UUID PRIMARY KEY REFERENCES cat_integration_accounts(id),
    allow_submissions BOOLEAN NOT NULL DEFAULT FALSE,
    version BIGINT NOT NULL DEFAULT 1 CHECK(version>0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL)
);

-- Local grants bound paid use of a purchased package. They are deliberately
-- distinct from the vendor's live checks_left/checks_used observation. No
-- calendar or credential rotation can replenish this budget.
CREATE TABLE reg_crypto_aml_package_credits (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    account_id UUID NOT NULL REFERENCES cat_integration_accounts(id),
    units BIGINT NOT NULL CHECK(units>0 AND units<=1000000000),
    external_reference TEXT NOT NULL CHECK(external_reference<>''),
    authorized_by TEXT NOT NULL CHECK(authorized_by<>''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
    UNIQUE(account_id,external_reference)
);

CREATE TABLE reg_crypto_aml_paid_admissions (
    job_id UUID PRIMARY KEY,
    account_id UUID NOT NULL REFERENCES cat_integration_accounts(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
    FOREIGN KEY(job_id,account_id) REFERENCES reg_crypto_aml_jobs(id,provider_account_id)
);
CREATE INDEX idx_crypto_aml_paid_admissions_account ON reg_crypto_aml_paid_admissions(account_id);

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

CREATE TABLE reg_crypto_aml_inventory_queue (
    kind TEXT NOT NULL CHECK(kind IN ('payment','transfer','resource')),
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
    risk_class TEXT NOT NULL CHECK(risk_class IN ('low','medium','high','unknown')),
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

CREATE TABLE reg_crypto_aml_resource_cancellations (
    budget_id UUID PRIMARY KEY REFERENCES reg_crypto_aml_resource_budgets(id),
    reason TEXT NOT NULL CHECK(length(reason) BETWEEN 3 AND 500),
    cancelled_by TEXT NOT NULL CHECK(cancelled_by<>''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL)
);

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

CREATE TABLE sys_crypto_aml_batch_policies (
 merchant_id UUID NOT NULL, network_id UUID NOT NULL, token_id UUID NOT NULL,
 risk_class TEXT NOT NULL CHECK(risk_class IN ('low','medium')),
 version BIGINT NOT NULL CHECK(version>0),
 max_gross NUMERIC(78,0) NOT NULL CHECK(max_gross>0),
 max_age_seconds INT NOT NULL CHECK(max_age_seconds BETWEEN 1 AND 2592000),
 configured_by TEXT NOT NULL CHECK(btrim(configured_by)<>''),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
 PRIMARY KEY(merchant_id,network_id,token_id,risk_class,version),
 FOREIGN KEY(merchant_id,network_id,token_id) REFERENCES sys_crypto_aml_routing_scopes(merchant_id,network_id,token_id)
);

CREATE TABLE reg_crypto_aml_batches (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
 merchant_id UUID NOT NULL, network_id UUID NOT NULL, token_id UUID NOT NULL,
 risk_class TEXT NOT NULL CHECK(risk_class IN ('low','medium')),
 policy_version BIGINT NOT NULL, scope_version BIGINT NOT NULL CHECK(scope_version>0),
 collector_wallet_id UUID NOT NULL UNIQUE,
 max_gross NUMERIC(78,0) NOT NULL CHECK(max_gross>0),
 opened_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), intake_closes_at TIMESTAMPTZ NOT NULL,
 intake_closed_at TIMESTAMPTZ, close_reason TEXT NOT NULL DEFAULT '',
 execution_held BOOLEAN NOT NULL DEFAULT FALSE, hold_reason TEXT NOT NULL DEFAULT '',
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 execution_revision BIGINT NOT NULL DEFAULT 1 CHECK(execution_revision>0),
 request_digest TEXT NOT NULL CHECK(request_digest<>''),
 created_by TEXT NOT NULL CHECK(btrim(created_by)<>''), last_actor TEXT NOT NULL CHECK(btrim(last_actor)<>''),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
 FOREIGN KEY(merchant_id,network_id,token_id,risk_class,policy_version) REFERENCES sys_crypto_aml_batch_policies(merchant_id,network_id,token_id,risk_class,version),
 FOREIGN KEY(collector_wallet_id,network_id,merchant_id) REFERENCES cat_wallets(id,network_id,custody_merchant_id),
 CHECK(intake_closes_at>opened_at),
 CHECK((intake_closed_at IS NULL AND close_reason='') OR (intake_closed_at IS NOT NULL AND close_reason<>'')),
 CHECK(execution_held=(hold_reason<>'')),
 CHECK(NOT execution_held OR intake_closed_at IS NOT NULL)
);
CREATE INDEX idx_crypto_aml_batches_open ON reg_crypto_aml_batches(merchant_id,network_id,token_id,risk_class,intake_closes_at,id) WHERE intake_closed_at IS NULL;

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
    batch_id UUID REFERENCES reg_crypto_aml_batches(id),
    batch_revision BIGINT,
    batch_execution_revision BIGINT,
    FOREIGN KEY(merchant_id,network_id,token_id) REFERENCES sys_crypto_aml_routing_scopes(merchant_id,network_id,token_id),
    FOREIGN KEY(source_wallet_id,network_id,merchant_id) REFERENCES cat_wallets(id,network_id,custody_merchant_id),
    FOREIGN KEY(destination_wallet_id,network_id,merchant_id) REFERENCES cat_wallets(id,network_id,custody_merchant_id),
    CHECK(source_wallet_id<>destination_wallet_id AND principal+service_fee>0),
    CONSTRAINT chk_crypto_aml_allocation_batch CHECK((batch_id IS NULL AND batch_revision IS NULL AND batch_execution_revision IS NULL)
   OR (batch_id IS NOT NULL AND batch_revision>0 AND batch_execution_revision>0))
);
CREATE INDEX idx_crypto_aml_allocation_source ON reg_crypto_aml_internal_allocations(source_wallet_id,token_id) WHERE status='reserved';
CREATE INDEX idx_crypto_aml_allocation_destination ON reg_crypto_aml_internal_allocations(destination_wallet_id,token_id) WHERE status='reserved';
CREATE INDEX idx_crypto_aml_allocation_batch ON reg_crypto_aml_internal_allocations(batch_id,status) WHERE batch_id IS NOT NULL;

CREATE TABLE reg_crypto_aml_allocation_cancellations (
    allocation_id UUID PRIMARY KEY REFERENCES reg_crypto_aml_internal_allocations(id),
    reason TEXT NOT NULL CHECK(length(reason) BETWEEN 3 AND 500),
    cancelled_by TEXT NOT NULL CHECK(cancelled_by<>''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL)
);

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

CREATE TABLE reg_crypto_aml_batch_events (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(), batch_id UUID NOT NULL REFERENCES reg_crypto_aml_batches(id),
 event_kind TEXT NOT NULL CHECK(event_kind IN ('opened','closed','held','reserved','applied','cancelled')),
 allocation_id UUID REFERENCES reg_crypto_aml_internal_allocations(id),
 reason TEXT NOT NULL CHECK(reason<>''), recorded_by TEXT NOT NULL CHECK(btrim(recorded_by)<>''),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
 UNIQUE(batch_id,event_kind,allocation_id)
);

-- Cryptographic database -> Vault namespace binding, not a tenant filter.
CREATE TABLE sys_crypto_aml_provisioning_identity (
 id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(id), vault_namespace UUID NOT NULL,
 database_name TEXT NOT NULL CHECK(database_name<>''), enrolled_by TEXT NOT NULL CHECK(btrim(enrolled_by)<>''),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL)
);

-- Separate control-plane queue. Ordinary wallet-pool workers cannot claim it
-- and do not gain access to Vault's immutable owner enrollment endpoint.
CREATE TABLE reg_crypto_aml_wallet_provisions (
 id UUID PRIMARY KEY,
 merchant_id UUID NOT NULL REFERENCES cat_merchants(id), network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id),
 wallet_role TEXT NOT NULL CHECK(wallet_role IN ('merchant_pool','liquidity_hot','network_fee')),
 derivation_path TEXT NOT NULL,
 request_digest TEXT NOT NULL CHECK(request_digest<>''), requested_by TEXT NOT NULL CHECK(btrim(requested_by)<>''),
 status TEXT NOT NULL DEFAULT 'processing' CHECK(status IN ('processing','retry_wait','completed')),
 attempts INT NOT NULL DEFAULT 1 CHECK(attempts>0), claimed_by TEXT NOT NULL DEFAULT '', claim_token UUID,
 lease_expires_at TIMESTAMPTZ, next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), last_error TEXT NOT NULL DEFAULT '',
 wallet_id UUID UNIQUE REFERENCES cat_wallets(id), enrollment_digest TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), completed_at TIMESTAMPTZ,
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
 UNIQUE(network_id,derivation_path),
 FOREIGN KEY(network_id,derivation_path) REFERENCES sys_crypto_wallet_derivations(network_id,derivation_path),
 CHECK((status='processing' AND claim_token IS NOT NULL AND lease_expires_at IS NOT NULL AND claimed_by<>'') OR (status<>'processing' AND claim_token IS NULL AND lease_expires_at IS NULL AND claimed_by='')),
 CHECK((status='completed' AND wallet_id=id AND enrollment_digest<>'' AND completed_at IS NOT NULL) OR (status<>'completed' AND wallet_id IS NULL AND enrollment_digest='' AND completed_at IS NULL))
);
CREATE UNIQUE INDEX idx_crypto_aml_fee_provision ON reg_crypto_aml_wallet_provisions(merchant_id,network_id) WHERE wallet_role='network_fee' AND status<>'completed';
CREATE INDEX idx_crypto_aml_provision_retry ON reg_crypto_aml_wallet_provisions(next_attempt_at) WHERE status<>'completed';
CREATE INDEX idx_crypto_aml_provision_pending_scope ON reg_crypto_aml_wallet_provisions(merchant_id,network_id,wallet_role) WHERE wallet_id IS NULL;

CREATE TABLE sys_crypto_aml_pool_policies (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(), merchant_id UUID NOT NULL REFERENCES cat_merchants(id),
 network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id),
 wallet_role TEXT NOT NULL CHECK(wallet_role IN ('merchant_pool','liquidity_hot','network_fee')),
 token_id UUID, risk_class TEXT,
 enabled BOOLEAN NOT NULL DEFAULT FALSE, version BIGINT NOT NULL DEFAULT 1 CHECK(version>0),
 low_watermark INT NOT NULL, target_ready INT NOT NULL, max_total INT NOT NULL, max_provision_batch INT NOT NULL,
 configured_by TEXT NOT NULL CHECK(btrim(configured_by)<>''),
 next_reconcile_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), last_reconcile_error TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
    automatic_baseline BOOLEAN NOT NULL DEFAULT FALSE,
 UNIQUE NULLS NOT DISTINCT(merchant_id,network_id,wallet_role,token_id,risk_class),
 FOREIGN KEY(token_id,network_id) REFERENCES cat_tokens(id,network_id),
 CHECK((wallet_role='liquidity_hot' AND token_id IS NOT NULL AND risk_class IN ('low','medium')) OR (wallet_role<>'liquidity_hot' AND token_id IS NULL AND risk_class IS NULL)),
 CHECK(low_watermark>=0 AND low_watermark<target_ready AND target_ready<=max_total AND max_total<=100000 AND max_provision_batch BETWEEN 1 AND 50),
 CHECK(wallet_role<>'network_fee' OR (low_watermark=0 AND target_ready=1 AND max_provision_batch=1))
);
CREATE INDEX idx_crypto_aml_pool_due ON sys_crypto_aml_pool_policies(next_reconcile_at,id) WHERE enabled;

CREATE TABLE reg_crypto_aml_pool_policy_versions (
 policy_id UUID NOT NULL REFERENCES sys_crypto_aml_pool_policies(id), version BIGINT NOT NULL,
 enabled BOOLEAN NOT NULL, low_watermark INT NOT NULL, target_ready INT NOT NULL, max_total INT NOT NULL, max_provision_batch INT NOT NULL,
 configured_by TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
    automatic_baseline BOOLEAN NOT NULL DEFAULT FALSE,
 PRIMARY KEY(policy_id,version)
);

CREATE TABLE reg_crypto_aml_pool_intents (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(), policy_id UUID NOT NULL, policy_version BIGINT NOT NULL,
 plan_id UUID NOT NULL, ordinal INT NOT NULL CHECK(ordinal BETWEEN 1 AND 50),
 next_batch_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), batch_attempts INT NOT NULL DEFAULT 0 CHECK(batch_attempts>=0), batch_error TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
 UNIQUE(policy_id,plan_id,ordinal), FOREIGN KEY(policy_id,policy_version) REFERENCES reg_crypto_aml_pool_policy_versions(policy_id,version)
);
CREATE INDEX idx_crypto_aml_pool_intent_due ON reg_crypto_aml_pool_intents(next_batch_attempt_at,created_at,id);
CREATE INDEX idx_crypto_aml_pool_intent_policy ON reg_crypto_aml_pool_intents(policy_id,id);

CREATE TABLE reg_crypto_aml_pool_intent_cancellations (
 intent_id UUID PRIMARY KEY REFERENCES reg_crypto_aml_pool_intents(id), reason TEXT NOT NULL CHECK(length(btrim(reason)) BETWEEN 3 AND 500),
 cancelled_by TEXT NOT NULL CHECK(btrim(cancelled_by)<>''), created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL)
);

CREATE TABLE reg_crypto_aml_watch_coverage (
 wallet_id UUID NOT NULL REFERENCES cat_wallets(id), token_id UUID NOT NULL REFERENCES cat_tokens(id), network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id),
 watcher_key TEXT NOT NULL CHECK(watcher_key<>''), lease_owner TEXT NOT NULL CHECK(lease_owner<>''), fencing_token BIGINT NOT NULL CHECK(fencing_token>0), start_height BIGINT NOT NULL CHECK(start_height>0), acknowledged_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL), PRIMARY KEY(wallet_id,token_id,watcher_key),
 FOREIGN KEY(token_id,network_id) REFERENCES cat_tokens(id,network_id)
);

CREATE TABLE reg_crypto_aml_preflight_jobs (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(), wallet_id UUID NOT NULL REFERENCES cat_wallets(id), token_id UUID NOT NULL REFERENCES cat_tokens(id),
 policy_id UUID NOT NULL REFERENCES sys_crypto_aml_pool_policies(id), policy_version BIGINT NOT NULL, scope_version BIGINT NOT NULL,
 status TEXT NOT NULL DEFAULT 'retry_wait' CHECK(status IN ('retry_wait','processing','completed','needs_attention')),
 attempts INT NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 120), next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 lease_token UUID, lease_expires_at TIMESTAMPTZ, worker_id TEXT NOT NULL DEFAULT '', error_code TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
    initial_policy_version BIGINT,
    initial_scope_version BIGINT,
 UNIQUE(wallet_id,token_id), FOREIGN KEY(policy_id,policy_version) REFERENCES reg_crypto_aml_pool_policy_versions(policy_id,version),
 CHECK((status='processing')=(lease_token IS NOT NULL AND lease_expires_at IS NOT NULL)),
    FOREIGN KEY(policy_id,initial_policy_version) REFERENCES reg_crypto_aml_pool_policy_versions(policy_id,version)
);
CREATE INDEX idx_crypto_aml_preflight_due ON reg_crypto_aml_preflight_jobs(next_attempt_at,id) WHERE status IN ('retry_wait','processing');

-- Append-only starts linearize risk invalidation and external signing/sending.
-- A start is potential exposure, including an RPC timeout. It is never a release.
CREATE TABLE reg_crypto_aml_dispatch_starts (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(), authorization_id UUID NOT NULL REFERENCES reg_crypto_signing_authorizations(id),
 phase TEXT NOT NULL CHECK(phase IN ('authorize','sign','broadcast')), snapshot JSONB NOT NULL CHECK(jsonb_typeof(snapshot)='object'),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL)
);
CREATE INDEX idx_crypto_aml_dispatch_authorization ON reg_crypto_aml_dispatch_starts(authorization_id,phase,created_at,id);

CREATE TABLE reg_crypto_aml_first_dispatches (
 authorization_id UUID PRIMARY KEY REFERENCES reg_crypto_signing_authorizations(id), start_id UUID NOT NULL UNIQUE REFERENCES reg_crypto_aml_dispatch_starts(id),
 snapshot JSONB NOT NULL CHECK(jsonb_typeof(snapshot)='object'), created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL)
);

CREATE TABLE reg_crypto_aml_withdrawal_plans (
 request_id UUID PRIMARY KEY REFERENCES doc_withdrawal_requests(id), merchant_id UUID NOT NULL REFERENCES cat_merchants(id), network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id), token_id UUID NOT NULL REFERENCES cat_tokens(id),
 risk_class TEXT NOT NULL CHECK(risk_class IN ('low','medium','high','unknown')), risk_accepted BOOLEAN NOT NULL, source_count INT NOT NULL CHECK(source_count BETWEEN 1 AND 1000),
 request_digest TEXT NOT NULL CHECK(request_digest ~ '^[a-f0-9]{64}$'), created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
 CHECK(risk_class NOT IN ('high','unknown') OR risk_accepted), FOREIGN KEY(merchant_id,network_id,token_id) REFERENCES sys_crypto_aml_routing_scopes(merchant_id,network_id,token_id)
);

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

CREATE TABLE reg_crypto_aml_withdrawal_cancellations (
 line_id UUID PRIMARY KEY REFERENCES reg_crypto_aml_withdrawal_lines(id), reason TEXT NOT NULL CHECK(length(btrim(reason)) BETWEEN 3 AND 500), actor TEXT NOT NULL CHECK(btrim(actor)<>''),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL)
);

CREATE TABLE reg_crypto_aml_withdrawal_facts (
 line_id UUID PRIMARY KEY REFERENCES reg_crypto_aml_withdrawal_lines(id), wallet_id UUID NOT NULL REFERENCES cat_wallets(id), token_id UUID NOT NULL REFERENCES cat_tokens(id),
 amount NUMERIC(78,0) NOT NULL CHECK(amount>0), succeeded BOOLEAN NOT NULL, settlement_id UUID NOT NULL UNIQUE REFERENCES reg_crypto_chain_settlements(id),
 expense_movement_id UUID UNIQUE REFERENCES reg_crypto_balance_movements(line_id), refund_movement_id UUID UNIQUE REFERENCES reg_crypto_balance_movements(line_id),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL), CONSTRAINT reg_crypto_aml_withdrawal_facts_check CHECK(
    refund_movement_id IS NULL AND (expense_movement_id IS NOT NULL)=succeeded)
);

CREATE TABLE reg_crypto_aml_withdrawal_events (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(), request_id UUID NOT NULL REFERENCES reg_crypto_aml_withdrawal_plans(request_id), merchant_id UUID NOT NULL REFERENCES cat_merchants(id),
 resource_version INT NOT NULL CHECK(resource_version>0), event_type TEXT NOT NULL CHECK(event_type IN ('withdrawal.confirmed','withdrawal.failed','withdrawal.partially_completed')),
 snapshot JSONB NOT NULL CHECK(jsonb_typeof(snapshot)='object'), created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL), UNIQUE(request_id,resource_version)
);

CREATE TABLE sys_crypto_aml_testnet_emulators (
 account_id UUID PRIMARY KEY REFERENCES cat_integration_accounts(id), network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id),
 seed TEXT NOT NULL CHECK(length(seed) BETWEEN 1 AND 128), max_delay_seconds INT NOT NULL DEFAULT 10 CHECK(max_delay_seconds BETWEEN 0 AND 60),
 unknown_rate_bp INT NOT NULL DEFAULT 1000 CHECK(unknown_rate_bp BETWEEN 0 AND 10000), configured_by TEXT NOT NULL CHECK(btrim(configured_by)<>''),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL)
);
CREATE INDEX idx_crypto_aml_testnet_network ON sys_crypto_aml_testnet_emulators(network_id);

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

CREATE TABLE reg_crypto_aml_authorization_renewals (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(), authorization_id UUID NOT NULL UNIQUE REFERENCES reg_crypto_signing_authorizations(id),
 expires_at TIMESTAMPTZ NOT NULL, reason TEXT NOT NULL CHECK(length(reason) BETWEEN 3 AND 500), authorized_by TEXT NOT NULL CHECK(length(btrim(authorized_by)) BETWEEN 1 AND 100),
 authorization_jws TEXT NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL)
);

CREATE TABLE reg_crypto_aml_preflight_attempts (
 job_id UUID NOT NULL REFERENCES reg_crypto_aml_preflight_jobs(id),
 lease_token UUID PRIMARY KEY, basis JSONB NOT NULL CHECK(jsonb_typeof(basis)='object'),
 worker_id TEXT NOT NULL CHECK(btrim(worker_id)<>''),
 claimed_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), lease_expires_at TIMESTAMPTZ NOT NULL,
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
 UNIQUE(job_id,lease_token)
);

CREATE TABLE reg_crypto_aml_preflight_observations (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(), job_id UUID NOT NULL REFERENCES reg_crypto_aml_preflight_jobs(id), lease_token UUID NOT NULL,
 wallet_id UUID NOT NULL REFERENCES cat_wallets(id), token_id UUID NOT NULL REFERENCES cat_tokens(id), network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id),
 block_number BIGINT NOT NULL CHECK(block_number>=0), block_hash TEXT NOT NULL CHECK(block_hash ~ '^(0x)?[a-f0-9]{64}$'),
 chain_amount NUMERIC(78,0) NOT NULL CHECK(chain_amount>=0), proof JSONB NOT NULL CHECK(jsonb_typeof(proof)='object'),
 observed_at TIMESTAMPTZ NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL), UNIQUE(job_id,lease_token),
    CONSTRAINT fk_crypto_preflight_observation_attempt
 FOREIGN KEY(job_id,lease_token) REFERENCES reg_crypto_aml_preflight_attempts(job_id,lease_token)
);


-- ---- Views ----
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

CREATE VIEW crypto_aml_batch_usage AS
 SELECT b.id,COALESCE(plans.committed,0) committed,COALESCE(plans.reserved,0) reserved,COALESCE(unplanned.amount,0) unplanned,
        COALESCE(plans.committed,0)+COALESCE(plans.reserved,0)+COALESCE(unplanned.amount,0) used
 FROM reg_crypto_aml_batches b
 LEFT JOIN LATERAL(SELECT SUM(gross) FILTER(WHERE status='applied') committed,SUM(gross) FILTER(WHERE status='reserved') reserved
   FROM reg_crypto_aml_internal_allocations WHERE batch_id=b.id) plans ON TRUE
 LEFT JOIN LATERAL(SELECT SUM(e.amount) amount FROM reg_crypto_aml_effects e JOIN reg_crypto_aml_effect_chain_states cs ON cs.effect_id=e.id AND cs.canonicality='canonical'
   WHERE e.wallet_id=b.collector_wallet_id AND e.token_id=b.token_id AND NOT EXISTS(
     SELECT 1 FROM reg_crypto_expected_chain_effects expected JOIN reg_crypto_signing_authorizations auth ON auth.id=expected.signing_authorization_id AND auth.basis_type='CryptoSweepAttempt'
     JOIN reg_crypto_aml_internal_allocations plan ON plan.sweep_attempt_id=auth.basis_id AND plan.batch_id=b.id AND plan.status IN ('reserved','applied')
     WHERE expected.id=e.expected_effect_id AND plan.source_wallet_id=e.source_wallet_id AND plan.destination_wallet_id=e.wallet_id AND plan.token_id=e.token_id AND plan.gross=e.amount)) unplanned ON TRUE;

CREATE VIEW crypto_aml_pool_runtime AS
 SELECT p.id FROM sys_crypto_aml_pool_policies p
 JOIN sys_crypto_custody_policies custody ON custody.network_id=p.network_id AND custody.enforced
 JOIN cat_blockchain_networks n ON n.id=p.network_id AND n.is_active AND NOT n.deletion_mark AND n.chain_family IN ('tron','evm')
 JOIN sys_crypto_wallet_pool_policies base ON base.network_id=p.network_id AND base.enabled AND NOT base.needs_review AND base.lease_blocked_reason=''
 JOIN sys_crypto_wallet_derivation_cursors cursor ON cursor.network_id=p.network_id AND NOT cursor.needs_review AND cursor.derivation_namespace=base.derivation_namespace AND cursor.derivation_namespace<>''
 WHERE p.enabled AND EXISTS(SELECT 1 FROM sys_crypto_aml_provisioning_identity WHERE id AND database_name=current_database())
   AND NOT EXISTS(SELECT 1 FROM reg_crypto_network_holds hold WHERE hold.network_id=p.network_id AND hold.status='open');


-- ---- Triggers ----
CREATE TRIGGER trg_crypto_wallet_custody_owner_guard
    BEFORE INSERT OR UPDATE OR DELETE ON cat_wallets FOR EACH ROW EXECUTE FUNCTION fn_crypto_wallet_custody_owner_guard();

CREATE TRIGGER trg_crypto_aml_checks_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_provider_checks
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_reports_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_reports
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_decisions_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_decisions
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_withdrawal_risk_snapshots_immutable BEFORE UPDATE OR DELETE ON reg_crypto_withdrawal_risk_snapshots
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_job_identity_guard BEFORE UPDATE ON reg_crypto_aml_jobs
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_job_identity_guard();

CREATE TRIGGER trg_crypto_aml_jobs_no_delete BEFORE DELETE ON reg_crypto_aml_jobs
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_subject_identity_guard BEFORE UPDATE ON reg_crypto_aml_subjects
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_subject_identity_guard();

CREATE TRIGGER trg_crypto_aml_subjects_no_delete BEFORE DELETE ON reg_crypto_aml_subjects
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_subjects_updated BEFORE UPDATE ON reg_crypto_aml_subjects
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_crypto_aml_subjects_txid BEFORE UPDATE ON reg_crypto_aml_subjects
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_crypto_aml_jobs_updated BEFORE UPDATE ON reg_crypto_aml_jobs
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_crypto_aml_jobs_txid BEFORE UPDATE ON reg_crypto_aml_jobs
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_crypto_wallet_risk_updated BEFORE UPDATE ON reg_crypto_wallet_risk_states
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_crypto_wallet_risk_txid BEFORE UPDATE ON reg_crypto_wallet_risk_states
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_crypto_custody_policy_guard BEFORE INSERT OR UPDATE OR DELETE ON sys_crypto_custody_policies
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_policy_guard();

CREATE TRIGGER trg_crypto_custody_policy_updated BEFORE UPDATE ON sys_crypto_custody_policies
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_crypto_custody_policy_txid BEFORE UPDATE ON sys_crypto_custody_policies
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_crypto_aml_mapping_scope_guard BEFORE INSERT ON sys_crypto_aml_provider_mappings
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_mapping_scope_guard();

CREATE TRIGGER trg_crypto_aml_account_policy_guard BEFORE INSERT OR UPDATE ON sys_crypto_aml_account_policies
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_runtime_account_guard();

CREATE TRIGGER trg_crypto_aml_credit_account_guard BEFORE INSERT ON reg_crypto_aml_package_credits
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_runtime_account_guard();

CREATE TRIGGER trg_crypto_aml_admission_account_guard BEFORE INSERT ON reg_crypto_aml_paid_admissions
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_runtime_account_guard();

CREATE TRIGGER trg_crypto_aml_mapping_immutable BEFORE UPDATE OR DELETE ON sys_crypto_aml_provider_mappings
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_credit_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_package_credits
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_admission_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_paid_admissions
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_account_policy_updated BEFORE UPDATE ON sys_crypto_aml_account_policies
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_crypto_aml_account_policy_txid BEFORE UPDATE ON sys_crypto_aml_account_policies
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_crypto_aml_routing_scope_guard BEFORE INSERT OR UPDATE OR DELETE ON sys_crypto_aml_routing_scopes
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_routing_scope_guard();

CREATE TRIGGER trg_crypto_aml_routing_scope_updated BEFORE UPDATE ON sys_crypto_aml_routing_scopes
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_crypto_aml_routing_scope_txid BEFORE UPDATE ON sys_crypto_aml_routing_scopes
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_crypto_aml_risk_policy_immutable BEFORE UPDATE OR DELETE ON sys_crypto_aml_risk_policies
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_baseline_guard BEFORE INSERT ON reg_crypto_aml_wallet_baselines
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_baseline_guard();

CREATE TRIGGER trg_crypto_aml_baseline_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_wallet_baselines
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_effect_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_effects
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_effect_checks_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_effect_checks
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_rescreen_request_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_rescreen_requests
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_risk_changes_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_risk_changes
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_balance_risk_guard AFTER INSERT OR UPDATE OR DELETE ON reg_crypto_balance_movements
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_balance_risk_guard();

CREATE TRIGGER trg_crypto_aml_withdrawal_contract_guard BEFORE INSERT OR UPDATE ON doc_withdrawal_requests
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_withdrawal_contract_guard();

CREATE TRIGGER trg_crypto_aml_effect_subject_guard BEFORE INSERT ON reg_crypto_aml_effect_checks
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_effect_subject_guard();

CREATE TRIGGER trg_crypto_aml_baseline_check_guard BEFORE UPDATE OR DELETE ON reg_crypto_reconciliation_checks
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_baseline_check_guard();

CREATE TRIGGER trg_crypto_aml_effect_proof_guard BEFORE INSERT ON reg_crypto_aml_effects
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_effect_proof_guard();

CREATE TRIGGER trg_crypto_aml_scope_changed AFTER INSERT OR UPDATE ON sys_crypto_aml_routing_scopes
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_scope_changed();

CREATE TRIGGER trg_crypto_aml_token_identity_guard BEFORE UPDATE ON cat_tokens
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_token_identity_guard();

CREATE TRIGGER trg_crypto_aml_balance_projection_guard AFTER INSERT OR UPDATE OR DELETE ON reg_crypto_balance_balances
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_balance_projection_guard();

CREATE TRIGGER trg_crypto_aml_inventory_updated BEFORE UPDATE ON reg_crypto_aml_inventory_positions FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_crypto_aml_inventory_txid BEFORE UPDATE ON reg_crypto_aml_inventory_positions FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_crypto_aml_allocation_updated BEFORE UPDATE ON reg_crypto_aml_internal_allocations FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_crypto_aml_allocation_txid BEFORE UPDATE ON reg_crypto_aml_internal_allocations FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_crypto_aml_allocation_cancellation_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_allocation_cancellations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_inventory_fact_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_inventory_facts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_allocation_guard BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_aml_internal_allocations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_allocation_guard();

CREATE TRIGGER trg_crypto_aml_inventory_fact_guard BEFORE INSERT ON reg_crypto_aml_inventory_facts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_inventory_fact_guard();

CREATE TRIGGER trg_crypto_aml_inventory_apply AFTER INSERT ON reg_crypto_aml_inventory_facts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_inventory_apply();

CREATE TRIGGER trg_crypto_aml_allocation_revision AFTER INSERT OR UPDATE ON reg_crypto_aml_internal_allocations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_allocation_revision();

CREATE TRIGGER trg_crypto_aml_allocated_attempt_guard BEFORE UPDATE ON reg_crypto_sweep_attempts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_allocated_attempt_guard();

CREATE TRIGGER trg_crypto_aml_allocation_signing_guard BEFORE INSERT ON reg_crypto_signing_authorizations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_allocation_signing_guard();

CREATE TRIGGER trg_crypto_aml_inventory_physical_queue AFTER INSERT OR UPDATE OR DELETE ON reg_crypto_balance_movements FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_inventory_movement_queue();

CREATE TRIGGER trg_crypto_aml_inventory_merchant_queue AFTER INSERT OR UPDATE OR DELETE ON reg_crypto_merchant_balance_movements FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_inventory_movement_queue();

CREATE TRIGGER trg_crypto_aml_inventory_fee_queue AFTER INSERT OR UPDATE OR DELETE ON reg_crypto_fee_movements FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_inventory_movement_queue();

CREATE TRIGGER trg_crypto_aml_inventory_payment_queue AFTER INSERT OR UPDATE ON doc_crypto_payments FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_inventory_document_queue();

CREATE TRIGGER trg_crypto_aml_inventory_sweep_queue AFTER INSERT OR UPDATE ON doc_crypto_sweeps FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_inventory_document_queue();

CREATE TRIGGER trg_crypto_aml_inventory_baseline AFTER INSERT ON reg_crypto_aml_wallet_baselines FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_inventory_baseline();

CREATE TRIGGER trg_crypto_aml_inventory_scope_queue AFTER INSERT OR UPDATE ON sys_crypto_aml_routing_scopes FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_inventory_scope_queue();

CREATE TRIGGER trg_crypto_aml_inventory_effect_queue AFTER INSERT OR UPDATE ON reg_crypto_aml_effect_chain_states FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_inventory_effect_queue();

CREATE TRIGGER trg_crypto_aml_resource_updated BEFORE UPDATE ON reg_crypto_aml_resource_positions FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_crypto_aml_resource_txid BEFORE UPDATE ON reg_crypto_aml_resource_positions FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_crypto_aml_operating_receipt_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_operating_receipts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_resource_budget_updated BEFORE UPDATE ON reg_crypto_aml_resource_budgets FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_crypto_aml_resource_budget_txid BEFORE UPDATE ON reg_crypto_aml_resource_budgets FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_crypto_aml_resource_cancellation_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_resource_cancellations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_resource_fact_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_resource_facts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_resource_fee_insert_guard BEFORE INSERT ON reg_crypto_fee_reservations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_fee_insert_guard();

CREATE TRIGGER trg_crypto_aml_operating_receipt_guard BEFORE INSERT ON reg_crypto_aml_operating_receipts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_operating_receipt_guard();

CREATE TRIGGER trg_crypto_aml_resource_budget_guard BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_aml_resource_budgets FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_budget_guard();

CREATE TRIGGER trg_crypto_aml_resource_budget_revision AFTER INSERT OR UPDATE ON reg_crypto_aml_resource_budgets FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_budget_revision();

CREATE TRIGGER trg_crypto_aml_resource_fact_guard BEFORE INSERT ON reg_crypto_aml_resource_facts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_fact_guard();

CREATE TRIGGER trg_crypto_aml_resource_fact_apply AFTER INSERT ON reg_crypto_aml_resource_facts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_fact_apply();

CREATE TRIGGER trg_crypto_aml_00_resource_exclusion BEFORE INSERT ON reg_crypto_aml_inventory_facts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_principal_resource_exclusion();

CREATE TRIGGER trg_crypto_aml_resource_baseline AFTER INSERT ON reg_crypto_aml_wallet_baselines FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_baseline();

CREATE TRIGGER trg_crypto_aml_resource_fee_identity BEFORE UPDATE ON reg_crypto_fee_reservations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_basis_guard();

CREATE TRIGGER trg_crypto_aml_resource_funding_identity BEFORE UPDATE ON doc_crypto_native_fundings FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_basis_guard();

CREATE TRIGGER trg_crypto_aml_00_resource_signing_guard BEFORE INSERT ON reg_crypto_signing_authorizations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_signing_guard();

CREATE TRIGGER trg_crypto_aml_resource_fee_queue AFTER INSERT OR UPDATE ON reg_crypto_fee_reservations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_queue_trigger();

CREATE TRIGGER trg_crypto_aml_resource_funding_queue AFTER INSERT OR UPDATE ON doc_crypto_native_fundings FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_queue_trigger();

CREATE TRIGGER trg_crypto_aml_resource_settlement_queue AFTER INSERT ON reg_crypto_chain_settlements FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_queue_trigger();

CREATE TRIGGER trg_crypto_aml_resource_movement_queue AFTER INSERT OR UPDATE ON reg_crypto_balance_movements FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_queue_trigger();

CREATE TRIGGER trg_crypto_aml_resource_effect_queue AFTER INSERT OR UPDATE ON reg_crypto_aml_effect_chain_states FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_queue_trigger();

CREATE TRIGGER trg_crypto_aml_batch_policy_immutable BEFORE UPDATE OR DELETE ON sys_crypto_aml_batch_policies FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_batch_updated BEFORE UPDATE ON reg_crypto_aml_batches FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_crypto_aml_batch_txid BEFORE UPDATE ON reg_crypto_aml_batches FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_crypto_aml_batch_event_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_batch_events FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_batch_policy_guard BEFORE INSERT ON sys_crypto_aml_batch_policies FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_batch_policy_guard();

CREATE TRIGGER trg_crypto_aml_batch_guard BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_aml_batches FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_batch_guard();

CREATE TRIGGER trg_crypto_aml_batch_audit AFTER INSERT OR UPDATE ON reg_crypto_aml_batches FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_batch_audit();

CREATE TRIGGER trg_crypto_aml_00_batch_allocation_guard BEFORE INSERT OR UPDATE ON reg_crypto_aml_internal_allocations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_batch_allocation_guard();

CREATE TRIGGER trg_crypto_aml_batch_allocation_audit AFTER INSERT OR UPDATE ON reg_crypto_aml_internal_allocations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_batch_allocation_audit();

CREATE TRIGGER trg_crypto_aml_01_batch_signing_guard BEFORE INSERT ON reg_crypto_signing_authorizations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_batch_signing_guard();

CREATE TRIGGER trg_crypto_aml_batch_risk AFTER INSERT OR UPDATE ON reg_crypto_wallet_risk_states FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_batch_observation();

CREATE TRIGGER trg_crypto_aml_batch_scope AFTER UPDATE ON sys_crypto_aml_routing_scopes FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_batch_observation();

CREATE TRIGGER trg_crypto_aml_batch_chain AFTER INSERT OR UPDATE ON reg_crypto_aml_effect_chain_states FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_batch_observation();

CREATE TRIGGER trg_crypto_aml_provision_identity_immutable BEFORE UPDATE OR DELETE ON sys_crypto_aml_provisioning_identity FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_provision_updated BEFORE UPDATE ON reg_crypto_aml_wallet_provisions FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_crypto_aml_provision_txid BEFORE UPDATE ON reg_crypto_aml_wallet_provisions FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_crypto_aml_provision_guard BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_aml_wallet_provisions FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_provision_guard();

CREATE TRIGGER trg_crypto_aml_legacy_provision_guard BEFORE INSERT ON cat_wallets FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_legacy_provision_guard();

CREATE TRIGGER trg_crypto_aml_pool_updated BEFORE UPDATE ON sys_crypto_aml_pool_policies FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_crypto_aml_pool_txid BEFORE UPDATE ON sys_crypto_aml_pool_policies FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_crypto_aml_pool_version_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_pool_policy_versions FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_pool_intent_updated BEFORE UPDATE ON reg_crypto_aml_pool_intents FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_crypto_aml_pool_intent_txid BEFORE UPDATE ON reg_crypto_aml_pool_intents FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_crypto_aml_pool_cancel_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_pool_intent_cancellations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_pool_scope_hold AFTER INSERT OR UPDATE ON sys_crypto_aml_routing_scopes FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_pool_scope_hold();

CREATE TRIGGER trg_crypto_aml_pool_high_hold AFTER INSERT OR UPDATE ON reg_crypto_wallet_risk_states FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_pool_high_hold();

CREATE TRIGGER trg_crypto_aml_pool_policy_guard BEFORE INSERT OR UPDATE OR DELETE ON sys_crypto_aml_pool_policies FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_pool_policy_guard();

CREATE TRIGGER trg_crypto_aml_pool_policy_audit AFTER INSERT OR UPDATE ON sys_crypto_aml_pool_policies FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_pool_policy_audit();

CREATE TRIGGER trg_crypto_aml_pool_intent_guard BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_aml_pool_intents FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_pool_intent_guard();

CREATE TRIGGER trg_crypto_aml_pool_cancel_guard BEFORE INSERT ON reg_crypto_aml_pool_intent_cancellations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_pool_cancel_guard();

CREATE TRIGGER trg_crypto_aml_pool_preflight_policy BEFORE UPDATE ON sys_crypto_aml_pool_policies FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_pool_preflight_policy_guard();

CREATE TRIGGER trg_crypto_aml_watch_coverage_guard BEFORE INSERT ON reg_crypto_aml_watch_coverage FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_watch_coverage_guard();

CREATE TRIGGER trg_crypto_aml_watch_coverage_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_watch_coverage FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_preflight_updated BEFORE UPDATE ON reg_crypto_aml_preflight_jobs FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_crypto_aml_preflight_txid BEFORE UPDATE ON reg_crypto_aml_preflight_jobs FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_crypto_aml_preflight_evidence BEFORE UPDATE OR DELETE ON reg_crypto_aml_preflight_observations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_automatic_baseline BEFORE INSERT ON reg_crypto_aml_wallet_baselines FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_automatic_baseline_guard();

CREATE TRIGGER trg_crypto_aml_preflight_observation BEFORE INSERT ON reg_crypto_aml_preflight_observations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_preflight_observation_guard();

CREATE TRIGGER trg_crypto_aml_resource_activation_queue AFTER INSERT OR UPDATE ON doc_crypto_network_operations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_resource_queue_trigger();

CREATE TRIGGER trg_crypto_aml_dispatch_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_dispatch_starts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_first_dispatch_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_first_dispatches FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_withdrawal_plan_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_withdrawal_plans FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_withdrawal_line_updated BEFORE UPDATE ON reg_crypto_aml_withdrawal_lines FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_crypto_aml_withdrawal_line_txid BEFORE UPDATE ON reg_crypto_aml_withdrawal_lines FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_crypto_aml_withdrawal_cancel_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_withdrawal_cancellations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_withdrawal_fact_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_withdrawal_facts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_withdrawal_line_guard BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_aml_withdrawal_lines FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_withdrawal_line_guard();

CREATE TRIGGER trg_crypto_aml_withdrawal_fact_guard BEFORE INSERT ON reg_crypto_aml_withdrawal_facts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_withdrawal_fact_guard();

CREATE TRIGGER trg_crypto_aml_withdrawal_fact_apply AFTER INSERT ON reg_crypto_aml_withdrawal_facts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_withdrawal_fact_apply();

CREATE CONSTRAINT TRIGGER trg_crypto_aml_withdrawal_plan_complete AFTER INSERT ON reg_crypto_aml_withdrawal_plans DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_withdrawal_plan_complete();

CREATE CONSTRAINT TRIGGER trg_crypto_aml_withdrawal_lines_complete AFTER INSERT ON reg_crypto_aml_withdrawal_lines DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_withdrawal_plan_complete();

CREATE TRIGGER trg_crypto_aml_withdrawal_signing_guard BEFORE INSERT ON reg_crypto_signing_authorizations FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_withdrawal_signing_guard();

CREATE TRIGGER trg_crypto_aml_withdrawal_parent_guard BEFORE UPDATE OR DELETE ON doc_withdrawal_requests FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_withdrawal_parent_guard();

CREATE TRIGGER trg_crypto_aml_withdrawal_merchant_movement BEFORE INSERT ON reg_crypto_merchant_balance_movements FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_withdrawal_merchant_movement_guard();

CREATE TRIGGER trg_crypto_aml_withdrawal_event_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_withdrawal_events FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_withdrawal_terminal_event AFTER UPDATE ON doc_withdrawal_requests FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_withdrawal_terminal_event();

CREATE TRIGGER trg_crypto_aml_testnet_binding BEFORE INSERT ON sys_crypto_aml_testnet_emulators FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_testnet_binding_guard();

CREATE TRIGGER trg_crypto_aml_testnet_immutable BEFORE UPDATE OR DELETE ON sys_crypto_aml_testnet_emulators FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_testnet_network BEFORE UPDATE ON cat_blockchain_networks FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_testnet_identity_guard();

CREATE TRIGGER trg_crypto_aml_testnet_account BEFORE UPDATE ON cat_integration_accounts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_testnet_identity_guard();

CREATE TRIGGER trg_crypto_aml_testnet_mapping BEFORE INSERT ON sys_crypto_aml_provider_mappings FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_testnet_mapping_guard();

CREATE TRIGGER trg_crypto_aml_job_provider BEFORE INSERT ON reg_crypto_aml_jobs FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_job_provider_guard();

CREATE TRIGGER trg_crypto_aml_check_provider BEFORE INSERT ON reg_crypto_aml_provider_checks FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_check_provider_guard();

CREATE TRIGGER trg_crypto_aml_report_emulation BEFORE INSERT ON reg_crypto_aml_reports FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_report_emulation_guard();

CREATE TRIGGER trg_crypto_aml_testnet_bootstrap_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_testnet_bootstraps FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_testnet_opening_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_testnet_openings FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

CREATE TRIGGER trg_crypto_aml_testnet_bootstrap_guard BEFORE INSERT ON reg_crypto_aml_testnet_bootstraps FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_testnet_bootstrap_guard();

CREATE TRIGGER trg_crypto_aml_testnet_opening_guard BEFORE INSERT ON reg_crypto_aml_testnet_openings FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_testnet_opening_guard();

CREATE TRIGGER trg_crypto_aml_z_testnet_opening AFTER INSERT ON reg_crypto_aml_wallet_baselines FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_testnet_opening_apply();

CREATE CONSTRAINT TRIGGER trg_crypto_aml_testnet_bootstrap_complete AFTER INSERT ON reg_crypto_aml_testnet_bootstraps DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_testnet_bootstrap_complete();

CREATE TRIGGER trg_crypto_aml_authorization_renewal_txid BEFORE UPDATE ON reg_crypto_aml_authorization_renewals FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_crypto_aml_authorization_renewal_guard BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_aml_authorization_renewals FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_authorization_renewal_guard();

CREATE TRIGGER trg_crypto_aml_preflight_attempt_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_preflight_attempts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();


-- ---- Seed data ----
INSERT INTO reg_crypto_aml_inventory_positions(wallet_id,token_id,merchant_id,network_id)
    SELECT wallet_id,token_id,merchant_id,network_id FROM reg_crypto_aml_wallet_baselines;

INSERT INTO reg_crypto_aml_inventory_queue(kind,basis_id,merchant_id,network_id,token_id)
 SELECT 'payment',p.id,p.merchant_id,p.network_id,p.token_id FROM doc_crypto_payments p JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=p.merchant_id AND s.network_id=p.network_id AND s.token_id=p.token_id AND s.enabled;

INSERT INTO reg_crypto_aml_resource_positions(wallet_id,token_id,merchant_id,network_id)
 SELECT b.wallet_id,b.token_id,b.merchant_id,b.network_id FROM reg_crypto_aml_wallet_baselines b JOIN cat_tokens t ON t.id=b.token_id AND t.token_standard='native';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
-- Recorded evidence is never discarded by a rollback; recover forward instead.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM reg_crypto_aml_authorization_renewals) THEN RAISE EXCEPTION 'cannot roll back authorization renewal history'; END IF;
 IF EXISTS(SELECT 1 FROM reg_crypto_aml_resource_facts f JOIN reg_crypto_aml_testnet_openings o ON o.wallet_id=f.source_wallet_id AND o.token_id=f.token_id WHERE o.operating>0)
 OR EXISTS(SELECT 1 FROM doc_crypto_native_fundings WHERE status='signed') THEN
  RAISE EXCEPTION 'cannot roll back active opening resource or signed funding repair; use a forward migration';
 END IF;
END $$;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM reg_crypto_aml_testnet_bootstraps) THEN RAISE EXCEPTION 'cannot discard testnet opening inventory history; use forward migration'; END IF; END $$;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM sys_crypto_aml_testnet_emulators) OR EXISTS(SELECT 1 FROM cat_integration_accounts WHERE provider='testnet_aml') OR EXISTS(SELECT 1 FROM cat_integration_pools WHERE provider='testnet_aml') OR EXISTS(SELECT 1 FROM reg_crypto_aml_jobs WHERE provider='testnet_aml') THEN RAISE EXCEPTION 'cannot discard AML emulation history; use forward migration'; END IF; END $$;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM reg_crypto_aml_withdrawal_plans) THEN RAISE EXCEPTION 'cannot discard risk withdrawal source history'; END IF; END $$;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM reg_crypto_aml_dispatch_starts) THEN RAISE EXCEPTION 'cannot discard dispatch exposure history'; END IF; END $$;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM reg_crypto_aml_resource_budgets b JOIN reg_crypto_fee_reservations f ON f.id=b.fee_reservation_id WHERE f.purpose='activation') THEN RAISE EXCEPTION 'cannot discard activation resource evidence'; END IF; END $$;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM reg_crypto_aml_preflight_observations) OR EXISTS(SELECT 1 FROM reg_crypto_aml_preflight_jobs) OR EXISTS(SELECT 1 FROM reg_crypto_aml_watch_coverage) THEN RAISE EXCEPTION 'cannot discard AML preflight evidence'; END IF; END $$;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM sys_crypto_aml_pool_policies) OR EXISTS(SELECT 1 FROM reg_crypto_aml_pool_intents) THEN RAISE EXCEPTION 'cannot roll back merchant pool history; use a forward migration'; END IF; END $$;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_provisions) OR EXISTS(SELECT 1 FROM sys_crypto_aml_provisioning_identity) THEN RAISE EXCEPTION 'cannot roll back merchant provisioning history; use a forward migration'; END IF; END $$;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM reg_crypto_aml_batches) OR EXISTS(SELECT 1 FROM sys_crypto_aml_batch_policies) THEN RAISE EXCEPTION 'cannot roll back AML batch evidence; use a forward migration'; END IF; END $$;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM reg_crypto_aml_operating_receipts) OR EXISTS(SELECT 1 FROM reg_crypto_aml_resource_budgets) OR EXISTS(SELECT 1 FROM reg_crypto_aml_resource_facts) THEN
   RAISE EXCEPTION 'cannot roll back AML resource evidence; use a forward migration';
 END IF;
END $$;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM reg_crypto_aml_inventory_facts) OR EXISTS(SELECT 1 FROM reg_crypto_aml_internal_allocations) THEN
   RAISE EXCEPTION 'cannot roll back AML inventory evidence; use a forward migration';
 END IF;
END $$;
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM reg_crypto_aml_effects) OR EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_baselines)
       OR EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes WHERE enabled) THEN
        RAISE EXCEPTION 'cannot roll back active AML routing evidence; use a forward migration';
    END IF;
END $$;
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM reg_crypto_aml_paid_admissions) OR EXISTS(SELECT 1 FROM reg_crypto_aml_package_credits)
       OR EXISTS(SELECT 1 FROM sys_crypto_aml_provider_mappings)
       OR EXISTS(SELECT 1 FROM cat_integration_accounts WHERE provider='getblock_net')
       OR EXISTS(SELECT 1 FROM cat_integration_pools WHERE provider='getblock_net') THEN
        RAISE EXCEPTION 'cannot roll back AML runtime evidence; use a forward migration';
    END IF;
END $$;
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM sys_crypto_custody_policies WHERE enforced) OR
       EXISTS(SELECT 1 FROM cat_wallets WHERE wallet_role='network_fee' AND is_active AND NOT deletion_mark
              GROUP BY network_id HAVING COUNT(*)>1) THEN
        RAISE EXCEPTION 'cannot roll back active merchant custody; use a forward migration';
    END IF;
END $$;
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM reg_crypto_aml_subjects) OR EXISTS(SELECT 1 FROM reg_crypto_withdrawal_risk_snapshots)
       OR EXISTS(SELECT 1 FROM reg_crypto_wallet_risk_states)
       OR EXISTS(SELECT 1 FROM cat_wallets WHERE custody_merchant_id IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot roll back AML custody/evidence with data; use a forward migration';
    END IF;
END $$;
DROP TRIGGER trg_crypto_wallet_custody_owner_guard ON cat_wallets;
DROP TRIGGER trg_crypto_aml_balance_risk_guard ON reg_crypto_balance_movements;
DROP TRIGGER trg_crypto_aml_withdrawal_contract_guard ON doc_withdrawal_requests;
DROP TRIGGER trg_crypto_aml_baseline_check_guard ON reg_crypto_reconciliation_checks;
DROP TRIGGER trg_crypto_aml_token_identity_guard ON cat_tokens;
DROP TRIGGER trg_crypto_aml_balance_projection_guard ON reg_crypto_balance_balances;
DROP TRIGGER trg_crypto_aml_allocated_attempt_guard ON reg_crypto_sweep_attempts;
DROP TRIGGER trg_crypto_aml_allocation_signing_guard ON reg_crypto_signing_authorizations;
DROP TRIGGER trg_crypto_aml_inventory_physical_queue ON reg_crypto_balance_movements;
DROP TRIGGER trg_crypto_aml_inventory_merchant_queue ON reg_crypto_merchant_balance_movements;
DROP TRIGGER trg_crypto_aml_inventory_fee_queue ON reg_crypto_fee_movements;
DROP TRIGGER trg_crypto_aml_inventory_payment_queue ON doc_crypto_payments;
DROP TRIGGER trg_crypto_aml_inventory_sweep_queue ON doc_crypto_sweeps;
DROP TRIGGER trg_crypto_aml_resource_fee_insert_guard ON reg_crypto_fee_reservations;
DROP TRIGGER trg_crypto_aml_resource_fee_identity ON reg_crypto_fee_reservations;
DROP TRIGGER trg_crypto_aml_resource_funding_identity ON doc_crypto_native_fundings;
DROP TRIGGER trg_crypto_aml_00_resource_signing_guard ON reg_crypto_signing_authorizations;
DROP TRIGGER trg_crypto_aml_resource_fee_queue ON reg_crypto_fee_reservations;
DROP TRIGGER trg_crypto_aml_resource_funding_queue ON doc_crypto_native_fundings;
DROP TRIGGER trg_crypto_aml_resource_settlement_queue ON reg_crypto_chain_settlements;
DROP TRIGGER trg_crypto_aml_resource_movement_queue ON reg_crypto_balance_movements;
DROP TRIGGER trg_crypto_aml_01_batch_signing_guard ON reg_crypto_signing_authorizations;
DROP TRIGGER trg_crypto_aml_legacy_provision_guard ON cat_wallets;
DROP TRIGGER trg_crypto_aml_resource_activation_queue ON doc_crypto_network_operations;
DROP TRIGGER trg_crypto_aml_withdrawal_signing_guard ON reg_crypto_signing_authorizations;
DROP TRIGGER trg_crypto_aml_withdrawal_parent_guard ON doc_withdrawal_requests;
DROP TRIGGER trg_crypto_aml_withdrawal_merchant_movement ON reg_crypto_merchant_balance_movements;
DROP TRIGGER trg_crypto_aml_withdrawal_terminal_event ON doc_withdrawal_requests;
DROP TRIGGER trg_crypto_aml_testnet_network ON cat_blockchain_networks;
DROP TRIGGER trg_crypto_aml_testnet_account ON cat_integration_accounts;
DROP VIEW crypto_aml_pool_runtime;
DROP VIEW crypto_aml_batch_usage;
DROP VIEW crypto_aml_resource_fact_validity;
DROP VIEW crypto_aml_ready_resource_budgets;
DROP VIEW crypto_aml_risk_edges;
DROP TABLE reg_crypto_aml_preflight_observations;
DROP TABLE reg_crypto_aml_preflight_attempts;
DROP TABLE reg_crypto_aml_authorization_renewals;
DROP TABLE reg_crypto_aml_testnet_openings;
DROP TABLE reg_crypto_aml_testnet_bootstraps;
DROP TABLE sys_crypto_aml_testnet_emulators;
DROP TABLE reg_crypto_aml_withdrawal_events;
DROP TABLE reg_crypto_aml_withdrawal_facts;
DROP TABLE reg_crypto_aml_withdrawal_cancellations;
DROP TABLE reg_crypto_aml_withdrawal_lines;
DROP TABLE reg_crypto_aml_withdrawal_plans;
DROP TABLE reg_crypto_aml_first_dispatches;
DROP TABLE reg_crypto_aml_dispatch_starts;
DROP TABLE reg_crypto_aml_preflight_jobs;
DROP TABLE reg_crypto_aml_watch_coverage;
DROP TABLE reg_crypto_aml_pool_intent_cancellations;
DROP TABLE reg_crypto_aml_pool_intents;
DROP TABLE reg_crypto_aml_pool_policy_versions;
DROP TABLE sys_crypto_aml_pool_policies;
DROP TABLE reg_crypto_aml_wallet_provisions;
DROP TABLE sys_crypto_aml_provisioning_identity;
DROP TABLE reg_crypto_aml_batch_events;
DROP TABLE reg_crypto_aml_inventory_facts;
DROP TABLE reg_crypto_aml_allocation_cancellations;
DROP TABLE reg_crypto_aml_internal_allocations;
DROP TABLE reg_crypto_aml_batches;
DROP TABLE sys_crypto_aml_batch_policies;
DROP TABLE reg_crypto_aml_resource_facts;
DROP TABLE reg_crypto_aml_resource_cancellations;
DROP TABLE reg_crypto_aml_resource_budgets;
DROP TABLE reg_crypto_aml_operating_receipts;
DROP TABLE reg_crypto_aml_resource_positions;
DROP TABLE reg_crypto_aml_inventory_queue;
DROP TABLE reg_crypto_aml_inventory_positions;
DROP TABLE reg_crypto_aml_risk_changes;
DROP TABLE reg_crypto_aml_projection_queue;
DROP TABLE reg_crypto_aml_rescreen_requests;
DROP TABLE reg_crypto_aml_effect_chain_states;
DROP TABLE reg_crypto_aml_effect_checks;
DROP TABLE reg_crypto_aml_effects;
DROP TABLE reg_crypto_aml_wallet_baselines;
DROP TABLE reg_crypto_aml_decision_queue;
DROP TABLE sys_crypto_aml_routing_scopes;
DROP TABLE sys_crypto_aml_risk_policies;
DROP TABLE reg_crypto_aml_paid_admissions;
DROP TABLE reg_crypto_aml_package_credits;
DROP TABLE sys_crypto_aml_account_policies;
DROP TABLE sys_crypto_aml_provider_mappings;
DROP TABLE sys_crypto_custody_policies;
DROP TABLE reg_crypto_withdrawal_risk_snapshots;
DROP TABLE reg_crypto_wallet_risk_states;
DROP TABLE reg_crypto_aml_decisions;
DROP TABLE reg_crypto_aml_reports;
DROP TABLE reg_crypto_aml_provider_checks;
DROP TABLE reg_crypto_aml_jobs;
DROP TABLE reg_crypto_aml_subjects;
DROP FUNCTION fn_crypto_aml_authorization_renewal_guard();
DROP FUNCTION fn_crypto_aml_testnet_bootstrap_complete();
DROP FUNCTION fn_crypto_aml_testnet_opening_apply();
DROP FUNCTION fn_crypto_aml_testnet_opening_valid(wallet uuid, asset uuid);
DROP FUNCTION fn_crypto_aml_testnet_opening_guard();
DROP FUNCTION fn_crypto_aml_testnet_bootstrap_guard();
DROP FUNCTION fn_crypto_aml_ledger_hash(wallet uuid, asset uuid, before_time timestamp with time zone);
DROP FUNCTION fn_crypto_aml_report_emulation_guard();
DROP FUNCTION fn_crypto_aml_check_provider_guard();
DROP FUNCTION fn_crypto_aml_job_provider_guard();
DROP FUNCTION fn_crypto_aml_testnet_mapping_guard();
DROP FUNCTION fn_crypto_aml_testnet_identity_guard();
DROP FUNCTION fn_crypto_aml_testnet_binding_guard();
DROP FUNCTION fn_crypto_aml_is_emulation_network(family text, identifier text);
DROP FUNCTION fn_crypto_aml_withdrawal_terminal_event();
DROP FUNCTION fn_crypto_aml_withdrawal_merchant_movement_guard();
DROP FUNCTION fn_crypto_aml_refresh_withdrawal(parent uuid);
DROP FUNCTION fn_crypto_aml_withdrawal_parent_guard();
DROP FUNCTION fn_crypto_aml_withdrawal_signing_guard();
DROP FUNCTION fn_crypto_aml_check_withdrawal_dispatch(a reg_crypto_signing_authorizations, phase text);
DROP FUNCTION fn_crypto_aml_withdrawal_plan_complete();
DROP FUNCTION fn_crypto_aml_withdrawal_fact_apply();
DROP FUNCTION fn_crypto_aml_withdrawal_fact_guard();
DROP FUNCTION fn_crypto_aml_withdrawal_line_guard();
DROP FUNCTION fn_crypto_aml_begin_dispatch(auth_id uuid, dispatch_phase text);
DROP FUNCTION fn_crypto_aml_has_unapplied_risk(root uuid, asset uuid);
DROP FUNCTION fn_crypto_aml_check_batch_dispatch(a reg_crypto_signing_authorizations, phase text);
DROP FUNCTION fn_crypto_aml_check_resource_dispatch(a reg_crypto_signing_authorizations, phase text);
DROP FUNCTION fn_crypto_aml_check_allocation_dispatch(a reg_crypto_signing_authorizations, phase text);
DROP FUNCTION fn_crypto_aml_spend_plan_matches(basis text, basis_id uuid, purpose text, source uuid, asset uuid, destination text, amount numeric);
DROP FUNCTION fn_crypto_aml_preflight_observation_guard();
DROP FUNCTION fn_crypto_aml_automatic_baseline_guard();
DROP FUNCTION fn_crypto_aml_watch_coverage_guard();
DROP FUNCTION fn_crypto_aml_pool_preflight_policy_guard();
DROP FUNCTION fn_crypto_aml_pool_cancel_guard();
DROP FUNCTION fn_crypto_aml_pool_intent_guard();
DROP FUNCTION fn_crypto_aml_pool_policy_audit();
DROP FUNCTION fn_crypto_aml_pool_policy_guard();
DROP FUNCTION fn_crypto_aml_pool_high_hold();
DROP FUNCTION fn_crypto_aml_pool_scope_hold();
DROP FUNCTION fn_crypto_aml_pool_baselines_ready(intent uuid);
DROP FUNCTION fn_crypto_aml_network_wallet_commitments(network uuid, excluded uuid);
DROP FUNCTION fn_crypto_aml_pool_counts(pool uuid);
DROP FUNCTION fn_crypto_aml_legacy_provision_guard();
DROP FUNCTION fn_crypto_aml_provision_guard();
DROP FUNCTION fn_crypto_aml_batch_observation();
DROP FUNCTION fn_crypto_aml_batch_signing_guard();
DROP FUNCTION fn_crypto_aml_batch_allocation_audit();
DROP FUNCTION fn_crypto_aml_batch_allocation_guard();
DROP FUNCTION fn_crypto_aml_batch_audit();
DROP FUNCTION fn_crypto_aml_batch_guard();
DROP FUNCTION fn_crypto_aml_batch_policy_guard();
DROP FUNCTION fn_crypto_aml_resource_queue_trigger();
DROP FUNCTION fn_crypto_aml_queue_resource_budget(reservation uuid);
DROP FUNCTION fn_crypto_aml_resource_signing_guard();
DROP FUNCTION fn_crypto_aml_resource_basis_guard();
DROP FUNCTION fn_crypto_aml_resource_baseline();
DROP FUNCTION fn_crypto_aml_principal_resource_exclusion();
DROP FUNCTION fn_crypto_aml_resource_fact_apply();
DROP FUNCTION fn_crypto_aml_resource_fact_guard();
DROP FUNCTION fn_crypto_aml_resource_budget_revision();
DROP FUNCTION fn_crypto_aml_resource_budget_guard();
DROP FUNCTION fn_crypto_aml_operating_receipt_guard();
DROP FUNCTION fn_crypto_aml_resource_fee_insert_guard();
DROP FUNCTION fn_crypto_aml_lock_fee_wallet_scopes(payer uuid, asset uuid);
DROP FUNCTION fn_crypto_aml_lock_resource_scopes(reservation uuid);
DROP FUNCTION fn_crypto_aml_total_inventory_covered(target uuid, asset uuid);
DROP FUNCTION fn_crypto_aml_inventory_effect_queue();
DROP FUNCTION fn_crypto_aml_inventory_scope_queue();
DROP FUNCTION fn_crypto_aml_inventory_baseline();
DROP FUNCTION fn_crypto_aml_inventory_document_queue();
DROP FUNCTION fn_crypto_aml_inventory_movement_queue();
DROP FUNCTION fn_crypto_aml_queue_inventory(basis_kind text, basis uuid);
DROP FUNCTION fn_crypto_aml_allocation_signing_guard();
DROP FUNCTION fn_crypto_aml_allocated_attempt_guard();
DROP FUNCTION fn_crypto_aml_allocation_revision();
DROP FUNCTION fn_crypto_aml_inventory_apply();
DROP FUNCTION fn_crypto_aml_inventory_fact_guard();
DROP FUNCTION fn_crypto_aml_allocation_guard();
DROP FUNCTION fn_crypto_aml_inventory_covered(target uuid, asset uuid);
DROP FUNCTION fn_crypto_aml_balance_projection_guard();
DROP FUNCTION fn_crypto_aml_token_identity_guard();
DROP FUNCTION fn_crypto_aml_scope_changed();
DROP FUNCTION fn_crypto_aml_effect_proof_guard();
DROP FUNCTION fn_crypto_aml_baseline_check_guard();
DROP FUNCTION fn_crypto_aml_effect_subject_guard();
DROP FUNCTION fn_crypto_aml_withdrawal_contract_guard();
DROP FUNCTION fn_crypto_aml_balance_risk_guard();
DROP FUNCTION fn_crypto_aml_invalidate_wallet(root_wallet uuid, asset uuid, why text);
DROP FUNCTION fn_crypto_aml_baseline_guard();
DROP FUNCTION fn_crypto_aml_routing_scope_guard();
DROP FUNCTION fn_crypto_aml_runtime_account_guard();
DROP FUNCTION fn_crypto_aml_mapping_scope_guard();
DROP FUNCTION fn_crypto_custody_policy_guard();
DROP FUNCTION fn_crypto_aml_subject_identity_guard();
DROP FUNCTION fn_crypto_aml_job_identity_guard();
DROP FUNCTION fn_crypto_aml_evidence_guard();
DROP FUNCTION fn_crypto_wallet_custody_owner_guard();
-- +goose StatementEnd
