-- +goose Up
-- Seed default roles for each tenant
-- This should be run after tenant creation or as part of tenant setup

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- Create default roles for 'default' tenant (используем фиксированный UUID вместо строки)
INSERT INTO roles (id, code, name, description, is_system) VALUES
                                                                          (gen_random_uuid(), 'admin', 'Administrator', 'Full system access', true),
                                                                          (gen_random_uuid(), 'accountant', 'Accountant', 'Access to documents and registers', true),
                                                                          (gen_random_uuid(), 'manager', 'Manager', 'Access to catalogs and documents', true),
                                                                          (gen_random_uuid(), 'user', 'User', 'Basic read access', true)
ON CONFLICT (code) DO NOTHING;

-- Assign all permissions to admin role
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
         CROSS JOIN permissions p
WHERE r.code = 'admin'
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- Assign permissions to accountant role
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
         CROSS JOIN permissions p
WHERE r.code = 'accountant'
  
  AND (
    p.code LIKE 'catalog:%:read'
        OR p.code LIKE 'document:%'
        OR p.code LIKE 'register:%:read'
    )
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- Assign permissions to manager role
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
         CROSS JOIN permissions p
WHERE r.code = 'manager'
  
  AND (
    p.code LIKE 'catalog:%'
        OR p.code LIKE 'document:%:read'
        OR p.code LIKE 'document:%:create'
    )
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- Assign read permissions to user role
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
         CROSS JOIN permissions p
WHERE r.code = 'user'
  
  AND p.code LIKE '%:read'
ON CONFLICT (role_id, permission_id) DO NOTHING;

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down