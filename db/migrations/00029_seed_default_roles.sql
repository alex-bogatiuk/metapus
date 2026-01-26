-- +goose Up
-- Seed default roles if not exist

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- Используем фиксированный UUID вместо строки 'default'
INSERT INTO roles (id, code, name, description, is_system)
VALUES
    (gen_random_uuid(), 'admin', 'Администратор', 'Полный доступ к системе', true),
    (gen_random_uuid(), 'accountant', 'Бухгалтер', 'Работа с документами и отчётами', true),
    (gen_random_uuid(), 'manager', 'Менеджер', 'Работа со справочниками и документами', true),
    (gen_random_uuid(), 'warehouse_keeper', 'Кладовщик', 'Складские операции', true),
    (gen_random_uuid(), 'user', 'Пользователь', 'Базовый доступ только на чтение', true)
ON CONFLICT (code) DO NOTHING;

-- Assign all permissions to admin role
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.code = 'admin'
ON CONFLICT DO NOTHING;

-- Assign relevant permissions to accountant role
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.code = 'accountant' 
  
  AND (
    p.code LIKE 'catalog:%:read'
    OR p.code LIKE 'document:%'
    OR p.code LIKE 'register:%:read'
    OR p.code LIKE 'report:%'
  )
ON CONFLICT DO NOTHING;

-- Assign relevant permissions to manager role
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.code = 'manager' 
  
  AND (
    p.code LIKE 'catalog:%'
    OR p.code LIKE 'document:%:read'
    OR p.code LIKE 'document:%:create'
    OR p.code LIKE 'register:%:read'
  )
ON CONFLICT DO NOTHING;

-- Assign relevant permissions to warehouse_keeper role
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.code = 'warehouse_keeper' 
  
  AND (
    p.code LIKE 'catalog:nomenclature:read'
    OR p.code LIKE 'catalog:warehouse:read'
    OR p.code LIKE 'catalog:unit:read'
    OR p.code LIKE 'document:goods_receipt:%'
    OR p.code LIKE 'document:goods_issue:%'
    OR p.code LIKE 'document:inventory:%'
    OR p.code LIKE 'register:stock:read'
  )
ON CONFLICT DO NOTHING;

-- Assign read-only permissions to user role
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.code = 'user' 
  
  AND p.code LIKE '%:read'
ON CONFLICT DO NOTHING;

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
DELETE FROM role_permissions 
WHERE role_id IN (SELECT id FROM roles WHERE is_system = true);

DELETE FROM roles WHERE is_system = true;