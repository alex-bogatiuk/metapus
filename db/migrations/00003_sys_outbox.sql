-- +goose Up
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

CREATE TYPE outbox_status AS ENUM ('pending', 'published', 'failed');

CREATE TABLE sys_outbox (
                            id            UUID        NOT NULL,
                            aggregate_type VARCHAR(50) NOT NULL,
                            aggregate_id  UUID        NOT NULL,
                            event_type    VARCHAR(50) NOT NULL,
                            payload       JSONB       NOT NULL,
                            status        outbox_status NOT NULL DEFAULT 'pending',
                            retry_count   INT         NOT NULL DEFAULT 0,
                            last_error    TEXT,
                            next_retry_at TIMESTAMPTZ,
                            created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
                            published_at  TIMESTAMPTZ,

                            PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

-- Упрощённый, но эффективный индекс — только по status = 'pending'
-- Это то, что использует воркер в 99% случаев
CREATE INDEX idx_outbox_pending
    ON sys_outbox (created_at)
    WHERE status = 'pending';

-- Дополнительный индекс для next_retry_at (если нужно)
CREATE INDEX idx_outbox_retry
    ON sys_outbox (next_retry_at)
    WHERE status = 'pending' AND next_retry_at IS NOT NULL;

CREATE TABLE sys_outbox_default PARTITION OF sys_outbox DEFAULT;

CREATE TABLE sys_outbox_dlq (
                                id            UUID        PRIMARY KEY,
                                aggregate_type VARCHAR(50) NOT NULL,
                                aggregate_id  UUID        NOT NULL,
                                event_type    VARCHAR(50) NOT NULL,
                                payload       JSONB       NOT NULL,
                                retry_count   INT         NOT NULL,
                                last_error    TEXT,
                                created_at    TIMESTAMPTZ NOT NULL,
                                failed_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
                                failure_reason TEXT
);



SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
DROP TABLE IF EXISTS sys_outbox_dlq;
DROP TABLE IF EXISTS sys_outbox;
DROP TYPE IF EXISTS outbox_status;