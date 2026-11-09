-- +goose Up
-- Description: Production merchant webhook endpoint registry, immutable events and delivery audit.

-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

CREATE TABLE cat_merchant_webhook_endpoints (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    merchant_id UUID NOT NULL REFERENCES cat_merchants(id),
    name VARCHAR(100) NOT NULL,
    url TEXT NOT NULL,
    enabled_events TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    secret_ref TEXT NOT NULL,
    secret_version INT NOT NULL DEFAULT 1,
    previous_secret_ref TEXT NOT NULL DEFAULT '',
    previous_secret_version INT,
    previous_secret_valid_until TIMESTAMPTZ,
    version INT NOT NULL DEFAULT 1,
    deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
    attributes JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    _txid BIGINT NOT NULL DEFAULT txid_current(),
    _deleted_at TIMESTAMPTZ,
    CONSTRAINT chk_merchant_webhook_endpoint_name CHECK (BTRIM(name) <> ''),
    CONSTRAINT chk_merchant_webhook_endpoint_url CHECK (url ~ '^https://'),
    CONSTRAINT chk_merchant_webhook_endpoint_secret_ref CHECK (BTRIM(secret_ref) <> ''),
    CONSTRAINT chk_merchant_webhook_endpoint_secret_version CHECK (secret_version >= 1),
    CONSTRAINT chk_merchant_webhook_endpoint_version CHECK (version >= 1),
    CONSTRAINT chk_merchant_webhook_endpoint_attributes CHECK (jsonb_typeof(attributes) = 'object'),
    CONSTRAINT chk_merchant_webhook_endpoint_previous_secret CHECK (
        (previous_secret_ref = '' AND previous_secret_version IS NULL AND previous_secret_valid_until IS NULL)
        OR
        (previous_secret_ref <> '' AND previous_secret_version >= 1 AND previous_secret_valid_until IS NOT NULL)
    )
);
CREATE UNIQUE INDEX uq_merchant_webhook_default
    ON cat_merchant_webhook_endpoints(merchant_id)
    WHERE is_default AND is_active AND deletion_mark = FALSE;
CREATE INDEX idx_merchant_webhook_endpoints_merchant
    ON cat_merchant_webhook_endpoints(merchant_id, created_at DESC)
    WHERE deletion_mark = FALSE;
CREATE INDEX idx_merchant_webhook_endpoints_txid
    ON cat_merchant_webhook_endpoints(_txid)
    WHERE _deleted_at IS NULL;

CREATE TRIGGER trg_merchant_webhook_endpoints_updated_at
    BEFORE UPDATE ON cat_merchant_webhook_endpoints
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER trg_merchant_webhook_endpoints_txid
    BEFORE UPDATE ON cat_merchant_webhook_endpoints
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();
CREATE TRIGGER trg_merchant_webhook_endpoints_soft_delete
    BEFORE UPDATE OF deletion_mark ON cat_merchant_webhook_endpoints
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

CREATE TABLE sys_merchant_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    event_id TEXT NOT NULL UNIQUE,
    merchant_id UUID NOT NULL REFERENCES cat_merchants(id),
    aggregate_type TEXT NOT NULL,
    aggregate_id UUID NOT NULL,
    invoice_id UUID REFERENCES doc_crypto_invoices(id),
    event_type TEXT NOT NULL,
    api_version TEXT NOT NULL,
    resource_version INT NOT NULL,
    causation_key TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT uq_merchant_event_causation UNIQUE(merchant_id, causation_key),
    CONSTRAINT chk_merchant_event_resource_version CHECK (resource_version >= 1),
    CONSTRAINT chk_merchant_event_payload CHECK (jsonb_typeof(payload) = 'object')
);
CREATE INDEX idx_merchant_events_merchant_created
    ON sys_merchant_events(merchant_id, created_at DESC);
CREATE INDEX idx_merchant_events_invoice_created
    ON sys_merchant_events(invoice_id, created_at DESC) WHERE invoice_id IS NOT NULL;

CREATE TABLE sys_webhook_delivery_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    event_id TEXT NOT NULL REFERENCES sys_merchant_events(event_id),
    endpoint_id UUID REFERENCES cat_merchant_webhook_endpoints(id) ON DELETE SET NULL,
    merchant_id UUID NOT NULL REFERENCES cat_merchants(id),
    snapshot_url TEXT NOT NULL,
    secret_ref TEXT NOT NULL,
    secret_version INT NOT NULL,
    previous_secret_ref TEXT NOT NULL DEFAULT '',
    previous_secret_valid_until TIMESTAMPTZ,
    attempt_count INT NOT NULL DEFAULT 0,
    claim_token UUID,
    lease_until TIMESTAMPTZ,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','processing','succeeded','failed','cancelled')),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deadline_at TIMESTAMPTZ NOT NULL,
    last_error TEXT NOT NULL DEFAULT '',
    manual BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_webhook_delivery_job_attempt CHECK (attempt_count >= 0),
    CONSTRAINT chk_webhook_delivery_job_deadline CHECK (deadline_at >= created_at)
);
CREATE UNIQUE INDEX uq_webhook_delivery_job_automatic
    ON sys_webhook_delivery_jobs(event_id, endpoint_id) WHERE manual = FALSE;
CREATE UNIQUE INDEX uq_webhook_delivery_jobs_processing_merchant
    ON sys_webhook_delivery_jobs(merchant_id) WHERE status='processing';
CREATE INDEX idx_webhook_delivery_jobs_expired
    ON sys_webhook_delivery_jobs(lease_until) WHERE status='processing';
CREATE INDEX idx_webhook_delivery_jobs_last_attempt
    ON sys_webhook_delivery_jobs(merchant_id,updated_at DESC) WHERE attempt_count>0;
CREATE INDEX idx_webhook_delivery_jobs_due
    ON sys_webhook_delivery_jobs(next_attempt_at) WHERE status = 'pending';

CREATE TABLE sys_webhook_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    invoice_id UUID REFERENCES doc_crypto_invoices(id),
    merchant_id UUID NOT NULL REFERENCES cat_merchants(id),
    event_type TEXT NOT NULL,
    webhook_url TEXT NOT NULL,
    event_id TEXT NOT NULL,
    delivery_id TEXT NOT NULL UNIQUE,
    status_code INT,
    response_time_ms INT,
    attempt INT NOT NULL DEFAULT 1,
    error_message TEXT,
    error_class TEXT NOT NULL DEFAULT '',
    response_body TEXT NOT NULL DEFAULT '',
    next_retry_at TIMESTAMPTZ,
    request_body JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_webhook_delivery_attempt CHECK (attempt >= 1),
    CONSTRAINT chk_webhook_delivery_status CHECK (status_code IS NULL OR status_code BETWEEN 100 AND 599),
    CONSTRAINT chk_webhook_delivery_response_time CHECK (response_time_ms IS NULL OR response_time_ms >= 0)
);
CREATE INDEX idx_webhook_del_invoice
    ON sys_webhook_deliveries(invoice_id) WHERE invoice_id IS NOT NULL;
CREATE INDEX idx_webhook_del_merchant
    ON sys_webhook_deliveries(merchant_id, created_at DESC);
CREATE INDEX idx_webhook_del_created
    ON sys_webhook_deliveries(created_at DESC);
CREATE INDEX idx_webhook_del_event
    ON sys_webhook_deliveries(event_id, created_at DESC);

COMMENT ON TABLE sys_webhook_deliveries IS
    'Immutable audit trail: every webhook HTTP attempt is stored as a separate row';
COMMENT ON COLUMN sys_webhook_deliveries.invoice_id IS
    'NULL for endpoint test events';
COMMENT ON COLUMN sys_webhook_deliveries.event_id IS
    'Stable merchant event identifier sent in Metapus-Event-Id';
COMMENT ON COLUMN sys_webhook_deliveries.delivery_id IS
    'Unique HTTP attempt identifier sent in Metapus-Delivery-Id';

CREATE TABLE sys_webhook_secret_retirements (
    secret_ref TEXT PRIMARY KEY,
    retire_after TIMESTAMPTZ NOT NULL,
    claimed_until TIMESTAMPTZ,
    attempt_count INT NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_webhook_secret_retirement_ref CHECK (BTRIM(secret_ref) <> '')
);
CREATE INDEX idx_webhook_secret_retirements_due
    ON sys_webhook_secret_retirements(retire_after, claimed_until);

CREATE TABLE sys_invoice_reconciliation_drift (
    invoice_id UUID PRIMARY KEY REFERENCES doc_crypto_invoices(id),
    consecutive_cycles INT NOT NULL DEFAULT 1 CHECK (consecutive_cycles >= 1),
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMPTZ,
    CONSTRAINT chk_invoice_reconciliation_details CHECK (jsonb_typeof(details) = 'object')
);

ALTER TABLE reg_crypto_security_incidents
    ADD COLUMN invoice_id UUID REFERENCES doc_crypto_invoices(id);
ALTER TABLE reg_crypto_security_incidents
    DROP CONSTRAINT reg_crypto_security_incidents_incident_kind_check;
ALTER TABLE reg_crypto_security_incidents
    ADD CONSTRAINT reg_crypto_security_incidents_incident_kind_check
    CHECK (incident_kind IN ('posted_payment_reorg','deep_reorg','nonce_divergence','invoice_projection_drift'));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

ALTER TABLE reg_crypto_security_incidents
    DROP CONSTRAINT reg_crypto_security_incidents_incident_kind_check;
ALTER TABLE reg_crypto_security_incidents
    ADD CONSTRAINT reg_crypto_security_incidents_incident_kind_check
    CHECK (incident_kind IN ('posted_payment_reorg','deep_reorg','nonce_divergence'));
ALTER TABLE reg_crypto_security_incidents DROP COLUMN invoice_id;

DROP TABLE sys_invoice_reconciliation_drift;
DROP TABLE sys_webhook_secret_retirements;
DROP TABLE sys_webhook_deliveries;
DROP TABLE sys_webhook_delivery_jobs;
DROP TABLE sys_merchant_events;
DROP TABLE cat_merchant_webhook_endpoints;

-- +goose StatementEnd
