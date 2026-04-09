-- +goose Up
-- Description: Optimize stock balance trigger from row-level to statement-level.
-- row-level: 1 UPSERT per movement row (100 rows = 100 UPSERTs)
-- statement-level: 1 aggregated UPSERT per dimension (100 rows = 1-10 UPSERTs)
-- PostgreSQL 10+ transition tables (REFERENCING NEW/OLD TABLE).

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- Drop old row-level trigger
DROP TRIGGER IF EXISTS trg_stock_movements_balance ON reg_stock_movements;
DROP FUNCTION IF EXISTS update_stock_balance();

-- ── INSERT trigger: batch aggregate new movements ────────────────────────────
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_stock_balance_on_insert()
RETURNS TRIGGER AS $func$
BEGIN
    INSERT INTO reg_stock_balances (warehouse_id, product_id, quantity, last_movement_at, updated_at)
    SELECT
        warehouse_id,
        product_id,
        SUM(CASE WHEN record_type = 'receipt' THEN quantity ELSE -quantity END),
        MAX(period),
        NOW()
    FROM new_rows
    GROUP BY warehouse_id, product_id
    ON CONFLICT (warehouse_id, product_id) DO UPDATE SET
        quantity = reg_stock_balances.quantity + EXCLUDED.quantity,
        last_movement_at = GREATEST(reg_stock_balances.last_movement_at, EXCLUDED.last_movement_at),
        updated_at = NOW();

    RETURN NULL;
END;
$func$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_stock_movements_balance_insert
    AFTER INSERT ON reg_stock_movements
    REFERENCING NEW TABLE AS new_rows
    FOR EACH STATEMENT
    EXECUTE FUNCTION update_stock_balance_on_insert();

-- ── DELETE trigger: batch reverse deleted movements ──────────────────────────
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_stock_balance_on_delete()
RETURNS TRIGGER AS $func$
BEGIN
    -- Reverse: receipt → subtract, expense → add back
    INSERT INTO reg_stock_balances (warehouse_id, product_id, quantity, last_movement_at, updated_at)
    SELECT
        warehouse_id,
        product_id,
        SUM(CASE WHEN record_type = 'receipt' THEN -quantity ELSE quantity END),
        NOW(),
        NOW()
    FROM old_rows
    GROUP BY warehouse_id, product_id
    ON CONFLICT (warehouse_id, product_id) DO UPDATE SET
        quantity = reg_stock_balances.quantity + EXCLUDED.quantity,
        updated_at = NOW();

    RETURN NULL;
END;
$func$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_stock_movements_balance_delete
    AFTER DELETE ON reg_stock_movements
    REFERENCING OLD TABLE AS old_rows
    FOR EACH STATEMENT
    EXECUTE FUNCTION update_stock_balance_on_delete();

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

DROP TRIGGER IF EXISTS trg_stock_movements_balance_insert ON reg_stock_movements;
DROP TRIGGER IF EXISTS trg_stock_movements_balance_delete ON reg_stock_movements;
DROP FUNCTION IF EXISTS update_stock_balance_on_insert();
DROP FUNCTION IF EXISTS update_stock_balance_on_delete();

-- Restore original row-level trigger
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_stock_balance()
RETURNS TRIGGER AS $func$
DECLARE
    v_signed_qty BIGINT;
    v_wh  UUID;
    v_pid UUID;
    v_per TIMESTAMPTZ;
BEGIN
    IF TG_OP = 'DELETE' THEN
        v_wh  := OLD.warehouse_id;
        v_pid := OLD.product_id;
        v_per := OLD.period;
        IF OLD.record_type = 'receipt' THEN
            v_signed_qty := -OLD.quantity;
        ELSE
            v_signed_qty := OLD.quantity;
        END IF;
    ELSE
        v_wh  := NEW.warehouse_id;
        v_pid := NEW.product_id;
        v_per := NEW.period;
        IF NEW.record_type = 'receipt' THEN
            v_signed_qty := NEW.quantity;
        ELSE
            v_signed_qty := -NEW.quantity;
        END IF;
    END IF;

    INSERT INTO reg_stock_balances (warehouse_id, product_id, quantity, last_movement_at, updated_at)
    VALUES (v_wh, v_pid, v_signed_qty, v_per, NOW())
    ON CONFLICT (warehouse_id, product_id) DO UPDATE SET
        quantity = reg_stock_balances.quantity + v_signed_qty,
        last_movement_at = GREATEST(reg_stock_balances.last_movement_at, v_per),
        updated_at = NOW();

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    ELSE
        RETURN NEW;
    END IF;
END;
$func$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_stock_movements_balance
    AFTER INSERT OR DELETE ON reg_stock_movements
    FOR EACH ROW
    EXECUTE FUNCTION update_stock_balance();

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
