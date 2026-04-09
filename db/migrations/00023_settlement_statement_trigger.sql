-- +goose Up
-- Description: Optimize settlement balance trigger from row-level to statement-level.
-- Settlement uses partial unique indexes for nullable contract_id:
--   - (counterparty_id, contract_id, currency_id) WHERE contract_id IS NOT NULL
--   - (counterparty_id, currency_id) WHERE contract_id IS NULL
-- Statement-level trigger aggregates movements before upserting.

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- Drop old row-level trigger
DROP TRIGGER IF EXISTS trg_settlement_movements_balance ON reg_settlement_movements;
DROP FUNCTION IF EXISTS update_settlement_balance();

-- ── INSERT trigger ───────────────────────────────────────────────────────
-- Split into two UPSERTs per partial index (WITH/WITHOUT contract).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_settlement_balance_on_insert()
RETURNS TRIGGER AS $func$
BEGIN
    -- 1. Rows WITH contract_id
    INSERT INTO reg_settlement_balances (counterparty_id, contract_id, currency_id, amount, last_movement_at, updated_at)
    SELECT
        counterparty_id,
        contract_id,
        currency_id,
        SUM(CASE WHEN record_type = 'receipt' THEN amount ELSE -amount END),
        MAX(period),
        NOW()
    FROM new_rows
    WHERE contract_id IS NOT NULL
    GROUP BY counterparty_id, contract_id, currency_id
    ON CONFLICT (counterparty_id, contract_id, currency_id) WHERE contract_id IS NOT NULL
    DO UPDATE SET
        amount = reg_settlement_balances.amount + EXCLUDED.amount,
        last_movement_at = GREATEST(reg_settlement_balances.last_movement_at, EXCLUDED.last_movement_at),
        updated_at = NOW();

    -- 2. Rows WITHOUT contract_id
    INSERT INTO reg_settlement_balances (counterparty_id, contract_id, currency_id, amount, last_movement_at, updated_at)
    SELECT
        counterparty_id,
        NULL,
        currency_id,
        SUM(CASE WHEN record_type = 'receipt' THEN amount ELSE -amount END),
        MAX(period),
        NOW()
    FROM new_rows
    WHERE contract_id IS NULL
    GROUP BY counterparty_id, currency_id
    ON CONFLICT (counterparty_id, currency_id) WHERE contract_id IS NULL
    DO UPDATE SET
        amount = reg_settlement_balances.amount + EXCLUDED.amount,
        last_movement_at = GREATEST(reg_settlement_balances.last_movement_at, EXCLUDED.last_movement_at),
        updated_at = NOW();

    RETURN NULL;
END;
$func$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_settlement_movements_balance_insert
    AFTER INSERT ON reg_settlement_movements
    REFERENCING NEW TABLE AS new_rows
    FOR EACH STATEMENT
    EXECUTE FUNCTION update_settlement_balance_on_insert();

-- ── DELETE trigger ───────────────────────────────────────────────────────
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_settlement_balance_on_delete()
RETURNS TRIGGER AS $func$
BEGIN
    -- 1. Rows WITH contract_id (reverse: receipt → subtract, expense → add back)
    INSERT INTO reg_settlement_balances (counterparty_id, contract_id, currency_id, amount, last_movement_at, updated_at)
    SELECT
        counterparty_id,
        contract_id,
        currency_id,
        SUM(CASE WHEN record_type = 'receipt' THEN -amount ELSE amount END),
        NOW(),
        NOW()
    FROM old_rows
    WHERE contract_id IS NOT NULL
    GROUP BY counterparty_id, contract_id, currency_id
    ON CONFLICT (counterparty_id, contract_id, currency_id) WHERE contract_id IS NOT NULL
    DO UPDATE SET
        amount = reg_settlement_balances.amount + EXCLUDED.amount,
        updated_at = NOW();

    -- 2. Rows WITHOUT contract_id
    INSERT INTO reg_settlement_balances (counterparty_id, contract_id, currency_id, amount, last_movement_at, updated_at)
    SELECT
        counterparty_id,
        NULL,
        currency_id,
        SUM(CASE WHEN record_type = 'receipt' THEN -amount ELSE amount END),
        NOW(),
        NOW()
    FROM old_rows
    WHERE contract_id IS NULL
    GROUP BY counterparty_id, currency_id
    ON CONFLICT (counterparty_id, currency_id) WHERE contract_id IS NULL
    DO UPDATE SET
        amount = reg_settlement_balances.amount + EXCLUDED.amount,
        updated_at = NOW();

    RETURN NULL;
END;
$func$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_settlement_movements_balance_delete
    AFTER DELETE ON reg_settlement_movements
    REFERENCING OLD TABLE AS old_rows
    FOR EACH STATEMENT
    EXECUTE FUNCTION update_settlement_balance_on_delete();

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

DROP TRIGGER IF EXISTS trg_settlement_movements_balance_insert ON reg_settlement_movements;
DROP TRIGGER IF EXISTS trg_settlement_movements_balance_delete ON reg_settlement_movements;
DROP FUNCTION IF EXISTS update_settlement_balance_on_insert();
DROP FUNCTION IF EXISTS update_settlement_balance_on_delete();

-- Restore original row-level trigger
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

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
