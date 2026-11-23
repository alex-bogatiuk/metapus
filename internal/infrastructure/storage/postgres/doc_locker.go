package postgres

import (
	"context"
	"fmt"
	"hash/crc32"

	"github.com/alex-bogatiuk/metapus/internal/core/id"
)

// DocLocker implements posting.DocumentLocker using PostgreSQL advisory locks.
// pg_advisory_xact_lock is transactional — released automatically on COMMIT/ROLLBACK.
type DocLocker struct{}

// NewDocLocker creates a new DocLocker.
func NewDocLocker() *DocLocker {
	return &DocLocker{}
}

// LockDocument acquires a transactional advisory lock on the given document.
// Uses two int32 hash keys: one for document type, one for document ID.
func (l *DocLocker) LockDocument(ctx context.Context, docType string, docID id.ID) error {
	txm := MustGetTxManager(ctx)
	typeHash := int32(crc32.ChecksumIEEE([]byte(docType)))
	docHash := int32(crc32.ChecksumIEEE([]byte(docID.String())))
	_, err := txm.GetQuerier(ctx).Exec(ctx,
		"SELECT pg_advisory_xact_lock($1, $2)", typeHash, docHash)
	if err != nil {
		return fmt.Errorf("pg_advisory_xact_lock(%s, %s): %w", docType, docID, err)
	}
	return nil
}

// TryLockDocument lets selectors skip a busy workflow before taking its rows.
func (l *DocLocker) TryLockDocument(ctx context.Context, docType string, docID id.ID) (bool, error) {
	var acquired bool
	err := MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, "SELECT pg_try_advisory_xact_lock($1,$2)", int32(crc32.ChecksumIEEE([]byte(docType))), int32(crc32.ChecksumIEEE([]byte(docID.String())))).Scan(&acquired)
	if err != nil {
		return false, fmt.Errorf("try document advisory lock: %w", err)
	}
	return acquired, nil
}
