package outboundapp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"metapus/internal/configuration/crypto/domain/catalogs/token"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/documents/crypto_network_operation"
	"metapus/internal/core/id"
	"metapus/internal/core/tx"
	"metapus/internal/core/types"
	"metapus/pkg/logger"
)

const _accountActivationBasisType = crypto_network_operation.ActivationTaskBasisType
const ActivationRetryPeriod = 30 * time.Second
const _accountActivationPeriod = ActivationRetryPeriod

type AccountActivationTask struct {
	ClaimToken         id.ID
	ActivationID       id.ID
	NetworkOperationID *id.ID
	WalletID           id.ID
	NetworkID          id.ID
	NetworkCode        string
	Address            string
	Status             string
	TxHash             string
	HotWallet          HotWallet
	CostTokenID        id.ID
}
type ActivationCostRepairTask struct {
	ActivationID   id.ID
	NetworkID      id.ID
	TxHash         string
	CostClaimToken id.ID
}

type ActivationStatePreflight interface {
	PrepareAccountActivationState(context.Context, AccountActivationTask) error
}
type ActivationRepository interface {
	PrepareAccountActivationState(context.Context, AccountActivationTask) error
	ApplyAccountActivationClaim(context.Context, AccountActivationTask, ActivationClaimUpdate) error
	PrepareActivationBroadcastRecovery(context.Context, AccountActivationTask, ActivationBroadcastEvidence) error
	GetActivationBroadcastEvidence(context.Context, AccountActivationTask, string) (ActivationBroadcastEvidence, bool, error)
	BeginAccountActivationDispatch(context.Context, AccountActivationTask, crypto.SigningAuthorization, string) error
	HasAccountActivationSigningExposure(context.Context, AccountActivationTask) (bool, error)
	DeferAccountActivation(
		ctx context.Context,
		activationID id.ID,
		delay time.Duration,
	) error
	GetAccountActivationSigningAttempt(ctx context.Context, task AccountActivationTask) (SigningAuthorizationRecord, bool, error)
	MarkAccountActivationActive(ctx context.Context, activationID id.ID) error
	MarkAccountActivationExternallyActive(ctx context.Context, activationID, _ id.ID) error
	MarkAccountActivationFailed(ctx context.Context, activationID id.ID, reason string) error
	MarkAccountActivationHeld(ctx context.Context, activationID id.ID, reason string) error
	MarkAccountActivationSigning(ctx context.Context, activationID id.ID) error
	PrepareActivationCostRepair(ctx context.Context, task ActivationCostRepairTask) error
	MarkActivationCostRepairHeld(ctx context.Context, task ActivationCostRepairTask, reason string) error
	MarkSigningAuthorizationBroadcast(ctx context.Context, authID, txID string) error
	MarkSigningAuthorizationFailed(ctx context.Context, authID, txID string) error
	MarkSigningAuthorizationHeld(ctx context.Context, authID, txID string) error
	ReserveAccountActivationBudget(
		ctx context.Context,
		task AccountActivationTask,
	) error
}
type ActivationFees interface {
	FeeBudget
	ReserveActivation(context.Context, AccountActivationTask, FeeReservationRequest) (crypto.FeeReservation, error)
	ReleaseUnbroadcast(context.Context, id.ID, string) error
}
type ActivationOperations interface {
	EnsureActivationBroadcast(context.Context, AccountActivationTask, string) (id.ID, error)
	RecoverActivationBroadcast(context.Context, AccountActivationTask, string) (id.ID, error)
	FinalizeActivation(context.Context, id.ID, id.ID, crypto.ChainReceipt) (*crypto_network_operation.CryptoNetworkOperation, error)
	FinalizeActivationRepair(context.Context, ActivationCostRepairTask, id.ID, crypto.ChainReceipt) (*crypto_network_operation.CryptoNetworkOperation, error)
}
type ActivationConfig struct {
	Drivers        *crypto.ChainDriverRegistry
	Signers        *crypto.FamilySignerRegistry
	Repository     ActivationRepository
	Authorizations Authorizations
	Fees           ActivationFees
	Operations     ActivationOperations
	WalletPool     crypto.WalletPoolActivationStatusRepository
	Transactions   tx.Manager
	Logger         *logger.Logger
	Observer       Observer
	Now            func() time.Time
}
type AccountActivationExecutor struct {
	drivers        *crypto.ChainDriverRegistry
	signers        *crypto.FamilySignerRegistry
	outboundRepo   ActivationRepository
	authorizations Authorizations
	feeBudget      ActivationFees
	operations     ActivationOperations
	walletPoolRepo crypto.WalletPoolActivationStatusRepository
	transactions   tx.Manager
	log            *logger.Logger
	observer       Observer
	now            func() time.Time
}

func NewAccountActivationExecutor(c ActivationConfig) *AccountActivationExecutor {
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Logger == nil {
		c.Logger = logger.Default()
	}
	return &AccountActivationExecutor{c.Drivers, c.Signers, c.Repository, c.Authorizations, c.Fees, c.Operations, c.WalletPool, c.Transactions, c.Logger, c.Observer, c.Now}
}
func (e *AccountActivationExecutor) validateTaskDriver(ctx context.Context, task AccountActivationTask) (crypto.NetworkDriver, crypto.NetworkPolicy, error) {
	driver, err := e.drivers.Resolve(task.NetworkID)
	if err != nil {
		return nil, crypto.NetworkPolicy{}, fmt.Errorf("resolve activation network driver: %w", err)
	}
	if !driver.Capabilities().WalletProvisioning.RequiresActivation() {
		return nil, crypto.NetworkPolicy{}, fmt.Errorf("network %s does not declare account activation capability", driver.Network().Code)
	}
	if driver.Accounts() == nil {
		return nil, crypto.NetworkPolicy{}, fmt.Errorf("network %s does not support account activation", driver.Network().Code)
	}
	if e.feeBudget == nil {
		return nil, crypto.NetworkPolicy{}, fmt.Errorf("fee budget manager is not configured")
	}
	policy, err := e.feeBudget.LoadPolicy(ctx, task.NetworkID)
	if err != nil {
		return nil, crypto.NetworkPolicy{}, fmt.Errorf("load activation network policy: %w", err)
	}
	if !policy.Enabled {
		return nil, crypto.NetworkPolicy{}, ErrNetworkPolicyDisabled
	}
	return driver, policy, nil
}

func (e *AccountActivationExecutor) holdClaim(ctx context.Context, task AccountActivationTask, reason string, fee *id.ID, auth *SigningAuthorizationRecord) error {
	return e.executionGuard(task).Run(ctx, func(ctx context.Context) error {
		if fee != nil {
			if err := e.feeBudget.Hold(ctx, *fee, reason); err != nil {
				return err
			}
		}
		if auth != nil && auth.SignedTx != nil {
			if err := e.outboundRepo.MarkSigningAuthorizationHeld(ctx, auth.Authorization.AuthorizationID, auth.TxID); err != nil {
				return err
			}
		}
		return e.updateClaim(ctx, task, ActivationClaimHeld, reason, 0)
	})
}
func (e *AccountActivationExecutor) handleAlreadyActiveAccount(ctx context.Context, task AccountActivationTask, hasSigningAttempt bool, auth SigningAuthorizationRecord) error {
	reason := "account is active but the activation signing outcome requires reconciliation"
	if hasSigningAttempt && auth.SignedTx != nil {
		if err := e.holdClaim(ctx, task, reason, nil, &auth); err != nil {
			return err
		}
		return HoldExecution(errors.New(reason))
	}
	exposed := false
	err := e.executionGuard(task).Run(ctx, func(ctx context.Context) error {
		var err error
		exposed, err = e.outboundRepo.HasAccountActivationSigningExposure(ctx, task)
		if err != nil {
			return err
		}
		reservation, found, err := e.feeBudget.Get(ctx, _accountActivationBasisType, task.ActivationID, crypto.SigningPurposeActivation, 1)
		if err != nil {
			return err
		}
		if exposed {
			if found {
				if err := e.feeBudget.Hold(ctx, reservation.ID, reason); err != nil {
					return err
				}
			}
			return e.updateClaim(ctx, task, ActivationClaimHeld, reason, 0)
		}
		if found && (reservation.Status == crypto.FeeReservationReserved || reservation.Status == crypto.FeeReservationHeld) {
			if err := e.feeBudget.ReleaseUnbroadcast(ctx, reservation.ID, "account became active before transaction signing"); err != nil {
				return err
			}
		}
		if hasSigningAttempt && auth.Status != "failed" {
			if err := e.outboundRepo.MarkSigningAuthorizationFailed(ctx, auth.Authorization.AuthorizationID, auth.TxID); err != nil {
				return err
			}
		}
		return e.updateClaim(ctx, task, ActivationClaimExternallyActive, "", 0)
	})
	if err != nil {
		return err
	}
	if exposed {
		return HoldExecution(errors.New(reason))
	}
	return nil
}
func (e *AccountActivationExecutor) retireExpiredSigningAttempt(ctx context.Context, task AccountActivationTask, auth SigningAuthorizationRecord, feeReservationID id.ID) error {
	reason := "staged activation signing authorization expired before transaction signing"
	exposed := false
	err := e.executionGuard(task).Run(ctx, func(ctx context.Context) error {
		var err error
		exposed, err = e.outboundRepo.HasAccountActivationSigningExposure(ctx, task)
		if err != nil {
			return err
		}
		if exposed {
			reason = "activation signing was dispatched but its signed result is not available"
			if err := e.feeBudget.Hold(ctx, feeReservationID, reason); err != nil {
				return err
			}
			return e.updateClaim(ctx, task, ActivationClaimHeld, reason, 0)
		}
		if err := e.outboundRepo.MarkSigningAuthorizationFailed(ctx, auth.Authorization.AuthorizationID, ""); err != nil {
			return err
		}
		if err := e.feeBudget.ReleaseUnbroadcast(ctx, feeReservationID, reason); err != nil {
			return err
		}
		return e.updateClaim(ctx, task, ActivationClaimFailed, reason, 0)
	})
	if err != nil {
		return fmt.Errorf("retire expired staged activation: %w", err)
	}
	if exposed {
		return HoldExecution(errors.New(reason))
	}
	return nil
}

// retireFailedUnsignedAttempt ends an activation whose only attempt failed
// before signing. Retrying would reuse a released fee reservation forever and
// keep the network's activation slot occupied; failing it frees the slot and
// lets the pool replace the wallet, as an expired staged attempt does.
func (e *AccountActivationExecutor) retireFailedUnsignedAttempt(ctx context.Context, task AccountActivationTask, reservation crypto.FeeReservation) error {
	reason := "activation signing attempt failed before transaction signing"
	exposed := false
	err := e.executionGuard(task).Run(ctx, func(ctx context.Context) error {
		var err error
		exposed, err = e.outboundRepo.HasAccountActivationSigningExposure(ctx, task)
		if err != nil {
			return err
		}
		if exposed {
			reason = "activation signing was dispatched but its signed result is not available"
			if reservation.Status != crypto.FeeReservationReleased {
				if err := e.feeBudget.Hold(ctx, reservation.ID, reason); err != nil {
					return err
				}
			}
			return e.updateClaim(ctx, task, ActivationClaimHeld, reason, 0)
		}
		if reservation.Status == crypto.FeeReservationReserved || reservation.Status == crypto.FeeReservationHeld {
			if err := e.feeBudget.ReleaseUnbroadcast(ctx, reservation.ID, reason); err != nil {
				return err
			}
		}
		return e.updateClaim(ctx, task, ActivationClaimFailed, reason, 0)
	})
	if err != nil {
		return fmt.Errorf("retire failed unsigned activation: %w", err)
	}
	if exposed {
		return HoldExecution(errors.New(reason))
	}
	return nil
}

func (e *AccountActivationExecutor) prepareSigningAuthorization(ctx context.Context, driver crypto.NetworkDriver, policy crypto.NetworkPolicy, task AccountActivationTask) (SigningAuthorizationRecord, crypto.FeeReservation, error) {
	guarded, ok := e.authorizations.(GuardedAuthorizations)
	if !ok {
		return SigningAuthorizationRecord{}, crypto.FeeReservation{}, fmt.Errorf("activation authorizations do not support execution fencing")
	}
	intent := crypto.TransferIntent{BasisType: _accountActivationBasisType, Purpose: crypto.SigningPurposeActivation, BasisID: task.ActivationID, Attempt: 1, Network: driver.Network(),
		Asset: crypto.AssetRef{TokenID: task.CostTokenID, Standard: token.TokenStandardNative, Decimals: driver.Network().NativeDecimals}, SourceWalletID: task.HotWallet.ID, SourceAddress: task.HotWallet.Address, DestinationAddress: task.Address, Amount: types.ZeroRawAmount()}
	if _, err := e.authorizations.PrepareTransferCustody(ctx, intent, nil); err != nil {
		return SigningAuthorizationRecord{}, crypto.FeeReservation{}, HoldExecution(err)
	}
	quote, err := driver.Transactions().QuoteFee(ctx, intent)
	if err != nil {
		return SigningAuthorizationRecord{}, crypto.FeeReservation{}, NewChainDependencyError(fmt.Errorf("quote activation fee: %w", err))
	}
	quote, err = policy.BoundFeeQuote(quote)
	if err != nil {
		return SigningAuthorizationRecord{}, crypto.FeeReservation{}, HoldExecution(err)
	}
	fee, err := e.feeBudget.ReserveActivation(ctx, task, FeeReservationRequest{NetworkID: task.NetworkID, PayerWalletID: task.HotWallet.ID, NativeTokenID: task.CostTokenID, OperationTokenID: task.CostTokenID, BasisType: intent.BasisType, BasisID: intent.BasisID, Purpose: intent.Purpose, Attempt: 1, Strategy: crypto.FeeStrategyNativeBurn, MaxFee: quote.MaxFee})
	if err != nil {
		if errors.Is(err, ErrActivationConcurrencyLimit) {
			if err := e.updateClaim(ctx, task, ActivationClaimDeferred, "", _accountActivationPeriod); err != nil {
				return SigningAuthorizationRecord{}, crypto.FeeReservation{}, err
			}
			return SigningAuthorizationRecord{}, crypto.FeeReservation{}, nil
		}
		reason := err.Error()
		_ = e.executionGuard(task).Run(ctx, func(ctx context.Context) error {
			if e.walletPoolRepo != nil {
				if err := e.walletPoolRepo.SetOperationalStatus(ctx, task.NetworkID, crypto.WalletPoolOperationalStatus{ErrorCode: crypto.WalletPoolErrorActivationHeld, PublicMessage: "Активация кошельков удерживается политикой бюджета или доступностью hot wallet."}); err != nil {
					return err
				}
			}
			return e.updateClaim(ctx, task, ActivationClaimHeld, reason, 0)
		})
		return SigningAuthorizationRecord{}, crypto.FeeReservation{}, HoldExecution(err)
	}
	unsigned, err := driver.Accounts().BuildActivation(ctx, intent, quote)
	if err != nil {
		_ = e.executionGuard(task).Run(ctx, func(ctx context.Context) error {
			if err := e.feeBudget.ReleaseUnbroadcast(ctx, fee.ID, "activation transaction was not built"); err != nil {
				return err
			}
			return e.updateClaim(ctx, task, ActivationClaimHeld, "create activation transaction failed", 0)
		})
		return SigningAuthorizationRecord{}, fee, NewChainDependencyError(err)
	}
	err = e.executionGuard(task).Run(ctx, func(ctx context.Context) error {
		if err := e.feeBudget.BindUnsignedFingerprint(ctx, fee.ID, unsigned.UnsignedFingerprint); err != nil {
			return err
		}
		return e.updateClaim(ctx, task, ActivationClaimSigning, "", 0)
	})
	if err != nil {
		return SigningAuthorizationRecord{}, fee, err
	}
	auth, err := guarded.CreateGuarded(ctx, SigningAttemptInput{Intent: intent, Capabilities: driver.Capabilities(), VaultKeyID: task.HotWallet.VaultKeyID, FeePayerAddress: task.HotWallet.Address, MaxNetworkFee: quote.MaxFee, Unsigned: unsigned}, e.executionGuard(task))
	if err != nil {
		_ = e.holdClaim(ctx, task, err.Error(), &fee.ID, nil)
		return SigningAuthorizationRecord{}, fee, HoldExecution(err)
	}
	return auth, fee, nil
}

func (e *AccountActivationExecutor) signAndBroadcastActivation(ctx context.Context, driver crypto.NetworkDriver, task AccountActivationTask, auth SigningAuthorizationRecord, fee crypto.FeeReservation, familySigner crypto.PolicyAwareTransactionSigner) error {
	guarded, ok := e.authorizations.(GuardedAuthorizations)
	if !ok {
		return fmt.Errorf("activation authorizations do not support execution fencing")
	}
	signed, err := guarded.SignOrReuseGuarded(ctx, familySigner, auth, e.executionGuard(task))
	if err != nil {
		_ = e.holdClaim(ctx, task, err.Error(), &fee.ID, nil)
		return HoldExecution(err)
	}
	if err := guarded.CheckCustodyDispatchGuarded(ctx, auth.Authorization, e.signers, e.executionGuard(task)); err != nil {
		return HoldExecution(err)
	}
	outcome, err := driver.Broadcaster().Broadcast(ctx, signed)
	if e.observer != nil {
		e.observer.Broadcast(ctx, driver.Network(), crypto.SigningPurposeActivation, outcome)
	}
	if err != nil {
		_ = e.holdClaim(ctx, task, err.Error(), &fee.ID, nil)
		return NewChainDependencyError(err)
	}
	if outcome.Status == crypto.BroadcastAccepted || outcome.Status == crypto.BroadcastAlreadyKnown {
		hash := outcome.TxHash
		if hash == "" {
			hash = signed.TxHash
		}
		if hash != signed.TxHash {
			return fmt.Errorf("activation broadcast returned another transaction identity")
		}
		// Persist known exposure even after the actor's lease changed. No dispatch
		// occurs here; exact signed bytes and the original fee tuple are required.
		err = e.recoverBroadcastEvidence(ctx, task, ActivationBroadcastEvidence{AuthorizationID: auth.Authorization.AuthorizationID, ReservationID: fee.ID, Fingerprint: auth.Authorization.UnsignedFingerprint, TxHash: hash})
		if err != nil {
			return err
		}
		return e.confirmBroadcast(ctx, driver, task, hash)
	}
	// A returned rejection after Sign is not proof that its signed transaction
	// cannot execute. Keep the reservation and original bytes for reconciliation.
	reason := outcome.Reason
	if reason == "" {
		reason = "activation broadcast outcome requires reconciliation"
	}
	if err := e.holdClaim(ctx, task, reason, &fee.ID, nil); err != nil {
		return err
	}
	return HoldExecution(errors.New(reason))
}

func (e *AccountActivationExecutor) Execute(ctx context.Context, task AccountActivationTask) (err error) {
	if e.observer != nil {
		var finish func(error)
		ctx, finish = e.observer.Start(ctx, "crypto.activation.execute", task.NetworkID, crypto.SigningPurposeActivation)
		defer func() { finish(err) }()
	}

	if err := e.executionGuard(task).Run(ctx, nil); err != nil {
		return err
	}
	driver, policy, err := e.validateTaskDriver(ctx, task)
	if err != nil {
		return err
	}
	if strings.TrimSpace(task.TxHash) != "" {
		return e.confirmBroadcast(ctx, driver, task, task.TxHash)
	}
	auth, hasSigningAttempt, err := e.outboundRepo.GetAccountActivationSigningAttempt(ctx, task)
	if err != nil {
		return err
	}
	if hasSigningAttempt {
		task.HotWallet = auth.SourceWallet
	}
	if hasSigningAttempt && auth.Status == "broadcast" && strings.TrimSpace(auth.TxID) != "" {
		return e.confirmBroadcast(ctx, driver, task, auth.TxID)
	}
	if hasSigningAttempt && (auth.Status == "signed" || auth.Status == "held") && strings.TrimSpace(auth.TxID) != "" {
		receipt, receiptErr := driver.Finality().Receipt(ctx, auth.TxID)
		if receiptErr == nil && receipt.Status != crypto.ReceiptPending {
			return e.confirmBroadcast(ctx, driver, task, auth.TxID)
		}
	}

	if hasSigningAttempt && auth.SignedTx == nil {
		exposed, err := e.outboundRepo.HasAccountActivationSigningExposure(ctx, task)
		if err != nil {
			return err
		}
		if exposed {
			reason := "activation signing result is unknown; signed result recovery is required"
			reservation, found, err := e.feeBudget.Get(ctx, _accountActivationBasisType, task.ActivationID, crypto.SigningPurposeActivation, 1)
			if err != nil {
				return err
			}
			var fee *id.ID
			if found {
				fee = &reservation.ID
			}
			if err := e.holdClaim(ctx, task, reason, fee, nil); err != nil {
				return err
			}
			return HoldExecution(errors.New(reason))
		}
	}

	exists, err := driver.Accounts().IsActive(ctx, task.Address)
	if err != nil {
		_ = e.holdClaim(ctx, task, fmt.Sprintf("check account: %v", err), nil, nil)
		return NewChainDependencyError(err)
	}
	if exists {
		return e.handleAlreadyActiveAccount(ctx, task, hasSigningAttempt, auth)
	}

	var familySigner crypto.PolicyAwareTransactionSigner
	if !hasSigningAttempt || auth.SignedTx == nil {
		familySigner, err = e.authorizations.ResolveSigner(ctx, e.signers, driver.Network())
		if err != nil {
			_ = e.holdClaim(ctx, task, err.Error(), nil, nil)
			return HoldExecution(err)
		}
	}

	var feeReservation crypto.FeeReservation
	if hasSigningAttempt {
		var found bool
		feeReservation, found, err = e.feeBudget.Get(ctx, _accountActivationBasisType, task.ActivationID, crypto.SigningPurposeActivation, 1)
		if err != nil {
			return fmt.Errorf("load activation fee reservation: %w", err)
		}
		if !found {
			return fmt.Errorf("persisted activation signing attempt has no fee reservation")
		}
		if FailedUnsignedSigningAttempt(auth) {
			return e.retireFailedUnsignedAttempt(ctx, task, feeReservation)
		}
		if ExpiredUnbroadcastSigningAttempt(auth, e.now().UTC()) {
			return e.retireExpiredSigningAttempt(ctx, task, auth, feeReservation.ID)
		}
	} else {
		auth, feeReservation, err = e.prepareSigningAuthorization(ctx, driver, policy, task)
		if err != nil {
			return err
		}
		if id.IsNil(feeReservation.ID) {
			// Concurrency limit deferred
			return nil
		}
	}

	return e.signAndBroadcastActivation(ctx, driver, task, auth, feeReservation, familySigner)
}

func (e *AccountActivationExecutor) recoverBroadcastEvidence(ctx context.Context, task AccountActivationTask, evidence ActivationBroadcastEvidence) error {
	return e.transactions.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := e.outboundRepo.PrepareActivationBroadcastRecovery(ctx, task, evidence); err != nil {
			return err
		}
		current, found, err := e.feeBudget.Get(ctx, _accountActivationBasisType, task.ActivationID, crypto.SigningPurposeActivation, 1)
		if err != nil {
			return err
		}
		if !found || current.ID != evidence.ReservationID {
			return fmt.Errorf("activation fee reservation identity differs")
		}
		alreadyBroadcast := current.Status == crypto.FeeReservationBroadcast && current.TxHash == evidence.TxHash && current.UnsignedFingerprint == evidence.Fingerprint
		if current.Status != crypto.FeeReservationSettled && !alreadyBroadcast {
			if err := e.feeBudget.MarkBroadcast(ctx, current.ID, evidence.Fingerprint, evidence.TxHash); err != nil {
				return err
			}
		}
		return e.outboundRepo.MarkSigningAuthorizationBroadcast(ctx, evidence.AuthorizationID, evidence.TxHash)
	})
}

func (e *AccountActivationExecutor) confirmBroadcast(ctx context.Context, driver crypto.NetworkDriver, task AccountActivationTask, txHash string) error {
	if txHash == "" {
		return fmt.Errorf("activation tx hash is required")
	}
	if e.operations == nil {
		return fmt.Errorf("crypto network operation coordinator is not configured")
	}
	evidence, found, evidenceErr := e.outboundRepo.GetActivationBroadcastEvidence(ctx, task, txHash)
	if evidenceErr != nil {
		return evidenceErr
	}
	if found {
		if err := e.recoverBroadcastEvidence(ctx, task, evidence); err != nil {
			return err
		}
	}

	operationID, err := e.operations.RecoverActivationBroadcast(ctx, task, txHash)
	if err != nil {
		return err
	}
	task.NetworkOperationID = &operationID
	task.TxHash = txHash
	receipt, err := driver.Finality().Receipt(ctx, txHash)
	if err != nil {
		return NewChainDependencyError(fmt.Errorf("get activation tx info: %w", err))
	}
	if !receipt.FinanciallyFinal() {
		return nil
	}
	feeReservation, found, err := e.feeBudget.Get(ctx, _accountActivationBasisType, task.ActivationID, crypto.SigningPurposeActivation, 1)
	if err != nil {
		return fmt.Errorf("load activation fee reservation for settlement: %w", err)
	}
	if !found {
		return fmt.Errorf("activation fee reservation is missing for settlement")
	}
	finalized, err := e.operations.FinalizeActivation(ctx, task.ActivationID, feeReservation.ID, receipt)
	if err != nil {
		return err
	}
	if receipt.Status == crypto.ReceiptReverted {
		reason := "activation transaction reverted on-chain"
		return errors.New(reason)
	}
	exists, err := driver.Accounts().IsActive(ctx, task.Address)
	if err != nil {
		_ = e.outboundRepo.MarkAccountActivationHeld(ctx, task.ActivationID, fmt.Sprintf("confirm account: %v", err))
		return NewChainDependencyError(err)
	}
	if !exists {
		_ = e.outboundRepo.MarkAccountActivationHeld(ctx, task.ActivationID, "activation transaction confirmed but account is not visible yet")
		return nil
	}
	if err := e.outboundRepo.MarkAccountActivationActive(ctx, task.ActivationID); err != nil {
		return err
	}
	e.log.Infow("chain account activated",
		"activation_id", task.ActivationID,
		"wallet_id", task.WalletID,
		"address", task.Address,
		"tx_hash", txHash,
		"fee", finalized.CostAmount,
	)
	return nil
}

func (e *AccountActivationExecutor) RepairCost(ctx context.Context, task ActivationCostRepairTask) error {
	if err := e.outboundRepo.PrepareActivationCostRepair(ctx, task); err != nil {
		return err
	}
	driver, err := e.drivers.Resolve(task.NetworkID)
	if err != nil {
		return err
	}
	receipt, err := driver.Finality().Receipt(ctx, task.TxHash)
	if err != nil {
		reason := fmt.Sprintf("get activation tx info: %v", err)
		_ = e.outboundRepo.MarkActivationCostRepairHeld(ctx, task, reason)
		return NewChainDependencyError(errors.New(reason))
	}
	if !receipt.FinanciallyFinal() {
		reason := "activation transaction is not confirmed"
		_ = e.outboundRepo.MarkActivationCostRepairHeld(ctx, task, reason)
		return errors.New(reason)
	}
	feeReservation, found, lookupErr := e.feeBudget.Get(ctx, _accountActivationBasisType, task.ActivationID, crypto.SigningPurposeActivation, 1)
	if lookupErr != nil {
		return fmt.Errorf("load activation fee reservation for repair: %w", lookupErr)
	}
	if !found {
		return fmt.Errorf("activation fee reservation is missing for repair")
	}
	finalized, err := e.operations.FinalizeActivationRepair(ctx, task, feeReservation.ID, receipt)
	if err != nil {
		_ = e.outboundRepo.MarkActivationCostRepairHeld(ctx, task, err.Error())
		return err
	}
	e.log.Infow("chain account activation cost repaired",
		"activation_id", task.ActivationID,
		"tx_hash", task.TxHash,
		"fee", finalized.CostAmount,
	)
	return nil
}
