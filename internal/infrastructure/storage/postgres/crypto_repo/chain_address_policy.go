package crypto_repo

import (
	"context"
	"fmt"
	"strings"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/blockchain/evm"
	"metapus/internal/infrastructure/blockchain/tron"
	"metapus/internal/infrastructure/storage/postgres"
)

type ChainAddressPolicy struct{}

func NewChainAddressPolicy() *ChainAddressPolicy { return &ChainAddressPolicy{} }

func (p *ChainAddressPolicy) ValidateAndNormalize(ctx context.Context, networkID id.ID, address string) (cryptoapp.NormalizedChainAddress, error) {
	var family string
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `
		SELECT chain_family FROM cat_blockchain_networks
		WHERE id=$1 AND deletion_mark=FALSE
	`, networkID).Scan(&family)
	if err != nil {
		return cryptoapp.NormalizedChainAddress{}, apperror.NewValidation("unknown blockchain network")
	}
	trimmed := strings.TrimSpace(address)
	switch family {
	case "tron":
		normalized := tron.NormalizeTronAddress(trimmed)
		if err := tron.ValidateTronAddress(normalized); err != nil {
			return cryptoapp.NormalizedChainAddress{}, apperror.NewValidation("invalid TRON address").WithDetail("field", "address")
		}
		return cryptoapp.NormalizedChainAddress{Address: normalized, CanonicalKey: normalized, ChainFamily: family}, nil
	case "evm":
		normalized, err := evm.NormalizeAddress(trimmed)
		if err != nil {
			return cryptoapp.NormalizedChainAddress{}, apperror.NewValidation("invalid EVM address").WithDetail("field", "address")
		}
		return cryptoapp.NormalizedChainAddress{Address: normalized, CanonicalKey: strings.ToLower(normalized), ChainFamily: family}, nil
	default:
		return cryptoapp.NormalizedChainAddress{}, apperror.NewValidation(fmt.Sprintf("unsupported chain family %q", family))
	}
}

var _ cryptoapp.ChainAddressPolicy = (*ChainAddressPolicy)(nil)
