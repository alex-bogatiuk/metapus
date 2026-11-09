package outboundapp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/catalogs/token"
	"metapus/internal/domain/crypto"
)

const nativeFundingBasisType = crypto.NativeFundingBasisType

type NativeFundingRepository interface {
	Lookup(context.Context, string, id.ID, int) (crypto.NativeFunding, bool, error)
	Create(context.Context, crypto.NativeFunding) (crypto.NativeFunding, error)
	LoadWallet(context.Context, id.ID, id.ID, string) (HotWallet, error)
	OperatingBalance(context.Context, id.ID, id.ID) (types.CryptoAmount, bool, error)
	LinkReservation(context.Context, id.ID, id.ID) error
	RetryBeforeAuthorization(context.Context, id.ID) error
	SetStatus(context.Context, id.ID, string, string, string, *id.ID) error
	MarkBroadcast(context.Context, id.ID, string, string) error
	GetSigningAttempt(context.Context, string, id.ID, crypto.SigningPurpose, int, int) (SigningAuthorizationRecord, bool, error)
	MarkSigningAuthorizationBroadcast(context.Context, string, string) error
}
type FundingSourceResolver interface {
	ResolveNativeFundingSource(context.Context, id.ID, id.ID) (HotWallet, error)
}

type FundingAuthorizations interface {
	Authorizations
	CheckCustody(context.Context, crypto.SigningAuthorization) error
	CheckCustodySigner(context.Context, crypto.NetworkRef, crypto.PolicyAwareTransactionSigner) error
}
type FundingSettlement interface {
	Settle(context.Context, id.ID, crypto.ChainReceipt) (bool, error)
}
type FundingObserver interface {
	Observer
	FundingFinality(context.Context, crypto.NetworkRef) (context.Context, func(error))
	FundingSettled(context.Context, id.ID, types.CryptoAmount)
}
type NativeFundingService struct {
	repository     NativeFundingRepository
	sources        FundingSourceResolver
	feeBudget      FeeBudget
	authorizations FundingAuthorizations
	settlement     FundingSettlement
	observer       FundingObserver
	evidence       FundingEvidence
}

func NewNativeFundingService(repo NativeFundingRepository, sources FundingSourceResolver, fees FeeBudget, auth FundingAuthorizations, settlement FundingSettlement, observer FundingObserver, evidence FundingEvidence) *NativeFundingService {
	return &NativeFundingService{repository: repo, sources: sources, feeBudget: fees, authorizations: auth, settlement: settlement, observer: observer, evidence: evidence}
}
func (s *NativeFundingService) Ensure(
	ctx context.Context,
	driver crypto.NetworkDriver,
	policy crypto.NetworkPolicy,
	req NativeFundingRequest,
	signer crypto.PolicyAwareTransactionSigner,
	requiredMaxFee types.CryptoAmount,
) (ready bool, err error) {
	if s != nil && s.observer != nil {
		var finish func(error)
		ctx, finish = s.observer.Start(ctx, "crypto.native_funding.execute", req.NetworkID, crypto.SigningPurposeNativeFunding)
		defer func() { finish(err) }()
	}

	if s == nil || s.repository == nil || s.feeBudget == nil || s.authorizations == nil || s.settlement == nil {
		return false, fmt.Errorf("%w: native funding service is not configured", ErrNativeFundingHeld)
	}
	if strings.TrimSpace(req.BasisType) == "" || id.IsNil(req.BasisID) || id.IsNil(req.NetworkID) || id.IsNil(req.WalletID) || strings.TrimSpace(req.Address) == "" {
		return false, fmt.Errorf("%w: native funding request identity is incomplete", ErrNativeFundingHeld)
	}
	// Tracking a prior broadcast is independent of today's owner/source policy.
	prior, priorFound, lookupErr := s.repository.Lookup(ctx, req.BasisType, req.BasisID, 0)
	if lookupErr != nil {
		return false, lookupErr
	}
	if priorFound && (prior.NetworkID != req.NetworkID || prior.DestinationWalletID != req.WalletID || driver.Network().ID != req.NetworkID) {
		return false, fmt.Errorf("%w: persisted funding belongs to another destination/network", ErrMerchantCustody)
	}
	if priorFound && prior.FeeReservationID != nil && prior.SigningAuthorizationID != nil && prior.Status != "settled" && prior.Status != "failed" && prior.Status != "broadcast" && prior.Status != "solidified" {
		saved, found, err := s.repository.GetSigningAttempt(ctx, nativeFundingBasisType, prior.ID, crypto.SigningPurposeNativeFunding, 1, 1)
		if err != nil {
			return false, err
		}
		if found && observedSavedOutflow(ctx, driver, saved) {
			hash := saved.SignedTx.TxHash
			if err := s.feeBudget.MarkBroadcast(ctx, *prior.FeeReservationID, saved.Authorization.UnsignedFingerprint, hash); err != nil {
				return false, err
			}
			if err := s.repository.MarkSigningAuthorizationBroadcast(ctx, saved.Authorization.AuthorizationID, hash); err != nil {
				return false, err
			}
			if err := s.repository.MarkBroadcast(ctx, prior.ID, hash, saved.Authorization.UnsignedFingerprint); err != nil {
				return false, err
			}
			prior.Status = "broadcast"
			prior.TxHash = hash
			return s.Confirm(ctx, driver, prior)
		}
	}
	if priorFound && prior.Status == "settled" {
		balance, enabled, err := s.repository.OperatingBalance(ctx, req.WalletID, driver.Network().NativeTokenID)
		if err != nil {
			return false, err
		}
		if !enabled || balance.Cmp(requiredMaxFee.Add(policy.FeeFunding.MinSourceWalletResidual)) >= 0 {
			return true, nil
		}
	}
	if priorFound && (prior.Status == "broadcast" || prior.Status == "solidified") {
		return s.Confirm(ctx, driver, prior)
	}

	if policy.FeeFunding.Mode != crypto.FeeFundingJIT || !policy.FeeFunding.MaxFundingAmount.IsPositive() {
		return false, fmt.Errorf("%w: JIT funding policy is not configured", ErrNativeFundingHeld)
	}
	if s.sources == nil {
		return false, fmt.Errorf("%w: funding source policy is not configured", ErrNativeFundingHeld)
	}
	feeWallet, err := s.sources.ResolveNativeFundingSource(ctx, req.NetworkID, req.WalletID)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrNativeFundingHeld, err)
	}
	destination, err := s.repository.LoadWallet(ctx, req.WalletID, req.NetworkID, "")
	if err != nil {
		return false, fmt.Errorf("%w: destination wallet: %v", ErrNativeFundingHeld, err)
	}
	if destination.Address != req.Address {
		return false, fmt.Errorf("%w: destination address does not match parent wallet", ErrNativeFundingHeld)
	}
	if !requiredMaxFee.IsPositive() {
		return false, fmt.Errorf("%w: positive required fee is missing", ErrNativeFundingHeld)
	}
	requiredBalance := requiredMaxFee.Add(policy.FeeFunding.MinSourceWalletResidual)
	existing, found, err := s.repository.Lookup(ctx, req.BasisType, req.BasisID, 0)
	if err != nil {
		return false, err
	}
	fundingAttempt := 1
	if found {
		switch existing.Status {
		case "settled":
			fundingAttempt = existing.Attempt + 1
			if fundingAttempt > 3 {
				return false, fmt.Errorf("%w: operating top-up round limit reached", ErrNativeFundingHeld)
			}
		case "held":
			// A hold before reservation/authorization has no external or economic
			// effect. Re-evaluate it against current balances and policy so an
			// operator top-up can resume the same durable funding intent.
			if existing.FeeReservationID == nil && existing.SigningAuthorizationID == nil &&
				existing.UnsignedFingerprint == "" && existing.TxHash == "" {
				if err := s.repository.RetryBeforeAuthorization(ctx, existing.ID); err != nil {
					return false, err
				}
				existing.Status = "created"
				existing.LastError = ""
				break
			}
			return false, fmt.Errorf("%w: %s", ErrNativeFundingHeld, existing.LastError)
		case "failed":
			return false, fmt.Errorf("%w: %s", ErrNativeFundingHeld, existing.LastError)
		case "broadcast", "solidified":
			// Settle the in-flight JIT into the ledger before treating chain
			// balance as sufficient. Otherwise the later fee reservation sees
			// residual against a still-zero destination ledger.
			return s.Confirm(ctx, driver, existing)
		}
	}
	chainBalance, err := driver.Balances().NativeBalance(ctx, req.Address)
	if err != nil {
		return false, fmt.Errorf("%w: read destination native balance: %v", ErrNativeFundingHeld, err)
	}
	operating, enabled, err := s.repository.OperatingBalance(ctx, req.WalletID, driver.Network().NativeTokenID)
	if err != nil {
		return false, err
	}
	if enabled && operating.Cmp(chainBalance) < 0 {
		chainBalance = operating
	}
	if chainBalance.Cmp(requiredBalance) >= 0 {
		return true, nil
	}
	deficit := requiredBalance.Sub(chainBalance)
	if !deficit.IsPositive() || deficit.Cmp(policy.FeeFunding.MaxFundingAmount) > 0 {
		return false, fmt.Errorf("%w: exact deficit %s exceeds per-operation cap %s", ErrNativeFundingHeld, deficit, policy.FeeFunding.MaxFundingAmount)
	}

	funding, err := s.repository.Create(ctx, crypto.NativeFunding{
		ID: id.New(), BasisType: req.BasisType, BasisID: req.BasisID, Attempt: fundingAttempt,
		NetworkID: req.NetworkID, NativeTokenID: driver.Network().NativeTokenID,
		SourceWalletID: feeWallet.ID, DestinationWalletID: req.WalletID,
		Amount: deficit,
	})
	if err != nil {
		return false, err
	}
	if funding.NetworkID != req.NetworkID || funding.NativeTokenID != driver.Network().NativeTokenID ||
		funding.SourceWalletID != feeWallet.ID || funding.DestinationWalletID != req.WalletID ||
		!funding.Amount.IsPositive() || funding.Amount.Cmp(policy.FeeFunding.MaxFundingAmount) > 0 {
		return false, fmt.Errorf("%w: persisted funding intent conflicts with current policy", ErrNativeFundingHeld)
	}

	switch funding.Status {
	case "settled":
		return true, nil
	case "held", "failed":
		return false, fmt.Errorf("%w: %s", ErrNativeFundingHeld, funding.LastError)
	case "broadcast", "solidified":
		return s.Confirm(ctx, driver, funding)
	}

	network := driver.Network()
	intent := crypto.TransferIntent{
		BasisType: nativeFundingBasisType, Purpose: crypto.SigningPurposeNativeFunding,
		BasisID: funding.ID, Attempt: 1, Network: network,
		Asset:          crypto.AssetRef{TokenID: network.NativeTokenID, Standard: token.TokenStandardNative, Decimals: network.NativeDecimals},
		SourceWalletID: feeWallet.ID, SourceAddress: feeWallet.Address,
		DestinationAddress: req.Address, Amount: funding.Amount,
	}
	if _, err := s.authorizations.PrepareTransferCustody(ctx, intent, nil); err != nil {
		return false, fmt.Errorf("%w: %v", ErrNativeFundingHeld, err)
	}

	var auth SigningAuthorizationRecord
	auth, hasAuthorization, err := s.repository.GetSigningAttempt(ctx, intent.BasisType, intent.BasisID, intent.Purpose, intent.Attempt, intent.EffectiveGeneration())
	if err != nil {
		return false, err
	}
	var reservation crypto.FeeReservation
	if funding.FeeReservationID != nil {
		reservation, _, err = s.feeBudget.Get(ctx, intent.BasisType, intent.BasisID, intent.Purpose, intent.Attempt)
		if err != nil {
			return false, err
		}
	}
	if !hasAuthorization {
		quote, quoteErr := driver.Transactions().QuoteFee(ctx, intent)
		if quoteErr != nil {
			return false, fmt.Errorf("quote native funding fee: %w", quoteErr)
		}
		quote, quoteErr = policy.BoundFeeQuote(quote)
		if quoteErr != nil {
			return false, fmt.Errorf("apply native funding fee policy: %w", quoteErr)
		}
		reservation, err = s.feeBudget.Reserve(ctx, FeeReservationRequest{
			NetworkID: network.ID, PayerWalletID: feeWallet.ID, NativeTokenID: network.NativeTokenID,
			OperationTokenID: network.NativeTokenID, ReservedPrincipal: funding.Amount,
			BasisType: intent.BasisType, BasisID: intent.BasisID, Purpose: intent.Purpose, Attempt: intent.Attempt,
			Strategy: crypto.FeeStrategyNativeFunding, MaxFee: quote.MaxFee,
		})
		if err != nil {
			_ = s.repository.SetStatus(ctx, funding.ID, "held", err.Error(), "", nil)
			return false, fmt.Errorf("%w: reserve JIT funding: %v", ErrNativeFundingHeld, err)
		}
		if err := s.repository.LinkReservation(ctx, funding.ID, reservation.ID); err != nil {
			_ = s.feeBudget.Hold(ctx, reservation.ID, err.Error())
			return false, err
		}
		unsigned, buildErr := driver.Transactions().Build(ctx, intent, quote)
		if buildErr != nil {
			reclaimUnsignedSequence(ctx, driver, intent)
			_ = s.feeBudget.Release(ctx, reservation.ID, buildErr.Error())
			return false, fmt.Errorf("build native funding transaction: %w", buildErr)
		}
		if err := s.feeBudget.BindUnsignedFingerprint(ctx, reservation.ID, unsigned.UnsignedFingerprint); err != nil {
			reclaimUnsignedSequence(ctx, driver, intent)
			_ = s.feeBudget.Hold(ctx, reservation.ID, err.Error())
			return false, err
		}
		auth, err = s.authorizations.Create(ctx, SigningAttemptInput{
			Intent: intent, Capabilities: driver.Capabilities(), VaultKeyID: feeWallet.VaultKeyID, FeePayerAddress: feeWallet.Address,
			MaxNetworkFee: quote.MaxFee, Unsigned: unsigned,
		})
		if err != nil {
			reclaimUnsignedSequence(ctx, driver, intent)
			_ = s.feeBudget.Hold(ctx, reservation.ID, err.Error())
			_ = s.repository.SetStatus(ctx, funding.ID, "held", err.Error(), unsigned.UnsignedFingerprint, nil)
			return false, fmt.Errorf("%w: create native funding authorization: %v", ErrNativeFundingHeld, err)
		}
		if id.IsNil(auth.ID) {
			reason := "persisted signing authorization has no internal row ID"
			_ = s.feeBudget.Hold(ctx, reservation.ID, reason)
			return false, fmt.Errorf("%w: %s", ErrNativeFundingHeld, reason)
		}
		if err := s.repository.SetStatus(ctx, funding.ID, "reserved", "", unsigned.UnsignedFingerprint, &auth.ID); err != nil {
			_ = s.feeBudget.Hold(ctx, reservation.ID, err.Error())
			return false, err
		}
	} else if funding.FeeReservationID == nil {
		return false, fmt.Errorf("%w: signing authorization has no fee reservation", ErrNativeFundingHeld)
	}

	signed, err := s.authorizations.SignOrReuse(ctx, signer, auth)
	if err != nil {
		if funding.FeeReservationID != nil {
			_ = s.feeBudget.Hold(ctx, *funding.FeeReservationID, err.Error())
		} else if !id.IsNil(reservation.ID) {
			_ = s.feeBudget.Hold(ctx, reservation.ID, err.Error())
		}
		_ = s.repository.SetStatus(ctx, funding.ID, "held", err.Error(), auth.Authorization.UnsignedFingerprint, nil)
		return false, fmt.Errorf("%w: %v", ErrNativeFundingHeld, err)
	}
	if err := s.repository.SetStatus(ctx, funding.ID, "signed", "", signed.UnsignedFingerprint, nil); err != nil {
		return false, err
	}
	if err := s.authorizations.CheckCustody(ctx, auth.Authorization); err != nil {
		return false, err
	}
	if err := s.authorizations.CheckCustodySigner(ctx, driver.Network(), signer); err != nil {
		return false, fmt.Errorf("%w: %v", ErrNativeFundingHeld, err)
	}
	outcome, broadcastErr := driver.Broadcaster().Broadcast(ctx, signed)
	if s.observer != nil {
		s.observer.Broadcast(ctx, network, intent.Purpose, outcome)
	}
	reservationID := reservation.ID
	if funding.FeeReservationID != nil {
		reservationID = *funding.FeeReservationID
	}
	trackableAmbiguous := trackableAmbiguousBroadcast(network, outcome, signed.TxHash)
	if (broadcastErr != nil || outcome.Status == crypto.BroadcastAmbiguous) && !trackableAmbiguous {
		reason := outcome.Reason
		if broadcastErr != nil {
			reason = broadcastErr.Error()
		}
		_ = s.feeBudget.Hold(ctx, reservationID, reason)
		_ = s.repository.SetStatus(ctx, funding.ID, "held", reason, signed.UnsignedFingerprint, nil)
		return false, fmt.Errorf("%w: ambiguous JIT funding broadcast: %s", ErrNativeFundingHeld, reason)
	}
	if outcome.Status == crypto.BroadcastRejected {
		_ = s.feeBudget.Release(ctx, reservationID, outcome.Reason)
		_ = s.repository.SetStatus(ctx, funding.ID, "failed", outcome.Reason, signed.UnsignedFingerprint, nil)
		return false, fmt.Errorf("%w: JIT funding rejected: %s", ErrNativeFundingHeld, outcome.Reason)
	}
	if (outcome.Status != crypto.BroadcastAccepted && outcome.Status != crypto.BroadcastAlreadyKnown && !trackableAmbiguous) ||
		outcome.TxHash == "" || outcome.TxHash != signed.TxHash {
		reason := "invalid JIT funding broadcast outcome"
		_ = s.feeBudget.Hold(ctx, reservationID, reason)
		_ = s.repository.SetStatus(ctx, funding.ID, "held", reason, signed.UnsignedFingerprint, nil)
		return false, fmt.Errorf("%w: %s", ErrNativeFundingHeld, reason)
	}
	if err := s.feeBudget.MarkBroadcast(ctx, reservationID, signed.UnsignedFingerprint, signed.TxHash); err != nil {
		return false, err
	}
	if err := s.repository.MarkSigningAuthorizationBroadcast(ctx, auth.Authorization.AuthorizationID, signed.TxHash); err != nil {
		return false, err
	}
	if err := s.repository.MarkBroadcast(ctx, funding.ID, signed.TxHash, signed.UnsignedFingerprint); err != nil {
		return false, err
	}
	return false, ErrNativeFundingPending
}

func (s *NativeFundingService) Confirm(ctx context.Context, driver crypto.NetworkDriver, funding crypto.NativeFunding) (bool, error) {
	if s == nil || s.settlement == nil {
		return false, fmt.Errorf("%w: native funding settlement is not configured", ErrNativeFundingHeld)
	}
	finalityCtx := ctx
	finish := func(error) {}
	if s.observer != nil {
		finalityCtx, finish = s.observer.FundingFinality(ctx, driver.Network())
	}
	receipt, err := driver.Finality().Receipt(finalityCtx, funding.TxHash)
	finish(err)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrNativeFundingPending, err)
	}
	if !receipt.FinanciallyFinal() {
		return false, ErrNativeFundingPending
	}
	if receipt.Status == crypto.ReceiptSucceeded {
		if s.evidence == nil {
			return false, errors.Join(ErrNativeFundingPending, crypto.ErrVerificationPending)
		}
		if err := s.evidence.VerifyFunding(ctx, driver, funding, receipt); err != nil {
			if errors.Is(err, crypto.ErrVerificationPending) || errors.Is(err, crypto.ErrChainQuorumUnavailable) {
				return false, errors.Join(ErrNativeFundingPending, err)
			}
			return false, err
		}
	}
	settled, err := s.settlement.Settle(ctx, funding.ID, receipt)
	if err != nil {
		// A transient database failure rolls back the whole command and stays
		// retryable. Do not convert it into a permanent orphaned funding hold.
		return false, err
	}
	if !settled {
		return false, fmt.Errorf("%w: native funding transaction reverted", ErrNativeFundingHeld)
	}
	if s.observer != nil {
		s.observer.FundingSettled(ctx, funding.NetworkID, funding.Amount)
	}
	return true, nil
}
