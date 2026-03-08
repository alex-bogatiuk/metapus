-- Composite indexes for cursor-based (keyset) pagination.
-- Each index covers the tuple comparison pattern: (sort_field, id) > ($val, $id)
-- with matching sort direction for efficient index-only scans.

-- ── Catalogs: default sort by (name ASC, id ASC) ─────────────────────────

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_cat_currencies_name_id
    ON cat_currencies (name ASC, id ASC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_cat_organizations_name_id
    ON cat_organizations (name ASC, id ASC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_cat_counterparties_name_id
    ON cat_counterparties (name ASC, id ASC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_cat_units_name_id
    ON cat_units (name ASC, id ASC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_cat_warehouses_name_id
    ON cat_warehouses (name ASC, id ASC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_cat_vat_rates_name_id
    ON cat_vat_rates (name ASC, id ASC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_cat_nomenclature_name_id
    ON cat_nomenclature (name ASC, id ASC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_cat_contracts_name_id
    ON cat_contracts (name ASC, id ASC);

-- ── Documents: default sort by (date DESC, id DESC) ──────────────────────

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_doc_goods_receipts_date_id
    ON doc_goods_receipts (date DESC, id DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_doc_goods_issues_date_id
    ON doc_goods_issues (date DESC, id DESC);

-- ── Documents: additional sort by (created_at DESC, id DESC) ─────────────

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_doc_goods_receipts_created_id
    ON doc_goods_receipts (created_at DESC, id DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_doc_goods_issues_created_id
    ON doc_goods_issues (created_at DESC, id DESC);
