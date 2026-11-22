// Package cryptoregisters assembles the crypto configuration's accumulation
// registers over PostgreSQL and installs them into a posting engine.
package cryptoregisters

import (
	"metapus/internal/configuration/crypto/domain/registers/crypto_balance"
	"metapus/internal/configuration/crypto/domain/registers/crypto_fee"
	"metapus/internal/configuration/crypto/domain/registers/crypto_merchant_balance"
	"metapus/internal/configuration/crypto/domain/registers/crypto_platform_cost"
	"metapus/internal/configuration/crypto/domain/registers/crypto_provider_balance"
	"metapus/internal/configuration/crypto/domain/registers/crypto_receipt_suspense"
	"metapus/internal/configuration/crypto/domain/registers/cryptoposting"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_register_repo"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/pkg/sdk/entity"
	"metapus/pkg/sdk/posting"
)

// Registers holds the crypto register services backed by PostgreSQL.
type Registers struct {
	Balance         *crypto_balance.Service
	Fee             *crypto_fee.Service
	MerchantBalance *crypto_merchant_balance.Service
	PlatformCost    *crypto_platform_cost.Service
	ProviderBalance *crypto_provider_balance.Service
	ReceiptSuspense *crypto_receipt_suspense.Service
}

// New creates the crypto register services.
func New() *Registers {
	return &Registers{
		Balance:         crypto_balance.NewService(crypto_register_repo.NewCryptoBalanceRepo()),
		Fee:             crypto_fee.NewService(crypto_register_repo.NewCryptoFeeRepo()),
		MerchantBalance: crypto_merchant_balance.NewService(crypto_register_repo.NewCryptoMerchantBalanceRepo()),
		PlatformCost:    crypto_platform_cost.NewService(crypto_register_repo.NewCryptoPlatformCostRepo()),
		ProviderBalance: crypto_provider_balance.NewService(crypto_register_repo.NewCryptoProviderBalanceRepo()),
		ReceiptSuspense: crypto_receipt_suspense.NewService(crypto_register_repo.NewCryptoReceiptSuspenseRepo()),
	}
}

// InstallPosting adds the crypto register visitors and recorders, plus the
// block-ingestion evidence check, to engine.
func (r *Registers) InstallPosting(engine *posting.Engine) []entity.MovementProvider {
	engine.AddVisitor(&cryptoposting.CryptoBalanceVisitor{})
	engine.AddVisitor(&cryptoposting.CryptoFeeVisitor{})
	engine.AddVisitor(&cryptoposting.CryptoMerchantBalanceVisitor{})
	engine.AddVisitor(&cryptoposting.CryptoPlatformCostVisitor{})
	engine.AddVisitor(&cryptoposting.CryptoProviderBalanceVisitor{})
	engine.AddVisitor(&cryptoposting.CryptoReceiptSuspenseVisitor{})

	engine.AddRecorder(cryptoposting.NewCryptoBalanceRecorder(r.Balance))
	engine.AddRecorder(cryptoposting.NewCryptoFeeRecorder(r.Fee))
	engine.AddRecorder(cryptoposting.NewCryptoMerchantBalanceRecorder(r.MerchantBalance))
	engine.AddRecorder(cryptoposting.NewCryptoPlatformCostRecorder(r.PlatformCost))
	engine.AddRecorder(cryptoposting.NewCryptoProviderBalanceRecorder(r.ProviderBalance))
	engine.AddRecorder(cryptoposting.NewCryptoReceiptSuspenseRecorder(r.ReceiptSuspense))
	engine.ValidateInTransaction(crypto_repo.NewBlockIngestionRepo().ValidatePosting)

	return []entity.MovementProvider{
		r.Balance,
		r.Fee,
		r.MerchantBalance,
		r.PlatformCost,
		r.ProviderBalance,
		r.ReceiptSuspense,
	}
}

var _ posting.Module = (*Registers)(nil)
