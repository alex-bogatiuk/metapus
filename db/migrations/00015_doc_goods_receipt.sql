-- +goose Up
-- Description: GoodsReceipt document (Поступление товаров)

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- Goods Receipt header
CREATE TABLE doc_goods_receipts (
    -- Base document fields
                                    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),

                                    deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
                                    version INT NOT NULL DEFAULT 1,
                                    attributes JSONB DEFAULT '{}',

    -- CDC-ready columns
                                    _deleted_at TIMESTAMPTZ,
                                    _txid BIGINT DEFAULT txid_current(),

    -- Audit fields
                                    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
                                    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
                                    created_by UUID NOT NULL,
                                    updated_by UUID NOT NULL,

    -- Document fields
                                    number VARCHAR(20) NOT NULL,
                                    date TIMESTAMPTZ NOT NULL,
                                    posted BOOLEAN NOT NULL DEFAULT FALSE,
                                    posted_version INT NOT NULL DEFAULT 0,
                                    organization_id UUID NOT NULL,
                                    description TEXT,

    -- GoodsReceipt specific fields
                                    supplier_id UUID NOT NULL,
                                    warehouse_id UUID NOT NULL,
                                    supplier_doc_number VARCHAR(50),
                                    supplier_doc_date DATE,
                                    currency CHAR(3) NOT NULL DEFAULT 'RUB',

    -- Totals (denormalized for performance)
                                    total_quantity BIGINT NOT NULL DEFAULT 0, -- scaled x10000
                                    total_amount BIGINT NOT NULL DEFAULT 0,
                                    total_vat BIGINT NOT NULL DEFAULT 0,

    -- Constraints
                                    CONSTRAINT uq_goods_receipt_number UNIQUE (organization_id, number),

    -- Foreign keys for audit
                                    CONSTRAINT fk_goods_receipts_created_by FOREIGN KEY (created_by) REFERENCES users(id),
                                    CONSTRAINT fk_goods_receipts_updated_by FOREIGN KEY (updated_by) REFERENCES users(id)
);

-- Goods Receipt lines (table part)
CREATE TABLE doc_goods_receipt_lines (
    -- Line identification
                                         line_id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
                                         document_id UUID NOT NULL REFERENCES doc_goods_receipts(id) ON DELETE CASCADE,
                                         line_no INT NOT NULL,

    -- Product
                                         product_id UUID NOT NULL,

    -- Quantity and pricing
                                         quantity BIGINT NOT NULL, -- scaled x10000
                                         unit_price BIGINT NOT NULL,
                                         vat_rate VARCHAR(5) NOT NULL DEFAULT '20',
                                         vat_amount BIGINT NOT NULL DEFAULT 0,
                                         amount BIGINT NOT NULL,

    -- Constraints
                                         CONSTRAINT chk_quantity_positive CHECK (quantity > 0),
                                         CONSTRAINT chk_unit_price_positive CHECK (unit_price >= 0),
                                         CONSTRAINT chk_vat_rate CHECK (vat_rate IN ('0', '10', '20')),

                                         UNIQUE (document_id, line_no)
);

-- Indexes for header
CREATE INDEX idx_goods_receipts_date ON doc_goods_receipts (date DESC);
CREATE INDEX idx_goods_receipts_supplier ON doc_goods_receipts (supplier_id);
CREATE INDEX idx_goods_receipts_warehouse ON doc_goods_receipts (warehouse_id);
CREATE INDEX idx_goods_receipts_posted ON doc_goods_receipts (posted) WHERE posted = FALSE;

-- Дополнительные индексы для полей аудита (полезны при фильтрации по пользователю)
CREATE INDEX idx_goods_receipts_created_by ON doc_goods_receipts (created_by);
CREATE INDEX idx_goods_receipts_updated_by ON doc_goods_receipts (updated_by);
CREATE INDEX idx_goods_receipts_created_at ON doc_goods_receipts (created_at DESC);

-- CDC index & triggers
CREATE INDEX IF NOT EXISTS idx_doc_goods_receipts_txid
    ON doc_goods_receipts (_txid) WHERE _deleted_at IS NULL;

CREATE TRIGGER trg_doc_goods_receipts_txid
    BEFORE UPDATE ON doc_goods_receipts
    FOR EACH ROW
    EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_doc_goods_receipts_soft_delete
    BEFORE UPDATE OF deletion_mark ON doc_goods_receipts
    FOR EACH ROW
    EXECUTE FUNCTION soft_delete_with_timestamp();

-- Indexes for lines
CREATE INDEX idx_goods_receipt_lines_doc ON doc_goods_receipt_lines (document_id);
CREATE INDEX idx_goods_receipt_lines_product ON doc_goods_receipt_lines (product_id);


COMMENT ON TABLE doc_goods_receipts IS 'Документ Поступление товаров';
COMMENT ON TABLE doc_goods_receipt_lines IS 'Табличная часть Товары документа Поступление товаров';

COMMENT ON COLUMN doc_goods_receipts.created_at IS 'Дата и время создания документа';
COMMENT ON COLUMN doc_goods_receipts.updated_at IS 'Дата и время последнего изменения документа';
COMMENT ON COLUMN doc_goods_receipts.created_by IS 'Пользователь, создавший документ';
COMMENT ON COLUMN doc_goods_receipts.updated_by IS 'Пользователь, последний изменивший документ';

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

DROP TRIGGER IF EXISTS trg_doc_goods_receipts_soft_delete ON doc_goods_receipts;
DROP TRIGGER IF EXISTS trg_doc_goods_receipts_txid ON doc_goods_receipts;
DROP TABLE IF EXISTS doc_goods_receipt_lines;
DROP TABLE IF EXISTS doc_goods_receipts;

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));