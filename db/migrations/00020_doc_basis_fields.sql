-- +goose Up
-- Description: Add basis_type/basis_id fields to document tables (Document-Basis / Документ-основание)

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- ── doc_goods_receipts ─────────────────────────────────────────────────────
ALTER TABLE doc_goods_receipts
    ADD COLUMN basis_type TEXT    NOT NULL DEFAULT '',
    ADD COLUMN basis_id   UUID;

CREATE INDEX idx_goods_receipts_basis
    ON doc_goods_receipts (basis_type, basis_id)
    WHERE basis_id IS NOT NULL;

COMMENT ON COLUMN doc_goods_receipts.basis_type IS 'Тип документа-основания (e.g. GoodsIssue)';
COMMENT ON COLUMN doc_goods_receipts.basis_id   IS 'ID документа-основания (полиморфная ссылка)';

-- ── doc_goods_issues ───────────────────────────────────────────────────────
ALTER TABLE doc_goods_issues
    ADD COLUMN basis_type TEXT    NOT NULL DEFAULT '',
    ADD COLUMN basis_id   UUID;

CREATE INDEX idx_goods_issues_basis
    ON doc_goods_issues (basis_type, basis_id)
    WHERE basis_id IS NOT NULL;

COMMENT ON COLUMN doc_goods_issues.basis_type IS 'Тип документа-основания (e.g. GoodsReceipt)';
COMMENT ON COLUMN doc_goods_issues.basis_id   IS 'ID документа-основания (полиморфная ссылка)';

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

DROP INDEX IF EXISTS idx_goods_issues_basis;
ALTER TABLE doc_goods_issues DROP COLUMN IF EXISTS basis_id, DROP COLUMN IF EXISTS basis_type;

DROP INDEX IF EXISTS idx_goods_receipts_basis;
ALTER TABLE doc_goods_receipts DROP COLUMN IF EXISTS basis_id, DROP COLUMN IF EXISTS basis_type;

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
