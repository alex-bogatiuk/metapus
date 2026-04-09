-- +goose Up
-- Description: Settlement accumulation register (Регистр накопления "Взаиморасчёты")

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- ── Movements ──────────────────────────────────────────────────────────────
CREATE TABLE reg_settlement_movements (
    line_id          UUID        PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    recorder_id      UUID        NOT NULL,
    recorder_type    VARCHAR(50) NOT NULL,
    recorder_version INT         NOT NULL DEFAULT 1,
    period           TIMESTAMPTZ NOT NULL,
    record_type      VARCHAR(10) NOT NULL,
    counterparty_id  UUID        NOT NULL,
    contract_id      UUID,
    currency_id      UUID        NOT NULL,
    amount           BIGINT      NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_settlement_record_type CHECK (record_type IN ('receipt', 'expense')),
    CONSTRAINT chk_settlement_amount_positive CHECK (amount > 0)
);

COMMENT ON TABLE reg_settlement_movements IS 'Регистр взаиморасчётов — движения';

CREATE INDEX idx_reg_settlement_movements_recorder
    ON reg_settlement_movements (recorder_id, recorder_version);
CREATE INDEX idx_reg_settlement_movements_balance
    ON reg_settlement_movements (counterparty_id, contract_id, currency_id, record_type);
CREATE INDEX idx_reg_settlement_movements_period
    ON reg_settlement_movements (period);
CREATE INDEX idx_reg_settlement_movements_counterparty
    ON reg_settlement_movements (counterparty_id, period DESC);

-- ── Balances ───────────────────────────────────────────────────────────────
-- Uses partial unique indexes to handle nullable contract_id
CREATE TABLE reg_settlement_balances (
    counterparty_id  UUID        NOT NULL,
    contract_id      UUID,
    currency_id      UUID        NOT NULL,
    amount           BIGINT      NOT NULL DEFAULT 0,
    last_movement_at TIMESTAMPTZ,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE reg_settlement_balances IS 'Регистр взаиморасчётов — текущие остатки';

CREATE UNIQUE INDEX idx_reg_settlement_balances_pk_with_contract
    ON reg_settlement_balances (counterparty_id, contract_id, currency_id)
    WHERE contract_id IS NOT NULL;

CREATE UNIQUE INDEX idx_reg_settlement_balances_pk_without_contract
    ON reg_settlement_balances (counterparty_id, currency_id)
    WHERE contract_id IS NULL;

CREATE INDEX idx_reg_settlement_balances_counterparty
    ON reg_settlement_balances (counterparty_id)
    WHERE amount != 0;

-- ── Trigger ────────────────────────────────────────────────────────────────
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_settlement_balance()
RETURNS TRIGGER AS $func$
DECLARE
    v_signed_amt   BIGINT;
    v_cpty_id      UUID;
    v_contract_id  UUID;
    v_currency_id  UUID;
    v_period       TIMESTAMPTZ;
BEGIN
    IF TG_OP = 'DELETE' THEN
        v_cpty_id     := OLD.counterparty_id;
        v_contract_id := OLD.contract_id;
        v_currency_id := OLD.currency_id;
        v_period      := OLD.period;
        IF OLD.record_type = 'receipt' THEN
            v_signed_amt := -OLD.amount;
        ELSE
            v_signed_amt := OLD.amount;
        END IF;
    ELSE
        v_cpty_id     := NEW.counterparty_id;
        v_contract_id := NEW.contract_id;
        v_currency_id := NEW.currency_id;
        v_period      := NEW.period;
        IF NEW.record_type = 'receipt' THEN
            v_signed_amt := NEW.amount;
        ELSE
            v_signed_amt := -NEW.amount;
        END IF;
    END IF;

    IF v_contract_id IS NOT NULL THEN
        INSERT INTO reg_settlement_balances (counterparty_id, contract_id, currency_id, amount, last_movement_at, updated_at)
        VALUES (v_cpty_id, v_contract_id, v_currency_id, v_signed_amt, v_period, NOW())
        ON CONFLICT (counterparty_id, contract_id, currency_id) WHERE contract_id IS NOT NULL
        DO UPDATE SET
            amount = reg_settlement_balances.amount + v_signed_amt,
            last_movement_at = GREATEST(reg_settlement_balances.last_movement_at, v_period),
            updated_at = NOW();
    ELSE
        INSERT INTO reg_settlement_balances (counterparty_id, contract_id, currency_id, amount, last_movement_at, updated_at)
        VALUES (v_cpty_id, NULL, v_currency_id, v_signed_amt, v_period, NOW())
        ON CONFLICT (counterparty_id, currency_id) WHERE contract_id IS NULL
        DO UPDATE SET
            amount = reg_settlement_balances.amount + v_signed_amt,
            last_movement_at = GREATEST(reg_settlement_balances.last_movement_at, v_period),
            updated_at = NOW();
    END IF;

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    ELSE
        RETURN NEW;
    END IF;
END;
$func$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_settlement_movements_balance
    AFTER INSERT OR DELETE ON reg_settlement_movements
    FOR EACH ROW
    EXECUTE FUNCTION update_settlement_balance();

-- ── Full recalculation ─────────────────────────────────────────────────────
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION recalculate_settlement_balance()
RETURNS void AS $func$
BEGIN
    TRUNCATE reg_settlement_balances;

    -- With contract
    INSERT INTO reg_settlement_balances (counterparty_id, contract_id, currency_id, amount, last_movement_at, updated_at)
    SELECT
        counterparty_id,
        contract_id,
        currency_id,
        SUM(CASE WHEN record_type = 'receipt' THEN amount ELSE -amount END),
        MAX(period),
        NOW()
    FROM reg_settlement_movements
    WHERE contract_id IS NOT NULL
    GROUP BY counterparty_id, contract_id, currency_id;

    -- Without contract
    INSERT INTO reg_settlement_balances (counterparty_id, contract_id, currency_id, amount, last_movement_at, updated_at)
    SELECT
        counterparty_id,
        NULL,
        currency_id,
        SUM(CASE WHEN record_type = 'receipt' THEN amount ELSE -amount END),
        MAX(period),
        NOW()
    FROM reg_settlement_movements
    WHERE contract_id IS NULL
    GROUP BY counterparty_id, currency_id;
END;
$func$ LANGUAGE plpgsql;
-- +goose StatementEnd

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
SELECT pg_advisory_lock(hashtext('metapus_migrations'));
DROP FUNCTION IF EXISTS recalculate_settlement_balance();
DROP TRIGGER IF EXISTS trg_settlement_movements_balance ON reg_settlement_movements;
DROP FUNCTION IF EXISTS update_settlement_balance();
DROP TABLE IF EXISTS reg_settlement_balances;
DROP TABLE IF EXISTS reg_settlement_movements;
SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
