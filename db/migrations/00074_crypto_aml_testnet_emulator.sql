-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE cat_integration_accounts DROP CONSTRAINT cat_integration_accounts_provider_check;
ALTER TABLE cat_integration_accounts ADD CONSTRAINT cat_integration_accounts_provider_check CHECK(provider IN ('alchemy','coingecko_demo','tron_api','chainstack','getblock','infura','drpc','telegram','smtp','resource_api','getblock_net','testnet_aml'));
ALTER TABLE cat_integration_pools DROP CONSTRAINT cat_integration_pools_provider_check;
ALTER TABLE cat_integration_pools ADD CONSTRAINT cat_integration_pools_provider_check CHECK(provider IN ('alchemy','coingecko_demo','tron_api','chainstack','getblock','infura','drpc','telegram','smtp','resource_api','getblock_net','testnet_aml'));
ALTER TABLE reg_crypto_aml_jobs DROP CONSTRAINT reg_crypto_aml_jobs_provider_check;
ALTER TABLE reg_crypto_aml_jobs ADD CONSTRAINT reg_crypto_aml_jobs_provider_check CHECK(provider IN ('getblock_net','testnet_aml'));
ALTER TABLE reg_crypto_aml_provider_checks DROP CONSTRAINT reg_crypto_aml_provider_checks_provider_check;
ALTER TABLE reg_crypto_aml_provider_checks ADD CONSTRAINT reg_crypto_aml_provider_checks_provider_check CHECK(provider IN ('getblock_net','testnet_aml'));

CREATE FUNCTION fn_crypto_aml_is_emulation_network(family TEXT,identifier TEXT) RETURNS BOOLEAN AS $$
 SELECT (family='tron' AND identifier='nile') OR (family='evm' AND identifier='11155111');
$$ LANGUAGE SQL IMMUTABLE;
CREATE TABLE sys_crypto_aml_testnet_emulators (
 account_id UUID PRIMARY KEY REFERENCES cat_integration_accounts(id), network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id),
 seed TEXT NOT NULL CHECK(length(seed) BETWEEN 1 AND 128), max_delay_seconds INT NOT NULL DEFAULT 10 CHECK(max_delay_seconds BETWEEN 0 AND 60),
 unknown_rate_bp INT NOT NULL DEFAULT 1000 CHECK(unknown_rate_bp BETWEEN 0 AND 10000), configured_by TEXT NOT NULL CHECK(btrim(configured_by)<>''),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), _txid BIGINT NOT NULL DEFAULT txid_current(), _deleted_at TIMESTAMPTZ CHECK(_deleted_at IS NULL)
);
CREATE INDEX idx_crypto_aml_testnet_network ON sys_crypto_aml_testnet_emulators(network_id);
CREATE FUNCTION fn_crypto_aml_testnet_binding_guard() RETURNS TRIGGER AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM cat_integration_accounts a JOIN cat_blockchain_networks n ON n.id=NEW.network_id
   WHERE a.id=NEW.account_id AND a.provider='testnet_aml' AND fn_crypto_aml_is_emulation_network(n.chain_family,n.network_identifier)) THEN
   RAISE EXCEPTION 'AML emulation is restricted to TRON Nile and Ethereum Sepolia' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_testnet_binding BEFORE INSERT ON sys_crypto_aml_testnet_emulators FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_testnet_binding_guard();
CREATE TRIGGER trg_crypto_aml_testnet_immutable BEFORE UPDATE OR DELETE ON sys_crypto_aml_testnet_emulators FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_evidence_guard();
CREATE FUNCTION fn_crypto_aml_testnet_identity_guard() RETURNS TRIGGER AS $$
BEGIN
 IF TG_TABLE_NAME='cat_blockchain_networks' THEN
   IF (NEW.chain_family,NEW.network_identifier) IS DISTINCT FROM (OLD.chain_family,OLD.network_identifier) AND EXISTS(SELECT 1 FROM sys_crypto_aml_testnet_emulators WHERE network_id=OLD.id) THEN
     RAISE EXCEPTION 'emulated network identity is immutable' USING ERRCODE='23514';
   END IF;
 ELSE
   IF NEW.provider<>OLD.provider AND (EXISTS(SELECT 1 FROM sys_crypto_aml_testnet_emulators WHERE account_id=OLD.id) OR EXISTS(SELECT 1 FROM reg_crypto_aml_jobs WHERE provider_account_id=OLD.id)) THEN
     RAISE EXCEPTION 'pinned AML account provider is immutable' USING ERRCODE='23514';
   END IF;
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_testnet_network BEFORE UPDATE ON cat_blockchain_networks FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_testnet_identity_guard();
CREATE TRIGGER trg_crypto_aml_testnet_account BEFORE UPDATE ON cat_integration_accounts FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_testnet_identity_guard();
CREATE FUNCTION fn_crypto_aml_testnet_mapping_guard() RETURNS TRIGGER AS $$
BEGIN
 IF NEW.contract_version='aml-testnet.v1' AND NOT EXISTS(SELECT 1 FROM cat_blockchain_networks n WHERE n.id=NEW.network_id AND fn_crypto_aml_is_emulation_network(n.chain_family,n.network_identifier) AND NEW.currencylist_snapshot->'emulated'='true'::jsonb) THEN
   RAISE EXCEPTION 'synthetic AML mappings are restricted to Nile/Sepolia' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_testnet_mapping BEFORE INSERT ON sys_crypto_aml_provider_mappings FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_testnet_mapping_guard();
CREATE FUNCTION fn_crypto_aml_job_provider_guard() RETURNS TRIGGER AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM cat_integration_accounts WHERE id=NEW.provider_account_id AND provider=NEW.provider) THEN RAISE EXCEPTION 'AML job account/provider mismatch' USING ERRCODE='23514'; END IF;
 IF NEW.provider='testnet_aml' AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_subjects s JOIN sys_crypto_aml_testnet_emulators e ON e.account_id=NEW.provider_account_id AND e.network_id=s.network_id
    JOIN cat_blockchain_networks n ON n.id=s.network_id JOIN sys_crypto_aml_provider_mappings m ON m.network_id=s.network_id AND m.token_id=s.token_id AND m.version=NEW.mapping_version
    WHERE s.id=NEW.subject_id AND fn_crypto_aml_is_emulation_network(n.chain_family,n.network_identifier) AND m.contract_version='aml-testnet.v1') THEN RAISE EXCEPTION 'emulated job requires its pinned testnet mapping' USING ERRCODE='23514'; END IF;
 IF NEW.provider='getblock_net' AND EXISTS(SELECT 1 FROM reg_crypto_aml_subjects s JOIN sys_crypto_aml_provider_mappings m ON m.network_id=s.network_id AND m.token_id=s.token_id AND m.version=NEW.mapping_version WHERE s.id=NEW.subject_id AND m.contract_version='aml-testnet.v1') THEN RAISE EXCEPTION 'synthetic mapping cannot create paid live jobs' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_job_provider BEFORE INSERT ON reg_crypto_aml_jobs FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_job_provider_guard();
CREATE FUNCTION fn_crypto_aml_check_provider_guard() RETURNS TRIGGER AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM reg_crypto_aml_jobs j WHERE j.id=NEW.job_id AND j.subject_id=NEW.subject_id AND j.provider=NEW.provider AND j.provider_account_id=NEW.provider_account_id AND j.request_digest=NEW.request_digest) THEN
   RAISE EXCEPTION 'AML provider check must match its pinned job' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_check_provider BEFORE INSERT ON reg_crypto_aml_provider_checks FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_check_provider_guard();
CREATE FUNCTION fn_crypto_aml_report_emulation_guard() RETURNS TRIGGER AS $$
DECLARE provider_name TEXT;
BEGIN
 SELECT c.provider INTO STRICT provider_name FROM reg_crypto_aml_provider_checks c JOIN reg_crypto_aml_jobs j ON j.id=c.job_id AND j.provider=c.provider AND j.provider_account_id=c.provider_account_id WHERE c.id=NEW.provider_check_id;
 IF provider_name='testnet_aml' THEN
   IF NEW.contract_version<>'aml-testnet.v1' OR NEW.adapter_version<>'testnet-aml.v1' OR (convert_from(NEW.raw_response,'UTF8')::jsonb)->'emulated' IS DISTINCT FROM 'true'::jsonb THEN
     RAISE EXCEPTION 'emulated AML reports must retain their explicit provenance' USING ERRCODE='23514';
   END IF;
 ELSIF NEW.contract_version='aml-testnet.v1' OR NEW.adapter_version='testnet-aml.v1' THEN RAISE EXCEPTION 'emulated report cannot satisfy a live provider check' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_aml_report_emulation BEFORE INSERT ON reg_crypto_aml_reports FOR EACH ROW EXECUTE FUNCTION fn_crypto_aml_report_emulation_guard();
CREATE OR REPLACE FUNCTION fn_crypto_aml_runtime_account_guard() RETURNS TRIGGER AS $$
BEGIN
    IF NOT EXISTS(SELECT 1 FROM cat_integration_accounts WHERE id=NEW.account_id AND provider IN ('getblock_net','testnet_aml')) THEN
        RAISE EXCEPTION 'AML runtime requires a Getblock.net account' USING ERRCODE='23514';
    END IF;
    IF TG_TABLE_NAME<>'sys_crypto_aml_account_policies' AND EXISTS(SELECT 1 FROM cat_integration_accounts WHERE id=NEW.account_id AND provider='testnet_aml') THEN RAISE EXCEPTION 'emulated AML cannot consume paid credits or admissions' USING ERRCODE='23514'; END IF;
    IF TG_TABLE_NAME='sys_crypto_aml_account_policies' AND TG_OP='UPDATE' THEN
        IF NEW.account_id<>OLD.account_id OR NEW.version<>OLD.version+1 THEN
            RAISE EXCEPTION 'AML account policy requires a new version' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE OR REPLACE FUNCTION fn_crypto_aml_routing_scope_guard() RETURNS TRIGGER AS $$
DECLARE executing BOOLEAN;
BEGIN
    IF TG_OP='DELETE' THEN RAISE EXCEPTION 'AML routing scope cannot be deleted' USING ERRCODE='23514'; END IF;
    IF TG_OP='UPDATE' THEN
        IF (NEW.merchant_id,NEW.network_id,NEW.token_id) IS DISTINCT FROM (OLD.merchant_id,OLD.network_id,OLD.token_id)
           OR NEW.version<>OLD.version+1 OR (OLD.enabled AND NOT NEW.enabled) THEN
            RAISE EXCEPTION 'AML routing scope identity/version cannot be downgraded' USING ERRCODE='23514';
        END IF;
    END IF;
    IF NOT EXISTS(SELECT 1 FROM cat_integration_accounts WHERE id=NEW.provider_account_id AND provider IN ('getblock_net','testnet_aml')) THEN
        RAISE EXCEPTION 'AML routing requires a Getblock.net account' USING ERRCODE='23514';
    END IF;
    IF EXISTS(SELECT 1 FROM cat_integration_accounts WHERE id=NEW.provider_account_id AND provider='testnet_aml') THEN
      IF NOT EXISTS(SELECT 1 FROM sys_crypto_aml_testnet_emulators e JOIN cat_blockchain_networks n ON n.id=e.network_id
          JOIN sys_crypto_aml_provider_mappings m ON m.network_id=e.network_id AND m.token_id=NEW.token_id AND m.version=NEW.mapping_version
          WHERE e.account_id=NEW.provider_account_id AND e.network_id=NEW.network_id AND fn_crypto_aml_is_emulation_network(n.chain_family,n.network_identifier)
            AND m.contract_version='aml-testnet.v1' AND m.currencylist_snapshot->'emulated'='true'::jsonb) THEN
        RAISE EXCEPTION 'emulated AML scope requires its pinned Nile/Sepolia binding and synthetic mapping' USING ERRCODE='23514';
      END IF;
    ELSIF EXISTS(SELECT 1 FROM sys_crypto_aml_provider_mappings WHERE network_id=NEW.network_id AND token_id=NEW.token_id AND version=NEW.mapping_version AND contract_version='aml-testnet.v1') THEN
      RAISE EXCEPTION 'synthetic AML mapping cannot be used by a live provider' USING ERRCODE='23514';
    END IF;
    IF NEW.enabled THEN
        SELECT enabled INTO executing FROM sys_crypto_network_policies WHERE network_id=NEW.network_id FOR UPDATE;
        IF executing THEN RAISE EXCEPTION 'pause network before changing enabled AML routing' USING ERRCODE='23514'; END IF;
        IF NOT EXISTS(SELECT 1 FROM sys_crypto_custody_policies WHERE network_id=NEW.network_id AND enforced) OR
           NOT EXISTS(SELECT 1 FROM sys_crypto_aml_provider_mappings mapping JOIN cat_tokens token ON token.id=mapping.token_id AND token.network_id=mapping.network_id
                      JOIN cat_blockchain_networks network ON network.id=mapping.network_id
                      WHERE mapping.network_id=NEW.network_id AND mapping.token_id=NEW.token_id AND mapping.version=NEW.mapping_version AND mapping.capability_accepted
                      AND mapping.decimal_places=token.decimal_places AND mapping.native=(token.token_standard='native')
                      AND CASE WHEN network.chain_family='evm' THEN LOWER(mapping.contract_address)=LOWER(COALESCE(token.contract_address,'')) ELSE mapping.contract_address=COALESCE(token.contract_address,'') END) THEN
            RAISE EXCEPTION 'AML routing requires merchant custody and accepted mapping' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DO $$ BEGIN IF EXISTS(SELECT 1 FROM sys_crypto_aml_testnet_emulators) OR EXISTS(SELECT 1 FROM cat_integration_accounts WHERE provider='testnet_aml') OR EXISTS(SELECT 1 FROM cat_integration_pools WHERE provider='testnet_aml') OR EXISTS(SELECT 1 FROM reg_crypto_aml_jobs WHERE provider='testnet_aml') THEN RAISE EXCEPTION 'cannot discard AML emulation history; use forward migration'; END IF; END $$;
CREATE OR REPLACE FUNCTION fn_crypto_aml_runtime_account_guard() RETURNS TRIGGER AS $$
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
CREATE OR REPLACE FUNCTION fn_crypto_aml_routing_scope_guard() RETURNS TRIGGER AS $$
DECLARE executing BOOLEAN;
BEGIN
    IF TG_OP='DELETE' THEN RAISE EXCEPTION 'AML routing scope cannot be deleted' USING ERRCODE='23514'; END IF;
    IF TG_OP='UPDATE' THEN
        IF (NEW.merchant_id,NEW.network_id,NEW.token_id) IS DISTINCT FROM (OLD.merchant_id,OLD.network_id,OLD.token_id)
           OR NEW.version<>OLD.version+1 OR (OLD.enabled AND NOT NEW.enabled) THEN
            RAISE EXCEPTION 'AML routing scope identity/version cannot be downgraded' USING ERRCODE='23514';
        END IF;
    END IF;
    IF NOT EXISTS(SELECT 1 FROM cat_integration_accounts WHERE id=NEW.provider_account_id AND provider='getblock_net') THEN
        RAISE EXCEPTION 'AML routing requires a Getblock.net account' USING ERRCODE='23514';
    END IF;
    IF NEW.enabled THEN
        SELECT enabled INTO executing FROM sys_crypto_network_policies WHERE network_id=NEW.network_id FOR UPDATE;
        IF executing THEN RAISE EXCEPTION 'pause network before changing enabled AML routing' USING ERRCODE='23514'; END IF;
        IF NOT EXISTS(SELECT 1 FROM sys_crypto_custody_policies WHERE network_id=NEW.network_id AND enforced) OR
           NOT EXISTS(SELECT 1 FROM sys_crypto_aml_provider_mappings mapping JOIN cat_tokens token ON token.id=mapping.token_id AND token.network_id=mapping.network_id
                      JOIN cat_blockchain_networks network ON network.id=mapping.network_id
                      WHERE mapping.network_id=NEW.network_id AND mapping.token_id=NEW.token_id AND mapping.version=NEW.mapping_version AND mapping.capability_accepted
                      AND mapping.decimal_places=token.decimal_places AND mapping.native=(token.token_standard='native')
                      AND CASE WHEN network.chain_family='evm' THEN LOWER(mapping.contract_address)=LOWER(COALESCE(token.contract_address,'')) ELSE mapping.contract_address=COALESCE(token.contract_address,'') END) THEN
            RAISE EXCEPTION 'AML routing requires merchant custody and accepted mapping' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER trg_crypto_aml_report_emulation ON reg_crypto_aml_reports;
DROP TRIGGER trg_crypto_aml_check_provider ON reg_crypto_aml_provider_checks;
DROP FUNCTION fn_crypto_aml_check_provider_guard();
DROP TRIGGER trg_crypto_aml_job_provider ON reg_crypto_aml_jobs;
DROP TRIGGER trg_crypto_aml_testnet_mapping ON sys_crypto_aml_provider_mappings;
DROP TRIGGER trg_crypto_aml_testnet_network ON cat_blockchain_networks;
DROP TRIGGER trg_crypto_aml_testnet_account ON cat_integration_accounts;
DROP FUNCTION fn_crypto_aml_report_emulation_guard();
DROP FUNCTION fn_crypto_aml_job_provider_guard();
DROP FUNCTION fn_crypto_aml_testnet_mapping_guard();
DROP FUNCTION fn_crypto_aml_testnet_identity_guard();
DROP TABLE sys_crypto_aml_testnet_emulators;
DROP FUNCTION fn_crypto_aml_testnet_binding_guard();
DROP FUNCTION fn_crypto_aml_is_emulation_network(TEXT,TEXT);
ALTER TABLE cat_integration_accounts DROP CONSTRAINT cat_integration_accounts_provider_check;
ALTER TABLE cat_integration_accounts ADD CONSTRAINT cat_integration_accounts_provider_check CHECK(provider IN ('alchemy','coingecko_demo','tron_api','chainstack','getblock','infura','drpc','telegram','smtp','resource_api','getblock_net'));
ALTER TABLE cat_integration_pools DROP CONSTRAINT cat_integration_pools_provider_check;
ALTER TABLE cat_integration_pools ADD CONSTRAINT cat_integration_pools_provider_check CHECK(provider IN ('alchemy','coingecko_demo','tron_api','chainstack','getblock','infura','drpc','telegram','smtp','resource_api','getblock_net'));
ALTER TABLE reg_crypto_aml_jobs DROP CONSTRAINT reg_crypto_aml_jobs_provider_check;
ALTER TABLE reg_crypto_aml_jobs ADD CONSTRAINT reg_crypto_aml_jobs_provider_check CHECK(provider='getblock_net');
ALTER TABLE reg_crypto_aml_provider_checks DROP CONSTRAINT reg_crypto_aml_provider_checks_provider_check;
ALTER TABLE reg_crypto_aml_provider_checks ADD CONSTRAINT reg_crypto_aml_provider_checks_provider_check CHECK(provider='getblock_net');
-- +goose StatementEnd
