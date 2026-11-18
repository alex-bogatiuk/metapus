package outboundapp

import (
	"context"
	"fmt"
	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/outbound_settlement"
	"metapus/internal/domain/documents/treasury_withdrawal"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/registers/crypto_platform_cost"
	"metapus/pkg/logger"
	"time"
)

type TreasuryExecutor struct {
	drivers           *crypto.ChainDriverRegistry
	signers           *crypto.FamilySignerRegistry
	treasuryRepo      treasury_withdrawal.Repository
	outboundRepo      TreasuryRepository
	approvalRepo      ApprovalBundles
	postingEngine     *posting.Engine
	numerator         numerator.Generator
	resourcePreparers ResourcePreparerResolver
	feeBudget         FeeBudget
	approvalSigners   crypto.MoneyMovementApprovalSignerProvider
	authorizations    Authorizations
	tenantID          string
	observer          Observer
	now               func() time.Time
	log               *logger.Logger
}

func NewTreasuryExecutor(
	drivers *crypto.ChainDriverRegistry,
	signers *crypto.FamilySignerRegistry,
	treasuryRepo treasury_withdrawal.Repository,
	outboundRepo TreasuryRepository,
	approvalRepo ApprovalBundles,
	postingEngine *posting.Engine,
	num numerator.Generator,
	resourcePreparers ResourcePreparerResolver,
	feeBudget FeeBudget,
	approvalSigners crypto.MoneyMovementApprovalSignerProvider,
	authorizations Authorizations,
	tenantID string,
	log *logger.Logger,
	observer Observer,
	now func() time.Time,
) *TreasuryExecutor {
	if now == nil {
		now = time.Now
	}
	if log == nil {
		log = logger.Default()
	}
	return &TreasuryExecutor{
		drivers:           drivers,
		signers:           signers,
		treasuryRepo:      treasuryRepo,
		outboundRepo:      outboundRepo,
		approvalRepo:      approvalRepo,
		postingEngine:     postingEngine,
		numerator:         num,
		resourcePreparers: resourcePreparers,
		feeBudget:         feeBudget,
		approvalSigners:   approvalSigners,
		authorizations:    authorizations,
		tenantID:          tenantID,
		observer:          observer, now: now,
		log: log.WithComponent("crypto_treasury_executor"),
	}
}

func (e *TreasuryExecutor) Execute(ctx context.Context, task TreasuryTask) (err error) {
	if e.observer != nil {
		var finish func(error)
		ctx, finish = e.observer.Start(ctx, "crypto.treasury.execute", task.NetworkID, crypto.SigningPurposeTreasury)
		defer func() { finish(err) }()
	}
	if e.treasuryRepo != nil {
		doc, err := e.treasuryRepo.GetByID(ctx, task.WithdrawalID)
		if err != nil {
			return err
		}
		if doc.Posted {
			return nil
		}
	}

	hotWallet, auth, ok, err := e.outboundRepo.GetTreasurySigningAttempt(ctx, task.WithdrawalID)
	if err != nil {
		return fmt.Errorf("load treasury signing attempt: %w", err)
	}
	if ok {
		driver, driverErr := e.drivers.Resolve(auth.Authorization.NetworkID)
		if driverErr != nil {
			return HoldExecution(driverErr)
		}
		if observedSavedOutflow(ctx, driver, auth) {
			fee, found, err := e.feeBudget.Get(ctx, treasury_withdrawal.DocumentType, task.WithdrawalID, crypto.SigningPurposeTreasury, auth.Authorization.Attempt)
			if err != nil {
				return HoldExecution(err)
			}
			if !found {
				return HoldExecution(fmt.Errorf("observed treasury withdrawal has no fee reservation"))
			}
			return e.completeTreasuryBroadcast(ctx, task, auth, fee, id.ID{}, auth.SignedTx.TxHash)
		}
	}
	if task.RecoveryOnly {
		return HoldExecution(fmt.Errorf("held signed treasury withdrawal awaits matching financial finality"))
	}
	if err := e.outboundRepo.ValidateExternalWithdrawalDestination(ctx, task.NetworkID, task.DestAddress); err != nil {
		_ = e.outboundRepo.MarkTreasuryHeld(ctx, task.WithdrawalID, statusFailedReason(err))
		return HoldExecution(err)
	}
	if !ok {
		if err := e.outboundRepo.ReserveFeeForTreasury(ctx, task.WithdrawalID, task.TokenID, task.Amount); err != nil {
			return fmt.Errorf("reserve treasury platform revenue: %w", err)
		}

		hotWallet, err = e.outboundRepo.ReserveHotWalletForTreasury(ctx, task)
		if err != nil {
			return fmt.Errorf("reserve treasury hot wallet: %w", err)
		}
	}
	approvalBundle, approvalDigest, err := e.loadAndVerifyApprovalBundle(ctx, task, hotWallet)
	if err != nil {
		_ = e.outboundRepo.MarkTreasuryHeld(ctx, task.WithdrawalID, statusFailedReason(err))
		return HoldExecution(err)
	}
	if ok && auth.Authorization.ApprovalBundleDigest != "" && auth.Authorization.ApprovalBundleDigest != approvalDigest {
		cause := fmt.Errorf("stored treasury signing authorization approval bundle digest mismatch")
		_ = e.outboundRepo.MarkTreasuryHeld(ctx, task.WithdrawalID, statusFailedReason(cause))
		return HoldExecution(cause)
	}
	if ok && auth.ApprovalBundle.IntentJWS == "" {
		auth.ApprovalBundle = approvalBundle
		auth.Authorization.ApprovalBundleDigest = approvalDigest
	}
	driver, err := e.drivers.Resolve(task.NetworkID)
	if err != nil {
		return HoldExecution(fmt.Errorf("resolve treasury network driver: %w", err))
	}
	network := driver.Network()
	intent := crypto.TransferIntent{
		BasisType: treasury_withdrawal.DocumentType, Purpose: crypto.SigningPurposeTreasury,
		BasisID: task.WithdrawalID, Attempt: 1, Network: network,
		Asset:          crypto.AssetRef{TokenID: task.TokenID, Standard: task.TokenStandard, ContractAddress: task.ContractAddress, Decimals: task.TokenDecimals},
		SourceWalletID: hotWallet.ID, SourceAddress: hotWallet.Address,
		DestinationAddress: task.DestAddress, Amount: task.Amount,
	}
	if _, err := e.authorizations.PrepareTransferCustody(ctx, intent, nil); err != nil {
		return HoldExecution(err)
	}
	if e.feeBudget == nil {
		return HoldExecution(fmt.Errorf("fee budget manager is not configured"))
	}
	policy, err := e.feeBudget.LoadPolicy(ctx, network.ID)
	if err != nil {
		return HoldExecution(fmt.Errorf("load treasury network policy: %w", err))
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
			return HoldExecution(fmt.Errorf("quote treasury fee: %w", err))
		}
		quote, err = policy.BoundFeeQuote(quote)
		if err != nil {
			return HoldExecution(fmt.Errorf("apply treasury fee policy: %w", err))
		}
	} else {
		var found bool
		feeReservation, found, err = e.feeBudget.Get(ctx, intent.BasisType, intent.BasisID, intent.Purpose, intent.Attempt)
		if err != nil {
			return HoldExecution(fmt.Errorf("load persisted treasury fee reservation: %w", err))
		}
		if !found {
			return HoldExecution(fmt.Errorf("persisted treasury signing attempt has no fee reservation"))
		}
		if feeReservation.Status == crypto.FeeReservationHeld {
			return HoldExecution(fmt.Errorf("%w: %s", ErrFeeReservationHeld, feeReservation.HoldReason))
		}
	}

	var resource ResourceReservationResult
	if !ok {
		providerFailureThreshold, providerOpenDuration := resourceCircuitValues(policy)
		resourceRequest := ResourceRequest{
			TenantID: e.tenantID, BasisType: treasury_withdrawal.DocumentType, BasisID: task.WithdrawalID,
			Purpose: crypto_platform_cost.PlatformCostPurposeTreasury, NetworkID: task.NetworkID,
			OperationTokenID: task.TokenID, SourceWalletID: hotWallet.ID,
			SourceAddress: hotWallet.Address, ContractAddress: task.ContractAddress,
			DestinationAddress: task.DestAddress, Amount: task.Amount,
			Requirements:             quote.ResourceRequirements,
			ProviderFailureThreshold: providerFailureThreshold,
			ProviderOpenDuration:     providerOpenDuration,
		}
		var strategy crypto.FeeStrategy
		resource, strategy, err = prepareObservedResource(ctx, e.observer, resolveResourcePreparer(e.resourcePreparers, network.Family), policy, len(quote.ResourceRequirements) > 0, resourceRequest)
		if err != nil {
			return HoldExecution(fmt.Errorf("prepare treasury chain resources: %w", err))
		}
		feeReservation, err = e.feeBudget.Reserve(ctx, FeeReservationRequest{
			NetworkID: network.ID, PayerWalletID: hotWallet.ID, NativeTokenID: network.NativeTokenID, OperationTokenID: task.TokenID,
			BasisType: intent.BasisType, BasisID: intent.BasisID, Purpose: intent.Purpose, Attempt: intent.Attempt,
			Strategy: strategy, MaxFee: quote.MaxFee,
		})
		if err != nil {
			_ = releasePreparedResource(ctx, resolveResourcePreparer(e.resourcePreparers, network.Family), resource.AllocationID, err.Error())
			return HoldExecution(fmt.Errorf("reserve treasury network fee: %w", err))
		}
	}

	if !ok {
		unsigned, err := driver.Transactions().Build(ctx, intent, quote)
		if err != nil {
			reclaimUnsignedSequence(ctx, driver, intent)
			_ = releasePreparedResource(ctx, resolveResourcePreparer(e.resourcePreparers, network.Family), resource.AllocationID, err.Error())
			_ = e.feeBudget.Release(ctx, feeReservation.ID, err.Error())
			return fmt.Errorf("build treasury transaction: %w", err)
		}
		if err := e.feeBudget.BindUnsignedFingerprint(ctx, feeReservation.ID, unsigned.UnsignedFingerprint); err != nil {
			reclaimUnsignedSequence(ctx, driver, intent)
			_ = releasePreparedResource(ctx, resolveResourcePreparer(e.resourcePreparers, network.Family), resource.AllocationID, err.Error())
			_ = e.feeBudget.Hold(ctx, feeReservation.ID, err.Error())
			return HoldExecution(err)
		}
		auth, err = e.authorizations.Create(ctx, SigningAttemptInput{
			Intent: intent, Capabilities: driver.Capabilities(), VaultKeyID: hotWallet.VaultKeyID, FeePayerAddress: hotWallet.Address,
			MaxNetworkFee: quote.MaxFee, Unsigned: unsigned, ApprovalBundle: approvalBundle,
		})
		if err != nil {
			reclaimUnsignedSequence(ctx, driver, intent)
			_ = releasePreparedResource(ctx, resolveResourcePreparer(e.resourcePreparers, network.Family), resource.AllocationID, err.Error())
			_ = e.feeBudget.Hold(ctx, feeReservation.ID, err.Error())
			return HoldExecution(fmt.Errorf("create treasury signing authorization: %w", err))
		}
	}

	signedTx, err := e.authorizations.SignOrReuse(ctx, familySigner, auth)
	if err != nil {
		_ = releasePreparedResource(ctx, resolveResourcePreparer(e.resourcePreparers, network.Family), resource.AllocationID, err.Error())
		_ = e.feeBudget.Hold(ctx, feeReservation.ID, err.Error())
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
		return e.hold(ctx, task.WithdrawalID, auth, signedTx.TxHash, fmt.Errorf("broadcast treasury tx result is uncertain: %w", err))
	}

	switch outcome.Status {
	case crypto.BroadcastAccepted, crypto.BroadcastAlreadyKnown, crypto.BroadcastAmbiguous:
		if outcome.Status == crypto.BroadcastAmbiguous && !trackableAmbiguous {
			_ = e.feeBudget.Hold(ctx, feeReservation.ID, outcome.Reason)
			return e.hold(ctx, task.WithdrawalID, auth, signedTx.TxHash, fmt.Errorf("broadcast treasury tx result is uncertain: %s", outcome.Reason))
		}
		if outcome.TxHash == "" || outcome.TxHash != signedTx.TxHash {
			return HoldExecution(fmt.Errorf("broadcast treasury tx returned empty tx hash"))
		}
	case crypto.BroadcastRejected:
		if err := e.outboundRepo.MarkSigningAuthorizationFailed(ctx, auth.Authorization.AuthorizationID, signedTx.TxHash); err != nil {
			return HoldExecution(fmt.Errorf("record rejected treasury tx: %w", err))
		}
		_ = releasePreparedResource(ctx, resolveResourcePreparer(e.resourcePreparers, network.Family), resource.AllocationID, outcome.Reason)
		_ = e.feeBudget.Release(ctx, feeReservation.ID, outcome.Reason)
		return fmt.Errorf("broadcast treasury tx rejected: %s", outcome.Reason)
	default:
		_ = e.feeBudget.Hold(ctx, feeReservation.ID, outcome.Reason)
		return e.hold(ctx, task.WithdrawalID, auth, signedTx.TxHash, fmt.Errorf("broadcast treasury tx result is uncertain: %s", outcome.Reason))
	}

	txHash := outcome.TxHash
	return e.completeTreasuryBroadcast(ctx, task, auth, feeReservation, resource.AllocationID, txHash)
}

func (e *TreasuryExecutor) completeTreasuryBroadcast(ctx context.Context, task TreasuryTask, auth SigningAuthorizationRecord, fee crypto.FeeReservation, allocationID id.ID, txHash string) error {
	doc, err := e.treasuryRepo.GetByID(ctx, task.WithdrawalID)
	if err != nil {
		return HoldExecution(fmt.Errorf("load treasury withdrawal after broadcast: %w", err))
	}
	if doc.Posted {
		if doc.TxHash != txHash {
			return HoldExecution(fmt.Errorf("treasury broadcast replay has a different transaction"))
		}
		return nil
	}
	if err := e.feeBudget.MarkBroadcast(ctx, fee.ID, auth.Authorization.UnsignedFingerprint, txHash); err != nil {
		return HoldExecution(fmt.Errorf("record treasury fee reservation broadcast: %w", err))
	}
	if err := markPreparedResourceBroadcast(ctx, resolveResourcePreparer(e.resourcePreparers, auth.Authorization.ChainFamily), ResourceBroadcastCommand{
		AllocationID: allocationID,
		BasisType:    treasury_withdrawal.DocumentType,
		BasisID:      task.WithdrawalID,
		TxHash:       txHash,
	}); err != nil {
		return HoldExecution(fmt.Errorf("record treasury resource allocation broadcast: %w", err))
	}
	doc.Status = treasury_withdrawal.StatusBroadcast
	doc.TxHash = txHash
	doc.SettlementStatus = outbound_settlement.StatusUnknown
	doc.FeeLines, err = e.outboundRepo.LoadTreasuryFeeReservationLines(ctx, task.WithdrawalID)
	if err != nil {
		return HoldExecution(fmt.Errorf("load treasury fee lines after broadcast: %w", err))
	}
	// A receipt refund spends no revenue: its money left suspense on approval.
	if len(doc.FeeLines) == 0 && !doc.IsReceiptRefund() {
		return HoldExecution(fmt.Errorf("treasury withdrawal has no fee reservation lines"))
	}
	if doc.Number == "" {
		number, err := e.numerator.GetNextNumber(ctx, numerator.DocumentSeries(treasury_withdrawal.Definition), e.now())
		if err != nil {
			return fmt.Errorf("generate treasury withdrawal number: %w", err)
		}
		doc.Number = number
	}

	if err := e.postingEngine.Post(ctx, doc, func(txCtx context.Context) error {
		if err := e.treasuryRepo.Update(txCtx, doc); err != nil {
			return fmt.Errorf("update treasury withdrawal: %w", err)
		}
		if err := e.outboundRepo.MarkSigningAuthorizationBroadcast(txCtx, auth.Authorization.AuthorizationID, txHash); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return e.hold(ctx, task.WithdrawalID, auth, txHash, fmt.Errorf("post treasury withdrawal after broadcast: %w", err))
	}

	e.log.Infow("treasury withdrawal broadcast",
		"withdrawal_id", task.WithdrawalID,
		"tx_hash", txHash,
		"authorization_id", auth.Authorization.AuthorizationID,
		"basis_id", auth.Authorization.BasisID,
		"tx_id", txHash,
		"amount", auth.Authorization.AmountMinorUnits,
		"policy_version", auth.Authorization.PolicyVersion,
	)
	return nil
}

func (e *TreasuryExecutor) loadAndVerifyApprovalBundle(ctx context.Context, task TreasuryTask, hotWallet HotWallet) (crypto.ApprovalBundle, string, error) {
	if e.approvalRepo == nil {
		return crypto.ApprovalBundle{}, "", fmt.Errorf("money movement approval repo is not configured")
	}
	if e.approvalSigners == nil {
		return crypto.ApprovalBundle{}, "", fmt.Errorf("money movement approval verifier is not configured")
	}
	bundle, ok, err := e.approvalRepo.LoadBundle(ctx, treasury_withdrawal.DocumentType, task.WithdrawalID)
	if err != nil {
		return crypto.ApprovalBundle{}, "", err
	}
	if !ok {
		return crypto.ApprovalBundle{}, "", fmt.Errorf("treasury money movement approval bundle is missing")
	}
	keys, err := e.approvalSigners.PublicKeysForTenant(e.tenantID)
	if err != nil {
		return crypto.ApprovalBundle{}, "", err
	}
	verified, err := crypto.VerifyApprovalBundle(bundle, keys, e.now().UTC())
	if err != nil {
		return crypto.ApprovalBundle{}, "", err
	}
	if verified.Intent.Purpose != crypto.SigningPurposeTreasury ||
		verified.Intent.BasisType != treasury_withdrawal.DocumentType ||
		verified.Intent.BasisID != task.WithdrawalID ||
		verified.Intent.TenantID != e.tenantID ||
		verified.Intent.TokenID != task.TokenID ||
		verified.Intent.NetworkCode != task.NetworkCode ||
		verified.Intent.TokenStandard != string(task.TokenStandard) ||
		verified.Intent.ContractAddress != task.ContractAddress ||
		!verified.Intent.AmountMinorUnits.Equal(task.Amount) ||
		verified.Intent.DestinationAddress != task.DestAddress ||
		verified.Intent.SourceAddress != hotWallet.Address ||
		verified.Intent.SourceVaultKeyID != hotWallet.VaultKeyID {
		return crypto.ApprovalBundle{}, "", fmt.Errorf("money movement approval intent does not match treasury task")
	}
	return bundle, verified.BundleDigest, nil
}

func (e *TreasuryExecutor) hold(ctx context.Context, withdrawalID id.ID, auth SigningAuthorizationRecord, txID string, cause error) error {
	if err := e.outboundRepo.MarkSigningAuthorizationHeld(ctx, auth.Authorization.AuthorizationID, txID); err != nil {
		return HoldExecution(fmt.Errorf("%w; mark authorization held: %v", cause, err))
	}
	if err := e.outboundRepo.MarkTreasuryHeld(ctx, withdrawalID, statusFailedReason(cause)); err != nil {
		return HoldExecution(fmt.Errorf("%w; mark treasury held: %v; withdrawal_id=%s", cause, err, withdrawalID.String()))
	}
	return HoldExecution(cause)
}
