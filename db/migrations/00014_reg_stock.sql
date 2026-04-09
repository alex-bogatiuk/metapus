-- +goose Up
-- Description: Stock accumulation register (Регистр накопления "Остатки товаров")

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- ── Movements ──────────────────────────────────────────────────────────────
CREATE TABLE reg_stock_movements (
    line_id          UUID         PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    recorder_id      UUID         NOT NULL,
    recorder_type    VARCHAR(50)  NOT NULL,
    recorder_version INT          NOT NULL DEFAULT 1,
    period           TIMESTAMPTZ  NOT NULL,
    record_type      VARCHAR(10)  NOT NULL,
    warehouse_id     UUID         NOT NULL,
    product_id       UUID         NOT NULL,
    quantity         BIGINT       NOT NULL,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_stock_record_type     CHECK (record_type IN ('receipt', 'expense')),
    CONSTRAINT chk_stock_quantity_positive CHECK (quantity > 0)
);

COMMENT ON TABLE reg_stock_movements IS 'Регистр остатков товаров — движения';
COMMENT ON COLUMN reg_stock_movements.recorder_id IS 'ID of the posting document';
COMMENT ON COLUMN reg_stock_movements.recorder_type IS 'Document type (goods_receipt, goods_issue)';
COMMENT ON COLUMN reg_stock_movements.recorder_version IS 'Version of document at posting time (for idempotent re-posting)';
COMMENT ON COLUMN reg_stock_movements.record_type IS 'receipt = incoming, expense = outgoing';
COMMENT ON COLUMN reg_stock_movements.quantity IS 'Quantity in minor units (same scale as currency)';

CREATE INDEX idx_reg_stock_movements_recorder
    ON reg_stock_movements (recorder_id, recorder_version);
CREATE INDEX idx_reg_stock_movements_balance
    ON reg_stock_movements (warehouse_id, product_id, record_type);
CREATE INDEX idx_reg_stock_movements_period
    ON reg_stock_movements (period);
CREATE INDEX idx_reg_stock_movements_product
    ON reg_stock_movements (product_id, period DESC);

-- ── Balances ───────────────────────────────────────────────────────────────
CREATE TABLE reg_stock_balances (
    warehouse_id     UUID        NOT NULL,
    product_id       UUID        NOT NULL,
    quantity         BIGINT      NOT NULL DEFAULT 0,
    last_movement_at TIMESTAMPTZ,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (warehouse_id, product_id)
);

COMMENT ON TABLE reg_stock_balances IS 'Регистр остатков товаров — текущие остатки';

CREATE INDEX idx_reg_stock_balances_product
    ON reg_stock_balances (product_id) WHERE quantity != 0;
CREATE INDEX idx_reg_stock_balances_warehouse
    ON reg_stock_balances (warehouse_id) WHERE quantity != 0;

-- ── Trigger: auto-update balances on movement insert/delete ────────────────
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

-- ── Full recalculation function (for audit / recovery) ─────────────────────
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION recalculate_stock_balance()
RETURNS void AS $func$
BEGIN
    TRUNCATE reg_stock_balances;
    INSERT INTO reg_stock_balances (warehouse_id, product_id, quantity, last_movement_at, updated_at)
    SELECT
        warehouse_id,
        product_id,
        SUM(CASE WHEN record_type = 'receipt' THEN quantity ELSE -quantity END),
        MAX(period),
        NOW()
    FROM reg_stock_movements
    GROUP BY warehouse_id, product_id;
END;
$func$ LANGUAGE plpgsql;
-- +goose StatementEnd

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
SELECT pg_advisory_lock(hashtext('metapus_migrations'));
DROP FUNCTION IF EXISTS recalculate_stock_balance();
DROP TRIGGER IF EXISTS trg_stock_movements_balance ON reg_stock_movements;
DROP FUNCTION IF EXISTS update_stock_balance();
DROP TABLE IF EXISTS reg_stock_balances;
DROP TABLE IF EXISTS reg_stock_movements;
SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
