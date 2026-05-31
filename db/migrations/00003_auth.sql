-- +goose Up
-- Description: Authentication & RBAC tables (users, roles, permissions, refresh tokens).

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- ── Users ──────────────────────────────────────────────────────────────────
CREATE TABLE users (
    id                    UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
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

CREATE INDEX idx_users_email     ON users (email) WHERE deletion_mark = FALSE;
CREATE INDEX idx_users_is_active ON users (is_active) WHERE deletion_mark = FALSE;

-- ── Roles ──────────────────────────────────────────────────────────────────
CREATE TABLE roles (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    code        VARCHAR(50)  NOT NULL,
    name        VARCHAR(100) NOT NULL,
    description TEXT,
    is_system   BOOLEAN      NOT NULL DEFAULT false,
    CONSTRAINT roles_code_unique UNIQUE (code)
);

-- ── Permissions ────────────────────────────────────────────────────────────
CREATE TABLE permissions (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    code        VARCHAR(100) NOT NULL UNIQUE,
    name        VARCHAR(200) NOT NULL,
    description TEXT,
    resource    VARCHAR(50)  NOT NULL,
    action      VARCHAR(50)  NOT NULL
);

CREATE INDEX idx_permissions_resource ON permissions (resource);
CREATE INDEX idx_permissions_code     ON permissions (code);

-- ── Role ↔ Permission (M2M) ───────────────────────────────────────────────
CREATE TABLE role_permissions (
    role_id       UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id UUID NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

-- ── User ↔ Role (M2M) ─────────────────────────────────────────────────────
CREATE TABLE user_roles (
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id    UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    granted_by UUID REFERENCES users(id),
    PRIMARY KEY (user_id, role_id)
);

CREATE INDEX idx_user_roles_user_id ON user_roles (user_id);
CREATE INDEX idx_user_roles_role_id ON user_roles (role_id);

-- Auth Policy State
-- Single-row tenant-local epoch for RBAC policy changes. Access JWTs carry this
-- value and are rejected when the server-side epoch changes.
CREATE TABLE auth_policy_state (
    id         BOOLEAN     PRIMARY KEY DEFAULT TRUE,
    version    BIGINT      NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT auth_policy_state_singleton CHECK (id = TRUE)
);

INSERT INTO auth_policy_state (id, version) VALUES (TRUE, 1);

-- Auth Sessions
-- Server-side session boundary for access-token revocation. Refresh tokens are
-- attached to a session; access JWTs carry auth_sessions.id as sid.
CREATE TABLE auth_sessions (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_auth_version BIGINT      NOT NULL,
    policy_version    BIGINT      NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at      TIMESTAMPTZ,
    expires_at        TIMESTAMPTZ NOT NULL,
    revoked_at        TIMESTAMPTZ,
    revoked_reason    VARCHAR(100),
    user_agent        TEXT,
    ip_address        INET
);

CREATE INDEX idx_auth_sessions_user_active
    ON auth_sessions (user_id)
    WHERE revoked_at IS NULL;

CREATE INDEX idx_auth_sessions_expires
    ON auth_sessions (expires_at)
    WHERE revoked_at IS NULL;

-- ── Refresh Tokens ─────────────────────────────────────────────────────────
CREATE TABLE refresh_tokens (
    id             UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_id     UUID         NOT NULL REFERENCES auth_sessions(id) ON DELETE CASCADE,
    token_hash     VARCHAR(255) NOT NULL UNIQUE,
    expires_at     TIMESTAMPTZ  NOT NULL,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    revoked_at     TIMESTAMPTZ,
    revoked_reason VARCHAR(100),
    user_agent     TEXT,
    ip_address     INET
);

CREATE INDEX idx_refresh_tokens_user_id ON refresh_tokens (user_id);
CREATE INDEX idx_refresh_tokens_session ON refresh_tokens (session_id) WHERE revoked_at IS NULL;
CREATE INDEX idx_refresh_tokens_expires ON refresh_tokens (expires_at) WHERE revoked_at IS NULL;

-- ── User Preferences ───────────────────────────────────────────────────────
CREATE TABLE user_preferences (
    user_id          UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    interface        JSONB       NOT NULL DEFAULT '{}',
    list_filters     JSONB       NOT NULL DEFAULT '{}',
    list_columns     JSONB       NOT NULL DEFAULT '{}',
    dashboard_layout JSONB       NOT NULL DEFAULT 'null',
    favorites        JSONB       NOT NULL DEFAULT '[]',
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id)
);

CREATE TRIGGER trg_user_preferences_updated_at
    BEFORE UPDATE ON user_preferences
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

COMMENT ON TABLE  user_preferences              IS 'Per-user UI preferences (theme, filters, columns, dashboard)';
COMMENT ON COLUMN user_preferences.interface    IS 'Typed UI settings: theme, language, pageSize, etc.';
COMMENT ON COLUMN user_preferences.list_filters IS 'Saved list filters per entity type (opaque JSON)';
COMMENT ON COLUMN user_preferences.list_columns IS 'Visible column keys per entity type';
COMMENT ON COLUMN user_preferences.dashboard_layout IS 'Per-user dashboard widget layout (opaque JSON, frontend owns schema)';
COMMENT ON COLUMN user_preferences.favorites        IS 'Per-user bookmarked entities (opaque JSON array, frontend owns schema)';

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
DROP TABLE IF EXISTS user_preferences;
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS auth_sessions;
DROP TABLE IF EXISTS auth_policy_state;
DROP TABLE IF EXISTS user_roles;
DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS permissions;
DROP TABLE IF EXISTS roles;
DROP TABLE IF EXISTS users;
