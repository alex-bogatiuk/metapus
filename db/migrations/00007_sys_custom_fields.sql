-- +goose Up
-- Description: Custom field schemas for JSONB validation

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

CREATE TYPE custom_field_type AS ENUM (
    'string', 'text', 'integer', 'decimal', 'boolean',
    'date', 'datetime', 'reference', 'enum', 'json'
);

CREATE TABLE sys_custom_field_schemas (
                                          id            UUID        PRIMARY KEY DEFAULT gen_random_uuid_v7(),
                                          entity_type   VARCHAR(50) NOT NULL,
                                          field_name    VARCHAR(50) NOT NULL,
                                          field_type    custom_field_type NOT NULL,
                                          display_name  VARCHAR(100) NOT NULL,
                                          description   TEXT,
                                          is_required   BOOLEAN     NOT NULL DEFAULT FALSE,
                                          is_indexed    BOOLEAN     NOT NULL DEFAULT FALSE,
                                          default_value JSONB,
                                          validation_rules JSONB,
                                          reference_type VARCHAR(50),
                                          enum_values   TEXT[],
                                          sort_order    INT         NOT NULL DEFAULT 0,
                                          is_active     BOOLEAN     NOT NULL DEFAULT TRUE,
                                          created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
                                          updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),


                                          CONSTRAINT uq_custom_field UNIQUE (entity_type, field_name)
);

CREATE INDEX idx_custom_fields_entity
    ON sys_custom_field_schemas (entity_type)
    WHERE is_active = TRUE;

CREATE TRIGGER trg_sys_custom_fields_updated_at
    BEFORE UPDATE ON sys_custom_field_schemas
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION notify_schema_change()
RETURNS TRIGGER AS $func$
BEGIN
    PERFORM pg_notify('schema_changed',
        COALESCE(NEW.entity_type, OLD.entity_type));
RETURN COALESCE(NEW, OLD);
END;
$func$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_custom_fields_notify
    AFTER INSERT OR UPDATE OR DELETE ON sys_custom_field_schemas
    FOR EACH ROW
EXECUTE FUNCTION notify_schema_change();

COMMENT ON TABLE sys_custom_field_schemas IS 'Определения пользовательских полей для JSONB attributes';

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
DROP TRIGGER IF EXISTS trg_custom_fields_notify ON sys_custom_field_schemas;
DROP FUNCTION IF EXISTS notify_schema_change();
DROP TRIGGER IF EXISTS trg_sys_custom_fields_updated_at ON sys_custom_field_schemas;
DROP TABLE IF EXISTS sys_custom_field_schemas;
DROP TYPE IF EXISTS custom_field_type;