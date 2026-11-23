package domain

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
	"github.com/alex-bogatiuk/metapus/internal/core/tx"
	"github.com/alex-bogatiuk/metapus/internal/core/types"
)

type outboxTestDocument struct{ ID id.ID }

func (d *outboxTestDocument) GetID() id.ID { return d.ID }

type outboxTxKey struct{}
type outboxTestTx struct{ committed, rolledBack int }

func (m *outboxTestTx) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	if ctx.Value(outboxTxKey{}) != nil {
		return fn(ctx)
	}
	inner, completion := tx.WithCommitScope(context.WithValue(ctx, outboxTxKey{}, true))
	defer completion.Discard()
	if err := fn(inner); err != nil {
		m.rolledBack++
		return err
	}
	m.committed++
	completion.Commit(ctx)
	return nil
}

type outboxTestService struct {
	DocumentService[*outboxTestDocument]
	doc       *outboxTestDocument
	err       error
	mutations int
}

func (s *outboxTestService) mutate(ctx context.Context) error {
	if ctx.Value(outboxTxKey{}) == nil {
		return errors.New("mutation outside transaction")
	}
	s.mutations++
	return s.err
}
func (s *outboxTestService) Create(ctx context.Context, _ *outboxTestDocument) error {
	return s.mutate(ctx)
}
func (s *outboxTestService) Update(ctx context.Context, _ *outboxTestDocument) error {
	return s.mutate(ctx)
}
func (s *outboxTestService) Delete(ctx context.Context, _ id.ID) error { return s.mutate(ctx) }
func (s *outboxTestService) Post(ctx context.Context, _ id.ID) error   { return s.mutate(ctx) }
func (s *outboxTestService) Unpost(ctx context.Context, _ id.ID) error { return s.mutate(ctx) }
func (s *outboxTestService) PostAndSave(ctx context.Context, _ *outboxTestDocument) error {
	return s.mutate(ctx)
}
func (s *outboxTestService) UpdateAndRepost(ctx context.Context, _ *outboxTestDocument) error {
	return s.mutate(ctx)
}
func (s *outboxTestService) SetDeletionMark(ctx context.Context, _ id.ID, _ bool) error {
	return s.mutate(ctx)
}
func (s *outboxTestService) GetByID(ctx context.Context, _ id.ID) (*outboxTestDocument, error) {
	if ctx.Value(outboxTxKey{}) == nil {
		return nil, errors.New("snapshot outside transaction")
	}
	return s.doc, nil
}

type outboxTestPublisher struct {
	err    error
	events []string
}

func (p *outboxTestPublisher) Publish(ctx context.Context, event DomainEvent) error {
	if ctx.Value(outboxTxKey{}) == nil {
		return errors.New("event outside transaction")
	}
	p.events = append(p.events, event.EventType)
	return p.err
}

func TestDocumentOutboxFailureRollsBackEveryMutation(t *testing.T) {
	operations := map[string]func(DocumentService[*outboxTestDocument], context.Context, *outboxTestDocument) error{
		"create": func(s DocumentService[*outboxTestDocument], c context.Context, d *outboxTestDocument) error {
			return s.Create(c, d)
		},
		"update": func(s DocumentService[*outboxTestDocument], c context.Context, d *outboxTestDocument) error {
			return s.Update(c, d)
		},
		"delete": func(s DocumentService[*outboxTestDocument], c context.Context, d *outboxTestDocument) error {
			return s.Delete(c, d.ID)
		},
		"post": func(s DocumentService[*outboxTestDocument], c context.Context, d *outboxTestDocument) error {
			return s.Post(c, d.ID)
		},
		"unpost": func(s DocumentService[*outboxTestDocument], c context.Context, d *outboxTestDocument) error {
			return s.Unpost(c, d.ID)
		},
		"post-save": func(s DocumentService[*outboxTestDocument], c context.Context, d *outboxTestDocument) error {
			return s.PostAndSave(c, d)
		},
		"repost": func(s DocumentService[*outboxTestDocument], c context.Context, d *outboxTestDocument) error {
			return s.UpdateAndRepost(c, d)
		},
		"mark": func(s DocumentService[*outboxTestDocument], c context.Context, d *outboxTestDocument) error {
			return s.SetDeletionMark(c, d.ID, true)
		},
	}
	for name, operation := range operations {
		t.Run(name, func(t *testing.T) {
			failure := errors.New("publisher serialization failure")
			manager := &outboxTestTx{}
			doc := &outboxTestDocument{ID: id.New()}
			next := &outboxTestService{doc: doc}
			publisher := &outboxTestPublisher{err: failure}
			service := WithOutboxEvents[*outboxTestDocument]("test", publisher, nil)(next)
			err := operation(service, tenant.WithTxManager(t.Context(), manager), doc)
			require.ErrorIs(t, err, failure)
			require.Equal(t, 1, next.mutations)
			require.Equal(t, 0, manager.committed)
			require.Equal(t, 1, manager.rolledBack)
		})
	}
}

func TestDocumentOutboxRepostCommitsBothEventsTogether(t *testing.T) {
	manager := &outboxTestTx{}
	doc := &outboxTestDocument{ID: id.New()}
	publisher := &outboxTestPublisher{}
	service := WithOutboxEvents[*outboxTestDocument]("test", publisher, nil)(&outboxTestService{doc: doc})
	require.NoError(t, service.UpdateAndRepost(tenant.WithTxManager(t.Context(), manager), doc))
	require.Equal(t, []string{"updated", "posted"}, publisher.events)
	require.Equal(t, 1, manager.committed)
}

func TestHumanAmountsStayExactThroughOutboxJSON(t *testing.T) {
	doc := struct {
		Amount types.MinorUnits `json:"amount"`
	}{Amount: types.MinorUnits(math.MaxInt64)}
	amounts, err := extractHumanAmounts(doc, &CurrencyInfo{DecimalPlaces: 2})
	require.NoError(t, err)
	require.Equal(t, "92233720368547758.07", amounts["amount"])
	encoded, err := json.Marshal(amounts)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.UnmarshalPreserveNumbers(encoded, &decoded))
	require.Equal(t, "92233720368547758.07", decoded["amount"])
}

func TestHumanAmountsRequireKnownCurrencyScale(t *testing.T) {
	doc := struct {
		Amount types.MinorUnits `json:"amount"`
	}{Amount: 100}
	amounts, err := extractHumanAmounts(doc, nil)
	require.NoError(t, err)
	require.Empty(t, amounts, "a missing unit must not silently become a two-decimal currency")
	_, err = extractHumanAmounts(doc, &CurrencyInfo{DecimalPlaces: 19})
	require.Error(t, err)
}
