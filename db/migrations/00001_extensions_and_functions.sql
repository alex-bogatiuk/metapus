-- +goose Up
-- Description: PostgreSQL extensions and shared helper functions.
-- This is the foundation migration — must run first.

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- ── Extensions ─────────────────────────────────────────────────────────────
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS btree_gin;

-- ── UUIDv7 generator ───────────────────────────────────────────────────────
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION gen_random_uuid_v7()
RETURNS uuid AS $func$
DECLARE
    v_time bigint;
    v_rand bytea;
    v_uuid uuid;
BEGIN
    v_time := (EXTRACT(EPOCH FROM clock_timestamp()) * 1000)::bigint;
    v_rand := gen_random_bytes(10);
    v_uuid := (
        lpad(to_hex(v_time >> 16), 8, '0') ||
        lpad(to_hex(v_time & 65535), 4, '0') ||
        '7' || substr(encode(v_rand, 'hex'), 1, 3) ||
        to_hex((get_byte(v_rand, 2) & 63) | 128) ||
        substr(encode(v_rand, 'hex'), 5, 2) ||
        substr(encode(v_rand, 'hex'), 7, 12)
    )::uuid;
    RETURN v_uuid;
END;
$func$ LANGUAGE plpgsql VOLATILE;
-- +goose StatementEnd

-- ── Shared trigger functions ───────────────────────────────────────────────

-- Auto-update updated_at timestamp on any UPDATE
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $func$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$func$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- CDC: update _txid on any change
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_txid_column()
RETURNS TRIGGER AS $func$
BEGIN
    NEW._txid = txid_current();
    RETURN NEW;
END;
$func$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- CDC: set _deleted_at timestamp when deletion_mark toggled
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

-- Safe text → UUID conversion
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

-- UUID format validation
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION is_valid_uuid(p_text TEXT)
RETURNS BOOLEAN AS $func$
BEGIN
    RETURN p_text ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$';
END;
$func$ LANGUAGE plpgsql IMMUTABLE;
-- +goose StatementEnd

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
DROP FUNCTION IF EXISTS is_valid_uuid(TEXT);
DROP FUNCTION IF EXISTS safe_uuid(TEXT);
DROP FUNCTION IF EXISTS soft_delete_with_timestamp();
DROP FUNCTION IF EXISTS update_txid_column();
DROP FUNCTION IF EXISTS update_updated_at_column();
DROP FUNCTION IF EXISTS gen_random_uuid_v7();
DROP EXTENSION IF EXISTS btree_gin;
DROP EXTENSION IF EXISTS pg_trgm;
DROP EXTENSION IF EXISTS pgcrypto;
