-- Partial index on sys_idempotency to keep periodic payload archival
-- (UPDATE sys_idempotency SET archived_at = NOW(), ... WHERE completed_at < ...
-- AND archived_at IS NULL) an O(pending) index scan instead of a full scan over
-- already-archived rows.

-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
CREATE INDEX idx_sys_idempotency_archive_pending
    ON sys_idempotency (completed_at)
    WHERE archived_at IS NULL AND completed_at IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP INDEX idx_sys_idempotency_archive_pending;
-- +goose StatementEnd
