package cryptoapp

import (
	"context"
	"fmt"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/documents/withdrawal_request"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
)

type WithdrawalApprovalIntentRepository interface {
	BuildWithdrawalIntent(context.Context, string, id.ID) (crypto.MoneyMovementIntent, error)
}

type WithdrawalApprovalRepository interface {
	LockWithdrawalRequestForApproval(context.Context, id.ID) (withdrawal_request.Status, error)
	ApproveWithdrawalRequest(context.Context, id.ID, id.ID) error
}

type WithdrawalApprovalService struct {
	repo      WithdrawalApprovalRepository
	intents   WithdrawalApprovalIntentRepository
	approvals *MoneyMovementApprovalService
}

func NewWithdrawalApprovalService(repo WithdrawalApprovalRepository, intents WithdrawalApprovalIntentRepository, approvals *MoneyMovementApprovalService) *WithdrawalApprovalService {
	if repo == nil || intents == nil || approvals == nil {
		panic("cryptoapp.NewWithdrawalApprovalService: all dependencies are required")
	}
	return &WithdrawalApprovalService{repo: repo, intents: intents, approvals: approvals}
}

func (s *WithdrawalApprovalService) Approve(ctx context.Context, requestID, actorID id.ID) error {
	tenantID := tenant.GetTenantID(ctx)
	current, err := s.intents.BuildWithdrawalIntent(ctx, tenantID, requestID)
	if err != nil {
		if apperror.IsNotFound(err) {
			return apperror.NewConflict("withdrawal request cannot be approved").WithCause(err)
		}
		if _, ok := apperror.AsAppError(err); ok {
			return err
		}
		return apperror.NewInternal(err)
	}
	prepared, err := s.approvals.PrepareAdminApproval(ctx, withdrawal_request.DocumentType, requestID, current, actorID)
	if err != nil {
		return err
	}
	txm, err := tenant.GetTxManager(ctx)
	if err != nil {
		return apperror.NewInternal(err)
	}
	err = txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		status, err := s.repo.LockWithdrawalRequestForApproval(txCtx, requestID)
		if err != nil {
			return err
		}
		if status != withdrawal_request.StatusPendingApproval {
			return apperror.NewConflict("withdrawal request is not pending approval")
		}
		lockedCurrent, err := s.intents.BuildWithdrawalIntent(txCtx, tenant.GetTenantID(txCtx), requestID)
		if err != nil {
			return err
		}
		if err := s.approvals.VerifyLockedIntent(txCtx, withdrawal_request.DocumentType, requestID, lockedCurrent, prepared.IntentDigest); err != nil {
			return err
		}
		if err := s.approvals.SavePreparedApproval(txCtx, withdrawal_request.DocumentType, requestID, prepared); err != nil {
			return err
		}
		return s.repo.ApproveWithdrawalRequest(txCtx, requestID, actorID)
	})
	if err != nil {
		if _, ok := apperror.AsAppError(err); ok {
			return err
		}
		return apperror.NewInternal(fmt.Errorf("approve withdrawal request: %w", err))
	}
	return nil
}
