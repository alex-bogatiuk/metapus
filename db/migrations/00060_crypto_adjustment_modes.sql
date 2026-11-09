-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

ALTER TABLE doc_crypto_adjustments
    ALTER COLUMN payment_id DROP NOT NULL,
    ALTER COLUMN posted_version DROP NOT NULL,
    ADD COLUMN mode TEXT NOT NULL DEFAULT 'payment_reversal'
        CHECK (mode IN ('payment_reversal','wallet_balance')),
    ADD COLUMN wallet_id UUID REFERENCES cat_wallets(id),
    ADD COLUMN token_id UUID REFERENCES cat_tokens(id),
    ADD COLUMN direction TEXT CHECK (direction IN ('receipt','expense')),
    ADD COLUMN amount NUMERIC(78,0) CHECK (amount > 0),
    ADD COLUMN decision_mode TEXT NOT NULL DEFAULT 'two_person'
        CHECK (decision_mode IN ('two_person','direct'));

-- Replace the original anonymous status/checker constraint without relying on
-- PostgreSQL's generated constraint suffix.
DO $$
DECLARE constraint_name TEXT;
BEGIN
    FOR constraint_name IN
        SELECT conname FROM pg_constraint
        WHERE conrelid='doc_crypto_adjustments'::regclass AND contype='c'
          AND pg_get_constraintdef(oid) LIKE '%checker_id <> maker_id%'
    LOOP
        EXECUTE format('ALTER TABLE doc_crypto_adjustments DROP CONSTRAINT %I', constraint_name);
    END LOOP;
END;
$$;
ALTER TABLE doc_crypto_adjustments
    ADD CONSTRAINT chk_crypto_adjustment_mode_basis CHECK (
        (mode='payment_reversal' AND payment_id IS NOT NULL AND posted_version>0
         AND wallet_id IS NULL AND token_id IS NULL AND direction IS NULL AND amount IS NULL)
        OR (mode='wallet_balance' AND payment_id IS NULL AND posted_version IS NULL
            AND wallet_id IS NOT NULL AND token_id IS NOT NULL
            AND direction IS NOT NULL AND amount>0)),
    ADD CONSTRAINT chk_crypto_adjustment_decision CHECK (
        (status='pending_approval' AND checker_id IS NULL AND decided_at IS NULL)
        OR (status IN ('posted','rejected') AND checker_id IS NOT NULL
            AND decided_at IS NOT NULL AND length(btrim(decision_reason))>=3
            AND (checker_id<>maker_id OR (status='posted' AND decision_mode='direct'))));

ALTER TABLE reg_crypto_adjustment_sources
    ADD COLUMN amount NUMERIC(78,0) CHECK (amount>0);

-- The old unique indexes prevented repeated partial reversal. A trigger takes
-- a row lock on each immutable source and checks the cumulative allocation.
DROP INDEX uq_crypto_balance_reversal_line;
DROP INDEX uq_crypto_merchant_reversal_line;
DROP INDEX uq_crypto_fee_reversal_line;
CREATE INDEX idx_crypto_balance_reversal_line
    ON reg_crypto_balance_movements(reversal_of_line_id) WHERE reversal_of_line_id IS NOT NULL;
CREATE INDEX idx_crypto_merchant_reversal_line
    ON reg_crypto_merchant_balance_movements(reversal_of_line_id) WHERE reversal_of_line_id IS NOT NULL;
CREATE INDEX idx_crypto_fee_reversal_line
    ON reg_crypto_fee_movements(reversal_of_line_id) WHERE reversal_of_line_id IS NOT NULL;
CREATE UNIQUE INDEX uq_crypto_balance_ledger_reversal
    ON reg_crypto_balance_movements(reversal_of_line_id)
    WHERE recorder_type='CryptoLedgerReversal' AND reversal_of_line_id IS NOT NULL;
CREATE UNIQUE INDEX uq_crypto_merchant_ledger_reversal
    ON reg_crypto_merchant_balance_movements(reversal_of_line_id)
    WHERE recorder_type='CryptoLedgerReversal' AND reversal_of_line_id IS NOT NULL;
CREATE UNIQUE INDEX uq_crypto_fee_ledger_reversal
    ON reg_crypto_fee_movements(reversal_of_line_id)
    WHERE recorder_type='CryptoLedgerReversal' AND reversal_of_line_id IS NOT NULL;

CREATE FUNCTION fn_crypto_payment_reversal_cap() RETURNS TRIGGER AS $$
DECLARE source_amount NUMERIC(78,0);
DECLARE used_amount NUMERIC(78,0);
DECLARE available NUMERIC(78,0);
DECLARE already_recorded BOOLEAN;
BEGIN
    IF NEW.reversal_of_line_id IS NULL THEN RETURN NEW; END IF;
    EXECUTE format('SELECT amount FROM %I WHERE line_id=$1 FOR UPDATE', TG_TABLE_NAME)
        INTO source_amount USING NEW.reversal_of_line_id;
    IF source_amount IS NULL THEN
        RAISE EXCEPTION 'reversal source is missing' USING ERRCODE='23514';
    END IF;
    EXECUTE format('SELECT COALESCE(SUM(amount),0) FROM %I WHERE reversal_of_line_id=$1', TG_TABLE_NAME)
        INTO used_amount USING NEW.reversal_of_line_id;
    available := source_amount-used_amount;
    IF NEW.recorder_type IN ('CryptoLedgerReversal','CryptoPaymentReorg') THEN
        EXECUTE format('SELECT EXISTS (SELECT 1 FROM %I WHERE reversal_of_line_id=$1 AND recorder_type=$2)', TG_TABLE_NAME)
            INTO already_recorded USING NEW.reversal_of_line_id,NEW.recorder_type;
        IF already_recorded OR available<=0 THEN RETURN NULL; END IF;
        IF NEW.amount>available THEN NEW.amount:=available; END IF;
    ELSIF NEW.amount>available THEN
        RAISE EXCEPTION 'reversal exceeds source amount' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_balance_reversal_cap BEFORE INSERT ON reg_crypto_balance_movements
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_payment_reversal_cap();
CREATE TRIGGER trg_crypto_merchant_reversal_cap BEFORE INSERT ON reg_crypto_merchant_balance_movements
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_payment_reversal_cap();
CREATE TRIGGER trg_crypto_fee_reversal_cap BEFORE INSERT ON reg_crypto_fee_movements
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_payment_reversal_cap();

-- A wallet correction has a separate gain/loss account. It never mutates
-- merchant liabilities or platform fee revenue.
CREATE TABLE reg_crypto_wallet_variance_movements (
    line_id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    adjustment_id UUID NOT NULL UNIQUE REFERENCES doc_crypto_adjustments(id),
    wallet_id UUID NOT NULL REFERENCES cat_wallets(id),
    token_id UUID NOT NULL REFERENCES cat_tokens(id),
    variance_type TEXT NOT NULL CHECK (variance_type IN ('surplus','shortage')),
    amount NUMERIC(78,0) NOT NULL CHECK (amount>0),
    period TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TRIGGER trg_crypto_wallet_variance_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_wallet_variance_movements
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_financial_movement_immutable();

CREATE OR REPLACE FUNCTION fn_crypto_adjustment_guard() RETURNS TRIGGER AS $$
BEGIN
    IF (OLD.payment_id,OLD.posted_version,OLD.reason_code,OLD.evidence,
        OLD.idempotency_key,OLD.request_hash,OLD.maker_id,OLD.mode,
        OLD.wallet_id,OLD.token_id,OLD.direction,OLD.amount,OLD.decision_mode)
       IS DISTINCT FROM
       (NEW.payment_id,NEW.posted_version,NEW.reason_code,NEW.evidence,
        NEW.idempotency_key,NEW.request_hash,NEW.maker_id,NEW.mode,
        NEW.wallet_id,NEW.token_id,NEW.direction,NEW.amount,NEW.decision_mode) THEN
        RAISE EXCEPTION 'crypto adjustment basis and evidence are immutable';
    END IF;
    IF OLD.status<>'pending_approval' OR NEW.status='pending_approval' THEN
        RAISE EXCEPTION 'crypto adjustment decision is final';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

INSERT INTO auth_permissions (code,name,description,resource,action)
VALUES ('crypto_adjustment:post_direct','Прямое проведение криптокорректировок',
        'Post own crypto adjustment after fresh step-up','crypto_adjustment','post_direct')
ON CONFLICT (code) DO NOTHING;
INSERT INTO auth_role_permissions(role_id,permission_id)
SELECT r.id,p.id FROM auth_roles r CROSS JOIN auth_permissions p
WHERE r.code='admin' AND p.code='crypto_adjustment:post_direct'
ON CONFLICT DO NOTHING;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'crypto adjustment migration 60 is forward-only; restore a verified database snapshot instead';
END $$;
-- +goose StatementEnd
