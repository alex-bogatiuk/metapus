package handlers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/domain"
)

type batchTestDocument struct{}

// panickingDocuments posts every document but one, on which it panics.
type panickingDocuments struct {
	domain.DocumentService[batchTestDocument]
	panicOn id.ID
}

func (s panickingDocuments) Post(_ context.Context, docID id.ID) error {
	if docID == s.panicOn {
		panic("posting exploded")
	}
	return nil
}

func TestBatchActionPanicBecomesTheDocumentsError(t *testing.T) {
	ids := []id.ID{id.New(), id.New(), id.New()}
	h := &BaseDocumentHandler[batchTestDocument, struct{}, struct{}]{
		service: panickingDocuments{panicOn: ids[1]},
	}

	errs := make(map[id.ID]error)
	for r := range h.executeBatchConcurrent(context.Background(), ids, "post", 2) {
		errs[r.id] = r.err
	}

	require.Len(t, errs, len(ids))
	require.NoError(t, errs[ids[0]])
	require.NoError(t, errs[ids[2]])
	var appErr *apperror.AppError
	require.ErrorAs(t, errs[ids[1]], &appErr)
	// The client sees a generic internal error, not the panic's text.
	require.NotContains(t, apperror.Public(errs[ids[1]]).Message, "exploded")
}
