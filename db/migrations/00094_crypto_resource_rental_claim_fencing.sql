-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE doc_crypto_resource_rentals ADD COLUMN maintenance_claim_token UUID;
ALTER TABLE doc_crypto_resource_rentals ADD CONSTRAINT chk_crypto_resource_rental_claim_token
 CHECK (maintenance_claim_token IS NULL OR (claimed_by<>'' AND claimed_at IS NOT NULL AND lease_expires_at IS NOT NULL));
COMMENT ON COLUMN doc_crypto_resource_rentals.maintenance_claim_token IS 'Fresh fencing token per maintenance claim; legacy leases drain before a new token is issued.';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE doc_crypto_resource_rentals DROP CONSTRAINT chk_crypto_resource_rental_claim_token;
ALTER TABLE doc_crypto_resource_rentals DROP COLUMN maintenance_claim_token;
-- +goose StatementEnd
