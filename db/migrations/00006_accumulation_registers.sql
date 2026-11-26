-- Declarative accumulation registers (internal/domain/registers/accumulation).
-- A configuration declares a register by its movement type and creates its
-- tables in its own migration; the triggers of those tables call the shared
-- functions below with the register's tables and columns as arguments, so a
-- register needs no plpgsql of its own. The tenant migration checks the
-- tables against the declaration (migration.CheckAccumulationRegisters).

-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- sys_accumulation_balances keeps a register's balances in step with its
-- movements. It is the statement trigger AFTER INSERT (REFERENCING NEW TABLE
-- AS new_rows) and AFTER DELETE (REFERENCING OLD TABLE AS old_rows) of the
-- movements table, with three arguments: the balances table, its dimension
-- columns and its resource columns, comma-separated. A receipt adds its
-- resources to the balance of its dimensions, an expense takes them away,
-- and deleting a movement undoes it.
CREATE FUNCTION sys_accumulation_balances()
RETURNS TRIGGER AS $func$
DECLARE
    balances   TEXT   := TG_ARGV[0];
    dimensions TEXT[] := string_to_array(TG_ARGV[1], ',');
    resources  TEXT[] := string_to_array(TG_ARGV[2], ',');
    source     TEXT   := CASE TG_OP WHEN 'INSERT' THEN 'new_rows' ELSE 'old_rows' END;
    sign       TEXT   := CASE TG_OP WHEN 'INSERT' THEN '' ELSE '-' END;
    dimension_list TEXT;
    resource_list  TEXT;
    sums           TEXT;
    additions      TEXT;
BEGIN
    SELECT string_agg(format('%I', d), ', ') INTO dimension_list FROM unnest(dimensions) AS d;
    SELECT string_agg(format('%I', r), ', ') INTO resource_list FROM unnest(resources) AS r;
    SELECT string_agg(format('%sSUM(CASE WHEN record_type = %L THEN %I ELSE -%I END)', sign, 'receipt', r, r), ', ')
      INTO sums FROM unnest(resources) AS r;
    SELECT string_agg(format('%1$I = b.%1$I + EXCLUDED.%1$I', r), ', ') INTO additions FROM unnest(resources) AS r;
    EXECUTE format(
        'INSERT INTO %I AS b (%s, %s, updated_at) SELECT %s, %s, NOW() FROM %I GROUP BY %s '
        'ON CONFLICT (%s) DO UPDATE SET %s, updated_at = NOW()',
        balances, dimension_list, resource_list, dimension_list, sums, source, dimension_list,
        dimension_list, additions);
    RETURN NULL;
END;
$func$ LANGUAGE plpgsql;

-- sys_accumulation_movements_immutable refuses to change a movement in
-- place: a posting replaces a document's movements by deleting and inserting
-- them, which keeps the balances right.
CREATE FUNCTION sys_accumulation_movements_immutable()
RETURNS TRIGGER AS $func$
BEGIN
    RAISE EXCEPTION 'movements of %.% are not updated: repost the document', TG_TABLE_SCHEMA, TG_TABLE_NAME
        USING ERRCODE = 'restrict_violation';
END;
$func$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP FUNCTION sys_accumulation_movements_immutable();
DROP FUNCTION sys_accumulation_balances();
-- +goose StatementEnd
