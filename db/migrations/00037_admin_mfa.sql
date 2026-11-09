-- +goose Up
-- Description: Admin MFA, TOTP recovery codes, and step-up confirmations.

SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

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

CREATE TRIGGER trg_user_mfa_totp_updated_at
    BEFORE UPDATE ON auth_user_mfa_totp
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE auth_user_mfa_backup_codes (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    user_id      UUID NOT NULL REFERENCES auth_users(id) ON DELETE CASCADE,
    code_hash    VARCHAR(255) NOT NULL,
    used_at      TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_mfa_backup_codes_user_unused
    ON auth_user_mfa_backup_codes (user_id, created_at)
    WHERE used_at IS NULL;

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

INSERT INTO auth_roles (id, code, name, description, is_system) VALUES
    ('b0000000-0000-0000-0000-000000000006', 'security_admin', 'Администратор безопасности', 'MFA reset and security administration', TRUE)
ON CONFLICT (code) DO NOTHING;


-- +goose Down
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DELETE FROM auth_roles WHERE code = 'security_admin' AND id = 'b0000000-0000-0000-0000-000000000006';
DROP TABLE IF EXISTS auth_step_up_confirmations;
DROP TABLE IF EXISTS auth_mfa_challenges;
DROP TABLE IF EXISTS auth_user_mfa_backup_codes;
DROP TRIGGER IF EXISTS trg_user_mfa_totp_updated_at ON auth_user_mfa_totp;
DROP TABLE IF EXISTS auth_user_mfa_totp;
