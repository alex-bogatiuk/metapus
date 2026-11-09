-- +goose Up
-- Description: Production treasury withdrawals and out-of-band crypto reconciliation

-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

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

CREATE TRIGGER trg_reject_managed_treasury_destination
    BEFORE INSERT OR UPDATE OF network_id, address, address_key, _deleted_at
    ON reg_treasury_withdrawal_addresses
    FOR EACH ROW EXECUTE FUNCTION fn_reject_managed_withdrawal_destination();

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
                      CHECK (status IN ('pending_approval','approved','signing','broadcast','confirmed','held','rejected','failed')),
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

CREATE TRIGGER trg_doc_treasury_withdrawals_cdc
    BEFORE UPDATE ON doc_treasury_withdrawals
    FOR EACH ROW EXECUTE FUNCTION fn_doc_treasury_withdrawals_cdc();

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
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
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

CREATE TRIGGER trg_external_outflow_evidence_guard
    BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_external_outflows
    FOR EACH ROW EXECUTE FUNCTION fn_external_outflow_evidence_guard();
CREATE TRIGGER trg_external_outflow_actions_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_external_outflow_actions
    FOR EACH ROW EXECUTE FUNCTION fn_external_outflow_evidence_guard();
CREATE TRIGGER trg_crypto_custody_adjustment_guard
    BEFORE INSERT OR UPDATE OR DELETE ON doc_crypto_custody_adjustments
    FOR EACH ROW EXECUTE FUNCTION fn_external_outflow_evidence_guard();

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
    read_errors     INT NOT NULL DEFAULT 0
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

CREATE CONSTRAINT TRIGGER trg_external_outflow_incident_state
    AFTER INSERT OR UPDATE ON reg_crypto_external_outflows
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION fn_external_outflow_incident_state_guard();
CREATE CONSTRAINT TRIGGER trg_external_outflow_reconciliation_state
    AFTER INSERT OR UPDATE ON reg_crypto_reconciliation_items
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION fn_external_outflow_incident_state_guard();

INSERT INTO auth_permissions (code, name, description, resource, action) VALUES
    ('document:treasury_withdrawal:read', 'Чтение казначейских выводов', 'View treasury withdrawals', 'treasury_withdrawal', 'read'),
    ('document:treasury_withdrawal:create', 'Создание казначейских выводов', 'Create treasury withdrawals', 'treasury_withdrawal', 'create'),
    ('document:treasury_withdrawal:update', 'Изменение казначейских выводов', 'Update treasury withdrawals', 'treasury_withdrawal', 'update'),
    ('document:treasury_withdrawal:delete', 'Удаление казначейских выводов', 'Delete treasury withdrawals', 'treasury_withdrawal', 'delete'),
    ('document:treasury_withdrawal:post', 'Проведение казначейских выводов', 'Post and approve treasury withdrawals', 'treasury_withdrawal', 'post'),
    ('document:treasury_withdrawal:unpost', 'Отмена проведения казначейских выводов', 'Unpost treasury withdrawals', 'treasury_withdrawal', 'unpost'),
    ('crypto_reconciliation:read', 'Чтение криптосверки', 'Read external outflows and evidence', 'crypto_reconciliation', 'read'),
    ('crypto_reconciliation:classify', 'Классификация outflow', 'Classify external outflows', 'crypto_reconciliation', 'classify'),
    ('crypto_reconciliation:resolve', 'Разрешение outflow', 'Resolve classified external outflows', 'crypto_reconciliation', 'resolve'),
    ('crypto_custody_adjustment:approve', 'Подтверждение custody loss', 'Approve custody loss adjustments', 'crypto_custody_adjustment', 'approve')
ON CONFLICT (code) DO NOTHING;

INSERT INTO auth_role_permissions (role_id, permission_id)
SELECT 'b0000000-0000-0000-0000-000000000001', id
FROM auth_permissions WHERE resource IN ('treasury_withdrawal','crypto_reconciliation','crypto_custody_adjustment')
ON CONFLICT DO NOTHING;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

DELETE FROM auth_role_permissions WHERE permission_id IN (
    SELECT id FROM auth_permissions WHERE resource IN ('treasury_withdrawal','crypto_reconciliation','crypto_custody_adjustment')
);
DELETE FROM auth_permissions WHERE resource IN ('treasury_withdrawal','crypto_reconciliation','crypto_custody_adjustment');

DROP TABLE IF EXISTS reg_crypto_reconciliation_items;
DROP TABLE IF EXISTS reg_crypto_reconciliation_runs;
DROP TABLE IF EXISTS doc_crypto_custody_adjustments;
DROP TABLE IF EXISTS reg_crypto_external_outflow_actions;
DROP TABLE IF EXISTS reg_crypto_external_outflows;
DROP FUNCTION IF EXISTS fn_external_outflow_incident_state_guard();
DROP FUNCTION IF EXISTS fn_external_outflow_evidence_guard();
DROP TABLE IF EXISTS reg_crypto_revenue_reservations;
DROP TABLE IF EXISTS doc_treasury_withdrawals;
DROP FUNCTION IF EXISTS fn_doc_treasury_withdrawals_cdc();
DROP TABLE IF EXISTS reg_treasury_withdrawal_addresses;

-- +goose StatementEnd
