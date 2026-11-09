-- +goose Up
-- Description: Chain watcher state persistence

-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- ═══════════════════════════════════════════════════════════════════════
-- CHAIN WATCHER STATE (checkpoint persistence)
-- ═══════════════════════════════════════════════════════════════════════

CREATE TABLE sys_chain_watcher_state (
    network_id      UUID NOT NULL,
    watcher_key     TEXT NOT NULL DEFAULT 'trc20_primary',
    last_block      BIGINT NOT NULL DEFAULT 0,
    last_timestamp  BIGINT NOT NULL DEFAULT 0,
    fingerprint     TEXT NOT NULL DEFAULT '',
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (network_id, watcher_key)
);

-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS sys_chain_watcher_state CASCADE;
