-- +goose Up
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
CREATE TABLE doc_doc_probes (
 id UUID PRIMARY KEY,
 number TEXT NOT NULL DEFAULT '',
 date TIMESTAMPTZ NOT NULL,
 posted BOOLEAN NOT NULL DEFAULT FALSE,
 posted_version INT NOT NULL DEFAULT 0,
 basis_type TEXT NOT NULL DEFAULT '',
 basis_id UUID,
 description TEXT NOT NULL DEFAULT '',
 created_by UUID,
 updated_by UUID,
 deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
 version INT NOT NULL DEFAULT 1,
 attributes JSONB NOT NULL DEFAULT '{}',
 _deleted_at TIMESTAMPTZ,
 _txid BIGINT NOT NULL DEFAULT txid_current(),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_doc_doc_probes_number ON doc_doc_probes (number)
 WHERE _deleted_at IS NULL AND number != '';
CREATE INDEX idx_doc_doc_probes_keyset ON doc_doc_probes (date,id) WHERE _deleted_at IS NULL;
CREATE INDEX idx_doc_doc_probes_txid ON doc_doc_probes (_txid) WHERE _deleted_at IS NULL;
CREATE TRIGGER trg_doc_doc_probes_txid BEFORE UPDATE ON doc_doc_probes FOR EACH ROW EXECUTE FUNCTION update_txid_column();
CREATE TRIGGER trg_doc_doc_probes_soft_delete BEFORE UPDATE OF deletion_mark ON doc_doc_probes FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();
CREATE TRIGGER trg_doc_doc_probes_updated_at BEFORE UPDATE ON doc_doc_probes FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

INSERT INTO auth_permissions (code,name,description,resource,action)
 SELECT 'doc_probe.' || action,'DocProbe ' || action,'DocProbe ' || action,'doc_probe',action
 FROM unnest(ARRAY['read','create','update','delete','post','unpost']) AS action;
INSERT INTO auth_role_permissions (role_id,permission_id)
 SELECT r.id,p.id FROM auth_roles r CROSS JOIN auth_permissions p WHERE r.code='admin' AND p.resource='doc_probe';

-- +goose Down
DELETE FROM auth_role_permissions WHERE permission_id IN (SELECT id FROM auth_permissions WHERE resource='doc_probe');
DELETE FROM auth_permissions WHERE resource='doc_probe';
DROP TABLE doc_doc_probes;
