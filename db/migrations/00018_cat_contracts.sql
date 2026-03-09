-- +goose Up
-- Description: Contract catalog (Справочник "Договоры контрагентов")

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

CREATE TABLE cat_contracts (
    -- Base catalog fields
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),

    deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
    version INT NOT NULL DEFAULT 1,
    attributes JSONB DEFAULT '{}',

    -- CDC-ready columns
    _deleted_at TIMESTAMPTZ,
    _txid BIGINT DEFAULT txid_current(),

    -- Audit fields
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Catalog fields
    code VARCHAR(20) NOT NULL,
    name VARCHAR(255) NOT NULL,
    parent_id UUID,
    is_folder BOOLEAN NOT NULL DEFAULT FALSE,

    -- Contract specific fields
    counterparty_id UUID NOT NULL REFERENCES cat_counterparties(id),
    type VARCHAR(20) NOT NULL DEFAULT 'supply',
    currency_id UUID REFERENCES cat_currencies(id),
    valid_from DATE,
    valid_to DATE,
    payment_term_days INT NOT NULL DEFAULT 0,
    description TEXT,

    -- Constraints
    CONSTRAINT chk_contract_type CHECK (type IN ('supply', 'sale', 'other')),
    CONSTRAINT chk_payment_term_days CHECK (payment_term_days >= 0),
    CONSTRAINT chk_valid_dates CHECK (valid_to IS NULL OR valid_from IS NULL OR valid_to >= valid_from)
);

-- Indexes
CREATE UNIQUE INDEX uq_cat_contracts_code ON cat_contracts (code) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_contracts_counterparty ON cat_contracts (counterparty_id);
CREATE INDEX idx_cat_contracts_type ON cat_contracts (type);
CREATE INDEX idx_cat_contracts_currency ON cat_contracts (currency_id) WHERE currency_id IS NOT NULL;
CREATE INDEX idx_cat_contracts_parent ON cat_contracts (parent_id) WHERE parent_id IS NOT NULL;

-- CDC index & triggers
CREATE INDEX IF NOT EXISTS idx_cat_contracts_txid
    ON cat_contracts (_txid) WHERE _deleted_at IS NULL;

CREATE TRIGGER trg_cat_contracts_txid
    BEFORE UPDATE ON cat_contracts
    FOR EACH ROW
    EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_cat_contracts_soft_delete
    BEFORE UPDATE OF deletion_mark ON cat_contracts
    FOR EACH ROW
    EXECUTE FUNCTION soft_delete_with_timestamp();

COMMENT ON TABLE cat_contracts IS 'Справочник Договоры контрагентов';

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

DROP TRIGGER IF EXISTS trg_cat_contracts_soft_delete ON cat_contracts;
DROP TRIGGER IF EXISTS trg_cat_contracts_txid ON cat_contracts;
DROP TABLE IF EXISTS cat_contracts;

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
