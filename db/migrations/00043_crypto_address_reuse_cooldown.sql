-- +goose Up
-- Description: Address reuse cooldown, allocation history, and inbound chain-event inbox
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE reg_crypto_wallet_allocations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    wallet_id       UUID NOT NULL REFERENCES cat_wallets(id),
    invoice_id      UUID NOT NULL REFERENCES doc_crypto_invoices(id) DEFERRABLE INITIALLY DEFERRED,
    merchant_id     UUID NOT NULL REFERENCES cat_merchants(id),
    token_id        UUID NOT NULL REFERENCES cat_tokens(id),
    network_id      UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    leased_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    closed_at       TIMESTAMPTZ,
    cooldown_until  TIMESTAMPTZ,
    close_reason    TEXT NOT NULL DEFAULT ''
                    CHECK (close_reason IN ('', 'expired', 'cancelled', 'confirmed', 'orphaned_lease')),
    version         INT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid           BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at     TIMESTAMPTZ,
    CONSTRAINT chk_allocation_close CHECK (
        (closed_at IS NULL AND cooldown_until IS NULL AND close_reason = '')
        OR (closed_at IS NOT NULL AND cooldown_until IS NOT NULL AND close_reason <> '')
    ),
    CONSTRAINT chk_allocation_window CHECK (
        closed_at IS NULL OR closed_at >= leased_at
    )
);

CREATE UNIQUE INDEX idx_crypto_wallet_allocations_invoice
    ON reg_crypto_wallet_allocations (invoice_id)
    WHERE _deleted_at IS NULL;

CREATE UNIQUE INDEX idx_crypto_wallet_allocations_open_wallet
    ON reg_crypto_wallet_allocations (wallet_id)
    WHERE closed_at IS NULL AND _deleted_at IS NULL;

CREATE UNIQUE INDEX idx_crypto_wallet_allocations_wallet_leased_at
    ON reg_crypto_wallet_allocations (wallet_id, leased_at)
    WHERE _deleted_at IS NULL;

ALTER TABLE reg_crypto_wallet_allocations
    ADD CONSTRAINT excl_crypto_wallet_allocations_window
    EXCLUDE USING gist (
        wallet_id WITH =,
        tstzrange(
            leased_at,
            COALESCE(cooldown_until, 'infinity'::timestamptz),
            '[]'
        ) WITH &&
    )
    WHERE (_deleted_at IS NULL);

CREATE INDEX idx_crypto_wallet_allocations_cover
    ON reg_crypto_wallet_allocations (wallet_id, token_id, leased_at, cooldown_until)
    WHERE _deleted_at IS NULL;

CREATE INDEX idx_crypto_wallet_allocations_txid
    ON reg_crypto_wallet_allocations (_txid)
    WHERE _deleted_at IS NULL;

CREATE TRIGGER trg_crypto_wallet_allocations_updated_at
    BEFORE UPDATE ON reg_crypto_wallet_allocations
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_crypto_wallet_allocations_txid
    BEFORE UPDATE ON reg_crypto_wallet_allocations
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TABLE reg_crypto_inbound_chain_events (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    network_id       UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    tx_hash          TEXT NOT NULL,
    event_kind       TEXT NOT NULL CHECK (event_kind IN ('native_value','contract_log','internal_value')),
    event_index      BIGINT NOT NULL CHECK (event_index >= 0),
    from_address     TEXT NOT NULL DEFAULT '',
    to_address       TEXT NOT NULL,
    token_contract   TEXT NOT NULL DEFAULT '',
    token_id         UUID REFERENCES cat_tokens(id),
    wallet_id        UUID REFERENCES cat_wallets(id),
    amount           NUMERIC(78,0) NOT NULL DEFAULT 0,
    block_number     BIGINT NOT NULL DEFAULT 0,
    block_hash       TEXT NOT NULL DEFAULT '',
    block_timestamp  TIMESTAMPTZ NOT NULL,
    confirmations    INT NOT NULL DEFAULT 0,
    required_confs   INT NOT NULL DEFAULT 0,
    finality_status  TEXT NOT NULL DEFAULT 'provisional',
    canonicality     TEXT NOT NULL DEFAULT 'unknown',
    event_type       TEXT NOT NULL DEFAULT 'transfer',
    direction        TEXT NOT NULL DEFAULT 'incoming',
    payload          JSONB NOT NULL DEFAULT '{}'::jsonb,
    match_status     TEXT NOT NULL DEFAULT 'received'
                     CHECK (match_status IN (
                         'received','matched','unmatched','needs_review','ignored','poison'
                     )),
    match_reason     TEXT NOT NULL DEFAULT '',
    invoice_id       UUID REFERENCES doc_crypto_invoices(id),
    payment_id       UUID REFERENCES doc_crypto_payments(id),
    allocation_id    UUID REFERENCES reg_crypto_wallet_allocations(id),
    details          JSONB NOT NULL DEFAULT '{}'::jsonb,
    retry_count      INT NOT NULL DEFAULT 0,
    next_retry_at    TIMESTAMPTZ,
    version          INT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid            BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at      TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_crypto_inbound_chain_events_identity
    ON reg_crypto_inbound_chain_events (network_id, tx_hash, event_kind, event_index)
    WHERE _deleted_at IS NULL;

CREATE INDEX idx_crypto_inbound_chain_events_retry
    ON reg_crypto_inbound_chain_events (next_retry_at, created_at)
    WHERE match_status = 'received' AND next_retry_at IS NOT NULL;

CREATE INDEX idx_crypto_inbound_chain_events_replay
    ON reg_crypto_inbound_chain_events (match_status, match_reason, created_at)
    WHERE match_status = 'unmatched';

CREATE INDEX idx_crypto_inbound_chain_events_single_review
    ON reg_crypto_inbound_chain_events (match_status, created_at, id)
    WHERE match_reason IN ('additional_single_payment','retired_deposit_address') AND _deleted_at IS NULL;

CREATE INDEX idx_crypto_inbound_chain_events_txid
    ON reg_crypto_inbound_chain_events (_txid)
    WHERE _deleted_at IS NULL;

CREATE TRIGGER trg_crypto_inbound_chain_events_updated_at
    BEFORE UPDATE ON reg_crypto_inbound_chain_events
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_crypto_inbound_chain_events_txid
    BEFORE UPDATE ON reg_crypto_inbound_chain_events
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

DROP TRIGGER IF EXISTS trg_crypto_inbound_chain_events_txid ON reg_crypto_inbound_chain_events;
DROP TRIGGER IF EXISTS trg_crypto_inbound_chain_events_updated_at ON reg_crypto_inbound_chain_events;
DROP TABLE IF EXISTS reg_crypto_inbound_chain_events;

DROP TRIGGER IF EXISTS trg_crypto_wallet_allocations_txid ON reg_crypto_wallet_allocations;
DROP TRIGGER IF EXISTS trg_crypto_wallet_allocations_updated_at ON reg_crypto_wallet_allocations;
DROP TABLE IF EXISTS reg_crypto_wallet_allocations;

-- +goose StatementEnd
