-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP INDEX idx_crypto_inbound_chain_events_replay;
CREATE INDEX idx_crypto_inbound_chain_events_replay_due
    ON reg_crypto_inbound_chain_events (match_reason, next_retry_at, created_at, id)
    WHERE match_status = 'unmatched' AND _deleted_at IS NULL;
ALTER TABLE reg_crypto_reconciliation_runs
    ADD COLUMN eligible_pairs INT NOT NULL DEFAULT 0,
    ADD COLUMN attempted_pairs INT NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE reg_crypto_reconciliation_runs
    DROP COLUMN attempted_pairs,
    DROP COLUMN eligible_pairs;
DROP INDEX idx_crypto_inbound_chain_events_replay_due;
CREATE INDEX idx_crypto_inbound_chain_events_replay
    ON reg_crypto_inbound_chain_events (match_status, match_reason, created_at)
    WHERE match_status = 'unmatched';
-- +goose StatementEnd
