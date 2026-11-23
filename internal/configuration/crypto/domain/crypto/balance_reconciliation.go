package crypto

import (
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

const (
	BalanceMismatchToken  = "token_balance_mismatch"
	BalanceMismatchNative = "native_fee_balance_mismatch"
)

// BalanceObservation is an already acquired chain read, never a request to an
// RPC provider. Run+wallet+token identifies one immutable piece of evidence.
type BalanceObservation struct {
	RunID, NetworkID, WalletID, TokenID id.ID
	Kind                                string
	LedgerAmount, ChainAmount           types.RawAmount
}

func (o BalanceObservation) Validate() error {
	if id.IsNil(o.RunID) || id.IsNil(o.NetworkID) || id.IsNil(o.WalletID) || id.IsNil(o.TokenID) {
		return apperror.NewValidation("balance observation requires run, network, wallet and token")
	}
	if o.Kind != BalanceMismatchToken && o.Kind != BalanceMismatchNative {
		return apperror.NewValidation("unsupported balance reconciliation kind")
	}
	return nil
}

func (o BalanceObservation) Delta() types.RawDelta {
	return o.ChainAmount.Difference(o.LedgerAmount)
}
