-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- Identity belongs to the immutable producer fact; leases/results are an
-- operational projection. Never repair an old conflicting target implicitly.
CREATE FUNCTION fn_crypto_reconciliation_request_identity_valid(network UUID,wallet UUID,token UUID,source_kind TEXT,source UUID) RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM cat_wallets w JOIN cat_tokens t ON t.network_id=w.network_id
     WHERE w.id=wallet AND t.id=token AND w.network_id=network)
 AND CASE source_kind
     WHEN 'native_funding_source' THEN EXISTS(SELECT 1 FROM doc_crypto_native_fundings f
         WHERE f.id=source AND f.network_id=network AND f.source_wallet_id=wallet AND f.native_token_id=token)
     WHEN 'native_funding_destination' THEN EXISTS(SELECT 1 FROM doc_crypto_native_fundings f
         WHERE f.id=source AND f.network_id=network AND f.destination_wallet_id=wallet AND f.native_token_id=token)
     WHEN 'custody_adjustment' THEN EXISTS(SELECT 1 FROM doc_crypto_custody_adjustments d
         WHERE d.id=source AND d.network_id=network AND d.wallet_id=wallet AND d.token_id=token)
     ELSE TRUE END;
$$;

DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM reg_crypto_reconciliation_requests r WHERE NOT fn_crypto_reconciliation_request_identity_valid(r.network_id,r.wallet_id,r.token_id,r.source_type,r.source_id)) THEN
     RAISE EXCEPTION 'existing reconciliation request identity requires review before upgrade' USING ERRCODE='23514';
 END IF;
END; $$;

CREATE FUNCTION fn_crypto_reconciliation_request_identity_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'reconciliation request identity cannot be deleted' USING ERRCODE='23514'; END IF;
 IF TG_OP='UPDATE' THEN
     IF (NEW.id,NEW.network_id,NEW.wallet_id,NEW.token_id,NEW.source_type,NEW.source_id,NEW.requested_at)
         IS DISTINCT FROM (OLD.id,OLD.network_id,OLD.wallet_id,OLD.token_id,OLD.source_type,OLD.source_id,OLD.requested_at) THEN
         RAISE EXCEPTION 'reconciliation request identity is immutable' USING ERRCODE='23514'; END IF;
 ELSE
     IF NOT fn_crypto_reconciliation_request_identity_valid(NEW.network_id,NEW.wallet_id,NEW.token_id,NEW.source_type,NEW.source_id) THEN
         RAISE EXCEPTION 'reconciliation request does not match its source or asset network' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER trg_crypto_reconciliation_request_identity BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_reconciliation_requests
 FOR EACH ROW EXECUTE FUNCTION fn_crypto_reconciliation_request_identity_guard();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TRIGGER trg_crypto_reconciliation_request_identity ON reg_crypto_reconciliation_requests;
DROP FUNCTION fn_crypto_reconciliation_request_identity_guard();
DROP FUNCTION fn_crypto_reconciliation_request_identity_valid(UUID,UUID,UUID,TEXT,UUID);
-- +goose StatementEnd
