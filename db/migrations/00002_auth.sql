-- Authentication and authorization: users, roles, permissions, sessions, MFA,
-- row/field security profiles and their seeds.
-- Consolidated schema: each object is declared once in its final form.

-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- Function bodies are validated when first executed; definitions precede the tables they use.
SET LOCAL check_function_bodies = off;


-- ---- Tables ----
CREATE TABLE auth_users (
    id                    UUID         PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    email                 VARCHAR(255) NOT NULL,
    password_hash         VARCHAR(255) NOT NULL,
    first_name            VARCHAR(100),
    last_name             VARCHAR(100),
    is_active             BOOLEAN      NOT NULL DEFAULT true,
    is_admin              BOOLEAN      NOT NULL DEFAULT false,
    email_verified        BOOLEAN      NOT NULL DEFAULT false,
    email_verified_at     TIMESTAMPTZ,
    last_login_at         TIMESTAMPTZ,
    failed_login_attempts INT          NOT NULL DEFAULT 0,
    locked_until          TIMESTAMPTZ,
    auth_version          BIGINT       NOT NULL DEFAULT 1,
    deletion_mark         BOOLEAN      NOT NULL DEFAULT false,
    version               INT          NOT NULL DEFAULT 1,
    attributes            JSONB        DEFAULT '{}',
    CONSTRAINT users_email_unique UNIQUE (email)
);
CREATE INDEX idx_users_email     ON auth_users (email) WHERE deletion_mark = FALSE;
CREATE INDEX idx_users_is_active ON auth_users (is_active) WHERE deletion_mark = FALSE;

CREATE TABLE auth_roles (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    code        VARCHAR(50)  NOT NULL,
    name        VARCHAR(100) NOT NULL,
    description TEXT,
    is_system   BOOLEAN      NOT NULL DEFAULT false,
    CONSTRAINT roles_code_unique UNIQUE (code)
);

CREATE TABLE auth_permissions (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    code        VARCHAR(100) NOT NULL UNIQUE,
    name        VARCHAR(200) NOT NULL,
    description TEXT,
    resource    VARCHAR(50)  NOT NULL,
    action      VARCHAR(50)  NOT NULL
);
CREATE INDEX idx_permissions_resource ON auth_permissions (resource);
CREATE INDEX idx_permissions_code     ON auth_permissions (code);

-- Role ↔ Permission (M2M)
CREATE TABLE auth_role_permissions (
    role_id       UUID NOT NULL REFERENCES auth_roles(id) ON DELETE CASCADE,
    permission_id UUID NOT NULL REFERENCES auth_permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

-- User ↔ Role (M2M)
CREATE TABLE auth_user_roles (
    user_id    UUID NOT NULL REFERENCES auth_users(id) ON DELETE CASCADE,
    role_id    UUID NOT NULL REFERENCES auth_roles(id) ON DELETE CASCADE,
    granted_by UUID REFERENCES auth_users(id),
    PRIMARY KEY (user_id, role_id)
);
CREATE INDEX idx_user_roles_user_id ON auth_user_roles (user_id);
CREATE INDEX idx_user_roles_role_id ON auth_user_roles (role_id);

-- Auth Policy State
-- Single-row tenant-local epoch for RBAC policy changes. Access JWTs carry this
-- value and are rejected when the server-side epoch changes.
CREATE TABLE auth_policy_state (
    id         BOOLEAN     PRIMARY KEY DEFAULT TRUE,
    version    BIGINT      NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT auth_policy_state_singleton CHECK (id = TRUE)
);

-- Auth Sessions
-- Server-side session boundary for access-token revocation. Refresh tokens are
-- attached to a session; access JWTs carry auth_sessions.id as sid.
CREATE TABLE auth_sessions (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    user_id           UUID        NOT NULL REFERENCES auth_users(id) ON DELETE CASCADE,
    user_auth_version BIGINT      NOT NULL,
    policy_version    BIGINT      NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at      TIMESTAMPTZ,
    expires_at        TIMESTAMPTZ NOT NULL,
    revoked_at        TIMESTAMPTZ,
    revoked_reason    VARCHAR(100),
    user_agent        TEXT,
    ip_address        INET,
    mfa_verified_at   TIMESTAMPTZ,
    mfa_method        VARCHAR(30),
    mfa_pending       BOOLEAN NOT NULL DEFAULT FALSE,
    idle_expires_at   TIMESTAMPTZ,
    absolute_expires_at TIMESTAMPTZ
);
CREATE INDEX idx_auth_sessions_user_active
    ON auth_sessions (user_id)
    WHERE revoked_at IS NULL;
CREATE INDEX idx_auth_sessions_expires
    ON auth_sessions (expires_at)
    WHERE revoked_at IS NULL;

CREATE TABLE auth_refresh_tokens (
    id             UUID         PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    user_id        UUID         NOT NULL REFERENCES auth_users(id) ON DELETE CASCADE,
    session_id     UUID         NOT NULL REFERENCES auth_sessions(id) ON DELETE CASCADE,
    token_hash     VARCHAR(255) NOT NULL UNIQUE,
    expires_at     TIMESTAMPTZ  NOT NULL,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    revoked_at     TIMESTAMPTZ,
    revoked_reason VARCHAR(100),
    user_agent     TEXT,
    ip_address     INET
);
CREATE INDEX idx_refresh_tokens_user_id ON auth_refresh_tokens (user_id);
CREATE INDEX idx_refresh_tokens_session ON auth_refresh_tokens (session_id) WHERE revoked_at IS NULL;
CREATE INDEX idx_refresh_tokens_expires ON auth_refresh_tokens (expires_at) WHERE revoked_at IS NULL;

CREATE TABLE sys_user_preferences (
    user_id          UUID        NOT NULL REFERENCES auth_users(id) ON DELETE CASCADE,
    interface        JSONB       NOT NULL DEFAULT '{}',
    list_filters     JSONB       NOT NULL DEFAULT '{}',
    list_columns     JSONB       NOT NULL DEFAULT '{}',
    dashboard_layout JSONB       NOT NULL DEFAULT 'null',
    favorites        JSONB       NOT NULL DEFAULT '[]',
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id)
);
COMMENT ON TABLE  sys_user_preferences              IS 'Per-user UI preferences (theme, filters, columns, dashboard)';
COMMENT ON COLUMN sys_user_preferences.interface    IS 'Typed UI settings: theme, language, pageSize, etc.';
COMMENT ON COLUMN sys_user_preferences.list_filters IS 'Saved list filters per entity type (opaque JSON)';
COMMENT ON COLUMN sys_user_preferences.list_columns IS 'Visible column keys per entity type';
COMMENT ON COLUMN sys_user_preferences.dashboard_layout IS 'Per-user dashboard widget layout (opaque JSON, frontend owns schema)';
COMMENT ON COLUMN sys_user_preferences.favorites        IS 'Per-user bookmarked entities (opaque JSON array, frontend owns schema)';

CREATE TABLE auth_security_profiles (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    code        VARCHAR(50)  NOT NULL,
    name        VARCHAR(200) NOT NULL,
    description TEXT,
    is_system   BOOLEAN      NOT NULL DEFAULT FALSE,
    is_active   BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_security_profiles_code UNIQUE (code)
);

-- RLS Dimensions (row-level security)
-- One row per (profile, entity_name, dimension_name) with an array of allowed IDs.
-- entity_name = '' means the dimension applies globally to all entities.
CREATE TABLE auth_security_profile_dimensions (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    profile_id      UUID        NOT NULL REFERENCES auth_security_profiles(id) ON DELETE CASCADE,
    entity_name     VARCHAR(50) NOT NULL DEFAULT '',
    dimension_name  VARCHAR(50) NOT NULL,
    allowed_ids     TEXT[]      NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_dim UNIQUE (profile_id, entity_name, dimension_name)
);
CREATE INDEX idx_spd_profile   ON auth_security_profile_dimensions (profile_id);
CREATE INDEX idx_spd_dimension ON auth_security_profile_dimensions (dimension_name);
COMMENT ON TABLE auth_security_profile_dimensions IS 'RLS dimensions — grants access to specific entity IDs per dimension';
COMMENT ON COLUMN auth_security_profile_dimensions.entity_name IS 'Entity scope (empty = all, or specific like crypto_invoice)';
COMMENT ON COLUMN auth_security_profile_dimensions.dimension_name IS 'Dimension name (organization, merchant, counterparty)';
COMMENT ON COLUMN auth_security_profile_dimensions.allowed_ids IS 'Array of allowed entity IDs for this dimension';

-- FLS Field Policies (field-level security)
-- One row per (profile, entity_name, action) with array of allowed fields + table parts JSONB.
CREATE TABLE auth_security_profile_field_policies (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    profile_id      UUID        NOT NULL REFERENCES auth_security_profiles(id) ON DELETE CASCADE,
    entity_name     VARCHAR(50) NOT NULL,
    action          VARCHAR(20) NOT NULL,
    allowed_fields  TEXT[]      NOT NULL DEFAULT '{}',
    table_parts     JSONB       NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_fls UNIQUE (profile_id, entity_name, action)
);
CREATE INDEX idx_fls_profile ON auth_security_profile_field_policies (profile_id);
CREATE INDEX idx_fls_entity  ON auth_security_profile_field_policies (entity_name);
COMMENT ON TABLE auth_security_profile_field_policies IS 'FLS — controls field-level access per entity and action';

-- User ↔ Security Profile (M2M)
CREATE TABLE auth_user_security_profiles (
    user_id    UUID NOT NULL REFERENCES auth_users(id) ON DELETE CASCADE,
    profile_id UUID NOT NULL REFERENCES auth_security_profiles(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, profile_id)
);
CREATE INDEX idx_usp_user    ON auth_user_security_profiles (user_id);
CREATE INDEX idx_usp_profile ON auth_user_security_profiles (profile_id);

CREATE TABLE auth_security_policy_rules (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    profile_id  UUID        NOT NULL REFERENCES auth_security_profiles(id) ON DELETE CASCADE,
    name        VARCHAR(200) NOT NULL,
    description TEXT,
    entity_name VARCHAR(50) NOT NULL,
    actions     TEXT[]      NOT NULL DEFAULT '{}',
    effect      VARCHAR(10) NOT NULL DEFAULT 'deny',
    expression  TEXT        NOT NULL,
    priority    INT         NOT NULL DEFAULT 0,
    enabled     BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_effect CHECK (effect IN ('deny', 'allow'))
);
CREATE INDEX idx_spr_profile ON auth_security_policy_rules (profile_id);
CREATE INDEX idx_spr_entity  ON auth_security_policy_rules (entity_name) WHERE enabled = TRUE;
COMMENT ON TABLE auth_security_policy_rules IS 'CEL-based fine-grained authorization rules';
COMMENT ON COLUMN auth_security_policy_rules.expression IS 'CEL expression evaluated at runtime';
COMMENT ON COLUMN auth_security_policy_rules.actions IS 'Array of actions: create, read, update, delete, post, unpost';

CREATE TABLE auth_user_mfa_totp (
    user_id              UUID PRIMARY KEY REFERENCES auth_users(id) ON DELETE CASCADE,
    secret_ciphertext    BYTEA,
    secret_nonce         BYTEA,
    enabled              BOOLEAN NOT NULL DEFAULT FALSE,
    enrolled_at          TIMESTAMPTZ,
    disabled_at          TIMESTAMPTZ,
    required_after       TIMESTAMPTZ,
    last_used_time_step  BIGINT,
    failed_attempts      INT NOT NULL DEFAULT 0,
    failed_window_start  TIMESTAMPTZ,
    locked_until         TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_totp_secret_when_enabled CHECK (
        enabled = FALSE OR (secret_ciphertext IS NOT NULL AND secret_nonce IS NOT NULL AND enrolled_at IS NOT NULL)
    )
);

CREATE TABLE auth_mfa_challenges (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    user_id                 UUID NOT NULL REFERENCES auth_users(id) ON DELETE CASCADE,
    challenge_type          VARCHAR(30) NOT NULL,
    setup_secret_ciphertext BYTEA,
    setup_secret_nonce      BYTEA,
    expires_at              TIMESTAMPTZ NOT NULL,
    consumed_at             TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    user_agent              TEXT,
    ip_address              INET,
    CONSTRAINT chk_mfa_challenge_type CHECK (challenge_type IN ('login', 'enrollment'))
);
CREATE INDEX idx_auth_mfa_challenges_user_active
    ON auth_mfa_challenges (user_id, expires_at)
    WHERE consumed_at IS NULL;

CREATE TABLE auth_step_up_confirmations (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    session_id   UUID NOT NULL REFERENCES auth_sessions(id) ON DELETE CASCADE,
    user_id      UUID NOT NULL REFERENCES auth_users(id) ON DELETE CASCADE,
    tenant_id    TEXT NOT NULL,
    action_code  VARCHAR(100) NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_step_up_lookup
    ON auth_step_up_confirmations (session_id, user_id, tenant_id, action_code, expires_at DESC);


-- ---- Triggers ----
CREATE TRIGGER trg_user_preferences_updated_at
    BEFORE UPDATE ON sys_user_preferences
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_security_profiles_updated_at
    BEFORE UPDATE ON auth_security_profiles
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_security_policy_rules_updated_at
    BEFORE UPDATE ON auth_security_policy_rules
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_user_mfa_totp_updated_at
    BEFORE UPDATE ON auth_user_mfa_totp
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();


-- ---- Seed data ----
INSERT INTO auth_policy_state (id, version) VALUES (TRUE, 1);

INSERT INTO auth_security_profiles (id, code, name, description, is_system) VALUES
    ('a0000000-0000-0000-0000-000000000001', 'full_access', 'Full Access', 'No restrictions — all dimensions, all fields', TRUE),
    ('a0000000-0000-0000-0000-000000000002', 'viewer', 'Viewer (Read-Only)', 'Read-only with hidden financial fields', TRUE);

-- Viewer FLS: hide financial fields (allowed_fields with '-' prefix = hidden)
INSERT INTO auth_security_profile_field_policies (profile_id, entity_name, action, allowed_fields) VALUES
    ('a0000000-0000-0000-0000-000000000002', 'crypto_invoice', 'read', ARRAY['-expected_amount', '-received_amount']),
    ('a0000000-0000-0000-0000-000000000002', 'crypto_payment', 'read', ARRAY['-amount']);

INSERT INTO auth_permissions (code, name, description, resource, action) VALUES
    -- Catalogs: Counterparties
    ('counterparty.read',   'Чтение контрагентов',   'View counterparties catalog', 'counterparty', 'read'),
    ('counterparty.create', 'Создание контрагентов', 'Create counterparties', 'counterparty', 'create'),
    ('counterparty.update', 'Изменение контрагентов', 'Update counterparties', 'counterparty', 'update'),
    ('counterparty.delete', 'Удаление контрагентов', 'Delete counterparties', 'counterparty', 'delete'),
    -- Catalogs: Currencies
    ('currency.read',   'Чтение валют',   'View currencies catalog', 'currency', 'read'),
    ('currency.create', 'Создание валют', 'Create currencies', 'currency', 'create'),
    ('currency.update', 'Изменение валют', 'Update currencies', 'currency', 'update'),
    ('currency.delete', 'Удаление валют', 'Delete currencies', 'currency', 'delete'),
    -- Catalogs: Organizations
    ('organization.read',   'Чтение организаций',   'View organizations catalog', 'organization', 'read'),
    ('organization.create', 'Создание организаций', 'Create organizations', 'organization', 'create'),
    ('organization.update', 'Изменение организаций', 'Update organizations', 'organization', 'update'),
    ('organization.delete', 'Удаление организаций', 'Delete organizations', 'organization', 'delete'),
    -- Catalogs: Contracts
    ('contract.read',   'Чтение договоров',   'View contracts catalog', 'contract', 'read'),
    ('contract.create', 'Создание договоров', 'Create contracts', 'contract', 'create'),
    ('contract.update', 'Изменение договоров', 'Update contracts', 'contract', 'update'),
    ('contract.delete', 'Удаление договоров', 'Delete contracts', 'contract', 'delete'),
    -- Reports
    ('report_documents.read', 'Журнал документов',    'View documents journal report', 'report_documents', 'read'),
    -- Admin
    ('admin.users',  'Управление пользователями', 'User management', 'admin', 'users'),
    ('admin.roles',  'Управление ролями',         'Role management', 'admin', 'roles')
ON CONFLICT (code) DO NOTHING;

INSERT INTO auth_roles (id, code, name, description, is_system) VALUES
    ('b0000000-0000-0000-0000-000000000001', 'admin',            'Администратор',     'Full access to all features', TRUE),
    ('b0000000-0000-0000-0000-000000000002', 'accountant',       'Бухгалтер',         'Read catalogs, full document and report access', TRUE),
    ('b0000000-0000-0000-0000-000000000003', 'manager',          'Менеджер',          'Full catalog access, limited documents', TRUE),
    ('b0000000-0000-0000-0000-000000000005', 'user',             'Пользователь',      'Basic read-only access', TRUE)
ON CONFLICT (code) DO NOTHING;

-- Role ↔ Permission assignments
-- Admin: ALL permissions
INSERT INTO auth_role_permissions (role_id, permission_id)
SELECT 'b0000000-0000-0000-0000-000000000001', id FROM auth_permissions
ON CONFLICT DO NOTHING;

-- Accountant: read catalogs + full documents + reports + registers
INSERT INTO auth_role_permissions (role_id, permission_id)
SELECT 'b0000000-0000-0000-0000-000000000002', id FROM auth_permissions
WHERE action = 'read'
   OR resource IN ('report_documents')
ON CONFLICT DO NOTHING;

-- Manager: full catalogs + read/create/update documents + reports
INSERT INTO auth_role_permissions (role_id, permission_id)
SELECT 'b0000000-0000-0000-0000-000000000003', id FROM auth_permissions
WHERE (resource IN ('counterparty', 'currency', 'organization', 'contract'))
   OR resource IN ('report_documents')
ON CONFLICT DO NOTHING;

-- User: read-only everything
INSERT INTO auth_role_permissions (role_id, permission_id)
SELECT 'b0000000-0000-0000-0000-000000000005', id FROM auth_permissions
WHERE action = 'read'
ON CONFLICT DO NOTHING;

INSERT INTO auth_permissions(code,name,description,resource,action)
SELECT 'catalog:'||resource||':'||action, label||': '||action, label,resource,action
FROM (VALUES ('integration_account','Аккаунты интеграций'),('integration_pool','Пулы аккаунтов')) AS r(resource,label)
CROSS JOIN (VALUES ('read'),('create'),('update'),('delete')) AS a(action);

INSERT INTO auth_role_permissions(role_id,permission_id)
SELECT 'b0000000-0000-0000-0000-000000000001',id FROM auth_permissions WHERE resource IN ('integration_account','integration_pool');

INSERT INTO auth_permissions (code, name, description, resource, action) VALUES
    -- Automation: Accounts
    ('automation_account.read',   'Чтение аккаунтов автоматизации',   'View automation accounts', 'automation_account', 'read'),
    ('automation_account.create', 'Создание аккаунтов автоматизации', 'Create automation accounts', 'automation_account', 'create'),
    ('automation_account.update', 'Изменение аккаунтов автоматизации', 'Update automation accounts', 'automation_account', 'update'),
    ('automation_account.delete', 'Удаление аккаунтов автоматизации', 'Delete automation accounts', 'automation_account', 'delete'),
    -- Automation: Channels
    ('automation_channel.read',   'Чтение каналов доставки',   'View automation channels', 'automation_channel', 'read'),
    ('automation_channel.create', 'Создание каналов доставки', 'Create automation channels', 'automation_channel', 'create'),
    ('automation_channel.update', 'Изменение каналов доставки', 'Update automation channels', 'automation_channel', 'update'),
    ('automation_channel.delete', 'Удаление каналов доставки', 'Delete automation channels', 'automation_channel', 'delete'),
    -- Automation: Rules
    ('automation_rule.read',   'Чтение правил автоматизации',   'View automation rules', 'automation_rule', 'read'),
    ('automation_rule.create', 'Создание правил автоматизации', 'Create automation rules', 'automation_rule', 'create'),
    ('automation_rule.update', 'Изменение правил автоматизации', 'Update automation rules', 'automation_rule', 'update'),
    ('automation_rule.delete', 'Удаление правил автоматизации', 'Delete automation rules', 'automation_rule', 'delete'),
    -- Automation: History (read-only)
    ('automation_history.read', 'Чтение истории автоматизации', 'View automation execution history', 'automation_history', 'read')
ON CONFLICT (code) DO NOTHING;

-- Grant all automation permissions to Admin role
INSERT INTO auth_role_permissions (role_id, permission_id)
SELECT 'b0000000-0000-0000-0000-000000000001', id FROM auth_permissions
WHERE resource LIKE 'automation_%'
ON CONFLICT DO NOTHING;

-- Grant read-only automation access to Accountant
INSERT INTO auth_role_permissions (role_id, permission_id)
SELECT 'b0000000-0000-0000-0000-000000000002', id FROM auth_permissions
WHERE resource LIKE 'automation_%' AND action = 'read'
ON CONFLICT DO NOTHING;

INSERT INTO auth_permissions(code,name,description,resource,action) SELECT 'catalog:crypto_rpc_endpoint:'||action,'Подключения RPC: '||action,'Настройки RPC','crypto_rpc_endpoint',action FROM (VALUES ('read'),('create'),('update'),('delete')) AS a(action);

INSERT INTO auth_role_permissions(role_id,permission_id) SELECT 'b0000000-0000-0000-0000-000000000001',id FROM auth_permissions WHERE resource='crypto_rpc_endpoint';

INSERT INTO auth_permissions(code,name,resource,action) VALUES
 ('register:exchange_rate:read','Чтение курсов валют','exchange_rate','read'),
 ('catalog:rate_source:read','Чтение источников курсов','rate_source','read'),
 ('catalog:rate_source:create','Создание источников курсов','rate_source','create'),
 ('catalog:rate_source:update','Настройка источников курсов','rate_source','update'),
 ('catalog:rate_source:delete','Удаление источников курсов','rate_source','delete')
ON CONFLICT(code) DO NOTHING;

INSERT INTO auth_role_permissions(role_id,permission_id)
SELECT r.id,p.id FROM auth_roles r CROSS JOIN auth_permissions p
WHERE (r.code IN ('admin','accountant','manager','user') AND p.code='register:exchange_rate:read')
 OR (r.code='admin' AND p.resource='rate_source')
ON CONFLICT DO NOTHING;

INSERT INTO auth_permissions (code, name, description, resource, action) VALUES
    ('document:withdrawal_request:read', 'Чтение заявок на вывод', 'View withdrawal requests', 'withdrawal_request', 'read'),
    ('document:withdrawal_request:post', 'Утверждение заявок на вывод', 'Approve withdrawal requests', 'withdrawal_request', 'post')
ON CONFLICT (code) DO NOTHING;

INSERT INTO auth_role_permissions (role_id, permission_id)
SELECT 'b0000000-0000-0000-0000-000000000001', id
FROM auth_permissions WHERE resource = 'withdrawal_request'
ON CONFLICT DO NOTHING;

INSERT INTO auth_permissions (code, name, description, resource, action) VALUES
    ('document:treasury_withdrawal:read', 'Чтение казначейских выводов', 'View treasury withdrawals', 'treasury_withdrawal', 'read'),
    ('document:treasury_withdrawal:create', 'Создание казначейских выводов', 'Create treasury withdrawals', 'treasury_withdrawal', 'create'),
    ('document:treasury_withdrawal:update', 'Изменение казначейских выводов', 'Update treasury withdrawals', 'treasury_withdrawal', 'update'),
    ('document:treasury_withdrawal:delete', 'Удаление казначейских выводов', 'Delete treasury withdrawals', 'treasury_withdrawal', 'delete'),
    ('document:treasury_withdrawal:post', 'Проведение казначейских выводов', 'Post and approve treasury withdrawals', 'treasury_withdrawal', 'post'),
    ('document:treasury_withdrawal:unpost', 'Отмена проведения казначейских выводов', 'Unpost treasury withdrawals', 'treasury_withdrawal', 'unpost'),
    ('crypto_reconciliation:read', 'Чтение криптосверки', 'Read external outflows and evidence', 'crypto_reconciliation', 'read'),
    ('crypto_reconciliation:classify', 'Классификация outflow', 'Classify external outflows', 'crypto_reconciliation', 'classify'),
    ('crypto_reconciliation:resolve', 'Разрешение outflow', 'Resolve classified external outflows', 'crypto_reconciliation', 'resolve'),
    ('crypto_custody_adjustment:approve', 'Подтверждение custody loss', 'Approve custody loss adjustments', 'crypto_custody_adjustment', 'approve')
ON CONFLICT (code) DO NOTHING;

INSERT INTO auth_role_permissions (role_id, permission_id)
SELECT 'b0000000-0000-0000-0000-000000000001', id
FROM auth_permissions WHERE resource IN ('treasury_withdrawal','crypto_reconciliation','crypto_custody_adjustment')
ON CONFLICT DO NOTHING;

INSERT INTO auth_permissions (code, name, description, resource, action) VALUES
    ('document:crypto_network_operation:read', 'Чтение сетевых криптоопераций',
     'View blockchain network operation documents', 'document:crypto_network_operation', 'read')
ON CONFLICT (code) DO NOTHING;

INSERT INTO auth_role_permissions (role_id, permission_id)
SELECT 'b0000000-0000-0000-0000-000000000001', id
FROM auth_permissions WHERE resource = 'document:crypto_network_operation'
ON CONFLICT DO NOTHING;

INSERT INTO auth_permissions (code,name,description,resource,action) VALUES
 ('document:crypto_provider_balance_operation:read','Чтение баланса провайдера','Read provider balance operations','document:crypto_provider_balance_operation','read'),
 ('document:crypto_provider_balance_operation:create','Подготовка opening провайдера','Prepare an evidenced opening with fresh MFA','document:crypto_provider_balance_operation','create'),
 ('document:crypto_provider_balance_operation:approve','Одобрение opening провайдера','Independent approval or audited system-admin self approval with fresh MFA','document:crypto_provider_balance_operation','approve'),
 ('document:crypto_provider_balance_operation:reject','Отклонение opening провайдера','Independent rejection or audited system-admin self rejection with fresh MFA','document:crypto_provider_balance_operation','reject')
ON CONFLICT (code) DO NOTHING;

INSERT INTO auth_role_permissions (role_id,permission_id)
SELECT 'b0000000-0000-0000-0000-000000000001',id FROM auth_permissions WHERE resource='document:crypto_provider_balance_operation'
ON CONFLICT DO NOTHING;

INSERT INTO auth_roles (id, code, name, description, is_system) VALUES
    ('b0000000-0000-0000-0000-000000000006', 'security_admin', 'Администратор безопасности', 'MFA reset and security administration', TRUE)
ON CONFLICT (code) DO NOTHING;

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

INSERT INTO auth_permissions (code,name,description,resource,action) VALUES
    ('crypto_network:read','Чтение криптосетей','View crypto network policies and runtime health','crypto_network','read'),
    ('crypto_network:update','Настройка криптосетей','Update guarded crypto network execution policies','crypto_network','update')
ON CONFLICT (code) DO NOTHING;

INSERT INTO auth_role_permissions (role_id,permission_id)
SELECT 'b0000000-0000-0000-0000-000000000001',id FROM auth_permissions WHERE resource='crypto_network'
ON CONFLICT DO NOTHING;

INSERT INTO auth_permissions (code,name,description,resource,action)
VALUES ('crypto_adjustment:post_direct','Прямое проведение криптокорректировок',
        'Post own crypto adjustment after fresh step-up','crypto_adjustment','post_direct')
ON CONFLICT (code) DO NOTHING;

INSERT INTO auth_role_permissions(role_id,permission_id)
SELECT r.id,p.id FROM auth_roles r CROSS JOIN auth_permissions p
WHERE r.code='admin' AND p.code='crypto_adjustment:post_direct'
ON CONFLICT DO NOTHING;

INSERT INTO auth_permissions (code,name,description,resource,action) VALUES
    ('crypto_receipt:read','Журнал невыясненных поступлений','View unidentified custody receipts','crypto_receipt','read'),
    ('crypto_receipt:credit','Зачисление невыясненных поступлений','Prepare or reject a merchant credit of an unidentified receipt','crypto_receipt','credit'),
    ('crypto_receipt:approve','Подтверждение зачисления поступлений','Approve a merchant credit prepared by another operator','crypto_receipt','approve')
ON CONFLICT (code) DO NOTHING;

-- Receipts and their manual attributions are read-only documents with
-- register movements; they are created only by the receipts workflow.
INSERT INTO auth_permissions (code,name,description,resource,action) VALUES
    ('document:crypto_custody_receipt:read','Чтение поступлений на кошельки','View custody receipts and their movements','crypto_custody_receipt','read'),
    ('document:crypto_merchant_credit:read','Чтение ручных зачислений','View manual merchant credits and their movements','crypto_merchant_credit','read'),
    ('document:crypto_platform_funding:read','Чтение пополнений платформы','View platform fundings and their movements','crypto_platform_funding','read')
ON CONFLICT (code) DO NOTHING;

INSERT INTO auth_role_permissions (role_id, permission_id)
SELECT 'b0000000-0000-0000-0000-000000000001', id FROM auth_permissions
WHERE resource IN ('crypto_custody_receipt','crypto_merchant_credit','crypto_platform_funding')
ON CONFLICT DO NOTHING;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TABLE auth_step_up_confirmations;
DROP TABLE auth_mfa_challenges;
DROP TABLE auth_user_mfa_totp;
DROP TABLE auth_security_policy_rules;
DROP TABLE auth_user_security_profiles;
DROP TABLE auth_security_profile_field_policies;
DROP TABLE auth_security_profile_dimensions;
DROP TABLE auth_security_profiles;
DROP TABLE sys_user_preferences;
DROP TABLE auth_refresh_tokens;
DROP TABLE auth_sessions;
DROP TABLE auth_policy_state;
DROP TABLE auth_user_roles;
DROP TABLE auth_role_permissions;
DROP TABLE auth_permissions;
DROP TABLE auth_roles;
DROP TABLE auth_users;
-- +goose StatementEnd
