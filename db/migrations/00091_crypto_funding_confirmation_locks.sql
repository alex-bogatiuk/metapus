-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- Queue triggers also lock custody scopes on lease-only UPDATEs. A selector
-- must acquire the whole footprint before its funding row, and release all
-- newly acquired locks if any resource (including the funding row) is busy.
CREATE FUNCTION fn_crypto_try_lock_funding_confirmation(funding UUID) RETURNS BOOLEAN LANGUAGE plpgsql AS $$
DECLARE wallets UUID[]; locked UUID;
BEGIN
    SELECT ARRAY(
        SELECT source_wallet_id FROM doc_crypto_native_fundings WHERE id=funding
        UNION SELECT destination_wallet_id FROM doc_crypto_native_fundings WHERE id=funding
        UNION SELECT fee.payer_wallet_id FROM doc_crypto_native_fundings f
            JOIN reg_crypto_fee_reservations fee ON fee.id=f.fee_reservation_id WHERE f.id=funding
    ) INTO wallets;
    IF cardinality(wallets)=0 THEN RETURN FALSE; END IF;
    IF NOT fn_crypto_try_lock_custody_wallets(wallets) THEN RETURN FALSE; END IF;
    SELECT id INTO locked FROM doc_crypto_native_fundings WHERE id=funding FOR UPDATE NOWAIT;
    RETURN locked IS NOT NULL;
EXCEPTION WHEN lock_not_available THEN
    RETURN FALSE;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP FUNCTION fn_crypto_try_lock_funding_confirmation(UUID);
-- +goose StatementEnd
