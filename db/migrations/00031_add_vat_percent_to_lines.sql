-- +goose Up
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- Store VAT percent on document lines (snapshot at document time, like 1C).
-- This avoids needing to look up the catalog when reading the document.
ALTER TABLE doc_goods_receipt_lines
    ADD COLUMN vat_percent INT NOT NULL DEFAULT 0;

ALTER TABLE doc_goods_issue_lines
    ADD COLUMN vat_percent INT NOT NULL DEFAULT 0;

COMMENT ON COLUMN doc_goods_receipt_lines.vat_percent IS 'Процент НДС (снимок ставки на момент документа)';
COMMENT ON COLUMN doc_goods_issue_lines.vat_percent IS 'Процент НДС (снимок ставки на момент документа)';

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

ALTER TABLE doc_goods_receipt_lines DROP COLUMN IF EXISTS vat_percent;
ALTER TABLE doc_goods_issue_lines DROP COLUMN IF EXISTS vat_percent;

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
