-- +goose Up
-- Description: Seed permissions for automation entities (accounts, channels, rules, history)

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- ── Automation Permissions ────────────────────────────────────────────────
INSERT INTO permissions (code, name, description, resource, action) VALUES
    -- Automation: Accounts
    ('automation_account.read',   'Чтение аккаунтов автоматизации',   'View automation accounts', 'automation_account', 'read'),
    ('automation_account.create', 'Создание аккаунтов автоматизации', 'Create automation accounts', 'automation_account', 'create'),
    ('automation_account.update', 'Изменение аккаунтов автоматизации', 'Update automation accounts', 'automation_account', 'update'),
    ('automation_account.delete', 'Удаление аккаунтов автоматизации', 'Delete automation accounts', 'automation_account', 'delete'),
    -- Automation: Channels
    ('automation_channel.read',   'Чтение каналов доставки',   'View automation channels', 'automation_channel', 'read'),
    ('automation_channel.create', 'Создание каналов доставки', 'Create automation channels', 'automation_channel', 'create'),
    ('automation_channel.update', 'Изменение каналов доставки', 'Update automation channels', 'automation_channel', 'update'),
    ('automation_channel.delete', 'Удаление каналов доставки', 'Delete automation channels', 'automation_channel', 'delete'),
    -- Automation: Rules
    ('automation_rule.read',   'Чтение правил автоматизации',   'View automation rules', 'automation_rule', 'read'),
    ('automation_rule.create', 'Создание правил автоматизации', 'Create automation rules', 'automation_rule', 'create'),
    ('automation_rule.update', 'Изменение правил автоматизации', 'Update automation rules', 'automation_rule', 'update'),
    ('automation_rule.delete', 'Удаление правил автоматизации', 'Delete automation rules', 'automation_rule', 'delete'),
    -- Automation: History (read-only)
    ('automation_history.read', 'Чтение истории автоматизации', 'View automation execution history', 'automation_history', 'read')
ON CONFLICT (code) DO NOTHING;

-- Grant all automation permissions to Admin role
INSERT INTO role_permissions (role_id, permission_id)
SELECT 'b0000000-0000-0000-0000-000000000001', id FROM permissions
WHERE resource LIKE 'automation_%'
ON CONFLICT DO NOTHING;

-- Grant read-only automation access to Accountant
INSERT INTO role_permissions (role_id, permission_id)
SELECT 'b0000000-0000-0000-0000-000000000002', id FROM permissions
WHERE resource LIKE 'automation_%' AND action = 'read'
ON CONFLICT DO NOTHING;

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
SELECT pg_advisory_lock(hashtext('metapus_migrations'));
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE resource LIKE 'automation_%');
DELETE FROM permissions WHERE resource LIKE 'automation_%';
SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
