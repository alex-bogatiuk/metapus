-- +goose Up
-- Description: Merchant catalog + Wallet catalog + junction table

-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- ═══════════════════════════════════════════════════════════════════════
-- MERCHANT CATALOG
-- ═══════════════════════════════════════════════════════════════════════

CREATE TABLE cat_merchants (
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
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    kyb_status      TEXT NOT NULL DEFAULT 'pending' CHECK (kyb_status IN ('pending','approved','rejected')),

    -- CDC fields
    _txid           BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at     TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_cat_merchants_code ON cat_merchants (code) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_merchants_name ON cat_merchants (name) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_merchants_name_id ON cat_merchants (name ASC, id ASC);

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

CREATE TABLE sys_merchant_users (
    user_id     UUID NOT NULL,
    merchant_id UUID NOT NULL REFERENCES cat_merchants(id),
    role        INT NOT NULL DEFAULT 1 CHECK (role BETWEEN 1 AND 3),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, merchant_id)
);

CREATE INDEX idx_merchant_users_merchant ON sys_merchant_users (merchant_id);

-- ═══════════════════════════════════════════════════════════════════════
-- WALLET CATALOG
-- ═══════════════════════════════════════════════════════════════════════

CREATE TABLE cat_wallets (
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
    address_key     TEXT NOT NULL DEFAULT '',
    derivation_path TEXT NOT NULL DEFAULT '',
    vault_key_id    TEXT NOT NULL DEFAULT '',
    tier            TEXT NOT NULL DEFAULT 'pool' CHECK (tier IN ('pool','hot','warm','cold')),
    wallet_role     TEXT NOT NULL DEFAULT 'merchant_pool'
                    CHECK (wallet_role IN ('merchant_pool','liquidity_hot','network_fee','treasury')),
    status          TEXT NOT NULL DEFAULT 'free' CHECK (status IN ('free','leased','assigned','sweep_pending','sweeping','frozen')),
    leased_until    TIMESTAMPTZ,
    leased_for_id   UUID,
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    chain_activation_status TEXT NOT NULL DEFAULT 'unknown'
        CHECK (chain_activation_status IN ('unknown','activating','active','held','failed')),
    cooldown_until TIMESTAMPTZ,

    -- Allocation mode: transient (lease per invoice) or persistent (assigned to customer)
    allocation_mode TEXT NOT NULL DEFAULT 'transient' CHECK (allocation_mode IN ('transient', 'persistent')),
    customer_id     TEXT NOT NULL DEFAULT '',  -- external customer ID (for persistent wallets)
    deposit_assigned_at TIMESTAMPTZ,
    deposit_retired_at  TIMESTAMPTZ,
    CONSTRAINT chk_deposit_address_retired CHECK (deposit_retired_at IS NULL OR
        (allocation_mode='persistent' AND merchant_id IS NOT NULL AND customer_id<>''
         AND status IN ('assigned','frozen') AND is_active AND NOT deletion_mark AND _deleted_at IS NULL)),
    last_swept_at   TIMESTAMPTZ,      -- when last sweep was executed

    -- CDC fields
    _txid           BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at     TIMESTAMPTZ
);

-- Unique address per network (prevent duplicate allocation)
CREATE UNIQUE INDEX idx_cat_wallets_network_address
    ON cat_wallets (network_id, address_key);

CREATE UNIQUE INDEX idx_cat_wallets_one_active_network_fee
    ON cat_wallets (network_id)
    WHERE wallet_role = 'network_fee'
      AND is_active = TRUE
      AND deletion_mark = FALSE;

CREATE INDEX idx_cat_wallets_active_free_pool
    ON cat_wallets (network_id)
    WHERE status = 'free'
      AND tier = 'pool'
      AND chain_activation_status = 'active'
      AND is_active = TRUE
      AND deletion_mark = FALSE;

CREATE INDEX idx_cat_wallets_cooldown
    ON cat_wallets (network_id, cooldown_until)
    WHERE status = 'free' AND cooldown_until IS NOT NULL;

-- Fast free wallet lookup for LeaseForInvoice (transient only)
CREATE INDEX idx_cat_wallets_free_pool
    ON cat_wallets (network_id)
    WHERE status = 'free' AND tier = 'pool' AND allocation_mode = 'transient'
      AND is_active = TRUE AND deletion_mark = FALSE;

CREATE UNIQUE INDEX idx_cat_wallets_code ON cat_wallets (code) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_wallets_merchant ON cat_wallets (merchant_id) WHERE merchant_id IS NOT NULL;
CREATE INDEX idx_cat_wallets_name ON cat_wallets (name) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_wallets_name_id ON cat_wallets (name ASC, id ASC);

-- Persistent customer address uniqueness: one wallet per (merchant, network, customer).
-- Prevents TOCTOU race in AssignPersistentAddress: if two concurrent requests
-- both pass the idempotency check, the second UPDATE will fail with a unique violation.
CREATE UNIQUE INDEX idx_cat_wallets_persistent_customer
    ON cat_wallets (merchant_id, network_id, customer_id)
    WHERE allocation_mode = 'persistent' AND deletion_mark = FALSE AND customer_id != ''
      AND deposit_retired_at IS NULL;

-- DEPOSIT_ADDRESS_ROTATION_SCHEMA_BEGIN
CREATE TABLE reg_crypto_deposit_address_rotations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    merchant_id UUID NOT NULL REFERENCES cat_merchants(id),
    network_id UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    customer_id TEXT NOT NULL CHECK (BTRIM(customer_id)<>''),
    old_wallet_id UUID NOT NULL UNIQUE REFERENCES cat_wallets(id),
    new_wallet_id UUID NOT NULL UNIQUE REFERENCES cat_wallets(id),
    network_code TEXT NOT NULL,
    network_name TEXT NOT NULL,
    tokens TEXT[] NOT NULL,
    old_address TEXT NOT NULL,
    new_address TEXT NOT NULL,
    reason TEXT NOT NULL CHECK (reason IN ('aml_high_risk','customer_request')),
    actor_id UUID NOT NULL,
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    version INT NOT NULL CHECK (version>0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ,
    UNIQUE (merchant_id,idempotency_key),
    CHECK (old_wallet_id<>new_wallet_id)
);
CREATE INDEX idx_deposit_rotations_customer
    ON reg_crypto_deposit_address_rotations (merchant_id,customer_id,created_at DESC);
CREATE INDEX idx_deposit_rotations_merchant_window
    ON reg_crypto_deposit_address_rotations (merchant_id,network_id,created_at DESC);

CREATE OR REPLACE FUNCTION fn_deposit_address_retirement_guard() RETURNS TRIGGER AS $$
BEGIN
    IF TG_TABLE_NAME='reg_crypto_deposit_address_rotations' THEN
        RAISE EXCEPTION 'deposit address rotation history is immutable';
    END IF;
    IF OLD.deposit_retired_at IS NOT NULL THEN
        IF TG_OP='DELETE' THEN
            RAISE EXCEPTION 'retired deposit addresses must remain monitored';
        END IF;
        IF NEW.deposit_retired_at IS DISTINCT FROM OLD.deposit_retired_at
           OR NEW.merchant_id IS DISTINCT FROM OLD.merchant_id
           OR NEW.customer_id IS DISTINCT FROM OLD.customer_id
           OR NEW.network_id IS DISTINCT FROM OLD.network_id
           OR NEW.address IS DISTINCT FROM OLD.address
           OR NEW.address_key IS DISTINCT FROM OLD.address_key
           OR NEW.vault_key_id IS DISTINCT FROM OLD.vault_key_id
           OR NEW.deposit_assigned_at IS DISTINCT FROM OLD.deposit_assigned_at THEN
            RAISE EXCEPTION 'retired deposit address attribution is immutable';
        END IF;
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_deposit_address_retirement_guard
    BEFORE UPDATE OR DELETE ON cat_wallets
    FOR EACH ROW EXECUTE FUNCTION fn_deposit_address_retirement_guard();
CREATE TRIGGER trg_deposit_rotation_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_deposit_address_rotations
    FOR EACH ROW EXECUTE FUNCTION fn_deposit_address_retirement_guard();

CREATE OR REPLACE FUNCTION fn_deposit_rotation_identity_guard() RETURNS TRIGGER AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM cat_wallets old_wallet JOIN cat_wallets new_wallet ON new_wallet.id=NEW.new_wallet_id
        WHERE old_wallet.id=NEW.old_wallet_id AND old_wallet.deposit_retired_at=NEW.created_at
          AND old_wallet.merchant_id=NEW.merchant_id AND new_wallet.merchant_id=NEW.merchant_id
          AND old_wallet.network_id=NEW.network_id AND new_wallet.network_id=NEW.network_id
          AND old_wallet.customer_id=NEW.customer_id AND new_wallet.customer_id=NEW.customer_id
          AND old_wallet.address=NEW.old_address AND new_wallet.address=NEW.new_address
          AND new_wallet.deposit_assigned_at=NEW.created_at
    ) THEN RAISE EXCEPTION 'deposit rotation assignment identity mismatch'; END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_deposit_rotation_identity
    BEFORE INSERT ON reg_crypto_deposit_address_rotations
    FOR EACH ROW EXECUTE FUNCTION fn_deposit_rotation_identity_guard();
-- DEPOSIT_ADDRESS_ROTATION_SCHEMA_END

-- ═══════════════════════════════════════════════════════════════════════
-- Merchant Token Config (Регистр сведений «Настройки токенов мерчанта»)
-- NULL = use token default from cat_tokens
-- ═══════════════════════════════════════════════════════════════════════

CREATE TABLE reg_merchant_token_config (
    merchant_id         UUID NOT NULL REFERENCES cat_merchants(id),
    token_id            UUID NOT NULL REFERENCES cat_tokens(id),

    -- Sweep overrides (NULL = use token default)
    sweep_threshold     NUMERIC(78,0),
    sweep_max_age_hours INT,
    sweep_retain_amount NUMERIC(78,0),

    -- Audit
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (merchant_id, token_id),
    CONSTRAINT chk_mtc_sweep_threshold CHECK (sweep_threshold IS NULL OR sweep_threshold >= 0),
    CONSTRAINT chk_mtc_sweep_max_age CHECK (sweep_max_age_hours IS NULL OR sweep_max_age_hours >= 0),
    CONSTRAINT chk_mtc_sweep_retain_amount CHECK (sweep_retain_amount IS NULL OR sweep_retain_amount >= 0)
);

COMMENT ON TABLE reg_merchant_token_config IS
    'Регистр сведений: крипто-настройки мерчанта per token. NULL = использовать дефолт из cat_tokens.';

-- CDC trigger for wallets
CREATE OR REPLACE FUNCTION fn_cat_wallets_cdc() RETURNS TRIGGER AS $$
DECLARE family TEXT;
BEGIN
	SELECT chain_family INTO family FROM cat_blockchain_networks WHERE id=NEW.network_id;
	NEW.address_key := CASE WHEN family='evm' THEN LOWER(NEW.address) ELSE NEW.address END;
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
    BEFORE INSERT OR UPDATE ON cat_wallets
    FOR EACH ROW EXECUTE FUNCTION fn_cat_wallets_cdc();

CREATE TRIGGER trg_cat_wallets_updated_at
    BEFORE UPDATE ON cat_wallets
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ═══════════════════════════════════════════════════════════════════════
-- MERCHANT API KEYS
-- Stores hashed API keys for the merchant public API (/merchant/v1/).
-- The plaintext key is shown once at creation and never stored.
-- key_hash = SHA-256(plaintext) stored as lowercase hex.
-- ═══════════════════════════════════════════════════════════════════════

CREATE TABLE cat_merchant_api_keys (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    merchant_id         UUID        NOT NULL REFERENCES cat_merchants(id) ON DELETE CASCADE,
    name                TEXT        NOT NULL,                   -- human-readable label ("Production Key")
    key_prefix          VARCHAR(16) NOT NULL,                   -- "mk_" + first 8 chars, for UI display
    key_hash            CHAR(64)    NOT NULL,                   -- hex(SHA-256(plaintext_key))
    scopes              TEXT[]      NOT NULL DEFAULT ARRAY['invoice:create','invoice:read'],
    is_active           BOOLEAN     NOT NULL DEFAULT TRUE,
    last_used_at        TIMESTAMPTZ,
    expires_at          TIMESTAMPTZ,
    -- Audit: which platform user issued this key (best-effort, no FK — users are in auth schema).
    -- NULL for keys created before this field was added or via automated processes.
    created_by_user_id  UUID,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (key_hash)
);

-- Hot-path: auth lookup by hash (only active keys)
CREATE UNIQUE INDEX idx_merchant_api_keys_hash
    ON cat_merchant_api_keys (key_hash)
    WHERE is_active = TRUE;

CREATE INDEX idx_merchant_api_keys_merchant
    ON cat_merchant_api_keys (merchant_id);

CREATE TRIGGER trg_merchant_api_keys_updated_at
    BEFORE UPDATE ON cat_merchant_api_keys
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ═══════════════════════════════════════════════════════════════════════
-- MERCHANT NETWORK ACCESS POLICIES
-- Baseline portal/API policies and optional per-key API intersections.
-- ═══════════════════════════════════════════════════════════════════════

ALTER TABLE cat_merchant_api_keys ADD CONSTRAINT uq_merchant_api_key_identity UNIQUE (id,merchant_id);

CREATE TABLE auth_merchant_network_policies (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    merchant_id    UUID NOT NULL REFERENCES cat_merchants(id) ON DELETE CASCADE,
    surface        TEXT NOT NULL CHECK (surface IN ('portal','api')),
    api_key_id     UUID,
    mode           TEXT NOT NULL DEFAULT 'off' CHECK (mode IN ('off','warn','enforce')),
    version        INT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by     UUID,
    updated_by     UUID,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_merchant_network_policy_surface CHECK (
      (surface='portal' AND api_key_id IS NULL) OR surface='api'
    ),
    CONSTRAINT fk_merchant_network_policy_api_key FOREIGN KEY (api_key_id,merchant_id)
      REFERENCES cat_merchant_api_keys(id,merchant_id)
);
CREATE UNIQUE INDEX uq_merchant_network_policy_baseline
    ON auth_merchant_network_policies(merchant_id,surface) WHERE api_key_id IS NULL;
CREATE UNIQUE INDEX uq_merchant_network_policy_api_key
    ON auth_merchant_network_policies(merchant_id,surface,api_key_id) WHERE api_key_id IS NOT NULL;

CREATE TABLE auth_merchant_allowed_networks (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    policy_id      UUID NOT NULL REFERENCES auth_merchant_network_policies(id) ON DELETE RESTRICT,
    network        CIDR NOT NULL,
    label          TEXT NOT NULL DEFAULT '',
    is_active      BOOLEAN NOT NULL DEFAULT TRUE,
    created_by     UUID,
    revoked_by     UUID,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at     TIMESTAMPTZ,
    CONSTRAINT chk_merchant_allowed_network_revocation CHECK (
      (is_active AND revoked_at IS NULL AND revoked_by IS NULL)
      OR (NOT is_active AND revoked_at IS NOT NULL)
    )
);
CREATE UNIQUE INDEX uq_merchant_allowed_network_active
    ON auth_merchant_allowed_networks(policy_id,network) WHERE is_active;

CREATE TABLE auth_merchant_network_policy_audit (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    merchant_id UUID NOT NULL,
    policy_id   UUID,
    actor_type  TEXT NOT NULL CHECK (actor_type IN ('user','api_key','system','system_admin','request')),
    actor_id    TEXT NOT NULL,
    action      TEXT NOT NULL,
    decision    TEXT NOT NULL DEFAULT '' CHECK (decision IN ('','allow','warn','deny')),
    client_ip   INET,
    old_policy  JSONB NOT NULL DEFAULT '{}'::jsonb,
    new_policy  JSONB NOT NULL DEFAULT '{}'::jsonb,
    reason      TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE OR REPLACE FUNCTION fn_merchant_network_policy_guard() RETURNS TRIGGER AS $$
DECLARE policy_mode TEXT;
BEGIN
    IF TG_TABLE_NAME='auth_merchant_network_policy_audit' THEN
        RAISE EXCEPTION 'merchant network policy audit is immutable';
    END IF;
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'merchant network policies are revoked, not deleted';
    END IF;
    IF TG_TABLE_NAME='auth_merchant_network_policies' THEN
        IF TG_OP='UPDATE' AND OLD.mode='off' AND NEW.mode='enforce' THEN
            RAISE EXCEPTION 'merchant network policy must pass through warn mode';
        END IF;
        NEW.version := CASE WHEN TG_OP='UPDATE' THEN OLD.version+1 ELSE NEW.version END;
        NEW.updated_at := NOW();
        policy_mode := NEW.mode;
    ELSE
        SELECT mode INTO policy_mode FROM auth_merchant_network_policies WHERE id=NEW.policy_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_merchant_network_policy_guard
    BEFORE UPDATE OR DELETE ON auth_merchant_network_policies
    FOR EACH ROW EXECUTE FUNCTION fn_merchant_network_policy_guard();
CREATE TRIGGER trg_merchant_allowed_network_guard
    BEFORE UPDATE OR DELETE ON auth_merchant_allowed_networks
    FOR EACH ROW EXECUTE FUNCTION fn_merchant_network_policy_guard();
CREATE TRIGGER trg_merchant_network_policy_audit_immutable
    BEFORE UPDATE OR DELETE ON auth_merchant_network_policy_audit
    FOR EACH ROW EXECUTE FUNCTION fn_merchant_network_policy_guard();

CREATE OR REPLACE FUNCTION fn_merchant_network_policy_enforce_has_cidr() RETURNS TRIGGER AS $$
DECLARE target_policy UUID;
BEGIN
    -- This trigger is shared by two tables with different row types. Accessing
    -- NEW.id/NEW.policy_id in one CASE expression is unsafe because PL/pgSQL can
    -- cache the expression plan for the row type that fires first.
    IF TG_TABLE_NAME='auth_merchant_network_policies' THEN
        target_policy := (to_jsonb(NEW)->>'id')::UUID;
    ELSIF TG_TABLE_NAME='auth_merchant_allowed_networks' THEN
        target_policy := (to_jsonb(NEW)->>'policy_id')::UUID;
    ELSE
        RAISE EXCEPTION 'unsupported merchant network policy trigger table: %', TG_TABLE_NAME;
    END IF;
    IF EXISTS (SELECT 1 FROM auth_merchant_network_policies p WHERE p.id=target_policy AND p.mode='enforce')
       AND NOT EXISTS (SELECT 1 FROM auth_merchant_allowed_networks n WHERE n.policy_id=target_policy AND n.is_active) THEN
        RAISE EXCEPTION 'enforce mode requires at least one active CIDR';
    END IF;
    PERFORM pg_notify('merchant_network_policy_changed',target_policy::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE CONSTRAINT TRIGGER trg_merchant_network_policy_has_cidr
    AFTER INSERT OR UPDATE ON auth_merchant_network_policies
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_merchant_network_policy_enforce_has_cidr();
CREATE CONSTRAINT TRIGGER trg_merchant_allowed_network_has_cidr
    AFTER INSERT OR UPDATE ON auth_merchant_allowed_networks
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fn_merchant_network_policy_enforce_has_cidr();

-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS auth_merchant_network_policy_audit CASCADE;
DROP TABLE IF EXISTS auth_merchant_allowed_networks CASCADE;
DROP TABLE IF EXISTS auth_merchant_network_policies CASCADE;
DROP FUNCTION IF EXISTS fn_merchant_network_policy_enforce_has_cidr();
DROP FUNCTION IF EXISTS fn_merchant_network_policy_guard();
DROP TABLE IF EXISTS cat_merchant_api_keys CASCADE;
DROP TABLE IF EXISTS reg_merchant_token_config CASCADE;
DROP TABLE IF EXISTS sys_merchant_users CASCADE;
DROP TABLE IF EXISTS reg_crypto_deposit_address_rotations CASCADE;
DROP TABLE IF EXISTS cat_wallets CASCADE;
DROP FUNCTION IF EXISTS fn_deposit_rotation_identity_guard();
DROP FUNCTION IF EXISTS fn_deposit_address_retirement_guard();
DROP TABLE IF EXISTS cat_merchants CASCADE;

