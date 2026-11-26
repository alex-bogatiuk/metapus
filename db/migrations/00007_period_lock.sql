-- The period lock (дата запрета изменения). Documents dated on or before
-- the lock date are not created, changed, posted, unposted or marked for
-- deletion except by holders of closed_period:edit (administrators have
-- it); the document repository checks it. The row without an organization
-- is the tenant's date, the settings section periodLock; a row per
-- organization is kept for dates by organization.

-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
CREATE TABLE sys_period_locks (
 id              UUID        PRIMARY KEY DEFAULT gen_random_uuid_v7(),
 organization_id UUID        REFERENCES cat_organizations(id),
 locked_until    DATE        NOT NULL,
 updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_by      UUID
);
COMMENT ON TABLE sys_period_locks IS 'Даты запрета изменения: документы с датой не позже locked_until (в часовом поясе клиента) не меняются; строка без организации — общая дата';
CREATE UNIQUE INDEX idx_sys_period_locks_organization ON sys_period_locks ((COALESCE(organization_id, '00000000-0000-0000-0000-000000000000'::uuid)));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TABLE sys_period_locks;
-- +goose StatementEnd
