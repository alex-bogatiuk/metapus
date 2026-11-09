package backend

import (
	"context"
	"sync"

	"github.com/hashicorp/vault/sdk/framework"
	"github.com/hashicorp/vault/sdk/logical"
)

func Factory(ctx context.Context, conf *logical.BackendConfig) (logical.Backend, error) {
	b := backend()
	if err := b.Setup(ctx, conf); err != nil {
		return nil, err
	}
	return b, nil
}

type evmBackend struct {
	*framework.Backend
	signMu sync.Mutex
}

func backend() *evmBackend {
	var b evmBackend
	b.Backend = &framework.Backend{
		BackendType:  logical.TypeLogical,
		PathsSpecial: &logical.Paths{SealWrapStorage: []string{"tenants/"}},
		Paths:        framework.PathAppend(pathConfig(&b), pathSweepDestinations(&b), pathMerchantCustody(&b), pathWallets(&b), pathSign(&b)),
	}
	return &b
}
