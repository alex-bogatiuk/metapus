//go:build integration

package crypto_repo

import (
	"metapus/internal/configuration/crypto/application/cryptoapp"
)

type unusedHistoricalFundingFees struct {
	cryptoapp.NativeFundingFeeSettlement
}
