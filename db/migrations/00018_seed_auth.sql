-- +goose Up
-- Description: Seed permissions and roles for all entities

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- ── Permissions ────────────────────────────────────────────────────────────
INSERT INTO permissions (code, name, description, resource, action) VALUES
    -- Catalogs: Nomenclature
    ('nomenclature.read',   'Чтение номенклатуры',   'View nomenclature catalog', 'nomenclature', 'read'),
    ('nomenclature.create', 'Создание номенклатуры', 'Create nomenclature items', 'nomenclature', 'create'),
    ('nomenclature.update', 'Изменение номенклатуры', 'Update nomenclature items', 'nomenclature', 'update'),
    ('nomenclature.delete', 'Удаление номенклатуры', 'Delete nomenclature items', 'nomenclature', 'delete'),
    -- Catalogs: Counterparties
    ('counterparty.read',   'Чтение контрагентов',   'View counterparties catalog', 'counterparty', 'read'),
    ('counterparty.create', 'Создание контрагентов', 'Create counterparties', 'counterparty', 'create'),
    ('counterparty.update', 'Изменение контрагентов', 'Update counterparties', 'counterparty', 'update'),
    ('counterparty.delete', 'Удаление контрагентов', 'Delete counterparties', 'counterparty', 'delete'),
    -- Catalogs: Warehouses
    ('warehouse.read',   'Чтение складов',   'View warehouses catalog', 'warehouse', 'read'),
    ('warehouse.create', 'Создание складов', 'Create warehouses', 'warehouse', 'create'),
    ('warehouse.update', 'Изменение складов', 'Update warehouses', 'warehouse', 'update'),
    ('warehouse.delete', 'Удаление складов', 'Delete warehouses', 'warehouse', 'delete'),
    -- Catalogs: Units
    ('unit.read',   'Чтение единиц измерения',   'View units catalog', 'unit', 'read'),
    ('unit.create', 'Создание единиц измерения', 'Create units', 'unit', 'create'),
    ('unit.update', 'Изменение единиц измерения', 'Update units', 'unit', 'update'),
    ('unit.delete', 'Удаление единиц измерения', 'Delete units', 'unit', 'delete'),
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
    -- Catalogs: VAT Rates
    ('vat_rate.read',   'Чтение ставок НДС',   'View VAT rates catalog', 'vat_rate', 'read'),
    ('vat_rate.create', 'Создание ставок НДС', 'Create VAT rates', 'vat_rate', 'create'),
    ('vat_rate.update', 'Изменение ставок НДС', 'Update VAT rates', 'vat_rate', 'update'),
    ('vat_rate.delete', 'Удаление ставок НДС', 'Delete VAT rates', 'vat_rate', 'delete'),
    -- Catalogs: Contracts
    ('contract.read',   'Чтение договоров',   'View contracts catalog', 'contract', 'read'),
    ('contract.create', 'Создание договоров', 'Create contracts', 'contract', 'create'),
    ('contract.update', 'Изменение договоров', 'Update contracts', 'contract', 'update'),
    ('contract.delete', 'Удаление договоров', 'Delete contracts', 'contract', 'delete'),
    -- Documents: Goods Receipt
    ('goods_receipt.read',   'Чтение поступлений',   'View goods receipts', 'goods_receipt', 'read'),
    ('goods_receipt.create', 'Создание поступлений', 'Create goods receipts', 'goods_receipt', 'create'),
    ('goods_receipt.update', 'Изменение поступлений', 'Update goods receipts', 'goods_receipt', 'update'),
    ('goods_receipt.delete', 'Удаление поступлений', 'Delete goods receipts', 'goods_receipt', 'delete'),
    ('goods_receipt.post',   'Проведение поступлений', 'Post goods receipts', 'goods_receipt', 'post'),
    ('goods_receipt.unpost', 'Отмена проведения поступлений', 'Unpost goods receipts', 'goods_receipt', 'unpost'),
    -- Documents: Goods Issue
    ('goods_issue.read',   'Чтение расходов',   'View goods issues', 'goods_issue', 'read'),
    ('goods_issue.create', 'Создание расходов', 'Create goods issues', 'goods_issue', 'create'),
    ('goods_issue.update', 'Изменение расходов', 'Update goods issues', 'goods_issue', 'update'),
    ('goods_issue.delete', 'Удаление расходов', 'Delete goods issues', 'goods_issue', 'delete'),
    ('goods_issue.post',   'Проведение расходов', 'Post goods issues', 'goods_issue', 'post'),
    ('goods_issue.unpost', 'Отмена проведения расходов', 'Unpost goods issues', 'goods_issue', 'unpost'),
    -- Registers
    ('register_stock.read', 'Чтение регистра остатков', 'View stock register', 'register_stock', 'read'),
    -- Reports
    ('report_stock.read',     'Отчёт по остаткам',   'View stock balance report', 'report_stock', 'read'),
    ('report_documents.read', 'Журнал документов',    'View documents journal report', 'report_documents', 'read'),
    -- Admin
    ('admin.users',  'Управление пользователями', 'User management', 'admin', 'users'),
    ('admin.roles',  'Управление ролями',         'Role management', 'admin', 'roles')
ON CONFLICT (code) DO NOTHING;

-- ── Roles ──────────────────────────────────────────────────────────────────
INSERT INTO roles (id, code, name, description, is_system) VALUES
    ('b0000000-0000-0000-0000-000000000001', 'admin',            'Администратор',     'Full access to all features', TRUE),
    ('b0000000-0000-0000-0000-000000000002', 'accountant',       'Бухгалтер',         'Read catalogs, full document and report access', TRUE),
    ('b0000000-0000-0000-0000-000000000003', 'manager',          'Менеджер',          'Full catalog access, limited documents', TRUE),
    ('b0000000-0000-0000-0000-000000000004', 'warehouse_keeper', 'Кладовщик',         'Stock-related documents only', TRUE),
    ('b0000000-0000-0000-0000-000000000005', 'user',             'Пользователь',      'Basic read-only access', TRUE)
ON CONFLICT (code) DO NOTHING;

-- ── Role ↔ Permission assignments ──────────────────────────────────────────
-- Admin: ALL permissions
INSERT INTO role_permissions (role_id, permission_id)
SELECT 'b0000000-0000-0000-0000-000000000001', id FROM permissions
ON CONFLICT DO NOTHING;

-- Accountant: read catalogs + full documents + reports + registers
INSERT INTO role_permissions (role_id, permission_id)
SELECT 'b0000000-0000-0000-0000-000000000002', id FROM permissions
WHERE action = 'read'
   OR resource IN ('goods_receipt', 'goods_issue', 'report_stock', 'report_documents', 'register_stock')
ON CONFLICT DO NOTHING;

-- Manager: full catalogs + read/create/update documents + reports
INSERT INTO role_permissions (role_id, permission_id)
SELECT 'b0000000-0000-0000-0000-000000000003', id FROM permissions
WHERE (resource IN ('nomenclature', 'counterparty', 'warehouse', 'unit', 'currency', 'organization', 'vat_rate', 'contract'))
   OR (resource IN ('goods_receipt', 'goods_issue') AND action IN ('read', 'create', 'update'))
   OR resource IN ('report_stock', 'report_documents', 'register_stock')
ON CONFLICT DO NOTHING;

-- Warehouse keeper: read catalogs + goods receipt/issue (read, create, update, post, unpost) + stock register/report
INSERT INTO role_permissions (role_id, permission_id)
SELECT 'b0000000-0000-0000-0000-000000000004', id FROM permissions
WHERE (resource IN ('nomenclature', 'counterparty', 'warehouse', 'unit', 'currency', 'organization', 'vat_rate', 'contract') AND action = 'read')
   OR resource IN ('goods_receipt', 'goods_issue', 'register_stock', 'report_stock')
ON CONFLICT DO NOTHING;

-- User: read-only everything
INSERT INTO role_permissions (role_id, permission_id)
SELECT 'b0000000-0000-0000-0000-000000000005', id FROM permissions
WHERE action = 'read'
ON CONFLICT DO NOTHING;

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
SELECT pg_advisory_lock(hashtext('metapus_migrations'));
DELETE FROM role_permissions;
DELETE FROM roles;
DELETE FROM permissions;
SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
