package content

import (
	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/numerator"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/infrastructure/storage/postgres/crypto_catalog_repo"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
)

// NewInvoiceWalletLease wires the invoice wallet-lease step to the Postgres
// stores. The ERP CryptoInvoice registration and the public merchant API both
// use it, so every invoice creation path leases its wallet the same way.
// The stores take their querier from the request context, so one instance is
// safe to share across tenants and requests.
func NewInvoiceWalletLease() *cryptoapp.InvoiceWalletLease {
	return cryptoapp.NewInvoiceWalletLease(
		crypto_catalog_repo.NewTokenRepo(),
		wallet.NewService(crypto_catalog_repo.NewWalletRepo(), numerator.Noop()),
		crypto_repo.NewWalletAllocationRepo(),
	)
}
