-- +goose Up
-- Description: Crypto processing catalogs (blockchain networks + tokens)
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

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

    rpc_config_source TEXT NOT NULL DEFAULT 'file' CHECK (rpc_config_source IN ('file','database')),
    -- Network-specific fields
    chain_family          TEXT         NOT NULL DEFAULT 'tron', -- driver family: tron, evm, ton, solana
    network_identifier    VARCHAR(50)  NOT NULL,     -- concrete runtime network: nile, mainnet, 1, 137
    native_token_symbol   VARCHAR(10)  NOT NULL,     -- "TRX", "ETH", "BTC"
    native_decimals       INT          NOT NULL,     -- 6, 18, 8
    confirmations_needed  INT          NOT NULL DEFAULT 1,
    finality_mode         TEXT         NOT NULL DEFAULT 'confirmations'
                          CHECK (finality_mode IN ('confirmations','protocol_finalized','confirmations_or_finalized')),
    block_time_seconds    INT          NOT NULL DEFAULT 10,
    explorer_url          TEXT         NOT NULL DEFAULT '', -- HTTPS tx URL template with one {txHash}; empty disables links
    is_active             BOOLEAN      NOT NULL DEFAULT TRUE,

    CONSTRAINT chk_native_decimals      CHECK (native_decimals >= 0 AND native_decimals <= 18),
    CONSTRAINT chk_confirmations_needed CHECK (confirmations_needed >= 1),
    CONSTRAINT chk_blockchain_network_family
        CHECK (chain_family IN ('tron','evm','ton','solana')),
    CONSTRAINT chk_blockchain_network_identifier
        CHECK (BTRIM(network_identifier) <> ''),
    CONSTRAINT chk_blockchain_network_explorer_url
        CHECK (
            explorer_url = '' OR (
                explorer_url ~ '^https://[^[:space:]]+$'
                AND (LENGTH(explorer_url) - LENGTH(REPLACE(explorer_url, '{txHash}', ''))) = LENGTH('{txHash}')
                AND POSITION('{' IN REPLACE(explorer_url, '{txHash}', '')) = 0
                AND POSITION('}' IN REPLACE(explorer_url, '{txHash}', '')) = 0
            )
        )
);

-- Unique indexes
CREATE UNIQUE INDEX idx_cat_blockchain_networks_code     ON cat_blockchain_networks (code) WHERE deletion_mark = FALSE;
CREATE UNIQUE INDEX idx_cat_blockchain_networks_family_identifier
    ON cat_blockchain_networks (chain_family, network_identifier)
    WHERE deletion_mark = FALSE;

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
COMMENT ON COLUMN cat_blockchain_networks.explorer_url IS 'HTTPS-шаблон ссылки на транзакцию с единственным плейсхолдером {txHash}; пустое значение отключает ссылку';


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
    sweep_threshold     NUMERIC(78,0) NOT NULL DEFAULT 0, -- raw units. 0 = sweep after every payment
    sweep_max_age_hours INT      NOT NULL DEFAULT 0,     -- max hours before forced sweep. 0 = disabled
    sweep_retain_amount NUMERIC(78,0) NOT NULL DEFAULT 0,

    CONSTRAINT chk_token_decimals CHECK (decimal_places >= 0 AND decimal_places <= 18),
    CONSTRAINT chk_sweep_threshold CHECK (sweep_threshold >= 0),
    CONSTRAINT chk_sweep_max_age CHECK (sweep_max_age_hours >= 0),
    CONSTRAINT chk_sweep_retain_amount CHECK (sweep_retain_amount >= 0)
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

COMMENT ON COLUMN cat_tokens.sweep_retain_amount IS
    'Token minor units retained on reusable deposit wallets after sweep';

COMMENT ON TABLE cat_tokens IS 'Справочник Токены — криптовалютные активы на блокчейн-сетях (USDT-TRC20, ETH, BTC и др.)';

-- +goose StatementEnd


-- INTEGRATION_RPC_SCHEMA_BEGIN
CREATE TABLE sys_crypto_rpc_network_settings(network_id UUID PRIMARY KEY REFERENCES cat_blockchain_networks(id),evm_settings JSONB,tron_settings JSONB,ingestion_alert_settings JSONB NOT NULL DEFAULT '{}' CHECK(jsonb_typeof(ingestion_alert_settings)='object'),version INT NOT NULL DEFAULT 1,updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
CREATE TABLE sys_crypto_rpc_endpoints(
 id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),code VARCHAR(50) NOT NULL,name VARCHAR(255) NOT NULL,
 parent_id UUID CHECK(parent_id IS NULL),is_folder BOOLEAN NOT NULL DEFAULT FALSE CHECK(NOT is_folder),
 deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,version INT NOT NULL DEFAULT 1,attributes JSONB NOT NULL DEFAULT '{}',
 _deleted_at TIMESTAMPTZ,_txid BIGINT DEFAULT txid_current(),created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id),
 role TEXT NOT NULL CHECK(role IN ('rpc','indexer','fullnode','solidity')),
 provider_id VARCHAR(64) NOT NULL,trust_group VARCHAR(64) NOT NULL,
 url TEXT NOT NULL,pool_id UUID REFERENCES cat_integration_pools(id) ON DELETE RESTRICT,
 credential_slot VARCHAR(64) NOT NULL DEFAULT 'default',priority INT NOT NULL DEFAULT 0 CHECK(priority>=0),
 min_request_interval_ms INT NOT NULL DEFAULT 0 CHECK(min_request_interval_ms=0 OR min_request_interval_ms BETWEEN 100 AND 30000),
 disable_keep_alives BOOLEAN NOT NULL DEFAULT FALSE,is_active BOOLEAN NOT NULL DEFAULT TRUE
);
CREATE UNIQUE INDEX uq_rpc_endpoint_code ON sys_crypto_rpc_endpoints(code) WHERE NOT deletion_mark;
CREATE UNIQUE INDEX uq_rpc_endpoint_provider ON sys_crypto_rpc_endpoints(network_id,role,provider_id) WHERE NOT deletion_mark;
CREATE INDEX idx_rpc_endpoint_name_id ON sys_crypto_rpc_endpoints(name,id);
CREATE INDEX idx_rpc_endpoint_pool ON sys_crypto_rpc_endpoints(pool_id);
CREATE INDEX idx_rpc_endpoint_network ON sys_crypto_rpc_endpoints(network_id);
CREATE INDEX idx_rpc_endpoint_txid ON sys_crypto_rpc_endpoints(_txid) WHERE _deleted_at IS NULL;
CREATE TRIGGER trg_rpc_endpoint_txid BEFORE INSERT OR UPDATE ON sys_crypto_rpc_endpoints FOR EACH ROW EXECUTE FUNCTION update_txid_column();
CREATE TRIGGER trg_rpc_endpoint_modtime BEFORE UPDATE ON sys_crypto_rpc_endpoints FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_rpc_endpoint_delete BEFORE UPDATE OF deletion_mark ON sys_crypto_rpc_endpoints FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();
INSERT INTO auth_permissions(code,name,description,resource,action) SELECT 'catalog:crypto_rpc_endpoint:'||action,'Подключения RPC: '||action,'Настройки RPC','crypto_rpc_endpoint',action FROM (VALUES ('read'),('create'),('update'),('delete')) AS a(action);
INSERT INTO auth_role_permissions(role_id,permission_id) SELECT 'b0000000-0000-0000-0000-000000000001',id FROM auth_permissions WHERE resource='crypto_rpc_endpoint';

-- +goose Down
DELETE FROM auth_role_permissions WHERE permission_id IN(SELECT id FROM auth_permissions WHERE resource='crypto_rpc_endpoint');
DELETE FROM auth_permissions WHERE resource='crypto_rpc_endpoint';
DROP TABLE sys_crypto_rpc_endpoints;
DROP TABLE sys_crypto_rpc_network_settings;
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TABLE IF EXISTS cat_tokens CASCADE;
DROP TABLE IF EXISTS cat_blockchain_networks CASCADE;
-- +goose StatementEnd
