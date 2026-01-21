-- +goose Up
-- Description: System sequences table for document auto-numbering

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

CREATE TABLE sys_sequences (
                               key VARCHAR(100) NOT NULL,

                               current_val BIGINT NOT NULL DEFAULT 0,
                               created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
                               updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

                               PRIMARY KEY (key)
);



-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $func$
BEGIN
    NEW.updated_at = NOW();
RETURN NEW;
END;
$func$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_sys_sequences_updated_at
    BEFORE UPDATE ON sys_sequences
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

COMMENT ON TABLE sys_sequences IS 'Auto-numbering sequences for documents (INV-2024-00001)';
COMMENT ON COLUMN sys_sequences.key IS 'Sequence key: {prefix}_{period}, e.g., INVOICE_2024';

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
DROP TRIGGER IF EXISTS trg_sys_sequences_updated_at ON sys_sequences;
DROP FUNCTION IF EXISTS update_updated_at_column();
DROP TABLE IF EXISTS sys_sequences;