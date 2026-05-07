-- +goose Up
-- Description: Merchant catalog + Wallet catalog + junction table

-- +goose StatementBegin
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- ═══════════════════════════════════════════════════════════════════════
-- MERCHANT CATALOG
-- ═══════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS cat_merchants (
    -- Base entity fields
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    code            TEXT NOT NULL DEFAULT '',
    name            TEXT NOT NULL,
    parent_id       UUID REFERENCES cat_merchants(id),
    is_folder       BOOLEAN NOT NULL DEFAULT FALSE,
    deletion_mark   BOOLEAN NOT NULL DEFAULT FALSE,
    version         INT NOT NULL DEFAULT 1,
    attributes      JSONB NOT NULL DEFAULT '{}'::jsonb,

    -- Timestamps
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Merchant-specific fields
    legal_name      TEXT NOT NULL DEFAULT '',
    webhook_url     TEXT NOT NULL DEFAULT '',
    commission_rate INT NOT NULL DEFAULT 0 CHECK (commission_rate >= 0 AND commission_rate <= 10000),
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    kyb_status      TEXT NOT NULL DEFAULT 'pending' CHECK (kyb_status IN ('pending','approved','rejected')),

    -- CDC fields
    _txid           BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at     TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_cat_merchants_code ON cat_merchants (code) WHERE deletion_mark = FALSE;
CREATE INDEX IF NOT EXISTS idx_cat_merchants_name ON cat_merchants (name) WHERE deletion_mark = FALSE;
CREATE INDEX IF NOT EXISTS idx_cat_merchants_name_id ON cat_merchants (name ASC, id ASC);

-- CDC trigger for merchants
CREATE OR REPLACE FUNCTION fn_cat_merchants_cdc() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        NEW._txid := txid_current();
        IF OLD.deletion_mark = FALSE AND NEW.deletion_mark = TRUE THEN
            NEW._deleted_at := NOW();
        ELSIF OLD.deletion_mark = TRUE AND NEW.deletion_mark = FALSE THEN
            NEW._deleted_at := NULL;
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_cat_merchants_cdc
    BEFORE UPDATE ON cat_merchants
    FOR EACH ROW EXECUTE FUNCTION fn_cat_merchants_cdc();

CREATE TRIGGER trg_cat_merchants_updated_at
    BEFORE UPDATE ON cat_merchants
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ═══════════════════════════════════════════════════════════════════════
-- MERCHANT-USER JUNCTION TABLE (M:N)
-- ═══════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS sys_merchant_users (
    user_id     UUID NOT NULL,
    merchant_id UUID NOT NULL REFERENCES cat_merchants(id),
    role        INT NOT NULL DEFAULT 1 CHECK (role BETWEEN 1 AND 3),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, merchant_id)
);

CREATE INDEX IF NOT EXISTS idx_merchant_users_merchant ON sys_merchant_users (merchant_id);

-- ═══════════════════════════════════════════════════════════════════════
-- WALLET CATALOG
-- ═══════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS cat_wallets (
    -- Base entity fields
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    code            TEXT NOT NULL DEFAULT '',
    name            TEXT NOT NULL,
    parent_id       UUID REFERENCES cat_wallets(id),
    is_folder       BOOLEAN NOT NULL DEFAULT FALSE,
    deletion_mark   BOOLEAN NOT NULL DEFAULT FALSE,
    version         INT NOT NULL DEFAULT 1,
    attributes      JSONB NOT NULL DEFAULT '{}'::jsonb,

    -- Timestamps
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Wallet-specific fields
    network_id      UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    merchant_id     UUID REFERENCES cat_merchants(id),
    address         TEXT NOT NULL,
    derivation_path TEXT NOT NULL DEFAULT '',
    tier            TEXT NOT NULL DEFAULT 'pool' CHECK (tier IN ('pool','hot','warm','cold')),
    status          TEXT NOT NULL DEFAULT 'free' CHECK (status IN ('free','leased','assigned','sweep_pending','frozen')),
    leased_until    TIMESTAMPTZ,
    leased_for_id   UUID,
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,

    -- Allocation mode: transient (lease per invoice) or persistent (assigned to customer)
    allocation_mode TEXT NOT NULL DEFAULT 'transient' CHECK (allocation_mode IN ('transient', 'persistent')),
    customer_ref    TEXT NOT NULL DEFAULT '',  -- external customer ID (for persistent wallets)
    last_swept_at   TIMESTAMPTZ,      -- when last sweep was executed

    -- CDC fields
    _txid           BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at     TIMESTAMPTZ
);

-- Unique address per network (prevent duplicate allocation)
CREATE UNIQUE INDEX IF NOT EXISTS idx_cat_wallets_network_address
    ON cat_wallets (network_id, address);

-- Fast free wallet lookup for LeaseForInvoice (transient only)
CREATE INDEX IF NOT EXISTS idx_cat_wallets_free_pool
    ON cat_wallets (network_id)
    WHERE status = 'free' AND tier = 'pool' AND allocation_mode = 'transient'
      AND is_active = TRUE AND deletion_mark = FALSE;

CREATE UNIQUE INDEX IF NOT EXISTS idx_cat_wallets_code ON cat_wallets (code) WHERE deletion_mark = FALSE;
CREATE INDEX IF NOT EXISTS idx_cat_wallets_merchant ON cat_wallets (merchant_id) WHERE merchant_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_cat_wallets_name ON cat_wallets (name) WHERE deletion_mark = FALSE;
CREATE INDEX IF NOT EXISTS idx_cat_wallets_name_id ON cat_wallets (name ASC, id ASC);

-- Persistent customer address lookup (Phase 2)
CREATE INDEX IF NOT EXISTS idx_cat_wallets_customer
    ON cat_wallets (merchant_id, customer_ref)
    WHERE allocation_mode = 'persistent' AND deletion_mark = FALSE;

-- ═══════════════════════════════════════════════════════════════════════
-- Merchant Token Config (Регистр сведений «Настройки токенов мерчанта»)
-- NULL = use token default from cat_tokens
-- ═══════════════════════════════════════════════════════════════════════

CREATE TABLE reg_merchant_token_config (
    merchant_id         UUID NOT NULL REFERENCES cat_merchants(id),
    token_id            UUID NOT NULL REFERENCES cat_tokens(id),

    -- Sweep overrides (NULL = use token default)
    sweep_threshold     BIGINT,
    sweep_max_age_hours INT,

    -- Audit
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (merchant_id, token_id),
    CONSTRAINT chk_mtc_sweep_threshold CHECK (sweep_threshold IS NULL OR sweep_threshold >= 0),
    CONSTRAINT chk_mtc_sweep_max_age CHECK (sweep_max_age_hours IS NULL OR sweep_max_age_hours >= 0)
);

COMMENT ON TABLE reg_merchant_token_config IS
    'Регистр сведений: крипто-настройки мерчанта per token. NULL = использовать дефолт из cat_tokens.';

-- CDC trigger for wallets
CREATE OR REPLACE FUNCTION fn_cat_wallets_cdc() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        NEW._txid := txid_current();
        IF OLD.deletion_mark = FALSE AND NEW.deletion_mark = TRUE THEN
            NEW._deleted_at := NOW();
        ELSIF OLD.deletion_mark = TRUE AND NEW.deletion_mark = FALSE THEN
            NEW._deleted_at := NULL;
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_cat_wallets_cdc
    BEFORE UPDATE ON cat_wallets
    FOR EACH ROW EXECUTE FUNCTION fn_cat_wallets_cdc();

CREATE TRIGGER trg_cat_wallets_updated_at
    BEFORE UPDATE ON cat_wallets
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS reg_merchant_token_config CASCADE;
DROP TABLE IF EXISTS sys_merchant_users CASCADE;
DROP TABLE IF EXISTS cat_wallets CASCADE;
DROP TABLE IF EXISTS cat_merchants CASCADE;
