-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
CREATE TABLE reg_money_movement_signing_job_replays (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    job_id UUID NOT NULL REFERENCES reg_money_movement_signing_jobs(id),
    actor_id UUID NOT NULL,
    previous_error TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_money_movement_signing_job_replays_job
    ON reg_money_movement_signing_job_replays(job_id,created_at);
CREATE TRIGGER trg_money_movement_signing_job_replays_immutable
    BEFORE UPDATE OR DELETE ON reg_money_movement_signing_job_replays
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_financial_movement_immutable();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TABLE reg_money_movement_signing_job_replays;
-- +goose StatementEnd
