-- +goose Up
-- Inventory document (Инвентаризация)
-- Migration: 00026_doc_inventory.sql

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- Status enum for inventory
CREATE TYPE inventory_status AS ENUM ('draft', 'in_progress', 'completed', 'cancelled');

CREATE TABLE doc_inventories (
                                 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

                                 deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
                                 version INTEGER NOT NULL DEFAULT 1,
                                 attributes JSONB DEFAULT '{}',

                                 number VARCHAR(50) NOT NULL,
                                 date TIMESTAMPTZ NOT NULL,
                                 posted BOOLEAN NOT NULL DEFAULT FALSE,
                                 posted_version INTEGER NOT NULL DEFAULT 0,
                                 organization_id VARCHAR(64) NOT NULL,
                                 description TEXT DEFAULT '',

                                 warehouse_id UUID NOT NULL REFERENCES cat_warehouses(id),
                                 status inventory_status NOT NULL DEFAULT 'draft',

                                 start_date TIMESTAMPTZ NOT NULL,
                                 end_date TIMESTAMPTZ,

                                 responsible_id UUID REFERENCES users(id),

                                 total_book_quantity BIGINT NOT NULL DEFAULT 0, -- scaled x10000
                                 total_actual_quantity BIGINT NOT NULL DEFAULT 0,
                                 total_surplus_quantity BIGINT NOT NULL DEFAULT 0,
                                 total_shortage_quantity BIGINT NOT NULL DEFAULT 0,

                                 CONSTRAINT uq_inventory_number UNIQUE (number)
);

CREATE TABLE doc_inventory_lines (
                                     line_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                     document_id UUID NOT NULL REFERENCES doc_inventories(id) ON DELETE CASCADE,
                                     line_no INTEGER NOT NULL,

                                     product_id UUID NOT NULL REFERENCES cat_nomenclature(id),

                                     book_quantity BIGINT NOT NULL DEFAULT 0, -- scaled x10000
                                     actual_quantity BIGINT,
                                     deviation BIGINT GENERATED ALWAYS AS (COALESCE(actual_quantity, 0) - book_quantity) STORED,

                                     unit_price BIGINT NOT NULL DEFAULT 0,
                                     deviation_amount BIGINT GENERATED ALWAYS AS (
                                         ((COALESCE(actual_quantity, 0) - book_quantity) * unit_price) / 10000
) STORED,

    counted BOOLEAN NOT NULL DEFAULT FALSE,
    counted_at TIMESTAMPTZ,
    counted_by VARCHAR(64),

    CONSTRAINT uq_inventory_line UNIQUE (document_id, line_no)
);

-- Indexes

CREATE INDEX idx_inventories_date ON doc_inventories(date);
CREATE INDEX idx_inventories_warehouse ON doc_inventories(warehouse_id);
CREATE INDEX idx_inventories_status ON doc_inventories(status);
CREATE INDEX idx_inventory_lines_product ON doc_inventory_lines(product_id);
CREATE INDEX idx_inventory_lines_counted ON doc_inventory_lines(counted);

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
DROP INDEX IF EXISTS idx_inventory_lines_counted;
DROP INDEX IF EXISTS idx_inventory_lines_product;
DROP INDEX IF EXISTS idx_inventories_status;
DROP INDEX IF EXISTS idx_inventories_warehouse;
DROP INDEX IF EXISTS idx_inventories_date;
DROP INDEX IF EXISTS idx_inventories_tenant;

DROP TABLE IF EXISTS doc_inventory_lines;
DROP TABLE IF EXISTS doc_inventories;
DROP TYPE IF EXISTS inventory_status;