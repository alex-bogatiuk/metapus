-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- Candidate selection must skip a busy *complete* footprint, without keeping
-- a prefix of its scopes/wallets and subsequently waiting in another order.
-- The exception block is a subtransaction: NOWAIT failure releases its locks.
CREATE FUNCTION fn_crypto_try_lock_custody_wallets(wallets UUID[],scope_merchant UUID DEFAULT NULL,scope_network UUID DEFAULT NULL) RETURNS BOOLEAN LANGUAGE plpgsql AS $$
DECLARE found_count INT; expected_count INT;
BEGIN
    IF wallets IS NULL OR cardinality(wallets)=0 OR array_position(wallets,NULL) IS NOT NULL
        OR (scope_merchant IS NULL)<>(scope_network IS NULL) THEN
        RAISE EXCEPTION 'custody selection requires valid wallet identifiers' USING ERRCODE='23514';
    END IF;
    PERFORM s.version FROM sys_crypto_aml_routing_scopes s
        WHERE EXISTS(SELECT 1 FROM cat_wallets w WHERE w.id=ANY(wallets)
            AND (w.custody_merchant_id,w.network_id)=(s.merchant_id,s.network_id))
            OR (s.merchant_id=scope_merchant AND s.network_id=scope_network)
        ORDER BY s.merchant_id,s.network_id,s.token_id FOR UPDATE OF s NOWAIT;
    PERFORM id FROM cat_wallets WHERE id=ANY(wallets) ORDER BY id FOR UPDATE NOWAIT;
    GET DIAGNOSTICS found_count=ROW_COUNT;
    SELECT count(DISTINCT value) INTO expected_count FROM unnest(wallets) value;
    IF found_count<>expected_count THEN
        RAISE EXCEPTION 'custody selection wallet is missing' USING ERRCODE='23514';
    END IF;
    RETURN TRUE;
EXCEPTION WHEN lock_not_available THEN
    RETURN FALSE;
END;
$$;

-- A withdrawal also owns its request merchant/network scope when legacy
-- platform wallets or retained fee/funding resources have other owners.
-- Acquire the union in one order, never merchant scope followed by a new owner.
CREATE FUNCTION fn_crypto_lock_withdrawal_wallets(wallets UUID[],scope_merchant UUID,scope_network UUID) RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
    IF scope_merchant IS NULL OR scope_network IS NULL THEN
        RAISE EXCEPTION 'withdrawal resources require their request scope' USING ERRCODE='23514';
    END IF;
    PERFORM s.version FROM sys_crypto_aml_routing_scopes s
        WHERE EXISTS(SELECT 1 FROM cat_wallets w WHERE w.id=ANY(wallets)
            AND (w.custody_merchant_id,w.network_id)=(s.merchant_id,s.network_id))
            OR (s.merchant_id=scope_merchant AND s.network_id=scope_network)
        ORDER BY s.merchant_id,s.network_id,s.token_id FOR UPDATE OF s;
    PERFORM fn_crypto_lock_custody_wallets(wallets);
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP FUNCTION fn_crypto_lock_withdrawal_wallets(UUID[],UUID,UUID);
DROP FUNCTION fn_crypto_try_lock_custody_wallets(UUID[],UUID,UUID);
-- +goose StatementEnd
