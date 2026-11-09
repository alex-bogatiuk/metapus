package backend

import (
	"context"
	"sync"

	"github.com/hashicorp/vault/sdk/framework"
	"github.com/hashicorp/vault/sdk/logical"
)

// Factory returns a new backend as logical.Backend.
func Factory(ctx context.Context, conf *logical.BackendConfig) (logical.Backend, error) {
	b := backend()
	if err := b.Setup(ctx, conf); err != nil {
		return nil, err
	}
	return b, nil
}

type tronBackend struct {
	*framework.Backend
	signMu sync.Mutex
}

func backend() *tronBackend {
	var b tronBackend
	b.Backend = &framework.Backend{
		BackendType: logical.TypeLogical,
		PathsSpecial: &logical.Paths{
			SealWrapStorage: []string{
				"tenants/",
			},
		},
		Paths: framework.PathAppend(
			pathConfig(&b),
			pathSweepDestinations(&b), pathMerchantCustody(&b),
			pathWallets(&b),
			pathSign(&b),
		),
	}
	return &b
}
