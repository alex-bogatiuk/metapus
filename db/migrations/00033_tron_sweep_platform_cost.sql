-- +goose Up
-- Description: TRON sweep dust retention, resource orders, and platform cost register

-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

CREATE TABLE reg_crypto_sweep_attempts (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    wallet_id            UUID NOT NULL REFERENCES cat_wallets(id),
    token_id             UUID NOT NULL REFERENCES cat_tokens(id),
    network_id           UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    sweep_id             UUID REFERENCES doc_crypto_sweeps(id),
    hot_wallet_id        UUID NOT NULL REFERENCES cat_wallets(id),
    source_address       TEXT NOT NULL,
    destination_address  TEXT NOT NULL,
    amount               NUMERIC(78,0) NOT NULL CHECK (amount > 0),
    status               TEXT NOT NULL DEFAULT 'claimed'
        CHECK (status IN ('claimed','resource_ready','signing','broadcast','posted','held','failed','expired')),
    error_message        TEXT NOT NULL DEFAULT '',
    claimed_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    broadcast_at         TIMESTAMPTZ,
    posted_at            TIMESTAMPTZ,
    expires_at           TIMESTAMPTZ NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_crypto_sweep_attempts_active_wallet_token
    ON reg_crypto_sweep_attempts (wallet_id, token_id)
    WHERE status IN ('claimed','resource_ready','signing','broadcast','held');

CREATE INDEX idx_crypto_sweep_attempts_status
    ON reg_crypto_sweep_attempts (status, updated_at);

CREATE INDEX idx_crypto_sweep_attempts_sweep
    ON reg_crypto_sweep_attempts (sweep_id, status)
    WHERE sweep_id IS NOT NULL;

CREATE TRIGGER trg_crypto_sweep_attempts_updated_at
    BEFORE UPDATE ON reg_crypto_sweep_attempts
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE UNIQUE INDEX idx_crypto_sweep_lines_tx_hash_unique
    ON doc_crypto_sweep_lines (tx_hash)
    WHERE tx_hash <> '';

CREATE TABLE reg_crypto_platform_cost_movements (
    line_id             UUID PRIMARY KEY,
    recorder_id         UUID NOT NULL,
    recorder_type       TEXT NOT NULL,
    recorder_version    INT NOT NULL,
    period              TIMESTAMPTZ NOT NULL,
    record_type         TEXT NOT NULL CHECK (record_type IN ('receipt', 'expense')),

    network_id          UUID NOT NULL REFERENCES cat_blockchain_networks(id),
    operation_token_id  UUID REFERENCES cat_tokens(id),
    cost_token_id       UUID NOT NULL REFERENCES cat_tokens(id),
    cost_type           TEXT NOT NULL
                        CHECK (cost_type IN ('chain_resource_rent','chain_network_fee','custody_loss')),
    purpose             TEXT NOT NULL
                        CHECK (purpose IN ('sweep','withdrawal','treasury','activation','resource_rental','native_funding','custody_loss')),
    provider            TEXT NOT NULL DEFAULT '',
    external_order_id   TEXT NOT NULL DEFAULT '',
    amount              NUMERIC(78,0) NOT NULL CHECK (amount >= 0),

    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_crypto_platform_cost_movements_recorder
    ON reg_crypto_platform_cost_movements (recorder_id);

CREATE INDEX idx_crypto_platform_cost_movements_period
    ON reg_crypto_platform_cost_movements (period DESC);

CREATE UNIQUE INDEX idx_crypto_platform_cost_effect
    ON reg_crypto_platform_cost_movements
       (recorder_type, recorder_id, record_type, cost_type, purpose, provider, external_order_id);

-- A custody adjustment is financially terminal only when posting.Engine has
-- atomically produced both required platform-owned effects. A deferred
-- constraint keeps the invariant independent from application code and still
-- permits the engine to record movements before updating the document state.
CREATE OR REPLACE FUNCTION fn_crypto_custody_adjustment_posting_effects_guard()
RETURNS TRIGGER AS $$
DECLARE
    wallet_total     BIGINT;
    wallet_effects   BIGINT;
    platform_total   BIGINT;
    platform_effects BIGINT;
    merchant_effects BIGINT;
BEGIN
    IF NEW.status <> 'posted' OR OLD.status = 'posted' THEN
        RETURN NEW;
    END IF;

    SELECT COUNT(*), COUNT(*) FILTER (WHERE
             movement.recorder_version=NEW.posted_version
         AND movement.record_type='expense'
         AND movement.wallet_id=NEW.wallet_id
         AND movement.token_id=NEW.token_id
         AND movement.amount=NEW.amount)
    INTO wallet_total, wallet_effects
    FROM reg_crypto_balance_movements movement
    WHERE movement.recorder_type='CryptoCustodyAdjustment'
      AND movement.recorder_id=NEW.id;

    SELECT COUNT(*), COUNT(*) FILTER (WHERE
             movement.recorder_version=NEW.posted_version
         AND movement.record_type='expense'
         AND movement.network_id=NEW.network_id
         AND movement.operation_token_id=NEW.token_id
         AND movement.cost_token_id=NEW.token_id
         AND movement.cost_type='custody_loss'
         AND movement.purpose='custody_loss'
         AND movement.amount=NEW.amount)
    INTO platform_total, platform_effects
    FROM reg_crypto_platform_cost_movements movement
    WHERE movement.recorder_type='CryptoCustodyAdjustment'
      AND movement.recorder_id=NEW.id;

    SELECT COUNT(*) INTO merchant_effects
    FROM reg_crypto_merchant_balance_movements movement
    WHERE movement.recorder_type='CryptoCustodyAdjustment'
      AND movement.recorder_id=NEW.id;

    IF wallet_total <> 1 OR wallet_effects <> 1
       OR platform_total <> 1 OR platform_effects <> 1
       OR merchant_effects <> 0 THEN
        RAISE EXCEPTION
          'posted custody adjustment requires exactly one wallet expense, one platform custody-loss expense and no merchant movement';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER trg_crypto_custody_adjustment_posting_effects
    AFTER UPDATE ON doc_crypto_custody_adjustments
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_adjustment_posting_effects_guard();

CREATE OR REPLACE FUNCTION fn_crypto_custody_wallet_effect_guard()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.recorder_type <> 'CryptoCustodyAdjustment' THEN
            RETURN NEW;
        END IF;
        IF NOT EXISTS (
            SELECT 1 FROM doc_crypto_custody_adjustments adjustment
            WHERE adjustment.id=NEW.recorder_id
              AND adjustment.status='pending_approval'
              AND NEW.recorder_version=1
              AND NEW.record_type='expense'
              AND NEW.wallet_id=adjustment.wallet_id
              AND NEW.token_id=adjustment.token_id
              AND NEW.amount=adjustment.amount
        ) THEN
            RAISE EXCEPTION 'custody adjustment wallet movement does not match its immutable source';
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' THEN
        IF OLD.recorder_type='CryptoCustodyAdjustment' OR NEW.recorder_type='CryptoCustodyAdjustment' THEN
            RAISE EXCEPTION 'custody adjustment wallet movements are immutable';
        END IF;
        RETURN NEW;
    END IF;
    IF OLD.recorder_type='CryptoCustodyAdjustment' THEN
        RAISE EXCEPTION 'custody adjustment wallet movements are immutable';
    END IF;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION fn_crypto_custody_platform_effect_guard()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.recorder_type <> 'CryptoCustodyAdjustment' THEN
            RETURN NEW;
        END IF;
        IF NOT EXISTS (
            SELECT 1 FROM doc_crypto_custody_adjustments adjustment
            WHERE adjustment.id=NEW.recorder_id
              AND adjustment.status='pending_approval'
              AND NEW.recorder_version=1
              AND NEW.record_type='expense'
              AND NEW.network_id=adjustment.network_id
              AND NEW.operation_token_id=adjustment.token_id
              AND NEW.cost_token_id=adjustment.token_id
              AND NEW.cost_type='custody_loss'
              AND NEW.purpose='custody_loss'
              AND NEW.amount=adjustment.amount
        ) THEN
            RAISE EXCEPTION 'custody adjustment platform movement does not match its immutable source';
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' THEN
        IF OLD.recorder_type='CryptoCustodyAdjustment' OR NEW.recorder_type='CryptoCustodyAdjustment' THEN
            RAISE EXCEPTION 'custody adjustment platform movements are immutable';
        END IF;
        RETURN NEW;
    END IF;
    IF OLD.recorder_type='CryptoCustodyAdjustment' THEN
        RAISE EXCEPTION 'custody adjustment platform movements are immutable';
    END IF;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_crypto_custody_wallet_effect_guard
    BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_balance_movements
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_wallet_effect_guard();

CREATE TRIGGER trg_crypto_custody_platform_effect_guard
    BEFORE INSERT OR UPDATE OR DELETE ON reg_crypto_platform_cost_movements
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_custody_platform_effect_guard();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

DROP TRIGGER IF EXISTS trg_crypto_custody_adjustment_posting_effects ON doc_crypto_custody_adjustments;
DROP TRIGGER IF EXISTS trg_crypto_custody_wallet_effect_guard ON reg_crypto_balance_movements;
DROP TRIGGER IF EXISTS trg_crypto_custody_platform_effect_guard ON reg_crypto_platform_cost_movements;
DROP FUNCTION IF EXISTS fn_crypto_custody_adjustment_posting_effects_guard();
DROP FUNCTION IF EXISTS fn_crypto_custody_wallet_effect_guard();
DROP FUNCTION IF EXISTS fn_crypto_custody_platform_effect_guard();
DROP TABLE IF EXISTS reg_crypto_platform_cost_movements CASCADE;
DROP TABLE IF EXISTS reg_crypto_sweep_attempts CASCADE;

DROP INDEX IF EXISTS idx_crypto_sweep_lines_tx_hash_unique;

-- +goose StatementEnd
