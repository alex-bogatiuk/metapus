package postgres

import (
	"context"
	"fmt"
	"time"

	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
)

// DocumentStatusChange is one row of a document's status history
// (sys_document_status_history): the status the document was written in,
// the one it had before (empty for a new document), when and by whom.
type DocumentStatusChange struct {
	Status         string
	PreviousStatus string
	ChangedAt      time.Time
	// ChangedBy is the user who wrote the document; nil for the system.
	ChangedBy *id.ID
}

// RecordDocumentStatus adds a row to the status history of the document of
// type documentType (its entity key): it is now in status, after previous.
// The user of ctx is its author.
func RecordDocumentStatus(ctx context.Context, documentType string, documentID any, status, previous string) error {
	var author *id.ID
	if user, err := id.Parse(appctx.GetUserID(ctx)); err == nil && !id.IsNil(user) {
		author = &user
	}
	var before *string
	if previous != "" {
		before = &previous
	}
	_, err := MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		INSERT INTO sys_document_status_history (document_type, document_id, status, previous_status, changed_by)
		VALUES ($1, $2, $3, $4, $5)
	`, documentType, documentID, status, before, author)
	if err != nil {
		return fmt.Errorf("record document status: %w", err)
	}
	return nil
}

// DocumentStatusHistory lists the status history of a document, oldest
// first.
func DocumentStatusHistory(ctx context.Context, documentType string, documentID id.ID) ([]DocumentStatusChange, error) {
	rows, err := MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, `
		SELECT status, COALESCE(previous_status, ''), changed_at, changed_by
		FROM sys_document_status_history
		WHERE document_type = $1 AND document_id = $2
		ORDER BY changed_at, id
	`, documentType, documentID)
	if err != nil {
		return nil, fmt.Errorf("read document status history: %w", err)
	}
	defer rows.Close()
	history := []DocumentStatusChange{}
	for rows.Next() {
		var change DocumentStatusChange
		if err := rows.Scan(&change.Status, &change.PreviousStatus, &change.ChangedAt, &change.ChangedBy); err != nil {
			return nil, fmt.Errorf("scan document status history: %w", err)
		}
		history = append(history, change)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read document status history: %w", err)
	}
	return history, nil
}
