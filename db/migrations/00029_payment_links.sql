-- +goose Up
-- Description: Payment Links — reusable payment templates for merchants

-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- ═══════════════════════════════════════════════════════════════════════════
-- Payment Links (Платёжные ссылки)
-- A payment link is a reusable template that creates invoices on demand.
-- When a customer opens the link, the system creates a fresh invoice
-- from the template (amount, token, description) and redirects to checkout.
-- ═══════════════════════════════════════════════════════════════════════════

CREATE TABLE doc_payment_links (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    deletion_mark   BOOLEAN     NOT NULL DEFAULT FALSE,
    version         INT         NOT NULL DEFAULT 1,
    merchant_id     UUID        NOT NULL,
    token_id        UUID        NOT NULL REFERENCES cat_tokens(id),
    amount          NUMERIC(78,0) NOT NULL,
    description     TEXT        NOT NULL DEFAULT '',
    public_ref      TEXT        NOT NULL,             -- URL: /pay/l/pl_<128-bit-hex>
    reusable        BOOLEAN     NOT NULL DEFAULT FALSE,
    max_uses        INT         NOT NULL DEFAULT 1,   -- 0 = unlimited (only if reusable=true)
    current_uses    INT         NOT NULL DEFAULT 0,
    status          TEXT        NOT NULL DEFAULT 'active', -- active | disabled
    ttl_minutes     INT         NOT NULL DEFAULT 60,  -- TTL for generated invoices
    created_by      UUID,                              -- platform user who created
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- CDC
    _deleted_at     TIMESTAMPTZ,
    _txid           BIGINT DEFAULT txid_current(),

    CONSTRAINT uq_payment_link_public_ref UNIQUE (public_ref),
    CONSTRAINT chk_payment_link_amount_positive CHECK (amount > 0),
    CONSTRAINT chk_payment_link_public_ref_format CHECK (public_ref ~ '^pl_[a-f0-9]{32}$'),
    CONSTRAINT chk_payment_link_max_uses_nonneg CHECK (max_uses >= 0),
    CONSTRAINT chk_payment_link_current_uses_nonneg CHECK (current_uses >= 0),
    CONSTRAINT chk_payment_link_status_valid CHECK (status IN ('active', 'disabled')),
    CONSTRAINT chk_payment_link_ttl_valid CHECK (ttl_minutes BETWEEN 5 AND 10080)
);

-- Indexes
CREATE INDEX idx_payment_links_merchant    ON doc_payment_links (merchant_id);
CREATE INDEX idx_payment_links_public_ref  ON doc_payment_links (public_ref) WHERE status = 'active';
CREATE INDEX idx_payment_links_status      ON doc_payment_links (status, merchant_id);

-- CDC
CREATE INDEX idx_doc_payment_links_txid ON doc_payment_links (_txid) WHERE _deleted_at IS NULL;

CREATE TRIGGER trg_doc_payment_links_txid
    BEFORE UPDATE ON doc_payment_links
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_doc_payment_links_soft_delete
    BEFORE UPDATE OF deletion_mark ON doc_payment_links
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

COMMENT ON TABLE doc_payment_links IS 'Платёжные ссылки — многоразовые шаблоны для создания инвойсов без API интеграции';
COMMENT ON COLUMN doc_payment_links.public_ref IS 'Opaque customer-facing checkout reference: pl_<128-bit-hex>';
COMMENT ON COLUMN doc_payment_links.reusable IS 'true = можно использовать многократно (до max_uses)';
COMMENT ON COLUMN doc_payment_links.max_uses IS 'Лимит использований (0 = безлимит). Действует только при reusable=true';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TABLE IF EXISTS doc_payment_links;
-- +goose StatementEnd
