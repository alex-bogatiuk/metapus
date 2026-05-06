-- +goose Up
-- Description: Chain watcher state persistence

-- +goose StatementBegin
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- ═══════════════════════════════════════════════════════════════════════
-- CHAIN WATCHER STATE (checkpoint persistence)
-- ═══════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS sys_chain_watcher_state (
    network_id      UUID PRIMARY KEY,
    last_block      BIGINT NOT NULL DEFAULT 0,
    last_timestamp  BIGINT NOT NULL DEFAULT 0,
    fingerprint     TEXT NOT NULL DEFAULT '',
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS sys_chain_watcher_state CASCADE;
