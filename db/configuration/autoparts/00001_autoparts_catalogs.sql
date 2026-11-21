-- Auto parts catalogs: items (Номенклатура) and warehouses (Склады).

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
-- +goose StatementEnd

-- Permissions (catalog:item:<action>, catalog:warehouse:<action>) are not
-- declared here: the tenant migration command derives them from the
-- registered entities and grants them to the admin role.

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TABLE cat_warehouses;
DROP TABLE cat_items;
-- +goose StatementEnd
