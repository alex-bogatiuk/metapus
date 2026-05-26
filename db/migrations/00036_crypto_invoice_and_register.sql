-- +goose Up
-- Description: Crypto Invoice document + Crypto Balance register

-- +goose StatementBegin
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- ═══════════════════════════════════════════════════════════════════════════
-- Crypto Invoice document (Документ «Крипто-инвойс»)
-- ═══════════════════════════════════════════════════════════════════════════

-- ── Header ─────────────────────────────────────────────────────────────────
CREATE TABLE doc_crypto_invoices (
    -- Base fields
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    deletion_mark BOOLEAN     NOT NULL DEFAULT FALSE,
    version       INT         NOT NULL DEFAULT 1,
    attributes    JSONB       DEFAULT '{}',

    -- CDC
    _deleted_at TIMESTAMPTZ,
    _txid       BIGINT DEFAULT txid_current(),

    -- Audit fields
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by UUID,        -- NULL for merchant API requests (no auth user context)
    updated_by UUID,

    -- Document fields
    number          VARCHAR(20)  NOT NULL,
    date            TIMESTAMPTZ  NOT NULL,
    posted          BOOLEAN      NOT NULL DEFAULT FALSE,
    posted_version  INT          NOT NULL DEFAULT 0,
    basis_type      TEXT         NOT NULL DEFAULT '',
    basis_id        UUID,
    description     TEXT,

    -- CryptoInvoice-specific fields
    merchant_id    UUID    NOT NULL,                                   -- FK to future cat_merchants
    token_id       UUID    NOT NULL REFERENCES cat_tokens(id),
    wallet_id      UUID,                                               -- assigned during payment setup
    expected_amount BIGINT NOT NULL,                                  -- CryptoAmount: BIGINT for arbitrary precision
    received_amount BIGINT NOT NULL DEFAULT 0,
    overpaid_amount BIGINT NOT NULL DEFAULT 0,                         -- excess amount: received - expected when received > expected
    status         TEXT     NOT NULL DEFAULT 'created',                 -- InvoiceStatus enum (string)
    expires_at     TIMESTAMPTZ NOT NULL,
    callback_url   TEXT    NOT NULL DEFAULT '',
    external_id    TEXT    NOT NULL DEFAULT '',                         -- idempotency key
    order_id       TEXT    NOT NULL DEFAULT '',
    customer_email TEXT    NOT NULL DEFAULT '',

    -- API key audit trail: which merchant API key created this invoice.
    -- NULL for invoices created via internal/admin flows (JWT user context).
    -- Chain: api_key_id → cat_merchant_api_keys.created_by_user_id → platform user.
    api_key_id     UUID,

    CONSTRAINT uq_crypto_invoice_number UNIQUE (number),
    CONSTRAINT chk_expected_amount_positive CHECK (expected_amount > 0),
    CONSTRAINT chk_received_amount_nonneg CHECK (received_amount >= 0),
    CONSTRAINT chk_overpaid_amount_nonneg CHECK (overpaid_amount >= 0),
    CONSTRAINT chk_status_valid CHECK (status IN ('created','partially_paid','paid','overpaid','confirmed','expired','cancelled'))
    -- No FK on created_by/updated_by: merchant API requests have no auth_users entry.
    -- Audit enrichment is best-effort: set when JWT user is present, NULL otherwise.
    -- No FK on api_key_id: forward reference (cat_merchant_api_keys defined in migration 00037).
);

-- Header indexes
CREATE INDEX idx_crypto_invoices_date         ON doc_crypto_invoices (date DESC);
CREATE INDEX idx_crypto_invoices_merchant     ON doc_crypto_invoices (merchant_id);
CREATE INDEX idx_crypto_invoices_token        ON doc_crypto_invoices (token_id);
CREATE INDEX idx_crypto_invoices_wallet       ON doc_crypto_invoices (wallet_id) WHERE wallet_id IS NOT NULL;
CREATE INDEX idx_crypto_invoices_status       ON doc_crypto_invoices (status) WHERE status IN ('created', 'partially_paid', 'paid', 'overpaid');
CREATE INDEX idx_crypto_invoices_external_id  ON doc_crypto_invoices (external_id) WHERE external_id != '';
CREATE INDEX idx_crypto_invoices_expires      ON doc_crypto_invoices (expires_at) WHERE status = 'created';
CREATE INDEX idx_crypto_invoices_posted       ON doc_crypto_invoices (posted) WHERE posted = FALSE;
CREATE INDEX idx_crypto_invoices_number_trgm  ON doc_crypto_invoices USING gin (number gin_trgm_ops);
CREATE INDEX idx_crypto_invoices_api_key      ON doc_crypto_invoices (api_key_id) WHERE api_key_id IS NOT NULL;

-- CDC indexes & triggers
CREATE INDEX idx_doc_crypto_invoices_txid ON doc_crypto_invoices (_txid) WHERE _deleted_at IS NULL;

CREATE TRIGGER trg_doc_crypto_invoices_txid
    BEFORE UPDATE ON doc_crypto_invoices
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_doc_crypto_invoices_soft_delete
    BEFORE UPDATE OF deletion_mark ON doc_crypto_invoices
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

-- Keyset pagination
CREATE INDEX idx_doc_crypto_invoices_date_id    ON doc_crypto_invoices (date DESC, id DESC);
CREATE INDEX idx_doc_crypto_invoices_created_id ON doc_crypto_invoices (created_at DESC, id DESC);

COMMENT ON TABLE doc_crypto_invoices IS 'Документ Крипто-инвойс — запрос на оплату криптовалютой (без табличной части)';
COMMENT ON COLUMN doc_crypto_invoices.expected_amount IS 'Ожидаемая сумма в минорных единицах токена (BIGINT для произвольной точности)';
COMMENT ON COLUMN doc_crypto_invoices.received_amount IS 'Фактически полученная сумма';
COMMENT ON COLUMN doc_crypto_invoices.overpaid_amount IS 'Сумма переплаты (received - expected), 0 если переплаты нет';
COMMENT ON COLUMN doc_crypto_invoices.status IS 'Статус инвойса: created, partially_paid, paid, overpaid, confirmed, expired, cancelled';
COMMENT ON COLUMN doc_crypto_invoices.external_id IS 'Идемпотентный ключ от мерчанта (Bender pattern)';


-- ═══════════════════════════════════════════════════════════════════════════
-- Crypto Balance accumulation register (Регистр «Крипто-балансы»)
-- ═══════════════════════════════════════════════════════════════════════════

-- ── Movements ──────────────────────────────────────────────────────────────
CREATE TABLE reg_crypto_balance_movements (
    line_id          UUID         PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    recorder_id      UUID         NOT NULL,
    recorder_type    VARCHAR(50)  NOT NULL,
    recorder_version INT          NOT NULL DEFAULT 1,
    period           TIMESTAMPTZ  NOT NULL,
    record_type      VARCHAR(10)  NOT NULL,
    wallet_id        UUID         NOT NULL,
    token_id         UUID         NOT NULL REFERENCES cat_tokens(id),
    amount           BIGINT      NOT NULL,                            -- CryptoAmount
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_crypto_balance_record_type CHECK (record_type IN ('receipt', 'expense')),
    CONSTRAINT chk_crypto_balance_amount_positive CHECK (amount > 0)
);

COMMENT ON TABLE reg_crypto_balance_movements IS 'Регистр крипто-балансов — движения';

CREATE INDEX idx_reg_crypto_balance_movements_recorder
    ON reg_crypto_balance_movements (recorder_id, recorder_version);
CREATE INDEX idx_reg_crypto_balance_movements_balance
    ON reg_crypto_balance_movements (wallet_id, token_id, record_type);
CREATE INDEX idx_reg_crypto_balance_movements_period
    ON reg_crypto_balance_movements (period);
CREATE INDEX idx_reg_crypto_balance_movements_token
    ON reg_crypto_balance_movements (token_id, period DESC);

-- ── Balances ───────────────────────────────────────────────────────────────
CREATE TABLE reg_crypto_balance_balances (
    wallet_id        UUID        NOT NULL,
    token_id         UUID        NOT NULL REFERENCES cat_tokens(id),
    amount           BIGINT     NOT NULL DEFAULT 0,                   -- CryptoAmount
    last_movement_at TIMESTAMPTZ,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (wallet_id, token_id)
);

COMMENT ON TABLE reg_crypto_balance_balances IS 'Регистр крипто-балансов — текущие остатки';

CREATE INDEX idx_reg_crypto_balance_balances_token
    ON reg_crypto_balance_balances (token_id) WHERE amount != 0;
CREATE INDEX idx_reg_crypto_balance_balances_wallet
    ON reg_crypto_balance_balances (wallet_id) WHERE amount != 0;


-- ═══════════════════════════════════════════════════════════════════════════
-- Webhook Delivery audit trail (Системная таблица «Доставки вебхуков»)
-- Tracks every webhook delivery attempt for debugging and merchant transparency.
-- Modeled after Stripe Events API — each attempt is a separate row.
-- ═══════════════════════════════════════════════════════════════════════════

CREATE TABLE sys_webhook_deliveries (
    id               UUID         PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    invoice_id       UUID         REFERENCES doc_crypto_invoices(id),  -- nullable for test webhooks
    merchant_id      UUID         NOT NULL,
    event_type       TEXT         NOT NULL,      -- 'invoice.paid', 'invoice.confirmed', 'test', etc.
    webhook_url      TEXT         NOT NULL,
    delivery_id      TEXT         NOT NULL,      -- X-Metapus-Delivery-ID (idempotency)
    status_code      INT,                        -- HTTP response status (NULL if connection error)
    response_time_ms INT,                        -- round-trip time in ms
    attempt          INT          NOT NULL DEFAULT 1,  -- retry attempt number (1-based)
    error_message    TEXT,                        -- error details on failure
    request_body     JSONB,                      -- webhook payload (for debugging)
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_webhook_del_invoice   ON sys_webhook_deliveries(invoice_id) WHERE invoice_id IS NOT NULL;
CREATE INDEX idx_webhook_del_merchant  ON sys_webhook_deliveries(merchant_id);
CREATE INDEX idx_webhook_del_created   ON sys_webhook_deliveries(created_at DESC);
CREATE UNIQUE INDEX idx_webhook_del_delivery  ON sys_webhook_deliveries(delivery_id);

COMMENT ON TABLE sys_webhook_deliveries IS 'Audit trail для доставок вебхуков мерчантам (Stripe Events pattern). Каждая попытка — отдельная строка.';
COMMENT ON COLUMN sys_webhook_deliveries.invoice_id IS 'NULL для тестовых вебхуков (POST /portal/v1/webhooks/test)';
COMMENT ON COLUMN sys_webhook_deliveries.delivery_id IS 'Уникальный ID доставки, передаётся в заголовке X-Metapus-Delivery-ID';


SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
-- +goose StatementEnd

-- ── Trigger: auto-update balances on movement insert/delete ────────────────
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_crypto_balance()
RETURNS TRIGGER AS $func$
DECLARE
    v_signed_amount BIGINT;
    v_wid  UUID;
    v_tid  UUID;
    v_per  TIMESTAMPTZ;
BEGIN
    IF TG_OP = 'DELETE' THEN
        v_wid := OLD.wallet_id;
        v_tid := OLD.token_id;
        v_per := OLD.period;
        IF OLD.record_type = 'receipt' THEN
            v_signed_amount := -OLD.amount;
        ELSE
            v_signed_amount := OLD.amount;
        END IF;
    ELSE
        v_wid := NEW.wallet_id;
        v_tid := NEW.token_id;
        v_per := NEW.period;
        IF NEW.record_type = 'receipt' THEN
            v_signed_amount := NEW.amount;
        ELSE
            v_signed_amount := -NEW.amount;
        END IF;
    END IF;

    INSERT INTO reg_crypto_balance_balances (wallet_id, token_id, amount, last_movement_at, updated_at)
    VALUES (v_wid, v_tid, v_signed_amount, v_per, NOW())
    ON CONFLICT (wallet_id, token_id) DO UPDATE SET
        amount = reg_crypto_balance_balances.amount + v_signed_amount,
        last_movement_at = GREATEST(reg_crypto_balance_balances.last_movement_at, v_per),
        updated_at = NOW();

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    ELSE
        RETURN NEW;
    END IF;
END;
$func$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_crypto_balance_movements_balance
    AFTER INSERT OR DELETE ON reg_crypto_balance_movements
    FOR EACH ROW
    EXECUTE FUNCTION update_crypto_balance();

-- ── Full recalculation function (for audit / recovery) ─────────────────────
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION recalculate_crypto_balance()
RETURNS void AS $func$
BEGIN
    TRUNCATE reg_crypto_balance_balances;
    INSERT INTO reg_crypto_balance_balances (wallet_id, token_id, amount, last_movement_at, updated_at)
    SELECT
        wallet_id,
        token_id,
        SUM(CASE WHEN record_type = 'receipt' THEN amount ELSE -amount END),
        MAX(period),
        NOW()
    FROM reg_crypto_balance_movements
    GROUP BY wallet_id, token_id;
END;
$func$ LANGUAGE plpgsql;
-- +goose StatementEnd


-- ═══════════════════════════════════════════════════════════════════════════
-- Rate Sources catalog (Справочник «Источники курсов»)
-- ═══════════════════════════════════════════════════════════════════════════

CREATE TABLE cat_rate_sources (
    -- Base catalog fields
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    deletion_mark BOOLEAN     NOT NULL DEFAULT FALSE,
    version       INT         NOT NULL DEFAULT 1,
    attributes    JSONB       DEFAULT '{}',

    _deleted_at TIMESTAMPTZ,
    _txid       BIGINT DEFAULT txid_current(),

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    code      VARCHAR(50)  NOT NULL,
    name      VARCHAR(255) NOT NULL,
    parent_id UUID REFERENCES cat_rate_sources(id),
    is_folder BOOLEAN      NOT NULL DEFAULT FALSE,

    -- Rate source specific fields
    source_type    VARCHAR(50)  NOT NULL,            -- 'coingecko', 'binance', 'coinmarketcap', 'manual'
    base_url       VARCHAR(255),                     -- API base URL (e.g. 'https://api.coingecko.com/api/v3')
    api_key        TEXT,                             -- encrypted API key (via AUTOMATION_ENCRYPTION_KEY)
    rate_limit_rpm INT          NOT NULL DEFAULT 100, -- requests per minute
    priority       INT          NOT NULL DEFAULT 100, -- for fallback ordering (lower = higher priority)
    is_active      BOOLEAN      NOT NULL DEFAULT TRUE
);

CREATE UNIQUE INDEX idx_cat_rate_sources_code ON cat_rate_sources (code) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_rate_sources_name ON cat_rate_sources USING gin (name gin_trgm_ops);
CREATE INDEX idx_cat_rate_sources_txid ON cat_rate_sources (_txid) WHERE _deleted_at IS NULL;
CREATE INDEX idx_cat_rate_sources_active ON cat_rate_sources (priority) WHERE is_active = TRUE AND deletion_mark = FALSE;
CREATE INDEX idx_cat_rate_sources_name_id ON cat_rate_sources (name ASC, id ASC);

CREATE TRIGGER trg_cat_rate_sources_txid
    BEFORE UPDATE ON cat_rate_sources
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_cat_rate_sources_soft_delete
    BEFORE UPDATE OF deletion_mark ON cat_rate_sources
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

CREATE TRIGGER trg_cat_rate_sources_updated_at
    BEFORE UPDATE ON cat_rate_sources
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

COMMENT ON TABLE cat_rate_sources IS 'Справочник Источники курсов — провайдеры курсов валют (CoinGecko, Binance, ручной ввод)';


-- ═══════════════════════════════════════════════════════════════════════════
-- Rate Source Mappings register (Регистр сведений «Соответствие валют источникам»)
-- ═══════════════════════════════════════════════════════════════════════════

CREATE TABLE reg_rate_source_mappings (
    currency_id     UUID         NOT NULL REFERENCES cat_currencies(id),
    rate_source_id  UUID         NOT NULL REFERENCES cat_rate_sources(id),
    external_id     VARCHAR(100) NOT NULL,            -- provider-specific coin identifier ('tether', 'bitcoin')
    is_active       BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (currency_id, rate_source_id)
);

CREATE INDEX idx_rate_source_mappings_active
    ON reg_rate_source_mappings (rate_source_id) WHERE is_active = TRUE;

COMMENT ON TABLE reg_rate_source_mappings IS 'Регистр сведений: Соответствие валют источникам курсов. Связывает валюту с internal ID провайдера';
COMMENT ON COLUMN reg_rate_source_mappings.external_id IS 'Идентификатор валюты в провайдере: CoinGecko→"tether", Binance→"USDT", CMC→"tether-usdt"';


-- ═══════════════════════════════════════════════════════════════════════════
-- Exchange Rates information register (Регистр сведений «Курсы валют»)
-- ═══════════════════════════════════════════════════════════════════════════

CREATE TABLE reg_exchange_rates (
    currency_id     UUID           NOT NULL REFERENCES cat_currencies(id),
    date            DATE           NOT NULL,
    rate            DECIMAL(24,12) NOT NULL,              -- exchange rate to base currency
    multiplier      INT            NOT NULL DEFAULT 1,    -- rate denominator (like 1C "Кратность"): 1, 10, 100
    rate_source_id  UUID           NOT NULL REFERENCES cat_rate_sources(id),
    created_at      TIMESTAMPTZ    NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ    NOT NULL DEFAULT now(),
    PRIMARY KEY (currency_id, date, rate_source_id),
    CONSTRAINT chk_exchange_rate_positive CHECK (rate > 0),
    CONSTRAINT chk_exchange_multiplier_positive CHECK (multiplier >= 1)
);

CREATE INDEX idx_reg_exchange_rates_latest
    ON reg_exchange_rates (currency_id, rate_source_id, date DESC);

COMMENT ON TABLE reg_exchange_rates IS 'Регистр сведений: Курсы валют — периодический, срез последних по (currency_id, rate_source_id). Аналог 1С РегистрСведений.КурсыВалют';
COMMENT ON COLUMN reg_exchange_rates.rate IS 'Курс к базовой валюте. Для USDT≈0.9997, для ETH≈2450.50, для JPY≈0.0067';
COMMENT ON COLUMN reg_exchange_rates.multiplier IS 'Кратность (как в 1С): для JPY multiplier=100 → «за 100 JPY = 0.67 USD»';
COMMENT ON COLUMN reg_exchange_rates.rate_source_id IS 'FK на источник курса (cat_rate_sources)';


-- ═══════════════════════════════════════════════════════════════════════════
-- Crypto Merchant Balance accumulation register
-- (Регистр накопления «Крипто-расчёты с мерчантами»)
-- Аналог reg_settlement, но для крипто: отслеживает долг платформы перед мерчантом
-- ═══════════════════════════════════════════════════════════════════════════

-- ── Movements ──────────────────────────────────────────────────────────────
CREATE TABLE reg_crypto_merchant_balance_movements (
    line_id          UUID         PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    recorder_id      UUID         NOT NULL,
    recorder_type    VARCHAR(50)  NOT NULL,
    recorder_version INT          NOT NULL DEFAULT 1,
    period           TIMESTAMPTZ  NOT NULL,
    record_type      VARCHAR(10)  NOT NULL,
    merchant_id      UUID         NOT NULL,
    token_id         UUID         NOT NULL REFERENCES cat_tokens(id),
    amount           BIGINT       NOT NULL,                            -- CryptoAmount
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_merchant_bal_record_type CHECK (record_type IN ('receipt', 'expense')),
    CONSTRAINT chk_merchant_bal_amount_positive CHECK (amount > 0)
);

COMMENT ON TABLE reg_crypto_merchant_balance_movements IS 'Регистр крипто-расчётов с мерчантами — движения (аналог reg_settlement для крипто)';

CREATE INDEX idx_reg_crypto_merch_bal_movements_recorder
    ON reg_crypto_merchant_balance_movements (recorder_id, recorder_version);
CREATE INDEX idx_reg_crypto_merch_bal_movements_balance
    ON reg_crypto_merchant_balance_movements (merchant_id, token_id, record_type);
CREATE INDEX idx_reg_crypto_merch_bal_movements_period
    ON reg_crypto_merchant_balance_movements (period);

-- ── Balances ───────────────────────────────────────────────────────────────
CREATE TABLE reg_crypto_merchant_balance_balances (
    merchant_id      UUID        NOT NULL,
    token_id         UUID        NOT NULL REFERENCES cat_tokens(id),
    amount           BIGINT      NOT NULL DEFAULT 0,                   -- CryptoAmount (signed: receipt-expense)
    last_movement_at TIMESTAMPTZ,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (merchant_id, token_id)
);

COMMENT ON TABLE reg_crypto_merchant_balance_balances IS 'Регистр крипто-расчётов с мерчантами — текущие остатки (долг платформы перед мерчантом)';

CREATE INDEX idx_reg_crypto_merch_bal_balances_token
    ON reg_crypto_merchant_balance_balances (token_id) WHERE amount != 0;
CREATE INDEX idx_reg_crypto_merch_bal_balances_merchant
    ON reg_crypto_merchant_balance_balances (merchant_id) WHERE amount != 0;

-- ── Trigger: auto-update merchant balances on movement insert/delete ───────
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_crypto_merchant_balance()
RETURNS TRIGGER AS $func$
DECLARE
    v_signed_amount BIGINT;
    v_mid  UUID;
    v_tid  UUID;
    v_per  TIMESTAMPTZ;
BEGIN
    IF TG_OP = 'DELETE' THEN
        v_mid := OLD.merchant_id;
        v_tid := OLD.token_id;
        v_per := OLD.period;
        IF OLD.record_type = 'receipt' THEN
            v_signed_amount := -OLD.amount;
        ELSE
            v_signed_amount := OLD.amount;
        END IF;
    ELSE
        v_mid := NEW.merchant_id;
        v_tid := NEW.token_id;
        v_per := NEW.period;
        IF NEW.record_type = 'receipt' THEN
            v_signed_amount := NEW.amount;
        ELSE
            v_signed_amount := -NEW.amount;
        END IF;
    END IF;

    INSERT INTO reg_crypto_merchant_balance_balances (merchant_id, token_id, amount, last_movement_at, updated_at)
    VALUES (v_mid, v_tid, v_signed_amount, v_per, NOW())
    ON CONFLICT (merchant_id, token_id) DO UPDATE SET
        amount = reg_crypto_merchant_balance_balances.amount + v_signed_amount,
        last_movement_at = GREATEST(reg_crypto_merchant_balance_balances.last_movement_at, v_per),
        updated_at = NOW();

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    ELSE
        RETURN NEW;
    END IF;
END;
$func$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_crypto_merchant_balance_movements_balance
    AFTER INSERT OR DELETE ON reg_crypto_merchant_balance_movements
    FOR EACH ROW
    EXECUTE FUNCTION update_crypto_merchant_balance();

-- ── Full recalculation function (for audit / recovery) ─────────────────────
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION recalculate_crypto_merchant_balance()
RETURNS void AS $func$
BEGIN
    TRUNCATE reg_crypto_merchant_balance_balances;
    INSERT INTO reg_crypto_merchant_balance_balances (merchant_id, token_id, amount, last_movement_at, updated_at)
    SELECT
        merchant_id,
        token_id,
        SUM(CASE WHEN record_type = 'receipt' THEN amount ELSE -amount END),
        MAX(period),
        NOW()
    FROM reg_crypto_merchant_balance_movements
    GROUP BY merchant_id, token_id;
END;
$func$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_lock(hashtext('metapus_migrations'));
DROP TABLE IF EXISTS sys_webhook_deliveries;
DROP FUNCTION IF EXISTS recalculate_crypto_merchant_balance();
DROP TRIGGER IF EXISTS trg_crypto_merchant_balance_movements_balance ON reg_crypto_merchant_balance_movements;
DROP FUNCTION IF EXISTS update_crypto_merchant_balance();
DROP TABLE IF EXISTS reg_crypto_merchant_balance_balances;
DROP TABLE IF EXISTS reg_crypto_merchant_balance_movements;
DROP TABLE IF EXISTS reg_exchange_rates;
DROP TABLE IF EXISTS reg_rate_source_mappings;
DROP TABLE IF EXISTS cat_rate_sources CASCADE;
DROP FUNCTION IF EXISTS recalculate_crypto_balance();
DROP TRIGGER IF EXISTS trg_crypto_balance_movements_balance ON reg_crypto_balance_movements;
DROP FUNCTION IF EXISTS update_crypto_balance();
DROP TABLE IF EXISTS reg_crypto_balance_balances;
DROP TABLE IF EXISTS reg_crypto_balance_movements;
DROP TABLE IF EXISTS doc_crypto_invoices;
SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
-- +goose StatementEnd

