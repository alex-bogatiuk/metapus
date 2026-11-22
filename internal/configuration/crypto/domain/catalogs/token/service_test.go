package token

import (
	"context"
	"testing"

	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/domain"
	"metapus/pkg/sdk/id"
)

type identityRepoStub struct {
	domain.CatalogRepository[*Token]
	identity Identity
	locked   bool
}

func (r *identityRepoStub) IdentityForUpdate(context.Context, id.ID) (Identity, bool, error) {
	return r.identity, r.locked, nil
}

func (r *identityRepoStub) FindBySymbolAndNetwork(context.Context, string, id.ID) (*Token, error) {
	return nil, apperror.NewNotFound("token", "")
}

func TestTokenIdentityBecomesImmutableAfterFinancialUse(t *testing.T) {
	networkID := id.New()
	tok := NewToken("USDT", "Tether", networkID, "USDT", 6, TokenStandardTRC20)
	tok.ContractAddress = "contract-a"
	repo := &identityRepoStub{
		identity: Identity{NetworkID: networkID, ContractAddress: tok.ContractAddress,
			DecimalPlaces: tok.DecimalPlaces, Standard: tok.Standard},
		locked: true,
	}
	svc := NewService(repo, nil)
	if err := svc.prepareForUpdate(context.Background(), tok); err != nil {
		t.Fatalf("non-identity edit rejected: %v", err)
	}
	tok.DecimalPlaces = 18
	err := svc.prepareForUpdate(context.Background(), tok)
	appErr, ok := apperror.AsAppError(err)
	if !ok || appErr.HTTPStatus != 409 {
		t.Fatalf("used token scale update must conflict, got %v", err)
	}
	repo.locked = false
	if err := svc.prepareForUpdate(context.Background(), tok); err != nil {
		t.Fatalf("unused token identity should remain editable: %v", err)
	}
}
