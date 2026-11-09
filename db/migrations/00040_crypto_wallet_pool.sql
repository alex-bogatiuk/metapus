-- +goose Up
-- Description: Auto-replenishing crypto wallet warm pool policies and provisioning jobs

-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

CREATE TABLE sys_crypto_wallet_pool_policies (
    network_id                      UUID PRIMARY KEY REFERENCES cat_blockchain_networks(id),
    enabled                         BOOLEAN NOT NULL DEFAULT FALSE,
    low_watermark                   INT NOT NULL DEFAULT 0,
    target_ready                    INT NOT NULL DEFAULT 1,
    max_total_wallets               INT NOT NULL DEFAULT 1,
    max_provision_batch             INT NOT NULL DEFAULT 1,
    -- NULL activation columns mean that this family derives usable addresses
    -- without a separate on-chain account-materialization step.
    max_concurrent_activations      INT,
    max_active_invoice_leases_per_merchant INT NOT NULL DEFAULT 1,
    max_persistent_addresses_per_merchant  INT NOT NULL DEFAULT 1,
    max_lease_starts_per_minute_per_merchant INT NOT NULL DEFAULT 5,
    min_global_ready_reserve        INT NOT NULL DEFAULT 0,
    activation_reserve_minor        NUMERIC(78,0),
    min_hot_native_balance_minor    NUMERIC(78,0),
    daily_activation_budget_minor   NUMERIC(78,0),
    retry_after_seconds             INT NOT NULL DEFAULT 30,
    address_reuse_cooldown_seconds  INT NOT NULL DEFAULT 0,
    max_watcher_lag_seconds         INT NOT NULL DEFAULT 0,
    lease_blocked_reason            TEXT NOT NULL DEFAULT '',
    lease_blocked_at                TIMESTAMPTZ,
    derivation_namespace            TEXT NOT NULL DEFAULT '',
    needs_review                    BOOLEAN NOT NULL DEFAULT TRUE,
    version                         INT NOT NULL DEFAULT 1,
    updated_at                      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by                      TEXT NOT NULL DEFAULT '',
    last_reconciled_at              TIMESTAMPTZ,
    last_error_code                 TEXT NOT NULL DEFAULT '',
    last_error_message              TEXT NOT NULL DEFAULT '',
    last_emitted_state              TEXT NOT NULL DEFAULT '',
    created_at                      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid                           BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at                     TIMESTAMPTZ,
    CONSTRAINT chk_wallet_pool_watermarks
        CHECK (low_watermark >= 0
           AND low_watermark < target_ready
           AND target_ready <= max_total_wallets),
    CONSTRAINT chk_wallet_pool_limits
        CHECK (max_provision_batch > 0
           AND max_active_invoice_leases_per_merchant > 0
           AND max_persistent_addresses_per_merchant > 0
           AND max_lease_starts_per_minute_per_merchant > 0
           AND min_global_ready_reserve >= 0
           AND min_global_ready_reserve <= low_watermark
           AND retry_after_seconds > 0
           AND address_reuse_cooldown_seconds >= 0
           AND max_watcher_lag_seconds >= 0
           AND (address_reuse_cooldown_seconds = 0 OR max_watcher_lag_seconds > 0)
           AND (lease_blocked_reason = '' OR lease_blocked_reason = 'watcher_lag')),
    CONSTRAINT chk_wallet_pool_activation_policy CHECK (
        (
            max_concurrent_activations IS NULL
            AND activation_reserve_minor IS NULL
            AND min_hot_native_balance_minor IS NULL
            AND daily_activation_budget_minor IS NULL
        ) OR (
            max_concurrent_activations IS NOT NULL
            AND activation_reserve_minor IS NOT NULL
            AND min_hot_native_balance_minor IS NOT NULL
            AND daily_activation_budget_minor IS NOT NULL
            AND max_concurrent_activations > 0
            AND activation_reserve_minor >= 0
            AND activation_reserve_minor <= 9007199254740991
            AND min_hot_native_balance_minor >= 0
            AND min_hot_native_balance_minor <= 9007199254740991
            AND daily_activation_budget_minor >= 0
            AND daily_activation_budget_minor <= 9007199254740991
        )
    ),
    CONSTRAINT chk_wallet_pool_version CHECK (version > 0)
);

-- Optional per-merchant exceptions. NULL means "inherit the network default".
-- Tenant isolation is provided by the physical tenant database boundary.
CREATE TABLE reg_merchant_wallet_allocation_overrides (
    merchant_id                      UUID NOT NULL REFERENCES cat_merchants(id) ON DELETE CASCADE,
    network_id                       UUID NOT NULL REFERENCES cat_blockchain_networks(id) ON DELETE CASCADE,
    max_active_invoice_leases        INT,
    max_persistent_addresses         INT,
    max_lease_starts_per_minute      INT,
    version                          INT NOT NULL DEFAULT 1,
    created_at                       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by                       TEXT NOT NULL DEFAULT '',
    _txid                            BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at                      TIMESTAMPTZ,
    PRIMARY KEY (merchant_id, network_id),
    CONSTRAINT chk_merchant_wallet_allocation_override_limits
        CHECK ((max_active_invoice_leases IS NULL OR max_active_invoice_leases > 0)
           AND (max_persistent_addresses IS NULL OR max_persistent_addresses > 0)
           AND (max_lease_starts_per_minute IS NULL OR max_lease_starts_per_minute > 0)),
    CONSTRAINT chk_merchant_wallet_allocation_override_version CHECK (version > 0)
);

-- Fixed one-minute admission window. Updates are serialized by the matching
-- network policy row, so a burst cannot oversubscribe the configured limit.
CREATE TABLE reg_merchant_wallet_lease_rate_windows (
    merchant_id       UUID NOT NULL REFERENCES cat_merchants(id) ON DELETE CASCADE,
    network_id        UUID NOT NULL REFERENCES cat_blockchain_networks(id) ON DELETE CASCADE,
    window_started_at TIMESTAMPTZ NOT NULL,
    lease_starts      INT NOT NULL DEFAULT 0 CHECK (lease_starts >= 0),
    version           INT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid             BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at       TIMESTAMPTZ,
    PRIMARY KEY (merchant_id, network_id)
);

CREATE OR REPLACE FUNCTION initialize_crypto_wallet_pool_policy()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO sys_crypto_wallet_pool_policies (
        network_id, enabled, low_watermark, target_ready, max_total_wallets,
        max_provision_batch, retry_after_seconds
    ) VALUES (
        NEW.id, FALSE, 0, 1, 1, 1, 30
    )
    ON CONFLICT (network_id) DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_blockchain_network_init_wallet_pool
    AFTER INSERT ON cat_blockchain_networks
    FOR EACH ROW EXECUTE FUNCTION initialize_crypto_wallet_pool_policy();

-- A single append-only publication registry fences every HD path across
-- historical wallets and provisioning jobs. A path is inserted before it is
-- sent to Vault and is never deleted or reused.
CREATE TABLE sys_crypto_wallet_derivations (
    network_id       UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    derivation_path  TEXT NOT NULL,
    -- Keep the published wallet identity even if an operator attempts a hard
    -- delete. Wallets use soft deletion; a hard delete must not make an HD
    -- path reusable.
    wallet_id        UUID REFERENCES cat_wallets(id),
    source           TEXT NOT NULL
                     CHECK (source IN ('historical_wallet','provisioning','bootstrap')),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    version          INT NOT NULL DEFAULT 1 CHECK (version > 0),
    _txid            BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at      TIMESTAMPTZ,
    PRIMARY KEY (network_id, derivation_path),
    CONSTRAINT chk_crypto_wallet_derivation_path_nonempty
        CHECK (BTRIM(derivation_path) <> '')
);

INSERT INTO sys_crypto_wallet_derivations (
    network_id, derivation_path, wallet_id, source
)
SELECT network_id, derivation_path, id, 'historical_wallet'
FROM cat_wallets
WHERE derivation_path <> ''
ON CONFLICT (network_id, derivation_path) DO NOTHING;

CREATE OR REPLACE FUNCTION publish_crypto_wallet_derivation()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.derivation_path = '' THEN
        RETURN NEW;
    END IF;

    INSERT INTO sys_crypto_wallet_derivations (
        network_id, derivation_path, wallet_id, source
    ) VALUES (
        NEW.network_id, NEW.derivation_path, NEW.id, 'historical_wallet'
    )
    ON CONFLICT (network_id, derivation_path) DO UPDATE
    SET wallet_id = EXCLUDED.wallet_id,
        version = sys_crypto_wallet_derivations.version + 1
    WHERE sys_crypto_wallet_derivations.wallet_id IS NULL
       OR sys_crypto_wallet_derivations.wallet_id = EXCLUDED.wallet_id;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'crypto wallet derivation path is already bound to another wallet';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_cat_wallets_publish_derivation
    AFTER INSERT OR UPDATE OF network_id, derivation_path ON cat_wallets
    FOR EACH ROW EXECUTE FUNCTION publish_crypto_wallet_derivation();

CREATE TABLE sys_crypto_wallet_derivation_cursors (
    network_id           UUID PRIMARY KEY REFERENCES cat_blockchain_networks(id),
    derivation_namespace TEXT NOT NULL,
    next_index           BIGINT NOT NULL CHECK (next_index >= 0),
    needs_review         BOOLEAN NOT NULL DEFAULT FALSE,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    version              INT NOT NULL DEFAULT 1 CHECK (version > 0),
    _txid                BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at          TIMESTAMPTZ,
    CONSTRAINT uq_wallet_derivation_cursor_namespace
        UNIQUE (network_id, derivation_namespace)
);

CREATE TABLE sys_crypto_wallet_provisioning_jobs (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    network_id          UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    derivation_path     TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending','processing','retry_wait','completed','held')),
    attempts            INT NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    claimed_by          TEXT NOT NULL DEFAULT '',
    claim_token         UUID,
    claimed_at          TIMESTAMPTZ,
    lease_expires_at    TIMESTAMPTZ,
    last_error          TEXT NOT NULL DEFAULT '',
    wallet_id           UUID REFERENCES cat_wallets(id),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at        TIMESTAMPTZ,
    version             INT NOT NULL DEFAULT 1 CHECK (version > 0),
    _txid               BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at         TIMESTAMPTZ,
    CONSTRAINT uq_wallet_provisioning_derivation_path
        UNIQUE (network_id, derivation_path),
    CONSTRAINT fk_wallet_provisioning_derivation
        FOREIGN KEY (network_id, derivation_path)
        REFERENCES sys_crypto_wallet_derivations (network_id, derivation_path)
);

CREATE INDEX idx_wallet_provisioning_claim
    ON sys_crypto_wallet_provisioning_jobs (next_attempt_at, created_at)
    WHERE status IN ('pending','retry_wait','processing');

CREATE INDEX idx_wallet_provisioning_network_status
    ON sys_crypto_wallet_provisioning_jobs (network_id, status);

CREATE TRIGGER trg_wallet_pool_policies_updated_at
    BEFORE UPDATE ON sys_crypto_wallet_pool_policies
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_wallet_pool_policies_txid
    BEFORE UPDATE ON sys_crypto_wallet_pool_policies
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_merchant_wallet_allocation_overrides_updated_at
    BEFORE UPDATE ON reg_merchant_wallet_allocation_overrides
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_merchant_wallet_allocation_overrides_txid
    BEFORE UPDATE ON reg_merchant_wallet_allocation_overrides
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_merchant_wallet_lease_rate_windows_updated_at
    BEFORE UPDATE ON reg_merchant_wallet_lease_rate_windows
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_merchant_wallet_lease_rate_windows_txid
    BEFORE UPDATE ON reg_merchant_wallet_lease_rate_windows
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_wallet_derivations_updated_at
    BEFORE UPDATE ON sys_crypto_wallet_derivations
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_wallet_derivations_txid
    BEFORE UPDATE ON sys_crypto_wallet_derivations
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_wallet_derivation_cursors_updated_at
    BEFORE UPDATE ON sys_crypto_wallet_derivation_cursors
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_wallet_derivation_cursors_txid
    BEFORE UPDATE ON sys_crypto_wallet_derivation_cursors
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_wallet_provisioning_jobs_updated_at
    BEFORE UPDATE ON sys_crypto_wallet_provisioning_jobs
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_wallet_provisioning_jobs_txid
    BEFORE UPDATE ON sys_crypto_wallet_provisioning_jobs
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE INDEX idx_wallet_pool_policies_txid
    ON sys_crypto_wallet_pool_policies (_txid)
    WHERE _deleted_at IS NULL;

CREATE INDEX idx_merchant_wallet_allocation_overrides_txid
    ON reg_merchant_wallet_allocation_overrides (_txid)
    WHERE _deleted_at IS NULL;

CREATE INDEX idx_merchant_wallet_lease_rate_windows_txid
    ON reg_merchant_wallet_lease_rate_windows (_txid)
    WHERE _deleted_at IS NULL;

CREATE INDEX idx_wallet_derivations_txid
    ON sys_crypto_wallet_derivations (_txid)
    WHERE _deleted_at IS NULL;

CREATE INDEX idx_wallet_derivation_cursors_txid
    ON sys_crypto_wallet_derivation_cursors (_txid)
    WHERE _deleted_at IS NULL;

CREATE INDEX idx_wallet_provisioning_jobs_txid
    ON sys_crypto_wallet_provisioning_jobs (_txid)
    WHERE _deleted_at IS NULL;

CREATE UNIQUE INDEX idx_cat_wallets_network_derivation_path
    ON cat_wallets (network_id, derivation_path)
    WHERE derivation_path <> '';

-- Every policy starts fail-closed. A deployment bootstrap or an MFA-protected
-- policy update explicitly enables the intended network.
INSERT INTO sys_crypto_wallet_pool_policies (
    network_id, enabled, low_watermark, target_ready, max_total_wallets,
    max_provision_batch, retry_after_seconds
)
SELECT id, FALSE, 0, 1, 1, 1, 30
FROM cat_blockchain_networks
ON CONFLICT (network_id) DO NOTHING;

-- Initialize unambiguous BIP-44 generations. Networks that contain more than
-- one namespace remain disabled and are surfaced as needs_review.
WITH parsed AS (
    SELECT network_id,
           regexp_replace(derivation_path, '/[0-9]+$', '') AS namespace,
           substring(derivation_path FROM '/([0-9]+)$')::BIGINT AS derivation_index
    FROM cat_wallets
    WHERE derivation_path ~ '^m(/[0-9]+''?)+/[0-9]+$'
      AND tier = 'pool'
),
generations AS (
    SELECT network_id,
           COUNT(DISTINCT namespace) AS namespace_count,
           MIN(namespace) AS namespace
    FROM parsed
    GROUP BY network_id
),
summary AS (
    SELECT g.network_id,
           g.namespace_count,
           g.namespace,
           COALESCE(
               MAX(substring(d.derivation_path FROM '/([0-9]+)$')::BIGINT),
               -1
           ) + 1 AS next_index
    FROM generations g
    LEFT JOIN sys_crypto_wallet_derivations d
      ON d.network_id = g.network_id
     AND d.derivation_path LIKE (g.namespace || '/%')
     AND d.derivation_path ~ '^m(/[0-9]+''?)+/[0-9]+$'
    GROUP BY g.network_id, g.namespace_count, g.namespace
)
INSERT INTO sys_crypto_wallet_derivation_cursors (
    network_id, derivation_namespace, next_index, needs_review
)
SELECT network_id, namespace, next_index, namespace_count <> 1
FROM summary
ON CONFLICT (network_id) DO NOTHING;

UPDATE sys_crypto_wallet_pool_policies p
SET derivation_namespace = c.derivation_namespace,
    needs_review = c.needs_review
FROM sys_crypto_wallet_derivation_cursors c
WHERE c.network_id = p.network_id;

INSERT INTO auth_permissions (code, name, description, resource, action) VALUES
    ('crypto_wallet_pool:read', 'Чтение пула кошельков',
     'View aggregate crypto wallet pool state and policy', 'crypto_wallet_pool', 'read'),
    ('crypto_wallet_pool:update', 'Настройка пула кошельков',
     'Update crypto wallet pool policy and request refill', 'crypto_wallet_pool', 'update')
ON CONFLICT (code) DO NOTHING;

INSERT INTO auth_role_permissions (role_id, permission_id)
SELECT 'b0000000-0000-0000-0000-000000000001', id
FROM auth_permissions
WHERE resource = 'crypto_wallet_pool'
ON CONFLICT DO NOTHING;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

DELETE FROM auth_role_permissions
WHERE permission_id IN (
    SELECT id FROM auth_permissions WHERE resource = 'crypto_wallet_pool'
);
DELETE FROM auth_permissions WHERE resource = 'crypto_wallet_pool';

DROP INDEX IF EXISTS idx_cat_wallets_network_derivation_path;
DROP TRIGGER IF EXISTS trg_cat_wallets_publish_derivation ON cat_wallets;
DROP FUNCTION IF EXISTS publish_crypto_wallet_derivation();
DROP TABLE IF EXISTS sys_crypto_wallet_provisioning_jobs;
DROP TABLE IF EXISTS sys_crypto_wallet_derivation_cursors;
DROP TABLE IF EXISTS sys_crypto_wallet_derivations;
DROP TABLE IF EXISTS reg_merchant_wallet_lease_rate_windows;
DROP TABLE IF EXISTS reg_merchant_wallet_allocation_overrides;
DROP TRIGGER IF EXISTS trg_blockchain_network_init_wallet_pool ON cat_blockchain_networks;
DROP FUNCTION IF EXISTS initialize_crypto_wallet_pool_policy();
DROP TABLE IF EXISTS sys_crypto_wallet_pool_policies;

-- +goose StatementEnd
