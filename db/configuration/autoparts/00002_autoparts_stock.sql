-- Customer orders, goods receipts and the stock registers:
-- - warehouse stock (ЗапасыНаСкладах): quantities by warehouse and item;
-- - stock (Запасы): by warehouse, item and customer order, the zero order
--   (00000000-…) being the free stock and any other a reserve;
-- - customer orders (ЗаказыПокупателей): what an order still awaits.
-- The platform's statement triggers keep the balances in step with the
-- movements.

-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

CREATE TABLE doc_customer_orders (
 id UUID PRIMARY KEY,
 number TEXT NOT NULL DEFAULT '',
 date TIMESTAMPTZ NOT NULL,
 posted BOOLEAN NOT NULL DEFAULT FALSE,
 posted_version INT NOT NULL DEFAULT 0,
 basis_type TEXT NOT NULL DEFAULT '',
 basis_id UUID,
 description TEXT NOT NULL DEFAULT '',
 customer_id UUID NOT NULL REFERENCES cat_counterparties(id),
 warehouse_id UUID NOT NULL REFERENCES cat_warehouses(id),
 currency_id UUID NOT NULL REFERENCES cat_currencies(id),
 state_id UUID REFERENCES cat_customer_order_states(id),
 status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'in_progress', 'completed', 'cancelled')),
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
COMMENT ON TABLE doc_customer_orders IS 'Заказ покупателя: заказанные запчасти и их резерв на складе';
CREATE UNIQUE INDEX idx_doc_customer_orders_number ON doc_customer_orders (number)
 WHERE _deleted_at IS NULL AND number != '';
CREATE INDEX idx_doc_customer_orders_keyset ON doc_customer_orders (date,id) WHERE _deleted_at IS NULL;
CREATE INDEX idx_doc_customer_orders_txid ON doc_customer_orders (_txid) WHERE _deleted_at IS NULL;
CREATE TRIGGER trg_doc_customer_orders_txid BEFORE UPDATE ON doc_customer_orders FOR EACH ROW EXECUTE FUNCTION update_txid_column();
CREATE TRIGGER trg_doc_customer_orders_soft_delete BEFORE UPDATE OF deletion_mark ON doc_customer_orders FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();
CREATE TRIGGER trg_doc_customer_orders_updated_at BEFORE UPDATE ON doc_customer_orders FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Table part goods: reserve is how much of the quantity is reserved now;
-- cancelled and released are what a closed order no longer ships and the
-- reserve it gave back (the order fills them when it is closed).
CREATE TABLE doc_customer_orders__goods (
 line_id UUID PRIMARY KEY,
 document_id UUID NOT NULL REFERENCES doc_customer_orders(id) ON DELETE CASCADE,
 line_no INT NOT NULL,
 item_id UUID NOT NULL REFERENCES cat_items(id),
 quantity BIGINT NOT NULL CHECK (quantity > 0),
 reserve BIGINT NOT NULL DEFAULT 0 CHECK (reserve >= 0 AND reserve <= quantity),
 price BIGINT NOT NULL DEFAULT 0 CHECK (price >= 0),
 amount BIGINT NOT NULL DEFAULT 0 CHECK (amount >= 0),
 cancelled BIGINT NOT NULL DEFAULT 0 CHECK (cancelled >= 0 AND cancelled <= quantity),
 released BIGINT NOT NULL DEFAULT 0 CHECK (released >= 0)
);
CREATE INDEX idx_doc_customer_orders__goods_document ON doc_customer_orders__goods (document_id, line_no);

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
 amount BIGINT NOT NULL DEFAULT 0 CHECK (amount >= 0),
 customer_order_id UUID REFERENCES doc_customer_orders(id)
);
CREATE INDEX idx_doc_goods_receipts__goods_document ON doc_goods_receipts__goods (document_id, line_no);

-- The registers are declared in Go (domain/registers/*); their tables follow
-- migration.AccumulationDDL of each declaration. The customer order
-- dimension has no foreign key: the free stock has the zero order, and an
-- order's movements must not depend on when its own row is written, and the tenant
-- migration checks them against it. The platform's shared triggers keep the
-- balances (db/migrations/00006_accumulation_registers.sql).
CREATE TABLE reg_warehouse_stock_movements (
 line_id          UUID        PRIMARY KEY,
 recorder_id      UUID        NOT NULL,
 recorder_type    TEXT        NOT NULL,
 recorder_version INT         NOT NULL,
 period           TIMESTAMPTZ NOT NULL,
 record_type      TEXT        NOT NULL CHECK (record_type IN ('receipt', 'expense')),
 created_at       TIMESTAMPTZ NOT NULL,
 warehouse_id     UUID        NOT NULL REFERENCES cat_warehouses(id),
 item_id          UUID        NOT NULL REFERENCES cat_items(id),
 quantity         BIGINT      NOT NULL CHECK (quantity > 0)
);
COMMENT ON TABLE reg_warehouse_stock_movements IS 'Запасы на складах — движения (количество в 1/10000 единицы)';
CREATE INDEX idx_reg_warehouse_stock_movements_recorder ON reg_warehouse_stock_movements (recorder_id, recorder_version);
CREATE INDEX idx_reg_warehouse_stock_movements_dimensions ON reg_warehouse_stock_movements (warehouse_id, item_id, period);
CREATE TRIGGER trg_reg_warehouse_stock_immutable BEFORE UPDATE ON reg_warehouse_stock_movements
    FOR EACH ROW EXECUTE FUNCTION sys_accumulation_movements_immutable();

CREATE TABLE reg_warehouse_stock_balances (
 warehouse_id UUID        NOT NULL REFERENCES cat_warehouses(id),
 item_id      UUID        NOT NULL REFERENCES cat_items(id),
 quantity     BIGINT      NOT NULL DEFAULT 0,
 updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY (warehouse_id, item_id)
);
COMMENT ON TABLE reg_warehouse_stock_balances IS 'Запасы на складах — остатки, ведутся триггерами движений';
CREATE TRIGGER trg_reg_warehouse_stock_balances_insert
    AFTER INSERT ON reg_warehouse_stock_movements
    REFERENCING NEW TABLE AS new_rows
    FOR EACH STATEMENT EXECUTE FUNCTION sys_accumulation_balances('reg_warehouse_stock_balances', 'warehouse_id,item_id', 'quantity');
CREATE TRIGGER trg_reg_warehouse_stock_balances_delete
    AFTER DELETE ON reg_warehouse_stock_movements
    REFERENCING OLD TABLE AS old_rows
    FOR EACH STATEMENT EXECUTE FUNCTION sys_accumulation_balances('reg_warehouse_stock_balances', 'warehouse_id,item_id', 'quantity');

CREATE TABLE reg_stock_movements (
 line_id           UUID        PRIMARY KEY,
 recorder_id       UUID        NOT NULL,
 recorder_type     TEXT        NOT NULL,
 recorder_version  INT         NOT NULL,
 period            TIMESTAMPTZ NOT NULL,
 record_type       TEXT        NOT NULL CHECK (record_type IN ('receipt', 'expense')),
 created_at        TIMESTAMPTZ NOT NULL,
 warehouse_id      UUID        NOT NULL REFERENCES cat_warehouses(id),
 item_id           UUID        NOT NULL REFERENCES cat_items(id),
 customer_order_id UUID        NOT NULL,
 quantity          BIGINT      NOT NULL CHECK (quantity > 0)
);
COMMENT ON TABLE reg_stock_movements IS 'Запасы — движения свободного остатка (нулевой заказ) и резервов под заказы';
CREATE INDEX idx_reg_stock_movements_recorder ON reg_stock_movements (recorder_id, recorder_version);
CREATE INDEX idx_reg_stock_movements_dimensions ON reg_stock_movements (warehouse_id, item_id, customer_order_id, period);
CREATE TRIGGER trg_reg_stock_immutable BEFORE UPDATE ON reg_stock_movements
    FOR EACH ROW EXECUTE FUNCTION sys_accumulation_movements_immutable();

CREATE TABLE reg_stock_balances (
 warehouse_id      UUID        NOT NULL REFERENCES cat_warehouses(id),
 item_id           UUID        NOT NULL REFERENCES cat_items(id),
 customer_order_id UUID        NOT NULL,
 quantity          BIGINT      NOT NULL DEFAULT 0,
 updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY (warehouse_id, item_id, customer_order_id)
);
COMMENT ON TABLE reg_stock_balances IS 'Запасы — свободный остаток и резервы, ведутся триггерами движений';
CREATE TRIGGER trg_reg_stock_balances_insert
    AFTER INSERT ON reg_stock_movements
    REFERENCING NEW TABLE AS new_rows
    FOR EACH STATEMENT EXECUTE FUNCTION sys_accumulation_balances('reg_stock_balances', 'warehouse_id,item_id,customer_order_id', 'quantity');
CREATE TRIGGER trg_reg_stock_balances_delete
    AFTER DELETE ON reg_stock_movements
    REFERENCING OLD TABLE AS old_rows
    FOR EACH STATEMENT EXECUTE FUNCTION sys_accumulation_balances('reg_stock_balances', 'warehouse_id,item_id,customer_order_id', 'quantity');

CREATE TABLE reg_customer_orders_movements (
 line_id           UUID        PRIMARY KEY,
 recorder_id       UUID        NOT NULL,
 recorder_type     TEXT        NOT NULL,
 recorder_version  INT         NOT NULL,
 period            TIMESTAMPTZ NOT NULL,
 record_type       TEXT        NOT NULL CHECK (record_type IN ('receipt', 'expense')),
 created_at        TIMESTAMPTZ NOT NULL,
 customer_order_id UUID        NOT NULL,
 item_id           UUID        NOT NULL REFERENCES cat_items(id),
 quantity          BIGINT      NOT NULL CHECK (quantity > 0)
);
COMMENT ON TABLE reg_customer_orders_movements IS 'Заказы покупателей — заказано (приход) и отгружено (расход)';
CREATE INDEX idx_reg_customer_orders_movements_recorder ON reg_customer_orders_movements (recorder_id, recorder_version);
CREATE INDEX idx_reg_customer_orders_movements_dimensions ON reg_customer_orders_movements (customer_order_id, item_id, period);
CREATE TRIGGER trg_reg_customer_orders_immutable BEFORE UPDATE ON reg_customer_orders_movements
    FOR EACH ROW EXECUTE FUNCTION sys_accumulation_movements_immutable();

CREATE TABLE reg_customer_orders_balances (
 customer_order_id UUID        NOT NULL,
 item_id           UUID        NOT NULL REFERENCES cat_items(id),
 quantity          BIGINT      NOT NULL DEFAULT 0,
 updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY (customer_order_id, item_id)
);
COMMENT ON TABLE reg_customer_orders_balances IS 'Заказы покупателей — остаток к отгрузке, ведётся триггерами движений';
CREATE TRIGGER trg_reg_customer_orders_balances_insert
    AFTER INSERT ON reg_customer_orders_movements
    REFERENCING NEW TABLE AS new_rows
    FOR EACH STATEMENT EXECUTE FUNCTION sys_accumulation_balances('reg_customer_orders_balances', 'customer_order_id,item_id', 'quantity');
CREATE TRIGGER trg_reg_customer_orders_balances_delete
    AFTER DELETE ON reg_customer_orders_movements
    REFERENCING OLD TABLE AS old_rows
    FOR EACH STATEMENT EXECUTE FUNCTION sys_accumulation_balances('reg_customer_orders_balances', 'customer_order_id,item_id', 'quantity');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TABLE reg_customer_orders_balances;
DROP TABLE reg_customer_orders_movements;
DROP TABLE reg_stock_balances;
DROP TABLE reg_stock_movements;
DROP TABLE reg_warehouse_stock_balances;
DROP TABLE reg_warehouse_stock_movements;
DROP TABLE doc_goods_receipts__goods;
DROP TABLE doc_goods_receipts;
DROP TABLE doc_customer_orders__goods;
DROP TABLE doc_customer_orders;
-- +goose StatementEnd
