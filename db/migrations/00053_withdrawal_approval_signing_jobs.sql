-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE doc_withdrawal_requests
    DROP CONSTRAINT doc_withdrawal_requests_status_check;
ALTER TABLE doc_withdrawal_requests
    ADD CONSTRAINT doc_withdrawal_requests_status_check
    CHECK (status IN ('pending_signature','pending_approval','approved','signing',
                     'broadcast','confirmed','held','rejected','failed'));
CREATE TABLE reg_money_movement_signing_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    basis_type TEXT NOT NULL CHECK (basis_type='WithdrawalRequest'),
    basis_id UUID NOT NULL REFERENCES doc_withdrawal_requests(id),
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
    UNIQUE (basis_type,basis_id)
);
CREATE INDEX idx_money_movement_signing_jobs_due
    ON reg_money_movement_signing_jobs(next_attempt_at,id)
    WHERE status IN ('pending','processing');
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
CREATE TRIGGER trg_money_movement_signing_job_identity_guard
    BEFORE UPDATE ON reg_money_movement_signing_jobs
    FOR EACH ROW EXECUTE FUNCTION fn_money_movement_signing_job_identity_guard();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TABLE reg_money_movement_signing_jobs;
DROP FUNCTION fn_money_movement_signing_job_identity_guard();
ALTER TABLE doc_withdrawal_requests
    DROP CONSTRAINT doc_withdrawal_requests_status_check;
ALTER TABLE doc_withdrawal_requests
    ADD CONSTRAINT doc_withdrawal_requests_status_check
    CHECK (status IN ('pending_approval','approved','signing','broadcast',
                     'confirmed','held','rejected','failed'));
-- +goose StatementEnd
