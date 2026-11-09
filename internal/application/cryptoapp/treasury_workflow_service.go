package cryptoapp

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/core/types"
	"metapus/internal/domain"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/treasury_withdrawal"
)

type TreasuryAddress struct {
	ID        id.ID
	NetworkID id.ID
	Address   string
	Label     string
	IsActive  bool
}

type TreasuryAvailability struct {
	TokenID   id.ID
	Amount    types.CryptoAmount
	Reserved  types.CryptoAmount
	Available types.CryptoAmount
}

type TreasurySourceWallet struct {
	ID                   id.ID
	NetworkID            id.ID
	Name                 string
	Address              string
	LedgerAmount         types.CryptoAmount
	ReservedAmount       types.CryptoAmount
	AvailableAmount      types.CryptoAmount
	LastChainAmount      *string
	LastReconciliationAt *time.Time
}

type LockedTreasuryWithdrawal struct {
	Status    treasury_withdrawal.Status
	CreatedBy *id.ID
	TokenID   id.ID
	Amount    types.CryptoAmount
}

type TreasuryWorkflowRepository interface {
	UpsertTreasuryAddress(context.Context, id.ID, string, string, string, id.ID) (id.ID, error)
	ListTreasuryAddresses(context.Context) ([]TreasuryAddress, error)
	GetTreasuryWithdrawalNetwork(context.Context, id.ID) (id.ID, error)
	ValidateTreasuryWithdrawalContext(context.Context, *treasury_withdrawal.TreasuryWithdrawal, string) error
	GetTreasuryAvailability(context.Context, id.ID) (TreasuryAvailability, error)
	ListTreasurySourceWallets(context.Context, id.ID) ([]TreasurySourceWallet, error)
	LockTreasuryWithdrawal(context.Context, id.ID) (LockedTreasuryWithdrawal, error)
	MarkTreasuryWithdrawalApproved(context.Context, id.ID, id.ID) error
	MarkTreasuryWithdrawalRejected(context.Context, id.ID, id.ID, string) error
}

type TreasuryIntentRepository interface {
	BuildTreasuryIntent(context.Context, string, id.ID) (crypto.MoneyMovementIntent, error)
	BuildTreasuryIntentForCreate(context.Context, string, *treasury_withdrawal.TreasuryWithdrawal) (crypto.MoneyMovementIntent, error)
	EnqueueApprovalSigning(context.Context, crypto.MoneyMovementIntent) error
}

type TreasuryReservationRepository interface {
	ReserveFeeForTreasury(context.Context, id.ID, id.ID, types.CryptoAmount) error
	ReleaseTreasuryReservations(context.Context, id.ID) error
}

// TreasuryWorkflowService is the single application transaction boundary for
// treasury address and withdrawal financial commands.
type TreasuryWorkflowService struct {
	documents    domain.DocumentService[*treasury_withdrawal.TreasuryWithdrawal]
	repo         TreasuryWorkflowRepository
	intents      TreasuryIntentRepository
	reservations TreasuryReservationRepository
	approvals    *MoneyMovementApprovalService
	addresses    ChainAddressPolicy
}

func NewTreasuryWorkflowService(
	documents domain.DocumentService[*treasury_withdrawal.TreasuryWithdrawal],
	repo TreasuryWorkflowRepository,
	intents TreasuryIntentRepository,
	reservations TreasuryReservationRepository,
	approvals *MoneyMovementApprovalService,
	addresses ChainAddressPolicy,
) *TreasuryWorkflowService {
	if documents == nil || repo == nil || intents == nil || reservations == nil || approvals == nil || addresses == nil {
		panic("cryptoapp.NewTreasuryWorkflowService: all dependencies are required")
	}
	return &TreasuryWorkflowService{documents: documents, repo: repo, intents: intents, reservations: reservations, approvals: approvals, addresses: addresses}
}

func (s *TreasuryWorkflowService) CreateAddress(ctx context.Context, networkID id.ID, address, label string, actorID id.ID) (id.ID, error) {
	normalized, err := s.addresses.ValidateAndNormalize(ctx, networkID, address)
	if err != nil {
		return id.Nil(), err
	}
	txm, err := tenant.GetTxManager(ctx)
	if err != nil {
		return id.Nil(), apperror.NewInternal(err)
	}
	var addressID id.ID
	err = txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		var writeErr error
		addressID, writeErr = s.repo.UpsertTreasuryAddress(txCtx, networkID, normalized.Address, normalized.CanonicalKey, label, actorID)
		return writeErr
	})
	return addressID, err
}

func (s *TreasuryWorkflowService) ListAddresses(ctx context.Context) ([]TreasuryAddress, error) {
	return s.repo.ListTreasuryAddresses(ctx)
}

func (s *TreasuryWorkflowService) GetAvailability(ctx context.Context, tokenID id.ID) (TreasuryAvailability, error) {
	return s.repo.GetTreasuryAvailability(ctx, tokenID)
}

func (s *TreasuryWorkflowService) ListSourceWallets(ctx context.Context, tokenID id.ID) ([]TreasurySourceWallet, error) {
	return s.repo.ListTreasurySourceWallets(ctx, tokenID)
}

func (s *TreasuryWorkflowService) Create(ctx context.Context, doc *treasury_withdrawal.TreasuryWithdrawal, actorID id.ID) error {
	networkID, err := s.repo.GetTreasuryWithdrawalNetwork(ctx, doc.TokenID)
	if err != nil {
		return apperror.NewValidation("invalid treasury token").WithCause(err)
	}
	normalized, err := s.addresses.ValidateAndNormalize(ctx, networkID, doc.DestAddress)
	if err != nil {
		return err
	}
	doc.DestAddress = normalized.Address
	if err := s.repo.ValidateTreasuryWithdrawalContext(ctx, doc, normalized.CanonicalKey); err != nil {
		return err
	}
	intent, err := s.intents.BuildTreasuryIntentForCreate(ctx, tenant.GetTenantID(ctx), doc)
	if err != nil {
		return err
	}
	doc.Status = treasury_withdrawal.StatusPendingSignature
	if !id.IsNil(actorID) {
		doc.CreatedBy = actorID
		doc.UpdatedBy = actorID
	}
	txm, err := tenant.GetTxManager(ctx)
	if err != nil {
		return apperror.NewInternal(err)
	}
	return txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		if err := s.documents.Create(txCtx, doc); err != nil {
			return err
		}
		return s.intents.EnqueueApprovalSigning(txCtx, intent)
	})
}

func (s *TreasuryWorkflowService) Approve(ctx context.Context, withdrawalID, approverID id.ID) error {
	tenantID := tenant.GetTenantID(ctx)
	current, err := s.intents.BuildTreasuryIntent(ctx, tenantID, withdrawalID)
	if err != nil {
		return err
	}
	prepared, err := s.approvals.PrepareAdminApproval(ctx, treasury_withdrawal.DocumentType, withdrawalID, current, approverID)
	if err != nil {
		return err
	}
	txm, err := tenant.GetTxManager(ctx)
	if err != nil {
		return apperror.NewInternal(err)
	}
	return txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		locked, err := s.repo.LockTreasuryWithdrawal(txCtx, withdrawalID)
		if err != nil {
			return err
		}
		if locked.Status != treasury_withdrawal.StatusPendingApproval {
			return apperror.NewValidation("treasury withdrawal is not pending approval")
		}
		if locked.CreatedBy != nil && !id.IsNil(*locked.CreatedBy) && *locked.CreatedBy == approverID {
			return apperror.NewValidation("maker-checker violation: creator cannot approve treasury withdrawal")
		}
		lockedCurrent, err := s.intents.BuildTreasuryIntent(txCtx, tenant.GetTenantID(txCtx), withdrawalID)
		if err != nil {
			return err
		}
		if err := s.approvals.VerifyLockedIntent(txCtx, treasury_withdrawal.DocumentType, withdrawalID, lockedCurrent, prepared.IntentDigest); err != nil {
			return err
		}
		if err := s.approvals.SavePreparedApproval(txCtx, treasury_withdrawal.DocumentType, withdrawalID, prepared); err != nil {
			return err
		}
		if err := s.reservations.ReserveFeeForTreasury(txCtx, withdrawalID, locked.TokenID, locked.Amount); err != nil {
			return err
		}
		return s.repo.MarkTreasuryWithdrawalApproved(txCtx, withdrawalID, approverID)
	})
}

func (s *TreasuryWorkflowService) Reject(ctx context.Context, withdrawalID, actorID id.ID, reason string) error {
	if id.IsNil(actorID) {
		return apperror.NewUnauthorized("authenticated user id is required")
	}
	txm, err := tenant.GetTxManager(ctx)
	if err != nil {
		return apperror.NewInternal(err)
	}
	return txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		if err := s.reservations.ReleaseTreasuryReservations(txCtx, withdrawalID); err != nil {
			return err
		}
		if err := s.repo.MarkTreasuryWithdrawalRejected(txCtx, withdrawalID, actorID, reason); err != nil {
			return fmt.Errorf("reject treasury withdrawal: %w", err)
		}
		return nil
	})
}
