-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
CREATE FUNCTION fn_crypto_activation_identity_valid(task UUID,wallet UUID,network UUID,operation UUID) RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM cat_wallets w WHERE w.id=wallet AND w.network_id=network)
 AND (operation IS NULL OR EXISTS(SELECT 1 FROM doc_crypto_network_operations o WHERE o.id=operation AND o.network_id=network AND o.target_wallet_id=wallet AND o.basis_type='CryptoAccountActivationTask' AND o.basis_id=task));
$$;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM reg_crypto_account_activations a WHERE NOT fn_crypto_activation_identity_valid(a.id,a.wallet_id,a.network_id,a.network_operation_id)) THEN
  RAISE EXCEPTION 'existing activation identity requires review before upgrade' USING ERRCODE='23514';
 END IF;
END; $$;
CREATE FUNCTION fn_crypto_activation_identity_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'activation task identity cannot be deleted' USING ERRCODE='23514'; END IF;
 IF TG_OP='UPDATE' AND ((NEW.id,NEW.wallet_id,NEW.network_id,NEW.created_at) IS DISTINCT FROM (OLD.id,OLD.wallet_id,OLD.network_id,OLD.created_at)
 OR (OLD.network_operation_id IS NOT NULL AND NEW.network_operation_id IS DISTINCT FROM OLD.network_operation_id)) THEN
  RAISE EXCEPTION 'activation task identity is immutable' USING ERRCODE='23514';
 END IF;
 IF NOT fn_crypto_activation_identity_valid(NEW.id,NEW.wallet_id,NEW.network_id,NEW.network_operation_id) THEN
  RAISE EXCEPTION 'activation task identity does not match its wallet or operation' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER trg_crypto_activation_identity BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_account_activations FOR EACH ROW EXECUTE FUNCTION fn_crypto_activation_identity_guard();
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TRIGGER trg_crypto_activation_identity ON reg_crypto_account_activations;
DROP FUNCTION fn_crypto_activation_identity_guard();
DROP FUNCTION fn_crypto_activation_identity_valid(UUID,UUID,UUID,UUID);
-- +goose StatementEnd
