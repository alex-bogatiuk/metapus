-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
-- Cryptographic database -> Vault namespace binding, not a tenant filter.
CREATE TABLE sys_crypto_aml_provisioning_identity (
 id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(id), vault_namespace UUID NOT NULL,
 database_name TEXT NOT NULL CHECK(database_name<>''), enrolled_by TEXT NOT NULL CHECK(btrim(enrolled_by)<>''),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL)
);
CREATE TRIGGER trg_crypto_aml_provision_identity_immutable BEFORE UPDATE OR DELETE ON sys_crypto_aml_provisioning_identity FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();
-- Separate control-plane queue. Ordinary wallet-pool workers cannot claim it
-- and do not gain access to Vault's immutable owner enrollment endpoint.
CREATE TABLE reg_crypto_aml_wallet_provisions (
 id UUID PRIMARY KEY,
 merchant_id UUID NOT NULL REFERENCES cat_merchants(id), network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id),
 wallet_role TEXT NOT NULL CHECK(wallet_role IN ('merchant_pool','liquidity_hot','network_fee')),
 derivation_path TEXT NOT NULL,
 request_digest TEXT NOT NULL CHECK(request_digest<>''), requested_by TEXT NOT NULL CHECK(btrim(requested_by)<>''),
 status TEXT NOT NULL DEFAULT 'processing' CHECK(status IN ('processing','retry_wait','completed')),
 attempts INT NOT NULL DEFAULT 1 CHECK(attempts>0), claimed_by TEXT NOT NULL DEFAULT '', claim_token UUID,
 lease_expires_at TIMESTAMPTZ, next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), last_error TEXT NOT NULL DEFAULT '',
 wallet_id UUID UNIQUE REFERENCES cat_wallets(id), enrollment_digest TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), completed_at TIMESTAMPTZ,
 _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
 UNIQUE(network_id,derivation_path),
 FOREIGN KEY(network_id,derivation_path) REFERENCES sys_crypto_wallet_derivations(network_id,derivation_path),
 CHECK((status='processing' AND claim_token IS NOT NULL AND lease_expires_at IS NOT NULL AND claimed_by<>'') OR (status<>'processing' AND claim_token IS NULL AND lease_expires_at IS NULL AND claimed_by='')),
 CHECK((status='completed' AND wallet_id=id AND enrollment_digest<>'' AND completed_at IS NOT NULL) OR (status<>'completed' AND wallet_id IS NULL AND enrollment_digest='' AND completed_at IS NULL))
);
CREATE UNIQUE INDEX idx_crypto_aml_fee_provision ON reg_crypto_aml_wallet_provisions(merchant_id,network_id) WHERE wallet_role='network_fee' AND status<>'completed';
CREATE INDEX idx_crypto_aml_provision_retry ON reg_crypto_aml_wallet_provisions(next_attempt_at) WHERE status<>'completed';
CREATE TRIGGER trg_crypto_aml_provision_updated BEFORE UPDATE ON reg_crypto_aml_wallet_provisions FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_crypto_aml_provision_txid BEFORE UPDATE ON reg_crypto_aml_wallet_provisions FOR EACH ROW EXECUTE FUNCTION update_txid_column();
CREATE FUNCTION fn_crypto_aml_provision_guard() RETURNS TRIGGER AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'merchant provisioning history cannot be deleted' USING ERRCODE='23514'; END IF;
 IF TG_OP='UPDATE' AND ((OLD.id,OLD.merchant_id,OLD.network_id,OLD.wallet_role,OLD.derivation_path,OLD.request_digest,OLD.requested_by,OLD.created_at)
    IS DISTINCT FROM (NEW.id,NEW.merchant_id,NEW.network_id,NEW.wallet_role,NEW.derivation_path,NEW.request_digest,NEW.requested_by,NEW.created_at) OR OLD.status='completed') THEN
   RAISE EXCEPTION 'merchant provisioning identity and completion are immutable' USING ERRCODE='23514';
 END IF;
 IF TG_OP='INSERT' AND (NEW.status<>'processing' OR NEW.attempts<>1 OR NOT EXISTS(SELECT 1 FROM sys_crypto_custody_policies WHERE network_id=NEW.network_id AND enforced)) THEN
   RAISE EXCEPTION 'merchant provisioning requires enforced custody and an initial lease' USING ERRCODE='23514';
 END IF;
 IF NEW.status='completed' AND (OLD.status<>'processing' OR OLD.lease_expires_at<=clock_timestamp() OR NOT EXISTS(
   SELECT 1 FROM cat_wallets w WHERE w.id=NEW.wallet_id AND w.custody_merchant_id=NEW.merchant_id AND w.network_id=NEW.network_id AND w.wallet_role=NEW.wallet_role AND w.derivation_path=NEW.derivation_path)) THEN
   RAISE EXCEPTION 'merchant provisioning completion lacks its owned wallet or current lease' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_provision_guard BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_aml_wallet_provisions FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_provision_guard();
CREATE FUNCTION fn_crypto_aml_legacy_provision_guard() RETURNS TRIGGER AS $$
BEGIN
 IF NEW.custody_merchant_id IS NULL AND NEW.wallet_role='merchant_pool'
    AND EXISTS(SELECT 1 FROM sys_crypto_wallet_provisioning_jobs WHERE network_id=NEW.network_id AND derivation_path=NEW.derivation_path) THEN
   PERFORM 1 FROM sys_crypto_custody_policies WHERE network_id=NEW.network_id AND enforced FOR UPDATE;
   IF FOUND THEN RAISE EXCEPTION 'owned network requires trusted merchant provisioning' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_legacy_provision_guard BEFORE INSERT ON cat_wallets FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_legacy_provision_guard();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN IF EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_provisions) OR EXISTS(SELECT 1 FROM sys_crypto_aml_provisioning_identity) THEN RAISE EXCEPTION 'cannot roll back merchant provisioning history; use a forward migration'; END IF; END $$;
DROP TABLE reg_crypto_aml_wallet_provisions;
DROP FUNCTION fn_crypto_aml_provision_guard();
DROP TRIGGER trg_crypto_aml_legacy_provision_guard ON cat_wallets;
DROP FUNCTION fn_crypto_aml_legacy_provision_guard();
DROP TABLE sys_crypto_aml_provisioning_identity;
-- +goose StatementEnd
