package cryptoapp

import (
	"context"
	"errors"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/core/types"
	"metapus/internal/domain"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/domain/documents/crypto_invoice"
)

type CheckoutPageData struct {
	ID                  id.ID
	Status              string
	AcceptsPayments     bool
	WalletAddress       string
	ExpectedAmount      types.RawAmount
	ReceivedAmount      types.RawAmount
	OverpaidAmount      types.RawAmount
	ExpiresAt           time.Time
	OrderID             string
	Description         string
	TokenSymbol         string
	TokenName           string
	DecimalPlaces       int
	NetworkName         string
	ExplorerURL         string
	ConfirmationsNeeded int
	MerchantName        string
	Confirmations       int
}

type CheckoutStatus struct {
	Status          string
	AcceptsPayments bool
	ReceivedAmount  types.RawAmount
	OverpaidAmount  types.RawAmount
	Confirmations   int
	DecimalPlaces   int
}

type RedeemablePaymentLink struct {
	ID          id.ID
	MerchantID  id.ID
	TokenID     id.ID
	Amount      types.RawAmount
	Description string
	MaxUses     int
	CurrentUses int
	Status      string
	TTLMinutes  int
}

type CheckoutPageRepository interface {
	GetCheckoutPage(context.Context, string) (CheckoutPageData, error)
	GetCheckoutStatus(context.Context, string) (CheckoutStatus, error)
	RecordInvoiceViewed(context.Context, id.ID) error
	LockPaymentLinkForRedemption(context.Context, string) (RedeemablePaymentLink, error)
	MarkPaymentLinkRedeemed(context.Context, id.ID) (bool, error)
}

type CheckoutPageService struct {
	repo     CheckoutPageRepository
	invoices domain.DocumentService[*crypto_invoice.CryptoInvoice]
	settings MerchantInvoiceSettingsReader
	now      func() time.Time
}

func NewCheckoutPageService(repo CheckoutPageRepository, invoices domain.DocumentService[*crypto_invoice.CryptoInvoice], settings ...MerchantInvoiceSettingsReader) *CheckoutPageService {
	if repo == nil || invoices == nil {
		panic("cryptoapp.NewCheckoutPageService: repository and invoice service are required")
	}
	service := &CheckoutPageService{repo: repo, invoices: invoices, now: time.Now}
	if len(settings) > 0 {
		service.settings = settings[0]
	}
	return service
}

func (s *CheckoutPageService) GetInvoice(ctx context.Context, publicRef string) (CheckoutPageData, error) {
	data, err := s.repo.GetCheckoutPage(ctx, publicRef)
	if err != nil {
		return CheckoutPageData{}, err
	}
	// View analytics is explicitly non-financial and best-effort.
	_ = s.repo.RecordInvoiceViewed(ctx, data.ID)
	return data, nil
}

func (s *CheckoutPageService) GetStatus(ctx context.Context, publicRef string) (CheckoutStatus, error) {
	return s.repo.GetCheckoutStatus(ctx, publicRef)
}

func (s *CheckoutPageService) RedeemPaymentLink(ctx context.Context, linkRef string) (string, CheckoutPageData, error) {
	txm, err := tenant.GetTxManager(ctx)
	if err != nil {
		return "", CheckoutPageData{}, apperror.NewInternal(err)
	}
	var invoiceRef string
	err = txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		link, err := s.repo.LockPaymentLinkForRedemption(txCtx, linkRef)
		if err != nil {
			return err
		}
		if link.Status != "active" {
			return apperror.NewConflict("payment link is not active").WithDetail("paymentLink", linkRef)
		}
		if link.MaxUses > 0 && link.CurrentUses >= link.MaxUses {
			return apperror.NewConflict("payment link usage limit reached").WithDetail("paymentLink", linkRef)
		}
		if link.TTLMinutes < 5 || link.TTLMinutes > 10080 {
			return apperror.NewValidation("payment link ttlMinutes must be between 5 and 10080").WithDetail("paymentLink", linkRef)
		}

		invoice := crypto_invoice.NewCryptoInvoice(link.MerchantID, link.TokenID, link.Amount)
		if s.settings != nil {
			settings, settingsErr := s.settings.GetMerchantInvoiceSettings(txCtx, link.MerchantID)
			if settingsErr != nil {
				return settingsErr
			}
			invoice.SinglePayment = settings.SinglePaymentPerInvoice
		}
		invoice.Status = crypto_invoice.InvoiceStatusCreated
		invoice.ExpiresAt = s.now().Add(time.Duration(link.TTLMinutes) * time.Minute)
		invoice.TTLMinutes = link.TTLMinutes
		invoice.Description = link.Description
		if err := s.invoices.Create(txCtx, invoice); err != nil {
			if appErr, ok := apperror.AsAppError(err); ok && appErr.Code == wallet.CodeWalletPoolExhausted {
				return appErr.WithDetail("paymentLink", linkRef)
			}
			return err
		}
		updated, err := s.repo.MarkPaymentLinkRedeemed(txCtx, link.ID)
		if err != nil {
			return err
		}
		if !updated {
			return apperror.NewConflict("payment link usage limit reached").WithDetail("paymentLink", linkRef)
		}
		invoiceRef = invoice.PublicRef
		return nil
	})
	if err != nil {
		return "", CheckoutPageData{}, err
	}
	data, err := s.GetInvoice(ctx, invoiceRef)
	if err != nil {
		return "", CheckoutPageData{}, errors.Join(errors.New("created invoice checkout cannot be read"), err)
	}
	return invoiceRef, data, nil
}
