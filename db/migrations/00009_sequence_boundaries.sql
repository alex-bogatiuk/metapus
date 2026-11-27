-- Boundaries of the sequences of documents (последовательности документов,
-- entity.SequenceRule): per sequence, the moment up to which every document
-- of it was taken into account by a posted closing, such as the month
-- closing that costs the stock. The document repository moves a boundary
-- back when a document of the sequence dated before it is posted, reposted
-- or unposted, and forward to a period's end when the document closing the
-- period is posted. A sequence without a row was never closed.

-- +goose Up
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
CREATE TABLE sys_sequence_boundaries (
 sequence   TEXT        PRIMARY KEY,
 boundary   TIMESTAMPTZ NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE sys_sequence_boundaries IS 'Границы последовательностей документов: до boundary все документы последовательности учтены проведённым закрытием';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'));
DROP TABLE sys_sequence_boundaries;
-- +goose StatementEnd
