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
                                    organization_id UUID NOT NULL REFERENCES cat_organizations(id),
                                    description TEXT,

    -- GoodsReceipt specific fields
                                    supplier_id UUID NOT NULL REFERENCES cat_counterparties(id),
                                    contract_id UUID REFERENCES cat_contracts(id),
                                    warehouse_id UUID NOT NULL REFERENCES cat_warehouses(id),
                                    supplier_doc_number VARCHAR(50),
                                    supplier_doc_date DATE,
                                    incoming_number VARCHAR(50),
                                    currency_id UUID NOT NULL REFERENCES cat_currencies(id),
                                    amount_includes_vat BOOLEAN NOT NULL DEFAULT FALSE,

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
                                         product_id UUID NOT NULL REFERENCES cat_nomenclature(id),

    -- Unit of measurement and conversion
                                         unit_id UUID,
                                         coefficient NUMERIC(15,6) NOT NULL DEFAULT 1,

    -- Quantity and pricing
                                         quantity BIGINT NOT NULL, -- scaled x10000
                                         unit_price BIGINT NOT NULL,

    -- Discount
                                         discount_percent NUMERIC(5,2) NOT NULL DEFAULT 0,
                                         discount_amount BIGINT NOT NULL DEFAULT 0,

    -- VAT (reference to cat_vat_rates)
                                         vat_rate_id UUID NOT NULL REFERENCES cat_vat_rates(id),
                                         vat_amount BIGINT NOT NULL DEFAULT 0,
                                         amount BIGINT NOT NULL,

    -- Constraints
                                         CONSTRAINT chk_quantity_positive CHECK (quantity > 0),
                                         CONSTRAINT chk_unit_price_positive CHECK (unit_price >= 0),
                                         CONSTRAINT chk_coefficient_positive CHECK (coefficient > 0),
                                         CONSTRAINT chk_discount_percent CHECK (discount_percent >= 0 AND discount_percent <= 100),
                                         CONSTRAINT chk_discount_amount CHECK (discount_amount >= 0),

                                         UNIQUE (document_id, line_no)
);

-- Indexes for header
CREATE INDEX idx_goods_receipts_date ON doc_goods_receipts (date DESC);
CREATE INDEX idx_goods_receipts_supplier ON doc_goods_receipts (supplier_id);
CREATE INDEX idx_goods_receipts_contract ON doc_goods_receipts (contract_id) WHERE contract_id IS NOT NULL;
CREATE INDEX idx_goods_receipts_warehouse ON doc_goods_receipts (warehouse_id);
CREATE INDEX idx_doc_goods_receipts_currency_id ON doc_goods_receipts (currency_id);
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
CREATE INDEX idx_goods_receipt_lines_vat_rate ON doc_goods_receipt_lines (vat_rate_id);
CREATE INDEX idx_goods_receipt_lines_unit ON doc_goods_receipt_lines (unit_id) WHERE unit_id IS NOT NULL;


COMMENT ON TABLE doc_goods_receipts IS 'Документ Поступление товаров';
COMMENT ON TABLE doc_goods_receipt_lines IS 'Табличная часть Товары документа Поступление товаров';

COMMENT ON COLUMN doc_goods_receipts.created_at IS 'Дата и время создания документа';
COMMENT ON COLUMN doc_goods_receipts.updated_at IS 'Дата и время последнего изменения документа';
COMMENT ON COLUMN doc_goods_receipts.created_by IS 'Пользователь, создавший документ';
COMMENT ON COLUMN doc_goods_receipts.updated_by IS 'Пользователь, последний изменивший документ';
COMMENT ON COLUMN doc_goods_receipts.currency_id IS 'Reference to currency catalog';
COMMENT ON COLUMN doc_goods_receipts.contract_id IS 'Ссылка на договор контрагента';
COMMENT ON COLUMN doc_goods_receipts.incoming_number IS 'Входящий номер (внутренний регистрационный)';
COMMENT ON COLUMN doc_goods_receipts.amount_includes_vat IS 'Сумма включает НДС (цены брутто)';
COMMENT ON COLUMN doc_goods_receipt_lines.unit_id IS 'Единица измерения (в чем пришел товар)';
COMMENT ON COLUMN doc_goods_receipt_lines.coefficient IS 'Коэффициент пересчета в базовую единицу';
COMMENT ON COLUMN doc_goods_receipt_lines.vat_rate_id IS 'Ссылка на справочник ставок НДС';
COMMENT ON COLUMN doc_goods_receipt_lines.discount_percent IS 'Процент скидки по строке';
COMMENT ON COLUMN doc_goods_receipt_lines.discount_amount IS 'Сумма скидки по строке';

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

DROP TRIGGER IF EXISTS trg_doc_goods_receipts_soft_delete ON doc_goods_receipts;
DROP TRIGGER IF EXISTS trg_doc_goods_receipts_txid ON doc_goods_receipts;
DROP TABLE IF EXISTS doc_goods_receipt_lines;
DROP TABLE IF EXISTS doc_goods_receipts;

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
