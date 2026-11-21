package crypto_repo

import (
	"context"
	"fmt"
	"strings"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/infrastructure/blockchain/evm"
	"metapus/internal/configuration/crypto/infrastructure/blockchain/tron"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
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
		normalized, err := normalizeTronDestination(trimmed)
		if err != nil {
			return cryptoapp.NormalizedChainAddress{}, err
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

// normalizeTronDestination accepts only the base58 form an operator copies
// from a TRON wallet. A hex form, including an Ethereum 0x address, would be
// silently converted to the TRON address of the same 20 bytes: funds meant
// for an exchange or contract on Ethereum would land where nobody controls them.
func normalizeTronDestination(address string) (string, error) {
	if !strings.HasPrefix(address, "T") {
		if strings.HasPrefix(address, "0x") || strings.HasPrefix(address, "0X") {
			return "", apperror.NewValidation("this looks like an Ethereum address; enter the TRON address that starts with T").WithDetail("field", "address")
		}
		return "", apperror.NewValidation("TRON address must be the base58 address that starts with T").WithDetail("field", "address")
	}
	if err := tron.ValidateTronAddress(address); err != nil {
		return "", apperror.NewValidation("invalid TRON address").WithDetail("field", "address")
	}
	return address, nil
}

var _ cryptoapp.ChainAddressPolicy = (*ChainAddressPolicy)(nil)
