-- +goose Up
-- A token's network, contract, standard and scale become immutable at first use.
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

ALTER TABLE cat_tokens
    ADD COLUMN financial_identity_locked BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE cat_tokens AS t
SET financial_identity_locked = TRUE
WHERE t.id IN (
    SELECT token_id FROM doc_crypto_invoices
    UNION SELECT token_id FROM doc_crypto_payments
    UNION SELECT token_id FROM doc_withdrawal_requests
    UNION SELECT token_id FROM doc_crypto_withdrawals
    UNION SELECT token_id FROM doc_crypto_sweeps
    UNION SELECT token_id FROM doc_treasury_withdrawals
    UNION SELECT token_id FROM doc_crypto_provider_balance_operations
    UNION SELECT token_id FROM reg_crypto_balance_movements
    UNION SELECT token_id FROM reg_crypto_merchant_balance_movements
    UNION SELECT token_id FROM reg_crypto_fee_movements
    UNION SELECT token_id FROM reg_crypto_provider_balance_movements
    UNION SELECT cost_token_id FROM reg_crypto_platform_cost_movements
    UNION SELECT operation_token_id FROM reg_crypto_platform_cost_movements
);

CREATE FUNCTION fn_crypto_token_identity_guard()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.financial_identity_locked AND NOT NEW.financial_identity_locked THEN
        RAISE EXCEPTION 'token financial identity lock cannot be cleared'
            USING ERRCODE = '23514';
    END IF;
    IF OLD.financial_identity_locked AND (
        NEW.network_id IS DISTINCT FROM OLD.network_id OR
        NEW.contract_address IS DISTINCT FROM OLD.contract_address OR
        NEW.token_standard IS DISTINCT FROM OLD.token_standard OR
        NEW.decimal_places IS DISTINCT FROM OLD.decimal_places
    ) THEN
        RAISE EXCEPTION 'token identity cannot change after financial use'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_crypto_token_identity_guard
    BEFORE UPDATE ON cat_tokens
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_token_identity_guard();

-- Marking the row happens before the FK check and serializes with an identity edit.
CREATE FUNCTION fn_crypto_mark_token_identity_used()
RETURNS TRIGGER AS $$
DECLARE
    field_name TEXT;
    token_key UUID;
BEGIN
    FOREACH field_name IN ARRAY TG_ARGV LOOP
        token_key := NULLIF(to_jsonb(NEW)->>field_name, '')::UUID;
        IF token_key IS NOT NULL THEN
            UPDATE cat_tokens
               SET financial_identity_locked = TRUE
             WHERE id = token_key AND financial_identity_locked = FALSE;
        END IF;
    END LOOP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DO $$
DECLARE
    entry RECORD;
BEGIN
    FOR entry IN
        SELECT * FROM (VALUES
            ('doc_crypto_invoices', 'token_id'),
            ('doc_crypto_payments', 'token_id'),
            ('doc_withdrawal_requests', 'token_id'),
            ('doc_crypto_withdrawals', 'token_id'),
            ('doc_crypto_sweeps', 'token_id'),
            ('doc_treasury_withdrawals', 'token_id'),
            ('doc_crypto_provider_balance_operations', 'token_id'),
            ('reg_crypto_balance_movements', 'token_id'),
            ('reg_crypto_merchant_balance_movements', 'token_id'),
            ('reg_crypto_fee_movements', 'token_id'),
            ('reg_crypto_provider_balance_movements', 'token_id'),
            ('reg_crypto_platform_cost_movements', 'operation_token_id,cost_token_id')
        ) AS sources(table_name, token_columns)
    LOOP
        EXECUTE format(
            'CREATE TRIGGER %I BEFORE INSERT OR UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION fn_crypto_mark_token_identity_used(%s)',
            'trg_token_identity_' || entry.table_name,
            entry.table_name,
            (SELECT string_agg(quote_literal(part), ',')
               FROM unnest(string_to_array(entry.token_columns, ',')) AS part)
        );
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$
DECLARE
    table_name TEXT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'doc_crypto_invoices', 'doc_crypto_payments',
        'doc_withdrawal_requests', 'doc_crypto_withdrawals',
        'doc_crypto_sweeps', 'doc_treasury_withdrawals',
        'doc_crypto_provider_balance_operations',
        'reg_crypto_balance_movements', 'reg_crypto_merchant_balance_movements',
        'reg_crypto_fee_movements', 'reg_crypto_provider_balance_movements',
        'reg_crypto_platform_cost_movements'
    ] LOOP
        EXECUTE format('DROP TRIGGER %I ON %I',
            'trg_token_identity_' || table_name, table_name);
    END LOOP;
END;
$$;
DROP FUNCTION fn_crypto_mark_token_identity_used();
DROP TRIGGER trg_crypto_token_identity_guard ON cat_tokens;
DROP FUNCTION fn_crypto_token_identity_guard();
ALTER TABLE cat_tokens DROP COLUMN financial_identity_locked;
-- +goose StatementEnd
