package crypto_repo

import (
	"context"
	"fmt"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/cryptoapp"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
)

type MerchantInvoiceSettingsRepo struct{}

func NewMerchantInvoiceSettingsRepo() *MerchantInvoiceSettingsRepo {
	return &MerchantInvoiceSettingsRepo{}
}

func (r *MerchantInvoiceSettingsRepo) GetMerchantInvoiceSettings(ctx context.Context, merchantID id.ID) (cryptoapp.MerchantInvoiceSettings, error) {
	var settings cryptoapp.MerchantInvoiceSettings
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `
		SELECT COALESCE((attributes->>'defaultTtlMinutes')::INT,60),
		       COALESCE((attributes->>'singlePaymentPerInvoice')::BOOLEAN,FALSE)
		FROM cat_merchants WHERE id=$1 AND _deleted_at IS NULL
	`, merchantID).Scan(&settings.DefaultTTLMinutes, &settings.SinglePaymentPerInvoice)
	if err != nil {
		return settings, fmt.Errorf("get merchant invoice settings: %w", err)
	}
	return settings, nil
}
