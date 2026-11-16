package aml

import (
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

// TestnetOpening is a synthetic partition of already booked physical money.
// It is not a payment, provider report, or permission to move funds.
type TestnetOpening struct {
	WalletID   id.ID           `json:"walletId"`
	TokenID    id.ID           `json:"tokenId"`
	CheckID    id.ID           `json:"reconciliationCheckId"`
	Principal  types.RawAmount `json:"principal"`
	ServiceFee types.RawAmount `json:"serviceFee"`
	Operating  types.RawAmount `json:"operating"`
	LedgerHash string          `json:"ledgerHash"`
}

type TestnetBootstrap struct {
	Schema      string           `json:"schema"`
	ID          id.ID            `json:"id"`
	MerchantID  id.ID            `json:"merchantId"`
	NetworkID   id.ID            `json:"networkId"`
	BlockNumber int64            `json:"blockNumber"`
	BlockHash   string           `json:"blockHash"`
	ExpiresAt   time.Time        `json:"expiresAt"`
	Actor       string           `json:"actor"`
	Openings    []TestnetOpening `json:"openings"`
}

func (b TestnetBootstrap) Validate(now time.Time) error {
	if b.Schema != "aml-testnet-bootstrap.v1" || id.IsNil(b.ID) || id.IsNil(b.MerchantID) || id.IsNil(b.NetworkID) || b.BlockNumber < 0 || b.BlockHash == "" || b.Actor == "" || !b.ExpiresAt.After(now) || b.ExpiresAt.After(now.Add(365*24*time.Hour)) || len(b.Openings) == 0 || len(b.Openings) > 5000 {
		return apperror.NewValidation("complete bounded testnet bootstrap manifest required")
	}
	seen := make(map[[2]id.ID]bool, len(b.Openings))
	for _, o := range b.Openings {
		key := [2]id.ID{o.WalletID, o.TokenID}
		if id.IsNil(o.WalletID) || id.IsNil(o.TokenID) || id.IsNil(o.CheckID) || len(o.LedgerHash) != 64 || seen[key] {
			return apperror.NewValidation("unique opening wallet/asset, reconciliation and journal digest required")
		}
		seen[key] = true
	}
	return nil
}
