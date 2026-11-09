package outboundapp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/numerator"
	"metapus/internal/core/tenant"
	"metapus/internal/core/types"
	"metapus/internal/domain/catalogs/token"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_sweep"
	"metapus/internal/domain/documents/outbound_settlement"
	"metapus/internal/domain/posting"
	"metapus/pkg/logger"
)

type SweepExecutor struct {
	drivers           *crypto.ChainDriverRegistry
	signers           *crypto.FamilySignerRegistry
	sweepRepo         crypto_sweep.Repository
	outboundRepo      SweepRepository
	postingEngine     *posting.Engine
	numerator         numerator.Generator
	resourcePreparers ResourcePreparerResolver
	feeBudget         FeeBudget
	nativeFunding     NativeFunding
	authorizations    Authorizations
	tenantID          string
	observer          Observer
	now               func() time.Time
	log               *logger.Logger
}

type SweepConfig struct {
	Drivers        *crypto.ChainDriverRegistry
	Signers        *crypto.FamilySignerRegistry
	Documents      crypto_sweep.Repository
	Repository     SweepRepository
	Posting        *posting.Engine
	Numerator      numerator.Generator
	Resources      ResourcePreparerResolver
	Fees           FeeBudget
	Funding        NativeFunding
	Authorizations Authorizations
	TenantID       string
	Logger         *logger.Logger
	Observer       Observer
	Now            func() time.Time
}

func NewSweepExecutor(c SweepConfig) *SweepExecutor {
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Logger == nil {
		c.Logger = logger.Default()
	}
	return &SweepExecutor{drivers: c.Drivers, signers: c.Signers, sweepRepo: c.Documents, outboundRepo: c.Repository, postingEngine: c.Posting, numerator: c.Numerator, resourcePreparers: c.Resources, feeBudget: c.Fees, nativeFunding: c.Funding, authorizations: c.Authorizations, tenantID: c.TenantID, log: c.Logger, observer: c.Observer, now: c.Now}
}

const _sweepAttemptBasisType = "CryptoSweepAttempt"
const _sweepLineBasisType = "CryptoSweepLine"

func (e *SweepExecutor) ExecuteGroup(ctx context.Context, group []SweepTask) error {
	if len(group) == 0 {
		return nil
	}

	var doc *crypto_sweep.CryptoSweep
	walletIDs := sweepGroupWalletIDs(group)
	releaseClaims := func(cause error) error {
		if doc != nil {
			_ = e.outboundRepo.MarkSweepExecutionStatus(ctx, doc.ID, string(crypto_sweep.SweepStatusPartialFailed))
		}
		for _, attemptID := range SweepGroupAttemptIDs(group) {
			_ = e.outboundRepo.MarkSweepAttemptFailed(ctx, attemptID, cause.Error())
		}
		if err := e.outboundRepo.ReleaseSweepClaims(ctx, walletIDs); err != nil {
			return fmt.Errorf("%w; release sweep claims: %v", cause, err)
		}
		return cause
	}

	total := types.ZeroCryptoAmount()
	for _, task := range group {
		total = total.Add(task.Balance)
	}

	hotWallet := group[0].HotWallet()
	attemptIDs := SweepGroupAttemptIDs(group)
	var candidate *crypto_sweep.CryptoSweep
	if group[0].SweepID == nil {
		candidate = crypto_sweep.NewCryptoSweep(group[0].TokenID, hotWallet.ID)
		candidate.TotalAmount = total
		candidate.TotalFee = types.ZeroCryptoAmount()
		placeholderLines := make([]crypto_sweep.CryptoSweepLine, 0, len(group))
		for i, task := range group {
			placeholderLines = append(placeholderLines, crypto_sweep.CryptoSweepLine{
				LineID: task.AttemptID, LineNo: i + 1, WalletID: task.WalletID,
				Amount: task.Balance, NetworkFee: types.ZeroCryptoAmount(),
				SettlementStatus: outbound_settlement.StatusNotApplicable,
			})
		}
		candidate.SetLines(placeholderLines)
		number, err := e.numerator.GetNextNumber(ctx, numerator.DefaultConfig("CS"), nil, e.now())
		if err != nil {
			return releaseClaims(fmt.Errorf("generate sweep number: %w", err))
		}
		candidate.Number = number
	}
	var err error
	doc, err = e.outboundRepo.EnsureSweepExecutionDocument(ctx, attemptIDs, candidate, e.sweepRepo)
	if err != nil {
		return releaseClaims(fmt.Errorf("ensure sweep execution draft: %w", err))
	}
	for i := range group {
		group[i].SweepID = new(doc.ID)
		group[i].AccountingModelVersion = doc.AccountingModelVersion
	}
	lines := make([]crypto_sweep.CryptoSweepLine, 0, len(group))
	platformCosts := make([]crypto_sweep.CryptoSweepPlatformCostLine, 0, len(group))
	postedAttemptIDs := make([]id.ID, 0, len(group))
	broadcastTotal := types.ZeroCryptoAmount()
	finalLines := make(map[id.ID]bool)
	if doc.AccountingModelVersion == 2 {
		for _, line := range doc.Lines {
			finalLines[line.LineID] = line.SettlementStatus == outbound_settlement.StatusRecorded
		}
	}
	broadcasted := false
	for i, task := range group {
		if finalLines[task.AttemptID] {
			continue
		}
		result, err := e.BroadcastLine(ctx, task, hotWallet, doc.ID)
		if err != nil {
			if errors.Is(err, ErrNativeFundingPending) {
				// Keep the attempt claimable. The same funding authorization and
				// signed envelope will be resumed after finality is observable.
				return err
			}
			if IsHeldExecutionError(err) {
				_ = e.outboundRepo.MarkSweepAttemptHeld(ctx, task.AttemptID, err.Error())
				_ = e.outboundRepo.MarkSweepExecutionStatus(ctx, doc.ID, string(crypto_sweep.SweepStatusHeld))
			}
			if !IsHeldExecutionError(err) {
				_ = e.outboundRepo.MarkSweepAttemptFailed(ctx, task.AttemptID, err.Error())
			}
			if !broadcasted && !IsHeldExecutionError(err) {
				return releaseClaims(err)
			}
			if !IsHeldExecutionError(err) {
				_ = e.outboundRepo.ReleaseSweepClaims(ctx, []id.ID{task.WalletID})
			}
			return err
		}
		broadcasted = true
		broadcastTotal = broadcastTotal.Add(result.Amount)
		postedAttemptIDs = append(postedAttemptIDs, task.AttemptID)
		if err := e.outboundRepo.RecordSweepLineBroadcast(ctx, doc.ID, task.AttemptID, result.TxHash, result.Amount); err != nil {
			_ = e.outboundRepo.MarkSweepExecutionStatus(ctx, doc.ID, string(crypto_sweep.SweepStatusHeld))
			return HoldExecution(err)
		}
		networkFee := types.ZeroCryptoAmount()
		if result.CostLine != nil {
			networkFee = result.CostLine.Amount
			platformCosts = append(platformCosts, crypto_sweep.CryptoSweepPlatformCostLine{
				NetworkID:       result.CostLine.NetworkID,
				CostTokenID:     result.CostLine.CostTokenID,
				CostType:        result.CostLine.CostType,
				Provider:        result.CostLine.Provider,
				ExternalOrderID: result.CostLine.ExternalOrderID,
				Amount:          result.CostLine.Amount,
			})
		}
		lines = append(lines, crypto_sweep.CryptoSweepLine{
			LineID:           result.LineID,
			LineNo:           i + 1,
			WalletID:         task.WalletID,
			Amount:           result.Amount,
			NetworkFee:       networkFee,
			TxHash:           result.TxHash,
			Confirmed:        false,
			SettlementStatus: outbound_settlement.StatusUnknown,
		})
	}
	doc.SetLines(lines)
	doc.Status = crypto_sweep.SweepStatusBroadcast
	doc.TotalAmount = broadcastTotal
	doc.PlatformCostLines = platformCosts
	for _, cost := range platformCosts {
		doc.TotalFee = doc.TotalFee.Add(cost.Amount)
	}
	if doc.AccountingModelVersion == 2 {
		doc.TotalFee = types.ZeroCryptoAmount()
	}

	// A v2 broadcast may already have its cost snapshot, while a poller has
	// not finished principal yet. Resuming it must never repost prior lines.
	if doc.AccountingModelVersion == 2 && doc.Posted {
		return nil
	}
	if err := e.postingEngine.Post(ctx, doc, func(txCtx context.Context) error {
		if err := e.sweepRepo.Update(txCtx, doc); err != nil {
			return fmt.Errorf("update crypto sweep: %w", err)
		}
		if doc.AccountingModelVersion == 2 {
			// Broadcast lines were persisted individually. Never delete/rewrite
			// another poller's final settlement or release the source at broadcast.
			return nil
		}
		if err := e.sweepRepo.SaveLines(txCtx, doc.ID, doc.GetLines()); err != nil {
			return fmt.Errorf("save crypto sweep lines: %w", err)
		}
		return cryptoapp.NewSweepLifecycleService(tenant.ContextTxManager{}, e.outboundRepo).
			Complete(txCtx, walletIDs, postedAttemptIDs)
	}); err != nil {
		return HoldExecution(fmt.Errorf("post crypto sweep after broadcast: %w", err))
	}

	e.log.Infow("sweep broadcast",
		"sweep_id", doc.ID,
		"token_id", doc.TokenID,
		"wallet_count", len(lines),
		"total_amount", doc.TotalAmount.String(),
	)
	return nil
}

type SweepLineBroadcastResult struct {
	TxHash   string
	LineID   id.ID
	Amount   types.CryptoAmount
	CostLine *SweepPlatformCostLine
}

func (e *SweepExecutor) BroadcastLine(ctx context.Context, task SweepTask, hotWallet HotWallet, sweepID id.ID) (result SweepLineBroadcastResult, err error) {
	if e.observer != nil {
		var finish func(error)
		ctx, finish = e.observer.Start(ctx, "crypto.sweep.execute", task.NetworkID, crypto.SigningPurposeSweep)
		defer func() { finish(err) }()
	}

	driver, err := e.drivers.Resolve(task.NetworkID)
	if err != nil {
		return SweepLineBroadcastResult{}, HoldExecution(fmt.Errorf("resolve sweep network driver: %w", err))
	}
	auth, ok, err := e.outboundRepo.GetSweepSigningAttempt(ctx, task)
	if err != nil {
		return SweepLineBroadcastResult{}, fmt.Errorf("load sweep signing attempt: %w", err)
	}
	if ok && auth.Status == "failed" {
		return SweepLineBroadcastResult{}, HoldExecution(fmt.Errorf("failed sweep authorization cannot be dispatched again"))
	}
	if ok && auth.Status == "broadcast" {
		if err := e.outboundRepo.MarkSweepAttemptBroadcast(ctx, task.AttemptID); err != nil {
			return SweepLineBroadcastResult{}, HoldExecution(fmt.Errorf("record sweep attempt broadcast: %w", err))
		}
		return SweepLineBroadcastResult{TxHash: auth.TxID, LineID: auth.Authorization.BasisID, Amount: auth.Authorization.AmountMinorUnits}, nil
	}

	if ok && observedSavedOutflow(ctx, driver, auth) {
		fee, found, err := e.feeBudget.Get(ctx, _sweepAttemptBasisType, task.AttemptID, crypto.SigningPurposeSweep, auth.Authorization.Attempt)
		if err != nil {
			return SweepLineBroadcastResult{}, HoldExecution(err)
		}
		if !found {
			return SweepLineBroadcastResult{}, HoldExecution(fmt.Errorf("observed sweep has no fee reservation"))
		}
		txHash := auth.SignedTx.TxHash
		if err := e.recordSweepBroadcast(ctx, task, auth, fee, id.ID{}, txHash); err != nil {
			return SweepLineBroadcastResult{}, HoldExecution(err)
		}
		return SweepLineBroadcastResult{TxHash: txHash, LineID: auth.Authorization.BasisID, Amount: auth.Authorization.AmountMinorUnits}, nil
	}
	if task.RecoveryOnly {
		return SweepLineBroadcastResult{}, HoldExecution(fmt.Errorf("held signed sweep awaits matching financial finality"))
	}
	network := driver.Network()
	intent := crypto.TransferIntent{
		BasisType: _sweepAttemptBasisType, Purpose: crypto.SigningPurposeSweep,
		BasisID: task.AttemptID, Attempt: 1, Network: network,
		Asset:          crypto.AssetRef{TokenID: task.TokenID, Standard: task.TokenStandard, ContractAddress: task.ContractAddress, Decimals: task.TokenDecimals},
		SourceWalletID: task.WalletID, SourceAddress: task.Address,
		DestinationAddress: hotWallet.Address, Amount: task.Balance,
	}
	if _, err := e.authorizations.PrepareTransferCustody(ctx, intent, nil); err != nil {
		_ = e.outboundRepo.MarkSweepAttemptHeld(ctx, task.AttemptID, err.Error())
		return SweepLineBroadcastResult{}, HoldExecution(err)
	}
	if err := e.EnsureSourceAccountActive(ctx, driver, task); err != nil {
		_ = e.outboundRepo.MarkSweepAttemptHeld(ctx, task.AttemptID, err.Error())
		return SweepLineBroadcastResult{}, HoldExecution(err)
	}
	if e.feeBudget == nil {
		return SweepLineBroadcastResult{}, HoldExecution(fmt.Errorf("fee budget manager is not configured"))
	}
	policy, err := e.feeBudget.LoadPolicy(ctx, network.ID)
	if err != nil {
		return SweepLineBroadcastResult{}, HoldExecution(fmt.Errorf("load sweep network policy: %w", err))
	}
	if !policy.Enabled {
		return SweepLineBroadcastResult{}, HoldExecution(ErrNetworkPolicyDisabled)
	}
	var familySigner crypto.PolicyAwareTransactionSigner
	if !ok || auth.SignedTx == nil {
		familySigner, err = e.authorizations.ResolveSigner(ctx, e.signers, network)
		if err != nil {
			return SweepLineBroadcastResult{}, HoldExecution(err)
		}
	}
	var quote crypto.FeeQuote
	var feeReservation crypto.FeeReservation
	if !ok {
		quote, err = driver.Transactions().QuoteFee(ctx, intent)
		if err != nil {
			return SweepLineBroadcastResult{}, HoldExecution(fmt.Errorf("quote sweep fee: %w", err))
		}
		quote, err = policy.BoundFeeQuote(quote)
		if err != nil {
			return SweepLineBroadcastResult{}, HoldExecution(fmt.Errorf("apply sweep fee policy: %w", err))
		}
		if task.TokenStandard == token.TokenStandardNative {
			chainBalance, balanceErr := driver.Balances().NativeBalance(ctx, task.Address)
			if balanceErr != nil {
				return SweepLineBroadcastResult{}, HoldExecution(fmt.Errorf("read native sweep balance: %w", balanceErr))
			}
			available := task.Balance
			if chainBalance.Cmp(available) < 0 {
				available = chainBalance
			}
			reserved := quote.MaxFee.Add(policy.FeeFunding.MinSourceWalletResidual)
			if available.Cmp(reserved) <= 0 {
				return SweepLineBroadcastResult{}, HoldExecution(fmt.Errorf("native sweep balance %s does not cover max fee and residual %s", available, reserved))
			}
			intent.Amount = available.Sub(reserved)
		}
	} else {
		var found bool
		feeReservation, found, err = e.feeBudget.Get(ctx, intent.BasisType, intent.BasisID, intent.Purpose, intent.Attempt)
		if err != nil {
			return SweepLineBroadcastResult{}, HoldExecution(fmt.Errorf("load persisted sweep fee reservation: %w", err))
		}
		if !found {
			return SweepLineBroadcastResult{}, HoldExecution(fmt.Errorf("persisted sweep signing attempt has no fee reservation"))
		}
		if feeReservation.Status == crypto.FeeReservationHeld {
			if IsUnrecoverableSigningError(fmt.Errorf("%s", feeReservation.HoldReason)) ||
				ExpiredUnbroadcastSigningAttempt(auth, e.now().UTC()) ||
				IsUnsignedEnvelopeExpired(auth.UnsignedTx, e.now().UTC()) {
				_ = e.outboundRepo.MarkSigningAuthorizationFailed(ctx, auth.Authorization.AuthorizationID, "")
				_ = e.feeBudget.Release(ctx, feeReservation.ID, "transaction expired before signing")
				_ = e.outboundRepo.CancelSweepAttemptAllocation(ctx, task.AttemptID, "transaction expired before signing", "worker")
				_ = e.outboundRepo.MarkSweepAttemptFailed(ctx, task.AttemptID, "transaction expired before signing")
				return SweepLineBroadcastResult{}, fmt.Errorf("staged sweep authorization expired before signing")
			}
			return SweepLineBroadcastResult{}, HoldExecution(fmt.Errorf("%w: %s", ErrFeeReservationHeld, feeReservation.HoldReason))
		}
		if ExpiredUnbroadcastSigningAttempt(auth, e.now().UTC()) || IsUnsignedEnvelopeExpired(auth.UnsignedTx, e.now().UTC()) {
			_ = e.outboundRepo.MarkSigningAuthorizationFailed(ctx, auth.Authorization.AuthorizationID, "")
			_ = e.feeBudget.Release(ctx, feeReservation.ID, "authorization expired before signing")
			_ = e.outboundRepo.CancelSweepAttemptAllocation(ctx, task.AttemptID, "authorization expired before signing", "worker")
			_ = e.outboundRepo.MarkSweepAttemptFailed(ctx, task.AttemptID, "authorization expired before signing")
			return SweepLineBroadcastResult{}, fmt.Errorf("staged sweep authorization expired before signing")
		}
	}
	var resource ResourceReservationResult
	if !ok {
		providerFailureThreshold, providerOpenDuration := resourceCircuitValues(policy)
		resourceRequest := ResourceRequest{
			TenantID: e.tenantID, BasisType: _sweepLineBasisType, BasisID: task.AttemptID,
			RentalBasisType: crypto_sweep.DocumentType, RentalBasisID: sweepID,
			Purpose: entity.PlatformCostPurposeSweep, NetworkID: task.NetworkID,
			OperationTokenID: task.TokenID, SourceWalletID: task.WalletID,
			SourceAddress: task.Address, ContractAddress: task.ContractAddress,
			DestinationAddress: hotWallet.Address, Amount: intent.Amount,
			Requirements:             quote.ResourceRequirements,
			ProviderFailureThreshold: providerFailureThreshold,
			ProviderOpenDuration:     providerOpenDuration,
		}
		var strategy crypto.FeeStrategy
		needsExternalResource := len(quote.ResourceRequirements) > 0
		resource, strategy, err = prepareObservedResource(ctx, e.observer, resolveResourcePreparer(e.resourcePreparers, network.Family), policy, needsExternalResource, resourceRequest)
		if err != nil {
			_ = e.outboundRepo.MarkSweepAttemptHeld(ctx, task.AttemptID, err.Error())
			return SweepLineBroadcastResult{}, HoldExecution(fmt.Errorf("prepare sweep chain resources: %w", err))
		}
		feeReservation, err = e.feeBudget.Reserve(ctx, FeeReservationRequest{
			NetworkID: network.ID, PayerWalletID: task.WalletID, NativeTokenID: network.NativeTokenID, OperationTokenID: task.TokenID,
			ReservedPrincipal: sweepNativePrincipal(task, intent.Amount),
			BasisType:         intent.BasisType, BasisID: intent.BasisID, Purpose: intent.Purpose, Attempt: intent.Attempt,
			Strategy: strategy, MaxFee: quote.MaxFee,
		})
		if err != nil {
			if errors.Is(err, ErrFeeBalanceResidual) && task.ContractAddress != "" {
				ready, fundingErr := e.nativeFunding.Ensure(ctx, driver, policy, NativeFundingRequest{
					BasisType: _sweepAttemptBasisType,
					BasisID:   task.AttemptID,
					NetworkID: task.NetworkID,
					WalletID:  task.WalletID,
					Address:   task.Address,
				}, familySigner, quote.MaxFee)
				if fundingErr != nil {
					if errors.Is(fundingErr, ErrNativeFundingPending) {
						// The provider lease has already been purchased for this
						// immutable attempt. Keep its reservation while JIT funding
						// finalizes; releasing it would orphan the later chain effect.
						return SweepLineBroadcastResult{}, fundingErr
					}
					_ = releasePreparedResource(ctx, resolveResourcePreparer(e.resourcePreparers, network.Family), resource.AllocationID, fundingErr.Error())
					_ = e.outboundRepo.MarkSweepAttemptHeld(ctx, task.AttemptID, fundingErr.Error())
					return SweepLineBroadcastResult{}, HoldExecution(fundingErr)
				}
				if !ready {
					return SweepLineBroadcastResult{}, ErrNativeFundingPending
				}
				feeReservation, err = e.feeBudget.Reserve(ctx, FeeReservationRequest{
					NetworkID: network.ID, PayerWalletID: task.WalletID, NativeTokenID: network.NativeTokenID, OperationTokenID: task.TokenID,
					BasisType: intent.BasisType, BasisID: intent.BasisID, Purpose: intent.Purpose, Attempt: intent.Attempt,
					Strategy: strategy, MaxFee: quote.MaxFee,
				})
			} else {
				_ = releasePreparedResource(ctx, resolveResourcePreparer(e.resourcePreparers, network.Family), resource.AllocationID, err.Error())
			}
		}
		if err != nil {
			_ = releasePreparedResource(ctx, resolveResourcePreparer(e.resourcePreparers, network.Family), resource.AllocationID, err.Error())
			_ = e.outboundRepo.MarkSweepAttemptHeld(ctx, task.AttemptID, err.Error())
			return SweepLineBroadcastResult{}, HoldExecution(fmt.Errorf("reserve sweep network fee: %w", err))
		}
	}
	var costLine *SweepPlatformCostLine
	if !ok {
		if err := e.outboundRepo.MarkSweepAttemptResourceReady(ctx, task.AttemptID); err != nil {
			return SweepLineBroadcastResult{}, err
		}
		unsigned, err := driver.Transactions().Build(ctx, intent, quote)
		if err != nil {
			reclaimUnsignedSequence(ctx, driver, intent)
			_ = releasePreparedResource(ctx, resolveResourcePreparer(e.resourcePreparers, network.Family), resource.AllocationID, err.Error())
			_ = e.feeBudget.Hold(ctx, feeReservation.ID, err.Error())
			return SweepLineBroadcastResult{}, HoldExecution(err)
		}
		if err := e.feeBudget.BindUnsignedFingerprint(ctx, feeReservation.ID, unsigned.UnsignedFingerprint); err != nil {
			reclaimUnsignedSequence(ctx, driver, intent)
			_ = releasePreparedResource(ctx, resolveResourcePreparer(e.resourcePreparers, network.Family), resource.AllocationID, err.Error())
			_ = e.feeBudget.Hold(ctx, feeReservation.ID, err.Error())
			return SweepLineBroadcastResult{}, HoldExecution(err)
		}
		if err := e.outboundRepo.MarkSweepAttemptSigning(ctx, task.AttemptID); err != nil {
			return SweepLineBroadcastResult{}, err
		}
		auth, err = e.authorizations.Create(ctx, SigningAttemptInput{
			Intent: intent, Capabilities: driver.Capabilities(), VaultKeyID: task.VaultKeyID, FeePayerAddress: task.Address,
			MaxNetworkFee: quote.MaxFee, Unsigned: unsigned,
		})
		if err != nil {
			reclaimUnsignedSequence(ctx, driver, intent)
			_ = releasePreparedResource(ctx, resolveResourcePreparer(e.resourcePreparers, network.Family), resource.AllocationID, err.Error())
			_ = e.feeBudget.Release(ctx, feeReservation.ID, err.Error())
			return SweepLineBroadcastResult{}, err
		}
	}

	signedTx, err := e.authorizations.SignOrReuse(ctx, familySigner, auth)
	if err != nil {
		_ = releasePreparedResource(ctx, resolveResourcePreparer(e.resourcePreparers, network.Family), resource.AllocationID, err.Error())
		if IsUnrecoverableSigningError(err) {
			_ = e.outboundRepo.MarkSigningAuthorizationFailed(ctx, auth.Authorization.AuthorizationID, "")
			_ = e.feeBudget.Release(ctx, feeReservation.ID, err.Error())
			_ = e.outboundRepo.CancelSweepAttemptAllocation(ctx, task.AttemptID, err.Error(), "worker")
			_ = e.outboundRepo.MarkSweepAttemptFailed(ctx, task.AttemptID, err.Error())
			return SweepLineBroadcastResult{}, fmt.Errorf("signing failed unrecoverably: %w", err)
		}
		_ = e.feeBudget.Hold(ctx, feeReservation.ID, err.Error())
		return SweepLineBroadcastResult{}, HoldExecution(err)
	}

	if err := e.authorizations.CheckCustodyDispatch(ctx, auth.Authorization, e.signers); err != nil {
		return SweepLineBroadcastResult{}, HoldExecution(err)
	}
	outcome, err := driver.Broadcaster().Broadcast(ctx, signedTx)
	if e.observer != nil {
		e.observer.Broadcast(ctx, network, intent.Purpose, outcome)
	}
	trackableAmbiguous := trackableAmbiguousBroadcast(network, outcome, signedTx.TxHash)
	if err != nil && !trackableAmbiguous {
		_ = e.feeBudget.Hold(ctx, feeReservation.ID, err.Error())
		if markErr := e.outboundRepo.MarkSigningAuthorizationHeld(ctx, auth.Authorization.AuthorizationID, signedTx.TxHash); markErr != nil {
			return SweepLineBroadcastResult{}, HoldExecution(fmt.Errorf("broadcast sweep tx result is uncertain: %w; mark authorization held: %v", err, markErr))
		}
		_ = e.outboundRepo.MarkSweepAttemptHeld(ctx, task.AttemptID, err.Error())
		return SweepLineBroadcastResult{}, HoldExecution(fmt.Errorf("broadcast sweep tx result is uncertain: %w", err))
	}

	switch outcome.Status {
	case crypto.BroadcastAccepted, crypto.BroadcastAlreadyKnown, crypto.BroadcastAmbiguous:
		if outcome.Status == crypto.BroadcastAmbiguous && !trackableAmbiguous {
			_ = e.feeBudget.Hold(ctx, feeReservation.ID, outcome.Reason)
			if err := e.outboundRepo.MarkSigningAuthorizationHeld(ctx, auth.Authorization.AuthorizationID, signedTx.TxHash); err != nil {
				return SweepLineBroadcastResult{}, HoldExecution(fmt.Errorf("record uncertain sweep tx: %w", err))
			}
			_ = e.outboundRepo.MarkSweepAttemptHeld(ctx, task.AttemptID, outcome.Reason)
			return SweepLineBroadcastResult{}, HoldExecution(fmt.Errorf("broadcast sweep tx result is uncertain: %s", outcome.Reason))
		}
		if outcome.TxHash == "" || outcome.TxHash != signedTx.TxHash {
			return SweepLineBroadcastResult{}, HoldExecution(fmt.Errorf("broadcast sweep tx returned empty tx hash"))
		}
	case crypto.BroadcastRejected:
		if err := e.outboundRepo.MarkSigningAuthorizationFailed(ctx, auth.Authorization.AuthorizationID, signedTx.TxHash); err != nil {
			return SweepLineBroadcastResult{}, HoldExecution(fmt.Errorf("record rejected sweep tx: %w", err))
		}
		_ = releasePreparedResource(ctx, resolveResourcePreparer(e.resourcePreparers, network.Family), resource.AllocationID, outcome.Reason)
		_ = e.feeBudget.Release(ctx, feeReservation.ID, outcome.Reason)
		_ = e.outboundRepo.MarkSweepAttemptFailed(ctx, task.AttemptID, outcome.Reason)
		return SweepLineBroadcastResult{}, fmt.Errorf("broadcast sweep tx rejected: %s", outcome.Reason)
	default:
		_ = e.feeBudget.Hold(ctx, feeReservation.ID, outcome.Reason)
		if err := e.outboundRepo.MarkSigningAuthorizationHeld(ctx, auth.Authorization.AuthorizationID, signedTx.TxHash); err != nil {
			return SweepLineBroadcastResult{}, HoldExecution(fmt.Errorf("record uncertain sweep tx: %w", err))
		}
		_ = e.outboundRepo.MarkSweepAttemptHeld(ctx, task.AttemptID, outcome.Reason)
		return SweepLineBroadcastResult{}, HoldExecution(fmt.Errorf("broadcast sweep tx result is uncertain: %s", outcome.Reason))
	}

	txHash := outcome.TxHash
	if err := e.recordSweepBroadcast(ctx, task, auth, feeReservation, resource.AllocationID, txHash); err != nil {
		return SweepLineBroadcastResult{}, HoldExecution(err)
	}
	e.log.Infow("sweep transaction broadcast",
		"authorization_id", auth.Authorization.AuthorizationID,
		"basis_id", auth.Authorization.BasisID,
		"tx_id", signedTx.TxHash,
		"tx_hash", txHash,
		"amount", auth.Authorization.AmountMinorUnits,
		"policy_version", auth.Authorization.PolicyVersion,
	)
	return SweepLineBroadcastResult{
		TxHash:   txHash,
		LineID:   auth.Authorization.BasisID,
		Amount:   auth.Authorization.AmountMinorUnits,
		CostLine: costLine,
	}, nil
}

func sweepNativePrincipal(task SweepTask, amount types.CryptoAmount) types.CryptoAmount {
	if task.AccountingModelVersion != 1 && task.TokenStandard == token.TokenStandardNative {
		return amount
	}
	return types.ZeroCryptoAmount()
}

func (e *SweepExecutor) EnsureSourceAccountActive(ctx context.Context, driver crypto.NetworkDriver, task SweepTask) error {
	if task.ChainActivationStatus != "" && task.ChainActivationStatus != wallet.ChainActivationActive {
		return fmt.Errorf("sweep source account is not chain-active: %s", task.ChainActivationStatus)
	}
	accounts := driver.Accounts()
	if accounts == nil {
		return nil
	}
	exists, err := accounts.IsActive(ctx, task.Address)
	if err != nil {
		return fmt.Errorf("check sweep source account activation: %w", err)
	}
	if !exists {
		return fmt.Errorf("sweep source account is not active on %s", driver.Network().Code)
	}
	return nil
}

func sweepGroupWalletIDs(group []SweepTask) []id.ID {
	walletIDs := make([]id.ID, 0, len(group))
	seen := make(map[id.ID]struct{}, len(group))
	for _, task := range group {
		if _, ok := seen[task.WalletID]; ok {
			continue
		}
		seen[task.WalletID] = struct{}{}
		walletIDs = append(walletIDs, task.WalletID)
	}
	return walletIDs
}

func SweepGroupAttemptIDs(group []SweepTask) []id.ID {
	attemptIDs := make([]id.ID, 0, len(group))
	seen := make(map[id.ID]struct{}, len(group))
	for _, task := range group {
		if _, ok := seen[task.AttemptID]; ok {
			continue
		}
		seen[task.AttemptID] = struct{}{}
		attemptIDs = append(attemptIDs, task.AttemptID)
	}
	return attemptIDs
}

func GroupSweepTasks(tasks []SweepTask) [][]SweepTask {
	type groupKey struct {
		tokenID         id.ID
		networkID       id.ID
		hotWalletID     id.ID
		contractAddress string
		standard        token.TokenStandard
		sweepID         string
	}

	order := make([]groupKey, 0)
	groups := make(map[groupKey][]SweepTask)
	for _, task := range tasks {
		// Each new attempt owns its document: partial funding retries cannot
		// replace another attempt's draft lines or repost an earlier success.
		sweepID := task.AttemptID.String()
		if task.SweepID != nil {
			sweepID = task.SweepID.String()
		}
		key := groupKey{
			tokenID:         task.TokenID,
			networkID:       task.NetworkID,
			hotWalletID:     task.HotWalletID,
			contractAddress: task.ContractAddress,
			standard:        task.TokenStandard,
			sweepID:         sweepID,
		}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], task)
	}

	result := make([][]SweepTask, 0, len(order))
	for _, key := range order {
		result = append(result, groups[key])
	}
	return result
}

// Records an observed chain effect without creating a signature or broadcasting.
func (e *SweepExecutor) recordSweepBroadcast(ctx context.Context, task SweepTask, auth SigningAuthorizationRecord, fee crypto.FeeReservation, allocationID id.ID, txHash string) error {
	if err := e.feeBudget.MarkBroadcast(ctx, fee.ID, auth.Authorization.UnsignedFingerprint, txHash); err != nil {
		return fmt.Errorf("record sweep fee reservation broadcast: %w", err)
	}
	if err := markPreparedResourceBroadcast(ctx, resolveResourcePreparer(e.resourcePreparers, auth.Authorization.ChainFamily), ResourceBroadcastCommand{
		AllocationID: allocationID,
		BasisType:    _sweepLineBasisType,
		BasisID:      task.AttemptID,
		TxHash:       txHash,
	}); err != nil {
		return fmt.Errorf("record sweep resource allocation broadcast: %w", err)
	}
	if err := e.outboundRepo.MarkSigningAuthorizationBroadcast(ctx, auth.Authorization.AuthorizationID, txHash); err != nil {
		return fmt.Errorf("record sweep broadcast: %w", err)
	}
	if err := e.outboundRepo.MarkSweepAttemptBroadcast(ctx, task.AttemptID); err != nil {
		return fmt.Errorf("record sweep attempt broadcast: %w", err)
	}
	return nil
}

func IsUnrecoverableSigningError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "transaction expired") ||
		strings.Contains(msg, "transaction expiration exceeds authorization")
}

func IsUnsignedEnvelopeExpired(envelope crypto.UnsignedTxEnvelope, now time.Time) bool {
	if envelope.Family == crypto.ChainFamilyTRON && len(envelope.Payload) > 0 {
		var header struct {
			RawData struct {
				Expiration int64 `json:"expiration"`
			} `json:"raw_data"`
		}
		if err := json.Unmarshal(envelope.Payload, &header); err == nil && header.RawData.Expiration > 0 {
			if !time.UnixMilli(header.RawData.Expiration).After(now) {
				return true
			}
		}
	}
	return false
}
