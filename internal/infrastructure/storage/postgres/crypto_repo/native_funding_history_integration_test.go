//go:build integration

package crypto_repo

import (
	"metapus/internal/application/cryptoapp"
)

type unusedHistoricalFundingFees struct {
	cryptoapp.NativeFundingFeeSettlement
}
