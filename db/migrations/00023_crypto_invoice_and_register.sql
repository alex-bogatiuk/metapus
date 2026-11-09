-- +goose Up
-- Description: Crypto Invoice document + Crypto Balance register

-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- ═══════════════════════════════════════════════════════════════════════════
-- Crypto Invoice document (Документ «Крипто-инвойс»)
-- ═══════════════════════════════════════════════════════════════════════════

-- ── Header ─────────────────────────────────────────────────────────────────
CREATE TABLE doc_crypto_invoices (
    -- Base fields
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    public_ref    TEXT        NOT NULL,
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
    number          TEXT         NOT NULL,
    date            TIMESTAMPTZ  NOT NULL,
    posted          BOOLEAN      NOT NULL DEFAULT FALSE,
    posted_version  INT          NOT NULL DEFAULT 0,
    basis_type      TEXT         NOT NULL DEFAULT '',
    basis_id        UUID,
    description     TEXT         NOT NULL DEFAULT '',

    -- CryptoInvoice-specific fields
    merchant_id    UUID    NOT NULL,                                   -- FK to future cat_merchants
    token_id       UUID    NOT NULL REFERENCES cat_tokens(id),
    wallet_id      UUID,                                               -- assigned during payment setup
    expected_amount NUMERIC(78,0) NOT NULL,
    received_amount NUMERIC(78,0) NOT NULL DEFAULT 0,
    confirmed_amount NUMERIC(78,0) NOT NULL DEFAULT 0,
    credited_amount NUMERIC(78,0) NOT NULL DEFAULT 0,
    overpaid_amount NUMERIC(78,0) NOT NULL DEFAULT 0,
    status         TEXT     NOT NULL DEFAULT 'created',                 -- InvoiceStatus enum (string)
    single_payment BOOLEAN NOT NULL DEFAULT FALSE,
    selected_payment_id UUID,
    expires_at     TIMESTAMPTZ NOT NULL,
    notification_url TEXT  NOT NULL DEFAULT '',
    return_url     TEXT    NOT NULL DEFAULT '',
    metadata       JSONB   NOT NULL DEFAULT '{}'::jsonb,
    required_confirmations INT NOT NULL DEFAULT 0,
    finality_mode  TEXT    NOT NULL DEFAULT '',
    paid_at        TIMESTAMPTZ,
    confirmed_at   TIMESTAMPTZ,
    completed_at   TIMESTAMPTZ,
    reversed_at    TIMESTAMPTZ,
    view_count     BIGINT NOT NULL DEFAULT 0,
    first_viewed_at TIMESTAMPTZ,
    last_viewed_at TIMESTAMPTZ,
    external_id    TEXT    NOT NULL DEFAULT '',                         -- merchant business reference
    order_id       TEXT    NOT NULL DEFAULT '',
    ttl_minutes    INT     NOT NULL DEFAULT 60 CHECK (ttl_minutes BETWEEN 5 AND 10080),
    customer_id    TEXT    NOT NULL DEFAULT '',
    creation_source TEXT   NOT NULL DEFAULT 'portal'
                   CHECK (creation_source IN ('merchant_api','portal','system')),
    invoice_kind   TEXT   NOT NULL DEFAULT 'payment'
                   CHECK (invoice_kind IN ('payment','top_up')),
    CONSTRAINT chk_crypto_invoice_top_up_source CHECK (invoice_kind <> 'top_up' OR creation_source = 'system'),

    -- API key audit trail: which merchant API key created this invoice.
    -- NULL for invoices created via internal/admin flows (JWT user context).
    -- Chain: api_key_id → cat_merchant_api_keys.created_by_user_id → platform user.
    api_key_id     UUID,

    CONSTRAINT uq_crypto_invoice_number UNIQUE (number),
    CONSTRAINT uq_crypto_invoice_public_ref UNIQUE (public_ref),
    CONSTRAINT chk_crypto_invoice_public_ref_format CHECK (public_ref ~ '^ci_[a-f0-9]{32}$'),
    CONSTRAINT chk_expected_amount_positive CHECK (expected_amount > 0),
    CONSTRAINT chk_received_amount_nonneg CHECK (received_amount >= 0),
    CONSTRAINT chk_confirmed_amount_nonneg CHECK (confirmed_amount >= 0),
    CONSTRAINT chk_credited_amount_nonneg CHECK (credited_amount >= 0),
    CONSTRAINT chk_overpaid_amount_nonneg CHECK (overpaid_amount >= 0),
    CONSTRAINT chk_invoice_amount_order CHECK (
        credited_amount <= confirmed_amount AND confirmed_amount <= received_amount
    ),
    CONSTRAINT chk_invoice_metadata_object CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT chk_invoice_required_confirmations CHECK (required_confirmations >= 0),
    CONSTRAINT chk_invoice_view_count_nonneg CHECK (view_count >= 0),
    CONSTRAINT chk_status_valid CHECK (
        status IN ('created','partially_paid','paid','confirmed','completed','underpaid','expired','cancelled','reversed')
    )
    -- No FK on created_by/updated_by: merchant API requests have no auth_users entry.
    -- Audit enrichment is best-effort: set when JWT user is present, NULL otherwise.
    -- No FK on api_key_id: forward reference (cat_merchant_api_keys defined in migration 00024).
);

-- Header indexes
CREATE INDEX idx_crypto_invoices_date         ON doc_crypto_invoices (date DESC);
CREATE INDEX idx_crypto_invoices_merchant     ON doc_crypto_invoices (merchant_id);
CREATE INDEX idx_crypto_invoices_token        ON doc_crypto_invoices (token_id);
CREATE INDEX idx_crypto_invoices_wallet       ON doc_crypto_invoices (wallet_id) WHERE wallet_id IS NOT NULL;
CREATE INDEX idx_crypto_invoices_status       ON doc_crypto_invoices (status) WHERE status IN ('created','partially_paid','paid','confirmed','reversed');
CREATE UNIQUE INDEX idx_crypto_invoices_merchant_order_id
    ON doc_crypto_invoices (merchant_id, order_id)
    WHERE order_id <> '' AND _deleted_at IS NULL;
CREATE UNIQUE INDEX idx_crypto_invoices_merchant_external_id
    ON doc_crypto_invoices (merchant_id, external_id)
    WHERE external_id <> '' AND _deleted_at IS NULL;
CREATE INDEX idx_crypto_invoices_expires      ON doc_crypto_invoices (expires_at) WHERE status IN ('created','partially_paid');
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
CREATE INDEX idx_crypto_invoices_merchant_created_id ON doc_crypto_invoices (merchant_id, created_at DESC, id DESC) WHERE _deleted_at IS NULL;
CREATE INDEX idx_crypto_invoices_merchant_completed_id ON doc_crypto_invoices (merchant_id, completed_at DESC, id DESC) WHERE completed_at IS NOT NULL AND _deleted_at IS NULL;

COMMENT ON TABLE doc_crypto_invoices IS 'Документ Крипто-инвойс — запрос на оплату криптовалютой (без табличной части)';
COMMENT ON COLUMN doc_crypto_invoices.public_ref IS 'Opaque customer-facing checkout reference: ci_<128-bit-hex>';
COMMENT ON COLUMN doc_crypto_invoices.expected_amount IS 'Ожидаемая сумма в точных raw units токена (NUMERIC(78,0))';
COMMENT ON COLUMN doc_crypto_invoices.received_amount IS 'Canonical observed payments, including provisional payments';
COMMENT ON COLUMN doc_crypto_invoices.confirmed_amount IS 'Canonical payments that reached platform network finality';
COMMENT ON COLUMN doc_crypto_invoices.credited_amount IS 'Canonical finalized payments posted to merchant ledger';
COMMENT ON COLUMN doc_crypto_invoices.overpaid_amount IS 'Сумма переплаты (received - expected), 0 если переплаты нет';
COMMENT ON COLUMN doc_crypto_invoices.status IS 'Lifecycle status; overpayment is represented by overpaid_amount, not a status';
COMMENT ON COLUMN doc_crypto_invoices.external_id IS 'Business external reference supplied by the merchant; transport idempotency lives in sys_idempotency';
COMMENT ON COLUMN doc_crypto_invoices.invoice_kind IS 'Platform-owned invoice classification; never inferred from merchant business references';
COMMENT ON COLUMN doc_crypto_invoices.version IS 'Monotonic public resourceVersion';
COMMENT ON COLUMN doc_crypto_invoices.return_url IS 'Browser UX destination only; never authoritative for payment status';
COMMENT ON COLUMN doc_crypto_invoices.view_count IS 'Internal checkout analytics only; never emitted as a merchant event';


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
    amount           NUMERIC(78,0) NOT NULL,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_crypto_balance_record_type CHECK (record_type IN ('receipt', 'expense')),
    CONSTRAINT chk_crypto_balance_amount_positive CHECK (amount > 0)
);

COMMENT ON TABLE reg_crypto_balance_movements IS 'Регистр крипто-балансов — движения';

CREATE INDEX idx_reg_crypto_balance_movements_recorder
    ON reg_crypto_balance_movements (recorder_id, recorder_version);
CREATE UNIQUE INDEX uq_crypto_custody_adjustment_balance_effect
    ON reg_crypto_balance_movements
       (recorder_id,recorder_version,record_type,wallet_id,token_id)
    WHERE recorder_type = 'CryptoCustodyAdjustment';
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
    amount           NUMERIC(78,0) NOT NULL DEFAULT 0,
    last_movement_at TIMESTAMPTZ,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (wallet_id, token_id)
);

COMMENT ON TABLE reg_crypto_balance_balances IS 'Регистр крипто-балансов — текущие остатки';

CREATE INDEX idx_reg_crypto_balance_balances_token
    ON reg_crypto_balance_balances (token_id) WHERE amount != 0;
CREATE INDEX idx_reg_crypto_balance_balances_wallet
    ON reg_crypto_balance_balances (wallet_id) WHERE amount != 0;


-- +goose StatementEnd

-- ── Trigger: auto-update balances on movement insert/delete ────────────────
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_crypto_balance()
RETURNS TRIGGER AS $func$
DECLARE
    v_signed_amount NUMERIC(78,0);
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
    integration_pool_id UUID REFERENCES cat_integration_pools(id) ON DELETE RESTRICT,
    api_key        TEXT CHECK (api_key IS NULL OR (
        octet_length(api_key) BETWEEN 1 AND 512 AND api_key !~ '[[:cntrl:]]'
    )),                                             -- write-only provider credential managed in ERP
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
    currency_id UUID NOT NULL REFERENCES cat_currencies(id),
    base_currency_id UUID NOT NULL REFERENCES cat_currencies(id),
    rate_source_id UUID NOT NULL REFERENCES cat_rate_sources(id),
    external_id VARCHAR(100) NOT NULL CHECK (length(trim(external_id)) > 0),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    version INT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ,
    PRIMARY KEY (currency_id, base_currency_id, rate_source_id),
    CHECK (currency_id <> base_currency_id)
);
CREATE INDEX idx_rate_source_mappings_active ON reg_rate_source_mappings(rate_source_id, base_currency_id) WHERE is_active;
CREATE TRIGGER trg_rate_source_mappings_txid BEFORE UPDATE ON reg_rate_source_mappings FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TABLE reg_exchange_rates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    currency_id UUID NOT NULL REFERENCES cat_currencies(id),
    base_currency_id UUID NOT NULL REFERENCES cat_currencies(id),
    rate_source_id UUID NOT NULL REFERENCES cat_rate_sources(id),
    rate NUMERIC(38,18) NOT NULL CHECK (rate > 0),
    multiplier INT NOT NULL DEFAULT 1 CHECK (multiplier > 0),
    provider_updated_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    version INT NOT NULL CHECK (version > 0),
    mapping_version INT NOT NULL CHECK (mapping_version > 0),
    feed_attempt_id UUID NOT NULL,
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ,
    CHECK (provider_updated_at <= received_at),
    UNIQUE (currency_id, base_currency_id, rate_source_id, provider_updated_at, version),
    UNIQUE (feed_attempt_id, currency_id, base_currency_id, rate_source_id)
);
CREATE INDEX idx_reg_exchange_rates_latest ON reg_exchange_rates(currency_id, base_currency_id, rate_source_id, provider_updated_at DESC, version DESC);
CREATE INDEX idx_reg_exchange_rates_history ON reg_exchange_rates(currency_id, base_currency_id, provider_updated_at DESC, version DESC, id DESC);
COMMENT ON TABLE reg_exchange_rates IS 'Immutable provider observations; all history retained. multiplier currency = rate base_currency.';

CREATE TABLE sys_rate_feed_state (
    rate_source_id UUID NOT NULL REFERENCES cat_rate_sources(id),
    base_currency_id UUID NOT NULL REFERENCES cat_currencies(id),
    lease_token UUID,
    lease_until TIMESTAMPTZ,
    next_run_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_attempt_at TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    last_completed_token UUID,
    status TEXT NOT NULL DEFAULT 'idle',
    requested INT NOT NULL DEFAULT 0,
    received INT NOT NULL DEFAULT 0,
    inserted INT NOT NULL DEFAULT 0,
    repeated INT NOT NULL DEFAULT 0,
    rejected INT NOT NULL DEFAULT 0,
    problems JSONB NOT NULL DEFAULT '[]',
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ,
    PRIMARY KEY (rate_source_id, base_currency_id)
);
CREATE TRIGGER trg_rate_feed_state_txid BEFORE UPDATE ON sys_rate_feed_state FOR EACH ROW EXECUTE FUNCTION update_txid_column();

-- +goose StatementBegin
CREATE FUNCTION guard_exchange_rate_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'exchange-rate observations are immutable' USING ERRCODE = '23514';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER trg_exchange_rate_immutable BEFORE UPDATE OR DELETE ON reg_exchange_rates FOR EACH ROW EXECUTE FUNCTION guard_exchange_rate_history();

-- +goose StatementBegin
CREATE FUNCTION guard_rate_source_type() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.source_type IS DISTINCT FROM OLD.source_type AND (
        EXISTS (SELECT 1 FROM reg_rate_source_mappings WHERE rate_source_id = OLD.id)
        OR EXISTS (SELECT 1 FROM reg_exchange_rates WHERE rate_source_id = OLD.id)
        OR EXISTS (SELECT 1 FROM sys_settings WHERE exchange_rates->>'sourceId' = OLD.id::text)
    ) THEN
        RAISE EXCEPTION 'source type is in use' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER trg_rate_source_type BEFORE UPDATE OF source_type ON cat_rate_sources FOR EACH ROW EXECUTE FUNCTION guard_rate_source_type();

INSERT INTO auth_permissions(code,name,resource,action) VALUES
 ('register:exchange_rate:read','Чтение курсов валют','exchange_rate','read'),
 ('catalog:rate_source:read','Чтение источников курсов','rate_source','read'),
 ('catalog:rate_source:create','Создание источников курсов','rate_source','create'),
 ('catalog:rate_source:update','Настройка источников курсов','rate_source','update'),
 ('catalog:rate_source:delete','Удаление источников курсов','rate_source','delete')
ON CONFLICT(code) DO NOTHING;
INSERT INTO auth_role_permissions(role_id,permission_id)
SELECT r.id,p.id FROM auth_roles r CROSS JOIN auth_permissions p
WHERE (r.code IN ('admin','accountant','manager','user') AND p.code='register:exchange_rate:read')
 OR (r.code='admin' AND p.resource='rate_source')
ON CONFLICT DO NOTHING;

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
    amount           NUMERIC(78,0) NOT NULL,
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
    amount           NUMERIC(78,0) NOT NULL DEFAULT 0,
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
    v_signed_amount NUMERIC(78,0);
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
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP FUNCTION IF EXISTS recalculate_crypto_merchant_balance();
DROP TRIGGER IF EXISTS trg_crypto_merchant_balance_movements_balance ON reg_crypto_merchant_balance_movements;
DROP FUNCTION IF EXISTS update_crypto_merchant_balance();
DROP TABLE IF EXISTS reg_crypto_merchant_balance_balances;
DROP TABLE IF EXISTS reg_crypto_merchant_balance_movements;
DROP TABLE IF EXISTS sys_rate_feed_state;
DROP TRIGGER IF EXISTS trg_rate_source_type ON cat_rate_sources;
DROP FUNCTION IF EXISTS guard_rate_source_type();
DROP TABLE IF EXISTS reg_exchange_rates;
DROP FUNCTION IF EXISTS guard_exchange_rate_history();
DROP TABLE IF EXISTS reg_rate_source_mappings;
DROP TABLE IF EXISTS cat_rate_sources CASCADE;
DROP FUNCTION IF EXISTS recalculate_crypto_balance();
DROP TRIGGER IF EXISTS trg_crypto_balance_movements_balance ON reg_crypto_balance_movements;
DROP FUNCTION IF EXISTS update_crypto_balance();
DROP TABLE IF EXISTS reg_crypto_balance_balances;
DROP TABLE IF EXISTS reg_crypto_balance_movements;
DROP TABLE IF EXISTS doc_crypto_invoices;
-- +goose StatementEnd

