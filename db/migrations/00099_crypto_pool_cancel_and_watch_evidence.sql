-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- An intent has one immutable cancellation command, including its first actor.
-- Compare before ON CONFLICT can hide a different command or skip constraints.
CREATE OR REPLACE FUNCTION fn_crypto_aml_pool_cancel_guard() RETURNS TRIGGER AS $$
DECLARE
 network UUID;
 prior reg_crypto_aml_pool_intent_cancellations%ROWTYPE;
BEGIN
 IF current_setting('transaction_isolation')<>'read committed' THEN
  RAISE EXCEPTION 'pool cancellation requires read committed isolation' USING ERRCODE='23514';
 END IF;
 IF NEW.intent_id IS NULL OR NEW.reason IS NULL OR length(btrim(NEW.reason)) NOT BETWEEN 3 AND 500
    OR NEW.cancelled_by IS NULL OR btrim(NEW.cancelled_by)='' OR NEW._deleted_at IS NOT NULL THEN
  RAISE EXCEPTION 'pool cancellation requires intent, reason and actor' USING ERRCODE='23514';
 END IF;
 SELECT p.network_id INTO network FROM reg_crypto_aml_pool_intents i JOIN sys_crypto_aml_pool_policies p ON p.id=i.policy_id WHERE i.id=NEW.intent_id;
 IF NOT FOUND THEN RAISE EXCEPTION 'pool cancellation intent is missing' USING ERRCODE='23503'; END IF;
 PERFORM 1 FROM sys_crypto_custody_policies WHERE network_id=network FOR UPDATE;
 PERFORM 1 FROM sys_crypto_aml_routing_scopes WHERE network_id=network ORDER BY merchant_id,network_id,token_id FOR UPDATE;
 PERFORM 1 FROM reg_crypto_aml_pool_intents WHERE id=NEW.intent_id FOR UPDATE;
 SELECT * INTO prior FROM reg_crypto_aml_pool_intent_cancellations WHERE intent_id=NEW.intent_id;
 IF FOUND THEN
  IF prior.reason IS DISTINCT FROM NEW.reason OR prior.cancelled_by IS DISTINCT FROM NEW.cancelled_by THEN
   RAISE EXCEPTION 'pool cancellation replay differs from its original command' USING ERRCODE='23514';
  END IF;
  RETURN NULL;
 END IF;
 IF EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_provisions WHERE id=NEW.intent_id) THEN
  RAISE EXCEPTION 'cannot cancel pool intent after derivation reservation' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Coverage is a first observation, retained across later address refreshes and
-- watcher leases. Check authority after any lock wait using the database clock.
CREATE OR REPLACE FUNCTION fn_crypto_aml_watch_coverage_guard() RETURNS TRIGGER AS $$
BEGIN
 PERFORM 1 FROM sys_chain_watcher_state state JOIN cat_wallets w ON w.id=NEW.wallet_id AND w.network_id=state.network_id
 JOIN cat_tokens t ON t.id=NEW.token_id AND t.network_id=w.network_id JOIN cat_blockchain_networks n ON n.id=w.network_id AND n.chain_family='evm'
 WHERE state.network_id=NEW.network_id AND state.watcher_key=NEW.watcher_key
 AND NEW.watcher_key=CASE WHEN t.token_standard='native' THEN 'native_eth' ELSE 'erc20:'||LOWER(t.contract_address) END FOR SHARE OF state;
 IF NOT FOUND THEN RAISE EXCEPTION 'address coverage watcher or asset changed' USING ERRCODE='23514'; END IF;
 IF NOT EXISTS(SELECT 1 FROM sys_chain_watcher_state WHERE network_id=NEW.network_id AND watcher_key=NEW.watcher_key
   AND lease_owner=NEW.lease_owner AND fencing_token=NEW.fencing_token AND lease_expires_at>clock_timestamp()) THEN
  RAISE EXCEPTION 'address coverage requires the current watcher lease' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
-- No evidence is transformed by this migration in either direction.
CREATE OR REPLACE FUNCTION fn_crypto_aml_pool_cancel_guard() RETURNS TRIGGER AS $$
BEGIN
 PERFORM 1 FROM reg_crypto_aml_pool_intents i JOIN sys_crypto_aml_pool_policies p ON p.id=i.policy_id JOIN sys_crypto_custody_policies c ON c.network_id=p.network_id WHERE i.id=NEW.intent_id FOR UPDATE OF c;
 PERFORM 1 FROM reg_crypto_aml_pool_intents WHERE id=NEW.intent_id FOR UPDATE;
 IF EXISTS(SELECT 1 FROM reg_crypto_aml_wallet_provisions WHERE id=NEW.intent_id) THEN RAISE EXCEPTION 'cannot cancel pool intent after derivation reservation' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE OR REPLACE FUNCTION fn_crypto_aml_watch_coverage_guard() RETURNS TRIGGER AS $$
BEGIN
 PERFORM 1 FROM sys_chain_watcher_state state JOIN cat_wallets w ON w.id=NEW.wallet_id AND w.network_id=state.network_id
 JOIN cat_tokens t ON t.id=NEW.token_id AND t.network_id=w.network_id JOIN cat_blockchain_networks n ON n.id=w.network_id AND n.chain_family='evm'
 WHERE state.network_id=NEW.network_id AND state.watcher_key=NEW.watcher_key AND state.lease_owner=NEW.lease_owner AND state.fencing_token=NEW.fencing_token AND state.lease_expires_at>NOW()
 AND NEW.watcher_key=CASE WHEN t.token_standard='native' THEN 'native_eth' ELSE 'erc20:'||LOWER(t.contract_address) END FOR SHARE OF state;
 IF NOT FOUND THEN RAISE EXCEPTION 'address coverage requires the current watcher lease' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
