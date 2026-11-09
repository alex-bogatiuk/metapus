-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- Fee/resource and AML withdrawal writers already lock an entire owner/network.
-- Custody admission must use the same granularity before touching any wallet,
-- including disabled token scopes that can be enabled concurrently.
CREATE FUNCTION fn_crypto_lock_custody_scopes(wallets UUID[]) RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
    PERFORM s.version FROM sys_crypto_aml_routing_scopes s
    WHERE EXISTS(SELECT 1 FROM cat_wallets w WHERE w.id=ANY(wallets)
        AND (w.custody_merchant_id,w.network_id)=(s.merchant_id,s.network_id))
    ORDER BY s.merchant_id,s.network_id,s.token_id FOR UPDATE OF s;
END;
$$;
CREATE FUNCTION fn_crypto_lock_custody_wallets(wallets UUID[]) RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE found_count INT; expected_count INT;
BEGIN
    IF array_position(wallets,NULL) IS NOT NULL THEN
        RAISE EXCEPTION 'custody wallet lock requires valid wallet identifiers' USING ERRCODE='23514';
    END IF;
    PERFORM fn_crypto_lock_custody_scopes(wallets);
    PERFORM id FROM cat_wallets WHERE id=ANY(wallets) ORDER BY id FOR UPDATE;
    GET DIAGNOSTICS found_count=ROW_COUNT;
    SELECT count(DISTINCT value) INTO expected_count FROM unnest(wallets) value;
    IF found_count<>expected_count THEN
        RAISE EXCEPTION 'custody wallet lock target is missing' USING ERRCODE='23514';
    END IF;
END;
$$;
CREATE OR REPLACE FUNCTION fn_crypto_aml_lock_resource_scopes(reservation UUID) RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
    PERFORM fn_crypto_lock_custody_scopes(ARRAY(SELECT payer_wallet_id FROM reg_crypto_fee_reservations WHERE id=reservation));
END;
$$;
CREATE OR REPLACE FUNCTION fn_crypto_aml_lock_fee_wallet_scopes(payer UUID,asset UUID) RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
    PERFORM fn_crypto_lock_custody_scopes(ARRAY[payer]);
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
-- Lock-only migration: no financial row, ownership or historical byte changes.
CREATE OR REPLACE FUNCTION fn_crypto_aml_lock_resource_scopes(reservation UUID) RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
 PERFORM 1 FROM reg_crypto_fee_reservations fee JOIN cat_wallets w ON w.id=fee.payer_wallet_id
   JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=w.custody_merchant_id AND s.network_id=fee.network_id
   WHERE fee.id=reservation AND (EXISTS(SELECT 1 FROM reg_crypto_aml_resource_budgets b WHERE b.fee_reservation_id=fee.id)
      OR EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes native WHERE native.merchant_id=w.custody_merchant_id AND native.network_id=fee.network_id AND native.token_id=fee.native_token_id AND native.enabled))
   ORDER BY s.merchant_id,s.network_id,s.token_id FOR UPDATE OF s;
END;
$$;
CREATE OR REPLACE FUNCTION fn_crypto_aml_lock_fee_wallet_scopes(payer UUID,asset UUID) RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
 PERFORM 1 FROM cat_wallets w JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=w.custody_merchant_id AND s.network_id=w.network_id
 WHERE w.id=payer AND EXISTS(SELECT 1 FROM sys_crypto_aml_routing_scopes native WHERE native.merchant_id=w.custody_merchant_id AND native.network_id=w.network_id AND native.token_id=asset AND native.enabled)
 ORDER BY s.merchant_id,s.network_id,s.token_id FOR UPDATE OF s;
END;
$$;
DROP FUNCTION fn_crypto_lock_custody_wallets(UUID[]);
DROP FUNCTION fn_crypto_lock_custody_scopes(UUID[]);
-- +goose StatementEnd
