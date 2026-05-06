-- +goose Up
-- Description: CryptoWithdrawal + CryptoSweep documents

-- +goose StatementBegin
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- ═══════════════════════════════════════════════════════════════════════
-- CRYPTO WITHDRAWAL DOCUMENT
-- ═══════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS doc_crypto_withdrawals (
    id                UUID PRIMARY KEY,
    number            TEXT NOT NULL DEFAULT '',
    date              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    organization_id   UUID NOT NULL,
    basis_type        TEXT NOT NULL DEFAULT '',
    basis_id          UUID,
    posted            BOOLEAN NOT NULL DEFAULT FALSE,
    posted_version    INT NOT NULL DEFAULT 0,
    deletion_mark     BOOLEAN NOT NULL DEFAULT FALSE,
    version           INT NOT NULL DEFAULT 1,
    attributes        JSONB DEFAULT '{}',
    description       TEXT NOT NULL DEFAULT '',
    created_by        UUID,
    updated_by        UUID,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Withdrawal-specific
    merchant_id       UUID NOT NULL,
    token_id          UUID NOT NULL,
    source_wallet_id  UUID NOT NULL,
    dest_address      TEXT NOT NULL,
    amount            NUMERIC NOT NULL DEFAULT 0,
    network_fee       NUMERIC NOT NULL DEFAULT 0,
    tx_hash           TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'created' CHECK (status IN ('created','signed','broadcast','confirmed','failed')),

    _txid             BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at       TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_doc_crypto_withdrawals_merchant ON doc_crypto_withdrawals (merchant_id);
CREATE INDEX IF NOT EXISTS idx_doc_crypto_withdrawals_status ON doc_crypto_withdrawals (status) WHERE status NOT IN ('confirmed','failed');
CREATE INDEX IF NOT EXISTS idx_doc_crypto_withdrawals_date ON doc_crypto_withdrawals (date DESC);

-- CDC trigger
CREATE OR REPLACE FUNCTION fn_doc_crypto_withdrawals_cdc() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        NEW._txid := txid_current();
        NEW.updated_at := NOW();
        IF OLD.deletion_mark = FALSE AND NEW.deletion_mark = TRUE THEN
            NEW._deleted_at := NOW();
        ELSIF OLD.deletion_mark = TRUE AND NEW.deletion_mark = FALSE THEN
            NEW._deleted_at := NULL;
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_doc_crypto_withdrawals_cdc
    BEFORE UPDATE ON doc_crypto_withdrawals
    FOR EACH ROW EXECUTE FUNCTION fn_doc_crypto_withdrawals_cdc();

-- Withdrawal lines
CREATE TABLE IF NOT EXISTS doc_crypto_withdrawal_lines (
    line_id       UUID PRIMARY KEY,
    document_id   UUID NOT NULL REFERENCES doc_crypto_withdrawals(id) ON DELETE CASCADE,
    line_no       INT NOT NULL DEFAULT 0,
    description   TEXT NOT NULL DEFAULT '',
    amount        NUMERIC NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_doc_crypto_withdrawal_lines_doc ON doc_crypto_withdrawal_lines (document_id);

-- ═══════════════════════════════════════════════════════════════════════
-- CRYPTO SWEEP DOCUMENT
-- ═══════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS doc_crypto_sweeps (
    id                UUID PRIMARY KEY,
    number            TEXT NOT NULL DEFAULT '',
    date              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    organization_id   UUID NOT NULL,
    basis_type        TEXT NOT NULL DEFAULT '',
    basis_id          UUID,
    posted            BOOLEAN NOT NULL DEFAULT FALSE,
    posted_version    INT NOT NULL DEFAULT 0,
    deletion_mark     BOOLEAN NOT NULL DEFAULT FALSE,
    version           INT NOT NULL DEFAULT 1,
    attributes        JSONB DEFAULT '{}',
    description       TEXT NOT NULL DEFAULT '',
    created_by        UUID,
    updated_by        UUID,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Sweep-specific
    token_id          UUID NOT NULL,
    hot_wallet_id     UUID NOT NULL,
    total_amount      NUMERIC NOT NULL DEFAULT 0,
    total_fee         NUMERIC NOT NULL DEFAULT 0,
    status            TEXT NOT NULL DEFAULT 'created' CHECK (status IN ('created','signed','broadcast','confirmed','partial_failed')),

    _txid             BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at       TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_doc_crypto_sweeps_status ON doc_crypto_sweeps (status) WHERE status NOT IN ('confirmed','partial_failed');
CREATE INDEX IF NOT EXISTS idx_doc_crypto_sweeps_date ON doc_crypto_sweeps (date DESC);

-- CDC trigger
CREATE OR REPLACE FUNCTION fn_doc_crypto_sweeps_cdc() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        NEW._txid := txid_current();
        NEW.updated_at := NOW();
        IF OLD.deletion_mark = FALSE AND NEW.deletion_mark = TRUE THEN
            NEW._deleted_at := NOW();
        ELSIF OLD.deletion_mark = TRUE AND NEW.deletion_mark = FALSE THEN
            NEW._deleted_at := NULL;
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_doc_crypto_sweeps_cdc
    BEFORE UPDATE ON doc_crypto_sweeps
    FOR EACH ROW EXECUTE FUNCTION fn_doc_crypto_sweeps_cdc();

-- Sweep lines (one per pool wallet)
CREATE TABLE IF NOT EXISTS doc_crypto_sweep_lines (
    line_id       UUID PRIMARY KEY,
    document_id   UUID NOT NULL REFERENCES doc_crypto_sweeps(id) ON DELETE CASCADE,
    line_no       INT NOT NULL DEFAULT 0,
    wallet_id     UUID NOT NULL,
    amount        NUMERIC NOT NULL DEFAULT 0,
    network_fee   NUMERIC NOT NULL DEFAULT 0,
    tx_hash       TEXT NOT NULL DEFAULT '',
    confirmed     BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS idx_doc_crypto_sweep_lines_doc ON doc_crypto_sweep_lines (document_id);

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS doc_crypto_sweep_lines CASCADE;
DROP TABLE IF EXISTS doc_crypto_sweeps CASCADE;
DROP TABLE IF EXISTS doc_crypto_withdrawal_lines CASCADE;
DROP TABLE IF EXISTS doc_crypto_withdrawals CASCADE;
