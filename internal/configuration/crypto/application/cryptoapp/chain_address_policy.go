package cryptoapp

import (
	"context"

	"metapus/pkg/sdk/id"
)

type NormalizedChainAddress struct {
	Address      string
	CanonicalKey string
	ChainFamily  string
}

// ChainAddressPolicy is the application boundary for chain-specific address
// validation. HTTP handlers never approximate blockchain formats with regexes.
type ChainAddressPolicy interface {
	ValidateAndNormalize(context.Context, id.ID, string) (NormalizedChainAddress, error)
}
