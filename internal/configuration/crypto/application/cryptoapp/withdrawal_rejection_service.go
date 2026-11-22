package cryptoapp

import (
	"context"
	"strings"

	"metapus/internal/configuration/crypto/domain/documents/withdrawal_request"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/tenant"
)

type AdminWithdrawalRejectionRepository interface {
	LockWithdrawalRequestForAdminRejection(context.Context, id.ID) (id.ID, int, withdrawal_request.Status, error)
	RejectAdminWithdrawalRequest(context.Context, id.ID, id.ID, string) error
}

// WithdrawalRejectionService restores the debit-first reservation and changes
// the workflow state in one tenant transaction.
type WithdrawalRejectionService struct {
	repo        AdminWithdrawalRejectionRepository
	compensator MerchantBalanceCompensator
}

func NewWithdrawalRejectionService(repo AdminWithdrawalRejectionRepository, compensator MerchantBalanceCompensator) *WithdrawalRejectionService {
	if repo == nil || compensator == nil {
		panic("cryptoapp.NewWithdrawalRejectionService: dependencies are required")
	}
	return &WithdrawalRejectionService{repo: repo, compensator: compensator}
}

func (s *WithdrawalRejectionService) Reject(ctx context.Context, requestID, actorID id.ID, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 2000 {
		return apperror.NewValidation("rejection reason must be 1-2000 bytes")
	}
	txm, err := tenant.GetTxManager(ctx)
	if err != nil {
		return apperror.NewInternal(err)
	}
	return txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		docID, postedVersion, status, err := s.repo.LockWithdrawalRequestForAdminRejection(txCtx, requestID)
		if err != nil {
			return err
		}
		if status != withdrawal_request.StatusPendingApproval && status != withdrawal_request.StatusPendingSignature {
			return apperror.NewConflict("withdrawal request is not pending approval")
		}
		if riskRepo, ok := s.repo.(interface {
			CancelUnsignedAMLWithdrawal(context.Context, id.ID, string, string) error
		}); ok {
			if err := riskRepo.CancelUnsignedAMLWithdrawal(txCtx, requestID, reason, actorID.String()); err != nil {
				return err
			}
		}
		if err := s.compensator.StornoMovements(txCtx, docID, postedVersion+1); err != nil {
			return err
		}
		return s.repo.RejectAdminWithdrawalRequest(txCtx, requestID, actorID, reason)
	})
}
