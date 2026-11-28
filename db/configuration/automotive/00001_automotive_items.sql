-- «Автобизнес» fields of the base's items (Номенклатура): the brand of a
-- part and its normalized article, in the add-on's own table one to one
-- with cat_items (registry.Extend). The base's table is not altered: an
-- item's row here goes with it (ON DELETE CASCADE).

-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
CREATE TABLE cat_items__automotive (
 id UUID PRIMARY KEY REFERENCES cat_items(id) ON DELETE CASCADE,
 brand TEXT NOT NULL DEFAULT '',
 normalized_article TEXT NOT NULL DEFAULT ''
);
COMMENT ON TABLE cat_items__automotive IS 'Реквизиты «Автобизнеса» к номенклатуре: бренд и нормализованный артикул';
COMMENT ON COLUMN cat_items__automotive.normalized_article IS 'Артикул в верхнем регистре без «-», «/», «.» и пробелов; заполняется при записи позиции';
-- The list search matches a prefix of the normalized article.
CREATE INDEX idx_cat_items__automotive_normalized_article ON cat_items__automotive (normalized_article text_pattern_ops)
 WHERE normalized_article != '';

-- A tenant of the base that installs the add-on gets the rows of its items,
-- normalized as the add-on's write hook does (item_automotive.NormalizeArticle).
INSERT INTO cat_items__automotive (id, normalized_article)
SELECT id, upper(regexp_replace(article, '[-/.[:space:]]', '', 'g')) FROM cat_items;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TABLE cat_items__automotive;
-- +goose StatementEnd
