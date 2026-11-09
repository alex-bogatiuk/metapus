-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE reg_crypto_account_activations ADD COLUMN cost_claim_token UUID;
ALTER TABLE reg_crypto_account_activations ADD CONSTRAINT chk_crypto_activation_cost_claim
 CHECK (cost_claim_token IS NULL OR (cost_status='repairing' AND cost_claimed_by<>'' AND cost_claimed_at IS NOT NULL AND cost_lease_expires_at IS NOT NULL));
COMMENT ON COLUMN reg_crypto_account_activations.cost_claim_token IS 'Fresh fencing token per cost-repair attempt; legacy repairing leases drain without rewriting history.';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE reg_crypto_account_activations DROP CONSTRAINT chk_crypto_activation_cost_claim;
ALTER TABLE reg_crypto_account_activations DROP COLUMN cost_claim_token;
-- +goose StatementEnd
