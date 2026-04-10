-- +goose Up
-- Description: Seed test users with RBAC roles, security profiles, RLS dimensions,
-- FLS field policies, and CEL policy rules.

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- ════════════════════════════════════════════════════════════════════════════
-- 1. Admin user (always created)
-- ════════════════════════════════════════════════════════════════════════════
INSERT INTO users (id, email, password_hash, first_name, last_name, is_active, is_admin, email_verified)
VALUES (
    'c0000000-0000-0000-0000-000000000001',
    'admin@metapus.io',
    -- bcrypt hash of 'Admin123!'
    '$2a$10$orbwnzP5xw6U9t5nJtWeceN4Fqx0EjsTr/MDbPhJq53V7tZDVtdwm',
    'Admin', 'Admin',
    TRUE, TRUE, TRUE
) ON CONFLICT (email) DO NOTHING;

-- Admin gets admin role
INSERT INTO user_roles (user_id, role_id)
VALUES ('c0000000-0000-0000-0000-000000000001', 'b0000000-0000-0000-0000-000000000001')
ON CONFLICT DO NOTHING;

-- Admin gets full_access security profile
INSERT INTO user_security_profiles (user_id, profile_id)
VALUES ('c0000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000001')
ON CONFLICT DO NOTHING;

-- ════════════════════════════════════════════════════════════════════════════
-- 2. Test organization for RLS testing
-- ════════════════════════════════════════════════════════════════════════════
-- ORG-001 is the main org (should already exist from seed or user creation)
-- ORG-002 is a second org for RLS boundary testing
INSERT INTO cat_organizations (id, code, name, full_name, inn, is_default)
VALUES (
    'd0000000-0000-0000-0000-000000000001',
    'ORG-001', 'ООО Метапус', 'Общество с ограниченной ответственностью "Метапус"',
    '7707123456', TRUE
) ON CONFLICT DO NOTHING;

INSERT INTO cat_organizations (id, code, name, full_name, inn)
VALUES (
    'd0000000-0000-0000-0000-000000000002',
    'ORG-002', 'ООО Второй Офис', 'Общество с ограниченной ответственностью "Второй Офис"',
    '7707654321'
) ON CONFLICT DO NOTHING;

-- ════════════════════════════════════════════════════════════════════════════
-- 3. Test users
-- ════════════════════════════════════════════════════════════════════════════
-- All passwords: Test123!
-- bcrypt hash below matches Test123!

-- Accountant
INSERT INTO users (id, email, password_hash, first_name, last_name, is_active, is_admin, email_verified)
VALUES (
    'c0000000-0000-0000-0000-000000000002',
    'accountant@metapus.io',
    '$2a$10$JmzGNLVB3GTGmWEQrBcWe.k2j1H0T8IdE2cFZFweidw57OpsfTNE.',
    'Иван', 'Бухгалтеров', TRUE, FALSE, TRUE
) ON CONFLICT (email) DO NOTHING;

-- Manager
INSERT INTO users (id, email, password_hash, first_name, last_name, is_active, is_admin, email_verified)
VALUES (
    'c0000000-0000-0000-0000-000000000003',
    'manager@metapus.io',
    '$2a$10$JmzGNLVB3GTGmWEQrBcWe.k2j1H0T8IdE2cFZFweidw57OpsfTNE.',
    'Мария', 'Менеджерова', TRUE, FALSE, TRUE
) ON CONFLICT (email) DO NOTHING;

-- Warehouse keeper
INSERT INTO users (id, email, password_hash, first_name, last_name, is_active, is_admin, email_verified)
VALUES (
    'c0000000-0000-0000-0000-000000000004',
    'warehouse@metapus.io',
    '$2a$10$JmzGNLVB3GTGmWEQrBcWe.k2j1H0T8IdE2cFZFweidw57OpsfTNE.',
    'Пётр', 'Складов', TRUE, FALSE, TRUE
) ON CONFLICT (email) DO NOTHING;

-- Viewer (read-only)
INSERT INTO users (id, email, password_hash, first_name, last_name, is_active, is_admin, email_verified)
VALUES (
    'c0000000-0000-0000-0000-000000000005',
    'viewer@metapus.io',
    '$2a$10$JmzGNLVB3GTGmWEQrBcWe.k2j1H0T8IdE2cFZFweidw57OpsfTNE.',
    'Анна', 'Зрителева', TRUE, FALSE, TRUE
) ON CONFLICT (email) DO NOTHING;

-- Limited manager (complex CEL rules)
INSERT INTO users (id, email, password_hash, first_name, last_name, is_active, is_admin, email_verified)
VALUES (
    'c0000000-0000-0000-0000-000000000006',
    'limited@metapus.io',
    '$2a$10$JmzGNLVB3GTGmWEQrBcWe.k2j1H0T8IdE2cFZFweidw57OpsfTNE.',
    'Олег', 'Ограниченнов', TRUE, FALSE, TRUE
) ON CONFLICT (email) DO NOTHING;

-- ════════════════════════════════════════════════════════════════════════════
-- 4. Assign RBAC roles
-- ════════════════════════════════════════════════════════════════════════════
INSERT INTO user_roles (user_id, role_id) VALUES
    ('c0000000-0000-0000-0000-000000000002', 'b0000000-0000-0000-0000-000000000002'), -- accountant
    ('c0000000-0000-0000-0000-000000000003', 'b0000000-0000-0000-0000-000000000003'), -- manager
    ('c0000000-0000-0000-0000-000000000004', 'b0000000-0000-0000-0000-000000000004'), -- warehouse_keeper
    ('c0000000-0000-0000-0000-000000000005', 'b0000000-0000-0000-0000-000000000005'), -- user (viewer)
    ('c0000000-0000-0000-0000-000000000006', 'b0000000-0000-0000-0000-000000000003')  -- limited = manager role
ON CONFLICT DO NOTHING;

-- ════════════════════════════════════════════════════════════════════════════
-- 5. Create security profiles for test users
-- ════════════════════════════════════════════════════════════════════════════
INSERT INTO security_profiles (id, code, name, description, is_system) VALUES
    ('a0000000-0000-0000-0000-000000000010', 'accountant_profile', 'Профиль бухгалтера', 'RLS: ORG-001 only', FALSE),
    ('a0000000-0000-0000-0000-000000000011', 'manager_profile',    'Профиль менеджера',  'RLS: ORG-001, FLS: hide prices', FALSE),
    ('a0000000-0000-0000-0000-000000000012', 'warehouse_profile',  'Профиль кладовщика', 'RLS: ORG-001, FLS: hide all financial', FALSE),
    ('a0000000-0000-0000-0000-000000000013', 'limited_profile',    'Ограниченный менеджер', 'CEL: amount limits + no edit posted', FALSE)
ON CONFLICT (code) DO NOTHING;

-- ════════════════════════════════════════════════════════════════════════════
-- 6. Assign security profiles to users
-- ════════════════════════════════════════════════════════════════════════════
INSERT INTO user_security_profiles (user_id, profile_id) VALUES
    ('c0000000-0000-0000-0000-000000000002', 'a0000000-0000-0000-0000-000000000010'), -- accountant
    ('c0000000-0000-0000-0000-000000000003', 'a0000000-0000-0000-0000-000000000011'), -- manager
    ('c0000000-0000-0000-0000-000000000004', 'a0000000-0000-0000-0000-000000000012'), -- warehouse
    ('c0000000-0000-0000-0000-000000000005', 'a0000000-0000-0000-0000-000000000002'), -- viewer → system viewer profile
    ('c0000000-0000-0000-0000-000000000006', 'a0000000-0000-0000-0000-000000000013')  -- limited
ON CONFLICT DO NOTHING;

-- ════════════════════════════════════════════════════════════════════════════
-- 7. RLS dimensions — restrict accountant, manager, warehouse to ORG-001
-- ════════════════════════════════════════════════════════════════════════════
INSERT INTO security_profile_dimensions (profile_id, entity_name, dimension_name, allowed_ids) VALUES
    -- Accountant: ORG-001 only
    ('a0000000-0000-0000-0000-000000000010', '', 'organization', ARRAY['d0000000-0000-0000-0000-000000000001']),
    -- Manager: ORG-001 only
    ('a0000000-0000-0000-0000-000000000011', '', 'organization', ARRAY['d0000000-0000-0000-0000-000000000001']),
    -- Warehouse: ORG-001 only
    ('a0000000-0000-0000-0000-000000000012', '', 'organization', ARRAY['d0000000-0000-0000-0000-000000000001']),
    -- Limited: ORG-001 only
    ('a0000000-0000-0000-0000-000000000013', '', 'organization', ARRAY['d0000000-0000-0000-0000-000000000001'])
ON CONFLICT DO NOTHING;

-- ════════════════════════════════════════════════════════════════════════════
-- 8. FLS field policies
-- ════════════════════════════════════════════════════════════════════════════
-- Manager: hide unit_price in documents
INSERT INTO security_profile_field_policies (profile_id, entity_name, action, allowed_fields) VALUES
    ('a0000000-0000-0000-0000-000000000011', 'goods_receipt', 'read', ARRAY['-unit_price']),
    ('a0000000-0000-0000-0000-000000000011', 'goods_issue',   'read', ARRAY['-unit_price'])
ON CONFLICT DO NOTHING;

-- Warehouse: hide all financial fields
INSERT INTO security_profile_field_policies (profile_id, entity_name, action, allowed_fields) VALUES
    ('a0000000-0000-0000-0000-000000000012', 'goods_receipt', 'read', ARRAY['-unit_price', '-amount', '-vat_amount', '-total_amount', '-total_vat']),
    ('a0000000-0000-0000-0000-000000000012', 'goods_issue',   'read', ARRAY['-unit_price', '-amount', '-vat_amount', '-total_amount', '-total_vat'])
ON CONFLICT DO NOTHING;

-- ════════════════════════════════════════════════════════════════════════════
-- 9. CEL policy rules
-- ════════════════════════════════════════════════════════════════════════════
-- Manager: cannot create documents with total_amount > 1,000,000
INSERT INTO security_policy_rules (profile_id, name, description, entity_name, actions, effect, expression, priority) VALUES
    ('a0000000-0000-0000-0000-000000000011',
     'Manager amount limit',
     'Manager cannot create/update documents exceeding 1M',
     'goods_receipt',
     ARRAY['create', 'update'],
     'deny',
     'entity.total_amount > 100000000',
     10),
    ('a0000000-0000-0000-0000-000000000011',
     'Manager amount limit (issue)',
     'Manager cannot create/update goods issues exceeding 1M',
     'goods_issue',
     ARRAY['create', 'update'],
     'deny',
     'entity.total_amount > 100000000',
     10);

-- Warehouse keeper: cannot post/unpost
INSERT INTO security_policy_rules (profile_id, name, description, entity_name, actions, effect, expression, priority) VALUES
    ('a0000000-0000-0000-0000-000000000012',
     'Warehouse no post (receipt)',
     'Warehouse keeper cannot post/unpost goods receipts',
     'goods_receipt',
     ARRAY['post', 'unpost'],
     'deny',
     'true',
     100),
    ('a0000000-0000-0000-0000-000000000012',
     'Warehouse no post (issue)',
     'Warehouse keeper cannot post/unpost goods issues',
     'goods_issue',
     ARRAY['post', 'unpost'],
     'deny',
     'true',
     100);

-- Limited manager: cannot edit posted documents
INSERT INTO security_policy_rules (profile_id, name, description, entity_name, actions, effect, expression, priority) VALUES
    ('a0000000-0000-0000-0000-000000000013',
     'No edit posted (receipt)',
     'Cannot update posted goods receipts',
     'goods_receipt',
     ARRAY['update'],
     'deny',
     'entity.posted == true',
     50),
    ('a0000000-0000-0000-0000-000000000013',
     'No edit posted (issue)',
     'Cannot update posted goods issues',
     'goods_issue',
     ARRAY['update'],
     'deny',
     'entity.posted == true',
     50),
    ('a0000000-0000-0000-0000-000000000013',
     'No delete any entity',
     'Limited manager cannot delete any entity',
     '*',
     ARRAY['delete'],
     'deny',
     'true',
     100);

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
SELECT pg_advisory_lock(hashtext('metapus_migrations'));
DELETE FROM security_policy_rules WHERE profile_id IN (
    'a0000000-0000-0000-0000-000000000010',
    'a0000000-0000-0000-0000-000000000011',
    'a0000000-0000-0000-0000-000000000012',
    'a0000000-0000-0000-0000-000000000013'
);
DELETE FROM security_profile_field_policies WHERE profile_id IN (
    'a0000000-0000-0000-0000-000000000011',
    'a0000000-0000-0000-0000-000000000012'
);
DELETE FROM security_profile_dimensions WHERE profile_id IN (
    'a0000000-0000-0000-0000-000000000010',
    'a0000000-0000-0000-0000-000000000011',
    'a0000000-0000-0000-0000-000000000012',
    'a0000000-0000-0000-0000-000000000013'
);
DELETE FROM user_security_profiles WHERE user_id IN (
    'c0000000-0000-0000-0000-000000000001',
    'c0000000-0000-0000-0000-000000000002',
    'c0000000-0000-0000-0000-000000000003',
    'c0000000-0000-0000-0000-000000000004',
    'c0000000-0000-0000-0000-000000000005',
    'c0000000-0000-0000-0000-000000000006'
);
DELETE FROM security_profiles WHERE id IN (
    'a0000000-0000-0000-0000-000000000010',
    'a0000000-0000-0000-0000-000000000011',
    'a0000000-0000-0000-0000-000000000012',
    'a0000000-0000-0000-0000-000000000013'
);
DELETE FROM user_roles WHERE user_id IN (
    'c0000000-0000-0000-0000-000000000001',
    'c0000000-0000-0000-0000-000000000002',
    'c0000000-0000-0000-0000-000000000003',
    'c0000000-0000-0000-0000-000000000004',
    'c0000000-0000-0000-0000-000000000005',
    'c0000000-0000-0000-0000-000000000006'
);
DELETE FROM cat_organizations WHERE code IN ('ORG-001', 'ORG-002');
DELETE FROM users WHERE email IN (
    'admin@metapus.io', 'accountant@metapus.io', 'manager@metapus.io',
    'warehouse@metapus.io', 'viewer@metapus.io', 'limited@metapus.io'
);
SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
