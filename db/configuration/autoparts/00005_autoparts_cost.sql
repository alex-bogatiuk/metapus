-- The cost of goods sold, averaged over the month (себестоимость, средняя
-- за месяц): the purchases register the goods receipt writes, the sales
-- register the goods issue writes with its revenue and the month closing
-- with the cost, the register of the stock's cost only the month closing
-- writes, and the month closing document. Goods receipts and issues are in
-- the cost sequence (sys_sequence_boundaries, key inventory_cost).

-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

CREATE TABLE reg_purchases_movements (
 line_id          UUID        PRIMARY KEY,
 recorder_id      UUID        NOT NULL,
 recorder_type    TEXT        NOT NULL,
 recorder_version INT         NOT NULL,
 period           TIMESTAMPTZ NOT NULL,
 record_type      TEXT        NOT NULL CHECK (record_type IN ('receipt', 'expense')),
 created_at       TIMESTAMPTZ NOT NULL,
 warehouse_id UUID NOT NULL REFERENCES cat_warehouses(id),
 item_id UUID NOT NULL REFERENCES cat_items(id),
 supplier_id UUID NOT NULL REFERENCES cat_counterparties(id),
 currency_id UUID NOT NULL REFERENCES cat_currencies(id),
 quantity BIGINT NOT NULL CHECK (quantity >= 0),
 amount BIGINT NOT NULL CHECK (amount >= 0)
);
COMMENT ON TABLE reg_purchases_movements IS 'Закупки — закуплено по складу, номенклатуре, поставщику и валюте';
CREATE INDEX idx_reg_purchases_movements_recorder ON reg_purchases_movements (recorder_id, recorder_version);
CREATE INDEX idx_reg_purchases_movements_dimensions ON reg_purchases_movements (warehouse_id, item_id, supplier_id, currency_id, period);
CREATE TRIGGER trg_reg_purchases_immutable BEFORE UPDATE ON reg_purchases_movements
    FOR EACH ROW EXECUTE FUNCTION sys_accumulation_movements_immutable();

CREATE TABLE reg_sales_movements (
 line_id          UUID        PRIMARY KEY,
 recorder_id      UUID        NOT NULL,
 recorder_type    TEXT        NOT NULL,
 recorder_version INT         NOT NULL,
 period           TIMESTAMPTZ NOT NULL,
 record_type      TEXT        NOT NULL CHECK (record_type IN ('receipt', 'expense')),
 created_at       TIMESTAMPTZ NOT NULL,
 warehouse_id UUID NOT NULL REFERENCES cat_warehouses(id),
 item_id UUID NOT NULL REFERENCES cat_items(id),
 customer_id UUID NOT NULL REFERENCES cat_counterparties(id),
 currency_id UUID NOT NULL REFERENCES cat_currencies(id),
 quantity BIGINT NOT NULL CHECK (quantity >= 0),
 revenue BIGINT NOT NULL CHECK (revenue >= 0),
 cost BIGINT NOT NULL CHECK (cost >= 0)
);
COMMENT ON TABLE reg_sales_movements IS 'Продажи — количество и выручка расходных накладных, себестоимость закрытия месяца';
CREATE INDEX idx_reg_sales_movements_recorder ON reg_sales_movements (recorder_id, recorder_version);
CREATE INDEX idx_reg_sales_movements_dimensions ON reg_sales_movements (warehouse_id, item_id, customer_id, currency_id, period);
CREATE TRIGGER trg_reg_sales_immutable BEFORE UPDATE ON reg_sales_movements
    FOR EACH ROW EXECUTE FUNCTION sys_accumulation_movements_immutable();

CREATE TABLE reg_inventory_cost_movements (
 line_id          UUID        PRIMARY KEY,
 recorder_id      UUID        NOT NULL,
 recorder_type    TEXT        NOT NULL,
 recorder_version INT         NOT NULL,
 period           TIMESTAMPTZ NOT NULL,
 record_type      TEXT        NOT NULL CHECK (record_type IN ('receipt', 'expense')),
 created_at       TIMESTAMPTZ NOT NULL,
 warehouse_id UUID NOT NULL REFERENCES cat_warehouses(id),
 item_id UUID NOT NULL REFERENCES cat_items(id),
 currency_id UUID NOT NULL REFERENCES cat_currencies(id),
 quantity BIGINT NOT NULL CHECK (quantity >= 0),
 amount BIGINT NOT NULL CHECK (amount >= 0)
);
COMMENT ON TABLE reg_inventory_cost_movements IS 'Себестоимость запасов — закупки месяца (приход) и продажи по средней (расход), пишет закрытие месяца';
CREATE INDEX idx_reg_inventory_cost_movements_recorder ON reg_inventory_cost_movements (recorder_id, recorder_version);
CREATE INDEX idx_reg_inventory_cost_movements_dimensions ON reg_inventory_cost_movements (warehouse_id, item_id, currency_id, period);
CREATE TRIGGER trg_reg_inventory_cost_immutable BEFORE UPDATE ON reg_inventory_cost_movements
    FOR EACH ROW EXECUTE FUNCTION sys_accumulation_movements_immutable();

CREATE TABLE reg_inventory_cost_balances (
 warehouse_id UUID NOT NULL REFERENCES cat_warehouses(id),
 item_id      UUID NOT NULL REFERENCES cat_items(id),
 currency_id  UUID NOT NULL REFERENCES cat_currencies(id),
 quantity     BIGINT      NOT NULL DEFAULT 0,
 amount       BIGINT      NOT NULL DEFAULT 0,
 updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY (warehouse_id, item_id, currency_id)
);
COMMENT ON TABLE reg_inventory_cost_balances IS 'Себестоимость запасов — количество и стоимость остатка, ведётся триггерами движений';
CREATE TRIGGER trg_reg_inventory_cost_balances_insert
    AFTER INSERT ON reg_inventory_cost_movements
    REFERENCING NEW TABLE AS new_rows
    FOR EACH STATEMENT EXECUTE FUNCTION sys_accumulation_balances('reg_inventory_cost_balances', 'warehouse_id,item_id,currency_id', 'quantity,amount');
CREATE TRIGGER trg_reg_inventory_cost_balances_delete
    AFTER DELETE ON reg_inventory_cost_movements
    REFERENCING OLD TABLE AS old_rows
    FOR EACH STATEMENT EXECUTE FUNCTION sys_accumulation_balances('reg_inventory_cost_balances', 'warehouse_id,item_id,currency_id', 'quantity,amount');

CREATE TABLE doc_month_closings (
 id UUID PRIMARY KEY,
 number TEXT NOT NULL DEFAULT '',
 date TIMESTAMPTZ NOT NULL,
 posted BOOLEAN NOT NULL DEFAULT FALSE,
 posted_version INT NOT NULL DEFAULT 0,
 basis_type TEXT NOT NULL DEFAULT '',
 basis_id UUID,
 description TEXT NOT NULL DEFAULT '',
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
COMMENT ON TABLE doc_month_closings IS 'Закрытие месяца: себестоимость продаж месяца по средней в валюте учёта';
CREATE UNIQUE INDEX idx_doc_month_closings_number ON doc_month_closings (number)
 WHERE _deleted_at IS NULL AND number != '';
CREATE INDEX idx_doc_month_closings_keyset ON doc_month_closings (date,id) WHERE _deleted_at IS NULL;
CREATE INDEX idx_doc_month_closings_txid ON doc_month_closings (_txid) WHERE _deleted_at IS NULL;
CREATE TRIGGER trg_doc_month_closings_txid BEFORE UPDATE ON doc_month_closings FOR EACH ROW EXECUTE FUNCTION update_txid_column();
CREATE TRIGGER trg_doc_month_closings_soft_delete BEFORE UPDATE OF deletion_mark ON doc_month_closings FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();
CREATE TRIGGER trg_doc_month_closings_updated_at BEFORE UPDATE ON doc_month_closings FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TABLE doc_month_closings;
DROP TABLE reg_inventory_cost_balances;
DROP TABLE reg_inventory_cost_movements;
DROP TABLE reg_sales_movements;
DROP TABLE reg_purchases_movements;
-- +goose StatementEnd
