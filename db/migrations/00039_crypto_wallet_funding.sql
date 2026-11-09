-- +goose Up
-- Description: Audited wallet funding receipts

-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

CREATE TABLE reg_crypto_wallet_fundings (
    id              UUID PRIMARY KEY,
    network_id      UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    wallet_id       UUID NOT NULL REFERENCES cat_wallets(id),
    token_id        UUID NOT NULL REFERENCES cat_tokens(id),
    tx_hash         TEXT NOT NULL,
    from_address    TEXT NOT NULL,
    to_address      TEXT NOT NULL,
    amount          NUMERIC(78,0) NOT NULL CHECK (amount > 0),
    block_number    BIGINT NOT NULL CHECK (block_number > 0),
    confirmations   INT NOT NULL CHECK (confirmations >= 1),
    block_timestamp TIMESTAMPTZ NOT NULL,
    recorded_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_crypto_wallet_fundings_tx_hash UNIQUE (tx_hash)
);

CREATE INDEX idx_crypto_wallet_fundings_wallet
    ON reg_crypto_wallet_fundings (wallet_id, token_id, block_timestamp DESC);

CREATE OR REPLACE FUNCTION fn_crypto_wallet_fundings_immutable() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'crypto wallet funding receipts are immutable';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_crypto_wallet_fundings_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_wallet_fundings
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_wallet_fundings_immutable();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

DELETE FROM reg_crypto_balance_movements
WHERE recorder_type = 'CryptoWalletFunding';

DROP TRIGGER IF EXISTS trg_crypto_wallet_fundings_immutable ON reg_crypto_wallet_fundings;
DROP FUNCTION IF EXISTS fn_crypto_wallet_fundings_immutable();
DROP INDEX IF EXISTS idx_crypto_wallet_fundings_wallet;
DROP TABLE IF EXISTS reg_crypto_wallet_fundings;

-- +goose StatementEnd
