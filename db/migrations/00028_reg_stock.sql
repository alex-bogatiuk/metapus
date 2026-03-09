-- +goose Up
-- Description: Stock Accumulation Register (Регистр накопления Остатки товаров)
-- Tracks product quantities in warehouses with versioned movements

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- Stock movements table (движения регистра)
-- Immutable: movements are never updated, only deleted when document is unposted
CREATE TABLE reg_stock_movements (
    -- Movement identification
    line_id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    
    -- Recorder (document that created this movement)
    recorder_id UUID NOT NULL,
    recorder_type VARCHAR(50) NOT NULL,
    recorder_version INT NOT NULL DEFAULT 1,
    
    -- Period for time-based queries
    period TIMESTAMPTZ NOT NULL,
    
    -- Movement type: receipt (приход) or expense (расход)
    record_type VARCHAR(10) NOT NULL,
    
    -- Multi-tenancy

    
    -- Dimensions (измерения регистра)
    warehouse_id UUID NOT NULL,
    product_id UUID NOT NULL,
    
    -- Resources (ресурсы регистра)
    -- Fixed-point scaled integer (scale=1e4). Matches Quantity in Go.
    quantity BIGINT NOT NULL,

    -- Metadata
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    -- Constraints
    CONSTRAINT chk_record_type CHECK (record_type IN ('receipt', 'expense')),
    CONSTRAINT chk_quantity_positive CHECK (quantity > 0)
);

-- Composite index for efficient document movement cleanup
-- This is the PRIMARY access pattern: delete all movements for a document version
CREATE INDEX idx_reg_stock_movements_recorder 
    ON reg_stock_movements (recorder_id, recorder_version);

-- Index for balance calculation: sum by dimensions
CREATE INDEX idx_reg_stock_movements_balance 
    ON reg_stock_movements (warehouse_id, product_id, record_type);

-- Index for period-based queries (monthly reports, period closes)
CREATE INDEX idx_reg_stock_movements_period 
    ON reg_stock_movements (period);

-- Index for product history
CREATE INDEX idx_reg_stock_movements_product 
    ON reg_stock_movements (product_id, period DESC);

-- Partitioning hint: For high-volume systems, partition by period (monthly)
-- ALTER TABLE reg_stock_movements PARTITION BY RANGE (period);

COMMENT ON TABLE reg_stock_movements IS 'Регистр накопления Остатки товаров - движения';
COMMENT ON COLUMN reg_stock_movements.recorder_version IS 'Document posting version - enables efficient cleanup on re-post';
COMMENT ON COLUMN reg_stock_movements.line_id IS 'Unique line identifier - replaces content-based hash for deterministic tracking';

-- Stock balances table (итоги регистра)
-- Materialized view of current balances for fast queries
-- Updated via trigger or batch job
CREATE TABLE reg_stock_balances (
    -- Dimensions (composite primary key)
    warehouse_id UUID NOT NULL,
    product_id UUID NOT NULL,

    
    -- Current balance
    -- Fixed-point scaled integer (scale=1e4)
    quantity BIGINT NOT NULL DEFAULT 0,
    
    -- Metadata for cache management
    last_movement_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    PRIMARY KEY (warehouse_id, product_id)
);

-- Index for warehouse stock report
CREATE INDEX idx_reg_stock_balances_warehouse 
    ON reg_stock_balances (warehouse_id) 
    WHERE quantity != 0;

-- Index for product availability across warehouses
CREATE INDEX idx_reg_stock_balances_product 
    ON reg_stock_balances (product_id) 
    WHERE quantity != 0;

COMMENT ON TABLE reg_stock_balances IS 'Регистр накопления Остатки товаров - текущие остатки';

-- Function to update balance after movement changes
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_stock_balance()
RETURNS TRIGGER AS $func$
DECLARE
    v_signed_qty BIGINT;
BEGIN
    -- Calculate signed quantity
    IF TG_OP = 'DELETE' THEN
        -- Reverse the movement
        IF OLD.record_type = 'receipt' THEN
            v_signed_qty := -OLD.quantity;
        ELSE
            v_signed_qty := OLD.quantity;
        END IF;
        
        -- Update balance
        INSERT INTO reg_stock_balances (warehouse_id, product_id, quantity, last_movement_at, updated_at)
        VALUES (OLD.warehouse_id, OLD.product_id, v_signed_qty, OLD.period, NOW())
        ON CONFLICT (warehouse_id, product_id) DO UPDATE SET
            quantity = reg_stock_balances.quantity + v_signed_qty,
            updated_at = NOW();
            
        RETURN OLD;
    ELSE
        -- INSERT: apply the movement
        IF NEW.record_type = 'receipt' THEN
            v_signed_qty := NEW.quantity;
        ELSE
            v_signed_qty := -NEW.quantity;
        END IF;
        
        -- Upsert balance
        INSERT INTO reg_stock_balances (warehouse_id, product_id, quantity, last_movement_at, updated_at)
        VALUES (NEW.warehouse_id, NEW.product_id, v_signed_qty, NEW.period, NOW())
        ON CONFLICT (warehouse_id, product_id) DO UPDATE SET
            quantity = reg_stock_balances.quantity + v_signed_qty,
            last_movement_at = GREATEST(reg_stock_balances.last_movement_at, NEW.period),
            updated_at = NOW();
            
        RETURN NEW;
    END IF;
END;
$func$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- Trigger to maintain balances automatically
CREATE TRIGGER trg_stock_movements_balance
    AFTER INSERT OR DELETE ON reg_stock_movements
    FOR EACH ROW
    EXECUTE FUNCTION update_stock_balance();

-- Function to recalculate balance from movements (for repair/audit)
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION recalculate_stock_balance(
    p_warehouse_id UUID DEFAULT NULL,
    p_product_id UUID DEFAULT NULL
)
RETURNS void AS $func$
BEGIN
    -- Delete existing balances for scope
    DELETE FROM reg_stock_balances 
    WHERE (p_warehouse_id IS NULL OR warehouse_id = p_warehouse_id)
      AND (p_product_id IS NULL OR product_id = p_product_id);
    
    -- Recalculate from movements
    INSERT INTO reg_stock_balances (warehouse_id, product_id, quantity, last_movement_at, updated_at)
    SELECT 
        warehouse_id,
        product_id,
        SUM(CASE WHEN record_type = 'receipt' THEN quantity ELSE -quantity END),
        MAX(period),
        NOW()
    FROM reg_stock_movements
    WHERE (p_warehouse_id IS NULL OR warehouse_id = p_warehouse_id)
      AND (p_product_id IS NULL OR product_id = p_product_id)
    GROUP BY warehouse_id, product_id
    HAVING SUM(CASE WHEN record_type = 'receipt' THEN quantity ELSE -quantity END) != 0;
END;
$func$ LANGUAGE plpgsql;
-- +goose StatementEnd

COMMENT ON FUNCTION recalculate_stock_balance IS 'Repairs stock balances from movements - use for audit/recovery';

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

DROP TRIGGER IF EXISTS trg_stock_movements_balance ON reg_stock_movements;
DROP FUNCTION IF EXISTS update_stock_balance();
DROP FUNCTION IF EXISTS recalculate_stock_balance(UUID, UUID);
DROP TABLE IF EXISTS reg_stock_balances;
DROP TABLE IF EXISTS reg_stock_movements;

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
