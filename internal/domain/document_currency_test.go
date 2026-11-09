package domain

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
)

type monetaryLifecycle struct {
	lifecycleDocument
	currency     id.ID
	contract     *id.ID
	organization id.ID
}

func (d *monetaryLifecycle) GetCurrencyID() id.ID                 { return d.currency }
func (d *monetaryLifecycle) SetCurrencyID(value id.ID)            { d.currency = value }
func (*monetaryLifecycle) ValidateCurrency(context.Context) error { return nil }
func (d *monetaryLifecycle) GetContractID() *id.ID                { return d.contract }
func (d *monetaryLifecycle) GetOrganizationID() id.ID             { return d.organization }

type currencyResolutionProbe struct {
	calls                          int
	explicit, organization, result id.ID
	contract                       *id.ID
	err                            error
}

func (r *currencyResolutionProbe) ResolveForDocument(_ context.Context, explicit id.ID, contract *id.ID, organization id.ID) (id.ID, error) {
	r.calls++
	r.explicit = explicit
	r.contract = contract
	r.organization = organization
	return r.result, r.err
}

func TestDocumentCurrencyResolutionIsOptionalAndPreservesMonetaryInputs(t *testing.T) {
	resolver := &currencyResolutionProbe{result: id.New()}
	doc := &lifecycleDocument{Document: entity.NewDocument()}
	for _, resolve := range []func(context.Context, *lifecycleDocument) error{
		NewBaseHeaderDocumentService(BaseHeaderDocumentServiceConfig[*lifecycleDocument]{CurrencyResolver: resolver}).ResolveCurrency,
		NewBaseDocumentService(BaseDocumentServiceConfig[*lifecycleDocument, int]{CurrencyResolver: resolver}).ResolveCurrency,
	} {
		require.NoError(t, resolve(context.Background(), doc))
	}
	require.Zero(t, resolver.calls)
	contract := id.New()
	monetary := &monetaryLifecycle{currency: id.New(), contract: &contract, organization: id.New()}
	original := monetary.currency
	require.NoError(t, resolveDocumentCurrency(context.Background(), monetary, resolver))
	require.Equal(t, original, resolver.explicit)
	require.Equal(t, &contract, resolver.contract)
	require.Equal(t, monetary.organization, resolver.organization)
	require.Equal(t, resolver.result, monetary.currency)
	resolver.err = errors.New("resolution failed")
	resolver.result = id.New()
	require.ErrorIs(t, resolveDocumentCurrency(context.Background(), monetary, resolver), resolver.err)
	require.NotEqual(t, resolver.result, monetary.currency)
}

type transactionalNumerator struct {
	numerator.Generator
	calls int
}

func (n *transactionalNumerator) GetNextNumber(ctx context.Context, _ numerator.Config, _ *numerator.Options, _ time.Time) (string, error) {
	if ctx.Value(outboxTxKey{}) == nil {
		return "", errors.New("number allocated outside transaction")
	}
	n.calls++
	return "L-NUM", nil
}
func TestDocumentNumberAllocationRunsInsideMutationTransaction(t *testing.T) {
	for _, lines := range []bool{false, true} {
		manager := &outboxTestTx{}
		num := &transactionalNumerator{}
		doc := &lifecycleDocument{Document: entity.NewDocument()}
		repo := &lifecycleRepository{doc: doc}
		var svc DocumentService[*lifecycleDocument]
		if lines {
			svc = NewBaseDocumentService(BaseDocumentServiceConfig[*lifecycleDocument, int]{Repo: repo, TxManager: manager, Numerator: num})
		} else {
			svc = NewBaseHeaderDocumentService(BaseHeaderDocumentServiceConfig[*lifecycleDocument]{Repo: repo, TxManager: manager, Numerator: num})
		}
		require.NoError(t, svc.Create(context.Background(), doc))
		require.Equal(t, 1, num.calls)
		require.Equal(t, "L-NUM", doc.Number)
	}
}
