-- +goose Up
-- Migration: Create auth tables (users, roles, permissions, sessions)
-- Этап 1: Авторизация и RBAC

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- Users table
CREATE TABLE IF NOT EXISTS users (
                                     id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

                                     email VARCHAR(255) NOT NULL,
                                     password_hash VARCHAR(255) NOT NULL,
                                     first_name VARCHAR(100),
                                     last_name VARCHAR(100),
                                     is_active BOOLEAN NOT NULL DEFAULT true,
                                     is_admin BOOLEAN NOT NULL DEFAULT false,
                                     email_verified BOOLEAN NOT NULL DEFAULT false,
                                     email_verified_at TIMESTAMPTZ,
                                     last_login_at TIMESTAMPTZ,
                                     failed_login_attempts INT NOT NULL DEFAULT 0,
                                     locked_until TIMESTAMPTZ,
                                     deletion_mark BOOLEAN NOT NULL DEFAULT false,
                                     version INT NOT NULL DEFAULT 1,
                                     attributes JSONB DEFAULT '{}',

                                     CONSTRAINT users_email_unique UNIQUE (email)
);

-- Indexes for users

CREATE INDEX IF NOT EXISTS idx_users_email ON users(email) WHERE deletion_mark = FALSE;
CREATE INDEX IF NOT EXISTS idx_users_is_active ON users(is_active) WHERE deletion_mark = FALSE;

-- Roles table
CREATE TABLE IF NOT EXISTS roles (
                                     id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

                                     code VARCHAR(50) NOT NULL,
                                     name VARCHAR(100) NOT NULL,
                                     description TEXT,
                                     is_system BOOLEAN NOT NULL DEFAULT false, -- system roles cannot be deleted

                                     CONSTRAINT roles_code_unique UNIQUE (code)
);



-- Permissions table (predefined list of permissions)
CREATE TABLE IF NOT EXISTS permissions (
                                           id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                           code VARCHAR(100) NOT NULL UNIQUE, -- e.g., "catalog:nomenclature:create"
                                           name VARCHAR(200) NOT NULL,
                                           description TEXT,
                                           resource VARCHAR(50) NOT NULL, -- e.g., "catalog", "document", "register"
                                           action VARCHAR(50) NOT NULL -- e.g., "create", "read", "update", "delete", "post"
);

CREATE INDEX IF NOT EXISTS idx_permissions_resource ON permissions(resource);
CREATE INDEX IF NOT EXISTS idx_permissions_code ON permissions(code);

-- Role permissions (many-to-many)
CREATE TABLE IF NOT EXISTS role_permissions (
                                                role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
                                                permission_id UUID NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,

                                                PRIMARY KEY (role_id, permission_id)
);

-- User roles (many-to-many)
CREATE TABLE IF NOT EXISTS user_roles (
                                          user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                                          role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
                                          granted_by UUID REFERENCES users(id),

                                          PRIMARY KEY (user_id, role_id)
);

CREATE INDEX IF NOT EXISTS idx_user_roles_user_id ON user_roles(user_id);
CREATE INDEX IF NOT EXISTS idx_user_roles_role_id ON user_roles(role_id);

-- User organizations (which organizations user has access to)
CREATE TABLE IF NOT EXISTS user_organizations (
                                                  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                                                  organization_id UUID NOT NULL,
                                                  is_default BOOLEAN NOT NULL DEFAULT false,

                                                  PRIMARY KEY (user_id, organization_id)
);

CREATE INDEX IF NOT EXISTS idx_user_organizations_user_id ON user_organizations(user_id);

-- Refresh tokens table
CREATE TABLE IF NOT EXISTS refresh_tokens (
                                              id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                              user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                                              token_hash VARCHAR(255) NOT NULL UNIQUE,
                                              expires_at TIMESTAMPTZ NOT NULL,
                                              created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
                                              revoked_at TIMESTAMPTZ,
                                              revoked_reason VARCHAR(100),
                                              user_agent TEXT,
                                              ip_address INET
);

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_id ON refresh_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_expires ON refresh_tokens(expires_at) WHERE revoked_at IS NULL;

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS user_organizations;
DROP TABLE IF EXISTS user_roles;
DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS permissions;
DROP TABLE IF EXISTS roles;
DROP TABLE IF EXISTS users;