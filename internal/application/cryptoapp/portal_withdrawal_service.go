package cryptoapp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/core/tenant"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/crypto/aml"
	"metapus/internal/domain/documents/withdrawal_request"
	"metapus/internal/domain/posting"
)

type PortalWithdrawalAddress struct {
	NetworkID id.ID
	Address   string
}

type PortalWithdrawalToken struct {
	NetworkID       id.ID
	NetworkCode     string
	DecimalPlaces   int
	TokenStandard   string
	ContractAddress string
}

type PortalWithdrawalRepository interface {
	AddWithdrawalAddress(context.Context, id.ID, id.ID, string, string, string) (id.ID, error)
	RemoveWithdrawalAddress(context.Context, id.ID, id.ID) error
	GetWithdrawalAddressContext(context.Context, id.ID, id.ID) (PortalWithdrawalAddress, error)
	GetWithdrawalTokenContext(context.Context, id.ID) (PortalWithdrawalToken, error)
	CreateWithdrawalRequest(context.Context, *withdrawal_request.WithdrawalRequest, aml.Class) error
	GetRefundablePayment(context.Context, id.ID, id.ID) (RefundablePayment, error)
	LockRefundablePayment(context.Context, id.ID, id.ID) (RefundablePayment, error)
	EnqueueApprovalSigning(context.Context, crypto.MoneyMovementIntent) error
	MarkWithdrawalRequestApprovedByPolicy(context.Context, id.ID) error
	LockWithdrawalRequestForRejection(context.Context, id.ID, id.ID) (id.ID, int, withdrawal_request.Status, error)
	RejectWithdrawalRequest(context.Context, id.ID, string) error
}

type FinancialPostingEngine interface {
	Post(context.Context, posting.Postable, func(context.Context) error) error
}

type MerchantBalanceCompensator interface {
	StornoMovements(context.Context, id.ID, int) error
}

type CreatePortalWithdrawalInput struct {
	MerchantID   id.ID
	TokenID      id.ID
	AddressID    id.ID
	Amount       string
	RiskClass    aml.Class
	RiskAccepted bool
	// RefundPaymentID makes the request a refund of that credited payment;
	// RefundReason is then required.
	RefundPaymentID *id.ID
	RefundReason    string
}

// RefundablePayment is a credited payment and what its refunds already hold
// or paid out. Refundable never exceeds the gross the payer sent.
type RefundablePayment struct {
	PaymentID     id.ID
	InvoiceID     id.ID
	InvoiceNumber string
	TokenID       id.ID
	FromAddress   string
	Amount        types.CryptoAmount
	Committed     types.CryptoAmount
}

func (p RefundablePayment) Refundable() types.CryptoAmount {
	if p.Committed.Cmp(p.Amount) >= 0 {
		return types.ZeroCryptoAmount()
	}
	return p.Amount.Sub(p.Committed)
}

type PortalRiskWithdrawalRepository interface {
	LockAMLWithdrawalScope(context.Context, id.ID, id.ID, id.ID) (bool, error)
	ReserveAMLWithdrawalSources(context.Context, crypto.MoneyMovementIntent, aml.WithdrawalRequest) (crypto.MoneyMovementIntent, error)
	CancelUnsignedAMLWithdrawal(context.Context, id.ID, string, string) error
	CancelAMLWithdrawalRemainder(context.Context, id.ID, id.ID, string) (types.CryptoAmount, error)
}

type CreatePortalWithdrawalResult struct {
	ID     id.ID
	Number string
	Status withdrawal_request.Status
}

// PortalWithdrawalService owns the debit-first financial command and its
// compensating rejection. HTTP only validates transport identity and maps DTOs.
type PortalWithdrawalService struct {
	repo        PortalWithdrawalRepository
	numerator   numerator.Generator
	posting     FinancialPostingEngine
	compensator MerchantBalanceCompensator
	approvals   *MoneyMovementApprovalService
	addresses   ChainAddressPolicy
	now         func() time.Time
}

func NewPortalWithdrawalService(
	repo PortalWithdrawalRepository,
	numeratorService numerator.Generator,
	postingEngine FinancialPostingEngine,
	compensator MerchantBalanceCompensator,
	approvals *MoneyMovementApprovalService,
	addresses ChainAddressPolicy,
) *PortalWithdrawalService {
	if repo == nil || numeratorService == nil || postingEngine == nil || compensator == nil || approvals == nil || addresses == nil {
		panic("cryptoapp.NewPortalWithdrawalService: all dependencies are required")
	}
	return &PortalWithdrawalService{repo: repo, numerator: numeratorService, posting: postingEngine, compensator: compensator, approvals: approvals, addresses: addresses, now: time.Now}
}

func (s *PortalWithdrawalService) AddAddress(ctx context.Context, merchantID, networkID id.ID, address, label string) (id.ID, error) {
	if id.IsNil(merchantID) || id.IsNil(networkID) {
		return id.Nil(), apperror.NewValidation("merchant and network are required")
	}
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
		addressID, writeErr = s.repo.AddWithdrawalAddress(txCtx, merchantID, networkID, normalized.Address, normalized.CanonicalKey, label)
		return writeErr
	})
	return addressID, err
}

func (s *PortalWithdrawalService) RemoveAddress(ctx context.Context, merchantID, addressID id.ID) error {
	if id.IsNil(merchantID) || id.IsNil(addressID) {
		return apperror.NewValidation("merchant and withdrawal address are required")
	}
	txm, err := tenant.GetTxManager(ctx)
	if err != nil {
		return apperror.NewInternal(err)
	}
	return txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		return s.repo.RemoveWithdrawalAddress(txCtx, merchantID, addressID)
	})
}

func (s *PortalWithdrawalService) Create(ctx context.Context, input CreatePortalWithdrawalInput) (CreatePortalWithdrawalResult, error) {
	address, err := s.repo.GetWithdrawalAddressContext(ctx, input.MerchantID, input.AddressID)
	if err != nil {
		return CreatePortalWithdrawalResult{}, apperror.NewValidation("address not found in whitelist").WithCause(err)
	}
	token, err := s.repo.GetWithdrawalTokenContext(ctx, input.TokenID)
	if err != nil {
		return CreatePortalWithdrawalResult{}, apperror.NewValidation("unknown token").WithCause(err)
	}
	if address.NetworkID != token.NetworkID {
		return CreatePortalWithdrawalResult{}, apperror.NewValidation("address is not whitelisted for this token's network")
	}
	amount, err := parseHumanTokenAmount(input.Amount, token.DecimalPlaces)
	if err != nil {
		return CreatePortalWithdrawalResult{}, err
	}

	doc := withdrawal_request.NewWithdrawalRequest(input.MerchantID, input.TokenID, input.AddressID, address.Address, amount)
	doc.Status = withdrawal_request.StatusPendingSignature
	if input.RefundPaymentID != nil {
		doc.MarkAsRefund(*input.RefundPaymentID, input.RefundReason)
	} else if strings.TrimSpace(input.RefundReason) != "" {
		return CreatePortalWithdrawalResult{}, apperror.NewValidation("refund reason requires the payment it returns").WithDetail("field", "refundPaymentId")
	}
	if err := doc.Validate(ctx); err != nil {
		return CreatePortalWithdrawalResult{}, err
	}
	number, err := s.numerator.GetNextNumber(ctx, numerator.DefaultConfig("WR"), nil, s.now())
	if err != nil {
		return CreatePortalWithdrawalResult{}, apperror.NewInternal(err)
	}
	doc.Number = number
	merchantID := input.MerchantID
	intent := crypto.MoneyMovementIntent{
		SchemaVersion: crypto.MoneyMovementIntentV1, TenantID: tenant.GetTenantID(ctx),
		IntentID: id.New().String(),
		Purpose:  crypto.SigningPurposeWithdrawal, BasisType: withdrawal_request.DocumentType, BasisID: doc.GetID(),
		MerchantID: &merchantID, NetworkCode: token.NetworkCode, TokenID: input.TokenID,
		TokenStandard: token.TokenStandard, ContractAddress: token.ContractAddress,
		AmountMinorUnits: amount, DestinationAddress: address.Address, AddressID: input.AddressID,
	}
	txm, err := tenant.GetTxManager(ctx)
	if err != nil {
		return CreatePortalWithdrawalResult{}, apperror.NewInternal(err)
	}
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if doc.IsRefund() {
			// Lock the payment before the debit, so refunds of one payment are
			// capped one after another; the database guard repeats the check.
			payment, err := s.repo.LockRefundablePayment(ctx, input.MerchantID, *doc.BasisID)
			if err != nil {
				return apperror.NewValidation("payment cannot be refunded: it is not a credited payment of this merchant").WithDetail("field", "refundPaymentId").WithCause(err)
			}
			if payment.TokenID != input.TokenID {
				return apperror.NewValidation("refund must be paid in the token of the payment").WithDetail("field", "tokenId")
			}
			if amount.Cmp(payment.Refundable()) > 0 {
				return apperror.NewValidation("refund exceeds what is left to refund of the payment").WithDetail("field", "amount")
			}
		}
		riskRepo, hasRisk := s.repo.(PortalRiskWithdrawalRepository)
		enabled := false
		if hasRisk {
			var lockErr error
			enabled, lockErr = riskRepo.LockAMLWithdrawalScope(ctx, input.MerchantID, token.NetworkID, input.TokenID)
			if lockErr != nil {
				return lockErr
			}
		}
		if enabled {
			if err := (aml.WithdrawalRequest{MerchantID: input.MerchantID, NetworkID: token.NetworkID, TokenID: input.TokenID, Class: input.RiskClass, Amount: amount, RiskAccepted: input.RiskAccepted}).Validate(); err != nil {
				return err
			}
		} else if input.RiskClass != "" || input.RiskAccepted {
			return apperror.NewValidation("risk-class withdrawals are not enabled for this asset")
		}
		return s.posting.Post(ctx, doc, func(txCtx context.Context) error {
			if err := s.repo.CreateWithdrawalRequest(txCtx, doc, input.RiskClass); err != nil {
				return err
			}
			if enabled {
				var planErr error
				intent, planErr = riskRepo.ReserveAMLWithdrawalSources(txCtx, intent, aml.WithdrawalRequest{MerchantID: input.MerchantID, NetworkID: token.NetworkID, TokenID: input.TokenID, Class: input.RiskClass, Amount: amount, RiskAccepted: input.RiskAccepted})
				if planErr != nil {
					return planErr
				}
			}
			return s.repo.EnqueueApprovalSigning(txCtx, intent)
		})
	})
	if err != nil {
		return CreatePortalWithdrawalResult{}, err
	}
	return CreatePortalWithdrawalResult{ID: doc.GetID(), Number: number, Status: withdrawal_request.StatusPendingSignature}, nil
}

// RefundablePayment reports what is left to refund of a merchant's payment.
func (s *PortalWithdrawalService) RefundablePayment(ctx context.Context, merchantID, paymentID id.ID) (RefundablePayment, error) {
	if id.IsNil(merchantID) || id.IsNil(paymentID) {
		return RefundablePayment{}, apperror.NewValidation("merchant and payment are required")
	}
	return s.repo.GetRefundablePayment(ctx, merchantID, paymentID)
}

func (s *PortalWithdrawalService) Reject(ctx context.Context, merchantID, requestID id.ID, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "Withdrawal cancelled by merchant"
	}
	if len(reason) > 500 {
		return apperror.NewValidation("cancellation reason exceeds 500 bytes")
	}
	txm, err := tenant.GetTxManager(ctx)
	if err != nil {
		return apperror.NewInternal(err)
	}
	return txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		docID, postedVersion, status, err := s.repo.LockWithdrawalRequestForRejection(txCtx, merchantID, requestID)
		if err != nil {
			return apperror.NewNotFound("withdrawal_request", requestID.String()).WithCause(err)
		}
		if status != withdrawal_request.StatusPendingApproval && status != withdrawal_request.StatusPendingSignature {
			if riskRepo, ok := s.repo.(PortalRiskWithdrawalRepository); ok {
				_, err := riskRepo.CancelAMLWithdrawalRemainder(txCtx, merchantID, requestID, reason)
				return err
			}
			return apperror.NewConflict("withdrawal request already processed (status: " + string(status) + ")")
		}
		if riskRepo, ok := s.repo.(PortalRiskWithdrawalRepository); ok {
			if err := riskRepo.CancelUnsignedAMLWithdrawal(txCtx, requestID, reason, "merchant"); err != nil {
				return err
			}
		}
		if err := s.compensator.StornoMovements(txCtx, docID, postedVersion+1); err != nil {
			return err
		}
		return s.repo.RejectWithdrawalRequest(txCtx, requestID, reason)
	})
}

func parseHumanTokenAmount(raw string, decimalPlaces int) (types.CryptoAmount, error) {
	human, err := decimal.NewFromString(raw)
	if err != nil || !human.IsPositive() {
		return types.ZeroCryptoAmount(), apperror.NewValidation("amount must be a positive decimal string")
	}
	minor := human.Shift(int32(decimalPlaces))
	if !minor.Equal(minor.Truncate(0)) {
		return types.ZeroCryptoAmount(), apperror.NewValidation("amount has too many decimal places for this token")
	}
	amount, err := types.NewCryptoAmountFromBigInt(minor.BigInt())
	if err != nil {
		return types.ZeroCryptoAmount(), apperror.NewValidation("amount must be a positive integer").WithCause(fmt.Errorf("convert minor units: %w", err))
	}
	return amount, nil
}
