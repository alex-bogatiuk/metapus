-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE reg_money_movement_signing_jobs
    ADD COLUMN traceparent TEXT NOT NULL DEFAULT '',
    ADD COLUMN correlation_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN causation_id TEXT NOT NULL DEFAULT '';
UPDATE reg_money_movement_signing_jobs
SET correlation_id=COALESCE(intent_payload->>'intent_id',''),
    causation_id=basis_id::text;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE reg_money_movement_signing_jobs
    DROP COLUMN traceparent,
    DROP COLUMN correlation_id,
    DROP COLUMN causation_id;
-- +goose StatementEnd
