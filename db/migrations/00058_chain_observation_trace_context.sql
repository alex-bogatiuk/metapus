-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE reg_crypto_chain_observations
    ADD COLUMN traceparent TEXT NOT NULL DEFAULT '',
    ADD COLUMN correlation_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN causation_id TEXT NOT NULL DEFAULT '';
UPDATE reg_crypto_chain_observations
SET correlation_id=network_id::text||':'||tx_hash||':'||event_kind||':'||event_index::text,
    causation_id=block_hash;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE reg_crypto_chain_observations
    DROP COLUMN traceparent,
    DROP COLUMN correlation_id,
    DROP COLUMN causation_id;
-- +goose StatementEnd
