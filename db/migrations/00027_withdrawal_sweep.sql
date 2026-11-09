-- +goose Up
-- Description: CryptoWithdrawal + CryptoSweep documents

-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- ═══════════════════════════════════════════════════════════════════════
-- CRYPTO WITHDRAWAL DOCUMENT
-- ═══════════════════════════════════════════════════════════════════════

CREATE TABLE doc_crypto_withdrawals (
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
    amount            NUMERIC(78,0) NOT NULL DEFAULT 0,
    network_fee       NUMERIC(78,0) NOT NULL DEFAULT 0,
    network_fee_token_id UUID REFERENCES cat_tokens(id),
    tx_hash           TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'created' CHECK (status IN ('created','signed','broadcast','confirmed','failed')),
    settlement_status TEXT NOT NULL DEFAULT 'unknown',
    settlement_checked_at TIMESTAMPTZ,
    settlement_error  TEXT NOT NULL DEFAULT '',
    settlement_claimed_by TEXT NOT NULL DEFAULT '',
    settlement_claimed_at TIMESTAMPTZ,
    settlement_lease_expires_at TIMESTAMPTZ,
    chain_result      TEXT NOT NULL DEFAULT '',
    energy_used       BIGINT NOT NULL DEFAULT 0,
    net_fee           NUMERIC(78,0) NOT NULL DEFAULT 0,
    energy_fee        NUMERIC(78,0) NOT NULL DEFAULT 0,
    net_usage         BIGINT NOT NULL DEFAULT 0,

    _txid             BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at       TIMESTAMPTZ,

    CONSTRAINT doc_crypto_withdrawals_settlement_status_check
        CHECK (settlement_status IN ('unknown','repairing','recorded','not_applicable','held'))
);

CREATE INDEX idx_doc_crypto_withdrawals_merchant ON doc_crypto_withdrawals (merchant_id);
CREATE INDEX idx_doc_crypto_withdrawals_status ON doc_crypto_withdrawals (status) WHERE status NOT IN ('confirmed','failed');
CREATE INDEX idx_doc_crypto_withdrawals_date ON doc_crypto_withdrawals (date DESC);
CREATE UNIQUE INDEX idx_crypto_withdrawals_request_basis
    ON doc_crypto_withdrawals (basis_type, basis_id)
    WHERE basis_type = 'WithdrawalRequest'
      AND basis_id IS NOT NULL
      AND deletion_mark = FALSE;
CREATE INDEX idx_crypto_withdrawals_settlement_repair
    ON doc_crypto_withdrawals (settlement_status, settlement_lease_expires_at, updated_at)
    WHERE tx_hash <> '' AND status IN ('broadcast','confirmed','failed')
      AND settlement_status IN ('unknown','repairing','held');

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

CREATE TABLE doc_crypto_sweeps (
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
    total_amount      NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_fee         NUMERIC(78,0) NOT NULL DEFAULT 0,
    status            TEXT NOT NULL DEFAULT 'created' CHECK (status IN ('created','signed','broadcast','confirmed','partial_failed','held')),

    _txid             BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at       TIMESTAMPTZ,

    CONSTRAINT doc_crypto_sweeps_no_internal_attempt_basis
        CHECK (basis_type <> 'CryptoSweepAttempt')
);

CREATE INDEX idx_doc_crypto_sweeps_status ON doc_crypto_sweeps (status) WHERE status NOT IN ('confirmed','partial_failed');
CREATE INDEX idx_doc_crypto_sweeps_date ON doc_crypto_sweeps (date DESC);

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
CREATE TABLE doc_crypto_sweep_lines (
    line_id       UUID PRIMARY KEY,
    document_id   UUID NOT NULL REFERENCES doc_crypto_sweeps(id) ON DELETE CASCADE,
    line_no       INT NOT NULL DEFAULT 0,
    wallet_id     UUID NOT NULL,
    amount        NUMERIC(78,0) NOT NULL DEFAULT 0,
    network_fee   NUMERIC(78,0) NOT NULL DEFAULT 0,
    network_fee_token_id UUID REFERENCES cat_tokens(id),
    tx_hash       TEXT NOT NULL DEFAULT '',
    confirmed     BOOLEAN NOT NULL DEFAULT FALSE,
    settlement_status TEXT NOT NULL DEFAULT 'unknown',
    settlement_checked_at TIMESTAMPTZ,
    settlement_error TEXT NOT NULL DEFAULT '',
    settlement_claimed_by TEXT NOT NULL DEFAULT '',
    settlement_claimed_at TIMESTAMPTZ,
    settlement_lease_expires_at TIMESTAMPTZ,
    chain_result TEXT NOT NULL DEFAULT '',
    energy_used BIGINT NOT NULL DEFAULT 0,
    net_fee NUMERIC(78,0) NOT NULL DEFAULT 0,
    energy_fee NUMERIC(78,0) NOT NULL DEFAULT 0,
    net_usage BIGINT NOT NULL DEFAULT 0,

    CONSTRAINT doc_crypto_sweep_lines_settlement_status_check
        CHECK (settlement_status IN ('unknown','repairing','recorded','not_applicable','held'))
);

CREATE INDEX idx_doc_crypto_sweep_lines_doc ON doc_crypto_sweep_lines (document_id);
CREATE INDEX idx_crypto_sweep_lines_settlement_repair
    ON doc_crypto_sweep_lines (settlement_status, settlement_lease_expires_at, line_id)
    WHERE tx_hash <> '' AND settlement_status IN ('unknown','repairing','held');


-- ═══════════════════════════════════════════════════════════════════════
-- WITHDRAWAL ADDRESS WHITELIST (portal self-service)
-- ═══════════════════════════════════════════════════════════════════════

CREATE TABLE reg_withdrawal_addresses (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    merchant_id UUID NOT NULL REFERENCES cat_merchants(id),
    network_id  UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    address     TEXT NOT NULL CHECK (length(address) >= 10),
    address_key TEXT NOT NULL CHECK (BTRIM(address_key) <> ''),
    label       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _deleted_at TIMESTAMPTZ,
    UNIQUE(merchant_id, network_id, address_key)
);

CREATE INDEX idx_reg_withdrawal_addresses_merchant ON reg_withdrawal_addresses (merchant_id);

CREATE OR REPLACE FUNCTION fn_reject_managed_withdrawal_destination() RETURNS TRIGGER AS $$
BEGIN
    IF NEW._deleted_at IS NOT NULL THEN
        RETURN NEW;
    END IF;

    IF EXISTS (
        SELECT 1
        FROM cat_wallets w
        JOIN cat_blockchain_networks n ON n.id = w.network_id
        WHERE w.network_id = NEW.network_id
          AND w.deletion_mark = FALSE
          AND w.address_key = NEW.address_key
    ) THEN
        RAISE EXCEPTION 'withdrawal destination must be external to managed custody'
            USING ERRCODE = '23514',
                  CONSTRAINT = 'chk_withdrawal_address_external_destination';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_reject_managed_withdrawal_destination
    BEFORE INSERT OR UPDATE OF network_id, address, address_key, _deleted_at
    ON reg_withdrawal_addresses
    FOR EACH ROW EXECUTE FUNCTION fn_reject_managed_withdrawal_destination();


-- ═══════════════════════════════════════════════════════════════════════
-- WITHDRAWAL REQUEST (portal → ERP approval → Vault signing)
-- ═══════════════════════════════════════════════════════════════════════

CREATE TABLE doc_withdrawal_requests (
    id               UUID PRIMARY KEY,
    deletion_mark    BOOLEAN NOT NULL DEFAULT FALSE,
    version          INT NOT NULL DEFAULT 1,
    attributes       JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by       UUID,
    updated_by       UUID,
    number           TEXT NOT NULL DEFAULT '',
    date             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    posted           BOOLEAN NOT NULL DEFAULT FALSE,
    posted_version   INT NOT NULL DEFAULT 0,
    basis_type       TEXT NOT NULL DEFAULT '',
    basis_id         UUID,
    description      TEXT NOT NULL DEFAULT '',
    merchant_id      UUID NOT NULL REFERENCES cat_merchants(id),
    token_id         UUID NOT NULL REFERENCES cat_tokens(id),
    amount           NUMERIC(78,0) NOT NULL CHECK (amount > 0),
    dest_address     TEXT NOT NULL,
    address_id       UUID REFERENCES reg_withdrawal_addresses(id),
    status           TEXT NOT NULL DEFAULT 'pending_approval'
                     CHECK (status IN ('pending_approval','approved','signing','broadcast','confirmed','held','rejected','failed')),
    approved_by      UUID,
    approved_at      TIMESTAMPTZ,
    rejection_reason TEXT NOT NULL DEFAULT '',
	 recovery_reason TEXT NOT NULL DEFAULT '',
	 recovery_started_at TIMESTAMPTZ,
    withdrawal_id    UUID REFERENCES doc_crypto_withdrawals(id),
    _deleted_at      TIMESTAMPTZ,

    _txid            BIGINT NOT NULL DEFAULT txid_current()
);

CREATE INDEX idx_doc_withdrawal_requests_merchant ON doc_withdrawal_requests (merchant_id);
CREATE INDEX idx_doc_withdrawal_requests_status ON doc_withdrawal_requests (status)
    WHERE status NOT IN ('confirmed','rejected','failed');
CREATE INDEX idx_doc_withdrawal_requests_basis ON doc_withdrawal_requests (basis_type, basis_id)
    WHERE basis_id IS NOT NULL;

-- CDC trigger
CREATE OR REPLACE FUNCTION fn_doc_withdrawal_requests_cdc() RETURNS TRIGGER AS $$
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

CREATE TRIGGER trg_doc_withdrawal_requests_cdc
    BEFORE UPDATE ON doc_withdrawal_requests
    FOR EACH ROW EXECUTE FUNCTION fn_doc_withdrawal_requests_cdc();

INSERT INTO auth_permissions (code, name, description, resource, action) VALUES
    ('document:withdrawal_request:read', 'Чтение заявок на вывод', 'View withdrawal requests', 'withdrawal_request', 'read'),
    ('document:withdrawal_request:post', 'Утверждение заявок на вывод', 'Approve withdrawal requests', 'withdrawal_request', 'post')
ON CONFLICT (code) DO NOTHING;

INSERT INTO auth_role_permissions (role_id, permission_id)
SELECT 'b0000000-0000-0000-0000-000000000001', id
FROM auth_permissions WHERE resource = 'withdrawal_request'
ON CONFLICT DO NOTHING;


-- +goose StatementEnd

-- +goose Down
DELETE FROM auth_role_permissions WHERE permission_id IN (
    SELECT id FROM auth_permissions WHERE resource = 'withdrawal_request'
);
DELETE FROM auth_permissions WHERE resource = 'withdrawal_request';

DROP TABLE IF EXISTS doc_withdrawal_requests CASCADE;
DROP TABLE IF EXISTS reg_withdrawal_addresses CASCADE;
DROP FUNCTION IF EXISTS fn_reject_managed_withdrawal_destination();
DROP TABLE IF EXISTS doc_crypto_sweep_lines CASCADE;
DROP TABLE IF EXISTS doc_crypto_sweeps CASCADE;
DROP TABLE IF EXISTS doc_crypto_withdrawals CASCADE;
