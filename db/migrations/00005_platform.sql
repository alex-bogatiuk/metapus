-- Platform services: settings, service and integration accounts, automation
-- (rules, channels, subscribers, history, files), notifications, report variants,
-- list views and worker jobs.
-- Consolidated schema: each object is declared once in its final form.

-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- Function bodies are validated when first executed; definitions precede the tables they use.
SET LOCAL check_function_bodies = off;


-- ---- Types ----
CREATE TYPE worker_job_status AS ENUM ('running', 'success', 'error', 'skipped');


-- ---- Tables ----
-- sys_settings: single-row tenant-level configuration.
-- Only system-wide settings remain here.
-- Organization-specific settings are in cat_organizations.
CREATE TABLE sys_settings (
    singleton    BOOLEAN      PRIMARY KEY DEFAULT TRUE CHECK (singleton = TRUE),

    -- General
    general      JSONB        NOT NULL DEFAULT '{"timezone": "UTC"}',
    numbering    JSONB        NOT NULL DEFAULT '{"autoNumbering": true, "numberPrefix": ""}',
    performance  JSONB        NOT NULL DEFAULT '{"batchConcurrency": 5}',

    -- Exchange rates
    exchange_rates JSONB        NOT NULL DEFAULT '{"sourceId": null}',

    -- Metadata
    version      INT          NOT NULL DEFAULT 1,
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_by   UUID
);
COMMENT ON TABLE  sys_settings              IS 'Single-row tenant settings (system-wide only)';
COMMENT ON COLUMN sys_settings.singleton    IS 'Always TRUE — enforces exactly one row via CHECK constraint';
COMMENT ON COLUMN sys_settings.general      IS 'General settings: timezone (IANA), display preferences';
COMMENT ON COLUMN sys_settings.numbering    IS 'Document numbering: autoNumbering, numberPrefix';
COMMENT ON COLUMN sys_settings.performance  IS 'Batch concurrency, processing limits';
COMMENT ON COLUMN sys_settings.exchange_rates IS 'Exchange rates feed settings: sourceId';
COMMENT ON COLUMN sys_settings.version      IS 'Optimistic locking version — incremented on each update';

-- sys_automation_accounts: centralized sender accounts with encrypted credentials.
-- Replaces old sys_service_accounts. Bot Token, SMTP password, API Key — stored once.
CREATE TABLE sys_automation_accounts (
    id                UUID          PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    name              VARCHAR(255)  NOT NULL,
    account_type      VARCHAR(30)   NOT NULL,
    -- telegram | email | webhook | rocketchat | slack

    -- Non-secret configuration depending on account_type:
    -- telegram: { "bot_username": "@metapus_bot" }
    -- email:    { "smtp_host": "smtp.gmail.com", "smtp_port": 587,
    --             "from_address": "noreply@c.com", "from_name": "Metapus", "tls": true }
    -- webhook:  { "base_url": "https://api.example.com", "default_headers": {...} }
    config            JSONB         NOT NULL DEFAULT '{}',

    -- Secrets encrypted with AES-256-GCM at application level (Go).
    -- telegram: bot_token
    -- email:    smtp_password
    -- webhook:  api_key / bearer_token
    credentials_enc   BYTEA,

    organization_id   UUID          REFERENCES cat_organizations(id) ON DELETE SET NULL,
    is_active         BOOLEAN       NOT NULL DEFAULT TRUE,
    status            VARCHAR(20)   NOT NULL DEFAULT 'active',
    last_error        TEXT,
    last_success_at   TIMESTAMPTZ,

    deletion_mark     BOOLEAN       NOT NULL DEFAULT FALSE,
    version           INT           NOT NULL DEFAULT 1,
    created_at        TIMESTAMPTZ   NOT NULL DEFAULT statement_timestamp(),
    updated_at        TIMESTAMPTZ   NOT NULL DEFAULT statement_timestamp(),
    _deleted_at       TIMESTAMPTZ,
    _txid             BIGINT        DEFAULT txid_current()
);
CREATE INDEX idx_sys_auto_accounts_type
    ON sys_automation_accounts(account_type, status);
CREATE INDEX idx_sys_auto_accounts_txid
    ON sys_automation_accounts(_txid);
COMMENT ON TABLE sys_automation_accounts IS
    'Centralized sender accounts with encrypted credentials (Bot Token, SMTP password, API Key)';
COMMENT ON COLUMN sys_automation_accounts.credentials_enc IS
    'AES-256-GCM encrypted in Go app layer. Never exposed in API responses.';

-- Tenant-local integration accounts and ordered pools. No plaintext credentials.
CREATE TABLE cat_integration_accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    code VARCHAR(50) NOT NULL, name VARCHAR(255) NOT NULL,
    parent_id UUID, is_folder BOOLEAN NOT NULL DEFAULT FALSE CHECK (NOT is_folder),
    deletion_mark BOOLEAN NOT NULL DEFAULT FALSE, version INT NOT NULL DEFAULT 1,
    attributes JSONB NOT NULL DEFAULT '{}', _deleted_at TIMESTAMPTZ,
    _txid BIGINT DEFAULT txid_current(), created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    provider TEXT NOT NULL CHECK(provider IN ('alchemy','coingecko_demo','tron_api','chainstack','getblock','infura','drpc','telegram','smtp','resource_api','getblock_net','testnet_aml')),
    external_account_ref VARCHAR(255) NOT NULL DEFAULT '', is_active BOOLEAN NOT NULL DEFAULT TRUE,
    reset_day INT NOT NULL DEFAULT 1 CHECK (reset_day BETWEEN 1 AND 31),
    reset_timezone TEXT NOT NULL DEFAULT 'UTC', credentials_enc BYTEA,
    credentials_version INT NOT NULL DEFAULT 0 CHECK (credentials_version >= 0),
    credentials_updated_at TIMESTAMPTZ,
    credentials_expires_at TIMESTAMPTZ, manual_period_started_at TIMESTAMPTZ,
    manual_period_version BIGINT NOT NULL DEFAULT 0,
    CONSTRAINT cat_integration_accounts_manual_period_check CHECK (
      (provider='drpc' AND credentials_expires_at IS NOT NULL AND manual_period_started_at IS NOT NULL
       AND manual_period_version>0 AND credentials_expires_at>manual_period_started_at)
      OR (provider<>'drpc' AND credentials_expires_at IS NULL AND manual_period_started_at IS NULL AND manual_period_version=0)),
    CHECK (parent_id IS NULL)
);
CREATE UNIQUE INDEX uq_integration_accounts_external ON cat_integration_accounts(provider,external_account_ref)
 WHERE external_account_ref <> '' AND NOT deletion_mark;
CREATE UNIQUE INDEX uq_cat_integration_accounts_code ON cat_integration_accounts(code) WHERE NOT deletion_mark;
CREATE INDEX idx_cat_integration_accounts_name_id ON cat_integration_accounts(name,id);
CREATE INDEX idx_cat_integration_accounts_txid ON cat_integration_accounts(_txid) WHERE _deleted_at IS NULL;

CREATE TABLE cat_integration_pools (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    code VARCHAR(50) NOT NULL, name VARCHAR(255) NOT NULL,
    parent_id UUID, is_folder BOOLEAN NOT NULL DEFAULT FALSE CHECK (NOT is_folder),
    deletion_mark BOOLEAN NOT NULL DEFAULT FALSE, version INT NOT NULL DEFAULT 1,
    attributes JSONB NOT NULL DEFAULT '{}', _deleted_at TIMESTAMPTZ,
    _txid BIGINT DEFAULT txid_current(), created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    provider TEXT NOT NULL CHECK(provider IN ('alchemy','coingecko_demo','tron_api','chainstack','getblock','infura','drpc','telegram','smtp','resource_api','getblock_net','testnet_aml')), is_active BOOLEAN NOT NULL DEFAULT TRUE,
    CHECK (parent_id IS NULL)
);
CREATE UNIQUE INDEX uq_cat_integration_pools_code ON cat_integration_pools(code) WHERE NOT deletion_mark;
CREATE INDEX idx_cat_integration_pools_name_id ON cat_integration_pools(name,id);
CREATE INDEX idx_cat_integration_pools_txid ON cat_integration_pools(_txid) WHERE _deleted_at IS NULL;

CREATE TABLE sys_integration_pool_accounts (
    pool_id UUID NOT NULL REFERENCES cat_integration_pools(id) ON DELETE CASCADE,
    account_id UUID NOT NULL REFERENCES cat_integration_accounts(id) ON DELETE RESTRICT,
    position INT NOT NULL CHECK (position >= 0), PRIMARY KEY(pool_id,account_id), UNIQUE(pool_id,position)
);
CREATE INDEX idx_integration_pool_account ON sys_integration_pool_accounts(account_id);

CREATE TABLE sys_integration_account_state (
    account_id UUID PRIMARY KEY REFERENCES cat_integration_accounts(id) ON DELETE CASCADE,
    period_start TIMESTAMPTZ NOT NULL, period_end TIMESTAMPTZ NOT NULL,
    exhausted_until TIMESTAMPTZ, throttled_until TIMESTAMPTZ, probe_lease_until TIMESTAMPTZ,
    invalid_credentials_version INT NOT NULL DEFAULT -1, invalid_slots TEXT[] NOT NULL DEFAULT '{}',
    manual_period_version BIGINT NOT NULL DEFAULT 0 CHECK (manual_period_version>=0),
    last_error TEXT NOT NULL DEFAULT '', updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), version BIGINT NOT NULL DEFAULT 1
);

CREATE TABLE sys_integration_pool_state (
    pool_id UUID PRIMARY KEY REFERENCES cat_integration_pools(id) ON DELETE CASCADE,
    account_id UUID REFERENCES cat_integration_accounts(id) ON DELETE RESTRICT,
    position INT NOT NULL DEFAULT -1, selection_version BIGINT NOT NULL DEFAULT 0,
    reason TEXT NOT NULL DEFAULT '', updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- sys_automation_rules: event → condition → reaction automation rules.
CREATE TABLE sys_automation_rules (
    id                 UUID          PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    name               VARCHAR(255)  NOT NULL,
    description        TEXT,

    -- TRIGGER
    trigger_type       VARCHAR(30)   NOT NULL DEFAULT 'entity_event',
    -- entity_event | business_event | scheduled | incoming_webhook
    event_type         VARCHAR(100)  NOT NULL,
    -- Action: posted, unposted, created, updated, deleted; for scheduled: cron:0 9 * * 1-5
    target_entities    TEXT[],
    -- Array of entity keys: ['crypto_invoice','crypto_payment']. NULL = any entity (wildcard).

    -- CONDITION
    condition_cel      TEXT,

    -- REACTION
    reaction_type      VARCHAR(30)   NOT NULL DEFAULT 'notify' CHECK (reaction_type IN ('notify', 'webhook_call', 'generate_report')),
    -- notify | webhook_call | generate_report
    notif_severity     VARCHAR(16)   NOT NULL DEFAULT 'info' CHECK (notif_severity IN ('info', 'warning', 'error', 'success')),
    message_format     VARCHAR(20)   NOT NULL DEFAULT 'text',
    -- text | markdown | html
    action_template    TEXT          NOT NULL DEFAULT '',

    -- REPORT (only for reaction_type = 'generate_report')
    report_config      JSONB,
    -- { "datasetKey", "variantId", "periodType", "customDays", "extraFilters", "fileFormat", "includeEmptyReport", "timezone" }


    -- SETTINGS
    priority           INT           NOT NULL DEFAULT 0,
    max_retries        INT           NOT NULL DEFAULT 3,
    cooldown_seconds   INT           NOT NULL DEFAULT 0,
    organization_id    UUID          REFERENCES cat_organizations(id) ON DELETE CASCADE,
    is_active          BOOLEAN       NOT NULL DEFAULT TRUE,

    -- STATS (updated atomically by Engine)
    execution_count    INT           NOT NULL DEFAULT 0,
    error_count        INT           NOT NULL DEFAULT 0,
    last_executed_at   TIMESTAMPTZ,

    -- Standard Metapus columns
    deletion_mark      BOOLEAN       NOT NULL DEFAULT FALSE,
    version            INT           NOT NULL DEFAULT 1,
    created_at         TIMESTAMPTZ   NOT NULL DEFAULT statement_timestamp(),
    updated_at         TIMESTAMPTZ   NOT NULL DEFAULT statement_timestamp(),
    _deleted_at        TIMESTAMPTZ,
    _txid              BIGINT        DEFAULT txid_current()
);
-- Active rules by event type — main query for Engine.Evaluate
CREATE INDEX idx_sys_auto_rules_event_type
    ON sys_automation_rules(event_type)
    WHERE is_active = TRUE AND deletion_mark = FALSE;
-- GIN index for array matching: 'crypto_invoice' = ANY(target_entities)
CREATE INDEX idx_sys_auto_rules_target_entities
    ON sys_automation_rules USING GIN(target_entities)
    WHERE is_active = TRUE AND deletion_mark = FALSE;
-- Scheduled rules lookup
CREATE INDEX idx_sys_auto_rules_trigger_type
    ON sys_automation_rules(trigger_type)
    WHERE is_active = TRUE AND deletion_mark = FALSE;
CREATE INDEX idx_sys_auto_rules_txid
    ON sys_automation_rules(_txid);
COMMENT ON TABLE sys_automation_rules IS 'Event-driven automation rules with CEL conditions and pluggable reactions';
COMMENT ON COLUMN sys_automation_rules.event_type IS 'Action: posted, unposted, created, updated, deleted; for scheduled: cron:0 9 * * 1-5';
COMMENT ON COLUMN sys_automation_rules.target_entities IS 'Array of entity keys. NULL = wildcard (matches any entity)';
COMMENT ON COLUMN sys_automation_rules.condition_cel IS 'CEL expression returning boolean. If null/empty → always true';

-- Persisted cursors are initialized at first activation. Existing history is
-- never replayed on the first rollout. Policy can be set per rule: latest/all/skip.
CREATE TABLE sys_automation_schedules (
 rule_id UUID PRIMARY KEY REFERENCES sys_automation_rules(id) ON DELETE CASCADE,
 expression TEXT NOT NULL,
 next_run_at TIMESTAMPTZ NOT NULL,
 catch_up_policy TEXT NOT NULL DEFAULT 'latest' CHECK(catch_up_policy IN ('latest','all','skip')),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 _txid BIGINT NOT NULL DEFAULT txid_current(),
 _deleted_at TIMESTAMPTZ
);

CREATE TABLE sys_automation_runs (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
 rule_id UUID NOT NULL REFERENCES sys_automation_rules(id) ON DELETE CASCADE,
 expression TEXT NOT NULL,
 scheduled_for TIMESTAMPTZ NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','processing','succeeded','failed','skipped')),
 attempt INT NOT NULL DEFAULT 0,
 max_attempts INT NOT NULL DEFAULT 4 CHECK(max_attempts>0),
 next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 claim_token UUID,
 lease_until TIMESTAMPTZ,
 last_error TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 _txid BIGINT NOT NULL DEFAULT txid_current(),
 _deleted_at TIMESTAMPTZ,
 UNIQUE(rule_id,scheduled_for)
);
CREATE UNIQUE INDEX uq_automation_runs_processing_rule ON sys_automation_runs(rule_id) WHERE status='processing';
CREATE INDEX idx_automation_runs_due ON sys_automation_runs(next_attempt_at,scheduled_for) WHERE status='pending';
CREATE INDEX idx_automation_runs_expired ON sys_automation_runs(lease_until) WHERE status='processing';

-- sys_automation_channels: delivery destinations that reference a centralized Account.
-- One Account (Bot Token) → many Channels (different Chat IDs).
CREATE TABLE sys_automation_channels (
    id                UUID          PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    name              VARCHAR(255)  NOT NULL,

    account_id        UUID          NOT NULL REFERENCES sys_automation_accounts(id) ON DELETE RESTRICT,
    -- RESTRICT: cannot delete account while channels exist

    -- Destination config depending on account_type:
    -- telegram: { "chat_id": "-1001234567890", "parse_mode": "Markdown", "thread_id": 123 }
    -- email:    { "to": ["user@company.com"], "cc": [], "subject_prefix": "[ERP]" }
    -- webhook:  { "url": "/hook/goods-receipt", "method": "POST", "headers": {...} }
    destination       JSONB         NOT NULL DEFAULT '{}',

    is_active         BOOLEAN       NOT NULL DEFAULT TRUE,

    deletion_mark     BOOLEAN       NOT NULL DEFAULT FALSE,
    version           INT           NOT NULL DEFAULT 1,
    created_at        TIMESTAMPTZ   NOT NULL DEFAULT statement_timestamp(),
    updated_at        TIMESTAMPTZ   NOT NULL DEFAULT statement_timestamp(),
    _deleted_at       TIMESTAMPTZ,
    _txid             BIGINT        DEFAULT txid_current()
);
CREATE INDEX idx_sys_auto_channels_account
    ON sys_automation_channels(account_id);
CREATE INDEX idx_sys_auto_channels_txid
    ON sys_automation_channels(_txid);
COMMENT ON TABLE sys_automation_channels IS
    'Delivery destinations (Telegram chat, email address, webhook URL). References Account for credentials.';
COMMENT ON COLUMN sys_automation_channels.destination IS
    'Channel-specific config: chat_id for Telegram, to[] for Email, url for Webhook';

-- sys_notifications: in-app notification messages for users.
CREATE TABLE sys_notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    user_id UUID NOT NULL REFERENCES auth_users(id) ON DELETE CASCADE,
    title VARCHAR(255) NOT NULL,
    message TEXT NOT NULL,
    severity VARCHAR(16) NOT NULL DEFAULT 'info' CHECK (severity IN ('info', 'warning', 'error', 'success')),
    link VARCHAR(255),
    is_read BOOLEAN NOT NULL DEFAULT false,
    attributes JSONB DEFAULT '{}'::jsonb,
    
    version INT NOT NULL DEFAULT 1,
    deletion_mark BOOLEAN NOT NULL DEFAULT false,
    
    created_at TIMESTAMPTZ NOT NULL DEFAULT statement_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT statement_timestamp(),
    _deleted_at TIMESTAMPTZ,
    _txid BIGINT DEFAULT txid_current()
);
CREATE INDEX idx_sys_notifications_user_unread ON sys_notifications(user_id) WHERE is_read = false AND deletion_mark = false;
CREATE INDEX idx_sys_notifications_created_at_desc ON sys_notifications(created_at DESC);
CREATE INDEX idx_sys_notifications_txid ON sys_notifications(_txid);

-- sys_automation_subscribers: polymorphic binding between rules and delivery targets.
-- Acts as a "table part" (табличная часть) of automation rules.
CREATE TABLE sys_automation_subscribers (
    id                UUID          PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    rule_id           UUID          NOT NULL REFERENCES sys_automation_rules(id) ON DELETE CASCADE,

    subscriber_type   VARCHAR(30)   NOT NULL,
    -- channel   → deliver via external channel (Telegram chat, email, webhook)
    -- user      → deliver to specific user (UI notification or email)
    -- role      → deliver to all users with this role
    -- doc_field → deliver to user ID extracted from document field

    -- Polymorphic reference: exactly ONE is populated based on subscriber_type
    channel_id        UUID          REFERENCES sys_automation_channels(id) ON DELETE RESTRICT,
    user_id           UUID          REFERENCES auth_users(id) ON DELETE CASCADE,
    role_name         VARCHAR(100),
    doc_field_path    VARCHAR(255),

    -- Delivery method for user/role/doc_field subscribers (ignored for channel type)
    delivery_method   VARCHAR(30)   NOT NULL DEFAULT 'ui_notification',
    -- ui_notification | email

    -- Sort order within rule
    idx               INT           NOT NULL DEFAULT 0,

    CONSTRAINT chk_subscriber_ref CHECK (
        (subscriber_type = 'channel'   AND channel_id IS NOT NULL) OR
        (subscriber_type = 'user'      AND user_id IS NOT NULL) OR
        (subscriber_type = 'role'      AND role_name IS NOT NULL) OR
        (subscriber_type = 'doc_field' AND doc_field_path IS NOT NULL)
    )
);
CREATE INDEX idx_sys_auto_subscribers_rule
    ON sys_automation_subscribers(rule_id);
CREATE INDEX idx_sys_auto_subscribers_channel
    ON sys_automation_subscribers(channel_id)
    WHERE channel_id IS NOT NULL;
CREATE INDEX idx_sys_auto_subscribers_user
    ON sys_automation_subscribers(user_id)
    WHERE user_id IS NOT NULL;
COMMENT ON TABLE sys_automation_subscribers IS
    'Polymorphic subscribers binding rules to channels, users, roles, or document fields';

-- sys_automation_history: append-only log of rule evaluations and deliveries.
-- Denormalized for fast UI display without JOINs.
CREATE TABLE sys_automation_history (
    id                UUID          PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    rule_id           UUID          NOT NULL REFERENCES sys_automation_rules(id) ON DELETE CASCADE,
    rule_name         VARCHAR(255)  NOT NULL,

    event_type        VARCHAR(100)  NOT NULL,
    aggregate_id      UUID,
    aggregate_name    VARCHAR(255),

    status            VARCHAR(20)   NOT NULL,
    -- success | error | condition_false | skipped | pending

    channel_id        UUID,
    channel_name      VARCHAR(255),
    account_name      VARCHAR(255),

    rendered_payload  TEXT,
    error_text        TEXT,
    duration_ms       INT,

    created_at        TIMESTAMPTZ   NOT NULL DEFAULT statement_timestamp()
);
-- Main UI query: recent history with filters
CREATE INDEX idx_sys_auto_hist_created
    ON sys_automation_history(created_at DESC);
CREATE INDEX idx_sys_auto_hist_rule
    ON sys_automation_history(rule_id);
CREATE INDEX idx_sys_auto_hist_status
    ON sys_automation_history(status);
-- Fast "recent errors" query for dashboard/alerts
CREATE INDEX idx_sys_auto_hist_errors
    ON sys_automation_history(created_at DESC)
    WHERE status = 'error';
COMMENT ON TABLE sys_automation_history IS
    'Append-only execution log for automation rules. Denormalized for fast UI queries.';

CREATE TABLE sys_report_variants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    dataset_key VARCHAR(255) NOT NULL,
    name VARCHAR(255) NOT NULL,
    author_id UUID REFERENCES auth_users(id) ON DELETE SET NULL,
    visibility VARCHAR(50) NOT NULL CHECK (visibility IN ('personal', 'shared', 'system')),
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    
    -- CDC 
    deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
    _deleted_at TIMESTAMPTZ,
    _txid BIGINT DEFAULT txid_current(),
    version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_sys_report_variants_dataset_key ON sys_report_variants(dataset_key);
CREATE INDEX idx_sys_report_variants_visibility ON sys_report_variants(visibility);
CREATE INDEX idx_sys_report_variants_author_id ON sys_report_variants(author_id);

CREATE TABLE sys_list_views (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    entity_type VARCHAR(255) NOT NULL,
    name VARCHAR(255) NOT NULL,
    author_id UUID REFERENCES auth_users(id) ON DELETE SET NULL,
    visibility VARCHAR(50) NOT NULL CHECK (visibility IN ('personal', 'shared', 'system')),
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    sort_order INT NOT NULL DEFAULT 0,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,

    -- CDC
    deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
    _deleted_at TIMESTAMPTZ,
    _txid BIGINT DEFAULT txid_current(),
    version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_sys_list_views_entity_type ON sys_list_views(entity_type);
CREATE INDEX idx_sys_list_views_author_id ON sys_list_views(author_id);
CREATE INDEX idx_sys_list_views_visibility ON sys_list_views(visibility);
CREATE UNIQUE INDEX idx_sys_list_views_default_scope
    ON sys_list_views(entity_type, visibility, author_id) NULLS NOT DISTINCT
    WHERE is_default = TRUE AND deletion_mark = FALSE;

-- sys_automation_files: temporary storage for generated report files.
-- Files are created during report generation (reaction_type = 'generate_report')
-- and cleaned up by the worker after expiration (default 24h).
CREATE TABLE sys_automation_files (
    id            UUID          PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    rule_id       UUID          NOT NULL REFERENCES sys_automation_rules(id) ON DELETE CASCADE,
    file_name     VARCHAR(255)  NOT NULL,
    mime_type     VARCHAR(100)  NOT NULL DEFAULT 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    file_data     BYTEA         NOT NULL,
    file_size     INT           NOT NULL,
    row_count     INT           NOT NULL DEFAULT 0,
    metadata      JSONB         NOT NULL DEFAULT '{}',
    -- { "datasetKey", "periodFrom", "periodTo", "variantName" }

    expires_at    TIMESTAMPTZ   NOT NULL,
    created_at    TIMESTAMPTZ   NOT NULL DEFAULT statement_timestamp()
);
-- Cleanup worker queries expired files
CREATE INDEX idx_sys_auto_files_expires ON sys_automation_files(expires_at);
-- List files by rule for debugging/monitoring
CREATE INDEX idx_sys_auto_files_rule ON sys_automation_files(rule_id);
COMMENT ON TABLE  sys_automation_files           IS 'Temporary storage for generated report files. Cleaned up by worker after 24h.';
COMMENT ON COLUMN sys_automation_files.file_data IS 'Raw binary file content (XLSX, CSV)';
COMMENT ON COLUMN sys_automation_files.metadata  IS 'Report generation context: dataset key, period, variant name';

CREATE TABLE sys_worker_jobs (
    id              UUID               PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    job_name        VARCHAR(100)       NOT NULL,
    job_category    VARCHAR(50)        NOT NULL,
    status          worker_job_status  NOT NULL DEFAULT 'running',
    started_at      TIMESTAMPTZ        NOT NULL DEFAULT NOW(),
    finished_at     TIMESTAMPTZ,
    duration_ms     INTEGER,
    items_processed INTEGER,
    error_message   TEXT,
    metadata        JSONB,
    _txid           BIGINT             DEFAULT txid_current()
);
-- Fast lookups: latest runs per job, and all errors
CREATE INDEX idx_worker_jobs_name_started   ON sys_worker_jobs (job_name, started_at DESC);
CREATE INDEX idx_worker_jobs_started        ON sys_worker_jobs (started_at DESC);
CREATE INDEX idx_worker_jobs_errors         ON sys_worker_jobs (started_at DESC) WHERE status = 'error';
COMMENT ON TABLE sys_worker_jobs IS 'Background worker task execution log (7-day retention)';
COMMENT ON COLUMN sys_worker_jobs.job_name     IS 'Stable identifier, e.g. crypto.expiration, cleanup.sessions';
COMMENT ON COLUMN sys_worker_jobs.job_category IS 'Grouping: crypto | outbox | cleanup | automation';
COMMENT ON COLUMN sys_worker_jobs.items_processed IS 'Rows/records affected (invoices expired, messages relayed, etc.)';
COMMENT ON COLUMN sys_worker_jobs.metadata    IS 'Arbitrary context, e.g. {tenant_id, threshold}';


-- ---- Triggers ----
CREATE TRIGGER trg_sys_automation_accounts_modtime
    BEFORE UPDATE ON sys_automation_accounts
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER set_sys_automation_accounts_txid
    BEFORE INSERT OR UPDATE ON sys_automation_accounts
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER soft_delete_sys_automation_accounts
    BEFORE UPDATE ON sys_automation_accounts
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

CREATE TRIGGER trg_cat_integration_accounts_txid BEFORE INSERT OR UPDATE ON cat_integration_accounts FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_cat_integration_accounts_modtime BEFORE UPDATE ON cat_integration_accounts FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_cat_integration_accounts_delete BEFORE UPDATE OF deletion_mark ON cat_integration_accounts FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

CREATE TRIGGER trg_cat_integration_pools_txid BEFORE INSERT OR UPDATE ON cat_integration_pools FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_cat_integration_pools_modtime BEFORE UPDATE ON cat_integration_pools FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_cat_integration_pools_delete BEFORE UPDATE OF deletion_mark ON cat_integration_pools FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

CREATE TRIGGER trg_sys_automation_rules_modtime
    BEFORE UPDATE ON sys_automation_rules
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER set_sys_automation_rules_txid
    BEFORE INSERT OR UPDATE ON sys_automation_rules
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER soft_delete_sys_automation_rules
    BEFORE UPDATE ON sys_automation_rules
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

CREATE TRIGGER trg_automation_schedules_txid BEFORE INSERT OR UPDATE ON sys_automation_schedules
 FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_automation_runs_txid BEFORE INSERT OR UPDATE ON sys_automation_runs
 FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_sys_automation_channels_modtime
    BEFORE UPDATE ON sys_automation_channels
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER set_sys_automation_channels_txid
    BEFORE INSERT OR UPDATE ON sys_automation_channels
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER soft_delete_sys_automation_channels
    BEFORE UPDATE ON sys_automation_channels
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

CREATE TRIGGER update_sys_notifications_modtime
    BEFORE UPDATE ON sys_notifications
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER set_sys_notifications_txid
    BEFORE INSERT OR UPDATE ON sys_notifications
    FOR EACH ROW
    EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER soft_delete_sys_notifications
    BEFORE UPDATE ON sys_notifications
    FOR EACH ROW
    EXECUTE FUNCTION soft_delete_with_timestamp();

CREATE TRIGGER trg_sys_report_variants_modtime
    BEFORE UPDATE ON sys_report_variants
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Triggers for CDC
CREATE TRIGGER trg_sys_report_variants_update_txid
    BEFORE UPDATE ON sys_report_variants
    FOR EACH ROW
    EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_sys_report_variants_soft_delete
    BEFORE UPDATE ON sys_report_variants
    FOR EACH ROW
    EXECUTE FUNCTION soft_delete_with_timestamp();

CREATE TRIGGER trg_sys_list_views_modtime
    BEFORE UPDATE ON sys_list_views
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Triggers for CDC
CREATE TRIGGER trg_sys_list_views_update_txid
    BEFORE UPDATE ON sys_list_views
    FOR EACH ROW
    EXECUTE FUNCTION update_txid_column();

CREATE TRIGGER trg_sys_list_views_soft_delete
    BEFORE UPDATE ON sys_list_views
    FOR EACH ROW
    EXECUTE FUNCTION soft_delete_with_timestamp();


-- ---- Seed data ----
-- Seed the single row with defaults
INSERT INTO sys_settings (singleton) VALUES (TRUE);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TABLE sys_worker_jobs;
DROP TABLE sys_automation_files;
DROP TABLE sys_list_views;
DROP TABLE sys_report_variants;
DROP TABLE sys_automation_history;
DROP TABLE sys_automation_subscribers;
DROP TABLE sys_notifications;
DROP TABLE sys_automation_channels;
DROP TABLE sys_automation_runs;
DROP TABLE sys_automation_schedules;
DROP TABLE sys_automation_rules;
DROP TABLE sys_integration_pool_state;
DROP TABLE sys_integration_account_state;
DROP TABLE sys_integration_pool_accounts;
DROP TABLE cat_integration_pools;
DROP TABLE cat_integration_accounts;
DROP TABLE sys_automation_accounts;
DROP TABLE sys_settings;
DROP TYPE worker_job_status;
-- +goose StatementEnd
