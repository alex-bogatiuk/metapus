package outbound_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"metapus/internal/configuration/crypto/application/outboundapp"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/documents/crypto_network_operation"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
)

type ActivationWorkflowStore struct{}

func (s ActivationWorkflowStore) LockOperation(ctx context.Context, task outboundapp.AccountActivationTask) (*id.ID, error) {
	existing, err := s.lockResources(ctx, task.ActivationID, &task, nil)
	if err != nil {
		return nil, err
	}
	if id.IsNil(task.ClaimToken) {
		err = requireUnclaimedActivation(ctx, task.ActivationID)
	} else {
		err = validateActivationExecutionClaim(ctx, task)
	}
	return existing, err
}

func (s ActivationWorkflowStore) LockRecoveredOperation(ctx context.Context, task outboundapp.AccountActivationTask, hash string) (*id.ID, error) {
	existing, err := s.lockResources(ctx, task.ActivationID, &task, nil)
	if err != nil {
		return nil, err
	}
	if err := validateActivationBroadcastRecovery(ctx, task, hash); err != nil {
		return nil, err
	}
	return existing, nil
}
func (s ActivationWorkflowStore) LinkRecoveredOperation(ctx context.Context, activation, operation id.ID) error {
	// Preserve the live actor and terminal projections; this is a fact binding.
	_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `UPDATE reg_crypto_account_activations SET network_operation_id=$2,broadcast_at=COALESCE(broadcast_at,clock_timestamp()) WHERE id=$1 AND network_operation_id IS NULL`, activation, operation)
	return err
}

func (s ActivationWorkflowStore) LockLinkedOperation(ctx context.Context, activationID, reservationID id.ID) (id.ID, error) {
	existing, err := s.lockResources(ctx, activationID, nil, &reservationID)
	if err != nil {
		return id.Nil(), err
	}
	if existing == nil {
		return id.Nil(), apperror.NewConflict("activation operation is not linked")
	}
	return *existing, nil
}

// Cost repair is fenced separately from authoritative finality/reconciliation.
func (s ActivationWorkflowStore) LockCostRepairOperation(ctx context.Context, task outboundapp.ActivationCostRepairTask, reservationID id.ID) (id.ID, error) {
	operation, err := s.LockLinkedOperation(ctx, task.ActivationID, reservationID)
	if err != nil {
		return id.Nil(), err
	}
	if err := validateActivationCostClaim(ctx, task); err != nil {
		return id.Nil(), err
	}
	return operation, nil
}

// The workflow gate precedes the operation advisory, the complete payer/target
// footprint and the activation row. Posting and reverted target updates cannot
// introduce a wallet or operation advisory after the workflow row is held.
func (ActivationWorkflowStore) lockResources(ctx context.Context, activationID id.ID, task *outboundapp.AccountActivationTask, reservationID *id.ID) (*id.ID, error) {
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return nil, apperror.NewValidation("activation resource locks require a transaction")
	}
	locker := postgres.NewDocLocker()
	if err := locker.LockDocument(ctx, crypto_network_operation.ActivationTaskBasisType, activationID); err != nil {
		return nil, err
	}
	q := tm.GetQuerier(ctx)
	var existing *id.ID
	var network, target, source id.ID
	err := q.QueryRow(ctx, `SELECT network_id,wallet_id,network_operation_id FROM reg_crypto_account_activations WHERE id=$1`, activationID).Scan(&network, &target, &existing)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperror.NewNotFound("account activation", activationID)
	}
	if err != nil {
		return nil, err
	}
	if task != nil && (task.NetworkID != network || task.WalletID != target) {
		return nil, apperror.NewConflict("activation task identity changed")
	}
	if existing != nil {
		if err := locker.LockDocument(ctx, crypto_network_operation.DocumentType, *existing); err != nil {
			return nil, err
		}
		var valid bool
		if err := q.QueryRow(ctx, `SELECT source_wallet_id,network_id=$2 AND target_wallet_id=$3 AND basis_type=$4 AND basis_id=$5
			FROM doc_crypto_network_operations WHERE id=$1`, *existing, network, target, crypto_network_operation.ActivationTaskBasisType, activationID).Scan(&source, &valid); err != nil {
			return nil, err
		}
		if !valid {
			return nil, apperror.NewConflict("activation document identity changed")
		}
	} else {
		if task == nil {
			return nil, apperror.NewConflict("activation operation is not linked")
		}
		source = task.HotWallet.ID
	}
	if reservationID != nil {
		var valid bool
		if err := q.QueryRow(ctx, `SELECT network_id=$2 AND payer_wallet_id=$3 AND purpose='activation' AND basis_type=$4 AND basis_id=$5
			FROM reg_crypto_fee_reservations WHERE id=$1`, *reservationID, network, source, crypto_network_operation.ActivationTaskBasisType, activationID).Scan(&valid); err != nil {
			return nil, err
		}
		if !valid {
			return nil, apperror.NewConflict("activation fee reservation identity mismatch")
		}
	}
	if err := lockActivationState(ctx, activationID, source, target); err != nil {
		return nil, err
	}
	var current *id.ID
	var currentNetwork, currentTarget id.ID
	if err := q.QueryRow(ctx, `SELECT network_id,wallet_id,network_operation_id FROM reg_crypto_account_activations WHERE id=$1 FOR UPDATE`, activationID).Scan(&currentNetwork, &currentTarget, &current); err != nil {
		return nil, err
	}
	if currentNetwork != network || currentTarget != target || (existing == nil) != (current == nil) || (existing != nil && *existing != *current) {
		return nil, apperror.NewConflict("activation resource identity changed")
	}
	return current, nil
}

func (ActivationWorkflowStore) LinkOperation(ctx context.Context, activationID, operationID id.ID) error {
	tag, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
			UPDATE reg_crypto_account_activations
			SET network_operation_id=$2,
			    status='broadcast', broadcast_at=COALESCE(broadcast_at,NOW()),
			    error_message='', activation_next_attempt_at=NOW()+interval '30 seconds',
			    activation_claimed_by='', activation_claim_token=NULL,
			    activation_lease_expires_at=NULL, updated_at=NOW()
			WHERE id=$1 AND (network_operation_id IS NULL OR network_operation_id=$2)
		`, activationID, operationID)
	if err != nil {
		return fmt.Errorf("link activation document: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("activation document link mismatch")
	}
	return nil
}

func (ActivationWorkflowStore) CompleteTechnicalState(ctx context.Context, activationID id.ID, status string, receiptStatus crypto.ReceiptStatus) error {

	var walletID id.ID
	if err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `
		UPDATE reg_crypto_account_activations
		SET status=CASE
		        WHEN status='active' AND $2='confirmed' THEN status
		        ELSE $2
		    END,
		    confirmed_at=COALESCE(confirmed_at,NOW()),
		    activation_cost_reserved_minor=0, activation_cost_reserved_at=NULL,
		    cost_status='recorded', cost_checked_at=NOW(), cost_error='',
		    cost_claim_token=NULL,cost_claimed_by='', cost_claimed_at=NULL, cost_lease_expires_at=NULL,
		    error_message=CASE WHEN $2='failed' THEN 'activation transaction reverted on-chain' ELSE '' END,
		    activation_claimed_by='', activation_claim_token=NULL,
		    activation_lease_expires_at=NULL, updated_at=NOW()
		WHERE id=$1
		RETURNING wallet_id
	`, activationID, status).Scan(&walletID); err != nil {
		return fmt.Errorf("complete activation workflow: %w", err)
	}
	if receiptStatus == crypto.ReceiptReverted {
		if _, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
			UPDATE cat_wallets
			SET chain_activation_status='failed', updated_at=NOW(), version=version+1
			WHERE id=$1
		`, walletID); err != nil {
			return fmt.Errorf("mark reverted activation wallet failed: %w", err)
		}
	}
	return nil
}
