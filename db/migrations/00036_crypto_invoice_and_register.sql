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
    created_by UUID        NOT NULL,
    updated_by UUID        NOT NULL,

    -- Document fields
    number          VARCHAR(20)  NOT NULL,
    date            TIMESTAMPTZ  NOT NULL,
    posted          BOOLEAN      NOT NULL DEFAULT FALSE,
    posted_version  INT          NOT NULL DEFAULT 0,
    organization_id UUID         NOT NULL REFERENCES cat_organizations(id),
    basis_type      TEXT         NOT NULL DEFAULT '',
    basis_id        UUID,
    description     TEXT,

    -- CryptoInvoice-specific fields
    merchant_id    UUID    NOT NULL,                                   -- FK to future cat_merchants
    token_id       UUID    NOT NULL REFERENCES cat_tokens(id),
    wallet_id      UUID,                                               -- assigned during payment setup
    expected_amount NUMERIC NOT NULL,                                  -- CryptoAmount: NUMERIC for arbitrary precision
    received_amount NUMERIC NOT NULL DEFAULT 0,
    status         TEXT     NOT NULL DEFAULT 'created',                 -- InvoiceStatus enum (string)
    expires_at     TIMESTAMPTZ NOT NULL,
    callback_url   TEXT    NOT NULL DEFAULT '',
    external_id    TEXT    NOT NULL DEFAULT '',                         -- idempotency key
    order_id       TEXT    NOT NULL DEFAULT '',
    customer_email TEXT    NOT NULL DEFAULT '',

    CONSTRAINT uq_crypto_invoice_number UNIQUE (organization_id, number),
    CONSTRAINT chk_expected_amount_positive CHECK (expected_amount > 0),
    CONSTRAINT chk_received_amount_nonneg CHECK (received_amount >= 0),
    CONSTRAINT chk_status_valid CHECK (status IN ('created','partially_paid','paid','confirmed','expired','cancelled')),
    CONSTRAINT fk_crypto_invoices_created_by FOREIGN KEY (created_by) REFERENCES users(id),
    CONSTRAINT fk_crypto_invoices_updated_by FOREIGN KEY (updated_by) REFERENCES users(id)
);

-- ── Lines ──────────────────────────────────────────────────────────────────
CREATE TABLE doc_crypto_invoice_lines (
    line_id     UUID    PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    document_id UUID    NOT NULL REFERENCES doc_crypto_invoices(id) ON DELETE CASCADE,
    line_no     INT     NOT NULL,

    description TEXT    NOT NULL DEFAULT '',
    amount      NUMERIC NOT NULL,                                      -- CryptoAmount

    CONSTRAINT chk_crypto_invoice_line_amount CHECK (amount > 0),
    UNIQUE (document_id, line_no)
);

-- Header indexes
CREATE INDEX idx_crypto_invoices_date         ON doc_crypto_invoices (date DESC);
CREATE INDEX idx_crypto_invoices_merchant     ON doc_crypto_invoices (merchant_id);
CREATE INDEX idx_crypto_invoices_token        ON doc_crypto_invoices (token_id);
CREATE INDEX idx_crypto_invoices_wallet       ON doc_crypto_invoices (wallet_id) WHERE wallet_id IS NOT NULL;
CREATE INDEX idx_crypto_invoices_status       ON doc_crypto_invoices (status) WHERE status IN ('created', 'partially_paid', 'paid');
CREATE INDEX idx_crypto_invoices_external_id  ON doc_crypto_invoices (external_id) WHERE external_id != '';
CREATE INDEX idx_crypto_invoices_expires      ON doc_crypto_invoices (expires_at) WHERE status = 'created';
CREATE INDEX idx_crypto_invoices_posted       ON doc_crypto_invoices (posted) WHERE posted = FALSE;
CREATE INDEX idx_crypto_invoices_number_trgm  ON doc_crypto_invoices USING gin (number gin_trgm_ops);

-- CDC indexes & triggers
CREATE INDEX idx_doc_crypto_invoices_txid ON doc_crypto_invoices (_txid) WHERE _deleted_at IS NULL;

CREATE TRIGGER trg_doc_crypto_invoices_txid
    BEFORE UPDATE ON doc_crypto_invoices
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_doc_crypto_invoices_soft_delete
    BEFORE UPDATE OF deletion_mark ON doc_crypto_invoices
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

-- Line indexes
CREATE INDEX idx_crypto_invoice_lines_doc ON doc_crypto_invoice_lines (document_id);

-- Keyset pagination
CREATE INDEX idx_doc_crypto_invoices_date_id    ON doc_crypto_invoices (date DESC, id DESC);
CREATE INDEX idx_doc_crypto_invoices_created_id ON doc_crypto_invoices (created_at DESC, id DESC);

COMMENT ON TABLE doc_crypto_invoices IS 'Документ Крипто-инвойс — запрос на оплату криптовалютой';
COMMENT ON TABLE doc_crypto_invoice_lines IS 'Табличная часть документа Крипто-инвойс';
COMMENT ON COLUMN doc_crypto_invoices.expected_amount IS 'Ожидаемая сумма в минорных единицах токена (NUMERIC для произвольной точности)';
COMMENT ON COLUMN doc_crypto_invoices.received_amount IS 'Фактически полученная сумма';
COMMENT ON COLUMN doc_crypto_invoices.status IS 'Статус инвойса: created, partially_paid, paid, confirmed, expired, cancelled';
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
    amount           NUMERIC      NOT NULL,                            -- CryptoAmount
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
    amount           NUMERIC     NOT NULL DEFAULT 0,                   -- CryptoAmount
    last_movement_at TIMESTAMPTZ,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (wallet_id, token_id)
);

COMMENT ON TABLE reg_crypto_balance_balances IS 'Регистр крипто-балансов — текущие остатки';

CREATE INDEX idx_reg_crypto_balance_balances_token
    ON reg_crypto_balance_balances (token_id) WHERE amount != 0;
CREATE INDEX idx_reg_crypto_balance_balances_wallet
    ON reg_crypto_balance_balances (wallet_id) WHERE amount != 0;

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
-- +goose StatementEnd

-- ── Trigger: auto-update balances on movement insert/delete ────────────────
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_crypto_balance()
RETURNS TRIGGER AS $func$
DECLARE
    v_signed_amount NUMERIC;
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

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_lock(hashtext('metapus_migrations'));
DROP FUNCTION IF EXISTS recalculate_crypto_balance();
DROP TRIGGER IF EXISTS trg_crypto_balance_movements_balance ON reg_crypto_balance_movements;
DROP FUNCTION IF EXISTS update_crypto_balance();
DROP TABLE IF EXISTS reg_crypto_balance_balances;
DROP TABLE IF EXISTS reg_crypto_balance_movements;
DROP TABLE IF EXISTS doc_crypto_invoice_lines;
DROP TABLE IF EXISTS doc_crypto_invoices;
SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
-- +goose StatementEnd
