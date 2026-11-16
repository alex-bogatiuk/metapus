package cryptoapp

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/token"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_invoice"
)

// InvoiceTokenReader resolves the token an invoice is denominated in.
type InvoiceTokenReader interface {
	GetByID(ctx context.Context, tokenID id.ID) (*token.Token, error)
}

// InvoiceWalletLeaser leases a free pool wallet for one invoice.
type InvoiceWalletLeaser interface {
	LeaseForInvoice(ctx context.Context, merchantID, invoiceID, networkID, tokenID id.ID, expiresAt time.Time) (*wallet.Wallet, error)
}

// InvoiceWalletLease assigns a receiving pool wallet to a new invoice and
// records the transient allocation. Every invoice creation path (ERP documents
// and the public merchant API) must run it before the invoice is stored, or
// the invoice is saved without a wallet and can never be paid.
type InvoiceWalletLease struct {
	tokens      InvoiceTokenReader
	wallets     InvoiceWalletLeaser
	allocations crypto.WalletAllocationRepository
}

// NewInvoiceWalletLease builds the lease step from its storage ports.
func NewInvoiceWalletLease(tokens InvoiceTokenReader, wallets InvoiceWalletLeaser, allocations crypto.WalletAllocationRepository) *InvoiceWalletLease {
	return &InvoiceWalletLease{tokens: tokens, wallets: wallets, allocations: allocations}
}

// Apply leases a wallet for doc, sets doc.WalletID and opens the allocation.
// It runs inside the invoice creation transaction, so a failure rolls back
// the lease together with the invoice.
func (l *InvoiceWalletLease) Apply(ctx context.Context, doc *crypto_invoice.CryptoInvoice) error {
	tok, err := l.tokens.GetByID(ctx, doc.TokenID)
	if err != nil {
		return fmt.Errorf("resolve token for wallet lease: %w", err)
	}
	w, err := l.wallets.LeaseForInvoice(ctx, doc.MerchantID, doc.ID, tok.NetworkID, tok.ID, doc.ExpiresAt)
	if err != nil {
		return fmt.Errorf("lease wallet for invoice: %w", err)
	}
	doc.WalletID = &w.ID
	if err := l.allocations.Open(ctx, crypto.WalletAllocation{
		WalletID:   w.ID,
		InvoiceID:  doc.ID,
		MerchantID: doc.MerchantID,
		TokenID:    doc.TokenID,
		NetworkID:  tok.NetworkID,
	}); err != nil {
		return fmt.Errorf("record wallet allocation: %w", err)
	}
	return nil
}
