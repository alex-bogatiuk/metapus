-- Goods issues (Расходная накладная): parts sold to a customer from a
-- warehouse. Posting takes them from the warehouse stock register, which
-- refuses to go below zero.

-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

CREATE TABLE doc_goods_issues (
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
COMMENT ON TABLE doc_goods_issues IS 'Расходная накладная: продажа запчастей покупателю со склада';
CREATE UNIQUE INDEX idx_doc_goods_issues_number ON doc_goods_issues (number)
 WHERE _deleted_at IS NULL AND number != '';
CREATE INDEX idx_doc_goods_issues_keyset ON doc_goods_issues (date,id) WHERE _deleted_at IS NULL;
CREATE INDEX idx_doc_goods_issues_txid ON doc_goods_issues (_txid) WHERE _deleted_at IS NULL;
CREATE TRIGGER trg_doc_goods_issues_txid BEFORE UPDATE ON doc_goods_issues FOR EACH ROW EXECUTE FUNCTION update_txid_column();
CREATE TRIGGER trg_doc_goods_issues_soft_delete BEFORE UPDATE OF deletion_mark ON doc_goods_issues FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();
CREATE TRIGGER trg_doc_goods_issues_updated_at BEFORE UPDATE ON doc_goods_issues FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Table part goods: one row per line, replaced with the document.
CREATE TABLE doc_goods_issues__goods (
 line_id UUID PRIMARY KEY,
 document_id UUID NOT NULL REFERENCES doc_goods_issues(id) ON DELETE CASCADE,
 line_no INT NOT NULL,
 item_id UUID NOT NULL REFERENCES cat_items(id),
 quantity BIGINT NOT NULL CHECK (quantity > 0),
 price BIGINT NOT NULL DEFAULT 0 CHECK (price >= 0),
 amount BIGINT NOT NULL DEFAULT 0 CHECK (amount >= 0)
);
CREATE INDEX idx_doc_goods_issues__goods_document ON doc_goods_issues__goods (document_id, line_no);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TABLE doc_goods_issues__goods;
DROP TABLE doc_goods_issues;
-- +goose StatementEnd
