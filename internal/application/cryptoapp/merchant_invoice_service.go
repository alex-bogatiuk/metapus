package cryptoapp

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/core/types"
	"metapus/internal/domain"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_invoice"
	"metapus/internal/domain/eventlog"
)

// MerchantInvoiceService coordinates merchant invoice creation, cancellation,
// and order reference binding.
type MerchantInvoiceService struct {
	invoiceService      domain.DocumentService[*crypto_invoice.CryptoInvoice]
	invoiceRepo         crypto_invoice.Repository
	referenceRepo       crypto_invoice.ReferenceRepository
	events              eventlog.Writer
	settings            MerchantInvoiceSettingsReader
	invoiceLifecycle    MerchantInvoiceCancellationLifecycle
	activationLifecycle InvoiceActivationLifecycle
	checkoutRefs        *CheckoutInvoiceService
	isUniqueViolation   func(error) bool
	now                 func() time.Time
}

type MerchantInvoiceSettings struct {
	DefaultTTLMinutes       int
	SinglePaymentPerInvoice bool
}

type MerchantInvoiceSettingsReader interface {
	GetMerchantInvoiceSettings(context.Context, id.ID) (MerchantInvoiceSettings, error)
}

type MerchantInvoiceCancellationLifecycle interface {
	CancelInTransaction(context.Context, id.ID) (bool, error)
}

type InvoiceActivationLifecycle interface {
	SyncInTransaction(context.Context, id.ID) (bool, error)
}

type merchantInvoiceActivationRepo interface {
	ListActiveMerchantInvoiceIDs(context.Context, id.ID) ([]id.ID, error)
	FirstTwoPaymentIDs(context.Context, id.ID) ([]id.ID, error)
}

type MerchantInvoiceServiceConfig struct {
	InvoiceService      domain.DocumentService[*crypto_invoice.CryptoInvoice]
	InvoiceRepo         crypto_invoice.Repository
	ReferenceRepo       crypto_invoice.ReferenceRepository
	Events              eventlog.Writer
	Settings            MerchantInvoiceSettingsReader
	InvoiceLifecycle    MerchantInvoiceCancellationLifecycle
	ActivationLifecycle InvoiceActivationLifecycle
	CheckoutRefs        *CheckoutInvoiceService
	IsUniqueViolation   func(error) bool
	Now                 func() time.Time
}

type CreateMerchantInvoiceInput struct {
	MerchantID            id.ID
	TokenID               id.ID
	Amount                types.CryptoAmount
	APIKeyID              *id.ID
	CreatedByID           id.ID
	CreationSource        crypto_invoice.CreationSource
	ExpiresAt             time.Time
	TTLMinutes            int
	OrderID               string
	ExternalID            string
	Description           string
	NotificationURL       string
	ReturnURL             string
	Metadata              map[string]string
	RequiredConfirmations int
	FinalityMode          string
	CustomerID            string
}

type CreateMerchantInvoiceResult struct {
	Invoice *crypto_invoice.CryptoInvoice
	Created bool
}

func NewMerchantInvoiceService(cfg MerchantInvoiceServiceConfig) (*MerchantInvoiceService, error) {
	if cfg.InvoiceService == nil {
		return nil, fmt.Errorf("invoice service is required")
	}
	if cfg.InvoiceRepo == nil {
		return nil, fmt.Errorf("invoice repository is required")
	}
	if cfg.IsUniqueViolation == nil {
		return nil, fmt.Errorf("unique violation detector is required")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}

	return &MerchantInvoiceService{
		invoiceService:      cfg.InvoiceService,
		invoiceRepo:         cfg.InvoiceRepo,
		referenceRepo:       cfg.ReferenceRepo,
		events:              cfg.Events,
		settings:            cfg.Settings,
		invoiceLifecycle:    cfg.InvoiceLifecycle,
		activationLifecycle: cfg.ActivationLifecycle,
		checkoutRefs:        cfg.CheckoutRefs,
		isUniqueViolation:   cfg.IsUniqueViolation,
		now:                 cfg.Now,
	}, nil
}

// CancelMerchantInvoice is the sole application boundary for merchant
// cancellation. The lifecycle transition, wallet row lock and allocation
// release commit atomically; the HTTP adapter never orchestrates the FSM.
func (s *MerchantInvoiceService) CancelMerchantInvoice(ctx context.Context, merchantID, invoiceID id.ID) error {
	if id.IsNil(merchantID) || id.IsNil(invoiceID) {
		return apperror.NewValidation("merchant and invoice are required")
	}
	if s.invoiceLifecycle == nil {
		return apperror.NewInternal(errors.New("invoice cancellation is not configured"))
	}
	invoice, err := s.invoiceService.GetByID(ctx, invoiceID)
	if err != nil {
		return err
	}
	if invoice.MerchantID != merchantID {
		return apperror.NewNotFound("invoice", invoiceID.String())
	}

	txm, err := tenant.GetTxManager(ctx)
	if err != nil {
		return apperror.NewInternal(err)
	}
	err = txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		cancelled, cancelErr := s.invoiceLifecycle.CancelInTransaction(txCtx, invoiceID)
		if cancelErr != nil {
			return cancelErr
		}
		if !cancelled {
			return nil
		}
		if lockErr := s.invoiceRepo.LockWalletForInvoice(txCtx, invoiceID); lockErr != nil {
			return lockErr
		}
		return s.invoiceRepo.ReleaseInvoiceWallet(txCtx, invoiceID, "cancelled")
	})
	if errors.Is(err, crypto.ErrInvoiceNotCancellable) {
		return apperror.NewConflict("invoice cannot be cancelled after finality")
	}
	return err
}

// EnableSinglePaymentForActiveInvoices is called in the same transaction as
// switching on the merchant setting. Invoices with multiple payments retain
// their multi-payment policy; a finalized single partial payment closes immediately.
func (s *MerchantInvoiceService) EnableSinglePaymentForActiveInvoices(ctx context.Context, merchantID id.ID) error {
	activationRepo, ok := s.invoiceRepo.(merchantInvoiceActivationRepo)
	if !ok || s.activationLifecycle == nil {
		return fmt.Errorf("single-payment activation is not configured")
	}
	ids, err := activationRepo.ListActiveMerchantInvoiceIDs(ctx, merchantID)
	if err != nil {
		return err
	}
	for _, invoiceID := range ids {
		invoice, err := s.invoiceRepo.GetByIDForUpdate(ctx, invoiceID)
		if err != nil {
			return err
		}
		if invoice.SinglePayment || invoice.MerchantID != merchantID ||
			(invoice.Status != crypto_invoice.InvoiceStatusCreated &&
				invoice.Status != crypto_invoice.InvoiceStatusPartiallyPaid &&
				invoice.Status != crypto_invoice.InvoiceStatusPaid) {
			continue
		}
		payments, err := activationRepo.FirstTwoPaymentIDs(ctx, invoiceID)
		if err != nil {
			return err
		}
		if len(payments) > 1 {
			continue
		}
		invoice.SinglePayment = true
		if len(payments) == 1 {
			invoice.SelectedPaymentID = &payments[0]
		}
		if err := s.invoiceService.Update(ctx, invoice); err != nil {
			return err
		}
		if len(payments) == 1 {
			closed, err := s.activationLifecycle.SyncInTransaction(ctx, invoiceID)
			if err != nil {
				return err
			}
			if closed {
				if err := s.invoiceRepo.LockWalletForInvoice(ctx, invoiceID); err != nil {
					return err
				}
				if err := s.invoiceRepo.ReleaseInvoiceWallet(ctx, invoiceID, "confirmed"); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (s *MerchantInvoiceService) CreateMerchantInvoice(ctx context.Context, in CreateMerchantInvoiceInput) (*CreateMerchantInvoiceResult, error) {
	implicitTTL := in.TTLMinutes == 0
	settings := MerchantInvoiceSettings{DefaultTTLMinutes: 60}
	if s.settings != nil {
		var err error
		settings, err = s.settings.GetMerchantInvoiceSettings(ctx, in.MerchantID)
		if err != nil {
			return nil, fmt.Errorf("load merchant invoice settings: %w", err)
		}
	}
	if in.TTLMinutes == 0 {
		in.TTLMinutes = settings.DefaultTTLMinutes
	}
	if in.ExpiresAt.IsZero() {
		in.ExpiresAt = s.now().UTC().Add(time.Duration(in.TTLMinutes) * time.Minute)
	}
	if err := s.validateMerchantInvoiceInput(in); err != nil {
		return nil, err
	}

	existing, err := s.findExisting(ctx, in)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if err := assertSameMerchantInvoicePayload(existing, in, implicitTTL); err != nil {
			return nil, err
		}
		if err := s.ensureCheckoutReference(ctx, existing); err != nil {
			return nil, err
		}
		return &CreateMerchantInvoiceResult{Invoice: existing, Created: false}, nil
	}

	inv := crypto_invoice.NewCryptoInvoice(in.MerchantID, in.TokenID, in.Amount)
	if in.APIKeyID != nil {
		keyID := *in.APIKeyID
		inv.APIKeyID = &keyID
	}
	inv.CreationSource = in.CreationSource
	if !id.IsNil(in.CreatedByID) {
		inv.CreatedBy = in.CreatedByID
		inv.UpdatedBy = in.CreatedByID
	}
	inv.ExpiresAt = in.ExpiresAt
	inv.TTLMinutes = in.TTLMinutes
	inv.SinglePayment = settings.SinglePaymentPerInvoice
	inv.OrderID = in.OrderID
	inv.ExternalID = in.ExternalID
	inv.Description = in.Description
	inv.NotificationURL = in.NotificationURL
	inv.ReturnURL = in.ReturnURL
	inv.Metadata = cloneMetadata(in.Metadata)
	inv.RequiredConfirmations = in.RequiredConfirmations
	inv.FinalityMode = in.FinalityMode
	inv.CustomerID = in.CustomerID

	if err := s.invoiceService.Create(ctx, inv); err != nil {
		if !s.isUniqueViolation(err) {
			return nil, err
		}
		existing, findErr := s.findExisting(ctx, in)
		if findErr != nil {
			return nil, findErr
		}
		if existing == nil {
			return nil, fmt.Errorf("invoice idempotency conflict was detected but existing invoice was not found: %w", err)
		}
		if payloadErr := assertSameMerchantInvoicePayload(existing, in, implicitTTL); payloadErr != nil {
			return nil, payloadErr
		}
		if err := s.ensureCheckoutReference(ctx, existing); err != nil {
			return nil, err
		}
		return &CreateMerchantInvoiceResult{Invoice: existing, Created: false}, nil
	}

	return &CreateMerchantInvoiceResult{Invoice: inv, Created: true}, nil
}

func (s *MerchantInvoiceService) ensureCheckoutReference(ctx context.Context, inv *crypto_invoice.CryptoInvoice) error {
	if s.checkoutRefs == nil {
		return nil
	}
	return s.checkoutRefs.EnsureActive(ctx, inv)
}

func (s *MerchantInvoiceService) validateMerchantInvoiceInput(in CreateMerchantInvoiceInput) error {
	if id.IsNil(in.MerchantID) {
		return apperror.NewValidation("merchant is required").WithDetail("field", "merchantId")
	}
	if id.IsNil(in.TokenID) {
		return apperror.NewValidation("token is required").WithDetail("field", "tokenId")
	}
	if !in.Amount.IsPositive() {
		return apperror.NewValidation("amount must be positive").WithDetail("field", "amount")
	}
	switch in.CreationSource {
	case crypto_invoice.CreationSourceMerchantAPI:
		if in.APIKeyID == nil || id.IsNil(*in.APIKeyID) {
			return apperror.NewValidation("API key is required").WithDetail("field", "apiKeyId")
		}
	case crypto_invoice.CreationSourcePortal:
		if id.IsNil(in.CreatedByID) {
			return apperror.NewValidation("portal user is required").WithDetail("field", "createdById")
		}
	default:
		return apperror.NewValidation("unsupported invoice creation source").WithDetail("field", "creationSource")
	}
	if in.ExpiresAt.IsZero() {
		return apperror.NewValidation("expiration time is required").WithDetail("field", "expiresAt")
	}
	if in.TTLMinutes < 5 || in.TTLMinutes > 10080 {
		return apperror.NewValidation("ttlMinutes must be between 5 and 10080").WithDetail("field", "ttlMinutes")
	}
	if !in.ExpiresAt.After(s.now()) {
		return apperror.NewValidation("expiration time must be in the future").WithDetail("field", "expiresAt")
	}
	return nil
}

func (s *MerchantInvoiceService) findExisting(ctx context.Context, in CreateMerchantInvoiceInput) (*crypto_invoice.CryptoInvoice, error) {
	var existing *crypto_invoice.CryptoInvoice

	if in.OrderID != "" {
		inv, err := s.invoiceRepo.FindByMerchantOrderID(ctx, in.MerchantID, in.OrderID)
		if err != nil {
			return nil, fmt.Errorf("find invoice by order id: %w", err)
		}
		existing = inv
	}

	if in.ExternalID != "" {
		inv, err := s.invoiceRepo.FindByMerchantExternalID(ctx, in.MerchantID, in.ExternalID)
		if err != nil {
			return nil, fmt.Errorf("find invoice by external id: %w", err)
		}
		if existing != nil && inv != nil && existing.ID != inv.ID {
			return nil, apperror.NewConflict("idempotency keys resolve to different invoices").
				WithDetail("orderId", in.OrderID).
				WithDetail("externalId", in.ExternalID)
		}
		if existing == nil {
			existing = inv
		}
	}

	return existing, nil
}

func assertSameMerchantInvoicePayload(inv *crypto_invoice.CryptoInvoice, in CreateMerchantInvoiceInput, implicitTTL bool) error {
	switch {
	case inv.MerchantID != in.MerchantID:
		return merchantInvoicePayloadConflict("merchantId")
	case inv.TokenID != in.TokenID:
		return merchantInvoicePayloadConflict("currency")
	case inv.ExpectedAmount.Cmp(in.Amount) != 0:
		return merchantInvoicePayloadConflict("amount")
	case inv.OrderID != in.OrderID:
		return merchantInvoicePayloadConflict("orderId")
	case !implicitTTL && inv.TTLMinutes != in.TTLMinutes:
		return merchantInvoicePayloadConflict("ttlMinutes")
	case inv.Description != in.Description:
		return merchantInvoicePayloadConflict("description")
	case inv.NotificationURL != in.NotificationURL:
		return merchantInvoicePayloadConflict("notificationUrl")
	case inv.ReturnURL != in.ReturnURL:
		return merchantInvoicePayloadConflict("returnUrl")
	case !maps.Equal(inv.Metadata, in.Metadata):
		return merchantInvoicePayloadConflict("metadata")
	case inv.CustomerID != in.CustomerID:
		return merchantInvoicePayloadConflict("customerId")
	default:
		return nil
	}
}

func merchantInvoicePayloadConflict(field string) error {
	return apperror.NewConflict("idempotency key payload mismatch").WithDetail("field", field)
}

func cloneMetadata(metadata map[string]string) map[string]string {
	if metadata == nil {
		return make(map[string]string)
	}
	cloned := make(map[string]string, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}
