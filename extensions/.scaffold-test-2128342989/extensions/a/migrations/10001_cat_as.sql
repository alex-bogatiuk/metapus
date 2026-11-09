-- +goose Up
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
CREATE TABLE cat_as (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
 code TEXT NOT NULL DEFAULT '',
 name TEXT NOT NULL,
 parent_id UUID REFERENCES cat_as(id),
 is_folder BOOLEAN NOT NULL DEFAULT FALSE,
 deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
 version INT NOT NULL DEFAULT 1,
 attributes JSONB NOT NULL DEFAULT '{}',
 _deleted_at TIMESTAMPTZ,
 _txid BIGINT NOT NULL DEFAULT txid_current(),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_cat_as_code ON cat_as (code)
 WHERE _deleted_at IS NULL AND code != '';
CREATE INDEX idx_cat_as_keyset ON cat_as (name,id) WHERE _deleted_at IS NULL;
CREATE INDEX idx_cat_as_txid ON cat_as (_txid) WHERE _deleted_at IS NULL;
CREATE TRIGGER trg_cat_as_txid BEFORE UPDATE ON cat_as FOR EACH ROW EXECUTE FUNCTION update_txid_column();
CREATE TRIGGER trg_cat_as_soft_delete BEFORE UPDATE OF deletion_mark ON cat_as FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();
CREATE TRIGGER trg_cat_as_updated_at BEFORE UPDATE ON cat_as FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

INSERT INTO auth_permissions (code,name,description,resource,action)
 SELECT 'a.' || action,'A ' || action,'A ' || action,'a',action
 FROM unnest(ARRAY['read','create','update','delete']) AS action;
INSERT INTO auth_role_permissions (role_id,permission_id)
 SELECT r.id,p.id FROM auth_roles r CROSS JOIN auth_permissions p WHERE r.code='admin' AND p.resource='a';

-- +goose Down
DELETE FROM auth_role_permissions WHERE permission_id IN (SELECT id FROM auth_permissions WHERE resource='a');
DELETE FROM auth_permissions WHERE resource='a';
DROP TABLE cat_as;
