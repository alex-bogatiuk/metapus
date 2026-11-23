package outboundapp

import (
	"context"
	"fmt"
	"strings"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/logger"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tx"
)

const _maxReplacementGenerations = 3

type ReplacementConfig struct {
	Drivers        *crypto.ChainDriverRegistry
	Signers        *crypto.FamilySignerRegistry
	Repository     ReplacementRepository
	Authorizations Authorizations
	Fees           ReplacementFees
	Transactions   tx.Manager
	Observer       ReplacementObserver
	Logger         *logger.Logger
}

type ReplacementService struct {
	drivers        *crypto.ChainDriverRegistry
	familySigners  *crypto.FamilySignerRegistry
	outboundRepo   ReplacementRepository
	authorizations Authorizations
	feeBudget      ReplacementFees
	transactions   tx.Manager
	observer       ReplacementObserver
	log            *logger.Logger
}

func NewReplacementService(c ReplacementConfig) *ReplacementService {
	if c.Logger == nil {
		c.Logger = logger.Default()
	}
	return &ReplacementService{
		drivers: c.Drivers, familySigners: c.Signers, outboundRepo: c.Repository,
		authorizations: c.Authorizations, feeBudget: c.Fees, transactions: c.Transactions,
		observer: c.Observer, log: c.Logger,
	}
}

func (p *ReplacementService) recordReplacement(ctx context.Context, network id.ID, generation int, event string) {
	if p.observer != nil {
		p.observer.Replacement(ctx, network, generation, event)
	}
}

func (p *ReplacementService) recordBroadcast(ctx context.Context, network crypto.NetworkRef, purpose crypto.SigningPurpose, outcome crypto.BroadcastOutcome) {
	if p.observer != nil {
		p.observer.Broadcast(ctx, network, purpose, outcome)
	}
}

func (p *ReplacementService) Execute(ctx context.Context, candidate ReplacementCandidate) error {
	if p == nil || p.outboundRepo == nil || p.drivers == nil || p.familySigners == nil || p.authorizations == nil || p.feeBudget == nil || p.transactions == nil {
		return fmt.Errorf("replacement service is not configured")
	}

	current, found, err := p.outboundRepo.GetSigningAttempt(ctx, candidate.BasisType, candidate.BasisID, candidate.Purpose, candidate.Attempt, candidate.Generation)
	if err != nil {
		return err
	}
	if !found || current.SignedTx == nil || current.TxID == "" || current.Status != "broadcast" {
		return nil
	}
	driver, err := p.drivers.Resolve(current.Authorization.NetworkID)
	if err != nil {
		return err
	}
	if _, ok := driver.(crypto.ReplacementTransactionBuilder); !ok {
		return nil
	}
	safety, ok := driver.(crypto.ReplacementSafetyReader)
	if !ok {
		return nil
	}
	generations, err := p.outboundRepo.ListSigningGenerations(ctx, candidate)
	if err != nil {
		return err
	}
	hashes := make([]string, 0, len(generations))
	for _, generation := range generations {
		if strings.TrimSpace(generation.TxHash) != "" {
			hashes = append(hashes, generation.TxHash)
		}
	}
	assessment, err := safety.AssessReplacement(ctx, current.Authorization.SourceAddress, current.Authorization.NetworkSequence, hashes)
	if err != nil {
		return err
	}
	p.recordReplacement(ctx, current.Authorization.NetworkID, current.Authorization.Generation, string(assessment.Disposition))
	reservation, found, err := p.feeBudget.Get(ctx, candidate.BasisType, candidate.BasisID, candidate.Purpose, candidate.Attempt)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("replacement fee reservation is missing")
	}

	switch assessment.Disposition {
	case crypto.ReplacementMined:
		return p.acceptMinedReplacementGeneration(ctx, candidate, current, generations, reservation, assessment.MinedTxHash)
	case crypto.ReplacementNonceConsumed:
		return p.holdReplacementNonceDivergence(ctx, driver, current, reservation, assessment.Reason)
	case crypto.ReplacementUncertain:
		if strings.Contains(strings.ToLower(assessment.Reason), "different mined") {
			return p.holdReplacementNonceDivergence(ctx, driver, current, reservation, assessment.Reason)
		}
		if err := p.authorizations.CheckCustodyDispatch(ctx, current.Authorization, p.familySigners); err != nil {
			return HoldExecution(err)
		}
		// No new authorization is permitted while providers disagree or fail to
		// provide a quorum. Re-submit the exact persisted bytes only.
		outcome, broadcastErr := driver.Broadcaster().Broadcast(ctx, *current.SignedTx)
		p.recordBroadcast(ctx, driver.Network(), current.Authorization.Purpose, outcome)
		_ = p.outboundRepo.MarkSigningAuthorizationBroadcast(ctx, current.Authorization.AuthorizationID, current.TxID)
		if broadcastErr != nil || outcome.Status == crypto.BroadcastAmbiguous {
			return nil
		}
		if outcome.Status == crypto.BroadcastRejected {
			reason := "byte-identical rebroadcast was rejected: " + outcome.Reason
			_ = p.feeBudget.Hold(ctx, reservation.ID, reason)
			_ = p.outboundRepo.MarkSigningAuthorizationHeld(ctx, current.Authorization.AuthorizationID, current.TxID)
			return fmt.Errorf("%s", reason)
		}
		if outcome.TxHash != "" && !strings.EqualFold(outcome.TxHash, current.TxID) {
			return p.holdReplacementNonceDivergence(ctx, driver, current, reservation, "byte-identical rebroadcast returned a different transaction hash")
		}
		return nil
	case crypto.ReplacementStuck:
		return p.createReplacementGeneration(ctx, candidate, current, generations, reservation, driver)
	default:
		return fmt.Errorf("unknown replacement disposition %q", assessment.Disposition)
	}
}

func (p *ReplacementService) createReplacementGeneration(
	ctx context.Context,
	candidate ReplacementCandidate,
	current SigningAuthorizationRecord,
	generations []SigningGeneration,
	reservation crypto.FeeReservation,
	driver crypto.NetworkDriver,
) error {
	if err := p.authorizations.CheckCustodyDispatch(ctx, current.Authorization, p.familySigners); err != nil {
		return HoldExecution(err)
	}
	if current.Authorization.Generation >= _maxReplacementGenerations {
		return nil
	}
	builder, ok := driver.Transactions().(crypto.ReplacementTransactionBuilder)
	if !ok {
		return fmt.Errorf("transaction builder does not implement replacements")
	}
	policy, err := p.feeBudget.LoadPolicy(ctx, current.Authorization.NetworkID)
	if err != nil {
		return err
	}
	asset, err := p.outboundRepo.LoadSigningAsset(ctx, current.Authorization)
	if err != nil {
		return err
	}
	intent := crypto.TransferIntent{
		BasisType:          current.Authorization.BasisType,
		Purpose:            current.Authorization.Purpose,
		BasisID:            current.Authorization.BasisID,
		Attempt:            current.Authorization.Attempt,
		Generation:         current.Authorization.Generation + 1,
		Network:            driver.Network(),
		Asset:              asset,
		SourceWalletID:     current.SourceWallet.ID,
		SourceAddress:      current.Authorization.SourceAddress,
		DestinationAddress: current.Authorization.DestinationAddress,
		Amount:             current.Authorization.AmountMinorUnits,
	}
	unsigned, quote, err := builder.BuildReplacement(ctx, intent, current.UnsignedTx, policy.PerTransactionMaxFee)
	if err != nil {
		reason := "replacement cannot satisfy fee policy: " + err.Error()
		_ = p.feeBudget.Hold(ctx, reservation.ID, reason)
		_ = p.outboundRepo.MarkSigningAuthorizationHeld(ctx, current.Authorization.AuthorizationID, current.TxID)
		return fmt.Errorf("%s", reason)
	}
	if unsigned.Generation != intent.Generation || unsigned.NetworkSequence != current.Authorization.NetworkSequence {
		return p.holdReplacementNonceDivergence(ctx, driver, current, reservation, "replacement builder changed nonce or generation")
	}
	if err := p.feeBudget.IncreaseMaxFee(ctx, reservation.ID, quote.MaxFee, unsigned.Generation); err != nil {
		_ = p.feeBudget.Hold(ctx, reservation.ID, err.Error())
		_ = p.outboundRepo.MarkSigningAuthorizationHeld(ctx, current.Authorization.AuthorizationID, current.TxID)
		return err
	}
	auth, err := p.authorizations.Create(ctx, SigningAttemptInput{
		Intent: intent, Capabilities: driver.Capabilities(), MerchantID: current.Authorization.MerchantID, VaultKeyID: current.Authorization.VaultKeyID,
		FeePayerAddress: current.Authorization.FeePayerAddress, MaxNetworkFee: quote.MaxFee,
		Unsigned: unsigned, ApprovalBundle: current.ApprovalBundle,
	})
	if err != nil {
		return fmt.Errorf("create replacement signing authorization: %w", err)
	}
	signer, err := p.familySigners.Resolve(current.Authorization.ChainFamily)
	if err != nil {
		return err
	}
	signed, err := p.authorizations.SignOrReuse(ctx, signer, auth)
	if err != nil {
		_ = p.feeBudget.Hold(ctx, reservation.ID, err.Error())
		_ = p.outboundRepo.MarkSigningAuthorizationHeld(ctx, auth.Authorization.AuthorizationID, "")
		return err
	}
	if err := p.authorizations.CheckCustodyDispatch(ctx, auth.Authorization, p.familySigners); err != nil {
		return HoldExecution(err)
	}
	outcome, broadcastErr := driver.Broadcaster().Broadcast(ctx, signed)
	p.recordBroadcast(ctx, driver.Network(), intent.Purpose, outcome)
	if outcome.Status == crypto.BroadcastRejected {
		reason := "replacement broadcast rejected: " + outcome.Reason
		_ = p.outboundRepo.MarkSigningAuthorizationFailed(ctx, auth.Authorization.AuthorizationID, signed.TxHash)
		_ = p.feeBudget.Hold(ctx, reservation.ID, reason)
		_ = p.outboundRepo.MarkSigningAuthorizationHeld(ctx, current.Authorization.AuthorizationID, current.TxID)
		return fmt.Errorf("%s", reason)
	}
	if outcome.TxHash != "" && !strings.EqualFold(outcome.TxHash, signed.TxHash) {
		return p.holdReplacementNonceDivergence(ctx, driver, current, reservation, "replacement broadcast returned a different transaction hash")
	}
	if broadcastErr != nil && outcome.Status != crypto.BroadcastAmbiguous {
		return broadcastErr
	}
	// Ambiguous broadcast is persisted as the current generation because its
	// raw bytes may already be in a provider's mempool. The next cycle will
	// assess every known generation before taking any further action.
	err = p.transactions.RunInTransaction(ctx, func(txCtx context.Context) error {
		if err := p.outboundRepo.UpdateOutboundReplacementHash(txCtx, candidate.BasisType, candidate.BasisID, current.TxID, signed.TxHash); err != nil {
			return err
		}
		if err := p.feeBudget.MarkReplacementBroadcast(txCtx, reservation.ID, current.Authorization.UnsignedFingerprint, current.TxID, unsigned.UnsignedFingerprint, signed.TxHash, unsigned.Generation); err != nil {
			return err
		}
		if err := p.outboundRepo.MarkSigningAuthorizationBroadcast(txCtx, auth.Authorization.AuthorizationID, signed.TxHash); err != nil {
			return err
		}
		for _, generation := range generations {
			if err := p.outboundRepo.MarkSigningAuthorizationSuperseded(txCtx, generation.AuthorizationID, signed.TxHash); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	p.log.Infow("replacement generation broadcast", "basis_type", candidate.BasisType, "basis_id", candidate.BasisID,
		"attempt", candidate.Attempt, "generation", unsigned.Generation, "tx_hash", signed.TxHash,
		"ambiguous", outcome.Status == crypto.BroadcastAmbiguous)
	p.recordReplacement(ctx, current.Authorization.NetworkID, unsigned.Generation, "broadcast")
	return nil
}

func (p *ReplacementService) acceptMinedReplacementGeneration(
	ctx context.Context,
	candidate ReplacementCandidate,
	current SigningAuthorizationRecord,
	generations []SigningGeneration,
	reservation crypto.FeeReservation,
	winnerHash string,
) error {
	var winner *SigningGeneration
	for i := range generations {
		if strings.EqualFold(generations[i].TxHash, winnerHash) {
			winner = &generations[i]
			break
		}
	}
	if winner == nil {
		return fmt.Errorf("RPC reported an unknown mined replacement hash")
	}
	return p.transactions.RunInTransaction(ctx, func(txCtx context.Context) error {
		if !strings.EqualFold(current.TxID, winner.TxHash) {
			if err := p.outboundRepo.UpdateOutboundReplacementHash(txCtx, candidate.BasisType, candidate.BasisID, current.TxID, winner.TxHash); err != nil {
				return err
			}
			if err := p.feeBudget.MarkReplacementBroadcast(txCtx, reservation.ID, current.Authorization.UnsignedFingerprint, current.TxID,
				winner.UnsignedFingerprint, winner.TxHash, winner.Generation); err != nil {
				return err
			}
		}
		if err := p.outboundRepo.MarkSigningAuthorizationWinner(txCtx, winner.AuthorizationID, winner.TxHash); err != nil {
			return err
		}
		for _, generation := range generations {
			if generation.AuthorizationID == winner.AuthorizationID {
				continue
			}
			if err := p.outboundRepo.MarkSigningAuthorizationSuperseded(txCtx, generation.AuthorizationID, winner.TxHash); err != nil {
				return err
			}
		}
		return nil
	})
}

func (p *ReplacementService) holdReplacementNonceDivergence(ctx context.Context, driver crypto.NetworkDriver, current SigningAuthorizationRecord, reservation crypto.FeeReservation, reason string) error {
	handler, ok := driver.(crypto.NonceDivergenceHandler)
	if !ok {
		return fmt.Errorf("driver does not implement sequence divergence handling")
	}
	if strings.TrimSpace(reason) == "" {
		reason = "account sequence diverged from persisted generations"
	}
	if err := handler.HoldNonceDivergence(ctx, current.Authorization.SourceAddress, reason); err != nil {
		return err
	}
	_ = p.feeBudget.Hold(ctx, reservation.ID, reason)
	_ = p.outboundRepo.MarkSigningAuthorizationHeld(ctx, current.Authorization.AuthorizationID, current.TxID)
	return fmt.Errorf("account sequence divergence: %s", reason)
}
