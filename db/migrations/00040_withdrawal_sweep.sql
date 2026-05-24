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
    amount            BIGINT NOT NULL DEFAULT 0,
    network_fee       BIGINT NOT NULL DEFAULT 0,
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


-- ═══════════════════════════════════════════════════════════════════════
-- CRYPTO SWEEP DOCUMENT
-- ═══════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS doc_crypto_sweeps (
    id                UUID PRIMARY KEY,
    number            TEXT NOT NULL DEFAULT '',
    date              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
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
    total_amount      BIGINT NOT NULL DEFAULT 0,
    total_fee         BIGINT NOT NULL DEFAULT 0,
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
    amount        BIGINT NOT NULL DEFAULT 0,
    network_fee   BIGINT NOT NULL DEFAULT 0,
    tx_hash       TEXT NOT NULL DEFAULT '',
    confirmed     BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS idx_doc_crypto_sweep_lines_doc ON doc_crypto_sweep_lines (document_id);


-- ═══════════════════════════════════════════════════════════════════════
-- WITHDRAWAL ADDRESS WHITELIST (portal self-service)
-- ═══════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS reg_withdrawal_addresses (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id UUID NOT NULL REFERENCES cat_merchants(id),
    network_id  UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    address     TEXT NOT NULL CHECK (length(address) >= 10),
    label       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _deleted_at TIMESTAMPTZ,
    UNIQUE(merchant_id, network_id, address)
);

CREATE INDEX IF NOT EXISTS idx_reg_withdrawal_addresses_merchant ON reg_withdrawal_addresses (merchant_id);


-- ═══════════════════════════════════════════════════════════════════════
-- WITHDRAWAL REQUEST (portal → ERP approval → Vault signing)
-- ═══════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS doc_withdrawal_requests (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    number           TEXT NOT NULL DEFAULT '',
    date             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    merchant_id      UUID NOT NULL REFERENCES cat_merchants(id),
    token_id         UUID NOT NULL REFERENCES cat_tokens(id),
    amount           BIGINT NOT NULL CHECK (amount > 0),
    dest_address     TEXT NOT NULL,
    address_id       UUID REFERENCES reg_withdrawal_addresses(id),
    status           TEXT NOT NULL DEFAULT 'pending_approval'
                     CHECK (status IN ('pending_approval','approved','signing','broadcast','confirmed','rejected','failed')),
    posted           BOOLEAN NOT NULL DEFAULT FALSE,
    posted_version   INT NOT NULL DEFAULT 0,
    approved_by      UUID,
    approved_at      TIMESTAMPTZ,
    rejection_reason TEXT NOT NULL DEFAULT '',
    withdrawal_id    UUID REFERENCES doc_crypto_withdrawals(id),
    version          INT NOT NULL DEFAULT 1,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _deleted_at      TIMESTAMPTZ,

    _txid            BIGINT NOT NULL DEFAULT txid_current()
);

CREATE INDEX IF NOT EXISTS idx_doc_withdrawal_requests_merchant ON doc_withdrawal_requests (merchant_id);
CREATE INDEX IF NOT EXISTS idx_doc_withdrawal_requests_status ON doc_withdrawal_requests (status)
    WHERE status NOT IN ('confirmed','rejected','failed');

-- CDC trigger
CREATE OR REPLACE FUNCTION fn_doc_withdrawal_requests_cdc() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        NEW._txid := txid_current();
        NEW.updated_at := NOW();
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_doc_withdrawal_requests_cdc
    BEFORE UPDATE ON doc_withdrawal_requests
    FOR EACH ROW EXECUTE FUNCTION fn_doc_withdrawal_requests_cdc();


SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS doc_withdrawal_requests CASCADE;
DROP TABLE IF EXISTS reg_withdrawal_addresses CASCADE;
DROP TABLE IF EXISTS doc_crypto_sweep_lines CASCADE;
DROP TABLE IF EXISTS doc_crypto_sweeps CASCADE;
DROP TABLE IF EXISTS doc_crypto_withdrawals CASCADE;
