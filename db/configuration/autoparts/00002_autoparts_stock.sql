-- Goods receipts and the warehouse stock register (ЗапасыНаСкладах):
-- quantities by warehouse and item. A statement trigger keeps the balances
-- in step with the movements.

-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- Function bodies are validated when first executed; definitions precede the tables they use.
SET LOCAL check_function_bodies = off;

CREATE OR REPLACE FUNCTION update_warehouse_stock_balance_on_insert()
RETURNS TRIGGER AS $func$
BEGIN
    INSERT INTO reg_warehouse_stock_balances (warehouse_id, item_id, quantity, updated_at)
    SELECT warehouse_id, item_id,
           SUM(CASE WHEN record_type = 'receipt' THEN quantity ELSE -quantity END),
           NOW()
    FROM new_rows
    GROUP BY warehouse_id, item_id
    ON CONFLICT (warehouse_id, item_id)
    DO UPDATE SET quantity = reg_warehouse_stock_balances.quantity + EXCLUDED.quantity,
                  updated_at = NOW();
    RETURN NULL;
END;
$func$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION update_warehouse_stock_balance_on_delete()
RETURNS TRIGGER AS $func$
BEGIN
    INSERT INTO reg_warehouse_stock_balances (warehouse_id, item_id, quantity, updated_at)
    SELECT warehouse_id, item_id,
           SUM(CASE WHEN record_type = 'receipt' THEN -quantity ELSE quantity END),
           NOW()
    FROM old_rows
    GROUP BY warehouse_id, item_id
    ON CONFLICT (warehouse_id, item_id)
    DO UPDATE SET quantity = reg_warehouse_stock_balances.quantity + EXCLUDED.quantity,
                  updated_at = NOW();
    RETURN NULL;
END;
$func$ LANGUAGE plpgsql;

CREATE TABLE doc_goods_receipts (
 id UUID PRIMARY KEY,
 number TEXT NOT NULL DEFAULT '',
 date TIMESTAMPTZ NOT NULL,
 posted BOOLEAN NOT NULL DEFAULT FALSE,
 posted_version INT NOT NULL DEFAULT 0,
 basis_type TEXT NOT NULL DEFAULT '',
 basis_id UUID,
 description TEXT NOT NULL DEFAULT '',
 supplier_id UUID NOT NULL REFERENCES cat_counterparties(id),
 warehouse_id UUID NOT NULL REFERENCES cat_warehouses(id),
 currency_id UUID NOT NULL REFERENCES cat_currencies(id),
 created_by UUID,
 updated_by UUID,
 deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
 version INT NOT NULL DEFAULT 1,
 attributes JSONB NOT NULL DEFAULT '{}',
 _deleted_at TIMESTAMPTZ,
 _txid BIGINT NOT NULL DEFAULT txid_current(),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE doc_goods_receipts IS 'Приходная накладная: поступление запчастей от поставщика на склад';
CREATE UNIQUE INDEX idx_doc_goods_receipts_number ON doc_goods_receipts (number)
 WHERE _deleted_at IS NULL AND number != '';
CREATE INDEX idx_doc_goods_receipts_keyset ON doc_goods_receipts (date,id) WHERE _deleted_at IS NULL;
CREATE INDEX idx_doc_goods_receipts_txid ON doc_goods_receipts (_txid) WHERE _deleted_at IS NULL;
CREATE TRIGGER trg_doc_goods_receipts_txid BEFORE UPDATE ON doc_goods_receipts FOR EACH ROW EXECUTE FUNCTION update_txid_column();
CREATE TRIGGER trg_doc_goods_receipts_soft_delete BEFORE UPDATE OF deletion_mark ON doc_goods_receipts FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();
CREATE TRIGGER trg_doc_goods_receipts_updated_at BEFORE UPDATE ON doc_goods_receipts FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Table part goods: one row per line, replaced with the document.
CREATE TABLE doc_goods_receipts__goods (
 line_id UUID PRIMARY KEY,
 document_id UUID NOT NULL REFERENCES doc_goods_receipts(id) ON DELETE CASCADE,
 line_no INT NOT NULL,
 item_id UUID NOT NULL REFERENCES cat_items(id),
 quantity BIGINT NOT NULL CHECK (quantity > 0),
 price BIGINT NOT NULL DEFAULT 0 CHECK (price >= 0),
 amount BIGINT NOT NULL DEFAULT 0 CHECK (amount >= 0)
);
CREATE INDEX idx_doc_goods_receipts__goods_document ON doc_goods_receipts__goods (document_id, line_no);

CREATE TABLE reg_warehouse_stock_movements (
 line_id          UUID        PRIMARY KEY DEFAULT gen_random_uuid_v7(),
 recorder_id      UUID        NOT NULL,
 recorder_type    VARCHAR(50) NOT NULL,
 recorder_version INT         NOT NULL DEFAULT 1,
 period           TIMESTAMPTZ NOT NULL,
 record_type      VARCHAR(10) NOT NULL,
 warehouse_id     UUID        NOT NULL REFERENCES cat_warehouses(id),
 item_id          UUID        NOT NULL REFERENCES cat_items(id),
 quantity         BIGINT      NOT NULL,
 created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 CONSTRAINT chk_warehouse_stock_record_type CHECK (record_type IN ('receipt', 'expense')),
 CONSTRAINT chk_warehouse_stock_quantity_positive CHECK (quantity > 0)
);
COMMENT ON TABLE reg_warehouse_stock_movements IS 'Запасы на складах — движения (количество в 1/10000 единицы)';
CREATE INDEX idx_reg_warehouse_stock_movements_recorder ON reg_warehouse_stock_movements (recorder_id, recorder_version);
CREATE INDEX idx_reg_warehouse_stock_movements_balance ON reg_warehouse_stock_movements (warehouse_id, item_id, period);

CREATE TABLE reg_warehouse_stock_balances (
 warehouse_id UUID        NOT NULL REFERENCES cat_warehouses(id),
 item_id      UUID        NOT NULL REFERENCES cat_items(id),
 quantity     BIGINT      NOT NULL DEFAULT 0,
 updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY (warehouse_id, item_id)
);
COMMENT ON TABLE reg_warehouse_stock_balances IS 'Запасы на складах — остатки, ведутся триггерами движений';

CREATE TRIGGER trg_warehouse_stock_balance_insert
    AFTER INSERT ON reg_warehouse_stock_movements
    REFERENCING NEW TABLE AS new_rows
    FOR EACH STATEMENT EXECUTE FUNCTION update_warehouse_stock_balance_on_insert();
CREATE TRIGGER trg_warehouse_stock_balance_delete
    AFTER DELETE ON reg_warehouse_stock_movements
    REFERENCING OLD TABLE AS old_rows
    FOR EACH STATEMENT EXECUTE FUNCTION update_warehouse_stock_balance_on_delete();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TABLE reg_warehouse_stock_balances;
DROP TABLE reg_warehouse_stock_movements;
DROP FUNCTION update_warehouse_stock_balance_on_delete();
DROP FUNCTION update_warehouse_stock_balance_on_insert();
DROP TABLE doc_goods_receipts__goods;
DROP TABLE doc_goods_receipts;
-- +goose StatementEnd
