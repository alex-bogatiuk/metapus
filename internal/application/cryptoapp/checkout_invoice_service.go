package cryptoapp

import (
	"context"
	"fmt"

	"metapus/internal/application/checkout"
	"metapus/internal/core/id"
	"metapus/internal/domain"
	"metapus/internal/domain/crypto/publicref"
	"metapus/internal/domain/documents/crypto_invoice"
)

// CheckoutInvoiceService decorates CryptoInvoice creation with public checkout
// reference saga: reserve meta ref -> create tenant invoice -> activate ref.
type CheckoutInvoiceService struct {
	next domain.DocumentService[*crypto_invoice.CryptoInvoice]
	refs *checkout.Service
}

func NewCheckoutInvoiceService(
	next domain.DocumentService[*crypto_invoice.CryptoInvoice],
	refs *checkout.Service,
) *CheckoutInvoiceService {
	if refs == nil {
		return &CheckoutInvoiceService{next: next}
	}
	return &CheckoutInvoiceService{next: next, refs: refs}
}

func (s *CheckoutInvoiceService) Create(ctx context.Context, inv *crypto_invoice.CryptoInvoice) error {
	return s.createWithCheckoutRef(ctx, inv, s.next.Create)
}

func (s *CheckoutInvoiceService) PostAndSave(ctx context.Context, inv *crypto_invoice.CryptoInvoice) error {
	return s.createWithCheckoutRef(ctx, inv, s.next.PostAndSave)
}

func (s *CheckoutInvoiceService) createWithCheckoutRef(
	ctx context.Context,
	inv *crypto_invoice.CryptoInvoice,
	create func(context.Context, *crypto_invoice.CryptoInvoice) error,
) error {
	if s.refs == nil {
		return create(ctx, inv)
	}

	if inv.PublicRef == "" {
		inv.PublicRef = publicref.MustNewInvoice()
	}
	if err := publicref.Validate(inv.PublicRef); err != nil {
		return err
	}

	ref, err := s.refs.Reserve(ctx, checkout.ReserveInput{
		Prefix:       publicref.InvoicePrefix,
		PublicRef:    inv.PublicRef,
		ResourceType: checkout.ResourceTypeInvoice,
		ResourceID:   inv.ID,
		MerchantID:   inv.MerchantID,
	})
	if err != nil {
		return err
	}
	inv.PublicRef = ref

	if err := create(ctx, inv); err != nil {
		_ = s.refs.Disable(ctx, inv.PublicRef)
		return err
	}
	if err := s.refs.Activate(ctx, inv.PublicRef); err != nil {
		return fmt.Errorf("activate invoice checkout reference: %w", err)
	}
	return nil
}

func (s *CheckoutInvoiceService) EnsureActive(ctx context.Context, inv *crypto_invoice.CryptoInvoice) error {
	if s.refs == nil || inv == nil {
		return nil
	}
	if inv.PublicRef == "" {
		return nil
	}
	return s.refs.EnsureActive(ctx, checkout.EnsureInput{
		PublicRef:    inv.PublicRef,
		ResourceType: checkout.ResourceTypeInvoice,
		ResourceID:   inv.ID,
		MerchantID:   inv.MerchantID,
	})
}

func (s *CheckoutInvoiceService) GetByID(ctx context.Context, invoiceID id.ID) (*crypto_invoice.CryptoInvoice, error) {
	return s.next.GetByID(ctx, invoiceID)
}

func (s *CheckoutInvoiceService) GetByIDForUpdate(ctx context.Context, invoiceID id.ID) (*crypto_invoice.CryptoInvoice, error) {
	return s.next.GetByIDForUpdate(ctx, invoiceID)
}

func (s *CheckoutInvoiceService) Update(ctx context.Context, inv *crypto_invoice.CryptoInvoice) error {
	return s.next.Update(ctx, inv)
}

func (s *CheckoutInvoiceService) Delete(ctx context.Context, invoiceID id.ID) error {
	var publicRef string
	if s.refs != nil {
		if inv, err := s.next.GetByID(ctx, invoiceID); err == nil {
			publicRef = inv.PublicRef
		}
	}
	if err := s.next.Delete(ctx, invoiceID); err != nil {
		return err
	}
	if publicRef != "" {
		_ = s.refs.Disable(ctx, publicRef)
	}
	return nil
}

func (s *CheckoutInvoiceService) Post(ctx context.Context, invoiceID id.ID) error {
	return s.next.Post(ctx, invoiceID)
}

func (s *CheckoutInvoiceService) Unpost(ctx context.Context, invoiceID id.ID) error {
	return s.next.Unpost(ctx, invoiceID)
}

func (s *CheckoutInvoiceService) UpdateAndRepost(ctx context.Context, inv *crypto_invoice.CryptoInvoice) error {
	return s.next.UpdateAndRepost(ctx, inv)
}

func (s *CheckoutInvoiceService) SetDeletionMark(ctx context.Context, invoiceID id.ID, marked bool) error {
	var publicRef string
	if s.refs != nil && marked {
		if inv, err := s.next.GetByID(ctx, invoiceID); err == nil {
			publicRef = inv.PublicRef
		}
	}
	if err := s.next.SetDeletionMark(ctx, invoiceID, marked); err != nil {
		return err
	}
	if publicRef != "" {
		_ = s.refs.Disable(ctx, publicRef)
	}
	return nil
}

func (s *CheckoutInvoiceService) List(ctx context.Context, filter domain.ListFilter) (domain.CursorListResult[*crypto_invoice.CryptoInvoice], error) {
	return s.next.List(ctx, filter)
}

func (s *CheckoutInvoiceService) ListIDs(ctx context.Context, filter domain.ListFilter, maxIDs int) ([]id.ID, error) {
	return s.next.ListIDs(ctx, filter, maxIDs)
}

var _ domain.DocumentService[*crypto_invoice.CryptoInvoice] = (*CheckoutInvoiceService)(nil)
