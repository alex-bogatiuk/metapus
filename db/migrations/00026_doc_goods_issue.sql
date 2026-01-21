-- GoodsIssue document (Расход товаров)
-- Migration: 00025_doc_goods_issue.sql
-- +goose Up
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

CREATE TABLE doc_goods_issues (
    -- Base fields (from entity.Base)
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
    version INTEGER NOT NULL DEFAULT 1,
    attributes JSONB DEFAULT '{}',

    -- Document fields
    number VARCHAR(50) NOT NULL,
    date TIMESTAMPTZ NOT NULL,
    posted BOOLEAN NOT NULL DEFAULT FALSE,
    posted_version INTEGER NOT NULL DEFAULT 0,
    organization_id VARCHAR(64) NOT NULL,
    description TEXT DEFAULT '',

    -- GoodsIssue specific fields
    customer_id UUID NOT NULL REFERENCES cat_counterparties(id),
    warehouse_id UUID NOT NULL REFERENCES cat_warehouses(id),

    -- Customer order reference (optional)
    customer_order_number VARCHAR(100),
    customer_order_date TIMESTAMPTZ,

    -- Currency and totals
    currency VARCHAR(3) NOT NULL DEFAULT 'RUB',
    total_quantity NUMERIC(18, 4) NOT NULL DEFAULT 0,
    total_amount BIGINT NOT NULL DEFAULT 0,
    total_vat BIGINT NOT NULL DEFAULT 0,

    -- Constraints
    CONSTRAINT uq_goods_issue_number UNIQUE (number)
);

-- Lines table
CREATE TABLE doc_goods_issue_lines (
    line_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id UUID NOT NULL REFERENCES doc_goods_issues(id) ON DELETE CASCADE,
    line_no INTEGER NOT NULL,

    -- Product reference
    product_id UUID NOT NULL REFERENCES cat_nomenclature(id),

    -- Quantity and pricing
    quantity NUMERIC(18, 4) NOT NULL,
    unit_price BIGINT NOT NULL,
    vat_rate VARCHAR(5) NOT NULL DEFAULT '20',
    vat_amount BIGINT NOT NULL DEFAULT 0,
    amount BIGINT NOT NULL DEFAULT 0,

    CONSTRAINT uq_goods_issue_line UNIQUE (document_id, line_no)
);

-- Indexes

CREATE INDEX idx_goods_issues_date ON doc_goods_issues(date);
CREATE INDEX idx_goods_issues_customer ON doc_goods_issues(customer_id);
CREATE INDEX idx_goods_issues_warehouse ON doc_goods_issues(warehouse_id);
CREATE INDEX idx_goods_issues_posted ON doc_goods_issues(posted);
CREATE INDEX idx_goods_issue_lines_product ON doc_goods_issue_lines(product_id);

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down