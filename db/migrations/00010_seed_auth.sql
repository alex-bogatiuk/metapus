-- +goose Up
-- Description: Seed permissions and roles for all entities

SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));

-- ── Permissions ────────────────────────────────────────────────────────────
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

-- ── Roles ──────────────────────────────────────────────────────────────────
INSERT INTO auth_roles (id, code, name, description, is_system) VALUES
    ('b0000000-0000-0000-0000-000000000001', 'admin',            'Администратор',     'Full access to all features', TRUE),
    ('b0000000-0000-0000-0000-000000000002', 'accountant',       'Бухгалтер',         'Read catalogs, full document and report access', TRUE),
    ('b0000000-0000-0000-0000-000000000003', 'manager',          'Менеджер',          'Full catalog access, limited documents', TRUE),
    ('b0000000-0000-0000-0000-000000000005', 'user',             'Пользователь',      'Basic read-only access', TRUE)
ON CONFLICT (code) DO NOTHING;

-- ── Role ↔ Permission assignments ──────────────────────────────────────────
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


-- +goose Down
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DELETE FROM auth_role_permissions;
DELETE FROM auth_roles;
DELETE FROM auth_permissions;
