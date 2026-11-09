-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- Ownership enforcement is rolled out separately from AML classification.
-- An absent policy remains legacy-compatible until the coordinated switch.
CREATE TABLE sys_crypto_custody_policies (
    network_id UUID PRIMARY KEY REFERENCES cat_blockchain_networks(id),
    enforced BOOLEAN NOT NULL DEFAULT FALSE,
    version BIGINT NOT NULL DEFAULT 1 CHECK(version>0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL)
);
CREATE FUNCTION fn_crypto_custody_policy_guard() RETURNS TRIGGER AS $$
DECLARE execution_enabled BOOLEAN;
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'custody policy cannot be deleted' USING ERRCODE='23514';
    END IF;
    IF TG_OP='UPDATE' THEN
        IF NEW.network_id<>OLD.network_id OR NEW.version<>OLD.version+1 THEN
            RAISE EXCEPTION 'custody policy requires a new version in the same network' USING ERRCODE='23514';
        END IF;
        IF OLD.enforced AND NOT NEW.enforced THEN
            RAISE EXCEPTION 'merchant custody enforcement cannot be downgraded' USING ERRCODE='23514';
        END IF;
    END IF;
    IF NEW.enforced AND (TG_OP='INSERT' OR NOT OLD.enforced) THEN
        SELECT enabled INTO execution_enabled FROM sys_crypto_network_policies
            WHERE network_id=NEW.network_id FOR UPDATE;
        IF execution_enabled THEN
            RAISE EXCEPTION 'pause network execution before enabling merchant custody' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_custody_policy_guard BEFORE INSERT OR UPDATE OR DELETE ON sys_crypto_custody_policies
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_policy_guard();
CREATE TRIGGER trg_crypto_custody_policy_updated BEFORE UPDATE ON sys_crypto_custody_policies
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_crypto_custody_policy_txid BEFORE UPDATE ON sys_crypto_custody_policies
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

DROP INDEX idx_cat_wallets_one_active_network_fee;
CREATE UNIQUE INDEX idx_cat_wallets_one_active_network_fee
    ON cat_wallets(network_id)
    WHERE wallet_role='network_fee' AND custody_merchant_id IS NULL AND is_active AND NOT deletion_mark;
CREATE UNIQUE INDEX idx_cat_wallets_one_merchant_network_fee
    ON cat_wallets(network_id,custody_merchant_id)
    WHERE wallet_role='network_fee' AND custody_merchant_id IS NOT NULL AND is_active AND NOT deletion_mark;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM sys_crypto_custody_policies WHERE enforced) OR
       EXISTS(SELECT 1 FROM cat_wallets WHERE wallet_role='network_fee' AND is_active AND NOT deletion_mark
              GROUP BY network_id HAVING COUNT(*)>1) THEN
        RAISE EXCEPTION 'cannot roll back active merchant custody; use a forward migration';
    END IF;
END $$;
DROP INDEX idx_cat_wallets_one_merchant_network_fee;
DROP INDEX idx_cat_wallets_one_active_network_fee;
CREATE UNIQUE INDEX idx_cat_wallets_one_active_network_fee ON cat_wallets(network_id)
    WHERE wallet_role='network_fee' AND is_active AND NOT deletion_mark;
DROP TABLE sys_crypto_custody_policies;
DROP FUNCTION fn_crypto_custody_policy_guard();
-- +goose StatementEnd
