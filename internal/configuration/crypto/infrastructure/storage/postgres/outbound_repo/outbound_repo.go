// Package outbound_repo implements execution storage ports using the tenant
// transaction manager in context. Polling, chain clients and telemetry SDKs
// belong to the worker adapters that compose these repositories.
package outbound_repo

import (
	"errors"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/outboundapp"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

const DefaultBatchLimit = 20
const _outboundBatchLimit = DefaultBatchLimit

const _signingAuthorizationTTL = outboundapp.SigningAuthorizationTTL

const _withdrawalExecutionRetryAge = 2 * time.Minute

// ErrNoHotWalletLiquidity means no hot wallet currently has enough unreserved balance.
var ErrNoHotWalletLiquidity = errors.New("no hot wallet with sufficient available balance")

// ErrManagedWithdrawalDestination means a merchant withdrawal points back into
// Metapus custody. Such transfers must use an explicit internal movement flow.
var ErrManagedWithdrawalDestination = errors.New("withdrawal destination is a managed custody address")

// ErrSigningAuthorizationExists means the immutable signing attempt was already created.
var ErrSigningAuthorizationExists = outboundapp.ErrSigningAuthorizationExists

// OutboundRepo contains worker-only queries for withdrawal and sweep execution.
type OutboundRepo struct{}

// WithdrawalTask is a claimed withdrawal request ready for signing.
type WithdrawalTask = crypto.WithdrawalExecutionTask

type HotWallet = crypto.SpendWallet

// SigningAuthorizationRecord is the persisted control-plane authorization that
// Vault verifies before signing an unsigned transaction.
type SigningAuthorizationRecord = outboundapp.SigningAuthorizationRecord

// BroadcastWithdrawal is a broadcast withdrawal awaiting confirmations.
type BroadcastWithdrawal struct {
	RequestID     id.ID
	WithdrawalID  id.ID
	NetworkID     id.ID
	TxHash        string
	RequiredConfs int
}

// SweepTask is one pool wallet/token balance awaiting sweep.
type SweepTask = outboundapp.SweepTask

// TreasuryTask is a claimed platform treasury withdrawal ready for signing.
type TreasuryTask = outboundapp.TreasuryTask

// BroadcastTreasuryWithdrawal is a broadcast treasury withdrawal awaiting confirmations.
type BroadcastTreasuryWithdrawal struct {
	WithdrawalID  id.ID
	NetworkID     id.ID
	TxHash        string
	RequiredConfs int
}

type ReplacementCandidate = outboundapp.ReplacementCandidate

type SigningGeneration = outboundapp.SigningGeneration

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
