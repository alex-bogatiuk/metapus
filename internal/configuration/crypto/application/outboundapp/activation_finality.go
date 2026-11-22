package outboundapp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/documents/crypto_network_operation"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/tx"
	"metapus/pkg/sdk/types"
)

type FeeSettlementResult struct {
	ReservationID id.ID
	SettlementID  id.ID
	ActualFee     types.RawAmount
}
type ActivationWorkflowRepository interface {
	LockOperation(context.Context, AccountActivationTask) (*id.ID, error)
	LockRecoveredOperation(context.Context, AccountActivationTask, string) (*id.ID, error)
	LockLinkedOperation(context.Context, id.ID, id.ID) (id.ID, error)
	LockCostRepairOperation(context.Context, ActivationCostRepairTask, id.ID) (id.ID, error)
	LinkOperation(context.Context, id.ID, id.ID) error
	LinkRecoveredOperation(context.Context, id.ID, id.ID) error
	CompleteTechnicalState(context.Context, id.ID, string, crypto.ReceiptStatus) error
}
type NetworkOperationCommands interface {
	Create(context.Context, *crypto_network_operation.CryptoNetworkOperation) error
	Post(context.Context, id.ID) error
}
type DocumentFeeSettlement interface {
	SettleForDocument(context.Context, id.ID, crypto.ChainReceipt, string, id.ID) (FeeSettlementResult, error)
}
type NetworkOperationCoordinator struct {
	repo         crypto_network_operation.Repository
	service      NetworkOperationCommands
	feeBudget    DocumentFeeSettlement
	workflow     ActivationWorkflowRepository
	transactions tx.Manager
	now          func() time.Time
}

func NewNetworkOperationCoordinator(repo crypto_network_operation.Repository, commands NetworkOperationCommands, fees DocumentFeeSettlement, workflow ActivationWorkflowRepository, transactions tx.Manager, now func() time.Time) *NetworkOperationCoordinator {
	if now == nil {
		now = time.Now
	}
	return &NetworkOperationCoordinator{repo, commands, fees, workflow, transactions, now}
}
func (c *NetworkOperationCoordinator) EnsureActivationBroadcast(
	ctx context.Context,
	task AccountActivationTask,
	txHash string,
) (operationID id.ID, err error) {
	return c.ensureActivationBroadcast(ctx, task, txHash, false)
}
func (c *NetworkOperationCoordinator) RecoverActivationBroadcast(ctx context.Context, task AccountActivationTask, txHash string) (id.ID, error) {
	return c.ensureActivationBroadcast(ctx, task, txHash, true)
}
func (c *NetworkOperationCoordinator) ensureActivationBroadcast(ctx context.Context, task AccountActivationTask, txHash string, recovery bool) (operationID id.ID, err error) {
	if c == nil || c.service == nil || c.repo == nil || c.workflow == nil || c.transactions == nil {
		return id.Nil(), fmt.Errorf("crypto network operation coordinator is not configured")
	}
	txHash = strings.TrimSpace(txHash)
	if txHash == "" {
		return id.Nil(), fmt.Errorf("activation tx hash is required")
	}
	txm := c.transactions
	err = txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		var existing *id.ID
		var err error
		if recovery {
			existing, err = c.workflow.LockRecoveredOperation(txCtx, task, txHash)
		} else {
			existing, err = c.workflow.LockOperation(txCtx, task)
		}
		if err != nil {
			return fmt.Errorf("lock account activation: %w", err)
		}
		if existing != nil && !id.IsNil(*existing) {
			doc, err := c.repo.GetByID(txCtx, *existing)
			if err != nil {
				return err
			}
			if doc.OperationType != crypto_network_operation.OperationTypeWalletActivation ||
				doc.BasisType != crypto_network_operation.ActivationTaskBasisType ||
				doc.BasisID == nil || *doc.BasisID != task.ActivationID ||
				doc.TxHash != txHash || doc.NetworkID != task.NetworkID ||
				doc.TargetWalletID != task.WalletID || doc.TargetAddress != strings.TrimSpace(task.Address) {
				return fmt.Errorf("activation document identity mismatch")
			}
			operationID = doc.ID
		} else {
			doc := crypto_network_operation.NewWalletActivation(
				task.ActivationID, task.NetworkID, task.WalletID, task.HotWallet.ID,
				task.CostTokenID, task.Address, txHash,
			)
			if err := c.service.Create(txCtx, doc); err != nil {
				return fmt.Errorf("create crypto network operation after broadcast: %w", err)
			}
			operationID = doc.ID
		}
		if recovery {
			return c.workflow.LinkRecoveredOperation(txCtx, task.ActivationID, operationID)
		}
		return c.workflow.LinkOperation(txCtx, task.ActivationID, operationID)
	})
	return operationID, err
}

func (c *NetworkOperationCoordinator) FinalizeActivation(
	ctx context.Context,
	activationID, reservationID id.ID,
	receipt crypto.ChainReceipt,
) (*crypto_network_operation.CryptoNetworkOperation, error) {
	return c.finalizeActivation(ctx, activationID, reservationID, receipt, nil)
}

func (c *NetworkOperationCoordinator) FinalizeActivationRepair(ctx context.Context, task ActivationCostRepairTask, reservationID id.ID, receipt crypto.ChainReceipt) (*crypto_network_operation.CryptoNetworkOperation, error) {
	return c.finalizeActivation(ctx, task.ActivationID, reservationID, receipt, &task)
}

func (c *NetworkOperationCoordinator) finalizeActivation(ctx context.Context, activationID, reservationID id.ID, receipt crypto.ChainReceipt, repair *ActivationCostRepairTask) (*crypto_network_operation.CryptoNetworkOperation, error) {
	if c == nil || c.service == nil || c.repo == nil || c.feeBudget == nil || c.workflow == nil || c.transactions == nil {
		return nil, fmt.Errorf("crypto network operation coordinator is not configured")
	}
	if !receipt.FinanciallyFinal() {
		return nil, fmt.Errorf("solidified or finalized activation receipt is required")
	}
	txm := c.transactions
	var finalized *crypto_network_operation.CryptoNetworkOperation
	err := txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		var operationID id.ID
		var err error
		if repair != nil {
			operationID, err = c.workflow.LockCostRepairOperation(txCtx, *repair, reservationID)
		} else {
			operationID, err = c.workflow.LockLinkedOperation(txCtx, activationID, reservationID)
		}
		if err != nil {
			return err
		}
		doc, err := c.repo.GetByID(txCtx, operationID)
		if err != nil {
			return err
		}
		if doc.TxHash != receipt.TxHash || doc.NetworkID != receipt.NetworkID {
			return fmt.Errorf("activation finality identity mismatch")
		}
		finalStatus := crypto_network_operation.StatusSucceeded
		technicalStatus := "confirmed"
		if receipt.Status == crypto.ReceiptReverted {
			finalStatus = crypto_network_operation.StatusReverted
			technicalStatus = "failed"
		}
		if doc.Posted {
			if doc.Status != finalStatus {
				return fmt.Errorf("posted activation finality status mismatch")
			}
			if doc.NetworkFeeReservationID == nil || *doc.NetworkFeeReservationID != reservationID {
				return fmt.Errorf("posted activation fee reservation mismatch")
			}
			if _, err := c.feeBudget.SettleForDocument(txCtx, reservationID, receipt, crypto_network_operation.DocumentType, doc.ID); err != nil {
				return err
			}
			finalized = doc
			return c.workflow.CompleteTechnicalState(txCtx, activationID, technicalStatus, receipt.Status)
		}
		if receipt.ObservedAt.IsZero() {
			receipt.ObservedAt = c.now().UTC()
		}

		settlement, err := c.feeBudget.SettleForDocument(
			txCtx, reservationID, receipt, crypto_network_operation.DocumentType, doc.ID,
		)
		if err != nil {
			return err
		}
		doc.Status = finalStatus
		doc.CostAmount = settlement.ActualFee
		doc.ChainSettlementID = &settlement.SettlementID
		doc.NetworkFeeReservationID = &settlement.ReservationID
		finalizedAt := receipt.ObservedAt
		doc.FinalizedAt = &finalizedAt
		if err := c.repo.Update(txCtx, doc); err != nil {
			return fmt.Errorf("finalize crypto network operation: %w", err)
		}
		if err := c.service.Post(txCtx, doc.ID); err != nil {
			return fmt.Errorf("post crypto network operation: %w", err)
		}
		if err := c.workflow.CompleteTechnicalState(txCtx, activationID, technicalStatus, receipt.Status); err != nil {
			return err
		}
		finalized, err = c.repo.GetByID(txCtx, doc.ID)
		return err
	})
	return finalized, err
}
