package domain

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/posting"
)

func TestDocumentsAreNumberedInTheSeriesOfTheirBusinessDate(t *testing.T) {
	doc := entity.NewDocument()
	doc.Date = time.Date(2025, 12, 30, 15, 0, 0, 0, time.UTC)
	require.Equal(t, doc.Date, numberPeriod(&doc), "a December document entered in January keeps its year")

	doc.Date = time.Time{}
	require.WithinDuration(t, time.Now(), numberPeriod(&doc), time.Minute, "a document without a date is numbered by today")
	require.WithinDuration(t, time.Now(), numberPeriod(struct{}{}), time.Minute)
}

// orderRecorder notes when the posting engine records movements.
type orderRecorder struct{ order *[]string }

func (orderRecorder) Name() string { return "order" }
func (r orderRecorder) RecordFromSet(context.Context, *posting.MovementSet) error {
	*r.order = append(*r.order, "movements")
	return nil
}
func (orderRecorder) ReverseMovements(context.Context, id.ID, int) error { return nil }

type orderRepository struct {
	lifecycleRepository
	order *[]string
}

func (r *orderRepository) Create(ctx context.Context, doc *lifecycleDocument) error {
	*r.order = append(*r.order, "insert "+doc.Number)
	return r.lifecycleRepository.Create(ctx, doc)
}

// A document created and posted at once takes its number after its movements
// are recorded, right before it is inserted: the series counter stays locked
// from that write to commit, not for the whole posting, and the document is
// inserted with its number.
func TestPostAndSaveTakesTheNumberAfterTheMovements(t *testing.T) {
	for _, lines := range []bool{false, true} {
		var order []string
		numbers := &numerator.MockGenerator{GetNextNumberFunc: func(context.Context, numerator.Config, time.Time) (string, error) {
			order = append(order, "number")
			return "L-2026-00001", nil
		}}
		doc := &lifecycleDocument{Document: entity.NewDocument(), Lines: []int{1}}
		doc.Date = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
		repo := &orderRepository{lifecycleRepository: lifecycleRepository{doc: doc}, order: &order}
		engine := posting.NewEngine(nil, orderRecorder{order: &order})
		manager := &outboxTestTx{}
		var svc DocumentService[*lifecycleDocument]
		if lines {
			svc = NewBaseDocumentService(BaseDocumentServiceConfig[*lifecycleDocument, int]{Repo: repo, TxManager: manager, Numerator: numbers, PostingEngine: engine, EntityName: "lifecycle", NumeratorPrefix: "L"})
		} else {
			svc = NewBaseHeaderDocumentService(BaseHeaderDocumentServiceConfig[*lifecycleDocument]{Repo: repo, TxManager: manager, Numerator: numbers, PostingEngine: engine, EntityName: "lifecycle", NumeratorPrefix: "L"})
		}

		require.NoError(t, svc.PostAndSave(tenant.WithTxManager(context.Background(), manager), doc))
		require.Equal(t, []string{"movements", "number", "insert L-2026-00001"}, order)
		require.Equal(t, "L-2026-00001", doc.Number)
	}
}
