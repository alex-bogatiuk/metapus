package cryptoapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/token"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_invoice"
)

type leaseTokenStub struct {
	tok *token.Token
	err error
}

func (s leaseTokenStub) GetByID(context.Context, id.ID) (*token.Token, error) { return s.tok, s.err }

type leaseWalletStub struct {
	wallet *wallet.Wallet
	err    error
	got    []id.ID
}

func (s *leaseWalletStub) LeaseForInvoice(_ context.Context, merchantID, invoiceID, networkID, tokenID id.ID, _ time.Time) (*wallet.Wallet, error) {
	s.got = []id.ID{merchantID, invoiceID, networkID, tokenID}
	return s.wallet, s.err
}

type leaseAllocationStub struct {
	crypto.WalletAllocationRepository
	opened []crypto.WalletAllocation
	err    error
}

func (s *leaseAllocationStub) Open(_ context.Context, alloc crypto.WalletAllocation) error {
	s.opened = append(s.opened, alloc)
	return s.err
}

func newLeaseFixture() (*crypto_invoice.CryptoInvoice, *token.Token, *wallet.Wallet) {
	tok := &token.Token{NetworkID: id.New()}
	tok.ID = id.New()
	w := &wallet.Wallet{}
	w.ID = id.New()
	doc := &crypto_invoice.CryptoInvoice{TokenID: tok.ID, MerchantID: id.New(), ExpiresAt: time.Now().Add(time.Hour)}
	doc.ID = id.New()
	return doc, tok, w
}

func TestInvoiceWalletLeaseAssignsWalletAndOpensAllocation(t *testing.T) {
	doc, tok, w := newLeaseFixture()
	wallets := &leaseWalletStub{wallet: w}
	allocations := &leaseAllocationStub{}
	lease := NewInvoiceWalletLease(leaseTokenStub{tok: tok}, wallets, allocations)

	require.NoError(t, lease.Apply(context.Background(), doc))

	require.NotNil(t, doc.WalletID)
	require.Equal(t, w.ID, *doc.WalletID)
	require.Equal(t, []id.ID{doc.MerchantID, doc.ID, tok.NetworkID, tok.ID}, wallets.got)
	require.Equal(t, []crypto.WalletAllocation{{
		WalletID:   w.ID,
		InvoiceID:  doc.ID,
		MerchantID: doc.MerchantID,
		TokenID:    doc.TokenID,
		NetworkID:  tok.NetworkID,
	}}, allocations.opened)
}

func TestInvoiceWalletLeaseStopsOnFirstFailure(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name        string
		tokens      leaseTokenStub
		walletErr   error
		allocErr    error
		error       string
		wantWallet  bool
		wantOpening bool
	}{
		{name: "token lookup", tokens: leaseTokenStub{err: boom}, error: "resolve token for wallet lease"},
		{name: "pool exhausted", walletErr: boom, error: "lease wallet for invoice"},
		{name: "allocation", allocErr: boom, error: "record wallet allocation", wantWallet: true, wantOpening: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, tok, w := newLeaseFixture()
			if tt.tokens.err == nil {
				tt.tokens.tok = tok
			}
			allocations := &leaseAllocationStub{err: tt.allocErr}
			lease := NewInvoiceWalletLease(tt.tokens, &leaseWalletStub{wallet: w, err: tt.walletErr}, allocations)

			err := lease.Apply(context.Background(), doc)

			require.ErrorIs(t, err, boom)
			require.ErrorContains(t, err, tt.error)
			require.Equal(t, tt.wantWallet, doc.WalletID != nil)
			require.Equal(t, tt.wantOpening, len(allocations.opened) == 1)
		})
	}
}
