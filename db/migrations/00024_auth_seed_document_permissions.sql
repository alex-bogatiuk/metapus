-- +goose Up
-- Description: Seed permissions for GoodsReceipt document and Stock register

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- GoodsReceipt document permissions
INSERT INTO permissions (code, name, description, resource, action) VALUES
                                                                        ('document:goods_receipt:read', 'Read Goods Receipt', 'View goods receipt documents', 'document:goods_receipt', 'read'),
                                                                        ('document:goods_receipt:create', 'Create Goods Receipt', 'Create goods receipt documents', 'document:goods_receipt', 'create'),
                                                                        ('document:goods_receipt:update', 'Update Goods Receipt', 'Update goods receipt documents', 'document:goods_receipt', 'update'),
                                                                        ('document:goods_receipt:delete', 'Delete Goods Receipt', 'Delete goods receipt documents', 'document:goods_receipt', 'delete'),
                                                                        ('document:goods_receipt:post', 'Post Goods Receipt', 'Post goods receipt documents', 'document:goods_receipt', 'post'),
                                                                        ('document:goods_receipt:unpost', 'Unpost Goods Receipt', 'Unpost goods receipt documents', 'document:goods_receipt', 'unpost')
ON CONFLICT (code) DO NOTHING;

-- Stock register permissions
INSERT INTO permissions (code, name, description, resource, action) VALUES
    ('register:stock:read', 'Read Stock Register', 'View stock balances and movements', 'register:stock', 'read')
ON CONFLICT (code) DO NOTHING;

-- Grant document permissions to admin role
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
         CROSS JOIN permissions p
WHERE r.code = 'admin'
  AND p.code IN (
                 'document:goods_receipt:read',
                 'document:goods_receipt:create',
                 'document:goods_receipt:update',
                 'document:goods_receipt:delete',
                 'document:goods_receipt:post',
                 'document:goods_receipt:unpost',
                 'register:stock:read'
    )
ON CONFLICT DO NOTHING;

-- Grant read permissions to user role
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
         CROSS JOIN permissions p
WHERE r.code = 'user'
  AND p.code IN (
                 'document:goods_receipt:read',
                 'register:stock:read'
    )
ON CONFLICT DO NOTHING;

-- Grant full document permissions to accountant role
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
         CROSS JOIN permissions p
WHERE r.code = 'accountant'
  AND p.code IN (
                 'document:goods_receipt:read',
                 'document:goods_receipt:create',
                 'document:goods_receipt:update',
                 'document:goods_receipt:post',
                 'register:stock:read',
                 'catalog:nomenclature:read',
                 'catalog:counterparty:read',
                 'catalog:warehouse:read',
                 'catalog:unit:read',
                 'catalog:currency:read'
    )
ON CONFLICT DO NOTHING;

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down