-- Crypto outbound flows: withdrawals, sweeps, treasury withdrawals, signing
-- authorizations, money-movement approvals and signing jobs, native funding,
-- resource rentals, reconciliation and security incidents.
-- Consolidated schema: each object is declared once in its final form.

-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- Function bodies are validated when first executed; definitions precede the tables they use.
SET LOCAL check_function_bodies = off;


-- ---- Functions ----
-- CDC trigger
CREATE OR REPLACE FUNCTION fn_doc_crypto_withdrawals_cdc() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        NEW._txid := txid_current();
        NEW.updated_at := NOW();
        IF OLD.deletion_mark = FALSE AND NEW.deletion_mark = TRUE THEN
            NEW._deleted_at := NOW();
        ELSIF OLD.deletion_mark = TRUE AND NEW.deletion_mark = FALSE THEN
            NEW._deleted_at := NULL;
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- CDC trigger
CREATE OR REPLACE FUNCTION fn_doc_crypto_sweeps_cdc() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        NEW._txid := txid_current();
        NEW.updated_at := NOW();
        IF OLD.deletion_mark = FALSE AND NEW.deletion_mark = TRUE THEN
            NEW._deleted_at := NOW();
        ELSIF OLD.deletion_mark = TRUE AND NEW.deletion_mark = FALSE THEN
            NEW._deleted_at := NULL;
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_reject_managed_withdrawal_destination() RETURNS TRIGGER AS $$
BEGIN
    IF NEW._deleted_at IS NOT NULL THEN
        RETURN NEW;
    END IF;

    IF EXISTS (
        SELECT 1
        FROM cat_wallets w
        JOIN cat_blockchain_networks n ON n.id = w.network_id
        WHERE w.network_id = NEW.network_id
          AND w.deletion_mark = FALSE
          AND w.address_key = NEW.address_key
    ) THEN
        RAISE EXCEPTION 'withdrawal destination must be external to managed custody'
            USING ERRCODE = '23514',
                  CONSTRAINT = 'chk_withdrawal_address_external_destination';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- CDC trigger
CREATE OR REPLACE FUNCTION fn_doc_withdrawal_requests_cdc() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        NEW._txid := txid_current();
        NEW.updated_at := NOW();
        IF OLD.deletion_mark = FALSE AND NEW.deletion_mark = TRUE THEN
            NEW._deleted_at := NOW();
        ELSIF OLD.deletion_mark = TRUE AND NEW.deletion_mark = FALSE THEN
            NEW._deleted_at := NULL;
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_crypto_signing_auth_updated_at() RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at := NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_crypto_signing_auth_identity_immutable() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.authorization_id IS DISTINCT FROM OLD.authorization_id
       OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
       OR NEW.merchant_id IS DISTINCT FROM OLD.merchant_id
       OR NEW.purpose IS DISTINCT FROM OLD.purpose
       OR NEW.schema_version IS DISTINCT FROM OLD.schema_version
       OR NEW.network_id IS DISTINCT FROM OLD.network_id
       OR NEW.network_code IS DISTINCT FROM OLD.network_code
       OR NEW.chain_family IS DISTINCT FROM OLD.chain_family
       OR NEW.network_identifier IS DISTINCT FROM OLD.network_identifier
       OR NEW.source_address IS DISTINCT FROM OLD.source_address
       OR NEW.fee_payer_address IS DISTINCT FROM OLD.fee_payer_address
       OR NEW.vault_key_id IS DISTINCT FROM OLD.vault_key_id
       OR NEW.destination_address IS DISTINCT FROM OLD.destination_address
       OR NEW.asset_standard IS DISTINCT FROM OLD.asset_standard
       OR NEW.contract_address IS DISTINCT FROM OLD.contract_address
       OR NEW.amount_minor_units IS DISTINCT FROM OLD.amount_minor_units
       OR NEW.max_network_fee_minor IS DISTINCT FROM OLD.max_network_fee_minor
       OR NEW.basis_type IS DISTINCT FROM OLD.basis_type
       OR NEW.basis_id IS DISTINCT FROM OLD.basis_id
       OR NEW.attempt IS DISTINCT FROM OLD.attempt
       OR NEW.generation IS DISTINCT FROM OLD.generation
       OR NEW.network_sequence IS DISTINCT FROM OLD.network_sequence
       OR NEW.expires_at IS DISTINCT FROM OLD.expires_at
       OR NEW.policy_version IS DISTINCT FROM OLD.policy_version
       OR (
            NEW.authorization_jws IS DISTINCT FROM OLD.authorization_jws
            AND NOT (
                OLD.status = 'created'
                AND NEW.status = 'created'
                AND OLD.authorization_jws = ''
                AND BTRIM(NEW.authorization_jws) <> ''
            )
       )
       OR NEW.unsigned_fingerprint IS DISTINCT FROM OLD.unsigned_fingerprint
       OR NEW.unsigned_envelope IS DISTINCT FROM OLD.unsigned_envelope
       OR NEW.approval_bundle_digest IS DISTINCT FROM OLD.approval_bundle_digest
       OR NEW.approval_bundle_json IS DISTINCT FROM OLD.approval_bundle_json
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'signing authorization identity is immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_crypto_chain_effect_evidence_immutable() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'expected chain effects and matches are immutable';
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_doc_treasury_withdrawals_cdc() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        NEW._txid := txid_current();
        NEW.updated_at := NOW();
        IF OLD.deletion_mark = FALSE AND NEW.deletion_mark = TRUE THEN
            NEW._deleted_at := NOW();
        ELSIF OLD.deletion_mark = TRUE AND NEW.deletion_mark = FALSE THEN
            NEW._deleted_at := NULL;
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_external_outflow_evidence_guard() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'external outflow evidence cannot be deleted';
    END IF;
    IF TG_TABLE_NAME IN ('reg_crypto_external_outflow_actions') THEN
        RAISE EXCEPTION 'external outflow actions are immutable';
    END IF;

    IF TG_OP = 'INSERT' THEN
        IF TG_TABLE_NAME = 'reg_crypto_external_outflows' AND NEW.status <> 'pending_review' THEN
            RAISE EXCEPTION 'external outflow must start in pending_review';
        END IF;
        IF TG_TABLE_NAME = 'doc_crypto_custody_adjustments' AND NEW.status <> 'pending_approval' THEN
            RAISE EXCEPTION 'custody adjustment must start in pending_approval';
        END IF;
        RETURN NEW;
    END IF;

    -- Keep table-specific row access in separate procedural branches. Boolean
    -- predicates do not guarantee short-circuit evaluation and this function is
    -- attached to tables with different composite NEW/OLD row types.
    IF TG_TABLE_NAME = 'doc_crypto_custody_adjustments' THEN
        IF NEW.external_outflow_id<>OLD.external_outflow_id OR NEW.network_id<>OLD.network_id
           OR NEW.wallet_id<>OLD.wallet_id OR NEW.token_id IS DISTINCT FROM OLD.token_id
           OR NEW.amount<>OLD.amount OR NEW.maker_id<>OLD.maker_id OR OLD.status<>'pending_approval' THEN
            RAISE EXCEPTION 'custody adjustment source and terminal decision are immutable';
        END IF;
        IF NEW.status='posted'
           AND NOT EXISTS (
                SELECT 1 FROM reg_crypto_external_outflows outflow
                WHERE outflow.id=NEW.external_outflow_id
                  AND outflow.status='classified'
                  AND outflow.classification='unauthorized_loss'
                  AND outflow.finality_status IN ('confirmed','finalized')
                  AND (outflow.required_confs=0 OR outflow.confirmations>=outflow.required_confs)
           ) THEN
            RAISE EXCEPTION 'custody adjustment requires a currently final unauthorized loss';
        END IF;
        RETURN NEW;
    ELSIF TG_TABLE_NAME = 'reg_crypto_external_outflows' THEN
        IF NEW.network_id<>OLD.network_id OR NEW.tx_hash<>OLD.tx_hash
           OR NEW.event_kind<>OLD.event_kind OR NEW.event_index<>OLD.event_index
           OR NEW.source_wallet_id IS DISTINCT FROM OLD.source_wallet_id
           OR NEW.token_id IS DISTINCT FROM OLD.token_id OR NEW.amount<>OLD.amount
           OR NEW.source_address_key<>OLD.source_address_key
           OR NEW.destination_address_key<>OLD.destination_address_key
           OR NEW.contract_address_key<>OLD.contract_address_key THEN
            RAISE EXCEPTION 'external outflow observed identity is immutable';
        END IF;

        IF NEW.status <> OLD.status
           AND NOT ((OLD.status='pending_review' AND NEW.status='classified')
                    OR (OLD.status='classified' AND NEW.status='resolved')) THEN
            RAISE EXCEPTION 'invalid external outflow state transition: % -> %', OLD.status, NEW.status;
        END IF;

        IF NEW.status = OLD.status
           AND ROW(NEW.classification,NEW.matched_basis_type,NEW.matched_basis_id,
                   NEW.classification_reason,NEW.classification_evidence,
                   NEW.classified_by,NEW.classified_at)
               IS DISTINCT FROM
               ROW(OLD.classification,OLD.matched_basis_type,OLD.matched_basis_id,
                   OLD.classification_reason,OLD.classification_evidence,
                   OLD.classified_by,OLD.classified_at) THEN
            IF OLD.status <> 'classified'
               OR NEW.classification = 'unauthorized_loss'
               OR NOT EXISTS (
                    SELECT 1 FROM doc_crypto_custody_adjustments adjustment
                    WHERE adjustment.external_outflow_id=OLD.id AND adjustment.status='rejected'
               ) THEN
                RAISE EXCEPTION 'external outflow classification is immutable without a rejected custody adjustment';
            END IF;
        END IF;

        IF NEW.status = 'resolved'
           AND ROW(NEW.classification,NEW.matched_basis_type,NEW.matched_basis_id,
                   NEW.classification_reason,NEW.classification_evidence,
                   NEW.classified_by,NEW.classified_at)
               IS DISTINCT FROM
               ROW(OLD.classification,OLD.matched_basis_type,OLD.matched_basis_id,
                   OLD.classification_reason,OLD.classification_evidence,
                   OLD.classified_by,OLD.classified_at) THEN
            RAISE EXCEPTION 'classification cannot change during resolution';
        END IF;

        IF NEW.status <> 'resolved'
           AND ROW(NEW.resolved_by,NEW.resolved_at,NEW.resolution)
               IS DISTINCT FROM ROW(OLD.resolved_by,OLD.resolved_at,OLD.resolution) THEN
            RAISE EXCEPTION 'resolution evidence can only be set by the resolution transition';
        END IF;

        IF NEW.classification = 'matched_basis' AND NOT EXISTS (
            SELECT 1
            FROM reg_crypto_expected_chain_effects effect
            JOIN reg_crypto_signing_authorizations signing_auth
              ON signing_auth.id=effect.signing_authorization_id
            JOIN reg_crypto_chain_effect_matches matched
              ON matched.expected_effect_id=effect.id
            WHERE signing_auth.basis_type=NEW.matched_basis_type
              AND signing_auth.basis_id=NEW.matched_basis_id
              AND effect.network_id=NEW.network_id
              AND effect.tx_hash=NEW.tx_hash
              AND effect.event_kind=NEW.event_kind
              AND effect.token_id IS NOT DISTINCT FROM NEW.token_id
              AND effect.source_address_key=NEW.source_address_key
              AND effect.destination_address_key=NEW.destination_address_key
              AND effect.contract_address_key=NEW.contract_address_key
              AND effect.amount=NEW.amount
              AND matched.network_id=NEW.network_id
              AND matched.tx_hash=NEW.tx_hash
              AND matched.event_kind=NEW.event_kind
              AND matched.event_index=NEW.event_index
        ) THEN
            RAISE EXCEPTION 'matched basis does not prove the exact observed chain effect';
        END IF;

        IF NEW.classification = 'unauthorized_loss'
           AND NEW.classification IS DISTINCT FROM OLD.classification
           AND (NEW.finality_status NOT IN ('confirmed','finalized')
                OR (NEW.required_confs>0 AND NEW.confirmations<NEW.required_confs)) THEN
            RAISE EXCEPTION 'unauthorized loss classification requires a final chain effect';
        END IF;

        IF OLD.status='classified' AND NEW.status='resolved'
           AND OLD.classification='unauthorized_loss'
           AND NOT EXISTS (
                SELECT 1 FROM doc_crypto_custody_adjustments adjustment
                WHERE adjustment.external_outflow_id=OLD.id AND adjustment.status='posted'
           ) THEN
            RAISE EXCEPTION 'posted custody adjustment is required before resolution';
        END IF;

        IF OLD.status='classified' AND NEW.status='resolved'
           AND OLD.classification='unauthorized_loss'
           AND (OLD.finality_status NOT IN ('confirmed','finalized')
                OR (OLD.required_confs>0 AND OLD.confirmations<OLD.required_confs)) THEN
            RAISE EXCEPTION 'custody loss chain effect is no longer final';
        END IF;

        IF OLD.status='classified' AND NEW.status='resolved'
           AND NOT EXISTS (
                SELECT 1
                FROM reg_crypto_reconciliation_checks check_result
                JOIN reg_crypto_reconciliation_runs run ON run.id=check_result.run_id
                WHERE check_result.wallet_id=OLD.source_wallet_id
                  AND check_result.token_id IS NOT DISTINCT FROM OLD.token_id
                  AND run.status='completed'
                  AND run.completed_at>GREATEST(
                      OLD.classified_at,
                      COALESCE((
                          SELECT adjustment.decided_at
                          FROM doc_crypto_custody_adjustments adjustment
                          WHERE adjustment.external_outflow_id=OLD.id
                            AND adjustment.status='posted'
                      ),OLD.classified_at)
                  )
                  AND check_result.delta_amount=0
                  AND NOT EXISTS (
                      SELECT 1 FROM reg_crypto_reconciliation_items incident
                      WHERE incident.wallet_id=check_result.wallet_id
                        AND incident.token_id IS NOT DISTINCT FROM check_result.token_id
                        AND incident.kind IN ('token_balance_mismatch','native_fee_balance_mismatch')
                        AND incident.status IN ('open','acknowledged')
                  )
           ) THEN
            RAISE EXCEPTION 'fresh successful reconciliation is required before resolution';
        END IF;
    ELSE
        RAISE EXCEPTION 'unsupported external outflow evidence trigger table: %', TG_TABLE_NAME;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- The operator workflow and its reconciliation incident are one terminal
-- fact. Deferred checks allow either update order inside the application
-- transaction, while rejecting direct SQL that closes only one side.
CREATE OR REPLACE FUNCTION fn_external_outflow_incident_state_guard() RETURNS TRIGGER AS $$
BEGIN
    -- As above, each table branch must be isolated before accessing its row
    -- fields because this deferred trigger function is shared across row types.
    IF TG_TABLE_NAME='reg_crypto_external_outflows' THEN
        IF NEW.status='resolved' AND (EXISTS (
            SELECT 1 FROM reg_crypto_reconciliation_items incident
            WHERE incident.network_id=NEW.network_id
              AND incident.wallet_id=NEW.source_wallet_id
              AND incident.token_id IS NOT DISTINCT FROM NEW.token_id
              AND incident.kind='external_outflow'
              AND incident.tx_hash=NEW.tx_hash
              AND incident.event_kind=NEW.event_kind
              AND incident.event_index=NEW.event_index
              AND incident.status IN ('open','acknowledged')
        ) OR NOT EXISTS (
            SELECT 1 FROM reg_crypto_reconciliation_items incident
            WHERE incident.network_id=NEW.network_id
              AND incident.wallet_id=NEW.source_wallet_id
              AND incident.token_id IS NOT DISTINCT FROM NEW.token_id
              AND incident.kind='external_outflow'
              AND incident.tx_hash=NEW.tx_hash
              AND incident.event_kind=NEW.event_kind
              AND incident.event_index=NEW.event_index
              AND incident.status='resolved'
        )) THEN
            RAISE EXCEPTION 'resolved external outflow requires its exact resolved reconciliation incident';
        END IF;
    ELSIF TG_TABLE_NAME='reg_crypto_reconciliation_items' THEN
        IF NEW.kind='external_outflow' AND NEW.status='resolved' AND NOT EXISTS (
            SELECT 1 FROM reg_crypto_external_outflows outflow
            WHERE outflow.network_id=NEW.network_id
              AND outflow.source_wallet_id=NEW.wallet_id
              AND outflow.token_id IS NOT DISTINCT FROM NEW.token_id
              AND outflow.tx_hash=NEW.tx_hash
              AND outflow.event_kind=NEW.event_kind
              AND outflow.event_index=NEW.event_index
              AND outflow.status='resolved'
        ) THEN
            RAISE EXCEPTION 'resolved external outflow incident requires its exact resolved outflow';
        END IF;
    ELSE
        RAISE EXCEPTION 'unsupported external outflow incident trigger table: %', TG_TABLE_NAME;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- A custody adjustment is financially terminal only when posting.Engine has
-- atomically produced both required platform-owned effects. A deferred
-- constraint keeps the invariant independent from application code and still
-- permits the engine to record movements before updating the document state.
CREATE OR REPLACE FUNCTION fn_crypto_custody_adjustment_posting_effects_guard()
RETURNS TRIGGER AS $$
DECLARE
    wallet_total     BIGINT;
    wallet_effects   BIGINT;
    platform_total   BIGINT;
    platform_effects BIGINT;
    merchant_effects BIGINT;
BEGIN
    IF NEW.status <> 'posted' OR OLD.status = 'posted' THEN
        RETURN NEW;
    END IF;

    SELECT COUNT(*), COUNT(*) FILTER (WHERE
             movement.recorder_version=NEW.posted_version
         AND movement.record_type='expense'
         AND movement.wallet_id=NEW.wallet_id
         AND movement.token_id=NEW.token_id
         AND movement.amount=NEW.amount)
    INTO wallet_total, wallet_effects
    FROM reg_crypto_balance_movements movement
    WHERE movement.recorder_type='CryptoCustodyAdjustment'
      AND movement.recorder_id=NEW.id;

    SELECT COUNT(*), COUNT(*) FILTER (WHERE
             movement.recorder_version=NEW.posted_version
         AND movement.record_type='expense'
         AND movement.network_id=NEW.network_id
         AND movement.operation_token_id=NEW.token_id
         AND movement.cost_token_id=NEW.token_id
         AND movement.cost_type='custody_loss'
         AND movement.purpose='custody_loss'
         AND movement.amount=NEW.amount)
    INTO platform_total, platform_effects
    FROM reg_crypto_platform_cost_movements movement
    WHERE movement.recorder_type='CryptoCustodyAdjustment'
      AND movement.recorder_id=NEW.id;

    SELECT COUNT(*) INTO merchant_effects
    FROM reg_crypto_merchant_balance_movements movement
    WHERE movement.recorder_type='CryptoCustodyAdjustment'
      AND movement.recorder_id=NEW.id;

    IF wallet_total <> 1 OR wallet_effects <> 1
       OR platform_total <> 1 OR platform_effects <> 1
       OR merchant_effects <> 0 THEN
        RAISE EXCEPTION
          'posted custody adjustment requires exactly one wallet expense, one platform custody-loss expense and no merchant movement';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_crypto_custody_wallet_effect_guard()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.recorder_type <> 'CryptoCustodyAdjustment' THEN
            RETURN NEW;
        END IF;
        IF NOT EXISTS (
            SELECT 1 FROM doc_crypto_custody_adjustments adjustment
            WHERE adjustment.id=NEW.recorder_id
              AND adjustment.status='pending_approval'
              AND NEW.recorder_version=1
              AND NEW.record_type='expense'
              AND NEW.wallet_id=adjustment.wallet_id
              AND NEW.token_id=adjustment.token_id
              AND NEW.amount=adjustment.amount
        ) THEN
            RAISE EXCEPTION 'custody adjustment wallet movement does not match its immutable source';
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' THEN
        IF OLD.recorder_type='CryptoCustodyAdjustment' OR NEW.recorder_type='CryptoCustodyAdjustment' THEN
            RAISE EXCEPTION 'custody adjustment wallet movements are immutable';
        END IF;
        RETURN NEW;
    END IF;
    IF OLD.recorder_type='CryptoCustodyAdjustment' THEN
        RAISE EXCEPTION 'custody adjustment wallet movements are immutable';
    END IF;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_crypto_custody_platform_effect_guard()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.recorder_type <> 'CryptoCustodyAdjustment' THEN
            RETURN NEW;
        END IF;
        IF NOT EXISTS (
            SELECT 1 FROM doc_crypto_custody_adjustments adjustment
            WHERE adjustment.id=NEW.recorder_id
              AND adjustment.status='pending_approval'
              AND NEW.recorder_version=1
              AND NEW.record_type='expense'
              AND NEW.network_id=adjustment.network_id
              AND NEW.operation_token_id=adjustment.token_id
              AND NEW.cost_token_id=adjustment.token_id
              AND NEW.cost_type='custody_loss'
              AND NEW.purpose='custody_loss'
              AND NEW.amount=adjustment.amount
        ) THEN
            RAISE EXCEPTION 'custody adjustment platform movement does not match its immutable source';
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' THEN
        IF OLD.recorder_type='CryptoCustodyAdjustment' OR NEW.recorder_type='CryptoCustodyAdjustment' THEN
            RAISE EXCEPTION 'custody adjustment platform movements are immutable';
        END IF;
        RETURN NEW;
    END IF;
    IF OLD.recorder_type='CryptoCustodyAdjustment' THEN
        RAISE EXCEPTION 'custody adjustment platform movements are immutable';
    END IF;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_crypto_network_operations_cdc() RETURNS TRIGGER AS $$
BEGIN
    NEW._txid := txid_current();
    NEW.updated_at := NOW();
    IF OLD.deletion_mark = FALSE AND NEW.deletion_mark = TRUE THEN
        NEW._deleted_at := NOW();
    ELSIF OLD.deletion_mark = TRUE AND NEW.deletion_mark = FALSE THEN
        NEW._deleted_at := NULL;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_crypto_wallet_hold_owner_guard() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP='DELETE' OR NEW.wallet_id<>OLD.wallet_id OR NEW.owner_type<>OLD.owner_type
       OR NEW.owner_id<>OLD.owner_id OR NEW.reason<>OLD.reason
       OR NEW.details IS DISTINCT FROM OLD.details OR NEW.held_at<>OLD.held_at
       OR NEW.held_by<>OLD.held_by OR OLD.status='resolved'
       OR (OLD.status='open' AND NEW.status NOT IN ('open','resolved')) THEN
        RAISE EXCEPTION 'wallet hold acquisition identity is immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_reconciliation_observations_immutable() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'reconciliation observations are immutable';
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_crypto_provider_accounts_cdc() RETURNS TRIGGER AS $$
BEGIN
    NEW._txid := txid_current();
    NEW.updated_at := NOW();
    IF OLD.deletion_mark = FALSE AND NEW.deletion_mark = TRUE THEN
        NEW._deleted_at := NOW();
    ELSIF OLD.deletion_mark = TRUE AND NEW.deletion_mark = FALSE THEN
        NEW._deleted_at := NULL;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_crypto_resource_rentals_cdc() RETURNS TRIGGER AS $$
BEGIN
    NEW._txid := txid_current();
    NEW.updated_at := NOW();
    IF OLD.deletion_mark = FALSE AND NEW.deletion_mark = TRUE THEN
        NEW._deleted_at := NOW();
    ELSIF OLD.deletion_mark = TRUE AND NEW.deletion_mark = FALSE THEN
        NEW._deleted_at := NULL;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION reject_crypto_resource_allocation_event_mutation() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'crypto resource allocation events are immutable';
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION guard_crypto_provider_balance_fact() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION 'provider balance evidence and movements are immutable' USING ERRCODE='23514';
    END IF;
    -- Common serialization point for openings, rental charges and observations.
    PERFORM 1 FROM cat_crypto_provider_accounts WHERE id=NEW.provider_account_id FOR UPDATE;
    IF NOT EXISTS (SELECT 1 FROM cat_crypto_provider_accounts WHERE id=NEW.provider_account_id AND balance_token_id=NEW.token_id) THEN
        RAISE EXCEPTION 'provider balance token mismatch' USING ERRCODE='23514';
    END IF;
    IF TG_TABLE_NAME='reg_crypto_provider_balance_observations' THEN
        IF NEW.ledger_amount <> (SELECT COALESCE(SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END),0)
            FROM reg_crypto_provider_balance_movements WHERE provider_account_id=NEW.provider_account_id AND token_id=NEW.token_id) THEN
            RAISE EXCEPTION 'provider observation ledger snapshot is stale' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Shared by the application adapter and the transition guard. Membership before
-- user matches RevokeRole (DELETE membership, then bump auth_users.auth_version).
-- The read projection uses lock_authority=false; commands/trigger always lock.
CREATE FUNCTION crypto_provider_opening_admin_authority(actor UUID, lock_authority BOOLEAN)
RETURNS TABLE (authority_basis TEXT, user_auth_version BIGINT) AS $$
DECLARE admin_member BOOLEAN; principal auth_users%ROWTYPE;
BEGIN
    IF lock_authority THEN
        PERFORM ur.user_id FROM auth_user_roles ur JOIN auth_roles r ON r.id=ur.role_id
            WHERE ur.user_id=actor AND r.code='admin' FOR SHARE OF r,ur;
        admin_member:=FOUND;
        SELECT * INTO principal FROM auth_users WHERE id=actor FOR SHARE;
    ELSE
        SELECT EXISTS(SELECT 1 FROM auth_user_roles ur JOIN auth_roles r ON r.id=ur.role_id
            WHERE ur.user_id=actor AND r.code='admin') INTO admin_member;
        SELECT * INTO principal FROM auth_users WHERE id=actor;
    END IF;
    IF principal.id IS NOT NULL AND principal.is_active AND NOT principal.deletion_mark THEN
        IF principal.is_admin THEN
            RETURN QUERY SELECT 'system_admin_flag'::TEXT, principal.auth_version;
        ELSIF admin_member THEN
            RETURN QUERY SELECT 'system_admin_role'::TEXT, principal.auth_version;
        END IF;
    END IF;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION guard_crypto_provider_balance_operation() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'provider balance operations cannot be deleted' USING ERRCODE='23514';
    END IF;
    PERFORM 1 FROM cat_crypto_provider_accounts WHERE id=NEW.provider_account_id FOR UPDATE;
    IF TG_OP='INSERT' THEN
        IF NEW.status<>'pending_approval' OR NEW.version<>1 OR NEW.updated_by IS DISTINCT FROM NEW.created_by THEN
            RAISE EXCEPTION 'provider operation must start with an authenticated maker' USING ERRCODE='23514';
        END IF;
    ELSE
        IF OLD.status<>'pending_approval' OR NEW.status NOT IN ('posted','rejected') OR NEW.version<>OLD.version+1 THEN
            RAISE EXCEPTION 'invalid provider operation transition' USING ERRCODE='23514';
        END IF;
        IF NEW.decision_mode='admin_self' AND NOT EXISTS (
            SELECT 1 FROM crypto_provider_opening_admin_authority(COALESCE(NEW.approved_by,NEW.rejected_by), TRUE)
        ) THEN
            RAISE EXCEPTION 'provider opening self decision requires current system administrator authority' USING ERRCODE='23514';
        END IF;
        IF ROW(NEW.id,NEW.number,NEW.date,NEW.basis_type,NEW.basis_id,NEW.created_by,NEW.created_at,
               NEW.provider_account_id,NEW.token_id,NEW.provider_observation_id,NEW.source_wallet_id,
               NEW.operation_type,NEW.record_type,NEW.amount,NEW.tx_hash,NEW.external_ref,NEW.evidence,NEW.description,NEW.attributes)
           IS DISTINCT FROM ROW(OLD.id,OLD.number,OLD.date,OLD.basis_type,OLD.basis_id,OLD.created_by,OLD.created_at,
               OLD.provider_account_id,OLD.token_id,OLD.provider_observation_id,OLD.source_wallet_id,
               OLD.operation_type,OLD.record_type,OLD.amount,OLD.tx_hash,OLD.external_ref,OLD.evidence,OLD.description,OLD.attributes) THEN
            RAISE EXCEPTION 'provider operation acquisition identity is immutable' USING ERRCODE='23514';
        END IF;
    END IF;
    IF NEW.status<>'rejected' THEN
        PERFORM 1 FROM reg_crypto_provider_balance_incidents WHERE id=NEW.basis_id FOR UPDATE;
        IF NOT EXISTS (
            SELECT 1 FROM reg_crypto_provider_balance_observations obs
            JOIN reg_crypto_provider_balance_incidents i ON i.id=NEW.basis_id
            WHERE obs.id=NEW.provider_observation_id AND NEW.basis_type='CryptoProviderBalanceIncident'
            AND NEW.external_ref='opening-observation:' || obs.id::text
            AND i.status IN ('open','acknowledged') AND i.provider_account_id=NEW.provider_account_id AND i.token_id=NEW.token_id
            AND obs.provider_account_id=NEW.provider_account_id AND obs.token_id=NEW.token_id
            AND (i.ledger_amount,i.provider_amount,i.delta_amount)=(obs.ledger_amount,obs.provider_amount,obs.delta_amount)
            AND obs.delta_amount=NEW.amount
            AND obs.id=(SELECT id FROM reg_crypto_provider_balance_observations
                WHERE provider_account_id=NEW.provider_account_id AND token_id=NEW.token_id ORDER BY observed_at DESC,id DESC LIMIT 1)
            AND obs.ledger_amount=(SELECT COALESCE(SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END),0)
                FROM reg_crypto_provider_balance_movements
                WHERE provider_account_id=NEW.provider_account_id AND token_id=NEW.token_id
                AND NOT (recorder_type='CryptoProviderBalanceOperation' AND recorder_id=NEW.id))
        ) THEN
            RAISE EXCEPTION 'opening requires exact current provider reconciliation evidence' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION guard_crypto_provider_decision_audit() RETURNS TRIGGER AS $$
BEGIN
    IF OLD.event_type='crypto.provider_opening.decision'
       OR (TG_OP='UPDATE' AND NEW.event_type='crypto.provider_opening.decision') THEN
        RAISE EXCEPTION 'provider opening decision audit is immutable' USING ERRCODE='23514';
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION check_crypto_provider_opening_posting() RETURNS TRIGGER AS $$
DECLARE operation_id UUID; doc doc_crypto_provider_balance_operations%ROWTYPE; movement_count INT; exact_count INT; audit_count INT; exact_audit INT;
BEGIN
    IF TG_TABLE_NAME='doc_crypto_provider_balance_operations' THEN operation_id:=NEW.id;
    ELSIF TG_NARGS>0 AND TG_ARGV[0]='audit' THEN
        IF NEW.event_type<>'crypto.provider_opening.decision' THEN RETURN NULL; END IF;
        operation_id:=NEW.entity_id;
    ELSE
        IF NEW.recorder_type<>'CryptoProviderBalanceOperation' THEN RETURN NULL; END IF;
        operation_id:=NEW.recorder_id;
    END IF;
    SELECT * INTO doc FROM doc_crypto_provider_balance_operations WHERE id=operation_id;
    IF NOT FOUND THEN RAISE EXCEPTION 'provider movement requires an operation' USING ERRCODE='23514'; END IF;
    SELECT COUNT(*),COUNT(*) FILTER (WHERE provider_account_id=doc.provider_account_id AND token_id=doc.token_id
        AND amount=doc.amount AND record_type=doc.record_type AND operation_type=doc.operation_type
        AND external_ref=doc.external_ref AND recorder_version=1)
    INTO movement_count,exact_count FROM reg_crypto_provider_balance_movements
    WHERE recorder_type='CryptoProviderBalanceOperation' AND recorder_id=operation_id;
    IF (doc.status='posted' AND (movement_count<>1 OR exact_count<>1))
       OR (doc.status<>'posted' AND movement_count<>0) THEN
        RAISE EXCEPTION 'provider opening and exact ledger effect must commit atomically' USING ERRCODE='23514';
    END IF;
    SELECT COUNT(*), COUNT(*) FILTER (WHERE user_id=COALESCE(doc.approved_by,doc.rejected_by)::text
        AND session_id IS NOT NULL AND session_id<>'' AND session_id<>'00000000-0000-0000-0000-000000000000'
        AND details->>'makerId'=doc.created_by::text AND details->>'actorId'=user_id
        AND details->>'decisionMode'=doc.decision_mode AND details->>'decisionPolicyVersion'='1'
        AND details->>'action'=CASE WHEN doc.status='posted' THEN 'approve' ELSE 'reject' END
        AND details->>'reason'=CASE WHEN doc.status='posted' THEN doc.approval_reason ELSE doc.rejection_reason END
        AND details->>'requestFingerprint' ~ '^[0-9a-f]{64}$' AND details->>'stepUpCheckedAt' IS NOT NULL
        AND (doc.decision_mode<>'admin_self' OR details->>'authorityBasis' IN ('system_admin_flag','system_admin_role')))
    INTO audit_count,exact_audit FROM sys_event_log
    WHERE entity_type='CryptoProviderBalanceOperation' AND entity_id=operation_id AND event_type='crypto.provider_opening.decision';
    IF (doc.status IN ('posted','rejected') AND (audit_count<>1 OR exact_audit<>1))
       OR (doc.status='pending_approval' AND audit_count<>0) THEN
        RAISE EXCEPTION 'provider opening decision and exact audit must commit atomically' USING ERRCODE='23514';
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_money_movement_signing_job_identity_guard() RETURNS TRIGGER AS $$
BEGIN
    IF (OLD.basis_type,OLD.basis_id,OLD.intent_payload,OLD.intent_fingerprint)
       IS DISTINCT FROM
       (NEW.basis_type,NEW.basis_id,NEW.intent_payload,NEW.intent_fingerprint) THEN
        RAISE EXCEPTION 'money movement signing job identity is immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION fn_money_movement_signing_job_basis_guard() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.basis_type='WithdrawalRequest' AND NOT EXISTS
        (SELECT 1 FROM doc_withdrawal_requests WHERE id=NEW.basis_id AND _deleted_at IS NULL) THEN
        RAISE EXCEPTION 'withdrawal request signing job has no active basis';
    ELSIF NEW.basis_type='TreasuryWithdrawal' AND NOT EXISTS
        (SELECT 1 FROM doc_treasury_withdrawals WHERE id=NEW.basis_id AND _deleted_at IS NULL) THEN
        RAISE EXCEPTION 'treasury signing job has no active basis';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;


-- ---- Tables ----
-- CRYPTO WITHDRAWAL DOCUMENT
CREATE TABLE doc_crypto_withdrawals (
    id                UUID PRIMARY KEY,
    number            TEXT NOT NULL DEFAULT '',
    date              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    basis_type        TEXT NOT NULL DEFAULT '',
    basis_id          UUID,
    posted            BOOLEAN NOT NULL DEFAULT FALSE,
    posted_version    INT NOT NULL DEFAULT 0,
    deletion_mark     BOOLEAN NOT NULL DEFAULT FALSE,
    version           INT NOT NULL DEFAULT 1,
    attributes        JSONB DEFAULT '{}',
    description       TEXT NOT NULL DEFAULT '',
    created_by        UUID,
    updated_by        UUID,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Withdrawal-specific
    merchant_id       UUID NOT NULL,
    token_id          UUID NOT NULL,
    source_wallet_id  UUID NOT NULL,
    dest_address      TEXT NOT NULL,
    amount            NUMERIC(78,0) NOT NULL DEFAULT 0,
    network_fee       NUMERIC(78,0) NOT NULL DEFAULT 0,
    network_fee_token_id UUID REFERENCES cat_tokens(id),
    tx_hash           TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'created' CHECK (status IN ('created','signed','broadcast','confirmed','failed')),
    settlement_status TEXT NOT NULL DEFAULT 'unknown',
    settlement_checked_at TIMESTAMPTZ,
    settlement_error  TEXT NOT NULL DEFAULT '',
    settlement_claimed_by TEXT NOT NULL DEFAULT '',
    settlement_claimed_at TIMESTAMPTZ,
    settlement_lease_expires_at TIMESTAMPTZ,
    chain_result      TEXT NOT NULL DEFAULT '',
    energy_used       BIGINT NOT NULL DEFAULT 0,
    net_fee           NUMERIC(78,0) NOT NULL DEFAULT 0,
    energy_fee        NUMERIC(78,0) NOT NULL DEFAULT 0,
    net_usage         BIGINT NOT NULL DEFAULT 0,

    _txid             BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at       TIMESTAMPTZ,

    CONSTRAINT doc_crypto_withdrawals_settlement_status_check
        CHECK (settlement_status IN ('unknown','repairing','recorded','not_applicable','held'))
);
CREATE INDEX idx_doc_crypto_withdrawals_merchant ON doc_crypto_withdrawals (merchant_id);
CREATE INDEX idx_doc_crypto_withdrawals_status ON doc_crypto_withdrawals (status) WHERE status NOT IN ('confirmed','failed');
CREATE INDEX idx_doc_crypto_withdrawals_date ON doc_crypto_withdrawals (date DESC);
CREATE UNIQUE INDEX idx_crypto_withdrawals_request_basis
    ON doc_crypto_withdrawals (basis_type, basis_id)
    WHERE basis_type = 'WithdrawalRequest'
      AND basis_id IS NOT NULL
      AND deletion_mark = FALSE;
CREATE INDEX idx_crypto_withdrawals_settlement_repair
    ON doc_crypto_withdrawals (settlement_status, settlement_lease_expires_at, updated_at)
    WHERE tx_hash <> '' AND status IN ('broadcast','confirmed','failed')
      AND settlement_status IN ('unknown','repairing','held');

-- CRYPTO SWEEP DOCUMENT
CREATE TABLE doc_crypto_sweeps (
    id                UUID PRIMARY KEY,
    number            TEXT NOT NULL DEFAULT '',
    date              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    basis_type        TEXT NOT NULL DEFAULT '',
    basis_id          UUID,
    posted            BOOLEAN NOT NULL DEFAULT FALSE,
    posted_version    INT NOT NULL DEFAULT 0,
    deletion_mark     BOOLEAN NOT NULL DEFAULT FALSE,
    version           INT NOT NULL DEFAULT 1,
    attributes        JSONB DEFAULT '{}',
    description       TEXT NOT NULL DEFAULT '',
    created_by        UUID,
    updated_by        UUID,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Sweep-specific
    token_id          UUID NOT NULL,
    hot_wallet_id     UUID NOT NULL,
    total_amount      NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_fee         NUMERIC(78,0) NOT NULL DEFAULT 0,
    status            TEXT NOT NULL DEFAULT 'created' CHECK (status IN ('created','signed','broadcast','confirmed','partial_failed','held')),

    _txid             BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at       TIMESTAMPTZ,

    CONSTRAINT doc_crypto_sweeps_no_internal_attempt_basis
        CHECK (basis_type <> 'CryptoSweepAttempt')
);
CREATE INDEX idx_doc_crypto_sweeps_status ON doc_crypto_sweeps (status) WHERE status NOT IN ('confirmed','partial_failed');
CREATE INDEX idx_doc_crypto_sweeps_date ON doc_crypto_sweeps (date DESC);

-- Sweep lines (one per pool wallet)
CREATE TABLE doc_crypto_sweep_lines (
    line_id       UUID PRIMARY KEY,
    document_id   UUID NOT NULL REFERENCES doc_crypto_sweeps(id) ON DELETE CASCADE,
    line_no       INT NOT NULL DEFAULT 0,
    wallet_id     UUID NOT NULL,
    amount        NUMERIC(78,0) NOT NULL DEFAULT 0,
    network_fee   NUMERIC(78,0) NOT NULL DEFAULT 0,
    network_fee_token_id UUID REFERENCES cat_tokens(id),
    tx_hash       TEXT NOT NULL DEFAULT '',
    confirmed     BOOLEAN NOT NULL DEFAULT FALSE,
    settlement_status TEXT NOT NULL DEFAULT 'unknown',
    settlement_checked_at TIMESTAMPTZ,
    settlement_error TEXT NOT NULL DEFAULT '',
    settlement_claimed_by TEXT NOT NULL DEFAULT '',
    settlement_claimed_at TIMESTAMPTZ,
    settlement_lease_expires_at TIMESTAMPTZ,
    chain_result TEXT NOT NULL DEFAULT '',
    energy_used BIGINT NOT NULL DEFAULT 0,
    net_fee NUMERIC(78,0) NOT NULL DEFAULT 0,
    energy_fee NUMERIC(78,0) NOT NULL DEFAULT 0,
    net_usage BIGINT NOT NULL DEFAULT 0,

    CONSTRAINT doc_crypto_sweep_lines_settlement_status_check
        CHECK (settlement_status IN ('unknown','repairing','recorded','not_applicable','held'))
);
CREATE INDEX idx_doc_crypto_sweep_lines_doc ON doc_crypto_sweep_lines (document_id);
CREATE INDEX idx_crypto_sweep_lines_settlement_repair
    ON doc_crypto_sweep_lines (settlement_status, settlement_lease_expires_at, line_id)
    WHERE tx_hash <> '' AND settlement_status IN ('unknown','repairing','held');
CREATE UNIQUE INDEX idx_crypto_sweep_lines_tx_hash_unique
    ON doc_crypto_sweep_lines (tx_hash)
    WHERE tx_hash <> '';

-- WITHDRAWAL ADDRESS WHITELIST (portal self-service)
CREATE TABLE reg_withdrawal_addresses (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    merchant_id UUID NOT NULL REFERENCES cat_merchants(id),
    network_id  UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    address     TEXT NOT NULL CHECK (length(address) >= 10),
    address_key TEXT NOT NULL CHECK (BTRIM(address_key) <> ''),
    label       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _deleted_at TIMESTAMPTZ,
    UNIQUE(merchant_id, network_id, address_key)
);
CREATE INDEX idx_reg_withdrawal_addresses_merchant ON reg_withdrawal_addresses (merchant_id);

-- WITHDRAWAL REQUEST (portal → ERP approval → Vault signing)
CREATE TABLE doc_withdrawal_requests (
    id               UUID PRIMARY KEY,
    deletion_mark    BOOLEAN NOT NULL DEFAULT FALSE,
    version          INT NOT NULL DEFAULT 1,
    attributes       JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by       UUID,
    updated_by       UUID,
    number           TEXT NOT NULL DEFAULT '',
    date             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    posted           BOOLEAN NOT NULL DEFAULT FALSE,
    posted_version   INT NOT NULL DEFAULT 0,
    basis_type       TEXT NOT NULL DEFAULT '',
    basis_id         UUID,
    description      TEXT NOT NULL DEFAULT '',
    merchant_id      UUID NOT NULL REFERENCES cat_merchants(id),
    token_id         UUID NOT NULL REFERENCES cat_tokens(id),
    amount           NUMERIC(78,0) NOT NULL CHECK (amount > 0),
    dest_address     TEXT NOT NULL,
    address_id       UUID REFERENCES reg_withdrawal_addresses(id),
    status           TEXT NOT NULL DEFAULT 'pending_approval'
                     CHECK(status IN ('pending_signature','pending_approval','approved','signing','broadcast','confirmed','held','rejected','failed','partially_completed')),
    approved_by      UUID,
    approved_at      TIMESTAMPTZ,
    rejection_reason TEXT NOT NULL DEFAULT '',
	 recovery_reason TEXT NOT NULL DEFAULT '',
	 recovery_started_at TIMESTAMPTZ,
    withdrawal_id    UUID REFERENCES doc_crypto_withdrawals(id),
    _deleted_at      TIMESTAMPTZ,

    _txid            BIGINT NOT NULL DEFAULT txid_current(),
    risk_class       TEXT CHECK(risk_class IN ('low','medium','high','unknown'))
);
CREATE INDEX idx_doc_withdrawal_requests_merchant ON doc_withdrawal_requests (merchant_id);
CREATE INDEX idx_doc_withdrawal_requests_status ON doc_withdrawal_requests (status)
    WHERE status NOT IN ('confirmed','rejected','failed');
CREATE INDEX idx_doc_withdrawal_requests_basis ON doc_withdrawal_requests (basis_type, basis_id)
    WHERE basis_id IS NOT NULL;

CREATE TABLE reg_crypto_balance_reservations (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    wallet_id     UUID NOT NULL REFERENCES cat_wallets(id),
    token_id      UUID NOT NULL REFERENCES cat_tokens(id),
    amount        NUMERIC(78,0) NOT NULL CHECK (amount > 0),
    purpose       TEXT NOT NULL DEFAULT 'withdrawal',
    document_type TEXT NOT NULL,
    document_id   UUID NOT NULL,
    status        TEXT NOT NULL DEFAULT 'active'
                  CHECK (status IN ('active','consumed','released')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_crypto_balance_reservations_active_doc
    ON reg_crypto_balance_reservations (document_type, document_id)
    WHERE status = 'active';
CREATE INDEX idx_crypto_balance_reservations_active_balance
    ON reg_crypto_balance_reservations (wallet_id, token_id)
    WHERE status = 'active';

CREATE TABLE reg_crypto_signing_authorizations (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    authorization_id   TEXT NOT NULL,
    tenant_id          TEXT NOT NULL,
    merchant_id        UUID,
    purpose            TEXT NOT NULL
                       CHECK (purpose IN ('withdrawal','sweep','treasury_withdrawal','activation','native_funding')),
    schema_version     INT NOT NULL CHECK (schema_version IN (1,2)),
    network_id         UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    network_code       TEXT NOT NULL,
    chain_family       TEXT NOT NULL
                       CHECK (chain_family IN ('tron','evm','ton','solana')),
    network_identifier TEXT NOT NULL,
    source_address     TEXT NOT NULL,
    fee_payer_address  TEXT NOT NULL CHECK (fee_payer_address <> ''),
    vault_key_id       TEXT NOT NULL,
    destination_address TEXT NOT NULL,
    asset_standard     TEXT NOT NULL,
    contract_address   TEXT NOT NULL DEFAULT '',
    amount_minor_units NUMERIC(78,0) NOT NULL CHECK (amount_minor_units >= 0),
    max_network_fee_minor NUMERIC(78,0) NOT NULL DEFAULT 0 CHECK (max_network_fee_minor >= 0),
    basis_type         TEXT NOT NULL,
    basis_id           UUID NOT NULL,
    attempt            INT NOT NULL DEFAULT 1 CHECK (attempt > 0),
    generation         INT NOT NULL DEFAULT 1 CHECK (generation > 0),
    network_sequence   TEXT NOT NULL DEFAULT '',
    replacement_claimed_by TEXT NOT NULL DEFAULT '',
    replacement_claimed_at TIMESTAMPTZ,
    replacement_lease_expires_at TIMESTAMPTZ,
    expires_at         TIMESTAMPTZ NOT NULL,
    policy_version     TEXT NOT NULL
                       CHECK (policy_version = chain_family || '-v1'),
    status             TEXT NOT NULL DEFAULT 'created'
                       CHECK (status IN ('created','signed','broadcast','superseded','held','failed')),
    authorization_jws  TEXT NOT NULL,
    tx_hash            TEXT NOT NULL DEFAULT '',
    unsigned_fingerprint TEXT NOT NULL CHECK (unsigned_fingerprint <> ''),
    unsigned_envelope  JSONB NOT NULL,
    signed_envelope    JSONB,
    approval_bundle_digest TEXT NOT NULL DEFAULT '',
    approval_bundle_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_evm_signing_authorization_sequence
        CHECK (schema_version <> 2 OR chain_family <> 'evm' OR BTRIM(network_sequence) <> ''),
    CONSTRAINT reg_crypto_signing_authorizations_money_movement_bundle_check CHECK (
        (purpose IN ('withdrawal','treasury_withdrawal') AND approval_bundle_digest <> '' AND approval_bundle_json <> '{}'::jsonb)
        OR
        (purpose NOT IN ('withdrawal','treasury_withdrawal') AND approval_bundle_digest = '' AND approval_bundle_json = '{}'::jsonb)
    )
);
CREATE UNIQUE INDEX idx_crypto_signing_auth_basis_attempt_generation
    ON reg_crypto_signing_authorizations (basis_type, basis_id, purpose, attempt, generation);
CREATE UNIQUE INDEX idx_crypto_signing_auth_authorization
    ON reg_crypto_signing_authorizations (authorization_id);
CREATE UNIQUE INDEX idx_crypto_signing_auth_unsigned_fingerprint
    ON reg_crypto_signing_authorizations (network_id, unsigned_fingerprint);
CREATE INDEX idx_crypto_signing_auth_status
    ON reg_crypto_signing_authorizations (status)
    WHERE status IN ('created','signed','held');

-- Immutable description of what a persisted signed envelope is authorized to
-- change on-chain. A tx hash alone is never proof of an expected transfer.
CREATE TABLE reg_crypto_expected_chain_effects (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    signing_authorization_id UUID NOT NULL REFERENCES reg_crypto_signing_authorizations(id),
    ordinal                  INT NOT NULL CHECK (ordinal >= 0),
    network_id               UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    event_kind               TEXT NOT NULL CHECK (event_kind IN ('native_value','contract_log','internal_value')),
    source_address_key       TEXT NOT NULL CHECK (BTRIM(source_address_key) <> ''),
    destination_address_key  TEXT NOT NULL CHECK (BTRIM(destination_address_key) <> ''),
    token_id                 UUID NOT NULL REFERENCES cat_tokens(id),
    contract_address_key     TEXT NOT NULL DEFAULT '',
    amount                   NUMERIC(78,0) NOT NULL CHECK (amount >= 0),
    tx_hash                  TEXT NOT NULL CHECK (BTRIM(tx_hash) <> ''),
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (signing_authorization_id, ordinal)
);
CREATE INDEX idx_crypto_expected_effect_exact
    ON reg_crypto_expected_chain_effects (
        network_id, tx_hash, event_kind, source_address_key,
        destination_address_key, contract_address_key, amount
    );
CREATE INDEX idx_expected_transfer_canonical_tx ON reg_crypto_expected_chain_effects(network_id,(regexp_replace(lower(tx_hash),'^0x','')),event_kind);

CREATE TABLE reg_crypto_chain_effect_matches (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    expected_effect_id UUID NOT NULL REFERENCES reg_crypto_expected_chain_effects(id),
    network_id         UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    tx_hash            TEXT NOT NULL CHECK (BTRIM(tx_hash) <> ''),
    event_kind         TEXT NOT NULL CHECK (event_kind IN ('native_value','contract_log','internal_value')),
    event_index        BIGINT NOT NULL CHECK (event_index >= 0),
    matched_source     TEXT NOT NULL DEFAULT 'chain_watcher',
    matched_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (network_id, tx_hash, event_kind, event_index),
    UNIQUE (expected_effect_id)
);

CREATE TABLE reg_treasury_withdrawal_addresses (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    network_id  UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    address     TEXT NOT NULL CHECK (length(address) >= 10),
    address_key TEXT NOT NULL CHECK (BTRIM(address_key) <> ''),
    label       TEXT NOT NULL DEFAULT '',
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    created_by  UUID,
    updated_by  UUID,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _deleted_at TIMESTAMPTZ,
    UNIQUE(network_id, address_key)
);
CREATE INDEX idx_treasury_withdrawal_addresses_active
    ON reg_treasury_withdrawal_addresses (network_id, address_key)
    WHERE is_active = TRUE AND _deleted_at IS NULL;

CREATE TABLE doc_treasury_withdrawals (
    id                UUID PRIMARY KEY,
    number            TEXT NOT NULL DEFAULT '',
    date              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    basis_type        TEXT NOT NULL DEFAULT '',
    basis_id          UUID,
    posted            BOOLEAN NOT NULL DEFAULT FALSE,
    posted_version    INT NOT NULL DEFAULT 0,
    deletion_mark     BOOLEAN NOT NULL DEFAULT FALSE,
    version           INT NOT NULL DEFAULT 1,
    attributes        JSONB DEFAULT '{}',
    description       TEXT NOT NULL DEFAULT '',
    created_by        UUID,
    updated_by        UUID,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    token_id          UUID NOT NULL REFERENCES cat_tokens(id),
    source_wallet_id  UUID NOT NULL REFERENCES cat_wallets(id),
    dest_address      TEXT NOT NULL,
    address_id        UUID NOT NULL REFERENCES reg_treasury_withdrawal_addresses(id),
    amount            NUMERIC(78,0) NOT NULL CHECK (amount > 0),
    network_fee       NUMERIC(78,0) NOT NULL DEFAULT 0 CHECK (network_fee >= 0),
    network_fee_token_id UUID REFERENCES cat_tokens(id),
    tx_hash           TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'pending_approval'
                      CHECK (status IN ('pending_signature','pending_approval','approved','signing',
                     'broadcast','confirmed','held','rejected','failed')),
    approved_by       UUID,
    approved_at       TIMESTAMPTZ,
    rejection_reason  TEXT NOT NULL DEFAULT '',
    held_reason       TEXT NOT NULL DEFAULT '',
    settlement_status TEXT NOT NULL DEFAULT 'unknown',
    settlement_checked_at TIMESTAMPTZ,
    settlement_error  TEXT NOT NULL DEFAULT '',
    settlement_claimed_by TEXT NOT NULL DEFAULT '',
    settlement_claimed_at TIMESTAMPTZ,
    settlement_lease_expires_at TIMESTAMPTZ,
    chain_result      TEXT NOT NULL DEFAULT '',
    energy_used       BIGINT NOT NULL DEFAULT 0,
    net_fee           NUMERIC(78,0) NOT NULL DEFAULT 0,
    energy_fee        NUMERIC(78,0) NOT NULL DEFAULT 0,
    net_usage         BIGINT NOT NULL DEFAULT 0,

    _txid             BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at       TIMESTAMPTZ,

    CONSTRAINT chk_treasury_withdrawal_maker_checker
        CHECK (approved_by IS NULL OR created_by IS NULL OR approved_by <> created_by),
    CONSTRAINT doc_treasury_withdrawals_settlement_status_check
        CHECK (settlement_status IN ('unknown','repairing','recorded','not_applicable','held'))
);
CREATE INDEX idx_doc_treasury_withdrawals_status
    ON doc_treasury_withdrawals (status)
    WHERE status NOT IN ('confirmed','rejected','failed');
CREATE INDEX idx_doc_treasury_withdrawals_date
    ON doc_treasury_withdrawals (date DESC);
CREATE INDEX idx_doc_treasury_withdrawals_tx_hash
    ON doc_treasury_withdrawals (tx_hash)
    WHERE tx_hash <> '';
CREATE INDEX idx_treasury_withdrawals_settlement_repair
    ON doc_treasury_withdrawals (settlement_status, settlement_lease_expires_at, updated_at)
    WHERE tx_hash <> '' AND status IN ('broadcast','confirmed','failed')
      AND settlement_status IN ('unknown','repairing','held');

CREATE TABLE reg_crypto_revenue_reservations (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    merchant_id   UUID NOT NULL,
    token_id      UUID NOT NULL REFERENCES cat_tokens(id),
    fee_type      TEXT NOT NULL CHECK (fee_type IN ('processing','network','withdrawal','sweep','payout','settlement','refund')),
    amount        NUMERIC(78,0) NOT NULL CHECK (amount > 0),
    purpose       TEXT NOT NULL DEFAULT 'treasury_withdrawal',
    document_type TEXT NOT NULL,
    document_id   UUID NOT NULL,
    status        TEXT NOT NULL DEFAULT 'active'
                  CHECK (status IN ('active','consumed','released')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_crypto_revenue_reservations_active_balance
    ON reg_crypto_revenue_reservations (merchant_id, token_id, fee_type)
    WHERE status = 'active';
CREATE INDEX idx_crypto_revenue_reservations_doc
    ON reg_crypto_revenue_reservations (document_type, document_id, status);

CREATE TABLE reg_crypto_external_outflows (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    network_id         UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    token_id           UUID REFERENCES cat_tokens(id),
    source_wallet_id   UUID REFERENCES cat_wallets(id),
    tx_hash            TEXT NOT NULL,
    event_kind         TEXT NOT NULL CHECK (event_kind IN ('native_value','contract_log','internal_value')),
    event_index        BIGINT NOT NULL CHECK (event_index >= 0),
    from_address       TEXT NOT NULL,
    to_address         TEXT NOT NULL,
    source_address_key TEXT NOT NULL CHECK (BTRIM(source_address_key) <> ''),
    destination_address_key TEXT NOT NULL CHECK (BTRIM(destination_address_key) <> ''),
    contract_address   TEXT NOT NULL DEFAULT '',
    contract_address_key TEXT NOT NULL DEFAULT '',
    amount             NUMERIC(78,0) NOT NULL DEFAULT 0,
    block_number       BIGINT NOT NULL DEFAULT 0,
    block_hash         TEXT NOT NULL DEFAULT '',
    confirmations      INT NOT NULL DEFAULT 0,
    required_confs     INT NOT NULL DEFAULT 0,
    finality_status    TEXT NOT NULL DEFAULT 'observed'
                       CHECK (finality_status IN ('observed','confirmed','finalized','removed','reorged')),
    status             TEXT NOT NULL DEFAULT 'pending_review'
                       CHECK (status IN ('pending_review','classified','resolved')),
    classification     TEXT NOT NULL DEFAULT ''
                       CHECK (classification IN ('','matched_basis','unauthorized_loss','false_positive_or_reorg')),
    matched_basis_type TEXT NOT NULL DEFAULT '',
    matched_basis_id   UUID,
    classification_reason TEXT NOT NULL DEFAULT '',
    classification_evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    detected_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_observed_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    classified_by      UUID,
    classified_at      TIMESTAMPTZ,
    resolved_by        UUID,
    resolved_at        TIMESTAMPTZ,
    resolution         TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    initial_basis      JSONB CHECK(initial_basis IS NULL OR jsonb_typeof(initial_basis)='object'),
    classification_basis JSONB,
    resolution_basis   JSONB,
    UNIQUE(network_id, tx_hash, event_kind, event_index),
    CONSTRAINT uq_external_outflow_observation_identity
      UNIQUE(id,network_id,tx_hash,event_kind,event_index),
    CONSTRAINT chk_external_outflow_classification_state CHECK (
      (status='pending_review' AND classification='' AND classified_by IS NULL AND classified_at IS NULL
       AND matched_basis_type='' AND matched_basis_id IS NULL AND classification_reason='')
      OR (status IN ('classified','resolved') AND classification<>'' AND classified_by IS NOT NULL
          AND classified_at IS NOT NULL AND classification_reason<>'')
    ),
    CONSTRAINT chk_external_outflow_matched_basis CHECK (
      (classification='matched_basis' AND matched_basis_type<>'' AND matched_basis_id IS NOT NULL)
      OR (classification<>'matched_basis' AND matched_basis_type='' AND matched_basis_id IS NULL)
    ),
    CONSTRAINT chk_external_outflow_loss_identity CHECK (
      classification<>'unauthorized_loss' OR (source_wallet_id IS NOT NULL AND token_id IS NOT NULL)
    ),
    CONSTRAINT chk_external_outflow_false_positive_proof CHECK (
      classification<>'false_positive_or_reorg'
      OR (finality_status IN ('removed','reorged') AND block_hash<>'')
    ),
    CONSTRAINT chk_external_outflow_resolution CHECK (
      (status<>'resolved' AND resolved_by IS NULL AND resolved_at IS NULL AND resolution='')
      OR (status='resolved' AND resolved_by IS NOT NULL AND resolved_at IS NOT NULL
          AND resolution<>'' AND resolved_by<>classified_by)
    ),
    CONSTRAINT chk_external_outflow_evidence_object CHECK (jsonb_typeof(classification_evidence)='object')
);
CREATE INDEX idx_crypto_external_outflows_status
    ON reg_crypto_external_outflows (status, detected_at DESC);

CREATE TABLE reg_crypto_external_outflow_actions (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    external_outflow_id UUID NOT NULL REFERENCES reg_crypto_external_outflows(id),
    actor_type     TEXT NOT NULL CHECK (actor_type IN ('user','worker','system')),
    actor_id       TEXT NOT NULL CHECK (BTRIM(actor_id) <> ''),
    action         TEXT NOT NULL CHECK (action IN (
                     'detected','classified','reclassified','classification_rejected','resolved',
                     'custody_adjustment_created','custody_adjustment_posted','custody_adjustment_rejected'
                   )),
    old_state      TEXT NOT NULL,
    new_state      TEXT NOT NULL,
    reason         TEXT NOT NULL,
    evidence       JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(evidence)='object'),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decision_basis JSONB
);

CREATE TABLE doc_crypto_custody_adjustments (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    external_outflow_id UUID NOT NULL UNIQUE REFERENCES reg_crypto_external_outflows(id),
    network_id         UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    wallet_id          UUID NOT NULL REFERENCES cat_wallets(id),
    token_id           UUID NOT NULL REFERENCES cat_tokens(id),
    amount             NUMERIC(78,0) NOT NULL CHECK (amount > 0),
    status             TEXT NOT NULL DEFAULT 'pending_approval'
                       CHECK (status IN ('pending_approval','posted','rejected')),
    maker_id           UUID NOT NULL,
    decision_by        UUID,
    decision_reason    TEXT NOT NULL DEFAULT '',
    posted_version     INT NOT NULL DEFAULT 0 CHECK (posted_version >= 0),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decided_at         TIMESTAMPTZ,
    decision_basis     JSONB,
    CONSTRAINT chk_crypto_custody_adjustment_checker CHECK (
      (status='pending_approval' AND decision_by IS NULL AND decided_at IS NULL
       AND decision_reason='' AND posted_version=0)
      OR (status IN ('posted','rejected') AND decision_by IS NOT NULL
          AND decision_by<>maker_id AND decided_at IS NOT NULL AND decision_reason<>'')
    ),
    CONSTRAINT chk_crypto_custody_adjustment_posting_version CHECK (
      (status='posted' AND posted_version=1)
      OR (status IN ('pending_approval','rejected') AND posted_version=0)
    )
);

CREATE TABLE reg_crypto_reconciliation_runs (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    network_id     UUID REFERENCES cat_blockchain_networks(id),
    status         TEXT NOT NULL DEFAULT 'running'
                   CHECK (status IN ('running','completed','failed')),
    worker_id      TEXT NOT NULL DEFAULT '',
    started_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    heartbeat_at   TIMESTAMPTZ,
    lease_expires_at TIMESTAMPTZ,
    completed_at   TIMESTAMPTZ,
    error_message  TEXT NOT NULL DEFAULT '',
    wallets_checked INT NOT NULL DEFAULT 0,
    items_created   INT NOT NULL DEFAULT 0,
    items_updated   INT NOT NULL DEFAULT 0,
    items_resolved  INT NOT NULL DEFAULT 0,
    read_errors     INT NOT NULL DEFAULT 0,
    eligible_pairs INT NOT NULL DEFAULT 0,
    attempted_pairs INT NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX idx_crypto_reconciliation_single_running
    ON reg_crypto_reconciliation_runs ((1))
    WHERE status = 'running';

CREATE TABLE reg_crypto_reconciliation_items (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    run_id            UUID NOT NULL REFERENCES reg_crypto_reconciliation_runs(id) ON DELETE CASCADE,
    last_run_id       UUID NOT NULL REFERENCES reg_crypto_reconciliation_runs(id),
    network_id        UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    wallet_id         UUID NOT NULL REFERENCES cat_wallets(id),
    token_id          UUID REFERENCES cat_tokens(id),
    kind              TEXT NOT NULL CHECK (kind IN ('token_balance_mismatch','native_fee_balance_mismatch','external_outflow')),
    ledger_amount     NUMERIC(78,0) NOT NULL DEFAULT 0,
    chain_amount      NUMERIC(78,0) NOT NULL DEFAULT 0,
    delta_amount      NUMERIC(78,0) NOT NULL DEFAULT 0,
    tx_hash           TEXT NOT NULL DEFAULT '',
    event_kind        TEXT NOT NULL DEFAULT 'native_value' CHECK (event_kind IN ('native_value','contract_log','internal_value')),
    event_index       BIGINT NOT NULL DEFAULT 0 CHECK (event_index >= 0),
    status            TEXT NOT NULL DEFAULT 'open'
                      CHECK (status IN ('open','acknowledged','resolved','ignored')),
    first_seen_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    occurrence_count  INT NOT NULL DEFAULT 1 CHECK (occurrence_count >= 1),
    resolution_reason TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_by       UUID,
    resolved_at       TIMESTAMPTZ
);
CREATE INDEX idx_crypto_reconciliation_items_status
    ON reg_crypto_reconciliation_items (status, created_at DESC);
CREATE UNIQUE INDEX idx_crypto_reconciliation_balance_incident_active
    ON reg_crypto_reconciliation_items (network_id, wallet_id, token_id, kind)
    WHERE status IN ('open','acknowledged')
      AND kind IN ('token_balance_mismatch','native_fee_balance_mismatch');
CREATE UNIQUE INDEX idx_crypto_reconciliation_outflow_incident_active
    ON reg_crypto_reconciliation_items (
        network_id,
        wallet_id,
        COALESCE(token_id, '00000000-0000-0000-0000-000000000000'::uuid),
        tx_hash,event_kind,event_index
    )
    WHERE status IN ('open','acknowledged')
      AND kind = 'external_outflow';

CREATE TABLE reg_crypto_sweep_attempts (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    wallet_id            UUID NOT NULL REFERENCES cat_wallets(id),
    token_id             UUID NOT NULL REFERENCES cat_tokens(id),
    network_id           UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    sweep_id             UUID REFERENCES doc_crypto_sweeps(id),
    hot_wallet_id        UUID NOT NULL REFERENCES cat_wallets(id),
    source_address       TEXT NOT NULL,
    destination_address  TEXT NOT NULL,
    amount               NUMERIC(78,0) NOT NULL CHECK (amount > 0),
    status               TEXT NOT NULL DEFAULT 'claimed'
        CHECK (status IN ('claimed','resource_ready','signing','broadcast','posted','held','failed','expired')),
    error_message        TEXT NOT NULL DEFAULT '',
    claimed_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    broadcast_at         TIMESTAMPTZ,
    posted_at            TIMESTAMPTZ,
    expires_at           TIMESTAMPTZ NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_crypto_sweep_attempts_active_wallet_token
    ON reg_crypto_sweep_attempts (wallet_id, token_id)
    WHERE status IN ('claimed','resource_ready','signing','broadcast','held');
CREATE INDEX idx_crypto_sweep_attempts_status
    ON reg_crypto_sweep_attempts (status, updated_at);
CREATE INDEX idx_crypto_sweep_attempts_sweep
    ON reg_crypto_sweep_attempts (sweep_id, status)
    WHERE sweep_id IS NOT NULL;

CREATE TABLE reg_crypto_platform_cost_movements (
    line_id             UUID PRIMARY KEY,
    recorder_id         UUID NOT NULL,
    recorder_type       TEXT NOT NULL,
    recorder_version    INT NOT NULL,
    period              TIMESTAMPTZ NOT NULL,
    record_type         TEXT NOT NULL CHECK (record_type IN ('receipt', 'expense')),

    network_id          UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    operation_token_id  UUID REFERENCES cat_tokens(id),
    cost_token_id       UUID NOT NULL REFERENCES cat_tokens(id),
    cost_type           TEXT NOT NULL
                        CHECK (cost_type IN ('chain_resource_rent','chain_network_fee','custody_loss')),
    purpose             TEXT NOT NULL
                        CHECK (purpose IN ('sweep','withdrawal','treasury','activation','resource_rental','native_funding','custody_loss')),
    provider            TEXT NOT NULL DEFAULT '',
    external_order_id   TEXT NOT NULL DEFAULT '',
    amount              NUMERIC(78,0) NOT NULL CHECK (amount >= 0),

    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reversal_of_line_id UUID REFERENCES reg_crypto_platform_cost_movements(line_id),
    reversal_reason     TEXT NOT NULL DEFAULT '',
    CONSTRAINT chk_provider_opening_no_cost_effect CHECK (recorder_type<>'CryptoProviderBalanceOperation')
);
CREATE INDEX idx_crypto_platform_cost_movements_recorder
    ON reg_crypto_platform_cost_movements (recorder_id);
CREATE INDEX idx_crypto_platform_cost_movements_period
    ON reg_crypto_platform_cost_movements (period DESC);
CREATE UNIQUE INDEX uq_crypto_platform_cost_reversal_line
    ON reg_crypto_platform_cost_movements(reversal_of_line_id)
    WHERE reversal_of_line_id IS NOT NULL;
CREATE UNIQUE INDEX idx_crypto_platform_cost_effect
    ON reg_crypto_platform_cost_movements
       (recorder_type, recorder_id, recorder_version, record_type, cost_type, purpose, provider, external_order_id)
    WHERE reversal_of_line_id IS NULL;

CREATE TABLE doc_crypto_network_operations (
    id                    UUID PRIMARY KEY,
    number                TEXT NOT NULL DEFAULT '',
    date                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    basis_type            TEXT NOT NULL DEFAULT '',
    basis_id              UUID,
    posted                BOOLEAN NOT NULL DEFAULT FALSE,
    posted_version        INT NOT NULL DEFAULT 0,
    deletion_mark         BOOLEAN NOT NULL DEFAULT FALSE,
    version               INT NOT NULL DEFAULT 1 CHECK (version > 0),
    attributes            JSONB NOT NULL DEFAULT '{}'::jsonb,
    description           TEXT NOT NULL DEFAULT '',
    created_by            UUID,
    updated_by            UUID,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    operation_type        TEXT NOT NULL
                          CHECK (operation_type IN ('wallet_activation')),
    status                TEXT NOT NULL DEFAULT 'broadcast'
                          CHECK (status IN ('broadcast','succeeded','reverted')),
    network_id            UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    target_wallet_id      UUID NOT NULL REFERENCES cat_wallets(id),
    target_address        TEXT NOT NULL CHECK (BTRIM(target_address) <> ''),
    source_wallet_id      UUID NOT NULL REFERENCES cat_wallets(id),
    cost_token_id         UUID NOT NULL REFERENCES cat_tokens(id),
    cost_amount           NUMERIC(78,0) NOT NULL DEFAULT 0 CHECK (cost_amount >= 0),
    tx_hash               TEXT NOT NULL CHECK (BTRIM(tx_hash) <> ''),
    chain_settlement_id   UUID,
    finalized_at          TIMESTAMPTZ,
    manual_edit_allowed   BOOLEAN NOT NULL DEFAULT FALSE,
    _txid                 BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at           TIMESTAMPTZ,
    network_fee_reservation_id UUID UNIQUE REFERENCES reg_crypto_fee_reservations(id),

    CONSTRAINT chk_crypto_network_operation_wallets
        CHECK (source_wallet_id <> target_wallet_id),
    CONSTRAINT chk_crypto_network_operation_finality
        CHECK (
            (status = 'broadcast' AND chain_settlement_id IS NULL AND finalized_at IS NULL)
            OR
            (status IN ('succeeded','reverted') AND chain_settlement_id IS NOT NULL AND finalized_at IS NOT NULL)
        ),
    CONSTRAINT fk_crypto_network_operation_settlement
    FOREIGN KEY (chain_settlement_id) REFERENCES reg_crypto_chain_settlements(id)
);
CREATE UNIQUE INDEX idx_crypto_network_operations_chain_tx
    ON doc_crypto_network_operations (operation_type, network_id, tx_hash);
CREATE UNIQUE INDEX idx_crypto_network_operations_basis
    ON doc_crypto_network_operations (basis_type, basis_id)
    WHERE basis_id IS NOT NULL;
CREATE INDEX idx_crypto_network_operations_date
    ON doc_crypto_network_operations (date DESC, id DESC)
    WHERE deletion_mark = FALSE;

-- Worker-only execution state. Blockchain and accounting facts are reached
-- through network_operation_id and are never duplicated here.
CREATE TABLE reg_crypto_account_activations (
    id                         UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    wallet_id                  UUID NOT NULL REFERENCES cat_wallets(id),
    network_id                 UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    network_operation_id       UUID UNIQUE REFERENCES doc_crypto_network_operations(id),
    status                     TEXT NOT NULL DEFAULT 'claimed'
                               CHECK (status IN ('claimed','signing','broadcast','confirmed','active','held','failed')),
    error_message              TEXT NOT NULL DEFAULT '',
    broadcast_at               TIMESTAMPTZ,
    confirmed_at               TIMESTAMPTZ,
    activation_cost_reserved_minor NUMERIC(78,0) NOT NULL DEFAULT 0
                                   CHECK (activation_cost_reserved_minor >= 0),
    activation_cost_reserved_at TIMESTAMPTZ,
    activation_attempts        INT NOT NULL DEFAULT 0 CHECK (activation_attempts >= 0),
    activation_next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    activation_claimed_by      TEXT NOT NULL DEFAULT '',
    activation_claim_token     UUID,
    activation_lease_expires_at TIMESTAMPTZ,
    cost_status                TEXT NOT NULL DEFAULT 'unknown'
                               CHECK (cost_status IN ('unknown','repairing','recorded','not_applicable','held')),
    cost_checked_at            TIMESTAMPTZ,
    cost_error                 TEXT NOT NULL DEFAULT '',
    cost_claimed_by            TEXT NOT NULL DEFAULT '',
    cost_claimed_at            TIMESTAMPTZ,
    cost_lease_expires_at      TIMESTAMPTZ,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    cost_claim_token           UUID,
    CONSTRAINT chk_crypto_activation_cost_claim
 CHECK (cost_claim_token IS NULL OR (cost_status='repairing' AND cost_claimed_by<>'' AND cost_claimed_at IS NOT NULL AND cost_lease_expires_at IS NOT NULL))
);
CREATE UNIQUE INDEX idx_crypto_account_activations_active_wallet
    ON reg_crypto_account_activations (network_id, wallet_id)
    WHERE status IN ('claimed','signing','broadcast','confirmed','held');
CREATE INDEX idx_crypto_account_activations_status
    ON reg_crypto_account_activations (status, updated_at);
CREATE INDEX idx_crypto_activation_budget_reservations
    ON reg_crypto_account_activations (network_id, status)
    WHERE activation_cost_reserved_at IS NOT NULL;
CREATE INDEX idx_crypto_activation_claim
    ON reg_crypto_account_activations (activation_next_attempt_at, updated_at)
    WHERE status IN ('claimed','signing','broadcast','confirmed','held');
CREATE INDEX idx_crypto_account_activations_cost_repair
    ON reg_crypto_account_activations (cost_status, cost_lease_expires_at, updated_at)
    WHERE network_operation_id IS NOT NULL
      AND status IN ('confirmed','active','failed')
      AND cost_status IN ('unknown','repairing','held');
COMMENT ON COLUMN reg_crypto_account_activations.cost_claim_token IS 'Fresh fencing token per cost-repair attempt; an expired repairing lease drains without rewriting history.';

CREATE TABLE reg_crypto_wallet_allocation_holds (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    wallet_id   UUID NOT NULL REFERENCES cat_wallets(id),
    reason      TEXT NOT NULL CHECK (BTRIM(reason) <> ''),
    owner_type  TEXT NOT NULL CHECK (BTRIM(owner_type) <> ''),
    owner_id    TEXT NOT NULL CHECK (BTRIM(owner_id) <> ''),
    details     JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(details)='object'),
    status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved')),
    held_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    held_by     TEXT NOT NULL CHECK (BTRIM(held_by) <> ''),
    resolved_at TIMESTAMPTZ,
    resolved_by TEXT NOT NULL DEFAULT '',
    resolution  TEXT NOT NULL DEFAULT '',
    CONSTRAINT chk_wallet_hold_resolution CHECK (
      (status='open' AND resolved_at IS NULL)
      OR (status='resolved' AND resolved_at IS NOT NULL AND resolved_by<>'' AND resolution<>'')
    )
);
CREATE UNIQUE INDEX idx_crypto_wallet_holds_open_owner
    ON reg_crypto_wallet_allocation_holds (wallet_id,owner_type,owner_id)
    WHERE status='open';
CREATE INDEX idx_crypto_wallet_holds_open_wallet
    ON reg_crypto_wallet_allocation_holds (wallet_id) WHERE status='open';

CREATE TABLE reg_crypto_reconciliation_observations (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    run_id         UUID NOT NULL REFERENCES reg_crypto_reconciliation_runs(id),
    incident_id    UUID NOT NULL REFERENCES reg_crypto_reconciliation_items(id),
    network_id     UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    wallet_id      UUID NOT NULL REFERENCES cat_wallets(id),
    token_id       UUID REFERENCES cat_tokens(id),
    kind           TEXT NOT NULL CHECK (kind IN ('token_balance_mismatch','native_fee_balance_mismatch','external_outflow')),
    ledger_amount  NUMERIC(78,0) NOT NULL DEFAULT 0,
    chain_amount   NUMERIC(78,0) NOT NULL DEFAULT 0,
    delta_amount   NUMERIC(78,0) NOT NULL DEFAULT 0,
    tx_hash        TEXT NOT NULL DEFAULT '',
    event_kind     TEXT NOT NULL DEFAULT 'native_value' CHECK (event_kind IN ('native_value','contract_log','internal_value')),
    event_index    BIGINT NOT NULL DEFAULT 0 CHECK (event_index >= 0),
    block_hash     TEXT NOT NULL DEFAULT '',
    observed_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_crypto_reconciliation_observation UNIQUE (run_id, incident_id)
);
CREATE INDEX idx_crypto_reconciliation_observations_incident
    ON reg_crypto_reconciliation_observations (incident_id, observed_at DESC);

CREATE TABLE reg_crypto_external_outflow_observations (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    external_outflow_id UUID NOT NULL REFERENCES reg_crypto_external_outflows(id),
    run_id              UUID NOT NULL REFERENCES reg_crypto_reconciliation_runs(id),
    network_id          UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    tx_hash             TEXT NOT NULL CHECK (BTRIM(tx_hash)<>''),
    event_kind          TEXT NOT NULL CHECK (event_kind IN ('native_value','contract_log','internal_value')),
    event_index         BIGINT NOT NULL CHECK (event_index>=0),
    block_number        BIGINT NOT NULL DEFAULT 0 CHECK (block_number>=0),
    block_hash          TEXT NOT NULL DEFAULT '',
    confirmations       INT NOT NULL DEFAULT 0 CHECK (confirmations>=0),
    required_confs      INT NOT NULL DEFAULT 0 CHECK (required_confs>=0),
    finality_status     TEXT NOT NULL CHECK (finality_status IN ('observed','confirmed','finalized','removed','reorged')),
    observation_source  TEXT NOT NULL CHECK (BTRIM(observation_source)<>''),
    observed_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    incident_id         UUID REFERENCES reg_crypto_reconciliation_items(id),
    CONSTRAINT uq_crypto_external_outflow_observation UNIQUE (run_id,external_outflow_id),
    CONSTRAINT fk_crypto_external_outflow_observation_identity
      FOREIGN KEY (external_outflow_id,network_id,tx_hash,event_kind,event_index)
      REFERENCES reg_crypto_external_outflows(id,network_id,tx_hash,event_kind,event_index)
);
CREATE INDEX idx_crypto_external_outflow_observations_identity
    ON reg_crypto_external_outflow_observations
       (network_id,tx_hash,event_kind,event_index,observed_at DESC);

-- Every successful chain balance read is retained, including zero-delta reads
-- that do not create an incident. Resolution workflows use this evidence.
CREATE TABLE reg_crypto_reconciliation_checks (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    run_id         UUID NOT NULL REFERENCES reg_crypto_reconciliation_runs(id),
    network_id     UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    wallet_id      UUID NOT NULL REFERENCES cat_wallets(id),
    token_id       UUID NOT NULL REFERENCES cat_tokens(id),
    ledger_amount  NUMERIC(78,0) NOT NULL,
    chain_amount   NUMERIC(78,0) NOT NULL,
    delta_amount   NUMERIC(78,0) NOT NULL,
    observed_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (run_id,wallet_id,token_id)
);
CREATE INDEX idx_crypto_reconciliation_checks_resolution
    ON reg_crypto_reconciliation_checks (wallet_id,token_id,observed_at DESC);

-- Financial workflows enqueue a request in their own transaction. Workers
-- claim it later, so no blockchain RPC runs while money rows are locked.
CREATE TABLE reg_crypto_reconciliation_requests (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    network_id     UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    wallet_id      UUID NOT NULL REFERENCES cat_wallets(id),
    token_id       UUID NOT NULL REFERENCES cat_tokens(id),
    source_type    TEXT NOT NULL CHECK (BTRIM(source_type)<>''),
    source_id      UUID NOT NULL,
    status         TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','running','completed','failed')),
    attempts       INT NOT NULL DEFAULT 0 CHECK (attempts>=0),
    requested_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    claimed_at     TIMESTAMPTZ,
    completed_at   TIMESTAMPTZ,
    last_error     TEXT NOT NULL DEFAULT '',
    UNIQUE (source_type,source_id),
    CONSTRAINT chk_crypto_reconciliation_request_state CHECK (
      (status='pending' AND completed_at IS NULL)
      OR (status='running' AND claimed_at IS NOT NULL AND completed_at IS NULL)
      OR (status IN ('completed','failed') AND claimed_at IS NOT NULL AND completed_at IS NOT NULL)
    )
);
CREATE INDEX idx_crypto_reconciliation_requests_pending
    ON reg_crypto_reconciliation_requests (requested_at,id) WHERE status='pending';

CREATE TABLE cat_crypto_provider_accounts (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    code                  TEXT NOT NULL,
    name                  TEXT NOT NULL,
    provider              TEXT NOT NULL,
    network_id            UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    balance_token_id      UUID NOT NULL REFERENCES cat_tokens(id),
    external_account_ref  TEXT NOT NULL DEFAULT '',
    credentials_enc       BYTEA NOT NULL DEFAULT '\x'::bytea,
    credentials_version   INT NOT NULL DEFAULT 1,
    credentials_updated_at TIMESTAMPTZ,
    is_active             BOOLEAN NOT NULL DEFAULT TRUE,
    parent_id             UUID REFERENCES cat_crypto_provider_accounts(id),
    is_folder             BOOLEAN NOT NULL DEFAULT FALSE,
    version               INT NOT NULL DEFAULT 1,
    deletion_mark         BOOLEAN NOT NULL DEFAULT FALSE,
    attributes            JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by            UUID,
    updated_by            UUID,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid                 BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at           TIMESTAMPTZ
);
CREATE UNIQUE INDEX idx_crypto_provider_accounts_code
    ON cat_crypto_provider_accounts (code) WHERE deletion_mark = FALSE;
CREATE UNIQUE INDEX idx_crypto_provider_accounts_network_code
    ON cat_crypto_provider_accounts (network_id, code) WHERE deletion_mark = FALSE;
CREATE UNIQUE INDEX idx_crypto_provider_accounts_provider_network_ref
    ON cat_crypto_provider_accounts (provider, network_id, external_account_ref)
    WHERE is_active = TRUE AND deletion_mark = FALSE AND external_account_ref <> '';

CREATE TABLE doc_crypto_resource_rentals (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    number                TEXT NOT NULL DEFAULT '',
    date                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    basis_type            TEXT NOT NULL DEFAULT '',
    basis_id              UUID,
    posted                BOOLEAN NOT NULL DEFAULT FALSE,
    posted_version        INT NOT NULL DEFAULT 0,
    deletion_mark         BOOLEAN NOT NULL DEFAULT FALSE,
    version               INT NOT NULL DEFAULT 1,
    attributes            JSONB NOT NULL DEFAULT '{}'::jsonb,
    description           TEXT NOT NULL DEFAULT '',
    created_by            UUID,
    updated_by            UUID,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    provider_account_id   UUID NOT NULL REFERENCES cat_crypto_provider_accounts(id),
    network_id            UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    receive_wallet_id     UUID NOT NULL REFERENCES cat_wallets(id),
    cost_token_id         UUID NOT NULL REFERENCES cat_tokens(id),
    resource_type         TEXT NOT NULL DEFAULT 'energy' CHECK (resource_type IN ('energy','bandwidth')),
    rental_mode           TEXT NOT NULL DEFAULT 'fixed' CHECK (rental_mode IN ('fixed')),
    rental_period         TEXT NOT NULL DEFAULT '1H',
    requested_amount      BIGINT NOT NULL DEFAULT 0 CHECK (requested_amount >= 0),
    delegated_amount      BIGINT NOT NULL DEFAULT 0 CHECK (delegated_amount >= 0),
    reserved_amount       BIGINT NOT NULL DEFAULT 0 CHECK (reserved_amount >= 0),
    consumed_amount       BIGINT NOT NULL DEFAULT 0 CHECK (consumed_amount >= 0),
    gross_cost_amount     NUMERIC(78,0) NOT NULL DEFAULT 0 CHECK (gross_cost_amount >= 0),
    refund_amount         NUMERIC(78,0) NOT NULL DEFAULT 0 CHECK (refund_amount >= 0),
    net_cost_amount       NUMERIC(78,0) GENERATED ALWAYS AS (GREATEST(gross_cost_amount - refund_amount, 0)) STORED,
    allocated_cost_amount NUMERIC(78,0) NOT NULL DEFAULT 0 CHECK (allocated_cost_amount >= 0),
    unused_cost_amount    NUMERIC(78,0) NOT NULL DEFAULT 0 CHECK (unused_cost_amount >= 0),
    provider              TEXT NOT NULL,
    out_trade_no          TEXT NOT NULL,
    external_serial       TEXT NOT NULL DEFAULT '',
    status                TEXT NOT NULL DEFAULT 'created'
                          CHECK (status IN ('created','ordering','delegated','active','reclaiming','closed','failed','held')),
    cost_status           TEXT NOT NULL DEFAULT 'unknown'
                          CHECK (cost_status IN ('unknown','recorded','partially_refunded','refunded','held')),
    provider_balance_after NUMERIC(78,0),
    request_json          JSONB NOT NULL DEFAULT '{}'::jsonb,
    response_json         JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_message         TEXT NOT NULL DEFAULT '',
    delegated_at          TIMESTAMPTZ,
    activated_at          TIMESTAMPTZ,
    reclaim_requested_at  TIMESTAMPTZ,
    expires_at            TIMESTAMPTZ,
    closed_at             TIMESTAMPTZ,
    claimed_by            TEXT NOT NULL DEFAULT '',
    claimed_at            TIMESTAMPTZ,
    lease_expires_at      TIMESTAMPTZ,
    _txid                 BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at           TIMESTAMPTZ,
    maintenance_claim_token UUID,

    CONSTRAINT chk_crypto_resource_rental_refund CHECK (refund_amount <= gross_cost_amount),
    CONSTRAINT chk_crypto_resource_rental_allocation CHECK (allocated_cost_amount <= GREATEST(gross_cost_amount - refund_amount, 0)),
    CONSTRAINT chk_crypto_resource_rental_claim_token
 CHECK (maintenance_claim_token IS NULL OR (claimed_by<>'' AND claimed_at IS NOT NULL AND lease_expires_at IS NOT NULL))
);
CREATE UNIQUE INDEX idx_crypto_resource_rentals_out_trade_no
    ON doc_crypto_resource_rentals (provider_account_id, out_trade_no);
CREATE UNIQUE INDEX idx_crypto_resource_rentals_one_active
    ON doc_crypto_resource_rentals (provider_account_id, network_id, receive_wallet_id, resource_type)
    WHERE status IN ('created','ordering','delegated','active','reclaiming','held') AND deletion_mark = FALSE;
CREATE INDEX idx_crypto_resource_rentals_worker
    ON doc_crypto_resource_rentals (status, lease_expires_at, updated_at)
    WHERE status NOT IN ('closed','failed');
CREATE INDEX idx_crypto_resource_rentals_basis
    ON doc_crypto_resource_rentals (basis_type, basis_id);
COMMENT ON COLUMN doc_crypto_resource_rentals.maintenance_claim_token IS 'Fresh fencing token per maintenance claim; an earlier lease drains before a new token is issued.';

CREATE TABLE reg_crypto_resource_allocations (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    rental_id             UUID NOT NULL REFERENCES doc_crypto_resource_rentals(id),
    basis_type            TEXT NOT NULL,
    basis_id              UUID NOT NULL,
    purpose               TEXT NOT NULL CHECK (purpose IN ('sweep','withdrawal','treasury')),
    operation_token_id    UUID NOT NULL REFERENCES cat_tokens(id),
    source_wallet_id      UUID NOT NULL REFERENCES cat_wallets(id),
    resource_type         TEXT NOT NULL DEFAULT 'energy' CHECK (resource_type IN ('energy','bandwidth')),
    generation            INT NOT NULL DEFAULT 1 CHECK (generation > 0),
    status                TEXT NOT NULL DEFAULT 'reserved'
                          CHECK (status IN ('reserved','broadcast','confirmed','released','failed')),
    reserved_amount       BIGINT NOT NULL CHECK (reserved_amount > 0),
    actual_amount         BIGINT NOT NULL DEFAULT 0 CHECK (actual_amount >= 0),
    allocated_cost_amount NUMERIC(78,0) NOT NULL DEFAULT 0 CHECK (allocated_cost_amount >= 0),
    tx_hash               TEXT NOT NULL DEFAULT '',
    chain_result          TEXT NOT NULL DEFAULT '',
    reserved_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    broadcast_at          TIMESTAMPTZ,
    confirmed_at          TIMESTAMPTZ,
    released_at           TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_crypto_resource_allocations_basis
    ON reg_crypto_resource_allocations (basis_type, basis_id, resource_type, generation);
CREATE UNIQUE INDEX idx_crypto_resource_allocations_active_basis
    ON reg_crypto_resource_allocations (basis_type, basis_id, resource_type)
    WHERE status IN ('reserved','broadcast','confirmed');
CREATE UNIQUE INDEX idx_crypto_resource_allocations_tx
    ON reg_crypto_resource_allocations (tx_hash) WHERE tx_hash <> '';
CREATE INDEX idx_crypto_resource_allocations_rental
    ON reg_crypto_resource_allocations (rental_id, status, reserved_at);

CREATE TABLE reg_crypto_resource_allocation_events (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    allocation_id UUID NOT NULL REFERENCES reg_crypto_resource_allocations(id),
    event_type   TEXT NOT NULL,
    old_status   TEXT NOT NULL DEFAULT '',
    new_status   TEXT NOT NULL DEFAULT '',
    details      JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_crypto_resource_allocation_events_effect
    ON reg_crypto_resource_allocation_events (allocation_id, event_type, new_status);

CREATE TABLE reg_crypto_provider_balance_movements (
    line_id             UUID PRIMARY KEY,
    recorder_id         UUID NOT NULL,
    recorder_type       TEXT NOT NULL,
    recorder_version    INT NOT NULL,
    period              TIMESTAMPTZ NOT NULL,
    record_type         TEXT NOT NULL CHECK (record_type IN ('receipt','expense')),
    provider_account_id UUID NOT NULL REFERENCES cat_crypto_provider_accounts(id),
    token_id            UUID NOT NULL REFERENCES cat_tokens(id),
    operation_type      TEXT NOT NULL CHECK (operation_type IN ('opening','funding','adjustment','rental_charge','refund')),
    external_ref        TEXT NOT NULL,
    amount              NUMERIC(78,0) NOT NULL CHECK (amount > 0),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reversal_of_line_id UUID REFERENCES reg_crypto_provider_balance_movements(line_id),
    reversal_reason     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_crypto_provider_balance_movement_balance
    ON reg_crypto_provider_balance_movements (provider_account_id, token_id, period);
CREATE UNIQUE INDEX uq_crypto_provider_balance_reversal_line
    ON reg_crypto_provider_balance_movements(reversal_of_line_id)
    WHERE reversal_of_line_id IS NOT NULL;
CREATE UNIQUE INDEX idx_crypto_provider_balance_movement_effect
    ON reg_crypto_provider_balance_movements
       (recorder_type, recorder_id, record_type, operation_type, external_ref)
    WHERE reversal_of_line_id IS NULL;

CREATE TABLE reg_crypto_provider_balance_observations (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    provider_account_id UUID NOT NULL REFERENCES cat_crypto_provider_accounts(id),
    token_id            UUID NOT NULL REFERENCES cat_tokens(id),
    external_ref        TEXT NOT NULL DEFAULT '',
    ledger_amount       NUMERIC(78,0) NOT NULL,
    provider_amount     NUMERIC(78,0) NOT NULL,
    delta_amount        NUMERIC(78,0) NOT NULL,
    observed_at         TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CHECK (provider_amount >= 0 AND delta_amount=provider_amount-ledger_amount)
);

CREATE TABLE doc_crypto_provider_balance_operations (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    number              TEXT NOT NULL DEFAULT '',
    date                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    basis_type          TEXT NOT NULL DEFAULT '',
    basis_id            UUID,
    posted              BOOLEAN NOT NULL DEFAULT FALSE,
    posted_version      INT NOT NULL DEFAULT 0,
    deletion_mark       BOOLEAN NOT NULL DEFAULT FALSE,
    version             INT NOT NULL DEFAULT 1,
    attributes          JSONB NOT NULL DEFAULT '{}'::jsonb,
    description         TEXT NOT NULL DEFAULT '',
    created_by          UUID NOT NULL CHECK (created_by <> '00000000-0000-0000-0000-000000000000'),
    updated_by          UUID NOT NULL CHECK (updated_by <> '00000000-0000-0000-0000-000000000000'),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    provider_account_id UUID NOT NULL REFERENCES cat_crypto_provider_accounts(id),
    token_id            UUID NOT NULL REFERENCES cat_tokens(id),
    source_wallet_id    UUID REFERENCES cat_wallets(id),
    operation_type      TEXT NOT NULL CHECK (operation_type IN ('opening','funding','adjustment','refund')),
    record_type         TEXT NOT NULL CHECK (record_type IN ('receipt','expense')),
    amount              NUMERIC(78,0) NOT NULL CHECK (amount > 0),
    tx_hash             TEXT NOT NULL DEFAULT '',
    external_ref        TEXT NOT NULL,
    evidence            JSONB NOT NULL DEFAULT '{}'::jsonb,
    status              TEXT NOT NULL DEFAULT 'pending_approval'
                        CHECK (status IN ('pending_approval','approved','posted','rejected')),
    approved_by         UUID,
    approved_at         TIMESTAMPTZ,
    approval_reason     TEXT NOT NULL DEFAULT '',
    rejected_by         UUID,
    rejected_at         TIMESTAMPTZ,
    rejection_reason    TEXT NOT NULL DEFAULT '',
    decision_mode       TEXT NOT NULL DEFAULT '',
    decision_policy_version INT NOT NULL DEFAULT 0,
    _txid               BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at         TIMESTAMPTZ,
    provider_observation_id UUID REFERENCES reg_crypto_provider_balance_observations(id),
    CONSTRAINT chk_crypto_provider_balance_maker_checker
        CHECK (
            (status='pending_approval' AND decision_mode='' AND decision_policy_version=0)
            OR (status IN ('posted','rejected') AND decision_policy_version=1
                AND COALESCE(approved_by,rejected_by) IS NOT NULL
                AND COALESCE(approved_by,rejected_by)<>'00000000-0000-0000-0000-000000000000'
                AND ((decision_mode='independent' AND COALESCE(approved_by,rejected_by)<>created_by)
                     OR (decision_mode='admin_self' AND COALESCE(approved_by,rejected_by)=created_by)))
        ),
    CONSTRAINT chk_crypto_provider_balance_decision CHECK (
        (status='pending_approval' AND NOT posted AND posted_version=0
         AND approved_by IS NULL AND approved_at IS NULL AND approval_reason=''
         AND rejected_by IS NULL AND rejected_at IS NULL AND rejection_reason='')
        OR (status='posted' AND posted AND posted_version=1
         AND approved_by IS NOT NULL AND approved_at IS NOT NULL AND btrim(approval_reason)<>''
         AND updated_by=approved_by AND rejected_by IS NULL AND rejected_at IS NULL AND rejection_reason='')
        OR (status='rejected' AND NOT posted AND posted_version=0
         AND rejected_by IS NOT NULL
         AND rejected_by<>'00000000-0000-0000-0000-000000000000' AND rejected_at IS NOT NULL AND btrim(rejection_reason)<>''
         AND updated_by=rejected_by AND approved_by IS NULL AND approved_at IS NULL AND approval_reason='')
    ),
    CHECK (NOT deletion_mark AND _deleted_at IS NULL AND btrim(description)<>''),
    -- Funding/refund/adjustment require server-verified receipt workflows.
    -- Client assertions in evidence JSON must never authorize a money movement.
    CHECK (operation_type='opening' AND record_type='receipt' AND source_wallet_id IS NULL AND tx_hash='')
);
CREATE UNIQUE INDEX idx_crypto_provider_balance_operations_external
    ON doc_crypto_provider_balance_operations (provider_account_id, external_ref)
    WHERE deletion_mark = FALSE;
CREATE UNIQUE INDEX idx_crypto_provider_balance_operations_tx
    ON doc_crypto_provider_balance_operations (tx_hash)
    WHERE tx_hash <> '' AND deletion_mark = FALSE;
CREATE UNIQUE INDEX idx_crypto_provider_balance_opening_observation
    ON doc_crypto_provider_balance_operations (provider_observation_id)
    WHERE operation_type = 'opening'
      AND provider_observation_id IS NOT NULL
      AND status IN ('pending_approval','approved','posted')
      AND deletion_mark = FALSE;
CREATE UNIQUE INDEX idx_crypto_provider_balance_one_opening
    ON doc_crypto_provider_balance_operations (provider_account_id, token_id)
    WHERE operation_type = 'opening'
      AND status IN ('pending_approval','approved','posted')
      AND deletion_mark = FALSE;

CREATE TABLE reg_crypto_provider_balance_incidents (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    provider_account_id UUID NOT NULL REFERENCES cat_crypto_provider_accounts(id),
    token_id            UUID NOT NULL REFERENCES cat_tokens(id),
    status              TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','acknowledged','resolved','ignored')),
    ledger_amount       NUMERIC(78,0) NOT NULL,
    provider_amount     NUMERIC(78,0) NOT NULL,
    delta_amount        NUMERIC(78,0) NOT NULL,
    first_seen_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    occurrence_count    INT NOT NULL DEFAULT 1,
    resolution_reason   TEXT NOT NULL DEFAULT '',
    resolved_at         TIMESTAMPTZ,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_crypto_provider_balance_incident_active
    ON reg_crypto_provider_balance_incidents (provider_account_id, token_id)
    WHERE status IN ('open','acknowledged');

CREATE TABLE reg_money_movement_intents (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    basis_type            TEXT NOT NULL,
    basis_id              UUID NOT NULL,
    purpose               TEXT NOT NULL CHECK (purpose IN ('withdrawal','treasury_withdrawal')),
    intent_digest         TEXT NOT NULL,
    policy_version        TEXT NOT NULL,
    policy_digest         TEXT NOT NULL,
    policy_bundle_jws     TEXT NOT NULL,
    intent_jws            TEXT NOT NULL,
    risk_assessment_jws   TEXT NOT NULL,
    policy_decision_jws   TEXT NOT NULL,
    required_principals   TEXT[] NOT NULL CHECK (cardinality(required_principals) > 0),
    matched_rule_ids      TEXT[] NOT NULL DEFAULT '{}',
    expires_at            TIMESTAMPTZ NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (basis_type, basis_id),
    UNIQUE (intent_digest)
);
CREATE INDEX idx_money_movement_intents_basis
    ON reg_money_movement_intents (basis_type, basis_id);
CREATE INDEX idx_money_movement_intents_policy
    ON reg_money_movement_intents (policy_version, policy_digest);

CREATE TABLE reg_money_movement_approvals (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    basis_type              TEXT NOT NULL,
    basis_id                UUID NOT NULL,
    intent_digest           TEXT NOT NULL,
    principal_type          TEXT NOT NULL CHECK (principal_type IN ('admin','risk')),
    principal_id            TEXT NOT NULL,
    approval_jws            TEXT NOT NULL,
    policy_version          TEXT NOT NULL,
    policy_digest           TEXT NOT NULL,
    policy_decision_digest  TEXT NOT NULL,
    expires_at              TIMESTAMPTZ NOT NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (basis_type, basis_id, principal_type, policy_version),
    FOREIGN KEY (intent_digest) REFERENCES reg_money_movement_intents(intent_digest)
);
CREATE INDEX idx_money_movement_approvals_basis
    ON reg_money_movement_approvals (basis_type, basis_id);

-- Idempotent dedicated fee-wallet -> source-wallet native transfer.
CREATE TABLE doc_crypto_native_fundings (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    number                TEXT NOT NULL DEFAULT '',
    date                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    basis_type            TEXT NOT NULL,
    basis_id              UUID NOT NULL,
    attempt               INT NOT NULL CHECK (attempt > 0),
    network_id            UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    native_token_id       UUID NOT NULL REFERENCES cat_tokens(id),
    source_wallet_id      UUID NOT NULL REFERENCES cat_wallets(id),
    destination_wallet_id UUID NOT NULL REFERENCES cat_wallets(id),
    amount_minor          NUMERIC(78,0) NOT NULL CHECK (amount_minor > 0),
    fee_reservation_id    UUID REFERENCES reg_crypto_fee_reservations(id),
    signing_authorization_id UUID REFERENCES reg_crypto_signing_authorizations(id),
    unsigned_fingerprint  TEXT NOT NULL DEFAULT '',
    signed_envelope       JSONB,
    tx_hash               TEXT NOT NULL DEFAULT '',
    status                TEXT NOT NULL DEFAULT 'created'
                          CHECK (status IN ('created','reserved','signed','broadcast','solidified','settled','retry_wait','held','failed')),
    last_error            TEXT NOT NULL DEFAULT '',
    next_attempt_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    claimed_by            TEXT NOT NULL DEFAULT '',
    claimed_at            TIMESTAMPTZ,
    lease_expires_at      TIMESTAMPTZ,
    solidified_at         TIMESTAMPTZ,
    settled_at            TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    version               INT NOT NULL DEFAULT 1 CHECK (version > 0),
    CONSTRAINT chk_crypto_native_funding_wallets
        CHECK (source_wallet_id <> destination_wallet_id)
);
CREATE UNIQUE INDEX idx_crypto_native_funding_basis_attempt
    ON doc_crypto_native_fundings (basis_type, basis_id, attempt);
CREATE UNIQUE INDEX idx_crypto_native_funding_tx_hash
    ON doc_crypto_native_fundings (network_id, tx_hash) WHERE tx_hash <> '';
CREATE INDEX idx_crypto_native_funding_worker
    ON doc_crypto_native_fundings (next_attempt_at, lease_expires_at)
    WHERE status IN ('broadcast','solidified');

CREATE TABLE reg_money_movement_signing_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    basis_type TEXT NOT NULL CHECK (basis_type IN ('WithdrawalRequest','TreasuryWithdrawal')),
    basis_id UUID NOT NULL,
    intent_payload JSONB NOT NULL,
    intent_fingerprint TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','processing','completed','cancelled','needs_review')),
    attempts INT NOT NULL DEFAULT 0 CHECK (attempts>=0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    lease_owner TEXT,
    lease_expires_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    traceparent TEXT NOT NULL DEFAULT '',
    correlation_id TEXT NOT NULL DEFAULT '',
    causation_id TEXT NOT NULL DEFAULT '',
    UNIQUE (basis_type,basis_id)
);
CREATE INDEX idx_money_movement_signing_jobs_due
    ON reg_money_movement_signing_jobs(next_attempt_at,id)
    WHERE status IN ('pending','processing');

CREATE TABLE reg_money_movement_signing_job_replays (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    job_id UUID NOT NULL REFERENCES reg_money_movement_signing_jobs(id),
    actor_id UUID NOT NULL,
    previous_error TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_money_movement_signing_job_replays_job
    ON reg_money_movement_signing_job_replays(job_id,created_at);


-- ---- Triggers ----
CREATE TRIGGER trg_doc_crypto_withdrawals_cdc
    BEFORE UPDATE ON doc_crypto_withdrawals
    FOR EACH ROW EXECUTE FUNCTION fn_doc_crypto_withdrawals_cdc();

CREATE TRIGGER trg_doc_crypto_sweeps_cdc
    BEFORE UPDATE ON doc_crypto_sweeps
    FOR EACH ROW EXECUTE FUNCTION fn_doc_crypto_sweeps_cdc();

CREATE TRIGGER trg_reject_managed_withdrawal_destination
    BEFORE INSERT OR UPDATE OF network_id, address, address_key, _deleted_at
    ON reg_withdrawal_addresses
    FOR EACH ROW EXECUTE FUNCTION fn_reject_managed_withdrawal_destination();

CREATE TRIGGER trg_doc_withdrawal_requests_cdc
    BEFORE UPDATE ON doc_withdrawal_requests
    FOR EACH ROW EXECUTE FUNCTION fn_doc_withdrawal_requests_cdc();

CREATE TRIGGER trg_crypto_signing_auth_updated_at
    BEFORE UPDATE ON reg_crypto_signing_authorizations
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_signing_auth_updated_at();

CREATE TRIGGER trg_crypto_signing_auth_identity_immutable
    BEFORE UPDATE ON reg_crypto_signing_authorizations
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_signing_auth_identity_immutable();

CREATE TRIGGER trg_crypto_expected_chain_effects_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_expected_chain_effects
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_chain_effect_evidence_immutable();

CREATE TRIGGER trg_crypto_chain_effect_matches_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_chain_effect_matches
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_chain_effect_evidence_immutable();

CREATE TRIGGER trg_reject_managed_treasury_destination
    BEFORE INSERT OR UPDATE OF network_id, address, address_key, _deleted_at
    ON reg_treasury_withdrawal_addresses
    FOR EACH ROW EXECUTE FUNCTION fn_reject_managed_withdrawal_destination();

CREATE TRIGGER trg_doc_treasury_withdrawals_cdc
    BEFORE UPDATE ON doc_treasury_withdrawals
    FOR EACH ROW EXECUTE FUNCTION fn_doc_treasury_withdrawals_cdc();

CREATE TRIGGER trg_external_outflow_evidence_guard
    BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_external_outflows
    FOR EACH ROW EXECUTE FUNCTION fn_external_outflow_evidence_guard();

CREATE TRIGGER trg_external_outflow_actions_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_external_outflow_actions
    FOR EACH ROW EXECUTE FUNCTION fn_external_outflow_evidence_guard();

CREATE TRIGGER trg_crypto_custody_adjustment_guard
    BEFORE INSERT OR UPDATE OR DELETE ON doc_crypto_custody_adjustments
    FOR EACH ROW EXECUTE FUNCTION fn_external_outflow_evidence_guard();

CREATE CONSTRAINT TRIGGER trg_external_outflow_incident_state
    AFTER INSERT OR UPDATE ON reg_crypto_external_outflows
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION fn_external_outflow_incident_state_guard();

CREATE CONSTRAINT TRIGGER trg_external_outflow_reconciliation_state
    AFTER INSERT OR UPDATE ON reg_crypto_reconciliation_items
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION fn_external_outflow_incident_state_guard();

CREATE TRIGGER trg_crypto_sweep_attempts_updated_at
    BEFORE UPDATE ON reg_crypto_sweep_attempts
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE CONSTRAINT TRIGGER trg_crypto_custody_adjustment_posting_effects
    AFTER UPDATE ON doc_crypto_custody_adjustments
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_adjustment_posting_effects_guard();

CREATE TRIGGER trg_crypto_custody_wallet_effect_guard
    BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_balance_movements
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_wallet_effect_guard();

CREATE TRIGGER trg_crypto_custody_platform_effect_guard
    BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_platform_cost_movements
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_platform_effect_guard();

CREATE TRIGGER trg_crypto_network_operations_cdc
    BEFORE UPDATE ON doc_crypto_network_operations
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_network_operations_cdc();

CREATE TRIGGER trg_crypto_account_activations_updated_at
    BEFORE UPDATE ON reg_crypto_account_activations
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_reconciliation_checks_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_reconciliation_checks
    FOR EACH ROW EXECUTE FUNCTION fn_reconciliation_observations_immutable();

CREATE TRIGGER trg_reconciliation_observations_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_reconciliation_observations
    FOR EACH ROW EXECUTE FUNCTION fn_reconciliation_observations_immutable();

CREATE TRIGGER trg_external_outflow_observations_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_external_outflow_observations
    FOR EACH ROW EXECUTE FUNCTION fn_reconciliation_observations_immutable();

CREATE TRIGGER trg_crypto_provider_accounts_cdc
    BEFORE UPDATE ON cat_crypto_provider_accounts
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_provider_accounts_cdc();

CREATE TRIGGER trg_crypto_resource_rentals_cdc
    BEFORE UPDATE ON doc_crypto_resource_rentals
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_resource_rentals_cdc();

CREATE TRIGGER trg_crypto_resource_allocations_updated_at
    BEFORE UPDATE ON reg_crypto_resource_allocations
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_crypto_resource_allocation_events_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_resource_allocation_events
    FOR EACH ROW EXECUTE FUNCTION reject_crypto_resource_allocation_event_mutation();

CREATE TRIGGER trg_crypto_provider_movements_fact BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_provider_balance_movements
    FOR EACH ROW EXECUTE FUNCTION guard_crypto_provider_balance_fact();

CREATE TRIGGER trg_crypto_provider_observations_fact BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_provider_balance_observations
    FOR EACH ROW EXECUTE FUNCTION guard_crypto_provider_balance_fact();

CREATE TRIGGER trg_crypto_provider_operation_guard BEFORE INSERT OR UPDATE OR DELETE ON doc_crypto_provider_balance_operations
    FOR EACH ROW EXECUTE FUNCTION guard_crypto_provider_balance_operation();

CREATE TRIGGER trg_crypto_provider_decision_audit BEFORE UPDATE OR DELETE ON sys_event_log
    FOR EACH ROW EXECUTE FUNCTION guard_crypto_provider_decision_audit();

CREATE CONSTRAINT TRIGGER trg_crypto_provider_opening_posting AFTER INSERT OR UPDATE ON doc_crypto_provider_balance_operations
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_crypto_provider_opening_posting();

CREATE CONSTRAINT TRIGGER trg_crypto_provider_opening_movement AFTER INSERT ON reg_crypto_provider_balance_movements
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_crypto_provider_opening_posting();

CREATE CONSTRAINT TRIGGER trg_crypto_provider_opening_audit AFTER INSERT ON sys_event_log
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_crypto_provider_opening_posting('audit');

CREATE TRIGGER trg_crypto_native_fundings_updated_at
    BEFORE UPDATE ON doc_crypto_native_fundings
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

DO $$
DECLARE
    entry RECORD;
BEGIN
    FOR entry IN
        SELECT * FROM (VALUES
            ('doc_crypto_invoices', 'token_id'),
            ('doc_crypto_payments', 'token_id'),
            ('doc_withdrawal_requests', 'token_id'),
            ('doc_crypto_withdrawals', 'token_id'),
            ('doc_crypto_sweeps', 'token_id'),
            ('doc_treasury_withdrawals', 'token_id'),
            ('doc_crypto_provider_balance_operations', 'token_id'),
            ('reg_crypto_balance_movements', 'token_id'),
            ('reg_crypto_merchant_balance_movements', 'token_id'),
            ('reg_crypto_fee_movements', 'token_id'),
            ('reg_crypto_provider_balance_movements', 'token_id'),
            ('reg_crypto_platform_cost_movements', 'operation_token_id,cost_token_id')
        ) AS sources(table_name, token_columns)
    LOOP
        EXECUTE format(
            'CREATE TRIGGER %I BEFORE INSERT OR UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION fn_crypto_mark_token_identity_used(%s)',
            'trg_token_identity_' || entry.table_name,
            entry.table_name,
            (SELECT string_agg(quote_literal(part), ',')
               FROM unnest(string_to_array(entry.token_columns, ',')) AS part)
        );
    END LOOP;
END;
$$;

DO $$
DECLARE
    table_name TEXT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'reg_crypto_balance_movements',
        'reg_crypto_merchant_balance_movements',
        'reg_crypto_fee_movements',
        'reg_crypto_platform_cost_movements',
        'reg_crypto_provider_balance_movements'
    ] LOOP
        EXECUTE format(
            'CREATE TRIGGER %I BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION fn_crypto_financial_movement_immutable()',
            'trg_immutable_' || table_name, table_name
        );
    END LOOP;
END;
$$;

CREATE TRIGGER trg_money_movement_signing_job_identity_guard
    BEFORE UPDATE ON reg_money_movement_signing_jobs
    FOR EACH ROW EXECUTE FUNCTION fn_money_movement_signing_job_identity_guard();

CREATE TRIGGER trg_money_movement_signing_job_basis_guard
    BEFORE INSERT ON reg_money_movement_signing_jobs
    FOR EACH ROW EXECUTE FUNCTION fn_money_movement_signing_job_basis_guard();

CREATE TRIGGER trg_money_movement_signing_job_replays_immutable
    BEFORE UPDATE OR DELETE ON reg_money_movement_signing_job_replays
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_financial_movement_immutable();


-- ---- Seed data ----
INSERT INTO cat_crypto_provider_accounts (
    id, code, name, provider, network_id, balance_token_id, external_account_ref
)
SELECT gen_random_uuid_v7(),
       'ITRX-' || UPPER(SUBSTRING(REPLACE(n.code, '-', '') FROM 1 FOR 24)),
       'iTRX ' || n.name,
       'itrx', n.id, t.id, ''
FROM cat_blockchain_networks n
JOIN LATERAL (
    SELECT id FROM cat_tokens
    WHERE network_id = n.id AND token_standard = 'native'
      AND is_active = TRUE AND deletion_mark = FALSE
    ORDER BY id LIMIT 1
) t ON TRUE
ON CONFLICT DO NOTHING;

-- An unwatched network has no reader. Watching stops only when nothing still
-- needs the chain: processing is off, every outbound transaction has reached
-- the chain or failed, and no invoice awaits its payment or finality.
CREATE FUNCTION fn_crypto_network_watch_guard() RETURNS TRIGGER AS $$
BEGIN
 IF OLD.is_active AND NOT NEW.is_active THEN
  -- Serializes with enabling processing, which locks the network row.
  PERFORM 1 FROM sys_crypto_network_policies WHERE network_id=NEW.id FOR UPDATE;
  IF EXISTS(SELECT 1 FROM sys_crypto_network_policies WHERE network_id=NEW.id AND enabled AND _deleted_at IS NULL) THEN
   RAISE EXCEPTION 'network watching cannot stop while operations processing is enabled' USING ERRCODE='23514';
  END IF;
  IF EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations a WHERE a.network_id=NEW.id
     AND (a.status IN ('created','signed','held') OR a.status='broadcast' AND EXISTS(
      SELECT 1 FROM reg_crypto_expected_chain_effects e WHERE e.signing_authorization_id=a.id
      AND NOT EXISTS(SELECT 1 FROM reg_crypto_chain_effect_matches m WHERE m.expected_effect_id=e.id)))) THEN
   RAISE EXCEPTION 'network watching cannot stop while outbound transactions await the chain' USING ERRCODE='23514';
  END IF;
  IF EXISTS(SELECT 1 FROM doc_crypto_invoices i JOIN cat_tokens t ON t.id=i.token_id WHERE t.network_id=NEW.id
     AND NOT i.deletion_mark AND i.status IN ('created','partially_paid','paid','confirmed')) THEN
   RAISE EXCEPTION 'network watching cannot stop while invoices await payment or finality' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_crypto_network_watch_guard
    BEFORE UPDATE OF is_active ON cat_blockchain_networks
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_network_watch_guard();

CREATE FUNCTION fn_crypto_network_policy_watch_guard() RETURNS TRIGGER AS $$
BEGIN
 IF NEW.enabled AND (TG_OP='INSERT' OR NOT OLD.enabled)
    AND NOT EXISTS(SELECT 1 FROM cat_blockchain_networks WHERE id=NEW.network_id AND is_active FOR SHARE) THEN
  RAISE EXCEPTION 'operations processing requires a watched network' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_crypto_network_policy_watch_guard
    BEFORE INSERT OR UPDATE OF enabled ON sys_crypto_network_policies
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_network_policy_watch_guard();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TRIGGER trg_crypto_network_policy_watch_guard ON sys_crypto_network_policies;
DROP TRIGGER trg_crypto_network_watch_guard ON cat_blockchain_networks;
DROP FUNCTION fn_crypto_network_policy_watch_guard();
DROP FUNCTION fn_crypto_network_watch_guard();
DROP TRIGGER trg_crypto_custody_wallet_effect_guard ON reg_crypto_balance_movements;
DROP TRIGGER trg_crypto_provider_decision_audit ON sys_event_log;
DROP TRIGGER trg_crypto_provider_opening_audit ON sys_event_log;
DROP TABLE reg_money_movement_signing_job_replays;
DROP TABLE reg_money_movement_signing_jobs;
DROP TABLE doc_crypto_native_fundings;
DROP TABLE reg_money_movement_approvals;
DROP TABLE reg_money_movement_intents;
DROP TABLE reg_crypto_provider_balance_incidents;
DROP TABLE doc_crypto_provider_balance_operations;
DROP TABLE reg_crypto_provider_balance_observations;
DROP TABLE reg_crypto_provider_balance_movements;
DROP TABLE reg_crypto_resource_allocation_events;
DROP TABLE reg_crypto_resource_allocations;
DROP TABLE doc_crypto_resource_rentals;
DROP TABLE cat_crypto_provider_accounts;
DROP TABLE reg_crypto_reconciliation_requests;
DROP TABLE reg_crypto_reconciliation_checks;
DROP TABLE reg_crypto_external_outflow_observations;
DROP TABLE reg_crypto_reconciliation_observations;
DROP TABLE reg_crypto_wallet_allocation_holds;
DROP TABLE reg_crypto_account_activations;
DROP TABLE doc_crypto_network_operations;
DROP TABLE reg_crypto_platform_cost_movements;
DROP TABLE reg_crypto_sweep_attempts;
DROP TABLE reg_crypto_reconciliation_items;
DROP TABLE reg_crypto_reconciliation_runs;
DROP TABLE doc_crypto_custody_adjustments;
DROP TABLE reg_crypto_external_outflow_actions;
DROP TABLE reg_crypto_external_outflows;
DROP TABLE reg_crypto_revenue_reservations;
DROP TABLE doc_treasury_withdrawals;
DROP TABLE reg_treasury_withdrawal_addresses;
DROP TABLE reg_crypto_chain_effect_matches;
DROP TABLE reg_crypto_expected_chain_effects;
DROP TABLE reg_crypto_signing_authorizations;
DROP TABLE reg_crypto_balance_reservations;
DROP TABLE doc_withdrawal_requests;
DROP TABLE reg_withdrawal_addresses;
DROP TABLE doc_crypto_sweep_lines;
DROP TABLE doc_crypto_sweeps;
DROP TABLE doc_crypto_withdrawals;
DROP FUNCTION fn_money_movement_signing_job_basis_guard();
DROP FUNCTION fn_money_movement_signing_job_identity_guard();
DROP FUNCTION check_crypto_provider_opening_posting();
DROP FUNCTION guard_crypto_provider_decision_audit();
DROP FUNCTION guard_crypto_provider_balance_operation();
DROP FUNCTION crypto_provider_opening_admin_authority(actor uuid, lock_authority boolean);
DROP FUNCTION guard_crypto_provider_balance_fact();
DROP FUNCTION reject_crypto_resource_allocation_event_mutation();
DROP FUNCTION fn_crypto_resource_rentals_cdc();
DROP FUNCTION fn_crypto_provider_accounts_cdc();
DROP FUNCTION fn_reconciliation_observations_immutable();
DROP FUNCTION fn_crypto_wallet_hold_owner_guard();
DROP FUNCTION fn_crypto_network_operations_cdc();
DROP FUNCTION fn_crypto_custody_platform_effect_guard();
DROP FUNCTION fn_crypto_custody_wallet_effect_guard();
DROP FUNCTION fn_crypto_custody_adjustment_posting_effects_guard();
DROP FUNCTION fn_external_outflow_incident_state_guard();
DROP FUNCTION fn_external_outflow_evidence_guard();
DROP FUNCTION fn_doc_treasury_withdrawals_cdc();
DROP FUNCTION fn_crypto_chain_effect_evidence_immutable();
DROP FUNCTION fn_crypto_signing_auth_identity_immutable();
DROP FUNCTION fn_crypto_signing_auth_updated_at();
DROP FUNCTION fn_doc_withdrawal_requests_cdc();
DROP FUNCTION fn_reject_managed_withdrawal_destination();
DROP FUNCTION fn_doc_crypto_sweeps_cdc();
DROP FUNCTION fn_doc_crypto_withdrawals_cdc();
-- +goose StatementEnd
