-- +goose Up
-- Description: Initialize PostgreSQL extensions required by Metapus
-- Advisory Lock: Prevents concurrent migration execution in K8s

-- Acquire advisory lock to prevent concurrent migrations
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- UUID v7 support (PostgreSQL 16+ has built-in gen_random_uuid_v7)
-- For older versions, we use pgcrypto and custom function
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Full-text search for Russian and English
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Better JSON support
CREATE EXTENSION IF NOT EXISTS btree_gin;

-- Create UUIDv7 function for older PostgreSQL versions
-- UUIDv7 is time-ordered: first 48 bits = Unix timestamp (ms)

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION gen_random_uuid_v7()
RETURNS uuid AS $func$
DECLARE
    v_time bigint;
v_rand bytea;
v_uuid uuid;
BEGIN
    -- Get current timestamp in milliseconds
    v_time := (EXTRACT(EPOCH FROM clock_timestamp()) * 1000)::bigint;

-- Get 10 random bytes
v_rand := gen_random_bytes(10);

    -- Construct UUIDv7
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

-- Release advisory lock
SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
DROP FUNCTION IF EXISTS gen_random_uuid_v7();
DROP EXTENSION IF EXISTS btree_gin;
DROP EXTENSION IF EXISTS pg_trgm;
DROP EXTENSION IF EXISTS pgcrypto;