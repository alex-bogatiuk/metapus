-- +goose Up
-- Description: Optimize cost balance trigger from row-level to statement-level.
-- Same optimization as 00021 for stock: transition tables aggregate N movements
-- into 1 UPSERT per dimension (warehouse_id, nomenclature_id, currency_id).
-- Also fixes: DELETE path now uses GREATEST for last_movement_at consistency.

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- Drop old row-level trigger
DROP TRIGGER IF EXISTS trg_cost_movements_balance ON reg_cost_movements;
DROP FUNCTION IF EXISTS update_cost_balance();

-- ── INSERT trigger ───────────────────────────────────────────────────────
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_cost_balance_on_insert()
RETURNS TRIGGER AS $func$
BEGIN
    INSERT INTO reg_cost_balances (warehouse_id, nomenclature_id, currency_id, quantity, amount, last_movement_at, updated_at)
    SELECT
        warehouse_id,
        nomenclature_id,
        currency_id,
        SUM(CASE WHEN record_type = 'receipt' THEN quantity ELSE -quantity END),
        SUM(CASE WHEN record_type = 'receipt' THEN amount ELSE -amount END),
        MAX(period),
        NOW()
    FROM new_rows
    GROUP BY warehouse_id, nomenclature_id, currency_id
    ON CONFLICT (warehouse_id, nomenclature_id, currency_id) DO UPDATE SET
        quantity = reg_cost_balances.quantity + EXCLUDED.quantity,
        amount   = reg_cost_balances.amount + EXCLUDED.amount,
        last_movement_at = GREATEST(reg_cost_balances.last_movement_at, EXCLUDED.last_movement_at),
        updated_at = NOW();

    RETURN NULL;
END;
$func$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_cost_movements_balance_insert
    AFTER INSERT ON reg_cost_movements
    REFERENCING NEW TABLE AS new_rows
    FOR EACH STATEMENT
    EXECUTE FUNCTION update_cost_balance_on_insert();

-- ── DELETE trigger ───────────────────────────────────────────────────────
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_cost_balance_on_delete()
RETURNS TRIGGER AS $func$
BEGIN
    INSERT INTO reg_cost_balances (warehouse_id, nomenclature_id, currency_id, quantity, amount, last_movement_at, updated_at)
    SELECT
        warehouse_id,
        nomenclature_id,
        currency_id,
        SUM(CASE WHEN record_type = 'receipt' THEN -quantity ELSE quantity END),
        SUM(CASE WHEN record_type = 'receipt' THEN -amount ELSE amount END),
        NOW(),
        NOW()
    FROM old_rows
    GROUP BY warehouse_id, nomenclature_id, currency_id
    ON CONFLICT (warehouse_id, nomenclature_id, currency_id) DO UPDATE SET
        quantity = reg_cost_balances.quantity + EXCLUDED.quantity,
        amount   = reg_cost_balances.amount + EXCLUDED.amount,
        updated_at = NOW();

    RETURN NULL;
END;
$func$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_cost_movements_balance_delete
    AFTER DELETE ON reg_cost_movements
    REFERENCING OLD TABLE AS old_rows
    FOR EACH STATEMENT
    EXECUTE FUNCTION update_cost_balance_on_delete();

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

DROP TRIGGER IF EXISTS trg_cost_movements_balance_insert ON reg_cost_movements;
DROP TRIGGER IF EXISTS trg_cost_movements_balance_delete ON reg_cost_movements;
DROP FUNCTION IF EXISTS update_cost_balance_on_insert();
DROP FUNCTION IF EXISTS update_cost_balance_on_delete();

-- Restore original row-level trigger
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_cost_balance()
RETURNS TRIGGER AS $func$
DECLARE
    v_signed_qty BIGINT;
    v_signed_amt BIGINT;
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.record_type = 'receipt' THEN
            v_signed_qty := -OLD.quantity;
            v_signed_amt := -OLD.amount;
        ELSE
            v_signed_qty := OLD.quantity;
            v_signed_amt := OLD.amount;
        END IF;

        INSERT INTO reg_cost_balances (warehouse_id, nomenclature_id, currency_id, quantity, amount, last_movement_at, updated_at)
        VALUES (OLD.warehouse_id, OLD.nomenclature_id, OLD.currency_id, v_signed_qty, v_signed_amt, OLD.period, NOW())
        ON CONFLICT (warehouse_id, nomenclature_id, currency_id) DO UPDATE SET
            quantity = reg_cost_balances.quantity + v_signed_qty,
            amount   = reg_cost_balances.amount + v_signed_amt,
            updated_at = NOW();

        RETURN OLD;
    ELSE
        IF NEW.record_type = 'receipt' THEN
            v_signed_qty := NEW.quantity;
            v_signed_amt := NEW.amount;
        ELSE
            v_signed_qty := -NEW.quantity;
            v_signed_amt := -NEW.amount;
        END IF;

        INSERT INTO reg_cost_balances (warehouse_id, nomenclature_id, currency_id, quantity, amount, last_movement_at, updated_at)
        VALUES (NEW.warehouse_id, NEW.nomenclature_id, NEW.currency_id, v_signed_qty, v_signed_amt, NEW.period, NOW())
        ON CONFLICT (warehouse_id, nomenclature_id, currency_id) DO UPDATE SET
            quantity = reg_cost_balances.quantity + v_signed_qty,
            amount   = reg_cost_balances.amount + v_signed_amt,
            last_movement_at = GREATEST(reg_cost_balances.last_movement_at, NEW.period),
            updated_at = NOW();

        RETURN NEW;
    END IF;
END;
$func$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_cost_movements_balance
    AFTER INSERT OR DELETE ON reg_cost_movements
    FOR EACH ROW
    EXECUTE FUNCTION update_cost_balance();

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
