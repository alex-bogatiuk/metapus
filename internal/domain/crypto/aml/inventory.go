package aml

import (
	"math/big"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

// InventoryPartition splits beneficial ownership, never risk. The entire
// fungible wallet/token still inherits its conservative risk class.
type InventoryPartition struct {
	Principal  types.CryptoAmount
	ServiceFee types.CryptoAmount
}

func (p InventoryPartition) Gross() types.CryptoAmount { return p.Principal.Add(p.ServiceFee) }

// SplitInventory allocates a partial transfer from the unreserved inventory.
// Principal rounds down in integer raw units; the indivisible remainder stays
// in service fee. The persisted split is never recomputed after later receipts.
func SplitInventory(available InventoryPartition, amount types.CryptoAmount) (InventoryPartition, error) {
	if !amount.IsPositive() || amount.Cmp(available.Gross()) > 0 {
		return InventoryPartition{}, apperror.NewConflict("insufficient allocated inventory")
	}
	net := new(big.Int).Mul(amount.BigInt(), available.Principal.BigInt())
	net.Quo(net, available.Gross().BigInt())
	principal, err := types.NewCryptoAmountFromBigInt(net)
	if err != nil {
		return InventoryPartition{}, apperror.NewInternal(err)
	}
	return InventoryPartition{Principal: principal, ServiceFee: amount.Sub(principal)}, nil
}

// InternalAllocationRequest is a financial allocation reservation, not signing
// authorization. Dispatch must still acquire a current risk/batch permit.
type InternalAllocationRequest struct {
	SweepAttemptID         id.ID
	MerchantID             id.ID
	NetworkID              id.ID
	TokenID                id.ID
	SourceWalletID         id.ID
	DestinationID          id.ID
	Amount                 types.CryptoAmount
	SourceRevision         int64
	RiskRevision           int64
	ScopeVersion           int64
	Class                  Class
	BatchID                *id.ID `json:",omitzero"`
	BatchRevision          int64  `json:",omitzero"`
	BatchExecutionRevision int64  `json:",omitzero"`
}

func (r InternalAllocationRequest) Validate() error {
	if id.IsNil(r.SweepAttemptID) || id.IsNil(r.MerchantID) || id.IsNil(r.NetworkID) || id.IsNil(r.TokenID) ||
		id.IsNil(r.SourceWalletID) || id.IsNil(r.DestinationID) || r.SourceWalletID == r.DestinationID ||
		!r.Amount.IsPositive() || r.SourceRevision <= 0 || r.RiskRevision <= 0 || r.ScopeVersion <= 0 || (r.Class != Low && r.Class != Medium) {
		return apperror.NewValidation("internal allocation requires owned sources, current revisions and low/medium risk")
	}
	if (r.BatchID == nil && (r.BatchRevision != 0 || r.BatchExecutionRevision != 0)) ||
		(r.BatchID != nil && (id.IsNil(*r.BatchID) || r.BatchRevision <= 0 || r.BatchExecutionRevision <= 0)) {
		return apperror.NewValidation("batch allocation requires its current intake and execution revisions")
	}
	return nil
}

type InternalAllocation struct {
	ID        id.ID
	Status    string
	Partition InventoryPartition
}
