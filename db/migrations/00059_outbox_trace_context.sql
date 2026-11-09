-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE sys_outbox
    ADD COLUMN traceparent TEXT NOT NULL DEFAULT '',
    ADD COLUMN correlation_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN causation_id TEXT NOT NULL DEFAULT '';
ALTER TABLE sys_outbox_dlq
    ADD COLUMN traceparent TEXT NOT NULL DEFAULT '',
    ADD COLUMN correlation_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN causation_id TEXT NOT NULL DEFAULT '';
ALTER TABLE sys_merchant_events
    ADD COLUMN traceparent TEXT NOT NULL DEFAULT '',
    ADD COLUMN correlation_id TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE sys_merchant_events
    DROP COLUMN traceparent,
    DROP COLUMN correlation_id;
ALTER TABLE sys_outbox_dlq
    DROP COLUMN traceparent,
    DROP COLUMN correlation_id,
    DROP COLUMN causation_id;
ALTER TABLE sys_outbox
    DROP COLUMN traceparent,
    DROP COLUMN correlation_id,
    DROP COLUMN causation_id;
-- +goose StatementEnd
