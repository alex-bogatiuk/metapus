-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE sys_chain_watcher_state
    ADD COLUMN lease_owner TEXT,
    ADD COLUMN fencing_token BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN lease_expires_at TIMESTAMPTZ;
CREATE TABLE sys_chain_watcher_rewinds (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    network_id UUID NOT NULL,
    watcher_key TEXT NOT NULL,
    fencing_token BIGINT NOT NULL,
    old_block BIGINT NOT NULL,
    new_block BIGINT NOT NULL,
    evidence TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TABLE sys_chain_watcher_rewinds;
ALTER TABLE sys_chain_watcher_state
    DROP COLUMN lease_expires_at,
    DROP COLUMN fencing_token,
    DROP COLUMN lease_owner;
-- +goose StatementEnd
