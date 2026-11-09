-- +goose Up
-- Due-time scheduling prevents old pending receipts from monopolizing a batch.
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
ALTER TABLE doc_crypto_payments
    ADD COLUMN next_confirmation_check_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN confirmation_check_attempts INT NOT NULL DEFAULT 0
        CHECK (confirmation_check_attempts >= 0),
    ADD COLUMN last_confirmation_checked_at TIMESTAMPTZ;
CREATE INDEX idx_crypto_payments_confirmation_due
    ON doc_crypto_payments(next_confirmation_check_at, created_at, id)
    WHERE status IN ('detected','confirming') AND deletion_mark = FALSE;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP INDEX idx_crypto_payments_confirmation_due;
ALTER TABLE doc_crypto_payments
    DROP COLUMN last_confirmation_checked_at,
    DROP COLUMN confirmation_check_attempts,
    DROP COLUMN next_confirmation_check_at;
-- +goose StatementEnd
