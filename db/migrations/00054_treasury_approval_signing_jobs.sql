-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE doc_treasury_withdrawals
    DROP CONSTRAINT doc_treasury_withdrawals_status_check;
ALTER TABLE doc_treasury_withdrawals
    ADD CONSTRAINT doc_treasury_withdrawals_status_check
    CHECK (status IN ('pending_signature','pending_approval','approved','signing',
                     'broadcast','confirmed','held','rejected','failed'));
ALTER TABLE reg_money_movement_signing_jobs
    DROP CONSTRAINT reg_money_movement_signing_jobs_basis_id_fkey;
ALTER TABLE reg_money_movement_signing_jobs
    DROP CONSTRAINT reg_money_movement_signing_jobs_basis_type_check;
ALTER TABLE reg_money_movement_signing_jobs
    ADD CONSTRAINT reg_money_movement_signing_jobs_basis_type_check
    CHECK (basis_type IN ('WithdrawalRequest','TreasuryWithdrawal'));
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
CREATE TRIGGER trg_money_movement_signing_job_basis_guard
    BEFORE INSERT ON reg_money_movement_signing_jobs
    FOR EACH ROW EXECUTE FUNCTION fn_money_movement_signing_job_basis_guard();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TRIGGER trg_money_movement_signing_job_basis_guard ON reg_money_movement_signing_jobs;
DROP FUNCTION fn_money_movement_signing_job_basis_guard();
DELETE FROM reg_money_movement_signing_jobs WHERE basis_type='TreasuryWithdrawal';
ALTER TABLE reg_money_movement_signing_jobs
    DROP CONSTRAINT reg_money_movement_signing_jobs_basis_type_check;
ALTER TABLE reg_money_movement_signing_jobs
    ADD CONSTRAINT reg_money_movement_signing_jobs_basis_type_check
    CHECK (basis_type='WithdrawalRequest');
ALTER TABLE reg_money_movement_signing_jobs
    ADD CONSTRAINT reg_money_movement_signing_jobs_basis_id_fkey
    FOREIGN KEY (basis_id) REFERENCES doc_withdrawal_requests(id);
ALTER TABLE doc_treasury_withdrawals
    DROP CONSTRAINT doc_treasury_withdrawals_status_check;
ALTER TABLE doc_treasury_withdrawals
    ADD CONSTRAINT doc_treasury_withdrawals_status_check
    CHECK (status IN ('pending_approval','approved','signing','broadcast',
                     'confirmed','held','rejected','failed'));
-- +goose StatementEnd
