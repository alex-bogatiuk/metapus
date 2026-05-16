-- +goose Up
-- Description: CryptoPayment document + CryptoFee register

-- +goose StatementBegin
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- ═══════════════════════════════════════════════════════════════════════
-- CRYPTO PAYMENT DOCUMENT
-- ═══════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS doc_crypto_payments (
    -- Base document fields
    id                UUID PRIMARY KEY,
    number            TEXT NOT NULL DEFAULT '',
    date              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    basis_type        TEXT NOT NULL DEFAULT '',
    basis_id          UUID,
    posted            BOOLEAN NOT NULL DEFAULT FALSE,
    posted_version    INT NOT NULL DEFAULT 0,
    deletion_mark     BOOLEAN NOT NULL DEFAULT FALSE,
    version           INT NOT NULL DEFAULT 1,
    attributes        JSONB DEFAULT '{}',
    description       TEXT NOT NULL DEFAULT '',
    created_by        UUID,
    updated_by        UUID,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- CryptoPayment-specific fields
    invoice_id        UUID NOT NULL,
    merchant_id       UUID NOT NULL,
    token_id          UUID NOT NULL,
    wallet_id         UUID NOT NULL,
    tx_hash           TEXT NOT NULL,
    from_address      TEXT NOT NULL DEFAULT '',
    amount            BIGINT NOT NULL DEFAULT 0,
    block_number      BIGINT NOT NULL DEFAULT 0,
    confirmations     INT NOT NULL DEFAULT 0,
    required_confs    INT NOT NULL DEFAULT 0,
    status            TEXT NOT NULL DEFAULT 'detected' CHECK (status IN ('detected','confirming','confirmed','settled','reorged')),
    -- Fee snapshot (from reg_fee_schedule at payment creation time)
    -- Formula: clamp(fee_fixed + amount × fee_percent_bp / 10000, fee_min, fee_max)
    fee_fixed         BIGINT NOT NULL DEFAULT 0,
    fee_percent_bp    INT NOT NULL DEFAULT 0 CHECK (fee_percent_bp >= 0 AND fee_percent_bp <= 10000),
    fee_min           BIGINT NOT NULL DEFAULT 0,
    fee_max           BIGINT NOT NULL DEFAULT 0,
    detected_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    confirmed_at      TIMESTAMPTZ,

    -- CDC fields
    _txid             BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at       TIMESTAMPTZ
);

-- Unique tx hash (idempotency — one payment per tx)
CREATE UNIQUE INDEX IF NOT EXISTS idx_doc_crypto_payments_tx_hash
    ON doc_crypto_payments (tx_hash) WHERE deletion_mark = FALSE;

CREATE INDEX IF NOT EXISTS idx_doc_crypto_payments_invoice ON doc_crypto_payments (invoice_id);
CREATE INDEX IF NOT EXISTS idx_doc_crypto_payments_merchant ON doc_crypto_payments (merchant_id);
CREATE INDEX IF NOT EXISTS idx_doc_crypto_payments_status ON doc_crypto_payments (status) WHERE status NOT IN ('settled','reorged');
CREATE INDEX IF NOT EXISTS idx_doc_crypto_payments_date ON doc_crypto_payments (date DESC);

-- CDC trigger
CREATE OR REPLACE FUNCTION fn_doc_crypto_payments_cdc() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        NEW._txid := txid_current();
        NEW.updated_at := NOW();
        IF OLD.deletion_mark = FALSE AND NEW.deletion_mark = TRUE THEN
            NEW._deleted_at := NOW();
        ELSIF OLD.deletion_mark = TRUE AND NEW.deletion_mark = FALSE THEN
            NEW._deleted_at := NULL;
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_doc_crypto_payments_cdc
    BEFORE UPDATE ON doc_crypto_payments
    FOR EACH ROW EXECUTE FUNCTION fn_doc_crypto_payments_cdc();


-- ═══════════════════════════════════════════════════════════════════════
-- CRYPTO FEE REGISTER
-- ═══════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS reg_crypto_fee_movements (
    line_id           UUID PRIMARY KEY,
    recorder_id       UUID NOT NULL,
    recorder_type     TEXT NOT NULL,
    recorder_version  INT NOT NULL,
    period            TIMESTAMPTZ NOT NULL,
    record_type       TEXT NOT NULL CHECK (record_type IN ('receipt', 'expense')),

    -- Dimensions
    merchant_id       UUID NOT NULL,
    token_id          UUID NOT NULL,
    fee_type          TEXT NOT NULL CHECK (fee_type IN ('processing','network','withdrawal','sweep','payout','settlement','refund')),

    -- Resources
    amount            BIGINT NOT NULL,

    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Immutable ledger: no UPDATE trigger, only DELETE + INSERT
CREATE INDEX IF NOT EXISTS idx_reg_crypto_fee_movements_recorder ON reg_crypto_fee_movements (recorder_id);
CREATE INDEX IF NOT EXISTS idx_reg_crypto_fee_movements_merchant ON reg_crypto_fee_movements (merchant_id, token_id, period);

-- Balance materialized table (maintained by trigger)
CREATE TABLE IF NOT EXISTS reg_crypto_fee_balances (
    merchant_id   UUID NOT NULL,
    token_id      UUID NOT NULL,
    fee_type      TEXT NOT NULL,
    balance       BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (merchant_id, token_id, fee_type)
);

-- Auto-update balance on movement insert/delete
CREATE OR REPLACE FUNCTION fn_crypto_fee_balance_update() RETURNS TRIGGER AS $$
DECLARE
    v_delta BIGINT;
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.record_type = 'receipt' THEN v_delta := -OLD.amount;
        ELSE v_delta := OLD.amount;
        END IF;
        INSERT INTO reg_crypto_fee_balances (merchant_id, token_id, fee_type, balance)
        VALUES (OLD.merchant_id, OLD.token_id, OLD.fee_type, v_delta)
        ON CONFLICT (merchant_id, token_id, fee_type)
        DO UPDATE SET balance = reg_crypto_fee_balances.balance + v_delta;
        RETURN OLD;
    ELSE
        IF NEW.record_type = 'receipt' THEN v_delta := NEW.amount;
        ELSE v_delta := -NEW.amount;
        END IF;
        INSERT INTO reg_crypto_fee_balances (merchant_id, token_id, fee_type, balance)
        VALUES (NEW.merchant_id, NEW.token_id, NEW.fee_type, v_delta)
        ON CONFLICT (merchant_id, token_id, fee_type)
        DO UPDATE SET balance = reg_crypto_fee_balances.balance + v_delta;
        RETURN NEW;
    END IF;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_crypto_fee_balance
    AFTER INSERT OR DELETE ON reg_crypto_fee_movements
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_fee_balance_update();

-- ═══════════════════════════════════════════════════════════════════════
-- FEE SCHEDULE (Регистр сведений «Тарифы комиссий»)
-- NULL merchant_id = global default for all merchants
-- ═══════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS reg_fee_schedule (
    -- Dimensions
    merchant_id  UUID REFERENCES cat_merchants(id),   -- NULL = global default
    token_id     UUID NOT NULL REFERENCES cat_tokens(id),
    direction    TEXT NOT NULL CHECK (direction IN ('processing', 'withdrawal', 'payout', 'settlement', 'refund')),

    -- Fee formula: clamp(fixed_fee + amount × percent_bp / 10000, min_fee, max_fee)
    fixed_fee    BIGINT NOT NULL DEFAULT 0,           -- fixed part (token minor units)
    percent_bp   INT    NOT NULL DEFAULT 0,           -- basis points [0..10000]
    min_fee      BIGINT NOT NULL DEFAULT 0,           -- minimum fee (0 = no floor)
    max_fee      BIGINT NOT NULL DEFAULT 0,           -- maximum fee (0 = no cap)

    -- Audit
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT chk_fs_percent CHECK (percent_bp >= 0 AND percent_bp <= 10000),
    CONSTRAINT chk_fs_fixed   CHECK (fixed_fee >= 0),
    CONSTRAINT chk_fs_min     CHECK (min_fee >= 0),
    CONSTRAINT chk_fs_max     CHECK (max_fee >= 0)
);

-- Composite unique: COALESCE maps NULL merchant_id → zero UUID for uniqueness
CREATE UNIQUE INDEX IF NOT EXISTS idx_fee_schedule_pk
    ON reg_fee_schedule (COALESCE(merchant_id, '00000000-0000-0000-0000-000000000000'::UUID), token_id, direction);

COMMENT ON TABLE reg_fee_schedule IS
    'Регистр сведений: тарифы комиссий. NULL merchant_id = глобальный дефолт.';

-- Fast lookup: merchant-specific fee
CREATE INDEX IF NOT EXISTS idx_fee_schedule_merchant ON reg_fee_schedule (merchant_id, token_id, direction)
    WHERE merchant_id IS NOT NULL;

-- Fast lookup: global defaults
CREATE INDEX IF NOT EXISTS idx_fee_schedule_global ON reg_fee_schedule (token_id, direction)
    WHERE merchant_id IS NULL;

-- ═══════════════════════════════════════════════════════════════════════
-- PAYMENT EVENT LOG (FSM audit trail)
-- ═══════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS reg_crypto_payment_events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_id    UUID NOT NULL REFERENCES doc_crypto_payments(id),
    from_status   TEXT NOT NULL,
    to_status     TEXT NOT NULL,
    event_type    TEXT NOT NULL,
    metadata      JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_crypto_payment_events_payment ON reg_crypto_payment_events (payment_id, created_at);

-- ═══════════════════════════════════════════════════════════════════════
-- RECALCULATE function for reconciliation
-- ═══════════════════════════════════════════════════════════════════════

CREATE OR REPLACE FUNCTION recalculate_crypto_fee() RETURNS void AS $$
BEGIN
    TRUNCATE reg_crypto_fee_balances;
    INSERT INTO reg_crypto_fee_balances (merchant_id, token_id, fee_type, balance)
    SELECT
        merchant_id,
        token_id,
        fee_type,
        SUM(CASE WHEN record_type = 'receipt' THEN amount ELSE -amount END) AS balance
    FROM reg_crypto_fee_movements
    GROUP BY merchant_id, token_id, fee_type;
END;
$$ LANGUAGE plpgsql;

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS reg_fee_schedule CASCADE;
DROP TABLE IF EXISTS reg_crypto_payment_events CASCADE;
DROP TABLE IF EXISTS reg_crypto_fee_balances CASCADE;
DROP TABLE IF EXISTS reg_crypto_fee_movements CASCADE;
DROP TABLE IF EXISTS doc_crypto_payments CASCADE;
