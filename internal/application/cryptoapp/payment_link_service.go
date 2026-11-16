package cryptoapp

import (
	"context"
	"fmt"

	"metapus/internal/application/checkout"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto/publicref"
)

type PaymentLinkRepository interface {
	CreatePaymentLink(
		ctx context.Context,
		linkID id.ID,
		merchantID id.ID,
		tokenID id.ID,
		amount types.RawAmount,
		description string,
		reusable bool,
		maxUses int,
		ttlMinutes int,
		createdBy *id.ID,
		publicRef string,
	) error
}

type PaymentLinkService struct {
	repo PaymentLinkRepository
	refs *checkout.Service
}

type PaymentLinkServiceConfig struct {
	Repo PaymentLinkRepository
	Refs *checkout.Service
}

type CreatePaymentLinkInput struct {
	MerchantID  id.ID
	TokenID     id.ID
	Amount      types.RawAmount
	Description string
	Reusable    bool
	MaxUses     int
	TTLMinutes  int
	CreatedBy   *id.ID
}

type CreatePaymentLinkResult struct {
	ID        id.ID
	PublicRef string
	PayURL    string
}

func NewPaymentLinkService(cfg PaymentLinkServiceConfig) (*PaymentLinkService, error) {
	if cfg.Repo == nil {
		return nil, fmt.Errorf("payment link repository is required")
	}
	if cfg.Refs == nil {
		return nil, fmt.Errorf("checkout reference service is required")
	}
	return &PaymentLinkService{repo: cfg.Repo, refs: cfg.Refs}, nil
}

func (s *PaymentLinkService) Create(ctx context.Context, in CreatePaymentLinkInput) (*CreatePaymentLinkResult, error) {
	if err := validateCreatePaymentLinkInput(in); err != nil {
		return nil, err
	}

	linkID := id.New()
	publicRef := publicref.MustNewPaymentLink()

	ref, err := s.refs.Reserve(ctx, checkout.ReserveInput{
		Prefix:       publicref.PaymentLinkPrefix,
		PublicRef:    publicRef,
		ResourceType: checkout.ResourceTypePaymentLink,
		ResourceID:   linkID,
		MerchantID:   in.MerchantID,
	})
	if err != nil {
		return nil, err
	}
	publicRef = ref

	if err := s.repo.CreatePaymentLink(
		ctx, linkID, in.MerchantID, in.TokenID, in.Amount, in.Description,
		in.Reusable, in.MaxUses, in.TTLMinutes, in.CreatedBy, publicRef,
	); err != nil {
		_ = s.refs.Disable(ctx, publicRef)
		return nil, err
	}
	if err := s.refs.Activate(ctx, publicRef); err != nil {
		return nil, fmt.Errorf("activate payment link checkout reference: %w", err)
	}

	return &CreatePaymentLinkResult{
		ID:        linkID,
		PublicRef: publicRef,
		PayURL:    s.refs.PaymentLinkPayURL(publicRef),
	}, nil
}

func validateCreatePaymentLinkInput(in CreatePaymentLinkInput) error {
	if id.IsNil(in.MerchantID) {
		return apperror.NewValidation("merchant is required").WithDetail("field", "merchantId")
	}
	if id.IsNil(in.TokenID) {
		return apperror.NewValidation("token is required").WithDetail("field", "tokenId")
	}
	if !in.Amount.IsPositive() {
		return apperror.NewValidation("amount must be positive").WithDetail("field", "amount")
	}
	if in.TTLMinutes < 5 || in.TTLMinutes > 10080 {
		return apperror.NewValidation("ttlMinutes must be between 5 and 10080").WithDetail("field", "ttlMinutes")
	}
	if in.MaxUses < 0 {
		return apperror.NewValidation("maxUses must be non-negative").WithDetail("field", "maxUses")
	}
	if !in.Reusable && in.MaxUses != 1 {
		return apperror.NewValidation("single-use payment links must have maxUses=1").WithDetail("field", "maxUses")
	}
	return nil
}
