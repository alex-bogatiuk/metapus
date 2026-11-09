-- +goose Up
-- Description: Concurrency guards for crypto outbound execution

-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

CREATE TABLE reg_crypto_balance_reservations (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    wallet_id     UUID NOT NULL REFERENCES cat_wallets(id),
    token_id      UUID NOT NULL REFERENCES cat_tokens(id),
    amount        NUMERIC(78,0) NOT NULL CHECK (amount > 0),
    purpose       TEXT NOT NULL DEFAULT 'withdrawal',
    document_type TEXT NOT NULL,
    document_id   UUID NOT NULL,
    status        TEXT NOT NULL DEFAULT 'active'
                  CHECK (status IN ('active','consumed','released')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_crypto_balance_reservations_active_doc
    ON reg_crypto_balance_reservations (document_type, document_id)
    WHERE status = 'active';

CREATE INDEX idx_crypto_balance_reservations_active_balance
    ON reg_crypto_balance_reservations (wallet_id, token_id)
    WHERE status = 'active';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

DROP TABLE IF EXISTS reg_crypto_balance_reservations;

-- +goose StatementEnd
