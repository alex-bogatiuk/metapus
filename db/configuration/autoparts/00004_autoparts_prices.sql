-- Item selling prices: the price setting document (Установка цен
-- номенклатуры) and the information register it records (ЦеныНоменклатуры),
-- a price per item from a day on.

-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

CREATE TABLE doc_price_settings (
 id UUID PRIMARY KEY,
 number TEXT NOT NULL DEFAULT '',
 date TIMESTAMPTZ NOT NULL,
 posted BOOLEAN NOT NULL DEFAULT FALSE,
 posted_version INT NOT NULL DEFAULT 0,
 basis_type TEXT NOT NULL DEFAULT '',
 basis_id UUID,
 description TEXT NOT NULL DEFAULT '',
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
COMMENT ON TABLE doc_price_settings IS 'Установка цен номенклатуры: цены продажи с даты документа';
CREATE UNIQUE INDEX idx_doc_price_settings_number ON doc_price_settings (number)
 WHERE _deleted_at IS NULL AND number != '';
CREATE INDEX idx_doc_price_settings_keyset ON doc_price_settings (date,id) WHERE _deleted_at IS NULL;
CREATE INDEX idx_doc_price_settings_txid ON doc_price_settings (_txid) WHERE _deleted_at IS NULL;
CREATE TRIGGER trg_doc_price_settings_txid BEFORE UPDATE ON doc_price_settings FOR EACH ROW EXECUTE FUNCTION update_txid_column();
CREATE TRIGGER trg_doc_price_settings_soft_delete BEFORE UPDATE OF deletion_mark ON doc_price_settings FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();
CREATE TRIGGER trg_doc_price_settings_updated_at BEFORE UPDATE ON doc_price_settings FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE doc_price_settings__prices (
 line_id UUID PRIMARY KEY,
 document_id UUID NOT NULL REFERENCES doc_price_settings(id) ON DELETE CASCADE,
 line_no INT NOT NULL,
 item_id UUID NOT NULL REFERENCES cat_items(id),
 price BIGINT NOT NULL CHECK (price > 0)
);
CREATE INDEX idx_doc_price_settings__prices_document ON doc_price_settings__prices (document_id, line_no);

-- The register is declared in Go (domain/registers/item_prices); the table
-- follows migration.InformationDDL and the tenant migration checks it.
CREATE TABLE reg_item_prices (
 period        TIMESTAMPTZ NOT NULL,
 recorder_id   UUID        NOT NULL,
 recorder_type TEXT        NOT NULL,
 item_id       UUID        NOT NULL REFERENCES cat_items(id),
 price         BIGINT      NOT NULL CHECK (price > 0),
 currency_id   UUID        NOT NULL REFERENCES cat_currencies(id),
 PRIMARY KEY (period, item_id)
);
COMMENT ON TABLE reg_item_prices IS 'Цены номенклатуры: цена продажи с дня, записывает установка цен';
CREATE INDEX idx_reg_item_prices_recorder ON reg_item_prices (recorder_id);
CREATE INDEX idx_reg_item_prices_item ON reg_item_prices (item_id, period);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TABLE reg_item_prices;
DROP TABLE doc_price_settings__prices;
DROP TABLE doc_price_settings;
-- +goose StatementEnd
