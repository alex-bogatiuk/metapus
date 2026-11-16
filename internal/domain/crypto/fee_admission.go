package crypto

import (
	"errors"
	"fmt"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"strings"
)

var (
	ErrNetworkPolicyDisabled  = errors.New("crypto network policy is disabled")
	ErrNetworkOperationalHold = errors.New("crypto network is on operational hold")
	ErrFeeBudgetExceeded      = errors.New("crypto network fee budget exceeded")
	ErrFeeBalanceResidual     = errors.New("crypto fee reservation violates wallet residual")
	ErrFeeReservationHeld     = errors.New("crypto fee reservation is held")
)

type FeeReservationRequest struct {
	NetworkID         id.ID
	PayerWalletID     id.ID
	NativeTokenID     id.ID
	OperationTokenID  id.ID
	ReservedPrincipal types.RawAmount
	BasisType         string
	BasisID           id.ID
	Purpose           SigningPurpose
	Attempt           int
	Strategy          FeeStrategy
	MaxFee            types.RawAmount
}

func (req FeeReservationRequest) Validate() error {
	if id.IsNil(req.NetworkID) || id.IsNil(req.PayerWalletID) || id.IsNil(req.NativeTokenID) || id.IsNil(req.OperationTokenID) ||
		id.IsNil(req.BasisID) || strings.TrimSpace(req.BasisType) == "" || req.Attempt <= 0 ||
		!req.Strategy.Valid() || !req.MaxFee.IsPositive() {
		return fmt.Errorf("invalid fee reservation request")
	}
	if req.Strategy == FeeStrategyNativeFunding && !req.ReservedPrincipal.IsPositive() {
		return fmt.Errorf("native funding reservation requires positive principal")
	}
	nativeSweep := req.BasisType == "CryptoSweepAttempt" && req.Purpose == SigningPurposeSweep && req.OperationTokenID == req.NativeTokenID
	if req.Strategy != FeeStrategyNativeFunding && !nativeSweep && !req.ReservedPrincipal.IsZero() {
		return fmt.Errorf("principal reservation requires native funding or native sweep")
	}
	return nil
}

// CheckFeeCap applies the same admission policy to first attempts and replacements.
func (p NetworkPolicy) CheckFeeCap(strategy FeeStrategy, maxFee types.RawAmount) error {
	if !p.Enabled {
		return ErrNetworkPolicyDisabled
	}
	if p.OperationalHold {
		return fmt.Errorf("%w: %s", ErrNetworkOperationalHold, p.OperationalHoldReason)
	}
	if strategy != FeeStrategyResourceRental && maxFee.Cmp(p.PerTransactionMaxFee) > 0 {
		return fmt.Errorf("%w: requested=%s cap=%s", ErrFeeBudgetExceeded, maxFee, p.PerTransactionMaxFee)
	}
	return nil
}
func (p NetworkPolicy) FeeBudgetFor(strategy FeeStrategy) types.RawAmount {
	switch strategy {
	case FeeStrategyNativeBurn:
		return p.DailyNetworkFeeBudget
	case FeeStrategyNativeFunding:
		return p.FeeFunding.DailyBudget
	}
	return types.ZeroRawAmount()
}
func (p NetworkPolicy) CheckDailyFeeCommitment(strategy FeeStrategy, committed, requested types.RawAmount) (*types.RawAmount, error) {
	budget := p.FeeBudgetFor(strategy)
	if !budget.IsPositive() {
		return nil, nil
	}
	if committed.Add(requested).Cmp(budget) > 0 {
		return nil, fmt.Errorf("%w: committed=%s requested=%s budget=%s", ErrFeeBudgetExceeded, committed, requested, budget)
	}
	remaining := budget.Sub(committed.Add(requested))
	return &remaining, nil
}
func (req FeeReservationRequest) ValidatePayer(networkID id.ID, role string) error {
	if networkID != req.NetworkID {
		return fmt.Errorf("fee payer wallet belongs to another network")
	}
	if req.Strategy == FeeStrategyNativeFunding && role != "network_fee" {
		return fmt.Errorf("native funding must use the dedicated network fee wallet")
	}
	return nil
}
func EffectiveFeeBalance(ledger, operating types.RawAmount, amlEnabled bool) types.RawAmount {
	if amlEnabled && operating.Cmp(ledger) < 0 {
		return operating
	}
	return ledger
}
func (p NetworkPolicy) CheckFeeWalletBalance(payerRole string, balance, activeReserved, requested types.RawAmount) error {
	minimum := p.FeeFunding.MinSourceWalletResidual
	if payerRole == "network_fee" {
		minimum = p.FeeFunding.MinFeeWalletBalance
	}
	if balance.Cmp(activeReserved.Add(requested).Add(minimum)) < 0 {
		return fmt.Errorf("%w: balance=%s reserved=%s requested=%s minimum=%s", ErrFeeBalanceResidual, balance, activeReserved, requested, minimum)
	}
	return nil
}
func (r FeeReservation) CheckReusable() error {
	if r.Status == FeeReservationHeld {
		return fmt.Errorf("%w: %s", ErrFeeReservationHeld, r.HoldReason)
	}
	if r.Status == FeeReservationReleased {
		return fmt.Errorf("released fee reservation cannot be reused by the same attempt")
	}
	return nil
}
