//go:build integration

package crypto_repo

import (
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/cryptoapp"
)

type unusedHistoricalFundingFees struct {
	cryptoapp.NativeFundingFeeSettlement
}
