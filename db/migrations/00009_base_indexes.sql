-- +goose Up
-- Description: Common helper functions and indexes

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- Безопасное преобразование текста в UUID
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION safe_uuid(p_text TEXT)
RETURNS UUID AS $func$
BEGIN
    RETURN p_text::uuid;
EXCEPTION WHEN invalid_text_representation THEN
    RETURN NULL;
END;
$func$ LANGUAGE plpgsql IMMUTABLE;
-- +goose StatementEnd

-- Проверка, является ли строка валидным UUID
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION is_valid_uuid(p_text TEXT)
RETURNS BOOLEAN AS $func$
BEGIN
    RETURN p_text ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$';
END;
$func$ LANGUAGE plpgsql IMMUTABLE;
-- +goose StatementEnd

-- CDC helper: update _txid on any change
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_txid_column()
RETURNS TRIGGER AS $func$
BEGIN
    NEW._txid = txid_current();
    RETURN NEW;
END;
$func$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- CDC helper: set _deleted_at timestamp based on deletion_mark toggling
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION soft_delete_with_timestamp()
RETURNS TRIGGER AS $func$
BEGIN
    IF NEW.deletion_mark = TRUE AND OLD.deletion_mark = FALSE THEN
        NEW._deleted_at = NOW();
    ELSIF NEW.deletion_mark = FALSE AND OLD.deletion_mark = TRUE THEN
        NEW._deleted_at = NULL;
    END IF;
    RETURN NEW;
END;
$func$ LANGUAGE plpgsql;
-- +goose StatementEnd

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
DROP FUNCTION IF EXISTS soft_delete_with_timestamp();
DROP FUNCTION IF EXISTS update_txid_column();
DROP FUNCTION IF EXISTS is_valid_uuid(TEXT);
DROP FUNCTION IF EXISTS safe_uuid(TEXT);