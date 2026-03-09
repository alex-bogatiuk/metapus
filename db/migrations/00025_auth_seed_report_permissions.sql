-- +goose Up
-- Report permissions

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- Insert report permissions
INSERT INTO permissions (code, name, description, resource, action) VALUES
                                                                        ('report:stock:read', 'View Stock Reports', 'View stock balance and turnover reports', 'report:stock', 'read'),
                                                                        ('report:documents:read', 'View Document Journal', 'View document journal report', 'report:documents', 'read')
ON CONFLICT (code) DO NOTHING;

-- Assign report permissions to existing roles
-- Admin gets all report permissions
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
         CROSS JOIN permissions p
WHERE r.code = 'admin'
  AND p.code IN ('report:stock:read', 'report:documents:read')
ON CONFLICT DO NOTHING;

-- Accountant gets all report permissions
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
         CROSS JOIN permissions p
WHERE r.code = 'accountant'
  AND p.code IN ('report:stock:read', 'report:documents:read')
ON CONFLICT DO NOTHING;

-- Manager gets stock report permissions
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
         CROSS JOIN permissions p
WHERE r.code = 'manager'
  AND p.code IN ('report:stock:read', 'report:documents:read')
ON CONFLICT DO NOTHING;

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions WHERE code IN ('report:stock:read', 'report:documents:read')
);

DELETE FROM permissions WHERE code IN ('report:stock:read', 'report:documents:read');