package outboundapp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/core/tx"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_withdrawal"
	"metapus/internal/domain/documents/outbound_settlement"
	"metapus/internal/domain/documents/withdrawal_request"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/registers/crypto_platform_cost"
	"metapus/pkg/logger"
)

type WithdrawalExecutor struct {
	drivers           *crypto.ChainDriverRegistry
	signers           *crypto.FamilySignerRegistry
	withdrawalRepo    crypto_withdrawal.Repository
	outboundRepo      WithdrawalRepository
	approvalRepo      ApprovalBundles
	postingEngine     *posting.Engine
	numerator         numerator.Generator
	resourcePreparers ResourcePreparerResolver
	feeBudget         FeeBudget
	nativeFunding     NativeFunding
	approvalSigners   crypto.MoneyMovementApprovalSignerProvider
	authorizations    Authorizations
	tenantID          string
	log               *logger.Logger
	observer          Observer
	txm               tx.Manager
	now               func() time.Time
}

type WithdrawalConfig struct {
	Drivers         *crypto.ChainDriverRegistry
	Signers         *crypto.FamilySignerRegistry
	Documents       crypto_withdrawal.Repository
	Repository      WithdrawalRepository
	Approvals       ApprovalBundles
	ApprovalSigners crypto.MoneyMovementApprovalSignerProvider
	Posting         *posting.Engine
	Numerator       numerator.Generator
	Resources       ResourcePreparerResolver
	Fees            FeeBudget
	Funding         NativeFunding
	Authorizations  Authorizations
	Transactions    tx.Manager
	TenantID        string
	Logger          *logger.Logger
	Observer        Observer
	Now             func() time.Time
}

func NewWithdrawalExecutor(c WithdrawalConfig) *WithdrawalExecutor {
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Logger == nil {
		c.Logger = logger.Default()
	}
	return &WithdrawalExecutor{drivers: c.Drivers, signers: c.Signers, withdrawalRepo: c.Documents, outboundRepo: c.Repository, approvalRepo: c.Approvals, approvalSigners: c.ApprovalSigners, postingEngine: c.Posting, numerator: c.Numerator, resourcePreparers: c.Resources, feeBudget: c.Fees, nativeFunding: c.Funding, authorizations: c.Authorizations, txm: c.Transactions, tenantID: c.TenantID, log: c.Logger, observer: c.Observer, now: c.Now}
}
func (e *WithdrawalExecutor) Execute(ctx context.Context, task WithdrawalTask) (err error) {
	if e.observer != nil {
		var finish func(error)
		ctx, finish = e.observer.Start(ctx, "crypto.withdrawal.execute", task.NetworkID, crypto.SigningPurposeWithdrawal)
		defer func() { finish(err) }()
	}

	var hotWallet HotWallet
	var auth SigningAuthorizationRecord
	var ok bool
	if !id.IsNil(task.AMLParentID) {
		hotWallet = task.AMLSource
		auth, ok, err = e.outboundRepo.GetSigningAttempt(ctx, task.SigningBasis(), task.RequestID, crypto.SigningPurposeWithdrawal, 1, 1)
	} else {
		hotWallet, auth, ok, err = e.outboundRepo.GetWithdrawalSigningAttempt(ctx, task.RequestID)
	}
	if err != nil {
		return fmt.Errorf("load withdrawal signing attempt: %w", err)
	}
	if ok && (id.IsNil(task.AMLParentID) || auth.SignedTx != nil) {
		return e.recoverSignedWithdrawal(ctx, task, auth)
	}
	if task.RecoveryOnly {
		return HoldExecution(fmt.Errorf("held withdrawal has no recoverable signed transaction"))
	}
	if err := e.outboundRepo.ValidateExternalWithdrawalDestination(ctx, task.NetworkID, task.DestAddress); err != nil {
		_ = e.outboundRepo.MarkWithdrawalHeld(ctx, task.RequestID, statusFailedReason(err))
		return HoldExecution(err)
	}
	approvalBundle, approvalDigest, err := e.loadAndVerifyApprovalBundle(ctx, task)
	if err != nil {
		if !ok && errors.Is(err, crypto.ErrApprovalArtifactExpired) {
			// The failure service rechecks absence under the authorization lock.
			return PreEffectWithdrawalError(err)
		}
		_ = e.outboundRepo.MarkWithdrawalHeld(ctx, task.RequestID, statusFailedReason(err))
		return HoldExecution(err)
	}
	if ok && auth.Authorization.ApprovalBundleDigest != "" && auth.Authorization.ApprovalBundleDigest != approvalDigest {
		cause := fmt.Errorf("stored signing authorization approval bundle digest mismatch")
		_ = e.outboundRepo.MarkWithdrawalHeld(ctx, task.RequestID, statusFailedReason(cause))
		return HoldExecution(cause)
	}
	if ok && auth.ApprovalBundle.IntentJWS == "" {
		auth.ApprovalBundle = approvalBundle
		auth.Authorization.ApprovalBundleDigest = approvalDigest
	}
	if !ok && id.IsNil(task.AMLParentID) {
		hotWallet, err = e.outboundRepo.ReserveHotWalletForWithdrawal(ctx, task.RequestID, task.MerchantID, task.NetworkID, task.TokenID, task.Amount)
		if err != nil {
			return fmt.Errorf("reserve hot wallet: %w", err)
		}

	}
	candidate := crypto_withdrawal.NewCryptoWithdrawal(task.MerchantID, task.TokenID, hotWallet.ID, task.DestAddress, task.Amount)
	candidate.BasisType = task.SigningBasis()
	candidate.BasisID = new(task.RequestID)
	number, err := e.numerator.GetNextNumber(ctx, numerator.DefaultConfig("CW"), nil, e.now())
	if err != nil {
		return fmt.Errorf("generate withdrawal number: %w", err)
	}
	candidate.Number = number
	var doc *crypto_withdrawal.CryptoWithdrawal
	if !id.IsNil(task.AMLParentID) {
		doc, err = e.outboundRepo.EnsureAMLWithdrawalExecution(ctx, task, candidate, e.withdrawalRepo)
	} else {
		doc, err = e.outboundRepo.EnsureWithdrawalExecutionDocument(ctx, task.RequestID, candidate, e.withdrawalRepo)
	}
	if err != nil {
		return fmt.Errorf("ensure withdrawal execution draft: %w", err)
	}
	driver, err := e.drivers.Resolve(task.NetworkID)
	if err != nil {
		return HoldExecution(fmt.Errorf("resolve withdrawal network driver: %w", err))
	}
	network := driver.Network()
	asset := crypto.AssetRef{TokenID: task.TokenID, Standard: task.TokenStandard, ContractAddress: task.ContractAddress, Decimals: task.TokenDecimals}
	intent := crypto.TransferIntent{
		BasisType: task.SigningBasis(), Purpose: crypto.SigningPurposeWithdrawal,
		BasisID: task.RequestID, Attempt: 1, Network: network, Asset: asset,
		SourceWalletID: hotWallet.ID, SourceAddress: hotWallet.Address,
		DestinationAddress: task.DestAddress, Amount: task.Amount,
	}
	if _, err := e.authorizations.PrepareTransferCustody(ctx, intent, &task.MerchantID); err != nil {
		return HoldExecution(err)
	}
	if e.feeBudget == nil {
		return HoldExecution(fmt.Errorf("fee budget manager is not configured"))
	}
	policy, err := e.feeBudget.LoadPolicy(ctx, network.ID)
	if err != nil {
		return HoldExecution(fmt.Errorf("load withdrawal network policy: %w", err))
	}
	if !policy.Enabled {
		return HoldExecution(ErrNetworkPolicyDisabled)
	}
	var familySigner crypto.PolicyAwareTransactionSigner
	if !ok || auth.SignedTx == nil {
		familySigner, err = e.authorizations.ResolveSigner(ctx, e.signers, network)
		if err != nil {
			return HoldExecution(err)
		}
	}
	var quote crypto.FeeQuote
	var feeReservation crypto.FeeReservation
	if !ok {
		quote, err = driver.Transactions().QuoteFee(ctx, intent)
		if err != nil {
			return HoldExecution(fmt.Errorf("quote withdrawal fee: %w", err))
		}
		quote, err = policy.BoundFeeQuote(quote)
		if err != nil {
			return HoldExecution(fmt.Errorf("apply withdrawal fee policy: %w", err))
		}
	} else {
		var found bool
		feeReservation, found, err = e.feeBudget.Get(ctx, intent.BasisType, intent.BasisID, intent.Purpose, intent.Attempt)
		if err != nil {
			return HoldExecution(fmt.Errorf("load persisted withdrawal fee reservation: %w", err))
		}
		if !found {
			return HoldExecution(fmt.Errorf("persisted withdrawal signing attempt has no fee reservation"))
		}
		if feeReservation.Status == crypto.FeeReservationHeld {
			return HoldExecution(fmt.Errorf("%w: %s", ErrFeeReservationHeld, feeReservation.HoldReason))
		}
	}

	var resource ResourceReservationResult
	if !ok {
		providerFailureThreshold, providerOpenDuration := resourceCircuitValues(policy)
		resourceRequest := ResourceRequest{
			TenantID: e.tenantID, BasisType: crypto_withdrawal.DocumentType, BasisID: doc.ID,
			RentalBasisType: crypto_withdrawal.DocumentType, RentalBasisID: doc.ID,
			Purpose: crypto_platform_cost.PlatformCostPurposeWithdrawal, NetworkID: task.NetworkID,
			OperationTokenID: task.TokenID, SourceWalletID: hotWallet.ID,
			SourceAddress: hotWallet.Address, ContractAddress: task.ContractAddress,
			DestinationAddress: task.DestAddress, Amount: task.Amount,
			Requirements:             quote.ResourceRequirements,
			ProviderFailureThreshold: providerFailureThreshold,
			ProviderOpenDuration:     providerOpenDuration,
		}
		var feeStrategy crypto.FeeStrategy
		resource, feeStrategy, err = prepareObservedResource(ctx, e.observer, resolveResourcePreparer(e.resourcePreparers, network.Family), policy, len(quote.ResourceRequirements) > 0, resourceRequest)
		if err != nil {
			return HoldExecution(fmt.Errorf("prepare withdrawal chain resources: %w", err))
		}
		feeReservation, err = e.feeBudget.Reserve(ctx, FeeReservationRequest{
			NetworkID: network.ID, PayerWalletID: hotWallet.ID, NativeTokenID: network.NativeTokenID, OperationTokenID: task.TokenID,
			BasisType: intent.BasisType, BasisID: intent.BasisID, Purpose: intent.Purpose, Attempt: intent.Attempt,
			Strategy: feeStrategy, MaxFee: quote.MaxFee,
		})
		if err != nil {
			if errors.Is(err, ErrFeeBalanceResidual) && task.ContractAddress != "" {
				ready, fundingErr := e.nativeFunding.Ensure(ctx, driver, policy, NativeFundingRequest{
					BasisType: task.SigningBasis(),
					BasisID:   task.RequestID,
					NetworkID: task.NetworkID,
					WalletID:  hotWallet.ID,
					Address:   hotWallet.Address,
				}, familySigner, quote.MaxFee)
				if fundingErr != nil {
					if errors.Is(fundingErr, ErrNativeFundingPending) {
						// Keep the immutable provider reservation while the exact
						// fee-wallet transfer reaches financial finality.
						return fundingErr
					}
					_ = releasePreparedResource(ctx, resolveResourcePreparer(e.resourcePreparers, network.Family), resource.AllocationID, fundingErr.Error())
					return HoldExecution(fundingErr)
				}
				if !ready {
					return ErrNativeFundingPending
				}
				feeReservation, err = e.feeBudget.Reserve(ctx, FeeReservationRequest{
					NetworkID: network.ID, PayerWalletID: hotWallet.ID, NativeTokenID: network.NativeTokenID, OperationTokenID: task.TokenID,
					BasisType: intent.BasisType, BasisID: intent.BasisID, Purpose: intent.Purpose, Attempt: intent.Attempt,
					Strategy: feeStrategy, MaxFee: quote.MaxFee,
				})
			} else {
				_ = releasePreparedResource(ctx, resolveResourcePreparer(e.resourcePreparers, network.Family), resource.AllocationID, err.Error())
			}
		}
		if err != nil {
			_ = releasePreparedResource(ctx, resolveResourcePreparer(e.resourcePreparers, network.Family), resource.AllocationID, err.Error())
			return HoldExecution(fmt.Errorf("reserve withdrawal network fee: %w", err))
		}
	}

	if !ok {
		unsigned, err := driver.Transactions().Build(ctx, intent, quote)
		if err != nil {
			reclaimUnsignedSequence(ctx, driver, intent)
			_ = releasePreparedResource(ctx, resolveResourcePreparer(e.resourcePreparers, network.Family), resource.AllocationID, err.Error())
			_ = e.feeBudget.Release(ctx, feeReservation.ID, err.Error())
			return fmt.Errorf("build withdrawal transaction: %w", err)
		}
		if err := e.feeBudget.BindUnsignedFingerprint(ctx, feeReservation.ID, unsigned.UnsignedFingerprint); err != nil {
			reclaimUnsignedSequence(ctx, driver, intent)
			_ = releasePreparedResource(ctx, resolveResourcePreparer(e.resourcePreparers, network.Family), resource.AllocationID, err.Error())
			_ = e.feeBudget.Hold(ctx, feeReservation.ID, err.Error())
			return HoldExecution(err)
		}
		auth, err = e.authorizations.Create(ctx, SigningAttemptInput{
			Intent: intent, Capabilities: driver.Capabilities(), MerchantID: &task.MerchantID, VaultKeyID: hotWallet.VaultKeyID,
			FeePayerAddress: hotWallet.Address, MaxNetworkFee: quote.MaxFee,
			Unsigned: unsigned, ApprovalBundle: approvalBundle,
		})
		if err != nil {
			reclaimUnsignedSequence(ctx, driver, intent)
			_ = releasePreparedResource(ctx, resolveResourcePreparer(e.resourcePreparers, network.Family), resource.AllocationID, err.Error())
			_ = e.feeBudget.Hold(ctx, feeReservation.ID, err.Error())
			return HoldExecution(fmt.Errorf("create withdrawal signing authorization: %w", err))
		}
	}

	signedTx, err := e.authorizations.SignOrReuse(ctx, familySigner, auth)
	if err != nil {
		_ = releasePreparedResource(ctx, resolveResourcePreparer(e.resourcePreparers, network.Family), resource.AllocationID, err.Error())
		_ = e.feeBudget.Hold(ctx, feeReservation.ID, err.Error())
		return HoldExecution(err)
	}
	if err := e.outboundRepo.MarkWithdrawalExecutionSigned(ctx, doc.ID); err != nil {
		return HoldExecution(err)
	}

	if err := e.authorizations.CheckCustodyDispatch(ctx, auth.Authorization, e.signers); err != nil {
		return HoldExecution(err)
	}
	outcome, err := driver.Broadcaster().Broadcast(ctx, signedTx)
	if e.observer != nil {
		e.observer.Broadcast(ctx, network, intent.Purpose, outcome)
	}
	trackableAmbiguous := trackableAmbiguousBroadcast(network, outcome, signedTx.TxHash)
	if err != nil && !trackableAmbiguous {
		_ = e.feeBudget.Hold(ctx, feeReservation.ID, err.Error())
		if markErr := e.outboundRepo.MarkSigningAuthorizationHeld(ctx, auth.Authorization.AuthorizationID, signedTx.TxHash); markErr != nil {
			return HoldExecution(fmt.Errorf("broadcast withdrawal tx result is uncertain: %w; mark authorization held: %v", err, markErr))
		}
		return HoldExecution(fmt.Errorf("broadcast withdrawal tx result is uncertain: %w", err))
	}

	switch outcome.Status {
	case crypto.BroadcastAccepted, crypto.BroadcastAlreadyKnown, crypto.BroadcastAmbiguous:
		if outcome.Status == crypto.BroadcastAmbiguous && !trackableAmbiguous {
			_ = e.feeBudget.Hold(ctx, feeReservation.ID, outcome.Reason)
			if err := e.outboundRepo.MarkSigningAuthorizationHeld(ctx, auth.Authorization.AuthorizationID, signedTx.TxHash); err != nil {
				return HoldExecution(fmt.Errorf("record uncertain withdrawal tx: %w", err))
			}
			return HoldExecution(fmt.Errorf("broadcast withdrawal tx result is uncertain: %s", outcome.Reason))
		}
		if outcome.TxHash == "" || outcome.TxHash != signedTx.TxHash {
			return HoldExecution(fmt.Errorf("broadcast withdrawal tx returned empty tx hash"))
		}
	case crypto.BroadcastRejected:
		if err := e.outboundRepo.MarkSigningAuthorizationHeld(ctx, auth.Authorization.AuthorizationID, signedTx.TxHash); err != nil {
			return HoldExecution(fmt.Errorf("record rejected withdrawal tx: %w", err))
		}
		_ = e.feeBudget.Hold(ctx, feeReservation.ID, outcome.Reason)
		return HoldExecution(fmt.Errorf("broadcast withdrawal tx rejected; final settlement required: %s", outcome.Reason))
	default:
		_ = e.feeBudget.Hold(ctx, feeReservation.ID, outcome.Reason)
		if err := e.outboundRepo.MarkSigningAuthorizationHeld(ctx, auth.Authorization.AuthorizationID, signedTx.TxHash); err != nil {
			return HoldExecution(fmt.Errorf("record uncertain withdrawal tx: %w", err))
		}
		return HoldExecution(fmt.Errorf("broadcast withdrawal tx result is uncertain: %s", outcome.Reason))
	}

	txHash := outcome.TxHash
	if err := e.feeBudget.MarkBroadcast(ctx, feeReservation.ID, auth.Authorization.UnsignedFingerprint, txHash); err != nil {
		return HoldExecution(fmt.Errorf("record withdrawal fee reservation broadcast: %w", err))
	}
	if err := markPreparedResourceBroadcast(ctx, resolveResourcePreparer(e.resourcePreparers, network.Family), ResourceBroadcastCommand{
		AllocationID: resource.AllocationID,
		BasisType:    crypto_withdrawal.DocumentType,
		BasisID:      doc.ID,
		TxHash:       txHash,
	}); err != nil {
		return HoldExecution(fmt.Errorf("record withdrawal resource allocation broadcast: %w", err))
	}

	if err := e.completeWithdrawalBroadcast(ctx, task, auth.Authorization, feeReservation.ID, txHash); err != nil {
		if markErr := e.outboundRepo.MarkSigningAuthorizationHeld(ctx, auth.Authorization.AuthorizationID, txHash); markErr != nil {
			return HoldExecution(fmt.Errorf("post crypto withdrawal after broadcast: %w; mark authorization held: %v", err, markErr))
		}
		return HoldExecution(fmt.Errorf("post crypto withdrawal after broadcast: %w", err))
	}

	e.log.Infow("withdrawal broadcast",
		"request_id", task.RequestID,
		"withdrawal_id", doc.ID,
		"tx_hash", txHash,
		"authorization_id", auth.Authorization.AuthorizationID,
		"basis_id", auth.Authorization.BasisID,
		"tx_id", signedTx.TxHash,
		"amount", auth.Authorization.AmountMinorUnits,
		"policy_version", auth.Authorization.PolicyVersion,
	)
	return nil
}

func (e *WithdrawalExecutor) loadAndVerifyApprovalBundle(ctx context.Context, task WithdrawalTask) (crypto.ApprovalBundle, string, error) {
	if e.approvalRepo == nil {
		return crypto.ApprovalBundle{}, "", fmt.Errorf("money movement approval repo is not configured")
	}
	if e.approvalSigners == nil {
		return crypto.ApprovalBundle{}, "", fmt.Errorf("money movement approval verifier is not configured")
	}
	bundle, ok, err := e.approvalRepo.LoadBundle(ctx, withdrawal_request.DocumentType, task.ApprovalRequestID())
	if err != nil {
		return crypto.ApprovalBundle{}, "", err
	}
	if !ok {
		return crypto.ApprovalBundle{}, "", fmt.Errorf("money movement approval bundle is missing")
	}
	keys, err := e.approvalSigners.PublicKeysForTenant(e.tenantID)
	if err != nil {
		return crypto.ApprovalBundle{}, "", err
	}
	now := time.Now
	if e.now != nil {
		now = e.now
	}
	verified, err := crypto.VerifyApprovalBundle(bundle, keys, now().UTC())
	if err != nil {
		return crypto.ApprovalBundle{}, "", err
	}
	if verified.Intent.Purpose != crypto.SigningPurposeWithdrawal ||
		verified.Intent.BasisType != withdrawal_request.DocumentType ||
		verified.Intent.BasisID != task.ApprovalRequestID() ||
		verified.Intent.TenantID != e.tenantID ||
		verified.Intent.TokenID != task.TokenID ||
		verified.Intent.NetworkCode != task.NetworkCode ||
		verified.Intent.TokenStandard != string(task.TokenStandard) ||
		verified.Intent.ContractAddress != task.ContractAddress ||
		!verified.Intent.AuthorizesSource(task.SigningBasis(), task.RequestID, task.AMLSource.Address, task.AMLSource.VaultKeyID, task.Amount) ||
		verified.Intent.DestinationAddress != task.DestAddress {
		return crypto.ApprovalBundle{}, "", fmt.Errorf("money movement approval intent does not match withdrawal task")
	}
	return bundle, verified.BundleDigest, nil
}

func (e *WithdrawalExecutor) recoverSignedWithdrawal(ctx context.Context, task WithdrawalTask, attempt SigningAuthorizationRecord) error {
	if attempt.SignedTx == nil {
		return holdWithdrawalRecovery("signing_outcome_unknown", fmt.Errorf("withdrawal authorization exists without a signed envelope; signing outcome must be reconciled"))
	}
	if err := ValidateSignedEnvelope(attempt.Authorization, attempt.UnsignedTx, *attempt.SignedTx); err != nil {
		return HoldExecution(err)
	}
	auth := attempt.Authorization
	if auth.BasisType != task.SigningBasis() || auth.BasisID != task.RequestID ||
		auth.TenantID != e.tenantID || auth.NetworkID != task.NetworkID ||
		auth.MerchantID == nil || *auth.MerchantID != task.MerchantID ||
		auth.ContractAddress != task.ContractAddress || auth.AssetStandard != string(task.TokenStandard) ||
		auth.DestinationAddress != task.DestAddress || !auth.AmountMinorUnits.Equal(task.Amount) {
		return HoldExecution(fmt.Errorf("withdrawal recovery identity mismatch"))
	}
	driver, err := e.drivers.Resolve(task.NetworkID)
	if err != nil {
		return HoldExecution(err)
	}
	receipt, err := driver.Finality().Receipt(ctx, attempt.SignedTx.TxHash)
	if !id.IsNil(task.AMLParentID) && (err != nil || receipt.Status == crypto.ReceiptPending || !receipt.FinanciallyFinal()) {
		return e.rebroadcastAMLWithdrawal(ctx, task, driver, attempt)
	}
	if err != nil {
		return holdWithdrawalRecovery("receipt_unavailable", fmt.Errorf("withdrawal recovery receipt unavailable: %w", err))
	}
	if receipt.TxHash != attempt.SignedTx.TxHash || receipt.NetworkID != task.NetworkID || !receipt.FinanciallyFinal() {
		return holdWithdrawalRecovery("awaiting_finality", fmt.Errorf("withdrawal recovery awaits matching financial finality"))
	}
	if e.feeBudget == nil {
		return HoldExecution(fmt.Errorf("withdrawal recovery requires fee budget"))
	}
	fee, found, err := e.feeBudget.Get(ctx, task.SigningBasis(), task.RequestID, crypto.SigningPurposeWithdrawal, auth.Attempt)
	if err != nil {
		return HoldExecution(err)
	}
	if !found {
		return HoldExecution(fmt.Errorf("withdrawal recovery fee reservation missing"))
	}
	return e.completeWithdrawalBroadcast(ctx, task, attempt.Authorization, fee.ID, attempt.SignedTx.TxHash)
}

// Initial execution and receipt recovery share the same locked completion path.
func (e *WithdrawalExecutor) completeWithdrawalBroadcast(ctx context.Context, task WithdrawalTask, auth crypto.SigningAuthorization, feeID id.ID, txHash string) error {
	return e.txm.RunInTransaction(ctx, func(ctx context.Context) error {
		state, err := e.outboundRepo.LockWithdrawalCompletion(ctx, task)
		if err != nil {
			return HoldExecution(err)
		}
		aml := !id.IsNil(task.AMLParentID)
		if state.Status == "broadcast" || state.Status == "confirmed" || state.Status == "failed" {
			return nil
		}
		if state.DocumentID == nil || (aml && state.Status == "cancelled") || (!aml && state.Status != "signing" && state.Status != "held") {
			return HoldExecution(fmt.Errorf("withdrawal recovery has no executable document"))
		}
		doc, err := e.withdrawalRepo.GetByID(ctx, *state.DocumentID)
		if err != nil {
			return HoldExecution(err)
		}
		if doc.IsPosted() {
			return HoldExecution(fmt.Errorf("withdrawal recovery found inconsistent posted document"))
		}
		if err := e.feeBudget.MarkBroadcast(ctx, feeID, auth.UnsignedFingerprint, txHash); err != nil {
			return HoldExecution(err)
		}
		doc.Status, doc.TxHash, doc.SettlementStatus = crypto_withdrawal.WithdrawalStatusBroadcast, txHash, outbound_settlement.StatusUnknown
		if err := e.postingEngine.Post(ctx, doc, func(ctx context.Context) error {
			if err := e.withdrawalRepo.Update(ctx, doc); err != nil {
				return err
			}
			if aml {
				return e.outboundRepo.LinkAMLWithdrawalBroadcast(ctx, task, auth.AuthorizationID, txHash)
			}
			if err := e.outboundRepo.LinkWithdrawalBroadcast(ctx, task.RequestID, doc.ID); err != nil {
				return err
			}
			return e.outboundRepo.MarkSigningAuthorizationBroadcast(ctx, auth.AuthorizationID, doc.TxHash)
		}); err != nil {
			return HoldExecution(err)
		}
		return nil
	})
}
func (e *WithdrawalExecutor) rebroadcastAMLWithdrawal(ctx context.Context, task WithdrawalTask, driver crypto.NetworkDriver, record SigningAuthorizationRecord) error {
	if e.feeBudget == nil || record.SignedTx == nil {
		return HoldExecution(fmt.Errorf("AML signed recovery requires its fee budget and saved envelope"))
	}
	fee, found, err := e.feeBudget.Get(ctx, task.SigningBasis(), task.RequestID, crypto.SigningPurposeWithdrawal, record.Authorization.Attempt)
	if err != nil {
		return err
	}
	if !found {
		return HoldExecution(fmt.Errorf("AML signed recovery has no fee reservation"))
	}
	if err := e.authorizations.CheckCustodyDispatch(ctx, record.Authorization, e.signers); err != nil {
		return HoldExecution(err)
	}
	outcome, err := driver.Broadcaster().Broadcast(ctx, *record.SignedTx)
	if e.observer != nil {
		e.observer.Broadcast(ctx, driver.Network(), crypto.SigningPurposeWithdrawal, outcome)
	}
	if (outcome.Status == crypto.BroadcastAccepted || outcome.Status == crypto.BroadcastAlreadyKnown || trackableAmbiguousBroadcast(driver.Network(), outcome, record.SignedTx.TxHash)) && outcome.TxHash == record.SignedTx.TxHash {
		return e.completeWithdrawalBroadcast(ctx, task, record.Authorization, fee.ID, record.SignedTx.TxHash)
	}
	if err == nil {
		err = fmt.Errorf("saved AML transaction awaits an unambiguous broadcast or final receipt")
	}
	_ = e.outboundRepo.MarkSigningAuthorizationHeld(ctx, record.Authorization.AuthorizationID, record.SignedTx.TxHash)
	_ = e.feeBudget.Hold(ctx, fee.ID, statusFailedReason(err))
	return HoldExecution(err)
}
