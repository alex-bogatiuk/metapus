package portalapp

import (
	"context"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/tx"
)

type SettingsRepository interface {
	GetMerchantSettings(context.Context, id.ID) (*PortalSettingsResponse, error)
	UpdateMerchantSettings(context.Context, id.ID, UpdatePortalSettingsRequest) error
}
type SinglePaymentActivator interface {
	EnableSinglePaymentForActiveInvoices(context.Context, id.ID) error
}
type SettingsService struct {
	repo     SettingsRepository
	invoices SinglePaymentActivator
	txm      tx.Manager
}

func NewSettingsService(repo SettingsRepository, invoices SinglePaymentActivator, txm tx.Manager) *SettingsService {
	return &SettingsService{repo, invoices, txm}
}
func (s *SettingsService) Update(ctx context.Context, merchantID id.ID, request UpdatePortalSettingsRequest) (*PortalSettingsResponse, error) {
	var result *PortalSettingsResponse
	err := s.txm.RunInTransaction(ctx, func(ctx context.Context) error {
		previous, err := s.repo.GetMerchantSettings(ctx, merchantID)
		if err != nil {
			return err
		}
		if err := s.repo.UpdateMerchantSettings(ctx, merchantID, request); err != nil {
			return err
		}
		if request.SinglePaymentPerInvoice != nil && *request.SinglePaymentPerInvoice && !previous.SinglePaymentPerInvoice {
			if err := s.invoices.EnableSinglePaymentForActiveInvoices(ctx, merchantID); err != nil {
				return err
			}
		}
		result, err = s.repo.GetMerchantSettings(ctx, merchantID)
		return err
	})
	return result, err
}
