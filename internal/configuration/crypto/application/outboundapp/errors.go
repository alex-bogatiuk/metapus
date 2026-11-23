package outboundapp

import (
	"errors"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
)

type heldExecutionError struct{ err error }

func (e *heldExecutionError) Error() string { return e.err.Error() }
func (e *heldExecutionError) Unwrap() error { return e.err }
func HoldExecution(err error) error         { return &heldExecutionError{err} }
func IsHeldExecutionError(err error) bool   { var held *heldExecutionError; return errors.As(err, &held) }

var (
	ErrNetworkPolicyDisabled        = crypto.ErrNetworkPolicyDisabled
	ErrFeeReservationHeld           = crypto.ErrFeeReservationHeld
	ErrResourceRentalRequired       = errors.New("network policy requires resource rental")
	ErrResourceCapacityInsufficient = errors.New("TRON resource rental capacity is insufficient")
)

func statusFailedReason(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if len(msg) > 500 {
		return msg[:500]
	}
	return msg
}

var ErrNativeFundingPending = errors.New("native funding is awaiting solidified receipt")
var ErrFeeBalanceResidual = crypto.ErrFeeBalanceResidual

type preEffectWithdrawalError struct{ cause error }

func (e *preEffectWithdrawalError) Error() string { return e.cause.Error() }
func (e *preEffectWithdrawalError) Unwrap() error { return e.cause }
func IsPreEffectWithdrawalError(err error) bool {
	var target *preEffectWithdrawalError
	return errors.As(err, &target)
}

type withdrawalRecoveryError struct {
	reason string
	error
}

func (e *withdrawalRecoveryError) Unwrap() error { return e.error }
func holdWithdrawalRecovery(reason string, err error) error {
	return &withdrawalRecoveryError{reason, HoldExecution(err)}
}
func WithdrawalRecoveryReason(err error) string {
	var recovery *withdrawalRecoveryError
	if errors.As(err, &recovery) {
		return recovery.reason
	}
	return "execution_uncertain"
}

func PreEffectWithdrawalError(err error) error { return &preEffectWithdrawalError{cause: err} }

var ErrNativeFundingHeld = errors.New("native funding is held")

var ErrMerchantCustody = errors.New("merchant custody boundary violation")

var ErrSigningAuthorizationExists = errors.New("signing authorization already exists")

type chainDependencyError struct {
	err error
}

func (e *chainDependencyError) Error() string {
	return e.err.Error()
}

func (e *chainDependencyError) Unwrap() error {
	return e.err
}

func NewChainDependencyError(err error) error {
	if err == nil {
		return nil
	}
	return &chainDependencyError{err: err}
}

func IsChainDependencyError(err error) bool {
	var dependencyErr *chainDependencyError
	return errors.As(err, &dependencyErr)
}

var ErrActivationConcurrencyLimit = errors.New("account activation concurrency limit reached")

var ErrResourceReclaimPending = errors.New("TRON resource rental reclaim is pending")
var ErrResourceRentalExists = errors.New("TRON resource rental already exists")
