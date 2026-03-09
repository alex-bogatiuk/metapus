-- +goose Up
-- Seed default permissions and roles

SELECT pg_advisory_lock(hashtext('metapus_migrations'));

-- Insert base permissions
INSERT INTO permissions (code, name, description, resource, action) VALUES
                                                                        -- Catalog permissions
                                                                        ('catalog:nomenclature:read', 'View Nomenclature', 'View products and services', 'catalog', 'read'),
                                                                        ('catalog:nomenclature:create', 'Create Nomenclature', 'Create new products and services', 'catalog', 'create'),
                                                                        ('catalog:nomenclature:update', 'Update Nomenclature', 'Modify products and services', 'catalog', 'update'),
                                                                        ('catalog:nomenclature:delete', 'Delete Nomenclature', 'Delete products and services', 'catalog', 'delete'),

                                                                        ('catalog:counterparty:read', 'View Counterparties', 'View suppliers and customers', 'catalog', 'read'),
                                                                        ('catalog:counterparty:create', 'Create Counterparties', 'Create suppliers and customers', 'catalog', 'create'),
                                                                        ('catalog:counterparty:update', 'Update Counterparties', 'Modify suppliers and customers', 'catalog', 'update'),
                                                                        ('catalog:counterparty:delete', 'Delete Counterparties', 'Delete suppliers and customers', 'catalog', 'delete'),

                                                                        ('catalog:warehouse:read', 'View Warehouses', 'View warehouses', 'catalog', 'read'),
                                                                        ('catalog:warehouse:create', 'Create Warehouses', 'Create warehouses', 'catalog', 'create'),
                                                                        ('catalog:warehouse:update', 'Update Warehouses', 'Modify warehouses', 'catalog', 'update'),
                                                                        ('catalog:warehouse:delete', 'Delete Warehouses', 'Delete warehouses', 'catalog', 'delete'),

                                                                        ('catalog:unit:read', 'View Units', 'View units of measurement', 'catalog', 'read'),
                                                                        ('catalog:unit:create', 'Create Units', 'Create units of measurement', 'catalog', 'create'),
                                                                        ('catalog:unit:update', 'Update Units', 'Modify units of measurement', 'catalog', 'update'),
                                                                        ('catalog:unit:delete', 'Delete Units', 'Delete units of measurement', 'catalog', 'delete'),

                                                                        ('catalog:currency:read', 'View Currencies', 'View currencies', 'catalog', 'read'),
                                                                        ('catalog:currency:create', 'Create Currencies', 'Create currencies', 'catalog', 'create'),
                                                                        ('catalog:currency:update', 'Update Currencies', 'Modify currencies', 'catalog', 'update'),
                                                                        ('catalog:currency:delete', 'Delete Currencies', 'Delete currencies', 'catalog', 'delete'),

                                                                        -- Document permissions
                                                                        ('document:goods_receipt:read', 'View Goods Receipts', 'View goods receipt documents', 'document', 'read'),
                                                                        ('document:goods_receipt:create', 'Create Goods Receipts', 'Create goods receipt documents', 'document', 'create'),
                                                                        ('document:goods_receipt:update', 'Update Goods Receipts', 'Modify goods receipt documents', 'document', 'update'),
                                                                        ('document:goods_receipt:delete', 'Delete Goods Receipts', 'Delete goods receipt documents', 'document', 'delete'),
                                                                        ('document:goods_receipt:post', 'Post Goods Receipts', 'Post goods receipt documents', 'document', 'post'),
                                                                        ('document:goods_receipt:unpost', 'Unpost Goods Receipts', 'Unpost goods receipt documents', 'document', 'unpost'),

                                                                        -- Register permissions
                                                                        ('register:stock:read', 'View Stock', 'View stock balances and movements', 'register', 'read'),

                                                                        -- Admin permissions
                                                                        ('admin:users:read', 'View Users', 'View user accounts', 'admin', 'read'),
                                                                        ('admin:users:create', 'Create Users', 'Create user accounts', 'admin', 'create'),
                                                                        ('admin:users:update', 'Update Users', 'Modify user accounts', 'admin', 'update'),
                                                                        ('admin:users:delete', 'Delete Users', 'Delete user accounts', 'admin', 'delete'),
                                                                        ('admin:roles:manage', 'Manage Roles', 'Assign roles to users', 'admin', 'manage')
ON CONFLICT (code) DO NOTHING;

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down