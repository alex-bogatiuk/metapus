-- +goose Up
-- Seed permissions for GoodsIssue and Inventory documents
-- Migration: 00027_auth_seed_goods_issue_inventory_permissions.sql

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- Insert GoodsIssue permissions
INSERT INTO permissions (code, name, description, resource, action) VALUES
                                                                        ('document:goods_issue:create', 'Create Goods Issue', 'Permission to create goods issue documents', 'document:goods_issue', 'create'),
                                                                        ('document:goods_issue:read', 'Read Goods Issue', 'Permission to view goods issue documents', 'document:goods_issue', 'read'),
                                                                        ('document:goods_issue:update', 'Update Goods Issue', 'Permission to update goods issue documents', 'document:goods_issue', 'update'),
                                                                        ('document:goods_issue:delete', 'Delete Goods Issue', 'Permission to delete goods issue documents', 'document:goods_issue', 'delete'),
                                                                        ('document:goods_issue:post', 'Post Goods Issue', 'Permission to post goods issue documents', 'document:goods_issue', 'post'),
                                                                        ('document:goods_issue:unpost', 'Unpost Goods Issue', 'Permission to unpost goods issue documents', 'document:goods_issue', 'unpost')
ON CONFLICT (code) DO NOTHING;

-- Insert Inventory permissions
INSERT INTO permissions (code, name, description, resource, action) VALUES
                                                                        ('document:inventory:create', 'Create Inventory', 'Permission to create inventory documents', 'document:inventory', 'create'),
                                                                        ('document:inventory:read', 'Read Inventory', 'Permission to view inventory documents', 'document:inventory', 'read'),
                                                                        ('document:inventory:update', 'Update Inventory', 'Permission to update inventory documents', 'document:inventory', 'update'),
                                                                        ('document:inventory:delete', 'Delete Inventory', 'Permission to delete inventory documents', 'document:inventory', 'delete'),
                                                                        ('document:inventory:post', 'Post Inventory', 'Permission to post inventory documents', 'document:inventory', 'post'),
                                                                        ('document:inventory:unpost', 'Unpost Inventory', 'Permission to unpost inventory documents', 'document:inventory', 'unpost')
ON CONFLICT (code) DO NOTHING;

-- Grant GoodsIssue + Inventory permissions to admin
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
         CROSS JOIN permissions p
WHERE r.code = 'admin'
    AND p.code LIKE 'document:goods_issue:%' OR p.code LIKE 'document:inventory:%'
ON CONFLICT DO NOTHING;

-- Grant GoodsIssue + Inventory to accountant
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
         CROSS JOIN permissions p
WHERE r.code = 'accountant'
  AND (p.code LIKE 'document:goods_issue:%' OR p.code LIKE 'document:inventory:%')
ON CONFLICT DO NOTHING;

-- Grant read-only to manager
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
         CROSS JOIN permissions p
WHERE r.code = 'manager'
  AND (p.code = 'document:goods_issue:read' OR p.code = 'document:inventory:read')
ON CONFLICT DO NOTHING;

-- Create warehouse_keeper role
INSERT INTO roles (id, code, name, description, is_system)
VALUES
    (gen_random_uuid(), 'warehouse_keeper', 'Warehouse Keeper', 'Role for warehouse staff managing stock operations', true)
ON CONFLICT (code) DO NOTHING;

-- Grant warehouse_keeper permissions
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
         CROSS JOIN permissions p
WHERE r.code = 'warehouse_keeper'
  AND p.code IN (
                 'document:goods_receipt:read','document:goods_receipt:create','document:goods_receipt:update',
                 'document:goods_issue:read','document:goods_issue:create','document:goods_issue:update',
                 'document:inventory:read','document:inventory:create','document:inventory:update',
                 'catalog:nomenclature:read','catalog:warehouse:read','catalog:unit:read','catalog:counterparty:read',
                 'register:stock:read'
    )
ON CONFLICT DO NOTHING;

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
-- Оставляем seed-данные