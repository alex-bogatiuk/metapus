-- +goose Up
-- Description: Cost accumulation register (Регистр накопления "Себестоимость товаров")

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- ── Movements ──────────────────────────────────────────────────────────────
CREATE TABLE reg_cost_movements (
    line_id          UUID        PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    recorder_id      UUID        NOT NULL,
    recorder_type    VARCHAR(50) NOT NULL,
    recorder_version INT         NOT NULL DEFAULT 1,
    period           TIMESTAMPTZ NOT NULL,
    record_type      VARCHAR(10) NOT NULL,
    warehouse_id     UUID        NOT NULL,
    nomenclature_id       UUID        NOT NULL,
    currency_id      UUID        NOT NULL,
    quantity         BIGINT      NOT NULL,
    amount           BIGINT      NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_cost_record_type      CHECK (record_type IN ('receipt', 'expense')),
    CONSTRAINT chk_cost_quantity_positive CHECK (quantity > 0),
    CONSTRAINT chk_cost_amount_positive  CHECK (amount > 0)
);

COMMENT ON TABLE reg_cost_movements IS 'Регистр себестоимости товаров — движения';

CREATE INDEX idx_reg_cost_movements_recorder
    ON reg_cost_movements (recorder_id, recorder_version);
CREATE INDEX idx_reg_cost_movements_balance
    ON reg_cost_movements (warehouse_id, nomenclature_id, currency_id, record_type);
CREATE INDEX idx_reg_cost_movements_period
    ON reg_cost_movements (period);
CREATE INDEX idx_reg_cost_movements_product
    ON reg_cost_movements (nomenclature_id, period DESC);

-- ── Balances ───────────────────────────────────────────────────────────────
CREATE TABLE reg_cost_balances (
    warehouse_id     UUID        NOT NULL,
    nomenclature_id       UUID        NOT NULL,
    currency_id      UUID        NOT NULL,
    quantity         BIGINT      NOT NULL DEFAULT 0,
    amount           BIGINT      NOT NULL DEFAULT 0,
    last_movement_at TIMESTAMPTZ,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (warehouse_id, nomenclature_id, currency_id)
);

COMMENT ON TABLE reg_cost_balances IS 'Регистр себестоимости товаров — текущие остатки';

CREATE INDEX idx_reg_cost_balances_product
    ON reg_cost_balances (nomenclature_id) WHERE quantity != 0;

-- ── Trigger ────────────────────────────────────────────────────────────────
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

-- ── Full recalculation ─────────────────────────────────────────────────────
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION recalculate_cost_balance()
RETURNS void AS $func$
BEGIN
    TRUNCATE reg_cost_balances;
    INSERT INTO reg_cost_balances (warehouse_id, nomenclature_id, currency_id, quantity, amount, last_movement_at, updated_at)
    SELECT
        warehouse_id,
        nomenclature_id,
        currency_id,
        SUM(CASE WHEN record_type = 'receipt' THEN quantity  ELSE -quantity END),
        SUM(CASE WHEN record_type = 'receipt' THEN amount   ELSE -amount   END),
        MAX(period),
        NOW()
    FROM reg_cost_movements
    GROUP BY warehouse_id, nomenclature_id, currency_id;
END;
$func$ LANGUAGE plpgsql;
-- +goose StatementEnd

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
SELECT pg_advisory_lock(hashtext('metapus_migrations'));
DROP FUNCTION IF EXISTS recalculate_cost_balance();
DROP TRIGGER IF EXISTS trg_cost_movements_balance ON reg_cost_movements;
DROP FUNCTION IF EXISTS update_cost_balance();
DROP TABLE IF EXISTS reg_cost_balances;
DROP TABLE IF EXISTS reg_cost_movements;
SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
