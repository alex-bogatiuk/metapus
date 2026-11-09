-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
CREATE TABLE reg_crypto_activation_dispatches (
 authorization_id TEXT NOT NULL REFERENCES reg_crypto_signing_authorizations(authorization_id),
 activation_id UUID NOT NULL REFERENCES reg_crypto_account_activations(id),
 phase TEXT NOT NULL CHECK(phase IN ('authorize','sign','broadcast')),
 claim_token UUID NOT NULL,
 network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id),
 unsigned_fingerprint TEXT NOT NULL CHECK(unsigned_fingerprint<>''),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 _txid BIGINT NOT NULL DEFAULT txid_current(),
 PRIMARY KEY(authorization_id,phase,claim_token)
);
CREATE UNIQUE INDEX idx_crypto_activation_single_sign_dispatch ON reg_crypto_activation_dispatches(authorization_id) WHERE phase='sign';
CREATE FUNCTION fn_crypto_activation_dispatch_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'activation dispatch facts are immutable' USING ERRCODE='23514'; END IF;
 IF NOT EXISTS(SELECT 1 FROM reg_crypto_account_activations a JOIN reg_crypto_signing_authorizations s ON s.basis_id=a.id AND s.basis_type='CryptoAccountActivationTask' AND s.purpose='activation'
 JOIN cat_wallets target ON target.id=a.wallet_id AND s.destination_address=target.address
 JOIN cat_wallets payer ON payer.network_id=a.network_id AND payer.address=s.source_address AND payer.address=s.fee_payer_address AND payer.vault_key_id=s.vault_key_id
 JOIN reg_crypto_fee_reservations f ON f.basis_type=s.basis_type AND f.basis_id=s.basis_id AND f.purpose=s.purpose AND f.attempt=s.attempt AND f.network_id=a.network_id
 AND f.payer_wallet_id=payer.id AND f.unsigned_fingerprint=s.unsigned_fingerprint AND f.max_fee_minor=s.max_network_fee_minor
 JOIN cat_tokens native ON native.id=f.native_token_id AND native.network_id=a.network_id AND native.token_standard='native' AND native.id=f.operation_token_id
 WHERE a.id=NEW.activation_id AND a.activation_claim_token=NEW.claim_token AND a.activation_lease_expires_at>clock_timestamp()
 AND a.activation_claimed_by<>'' AND a.status IN ('claimed','held','signing','broadcast','confirmed')
 AND s.authorization_id=NEW.authorization_id AND s.network_id=a.network_id AND s.network_id=NEW.network_id
 AND s.unsigned_fingerprint=NEW.unsigned_fingerprint AND s.attempt=1 AND s.generation=1 AND s.amount_minor_units=0 AND s.asset_standard='native' AND s.contract_address=''
 AND (NEW.phase<>'broadcast' OR (s.signed_envelope IS NOT NULL AND s.tx_hash<>'' AND s.signed_envelope->>'txHash'=s.tx_hash))) THEN
  RAISE EXCEPTION 'activation dispatch requires its current claim and exact authorization' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER trg_crypto_activation_dispatch_guard BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_activation_dispatches FOR EACH ROW EXECUTE FUNCTION fn_crypto_activation_dispatch_guard();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM reg_crypto_activation_dispatches) THEN
  RAISE EXCEPTION 'activation dispatch history requires forward recovery' USING ERRCODE='23514';
 END IF;
END; $$;
DROP TABLE reg_crypto_activation_dispatches;
DROP FUNCTION fn_crypto_activation_dispatch_guard();
-- +goose StatementEnd
