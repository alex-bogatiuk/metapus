-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

ALTER TABLE cat_integration_accounts DROP CONSTRAINT cat_integration_accounts_provider_check;
ALTER TABLE cat_integration_accounts ADD CONSTRAINT cat_integration_accounts_provider_check CHECK(provider IN ('alchemy','coingecko_demo','tron_api','chainstack','getblock','infura','drpc','telegram','smtp','resource_api','getblock_net'));
ALTER TABLE cat_integration_pools DROP CONSTRAINT cat_integration_pools_provider_check;
ALTER TABLE cat_integration_pools ADD CONSTRAINT cat_integration_pools_provider_check CHECK(provider IN ('alchemy','coingecko_demo','tron_api','chainstack','getblock','infura','drpc','telegram','smtp','resource_api','getblock_net'));

-- A mapping is an accepted, immutable contract snapshot. New vendor contracts
-- receive a new version; old tickets continue using their original version.
CREATE TABLE sys_crypto_aml_provider_mappings (
    network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    token_id UUID NOT NULL,
    version TEXT NOT NULL CHECK(version<>''),
    contract_version TEXT NOT NULL CHECK(contract_version<>''),
    contract_address TEXT NOT NULL DEFAULT '',
    currency TEXT NOT NULL CHECK(currency IN ('TRX','ETH')),
    provider_token_id TEXT NOT NULL DEFAULT '',
    native BOOLEAN NOT NULL,
    decimal_places INT NOT NULL CHECK(decimal_places BETWEEN 0 AND 18),
    capability_accepted BOOLEAN NOT NULL DEFAULT FALSE,
    acceptance_reference TEXT NOT NULL DEFAULT '',
    currencylist_snapshot JSONB NOT NULL CHECK(jsonb_typeof(currencylist_snapshot)='object'),
    pending_statuses TEXT[] NOT NULL DEFAULT '{}',
    failure_statuses TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
    PRIMARY KEY(network_id,token_id,version),
    FOREIGN KEY(token_id,network_id) REFERENCES cat_tokens(id,network_id),
    CHECK((native AND contract_address='' AND provider_token_id='') OR
          (NOT native AND contract_address<>'' AND provider_token_id<>'')),
    CHECK(NOT capability_accepted OR (acceptance_reference<>'' AND currencylist_snapshot<>'{}'::jsonb))
);

CREATE FUNCTION fn_crypto_aml_mapping_scope_guard() RETURNS TRIGGER AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM cat_tokens t JOIN cat_blockchain_networks n ON n.id=t.network_id
        WHERE t.id=NEW.token_id AND t.network_id=NEW.network_id AND t.decimal_places=NEW.decimal_places
          AND (t.token_standard='native')=NEW.native
          AND ((n.chain_family='tron' AND NEW.currency='TRX' AND COALESCE(t.contract_address,'')=NEW.contract_address)
            OR (n.chain_family='evm' AND NEW.currency='ETH' AND LOWER(COALESCE(t.contract_address,''))=LOWER(NEW.contract_address)))) THEN
        RAISE EXCEPTION 'AML mapping does not match network/token/contract/decimals' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_mapping_scope_guard BEFORE INSERT ON sys_crypto_aml_provider_mappings
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_mapping_scope_guard();

CREATE TABLE sys_crypto_aml_account_policies (
    account_id UUID PRIMARY KEY REFERENCES cat_integration_accounts(id),
    allow_submissions BOOLEAN NOT NULL DEFAULT FALSE,
    version BIGINT NOT NULL DEFAULT 1 CHECK(version>0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL)
);

-- Local grants bound paid use of a purchased package. They are deliberately
-- distinct from the vendor's live checks_left/checks_used observation. No
-- calendar or credential rotation can replenish this budget.
CREATE TABLE reg_crypto_aml_package_credits (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    account_id UUID NOT NULL REFERENCES cat_integration_accounts(id),
    units BIGINT NOT NULL CHECK(units>0 AND units<=1000000000),
    external_reference TEXT NOT NULL CHECK(external_reference<>''),
    authorized_by TEXT NOT NULL CHECK(authorized_by<>''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
    UNIQUE(account_id,external_reference)
);
ALTER TABLE reg_crypto_aml_jobs ADD CONSTRAINT uq_crypto_aml_job_account UNIQUE(id,provider_account_id);
ALTER TABLE reg_crypto_aml_jobs ADD CONSTRAINT uq_crypto_aml_job_generation_provider UNIQUE(subject_id,generation,provider);
CREATE TABLE reg_crypto_aml_paid_admissions (
    job_id UUID PRIMARY KEY,
    account_id UUID NOT NULL REFERENCES cat_integration_accounts(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL),
    FOREIGN KEY(job_id,account_id) REFERENCES reg_crypto_aml_jobs(id,provider_account_id)
);
CREATE INDEX idx_crypto_aml_paid_admissions_account ON reg_crypto_aml_paid_admissions(account_id);

CREATE FUNCTION fn_crypto_aml_runtime_account_guard() RETURNS TRIGGER AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM cat_integration_accounts WHERE id=NEW.account_id AND provider='getblock_net') THEN
        RAISE EXCEPTION 'AML runtime requires a Getblock.net account' USING ERRCODE='23514';
    END IF;
    IF TG_TABLE_NAME='sys_crypto_aml_account_policies' AND TG_OP='UPDATE' THEN
        IF NEW.account_id<>OLD.account_id OR NEW.version<>OLD.version+1 THEN
            RAISE EXCEPTION 'AML account policy requires a new version' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_account_policy_guard BEFORE INSERT OR UPDATE ON sys_crypto_aml_account_policies
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_runtime_account_guard();
CREATE TRIGGER trg_crypto_aml_credit_account_guard BEFORE INSERT ON reg_crypto_aml_package_credits
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_runtime_account_guard();
CREATE TRIGGER trg_crypto_aml_admission_account_guard BEFORE INSERT ON reg_crypto_aml_paid_admissions
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_runtime_account_guard();
CREATE TRIGGER trg_crypto_aml_mapping_immutable BEFORE UPDATE OR DELETE ON sys_crypto_aml_provider_mappings
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();
CREATE TRIGGER trg_crypto_aml_credit_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_package_credits
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();
CREATE TRIGGER trg_crypto_aml_admission_immutable BEFORE UPDATE OR DELETE ON reg_crypto_aml_paid_admissions
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();
CREATE TRIGGER trg_crypto_aml_account_policy_updated BEFORE UPDATE ON sys_crypto_aml_account_policies
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_crypto_aml_account_policy_txid BEFORE UPDATE ON sys_crypto_aml_account_policies
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN
    IF EXISTS(SELECT 1 FROM reg_crypto_aml_paid_admissions) OR EXISTS(SELECT 1 FROM reg_crypto_aml_package_credits)
       OR EXISTS(SELECT 1 FROM sys_crypto_aml_provider_mappings)
       OR EXISTS(SELECT 1 FROM cat_integration_accounts WHERE provider='getblock_net')
       OR EXISTS(SELECT 1 FROM cat_integration_pools WHERE provider='getblock_net') THEN
        RAISE EXCEPTION 'cannot roll back AML runtime evidence; use a forward migration';
    END IF;
END $$;
DROP TABLE reg_crypto_aml_paid_admissions;
ALTER TABLE reg_crypto_aml_jobs DROP CONSTRAINT uq_crypto_aml_job_account;
ALTER TABLE reg_crypto_aml_jobs DROP CONSTRAINT uq_crypto_aml_job_generation_provider;
DROP TABLE reg_crypto_aml_package_credits;
DROP TABLE sys_crypto_aml_account_policies;
DROP TABLE sys_crypto_aml_provider_mappings;
DROP FUNCTION fn_crypto_aml_mapping_scope_guard();
DROP FUNCTION fn_crypto_aml_runtime_account_guard();
ALTER TABLE cat_integration_accounts DROP CONSTRAINT cat_integration_accounts_provider_check;
ALTER TABLE cat_integration_accounts ADD CONSTRAINT cat_integration_accounts_provider_check CHECK(provider IN ('alchemy','coingecko_demo','tron_api','chainstack','getblock','infura','drpc','telegram','smtp','resource_api'));
ALTER TABLE cat_integration_pools DROP CONSTRAINT cat_integration_pools_provider_check;
ALTER TABLE cat_integration_pools ADD CONSTRAINT cat_integration_pools_provider_check CHECK(provider IN ('alchemy','coingecko_demo','tron_api','chainstack','getblock','infura','drpc','telegram','smtp','resource_api'));
-- +goose StatementEnd
