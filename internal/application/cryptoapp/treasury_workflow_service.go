package cryptoapp

import (
	"context"
	"fmt"
	"strings"
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
	Amount    types.RawAmount
	Reserved  types.RawAmount
	Available types.RawAmount
}

type TreasurySourceWallet struct {
	ID                   id.ID
	NetworkID            id.ID
	Name                 string
	Address              string
	LedgerAmount         types.RawAmount
	ReservedAmount       types.RawAmount
	AvailableAmount      types.RawAmount
	LastChainAmount      *string
	LastReconciliationAt *time.Time
}

type LockedTreasuryWithdrawal struct {
	Status    treasury_withdrawal.Status
	CreatedBy *id.ID
	TokenID   id.ID
	Amount    types.RawAmount
}

// RefundableReceipt is what is left of an unidentified receipt in suspense.
type RefundableReceipt struct {
	ReceiptID id.ID
	Number    string
	NetworkID id.ID
	TokenID   id.ID
	Remainder types.RawAmount
	Orphaned  bool
}

// ReceiptRefundDestination is the address one refund pays and the operator's
// evidence of the sender's right to it.
type ReceiptRefundDestination struct {
	ID         id.ID
	ReceiptID  id.ID
	NetworkID  id.ID
	Address    string
	AddressKey string
	Evidence   string
	CreatedBy  id.ID
}

// CreateReceiptRefundInput prepares the return of an unidentified receipt.
// Total is what leaves suspense; with the recipient paying gas, Withheld of it
// stays with the platform and the payer receives Total-Withheld.
type CreateReceiptRefundInput struct {
	ReceiptID      id.ID
	SourceWalletID id.ID
	DestAddress    string
	Evidence       string
	Reason         string
	Total          types.RawAmount
	GasPayer       treasury_withdrawal.GasPayer
	Withheld       types.RawAmount
}

type TreasuryWorkflowRepository interface {
	GetRefundableReceipt(context.Context, id.ID) (RefundableReceipt, error)
	LockReceiptRemainder(context.Context, id.ID) (ReceiptRemainder, error)
	CreateReceiptRefundDestination(context.Context, ReceiptRefundDestination) error
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
	ReserveFeeForTreasury(context.Context, id.ID, id.ID, types.RawAmount) error
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
	if doc.IsReceiptRefund() {
		return apperror.NewValidation("a receipt refund is prepared from its receipt")
	}
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
	return s.create(ctx, doc, actorID, nil)
}

// CreateReceiptRefund prepares the return of an unidentified receipt to its
// sender. Nothing moves yet: on approval by another operator the total leaves
// suspense into refund payable, and the payout closes the payable.
func (s *TreasuryWorkflowService) CreateReceiptRefund(ctx context.Context, input CreateReceiptRefundInput, actorID id.ID) (*treasury_withdrawal.TreasuryWithdrawal, error) {
	if id.IsNil(actorID) {
		return nil, apperror.NewUnauthorized("authenticated user id is required")
	}
	evidence := strings.TrimSpace(input.Evidence)
	if evidence == "" || len(evidence) > treasury_withdrawal.MaxRefundTextBytes {
		return nil, apperror.NewValidation("destination evidence is required and at most 2000 bytes").WithDetail("field", "evidence")
	}
	receipt, err := s.repo.GetRefundableReceipt(ctx, input.ReceiptID)
	if err != nil {
		return nil, err
	}
	if receipt.Orphaned {
		return nil, apperror.NewValidation("the receipt left the chain and cannot be refunded")
	}
	if input.Total.Cmp(receipt.Remainder) > 0 {
		return nil, apperror.NewValidation("refund exceeds what is left of the receipt").WithDetail("field", "amount")
	}
	normalized, err := s.addresses.ValidateAndNormalize(ctx, receipt.NetworkID, input.DestAddress)
	if err != nil {
		return nil, err
	}
	destination := ReceiptRefundDestination{ID: id.New(), ReceiptID: receipt.ReceiptID, NetworkID: receipt.NetworkID,
		Address: normalized.Address, AddressKey: normalized.CanonicalKey, Evidence: evidence, CreatedBy: actorID}
	doc, err := treasury_withdrawal.NewReceiptRefund(receipt.ReceiptID, receipt.TokenID, input.SourceWalletID, destination.ID,
		normalized.Address, input.Total, input.Withheld, input.GasPayer, input.Reason)
	if err != nil {
		return nil, err
	}
	if err := s.repo.ValidateTreasuryWithdrawalContext(ctx, doc, normalized.CanonicalKey); err != nil {
		return nil, err
	}
	err = s.create(ctx, doc, actorID, func(txCtx context.Context) error {
		remainder, err := s.repo.LockReceiptRemainder(txCtx, receipt.ReceiptID)
		if err != nil {
			return err
		}
		if err := remainder.attributable(); err != nil {
			return err
		}
		if remainder.AwaitingDecision {
			return errReceiptAwaitsDecision
		}
		if doc.RefundTotal().Cmp(remainder.Amount) > 0 {
			return apperror.NewValidation("refund exceeds what is left of the receipt").WithDetail("field", "amount")
		}
		return s.repo.CreateReceiptRefundDestination(txCtx, destination)
	})
	if err != nil {
		return nil, err
	}
	return doc, nil
}

// create persists the document with its approval signing job; before runs
// first in the same transaction.
func (s *TreasuryWorkflowService) create(ctx context.Context, doc *treasury_withdrawal.TreasuryWithdrawal, actorID id.ID, before func(context.Context) error) error {
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
		if before != nil {
			if err := before(txCtx); err != nil {
				return err
			}
		}
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
