-- +goose Up
-- Description: User sessions and refresh tokens
-- Supports: Token revocation, session listing, forced logout

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

CREATE TABLE sys_sessions (
    refresh_token UUID PRIMARY KEY,
    user_id VARCHAR(50) NOT NULL,
    user_email VARCHAR(255) NOT NULL,
    user_agent TEXT,
    ip_address INET,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    last_used_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    is_revoked BOOLEAN NOT NULL DEFAULT FALSE,
    revoked_at TIMESTAMPTZ,
    revoke_reason VARCHAR(50)             -- 'logout', 'password_change', 'admin_action'
);

-- Index for user session listing
CREATE INDEX idx_sessions_user ON sys_sessions (user_id, created_at DESC) 
    WHERE is_revoked = FALSE;

-- Index for cleanup job
CREATE INDEX idx_sessions_expires ON sys_sessions (expires_at) 
    WHERE is_revoked = FALSE;

-- Index for tenant


COMMENT ON TABLE sys_sessions IS 'Active user sessions with refresh tokens';
COMMENT ON COLUMN sys_sessions.is_revoked IS 'TRUE = session invalidated (logout, password change)';

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
DROP TABLE IF EXISTS sys_sessions;
