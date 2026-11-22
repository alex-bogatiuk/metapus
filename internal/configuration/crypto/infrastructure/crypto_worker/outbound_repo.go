package crypto_worker

import (
	"metapus/internal/configuration/crypto/domain/registers/crypto_fee"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/outbound_repo"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/types"
)

const _outboundBatchLimit = outbound_repo.DefaultBatchLimit

var ErrNoHotWalletLiquidity = outbound_repo.ErrNoHotWalletLiquidity

var ErrManagedWithdrawalDestination = outbound_repo.ErrManagedWithdrawalDestination

var ErrSigningAuthorizationExists = outbound_repo.ErrSigningAuthorizationExists

type OutboundRepo = outbound_repo.OutboundRepo

// NewOutboundRepo preserves the constructor used by worker composition.
func NewOutboundRepo() *OutboundRepo {
	return outbound_repo.NewOutboundRepo()
}

type WithdrawalTask = outbound_repo.WithdrawalTask

type HotWallet = outbound_repo.HotWallet

type SigningAuthorizationRecord = outbound_repo.SigningAuthorizationRecord

type BroadcastWithdrawal = outbound_repo.BroadcastWithdrawal

type SweepTask = outbound_repo.SweepTask

type TreasuryTask = outbound_repo.TreasuryTask

// FeeReservationLine is a platform revenue reservation consumed by a treasury withdrawal.
type FeeReservationLine struct {
	MerchantID id.ID
	TokenID    id.ID
	FeeType    crypto_fee.FeeType
	Amount     types.RawAmount
}

type BroadcastTreasuryWithdrawal = outbound_repo.BroadcastTreasuryWithdrawal

type ReplacementCandidate = outbound_repo.ReplacementCandidate

type SigningGeneration = outbound_repo.SigningGeneration

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
