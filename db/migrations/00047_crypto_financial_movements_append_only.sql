-- +goose Up
-- Crypto financial movements are facts. Corrections append linked opposite facts.
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- Refuse to freeze a ledger whose live projections already diverge. Resolve
-- such discrepancies with an audited incident before enabling this guard.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM (
            SELECT wallet_id,token_id,
                   SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END) AS amount
            FROM reg_crypto_balance_movements GROUP BY wallet_id,token_id
        ) movement FULL JOIN reg_crypto_balance_balances balance
          USING (wallet_id,token_id)
        WHERE COALESCE(movement.amount,0)<>COALESCE(balance.amount,0)
    ) THEN
        RAISE EXCEPTION 'crypto custody balance projection differs from movements';
    END IF;
    IF EXISTS (
        SELECT 1 FROM (
            SELECT merchant_id,token_id,
                   SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END) AS amount
            FROM reg_crypto_merchant_balance_movements GROUP BY merchant_id,token_id
        ) movement FULL JOIN reg_crypto_merchant_balance_balances balance
          USING (merchant_id,token_id)
        WHERE COALESCE(movement.amount,0)<>COALESCE(balance.amount,0)
    ) THEN
        RAISE EXCEPTION 'crypto merchant balance projection differs from movements';
    END IF;
    IF EXISTS (
        SELECT 1 FROM (
            SELECT merchant_id,token_id,fee_type,
                   SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END) AS amount
            FROM reg_crypto_fee_movements GROUP BY merchant_id,token_id,fee_type
        ) movement FULL JOIN reg_crypto_fee_balances balance
          USING (merchant_id,token_id,fee_type)
        WHERE COALESCE(movement.amount,0)<>COALESCE(balance.balance,0)
    ) THEN
        RAISE EXCEPTION 'crypto fee balance projection differs from movements';
    END IF;
END;
$$;

ALTER TABLE reg_crypto_platform_cost_movements
    ADD COLUMN reversal_of_line_id UUID REFERENCES reg_crypto_platform_cost_movements(line_id);
CREATE UNIQUE INDEX uq_crypto_platform_cost_reversal_line
    ON reg_crypto_platform_cost_movements(reversal_of_line_id)
    WHERE reversal_of_line_id IS NOT NULL;

ALTER TABLE reg_crypto_provider_balance_movements
    ADD COLUMN reversal_of_line_id UUID REFERENCES reg_crypto_provider_balance_movements(line_id);
CREATE UNIQUE INDEX uq_crypto_provider_balance_reversal_line
    ON reg_crypto_provider_balance_movements(reversal_of_line_id)
    WHERE reversal_of_line_id IS NOT NULL;

ALTER TABLE reg_crypto_balance_movements
    ADD COLUMN reversal_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE reg_crypto_merchant_balance_movements
    ADD COLUMN reversal_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE reg_crypto_fee_movements
    ADD COLUMN reversal_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE reg_crypto_platform_cost_movements
    ADD COLUMN reversal_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE reg_crypto_provider_balance_movements
    ADD COLUMN reversal_reason TEXT NOT NULL DEFAULT '';

-- Posting corrections retain the original effects from earlier versions.
-- Enforce one platform cost effect per posting version; linked reversals have
-- their own line-level unique index above.
DROP INDEX idx_crypto_platform_cost_effect;
CREATE UNIQUE INDEX idx_crypto_platform_cost_effect
    ON reg_crypto_platform_cost_movements
       (recorder_type, recorder_id, recorder_version, record_type, cost_type, purpose, provider, external_order_id)
    WHERE reversal_of_line_id IS NULL;

-- Existing uniqueness applies to the original provider effect only. Every
-- reversal has its own line-level unique index above.
DROP INDEX idx_crypto_provider_balance_movement_effect;
CREATE UNIQUE INDEX idx_crypto_provider_balance_movement_effect
    ON reg_crypto_provider_balance_movements
       (recorder_type, recorder_id, record_type, operation_type, external_ref)
    WHERE reversal_of_line_id IS NULL;

CREATE FUNCTION fn_crypto_financial_movement_immutable()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'crypto financial movements are append-only'
        USING ERRCODE = '23514';
END;
$$ LANGUAGE plpgsql;

DO $$
DECLARE
    table_name TEXT;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'reg_crypto_balance_movements',
        'reg_crypto_merchant_balance_movements',
        'reg_crypto_fee_movements',
        'reg_crypto_platform_cost_movements',
        'reg_crypto_provider_balance_movements'
    ] LOOP
        EXECUTE format(
            'CREATE TRIGGER %I BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION fn_crypto_financial_movement_immutable()',
            'trg_immutable_' || table_name, table_name
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
        'reg_crypto_balance_movements',
        'reg_crypto_merchant_balance_movements',
        'reg_crypto_fee_movements',
        'reg_crypto_platform_cost_movements',
        'reg_crypto_provider_balance_movements'
    ] LOOP
        EXECUTE format('DROP TRIGGER %I ON %I',
            'trg_immutable_' || table_name, table_name);
    END LOOP;
END;
$$;
DROP FUNCTION fn_crypto_financial_movement_immutable();
DROP INDEX idx_crypto_platform_cost_effect;
CREATE UNIQUE INDEX idx_crypto_platform_cost_effect
    ON reg_crypto_platform_cost_movements
       (recorder_type, recorder_id, record_type, cost_type, purpose, provider, external_order_id);
DROP INDEX idx_crypto_provider_balance_movement_effect;
CREATE UNIQUE INDEX idx_crypto_provider_balance_movement_effect
    ON reg_crypto_provider_balance_movements
       (recorder_type, recorder_id, record_type, operation_type, external_ref);
ALTER TABLE reg_crypto_provider_balance_movements DROP COLUMN reversal_reason;
ALTER TABLE reg_crypto_platform_cost_movements DROP COLUMN reversal_reason;
ALTER TABLE reg_crypto_fee_movements DROP COLUMN reversal_reason;
ALTER TABLE reg_crypto_merchant_balance_movements DROP COLUMN reversal_reason;
ALTER TABLE reg_crypto_balance_movements DROP COLUMN reversal_reason;
DROP INDEX uq_crypto_provider_balance_reversal_line;
ALTER TABLE reg_crypto_provider_balance_movements DROP COLUMN reversal_of_line_id;
DROP INDEX uq_crypto_platform_cost_reversal_line;
ALTER TABLE reg_crypto_platform_cost_movements DROP COLUMN reversal_of_line_id;
-- +goose StatementEnd
