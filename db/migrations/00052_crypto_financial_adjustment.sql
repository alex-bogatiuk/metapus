-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
CREATE TABLE doc_crypto_adjustments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    payment_id UUID NOT NULL REFERENCES doc_crypto_payments(id),
    posted_version INT NOT NULL CHECK (posted_version > 0),
    reason_code TEXT NOT NULL CHECK (length(btrim(reason_code)) >= 3),
    evidence JSONB NOT NULL CHECK (evidence <> '{}'::jsonb),
    idempotency_key TEXT NOT NULL UNIQUE,
    request_hash TEXT NOT NULL,
    maker_id UUID NOT NULL,
    checker_id UUID,
    status TEXT NOT NULL DEFAULT 'pending_approval'
        CHECK (status IN ('pending_approval','posted','rejected')),
    decision_reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decided_at TIMESTAMPTZ,
    CHECK ((status='pending_approval' AND checker_id IS NULL AND decided_at IS NULL)
        OR (status IN ('posted','rejected') AND checker_id IS NOT NULL
            AND checker_id<>maker_id AND decided_at IS NOT NULL
            AND length(btrim(decision_reason))>=3))
);
CREATE TABLE reg_crypto_adjustment_sources (
    adjustment_id UUID NOT NULL REFERENCES doc_crypto_adjustments(id),
    source_register TEXT NOT NULL
        CHECK (source_register IN ('custody','merchant','fee')),
    source_line_id UUID NOT NULL,
    PRIMARY KEY (adjustment_id,source_register,source_line_id)
);
CREATE FUNCTION fn_crypto_adjustment_guard() RETURNS TRIGGER AS $$
BEGIN
    IF (OLD.payment_id,OLD.posted_version,OLD.reason_code,OLD.evidence,
        OLD.idempotency_key,OLD.request_hash,OLD.maker_id)
       IS DISTINCT FROM
       (NEW.payment_id,NEW.posted_version,NEW.reason_code,NEW.evidence,
        NEW.idempotency_key,NEW.request_hash,NEW.maker_id) THEN
        RAISE EXCEPTION 'crypto adjustment basis and evidence are immutable';
    END IF;
    IF OLD.status<>'pending_approval' OR NEW.status='pending_approval' THEN
        RAISE EXCEPTION 'crypto adjustment decision is final';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_adjustment_guard
    BEFORE UPDATE ON doc_crypto_adjustments
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_adjustment_guard();
CREATE FUNCTION fn_crypto_adjustment_source_insert_guard() RETURNS TRIGGER AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM doc_crypto_adjustments
                   WHERE id=NEW.adjustment_id AND status='pending_approval') THEN
        RAISE EXCEPTION 'crypto adjustment sources are frozen after decision';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_crypto_adjustment_source_insert_guard
    BEFORE INSERT ON reg_crypto_adjustment_sources
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_adjustment_source_insert_guard();
CREATE TRIGGER trg_crypto_adjustment_sources_immutable
    BEFORE UPDATE OR DELETE ON reg_crypto_adjustment_sources
    FOR EACH ROW EXECUTE FUNCTION fn_crypto_financial_movement_immutable();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TABLE reg_crypto_adjustment_sources;
DROP TABLE doc_crypto_adjustments;
DROP FUNCTION fn_crypto_adjustment_guard();
DROP FUNCTION fn_crypto_adjustment_source_insert_guard();
-- +goose StatementEnd
