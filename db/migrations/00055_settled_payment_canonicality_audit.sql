-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE doc_crypto_payments
    ADD COLUMN next_canonicality_check_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN last_canonicality_checked_at TIMESTAMPTZ,
    ADD COLUMN canonicality_check_attempts INT NOT NULL DEFAULT 0 CHECK (canonicality_check_attempts>=0),
    ADD COLUMN canonicality_last_error TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_crypto_payments_canonicality_due
    ON doc_crypto_payments(next_canonicality_check_at,id)
    WHERE status='settled' AND posted=TRUE AND deletion_mark=FALSE;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP INDEX idx_crypto_payments_canonicality_due;
ALTER TABLE doc_crypto_payments
    DROP COLUMN next_canonicality_check_at,
    DROP COLUMN last_canonicality_checked_at,
    DROP COLUMN canonicality_check_attempts,
    DROP COLUMN canonicality_last_error;
-- +goose StatementEnd
