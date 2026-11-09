-- +goose Up
-- The incident identifies a chain event; each posted generation has its own effect.
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

CREATE TABLE reg_crypto_reorg_compensations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    incident_id UUID NOT NULL REFERENCES reg_crypto_security_incidents(id),
    payment_id UUID NOT NULL REFERENCES doc_crypto_payments(id),
    posted_version INT NOT NULL CHECK (posted_version > 0),
    observed_block_number BIGINT NOT NULL,
    observed_block_hash TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_crypto_reorg_compensation_generation UNIQUE (payment_id, posted_version)
);
CREATE INDEX idx_crypto_reorg_compensations_incident
    ON reg_crypto_reorg_compensations(incident_id, posted_version);

-- Old reorg movements used the incident ID as recorder ID. Preserve that identity
-- so a retry after rollout cannot compensate the same posting twice.
INSERT INTO reg_crypto_reorg_compensations
    (id, incident_id, payment_id, posted_version, observed_block_number, observed_block_hash)
SELECT id, id, payment_id, (details->>'postedVersion')::INT,
       COALESCE((details->>'blockNumber')::BIGINT, 0),
       COALESCE(details->>'observedBlockHash', '')
FROM reg_crypto_security_incidents
WHERE incident_kind = 'posted_payment_reorg'
  AND payment_id IS NOT NULL
  AND details->>'postedVersion' ~ '^[1-9][0-9]*$';

ALTER TABLE reg_crypto_balance_movements
    ADD COLUMN reversal_of_line_id UUID REFERENCES reg_crypto_balance_movements(line_id);
CREATE UNIQUE INDEX uq_crypto_balance_reversal_line
    ON reg_crypto_balance_movements(reversal_of_line_id)
    WHERE reversal_of_line_id IS NOT NULL;

ALTER TABLE reg_crypto_merchant_balance_movements
    ADD COLUMN reversal_of_line_id UUID REFERENCES reg_crypto_merchant_balance_movements(line_id);
CREATE UNIQUE INDEX uq_crypto_merchant_reversal_line
    ON reg_crypto_merchant_balance_movements(reversal_of_line_id)
    WHERE reversal_of_line_id IS NOT NULL;

ALTER TABLE reg_crypto_fee_movements
    ADD COLUMN reversal_of_line_id UUID REFERENCES reg_crypto_fee_movements(line_id);
CREATE UNIQUE INDEX uq_crypto_fee_reversal_line
    ON reg_crypto_fee_movements(reversal_of_line_id)
    WHERE reversal_of_line_id IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP INDEX uq_crypto_fee_reversal_line;
ALTER TABLE reg_crypto_fee_movements DROP COLUMN reversal_of_line_id;
DROP INDEX uq_crypto_merchant_reversal_line;
ALTER TABLE reg_crypto_merchant_balance_movements DROP COLUMN reversal_of_line_id;
DROP INDEX uq_crypto_balance_reversal_line;
ALTER TABLE reg_crypto_balance_movements DROP COLUMN reversal_of_line_id;
DROP TABLE reg_crypto_reorg_compensations;
-- +goose StatementEnd
