-- Auto parts catalogs: items (Номенклатура), warehouses (Склады) and
-- customer order states (Состояния заказов покупателей) with the states
-- every store starts with.

-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
CREATE TABLE cat_items (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
 code TEXT NOT NULL DEFAULT '',
 name TEXT NOT NULL,
 parent_id UUID REFERENCES cat_items(id),
 is_folder BOOLEAN NOT NULL DEFAULT FALSE,
 article TEXT NOT NULL DEFAULT '',
 manufacturer TEXT NOT NULL DEFAULT '',
 unit TEXT NOT NULL DEFAULT '',
 deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
 version INT NOT NULL DEFAULT 1,
 attributes JSONB NOT NULL DEFAULT '{}',
 _deleted_at TIMESTAMPTZ,
 _txid BIGINT NOT NULL DEFAULT txid_current(),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE cat_items IS 'Номенклатура: запчасти, жидкости и аксессуары по артикулу производителя';
CREATE UNIQUE INDEX idx_cat_items_code ON cat_items (code)
 WHERE _deleted_at IS NULL AND code != '';
CREATE INDEX idx_cat_items_keyset ON cat_items (name,id) WHERE _deleted_at IS NULL;
CREATE INDEX idx_cat_items_article ON cat_items (article) WHERE _deleted_at IS NULL AND article != '';
CREATE INDEX idx_cat_items_txid ON cat_items (_txid) WHERE _deleted_at IS NULL;
CREATE TRIGGER trg_cat_items_txid BEFORE UPDATE ON cat_items FOR EACH ROW EXECUTE FUNCTION update_txid_column();
CREATE TRIGGER trg_cat_items_soft_delete BEFORE UPDATE OF deletion_mark ON cat_items FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();
CREATE TRIGGER trg_cat_items_updated_at BEFORE UPDATE ON cat_items FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE cat_warehouses (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
 code TEXT NOT NULL DEFAULT '',
 name TEXT NOT NULL,
 parent_id UUID REFERENCES cat_warehouses(id),
 is_folder BOOLEAN NOT NULL DEFAULT FALSE,
 address TEXT NOT NULL DEFAULT '',
 deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
 version INT NOT NULL DEFAULT 1,
 attributes JSONB NOT NULL DEFAULT '{}',
 _deleted_at TIMESTAMPTZ,
 _txid BIGINT NOT NULL DEFAULT txid_current(),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE cat_warehouses IS 'Склады: торговый зал, основной склад, склад брака, зона сервиса';
CREATE UNIQUE INDEX idx_cat_warehouses_code ON cat_warehouses (code)
 WHERE _deleted_at IS NULL AND code != '';
CREATE INDEX idx_cat_warehouses_keyset ON cat_warehouses (name,id) WHERE _deleted_at IS NULL;
CREATE INDEX idx_cat_warehouses_txid ON cat_warehouses (_txid) WHERE _deleted_at IS NULL;
CREATE TRIGGER trg_cat_warehouses_txid BEFORE UPDATE ON cat_warehouses FOR EACH ROW EXECUTE FUNCTION update_txid_column();
CREATE TRIGGER trg_cat_warehouses_soft_delete BEFORE UPDATE OF deletion_mark ON cat_warehouses FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();
CREATE TRIGGER trg_cat_warehouses_updated_at BEFORE UPDATE ON cat_warehouses FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE cat_customer_order_states (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
 code TEXT NOT NULL DEFAULT '',
 name TEXT NOT NULL,
 parent_id UUID REFERENCES cat_customer_order_states(id),
 is_folder BOOLEAN NOT NULL DEFAULT FALSE,
 status TEXT NOT NULL CHECK (status IN ('open', 'in_progress', 'completed', 'cancelled')),
 deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
 version INT NOT NULL DEFAULT 1,
 attributes JSONB NOT NULL DEFAULT '{}',
 _deleted_at TIMESTAMPTZ,
 _txid BIGINT NOT NULL DEFAULT txid_current(),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE cat_customer_order_states IS 'Состояния заказов покупателей: названия магазина, каждое со статусом open / in_progress / completed / cancelled';
CREATE UNIQUE INDEX idx_cat_customer_order_states_code ON cat_customer_order_states (code)
 WHERE _deleted_at IS NULL AND code != '';
CREATE INDEX idx_cat_customer_order_states_keyset ON cat_customer_order_states (name,id) WHERE _deleted_at IS NULL;
CREATE INDEX idx_cat_customer_order_states_txid ON cat_customer_order_states (_txid) WHERE _deleted_at IS NULL;
CREATE TRIGGER trg_cat_customer_order_states_txid BEFORE UPDATE ON cat_customer_order_states FOR EACH ROW EXECUTE FUNCTION update_txid_column();
CREATE TRIGGER trg_cat_customer_order_states_soft_delete BEFORE UPDATE OF deletion_mark ON cat_customer_order_states FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();
CREATE TRIGGER trg_cat_customer_order_states_updated_at BEFORE UPDATE ON cat_customer_order_states FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
-- The states order_state names in Go (order_state.New, …).
INSERT INTO cat_customer_order_states (id, code, name, status) VALUES
 ('01960000-0000-7000-8000-000000000001', 'NEW', 'Новый', 'open'),
 ('01960000-0000-7000-8000-000000000002', 'PREPAY', 'Ожидает предоплаты', 'open'),
 ('01960000-0000-7000-8000-000000000003', 'WORK', 'В работе', 'in_progress'),
 ('01960000-0000-7000-8000-000000000004', 'READY', 'Готов к выдаче', 'in_progress'),
 ('01960000-0000-7000-8000-000000000005', 'DONE', 'Выполнен', 'completed'),
 ('01960000-0000-7000-8000-000000000006', 'CANCEL', 'Отменён', 'cancelled');
-- +goose StatementEnd

-- Permissions (catalog:item:<action>, catalog:warehouse:<action>) are not
-- declared here: the tenant migration command derives them from the
-- registered entities and grants them to the admin role.

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TABLE cat_customer_order_states;
DROP TABLE cat_warehouses;
DROP TABLE cat_items;
-- +goose StatementEnd
