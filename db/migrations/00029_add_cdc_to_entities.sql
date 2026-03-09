-- +goose Up
-- Description: Add Change Data Capture (CDC) fields and triggers to all entities
-- This migration ensures that all catalog and document tables have _txid and _deleted_at columns,
-- and that they are automatically updated via triggers.

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- 1. Create universal trigger function for CDC fields
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION fn_update_cdc_fields()
RETURNS TRIGGER AS $$
BEGIN
    -- Update transaction ID
    NEW._txid = txid_current();
    
    -- Update soft deletion timestamp if deletion_mark toggled
    -- Check if deletion_mark column exists in the table
    IF EXISTS (
        SELECT 1 FROM information_schema.columns 
        WHERE table_name = TG_TABLE_NAME 
        AND column_name = 'deletion_mark'
    ) THEN
        IF (TG_OP = 'UPDATE') THEN
            IF (NEW.deletion_mark = TRUE AND OLD.deletion_mark = FALSE) THEN
                NEW._deleted_at = NOW();
            ELSIF (NEW.deletion_mark = FALSE AND OLD.deletion_mark = TRUE) THEN
                NEW._deleted_at = NULL;
            END IF;
        END IF;
    END IF;
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- 2. Helper procedure to add CDC to a table
-- +goose StatementBegin
CREATE OR REPLACE PROCEDURE pr_add_cdc_to_table(p_table_name TEXT) 
AS $$
BEGIN
    -- Add columns if they don't exist
    EXECUTE format('ALTER TABLE %I ADD COLUMN IF NOT EXISTS _txid BIGINT DEFAULT 0', p_table_name);
    EXECUTE format('ALTER TABLE %I ADD COLUMN IF NOT EXISTS _deleted_at TIMESTAMP WITH TIME ZONE', p_table_name);

    -- Create index for performance
    EXECUTE format('CREATE INDEX IF NOT EXISTS %I ON %I (_txid) WHERE _deleted_at IS NULL', 'idx_' || p_table_name || '_txid', p_table_name);

    -- Create trigger
    EXECUTE format('DROP TRIGGER IF EXISTS trg_%I_cdc ON %I', p_table_name, p_table_name);
    EXECUTE format('CREATE TRIGGER trg_%I_cdc BEFORE INSERT OR UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION fn_update_cdc_fields()', p_table_name, p_table_name);
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- 3. Apply to all catalog and document tables
-- +goose StatementBegin
DO $$
DECLARE
    t TEXT;
BEGIN
    FOR t IN 
        SELECT table_name 
        FROM information_schema.tables 
        WHERE table_schema = 'public' 
        AND (table_name LIKE 'cat_%' OR table_name LIKE 'doc_%')
    LOOP
        CALL pr_add_cdc_to_table(t);
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- Clean up helper procedure (optional, but keeps schema clean)
DROP PROCEDURE pr_add_cdc_to_table(TEXT);

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- Note: We generally don't drop columns in Down to avoid data loss, 
-- but we should remove the triggers and function.

-- +goose StatementBegin
DO $$
DECLARE
    t TEXT;
BEGIN
    FOR t IN 
        SELECT table_name 
        FROM information_schema.tables 
        WHERE table_schema = 'public' 
        AND (table_name LIKE 'cat_%' OR table_name LIKE 'doc_%')
    LOOP
        EXECUTE format('DROP TRIGGER IF EXISTS trg_%I_cdc ON %I', t, t);
    END LOOP;
END;
$$;
-- +goose StatementEnd

DROP FUNCTION IF EXISTS fn_update_cdc_fields();

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
