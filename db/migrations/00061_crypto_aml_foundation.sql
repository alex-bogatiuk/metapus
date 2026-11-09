-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- No legacy owner is guessed from a transient invoice or a current balance.
-- Application activation/backfill is a separate, explicitly verified rollout.
ALTER TABLE cat_wallets
    ADD COLUMN custody_merchant_id UUID REFERENCES cat_merchants(id),
    ADD CONSTRAINT uq_crypto_wallet_custody_scope UNIQUE(id,network_id,custody_merchant_id);
ALTER TABLE cat_tokens ADD CONSTRAINT uq_crypto_token_network_scope UNIQUE(id,network_id);
CREATE INDEX idx_crypto_wallet_custody_owner
    ON cat_wallets(custody_merchant_id,network_id,wallet_role,id)
    WHERE custody_merchant_id IS NOT NULL AND deletion_mark=FALSE;

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
CREATE TRIGGER trg_crypto_wallet_custody_owner_guard
    BEFORE INSERT OR UPDATE OR DELETE ON cat_wallets FOR EACH ROW EXECUTE FUNCTION fn_crypto_wallet_custody_owner_guard();

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
    provider TEXT NOT NULL CHECK(provider='getblock_net'),
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
    CHECK(status NOT IN ('awaiting_result','polling','succeeded') OR ticket IS NOT NULL)
);
CREATE INDEX idx_crypto_aml_jobs_due ON reg_crypto_aml_jobs(next_attempt_at,id)
    WHERE status IN ('pending_submit','awaiting_result','submitting','polling') AND _deleted_at IS NULL;

CREATE TABLE reg_crypto_aml_provider_checks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    subject_id UUID NOT NULL REFERENCES reg_crypto_aml_subjects(id),
    job_id UUID NOT NULL UNIQUE,
    provider TEXT NOT NULL CHECK(provider='getblock_net'),
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

CREATE FUNCTION fn_crypto_aml_evidence_guard() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'AML evidence is append-only' USING ERRCODE='23514';
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_checks_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_provider_checks
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();
CREATE TRIGGER trg_crypto_aml_reports_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_reports
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();
CREATE TRIGGER trg_crypto_aml_decisions_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_decisions
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();
CREATE TRIGGER trg_crypto_withdrawal_risk_snapshots_immutable BEFORE UPDATE OR DELETE ON reg_crypto_withdrawal_risk_snapshots
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

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
CREATE TRIGGER trg_crypto_aml_job_identity_guard BEFORE UPDATE ON reg_crypto_aml_jobs
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_job_identity_guard();
CREATE TRIGGER trg_crypto_aml_jobs_no_delete BEFORE DELETE ON reg_crypto_aml_jobs
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();

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
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM reg_crypto_aml_subjects) OR EXISTS(SELECT 1 FROM reg_crypto_withdrawal_risk_snapshots)
       OR EXISTS(SELECT 1 FROM reg_crypto_wallet_risk_states)
       OR EXISTS(SELECT 1 FROM cat_wallets WHERE custody_merchant_id IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot roll back AML custody/evidence with data; use a forward migration';
    END IF;
END $$;
DROP TABLE reg_crypto_withdrawal_risk_snapshots;
DROP TABLE reg_crypto_wallet_risk_states;
DROP TABLE reg_crypto_aml_decisions;
DROP TABLE reg_crypto_aml_reports;
DROP TABLE reg_crypto_aml_provider_checks;
DROP TABLE reg_crypto_aml_jobs;
DROP TABLE reg_crypto_aml_subjects;
DROP FUNCTION fn_crypto_aml_evidence_guard();
DROP FUNCTION fn_crypto_aml_job_identity_guard();
DROP FUNCTION fn_crypto_aml_subject_identity_guard();
DROP TRIGGER trg_crypto_wallet_custody_owner_guard ON cat_wallets;
DROP FUNCTION fn_crypto_wallet_custody_owner_guard();
DROP INDEX idx_crypto_wallet_custody_owner;
ALTER TABLE cat_wallets DROP CONSTRAINT uq_crypto_wallet_custody_scope, DROP COLUMN custody_merchant_id;
ALTER TABLE cat_tokens DROP CONSTRAINT uq_crypto_token_network_scope;
-- +goose StatementEnd
