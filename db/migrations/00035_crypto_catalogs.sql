-- +goose Up
-- Description: Crypto processing catalogs (blockchain networks + tokens)
-- +goose StatementBegin
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- ═══════════════════════════════════════════════════════════════════════════
-- Blockchain Networks catalog (Справочник «Блокчейн-сети»)
-- ═══════════════════════════════════════════════════════════════════════════

CREATE TABLE cat_blockchain_networks (
    -- Base fields
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    deletion_mark BOOLEAN     NOT NULL DEFAULT FALSE,
    version       INT         NOT NULL DEFAULT 1,
    attributes    JSONB       DEFAULT '{}',

    -- CDC
    _deleted_at TIMESTAMPTZ,
    _txid       BIGINT DEFAULT txid_current(),

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Catalog fields
    code      VARCHAR(50)  NOT NULL,
    name      VARCHAR(255) NOT NULL,
    parent_id UUID REFERENCES cat_blockchain_networks(id),
    is_folder BOOLEAN      NOT NULL DEFAULT FALSE,

    -- Network-specific fields
    chain_id              VARCHAR(50)  NOT NULL,     -- "tron", "ethereum", "bitcoin"
    native_token_symbol   VARCHAR(10)  NOT NULL,     -- "TRX", "ETH", "BTC"
    native_decimals       INT          NOT NULL,     -- 6, 18, 8
    confirmations_needed  INT          NOT NULL DEFAULT 1,
    block_time_seconds    INT          NOT NULL DEFAULT 10,
    explorer_url          TEXT         NOT NULL DEFAULT '',
    is_active             BOOLEAN      NOT NULL DEFAULT TRUE,

    CONSTRAINT chk_native_decimals      CHECK (native_decimals >= 0 AND native_decimals <= 18),
    CONSTRAINT chk_confirmations_needed CHECK (confirmations_needed >= 1)
);

-- Unique indexes
CREATE UNIQUE INDEX idx_cat_blockchain_networks_code     ON cat_blockchain_networks (code) WHERE deletion_mark = FALSE;
CREATE UNIQUE INDEX idx_cat_blockchain_networks_chain_id ON cat_blockchain_networks (chain_id) WHERE deletion_mark = FALSE;

-- Search indexes
CREATE INDEX idx_cat_blockchain_networks_name   ON cat_blockchain_networks USING gin (name gin_trgm_ops);
CREATE INDEX idx_cat_blockchain_networks_parent ON cat_blockchain_networks (parent_id) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_blockchain_networks_attrs  ON cat_blockchain_networks USING gin (attributes);

-- CDC indexes & triggers
CREATE INDEX idx_cat_blockchain_networks_txid ON cat_blockchain_networks (_txid) WHERE _deleted_at IS NULL;

CREATE TRIGGER trg_cat_blockchain_networks_txid
    BEFORE UPDATE ON cat_blockchain_networks
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_cat_blockchain_networks_soft_delete
    BEFORE UPDATE OF deletion_mark ON cat_blockchain_networks
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

CREATE TRIGGER trg_cat_blockchain_networks_updated_at
    BEFORE UPDATE ON cat_blockchain_networks
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Keyset pagination
CREATE INDEX idx_cat_blockchain_networks_name_id ON cat_blockchain_networks (name ASC, id ASC);

COMMENT ON TABLE cat_blockchain_networks IS 'Справочник Блокчейн-сети — поддерживаемые blockchain платформы (Bitcoin, Ethereum, TRON и др.)';


-- ═══════════════════════════════════════════════════════════════════════════
-- Tokens catalog (Справочник «Токены»)
-- ═══════════════════════════════════════════════════════════════════════════

CREATE TABLE cat_tokens (
    -- Base fields
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    deletion_mark BOOLEAN     NOT NULL DEFAULT FALSE,
    version       INT         NOT NULL DEFAULT 1,
    attributes    JSONB       DEFAULT '{}',

    -- CDC
    _deleted_at TIMESTAMPTZ,
    _txid       BIGINT DEFAULT txid_current(),

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Catalog fields
    code      VARCHAR(50)  NOT NULL,
    name      VARCHAR(255) NOT NULL,
    parent_id UUID REFERENCES cat_tokens(id),
    is_folder BOOLEAN      NOT NULL DEFAULT FALSE,

    -- Token-specific fields
    network_id       UUID         NOT NULL REFERENCES cat_blockchain_networks(id),
    contract_address TEXT         NOT NULL DEFAULT '',   -- empty for native tokens
    symbol           VARCHAR(20)  NOT NULL,              -- "USDT", "ETH", "BTC"
    decimal_places   INT          NOT NULL,              -- 6, 18, 8 — NEVER hardcode!
    token_standard   VARCHAR(20)  NOT NULL,              -- "native", "TRC-20", "ERC-20"
    is_active        BOOLEAN      NOT NULL DEFAULT TRUE,

    -- Currency link: maps token to its currency for exchange rate lookups.
    -- Multiple tokens can reference the same currency (e.g., USDT-TRC20 + USDT-ERC20 → USDT).
    currency_id      UUID         REFERENCES cat_currencies(id),

    -- Sweep defaults (merchant can override via reg_merchant_token_config)
    sweep_threshold     BIGINT   NOT NULL DEFAULT 0,     -- min balance for sweep (minor units). 0 = sweep after every payment
    sweep_max_age_hours INT      NOT NULL DEFAULT 0,     -- max hours before forced sweep. 0 = disabled

    CONSTRAINT chk_token_decimals CHECK (decimal_places >= 0 AND decimal_places <= 18),
    CONSTRAINT chk_sweep_threshold CHECK (sweep_threshold >= 0),
    CONSTRAINT chk_sweep_max_age CHECK (sweep_max_age_hours >= 0)
);

-- Unique indexes
CREATE UNIQUE INDEX idx_cat_tokens_code ON cat_tokens (code) WHERE deletion_mark = FALSE;
CREATE UNIQUE INDEX idx_cat_tokens_symbol_network ON cat_tokens (symbol, network_id) WHERE deletion_mark = FALSE;

-- Search indexes
CREATE INDEX idx_cat_tokens_name    ON cat_tokens USING gin (name gin_trgm_ops);
CREATE INDEX idx_cat_tokens_parent  ON cat_tokens (parent_id) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_tokens_network ON cat_tokens (network_id) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_tokens_currency ON cat_tokens (currency_id) WHERE deletion_mark = FALSE AND currency_id IS NOT NULL;
CREATE INDEX idx_cat_tokens_attrs   ON cat_tokens USING gin (attributes);

-- CDC indexes & triggers
CREATE INDEX idx_cat_tokens_txid ON cat_tokens (_txid) WHERE _deleted_at IS NULL;

CREATE TRIGGER trg_cat_tokens_txid
    BEFORE UPDATE ON cat_tokens
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_cat_tokens_soft_delete
    BEFORE UPDATE OF deletion_mark ON cat_tokens
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

CREATE TRIGGER trg_cat_tokens_updated_at
    BEFORE UPDATE ON cat_tokens
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Keyset pagination
CREATE INDEX idx_cat_tokens_name_id ON cat_tokens (name ASC, id ASC);

COMMENT ON TABLE cat_tokens IS 'Справочник Токены — криптовалютные активы на блокчейн-сетях (USDT-TRC20, ETH, BTC и др.)';

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_lock(hashtext('metapus_migrations'));
DROP TABLE IF EXISTS cat_tokens CASCADE;
DROP TABLE IF EXISTS cat_blockchain_networks CASCADE;
SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
-- +goose StatementEnd
