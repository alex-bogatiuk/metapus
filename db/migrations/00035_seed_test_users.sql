-- +goose Up
-- Seed: test users with different security profiles for authorization testing.
-- Users: accountant, manager, warehouse keeper, viewer, limited manager.
-- Covers: RBAC roles, RLS dimensions, FLS field policies, CEL policy rules.

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- ============================================================================
-- 0. Create a second organization for RLS testing
-- ============================================================================
INSERT INTO cat_organizations (id, code, name, full_name, inn, kpp, legal_address, version, deletion_mark, attributes)
VALUES (
    'a0000000-0000-0000-0000-000000000002'::uuid,
    'ORG-002',
    'ООО Василёк',
    'Общество с ограниченной ответственностью "Василёк"',
    '7700000002', '770002001',
    'г. Москва, ул. Цветочная, 5',
    1, false, '{}'
)
ON CONFLICT (code) WHERE deletion_mark = FALSE DO NOTHING;

-- ============================================================================
-- 1. Create test users (bcrypt cost=10 hashes)
-- ============================================================================

-- 1a. Accountant
INSERT INTO users (id, email, password_hash, first_name, last_name, is_active, is_admin, email_verified, version)
VALUES (
    'b0000000-0000-0000-0000-000000000001'::uuid,
    'accountant@metapus.io',
    '$2a$10$msoNEBOfkuLd0WRvbX0N5uTnY/AmHBQ5ykGOP6om6rTShdVz1mGSu',
    'Мария', 'Иванова',
    true, false, true, 1
)
ON CONFLICT (email) DO NOTHING;

-- 1b. Manager
INSERT INTO users (id, email, password_hash, first_name, last_name, is_active, is_admin, email_verified, version)
VALUES (
    'b0000000-0000-0000-0000-000000000002'::uuid,
    'manager@metapus.io',
    '$2a$10$X4RU3KQ9876JTUt8lZnMquKtYy2wlUPxs0fvPxSGsltWJAuhha7uu',
    'Алексей', 'Петров',
    true, false, true, 1
)
ON CONFLICT (email) DO NOTHING;

-- 1c. Warehouse Keeper
INSERT INTO users (id, email, password_hash, first_name, last_name, is_active, is_admin, email_verified, version)
VALUES (
    'b0000000-0000-0000-0000-000000000003'::uuid,
    'warehouse@metapus.io',
    '$2a$10$37nvrNHwVJEBwWZgzvhmOu/VGN/Y2Qem9QVgIlBZCIPeKIqVYP32.',
    'Сергей', 'Кузнецов',
    true, false, true, 1
)
ON CONFLICT (email) DO NOTHING;

-- 1d. Viewer (read-only)
INSERT INTO users (id, email, password_hash, first_name, last_name, is_active, is_admin, email_verified, version)
VALUES (
    'b0000000-0000-0000-0000-000000000004'::uuid,
    'viewer@metapus.io',
    '$2a$10$XTUxIg9kGqGKJrGe3u30l.VacHzFFsFmYCuRh/hdj/bj6ELRZwjz2',
    'Елена', 'Смирнова',
    true, false, true, 1
)
ON CONFLICT (email) DO NOTHING;

-- 1e. Limited Manager
INSERT INTO users (id, email, password_hash, first_name, last_name, is_active, is_admin, email_verified, version)
VALUES (
    'b0000000-0000-0000-0000-000000000005'::uuid,
    'limited@metapus.io',
    '$2a$10$I3i97amyCxAQHykcSdja2u0Q6s7bP4bMvam34nY3BlglHtmvGVW0i',
    'Дмитрий', 'Новиков',
    true, false, true, 1
)
ON CONFLICT (email) DO NOTHING;

-- ============================================================================
-- 2. Assign RBAC roles
-- ============================================================================
INSERT INTO user_roles (user_id, role_id)
SELECT 'b0000000-0000-0000-0000-000000000001'::uuid, id FROM roles WHERE code = 'accountant'
ON CONFLICT DO NOTHING;

INSERT INTO user_roles (user_id, role_id)
SELECT 'b0000000-0000-0000-0000-000000000002'::uuid, id FROM roles WHERE code = 'manager'
ON CONFLICT DO NOTHING;

INSERT INTO user_roles (user_id, role_id)
SELECT 'b0000000-0000-0000-0000-000000000003'::uuid, id FROM roles WHERE code = 'warehouse_keeper'
ON CONFLICT DO NOTHING;

INSERT INTO user_roles (user_id, role_id)
SELECT 'b0000000-0000-0000-0000-000000000004'::uuid, id FROM roles WHERE code = 'user'
ON CONFLICT DO NOTHING;

INSERT INTO user_roles (user_id, role_id)
SELECT 'b0000000-0000-0000-0000-000000000005'::uuid, id FROM roles WHERE code = 'manager'
ON CONFLICT DO NOTHING;

-- ============================================================================
-- 3. Assign organizations
-- ============================================================================
-- Accountant → ORG-001 only
INSERT INTO user_organizations (user_id, organization_id, is_default)
SELECT 'b0000000-0000-0000-0000-000000000001'::uuid, id, true
FROM cat_organizations WHERE code = 'ORG-001' AND deletion_mark = FALSE
ON CONFLICT DO NOTHING;

-- Manager → ORG-001 only
INSERT INTO user_organizations (user_id, organization_id, is_default)
SELECT 'b0000000-0000-0000-0000-000000000002'::uuid, id, true
FROM cat_organizations WHERE code = 'ORG-001' AND deletion_mark = FALSE
ON CONFLICT DO NOTHING;

-- Warehouse → ORG-001 only
INSERT INTO user_organizations (user_id, organization_id, is_default)
SELECT 'b0000000-0000-0000-0000-000000000003'::uuid, id, true
FROM cat_organizations WHERE code = 'ORG-001' AND deletion_mark = FALSE
ON CONFLICT DO NOTHING;

-- Viewer → both ORG-001 and ORG-002
INSERT INTO user_organizations (user_id, organization_id, is_default)
SELECT 'b0000000-0000-0000-0000-000000000004'::uuid, id, true
FROM cat_organizations WHERE code = 'ORG-001' AND deletion_mark = FALSE
ON CONFLICT DO NOTHING;

INSERT INTO user_organizations (user_id, organization_id, is_default)
SELECT 'b0000000-0000-0000-0000-000000000004'::uuid, id, false
FROM cat_organizations WHERE code = 'ORG-002' AND deletion_mark = FALSE
ON CONFLICT DO NOTHING;

-- Limited → both ORG-001 and ORG-002
INSERT INTO user_organizations (user_id, organization_id, is_default)
SELECT 'b0000000-0000-0000-0000-000000000005'::uuid, id, true
FROM cat_organizations WHERE code = 'ORG-001' AND deletion_mark = FALSE
ON CONFLICT DO NOTHING;

INSERT INTO user_organizations (user_id, organization_id, is_default)
SELECT 'b0000000-0000-0000-0000-000000000005'::uuid, id, false
FROM cat_organizations WHERE code = 'ORG-002' AND deletion_mark = FALSE
ON CONFLICT DO NOTHING;

-- ============================================================================
-- 4. Create security profiles
-- ============================================================================

-- 4a. Accountant profile — full data access within org, all fields visible
INSERT INTO security_profiles (id, code, name, description, is_system)
VALUES (
    'c0000000-0000-0000-0000-000000000001'::uuid,
    'accountant_profile',
    'Бухгалтер',
    'Полный доступ к документам и финансовым полям в рамках организации',
    false
)
ON CONFLICT (code) DO NOTHING;

-- 4b. Manager profile — limited financial visibility, amount cap via CEL
INSERT INTO security_profiles (id, code, name, description, is_system)
VALUES (
    'c0000000-0000-0000-0000-000000000002'::uuid,
    'manager_profile',
    'Менеджер',
    'Управление справочниками, ограниченный доступ к финансам документов, лимит суммы',
    false
)
ON CONFLICT (code) DO NOTHING;

-- 4c. Warehouse profile — no financial fields, no posting
INSERT INTO security_profiles (id, code, name, description, is_system)
VALUES (
    'c0000000-0000-0000-0000-000000000003'::uuid,
    'warehouse_profile',
    'Кладовщик',
    'Работа со складскими документами без доступа к ценам и проведению',
    false
)
ON CONFLICT (code) DO NOTHING;

-- 4d. Limited manager — CEL blocks editing posted docs and deleting
INSERT INTO security_profiles (id, code, name, description, is_system)
VALUES (
    'c0000000-0000-0000-0000-000000000004'::uuid,
    'limited_manager_profile',
    'Ограниченный менеджер',
    'Менеджер без права редактирования проведённых документов и удаления',
    false
)
ON CONFLICT (code) DO NOTHING;

-- ============================================================================
-- 5. RLS dimensions (organization-scoped access)
-- ============================================================================

-- Accountant → only ORG-001
INSERT INTO security_profile_dimensions (profile_id, dimension_name, allowed_ids)
SELECT 'c0000000-0000-0000-0000-000000000001'::uuid, 'organization', ARRAY[o.id]
FROM cat_organizations o WHERE o.code = 'ORG-001' AND o.deletion_mark = FALSE
ON CONFLICT (profile_id, dimension_name) DO NOTHING;

-- Manager → only ORG-001
INSERT INTO security_profile_dimensions (profile_id, dimension_name, allowed_ids)
SELECT 'c0000000-0000-0000-0000-000000000002'::uuid, 'organization', ARRAY[o.id]
FROM cat_organizations o WHERE o.code = 'ORG-001' AND o.deletion_mark = FALSE
ON CONFLICT (profile_id, dimension_name) DO NOTHING;

-- Warehouse → only ORG-001
INSERT INTO security_profile_dimensions (profile_id, dimension_name, allowed_ids)
SELECT 'c0000000-0000-0000-0000-000000000003'::uuid, 'organization', ARRAY[o.id]
FROM cat_organizations o WHERE o.code = 'ORG-001' AND o.deletion_mark = FALSE
ON CONFLICT (profile_id, dimension_name) DO NOTHING;

-- Viewer → uses existing 'viewer' profile (no custom dimensions → falls back to JWT orgs)
-- Limited → no RLS dimensions (uses JWT orgs: both ORG-001 and ORG-002)

-- ============================================================================
-- 6. FLS field policies
-- ============================================================================

-- 6a. Manager: read documents — hide unit_price
INSERT INTO security_profile_field_policies (profile_id, entity_name, action, allowed_fields, table_parts)
VALUES (
    'c0000000-0000-0000-0000-000000000002'::uuid,
    'goods_receipt', 'read',
    ARRAY['*', '-unit_price'],
    '{"lines": ["*", "-unit_price"]}'::jsonb
)
ON CONFLICT (profile_id, entity_name, action) DO NOTHING;

INSERT INTO security_profile_field_policies (profile_id, entity_name, action, allowed_fields, table_parts)
VALUES (
    'c0000000-0000-0000-0000-000000000002'::uuid,
    'goods_issue', 'read',
    ARRAY['*', '-unit_price'],
    '{"lines": ["*", "-unit_price"]}'::jsonb
)
ON CONFLICT (profile_id, entity_name, action) DO NOTHING;

-- 6b. Warehouse: read documents — hide all financial fields
INSERT INTO security_profile_field_policies (profile_id, entity_name, action, allowed_fields, table_parts)
VALUES (
    'c0000000-0000-0000-0000-000000000003'::uuid,
    'goods_receipt', 'read',
    ARRAY['*', '-unit_price', '-amount', '-total_amount', '-total_vat', '-discount_amount'],
    '{"lines": ["*", "-unit_price", "-amount", "-vat_amount", "-discount_amount"]}'::jsonb
)
ON CONFLICT (profile_id, entity_name, action) DO NOTHING;

INSERT INTO security_profile_field_policies (profile_id, entity_name, action, allowed_fields, table_parts)
VALUES (
    'c0000000-0000-0000-0000-000000000003'::uuid,
    'goods_issue', 'read',
    ARRAY['*', '-unit_price', '-amount', '-total_amount', '-total_vat', '-discount_amount'],
    '{"lines": ["*", "-unit_price", "-amount", "-vat_amount", "-discount_amount"]}'::jsonb
)
ON CONFLICT (profile_id, entity_name, action) DO NOTHING;

-- 6c. Warehouse: write documents — block financial field changes
INSERT INTO security_profile_field_policies (profile_id, entity_name, action, allowed_fields, table_parts)
VALUES (
    'c0000000-0000-0000-0000-000000000003'::uuid,
    'goods_receipt', 'write',
    ARRAY['*', '-unit_price', '-amount', '-total_amount', '-total_vat', '-discount_amount', '-discount_percent'],
    '{"lines": ["*", "-unit_price", "-amount", "-vat_amount", "-discount_amount", "-discount_percent"]}'::jsonb
)
ON CONFLICT (profile_id, entity_name, action) DO NOTHING;

INSERT INTO security_profile_field_policies (profile_id, entity_name, action, allowed_fields, table_parts)
VALUES (
    'c0000000-0000-0000-0000-000000000003'::uuid,
    'goods_issue', 'write',
    ARRAY['*', '-unit_price', '-amount', '-total_amount', '-total_vat', '-discount_amount', '-discount_percent'],
    '{"lines": ["*", "-unit_price", "-amount", "-vat_amount", "-discount_amount", "-discount_percent"]}'::jsonb
)
ON CONFLICT (profile_id, entity_name, action) DO NOTHING;

-- ============================================================================
-- 7. CEL policy rules
-- ============================================================================

-- 7a. Manager: deny create/update on documents with total > 50,000 RUB (5,000,000 minor units)
INSERT INTO security_policy_rules (id, profile_id, name, description, entity_name, actions, expression, effect, priority, enabled)
VALUES (
    'd0000000-0000-0000-0000-000000000001'::uuid,
    'c0000000-0000-0000-0000-000000000002'::uuid,
    'Лимит суммы документа',
    'Запрет создания и редактирования документов с суммой более 50 000 ₽',
    'goods_receipt',
    ARRAY['create', 'update'],
    'doc.total_amount > 5000000',
    'deny',
    100,
    true
);

INSERT INTO security_policy_rules (id, profile_id, name, description, entity_name, actions, expression, effect, priority, enabled)
VALUES (
    'd0000000-0000-0000-0000-000000000002'::uuid,
    'c0000000-0000-0000-0000-000000000002'::uuid,
    'Лимит суммы реализации',
    'Запрет создания и редактирования реализаций с суммой более 50 000 ₽',
    'goods_issue',
    ARRAY['create', 'update'],
    'doc.total_amount > 5000000',
    'deny',
    100,
    true
);

-- 7b. Warehouse: deny post/unpost actions
INSERT INTO security_policy_rules (id, profile_id, name, description, entity_name, actions, expression, effect, priority, enabled)
VALUES (
    'd0000000-0000-0000-0000-000000000003'::uuid,
    'c0000000-0000-0000-0000-000000000003'::uuid,
    'Запрет проведения',
    'Кладовщик не может проводить и распроводить документы',
    '*',
    ARRAY['post', 'unpost'],
    'true',
    'deny',
    100,
    true
);

-- 7c. Limited manager: deny editing posted documents
INSERT INTO security_policy_rules (id, profile_id, name, description, entity_name, actions, expression, effect, priority, enabled)
VALUES (
    'd0000000-0000-0000-0000-000000000004'::uuid,
    'c0000000-0000-0000-0000-000000000004'::uuid,
    'Запрет редактирования проведённых',
    'Нельзя изменять документы, которые уже проведены',
    '*',
    ARRAY['update'],
    'doc.posted == true',
    'deny',
    100,
    true
);

-- 7d. Limited manager: deny delete on all entities
INSERT INTO security_policy_rules (id, profile_id, name, description, entity_name, actions, expression, effect, priority, enabled)
VALUES (
    'd0000000-0000-0000-0000-000000000005'::uuid,
    'c0000000-0000-0000-0000-000000000004'::uuid,
    'Запрет удаления',
    'Ограниченный менеджер не может удалять никакие сущности',
    '*',
    ARRAY['delete'],
    'true',
    'deny',
    100,
    true
);

-- ============================================================================
-- 8. Assign security profiles to users
-- ============================================================================
INSERT INTO user_security_profiles (user_id, profile_id)
VALUES
    ('b0000000-0000-0000-0000-000000000001'::uuid, 'c0000000-0000-0000-0000-000000000001'::uuid),
    ('b0000000-0000-0000-0000-000000000002'::uuid, 'c0000000-0000-0000-0000-000000000002'::uuid),
    ('b0000000-0000-0000-0000-000000000003'::uuid, 'c0000000-0000-0000-0000-000000000003'::uuid),
    ('b0000000-0000-0000-0000-000000000005'::uuid, 'c0000000-0000-0000-0000-000000000004'::uuid)
ON CONFLICT DO NOTHING;

-- Viewer uses existing 'viewer' profile
INSERT INTO user_security_profiles (user_id, profile_id)
SELECT 'b0000000-0000-0000-0000-000000000004'::uuid, id
FROM security_profiles WHERE code = 'viewer'
ON CONFLICT DO NOTHING;

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
DELETE FROM user_security_profiles WHERE user_id IN (
    'b0000000-0000-0000-0000-000000000001'::uuid,
    'b0000000-0000-0000-0000-000000000002'::uuid,
    'b0000000-0000-0000-0000-000000000003'::uuid,
    'b0000000-0000-0000-0000-000000000004'::uuid,
    'b0000000-0000-0000-0000-000000000005'::uuid
);

DELETE FROM security_policy_rules WHERE id IN (
    'd0000000-0000-0000-0000-000000000001'::uuid,
    'd0000000-0000-0000-0000-000000000002'::uuid,
    'd0000000-0000-0000-0000-000000000003'::uuid,
    'd0000000-0000-0000-0000-000000000004'::uuid,
    'd0000000-0000-0000-0000-000000000005'::uuid
);

DELETE FROM security_profile_field_policies WHERE profile_id IN (
    'c0000000-0000-0000-0000-000000000002'::uuid,
    'c0000000-0000-0000-0000-000000000003'::uuid
);

DELETE FROM security_profile_dimensions WHERE profile_id IN (
    'c0000000-0000-0000-0000-000000000001'::uuid,
    'c0000000-0000-0000-0000-000000000002'::uuid,
    'c0000000-0000-0000-0000-000000000003'::uuid
);

DELETE FROM security_profiles WHERE code IN (
    'accountant_profile', 'manager_profile', 'warehouse_profile', 'limited_manager_profile'
);

DELETE FROM user_organizations WHERE user_id IN (
    'b0000000-0000-0000-0000-000000000001'::uuid,
    'b0000000-0000-0000-0000-000000000002'::uuid,
    'b0000000-0000-0000-0000-000000000003'::uuid,
    'b0000000-0000-0000-0000-000000000004'::uuid,
    'b0000000-0000-0000-0000-000000000005'::uuid
);

DELETE FROM user_roles WHERE user_id IN (
    'b0000000-0000-0000-0000-000000000001'::uuid,
    'b0000000-0000-0000-0000-000000000002'::uuid,
    'b0000000-0000-0000-0000-000000000003'::uuid,
    'b0000000-0000-0000-0000-000000000004'::uuid,
    'b0000000-0000-0000-0000-000000000005'::uuid
);

DELETE FROM users WHERE id IN (
    'b0000000-0000-0000-0000-000000000001'::uuid,
    'b0000000-0000-0000-0000-000000000002'::uuid,
    'b0000000-0000-0000-0000-000000000003'::uuid,
    'b0000000-0000-0000-0000-000000000004'::uuid,
    'b0000000-0000-0000-0000-000000000005'::uuid
);

DELETE FROM cat_organizations WHERE code = 'ORG-002';
