-- The status history of documents with a status model (entity.StatusModel):
-- one row each time a document is written in a status other than the one
-- it had, the first write of a new document included. The document
-- repository writes it in the transaction of the write; the document's
-- form reads it from GET /document/<route>/:id/status-history. A row names
-- its document by type (the entity key) and ID, so it has no foreign key;
-- the history of a deleted document is kept.

-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
CREATE TABLE sys_document_status_history (
 id              UUID        PRIMARY KEY DEFAULT gen_random_uuid_v7(),
 document_type   TEXT        NOT NULL,
 document_id     UUID        NOT NULL,
 status          TEXT        NOT NULL,
 previous_status TEXT,
 changed_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
 changed_by      UUID
);
COMMENT ON TABLE sys_document_status_history IS 'История статусов документов: строка на каждую запись документа в новом статусе; previous_status пуст у нового документа';
CREATE INDEX idx_sys_document_status_history_document ON sys_document_status_history (document_type, document_id, changed_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TABLE sys_document_status_history;
-- +goose StatementEnd
