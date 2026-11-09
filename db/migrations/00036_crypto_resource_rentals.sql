-- +goose Up
-- Description: Shared TRON resource rentals, provider prepayments, and analytical allocations

-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

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

CREATE TRIGGER trg_crypto_provider_accounts_cdc
    BEFORE UPDATE ON cat_crypto_provider_accounts
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_provider_accounts_cdc();

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

    CONSTRAINT chk_crypto_resource_rental_refund CHECK (refund_amount <= gross_cost_amount),
    CONSTRAINT chk_crypto_resource_rental_allocation CHECK (allocated_cost_amount <= GREATEST(gross_cost_amount - refund_amount, 0))
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

CREATE TRIGGER trg_crypto_resource_rentals_cdc
    BEFORE UPDATE ON doc_crypto_resource_rentals
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_resource_rentals_cdc();

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
CREATE TRIGGER trg_crypto_resource_allocations_updated_at
    BEFORE UPDATE ON reg_crypto_resource_allocations
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

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

CREATE OR REPLACE FUNCTION reject_crypto_resource_allocation_event_mutation() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'crypto resource allocation events are immutable';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_crypto_resource_allocation_events_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_resource_allocation_events
    FOR EACH ROW EXECUTE FUNCTION reject_crypto_resource_allocation_event_mutation();

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
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_crypto_provider_balance_movement_effect
    ON reg_crypto_provider_balance_movements
       (recorder_type, recorder_id, record_type, operation_type, external_ref);
CREATE INDEX idx_crypto_provider_balance_movement_balance
    ON reg_crypto_provider_balance_movements (provider_account_id, token_id, period);

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

ALTER TABLE doc_crypto_provider_balance_operations
    ADD COLUMN provider_observation_id UUID
        REFERENCES reg_crypto_provider_balance_observations(id);

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
CREATE TRIGGER trg_crypto_provider_movements_fact BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_provider_balance_movements
    FOR EACH ROW EXECUTE FUNCTION guard_crypto_provider_balance_fact();
CREATE TRIGGER trg_crypto_provider_observations_fact BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_provider_balance_observations
    FOR EACH ROW EXECUTE FUNCTION guard_crypto_provider_balance_fact();

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
CREATE TRIGGER trg_crypto_provider_operation_guard BEFORE INSERT OR UPDATE OR DELETE ON doc_crypto_provider_balance_operations
    FOR EACH ROW EXECUTE FUNCTION guard_crypto_provider_balance_operation();

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
CREATE TRIGGER trg_crypto_provider_decision_audit BEFORE UPDATE OR DELETE ON sys_event_log
    FOR EACH ROW EXECUTE FUNCTION guard_crypto_provider_decision_audit();

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
CREATE CONSTRAINT TRIGGER trg_crypto_provider_opening_posting AFTER INSERT OR UPDATE ON doc_crypto_provider_balance_operations
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_crypto_provider_opening_posting();
CREATE CONSTRAINT TRIGGER trg_crypto_provider_opening_movement AFTER INSERT ON reg_crypto_provider_balance_movements
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_crypto_provider_opening_posting();
CREATE CONSTRAINT TRIGGER trg_crypto_provider_opening_audit AFTER INSERT ON sys_event_log
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_crypto_provider_opening_posting('audit');

-- An opening acknowledges a pre-existing provider asset, never a wallet
-- transfer, merchant credit, fee or new platform expenditure.
ALTER TABLE reg_crypto_balance_movements ADD CONSTRAINT chk_provider_opening_no_wallet_effect CHECK (recorder_type<>'CryptoProviderBalanceOperation');
ALTER TABLE reg_crypto_merchant_balance_movements ADD CONSTRAINT chk_provider_opening_no_merchant_effect CHECK (recorder_type<>'CryptoProviderBalanceOperation');
ALTER TABLE reg_crypto_fee_movements ADD CONSTRAINT chk_provider_opening_no_fee_effect CHECK (recorder_type<>'CryptoProviderBalanceOperation');
ALTER TABLE reg_crypto_platform_cost_movements ADD CONSTRAINT chk_provider_opening_no_cost_effect CHECK (recorder_type<>'CryptoProviderBalanceOperation');

INSERT INTO auth_permissions (code,name,description,resource,action) VALUES
 ('document:crypto_provider_balance_operation:read','Чтение баланса провайдера','Read provider balance operations','document:crypto_provider_balance_operation','read'),
 ('document:crypto_provider_balance_operation:create','Подготовка opening провайдера','Prepare an evidenced opening with fresh MFA','document:crypto_provider_balance_operation','create'),
 ('document:crypto_provider_balance_operation:approve','Одобрение opening провайдера','Independent approval or audited system-admin self approval with fresh MFA','document:crypto_provider_balance_operation','approve'),
 ('document:crypto_provider_balance_operation:reject','Отклонение opening провайдера','Independent rejection or audited system-admin self rejection with fresh MFA','document:crypto_provider_balance_operation','reject')
ON CONFLICT (code) DO NOTHING;
INSERT INTO auth_role_permissions (role_id,permission_id)
SELECT 'b0000000-0000-0000-0000-000000000001',id FROM auth_permissions WHERE resource='document:crypto_provider_balance_operation'
ON CONFLICT DO NOTHING;

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

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

DELETE FROM auth_role_permissions WHERE permission_id IN (SELECT id FROM auth_permissions WHERE resource='document:crypto_provider_balance_operation');
DELETE FROM auth_permissions WHERE resource='document:crypto_provider_balance_operation';
ALTER TABLE reg_crypto_balance_movements DROP CONSTRAINT IF EXISTS chk_provider_opening_no_wallet_effect;
ALTER TABLE reg_crypto_merchant_balance_movements DROP CONSTRAINT IF EXISTS chk_provider_opening_no_merchant_effect;
ALTER TABLE reg_crypto_fee_movements DROP CONSTRAINT IF EXISTS chk_provider_opening_no_fee_effect;
ALTER TABLE reg_crypto_platform_cost_movements DROP CONSTRAINT IF EXISTS chk_provider_opening_no_cost_effect;
DROP TABLE IF EXISTS doc_crypto_provider_balance_operations CASCADE;
DROP TABLE IF EXISTS reg_crypto_provider_balance_incidents;
DROP TABLE IF EXISTS reg_crypto_provider_balance_observations;
DROP TABLE IF EXISTS reg_crypto_provider_balance_movements;
DROP TRIGGER IF EXISTS trg_crypto_provider_opening_audit ON sys_event_log;
DROP FUNCTION IF EXISTS check_crypto_provider_opening_posting();
DROP FUNCTION IF EXISTS guard_crypto_provider_balance_operation();
DROP FUNCTION IF EXISTS crypto_provider_opening_admin_authority(UUID, BOOLEAN);
DROP TRIGGER IF EXISTS trg_crypto_provider_decision_audit ON sys_event_log;
DROP FUNCTION IF EXISTS guard_crypto_provider_decision_audit();
DROP FUNCTION IF EXISTS guard_crypto_provider_balance_fact();
DROP TRIGGER IF EXISTS trg_crypto_resource_allocation_events_immutable ON reg_crypto_resource_allocation_events;
DROP FUNCTION IF EXISTS reject_crypto_resource_allocation_event_mutation();
DROP TABLE IF EXISTS reg_crypto_resource_allocation_events;
DROP TABLE IF EXISTS reg_crypto_resource_allocations;
DROP TABLE IF EXISTS doc_crypto_resource_rentals;
DROP FUNCTION IF EXISTS fn_crypto_resource_rentals_cdc();
DROP TABLE IF EXISTS cat_crypto_provider_accounts;
DROP FUNCTION IF EXISTS fn_crypto_provider_accounts_cdc();

-- +goose StatementEnd
