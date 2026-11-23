package domain

import (
	"context"

	"github.com/alex-bogatiuk/metapus/internal/core/id"
)

// Currency resolution is opt-in. Token-based and non-monetary documents do not
// need dummy currency methods to participate in the document lifecycle.
func resolveDocumentCurrency[T any](ctx context.Context, doc T, resolver CurrencyResolveStrategy) error {
	monetary, ok := any(doc).(CurrencyAwareDoc)
	if !ok || resolver == nil {
		return nil
	}
	var organizationID id.ID
	if owned, ok := any(doc).(OrganizationOwned); ok {
		organizationID = owned.GetOrganizationID()
	}
	currencyID, err := resolver.ResolveForDocument(ctx, monetary.GetCurrencyID(), monetary.GetContractID(), organizationID)
	if err != nil {
		return err
	}
	monetary.SetCurrencyID(currencyID)
	return nil
}
