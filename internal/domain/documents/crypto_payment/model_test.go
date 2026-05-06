// internal/domain/documents/crypto_payment/model_test.go
package crypto_payment

import (
	"context"
	"math/big"
	"testing"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

func TestNewCryptoPayment(t *testing.T) {
	invoiceID := id.New()
	merchantID := id.New()
	tokenID := id.New()
	walletID := id.New()
	amount := types.NewCryptoAmount(big.NewInt(5_000_000))

	p := NewCryptoPayment(invoiceID, merchantID, tokenID, walletID, "txhash123", "TSender", amount, 12345, 19)

	if p.InvoiceID != invoiceID {
		t.Errorf("InvoiceID = %v, want %v", p.InvoiceID, invoiceID)
	}
	if p.MerchantID != merchantID {
		t.Errorf("MerchantID = %v, want %v", p.MerchantID, merchantID)
	}
	if p.TokenID != tokenID {
		t.Errorf("TokenID = %v, want %v", p.TokenID, tokenID)
	}
	if p.WalletID != walletID {
		t.Errorf("WalletID = %v, want %v", p.WalletID, walletID)
	}
	if p.TxHash != "txhash123" {
		t.Errorf("TxHash = %q, want %q", p.TxHash, "txhash123")
	}
	if p.FromAddress != "TSender" {
		t.Errorf("FromAddress = %q, want %q", p.FromAddress, "TSender")
	}
	if p.Amount.BigInt().Int64() != 5_000_000 {
		t.Errorf("Amount = %s, want 5000000", p.Amount.String())
	}
	if p.BlockNumber != 12345 {
		t.Errorf("BlockNumber = %d, want 12345", p.BlockNumber)
	}
	if p.RequiredConfs != 19 {
		t.Errorf("RequiredConfs = %d, want 19", p.RequiredConfs)
	}
	if p.Status != PaymentStatusDetected {
		t.Errorf("Status = %q, want %q", p.Status, PaymentStatusDetected)
	}
	if p.Confirmations != 0 {
		t.Errorf("Confirmations = %d, want 0", p.Confirmations)
	}
	if !p.NetworkFee.IsZero() {
		t.Errorf("NetworkFee = %s, want 0", p.NetworkFee.String())
	}
	// BasisType and BasisID should be set for subordination
	if p.BasisType != "CryptoInvoice" {
		t.Errorf("BasisType = %q, want %q", p.BasisType, "CryptoInvoice")
	}
	if p.BasisID == nil || *p.BasisID != invoiceID {
		t.Errorf("BasisID = %v, want %v", p.BasisID, &invoiceID)
	}
}

func TestCryptoPayment_Validate(t *testing.T) {
	ctx := context.Background()

	validPayment := func() *CryptoPayment {
		return NewCryptoPayment(
			id.New(), id.New(), id.New(), id.New(),
			"0xabc123", "TSender",
			types.NewCryptoAmount(big.NewInt(1_000_000)),
			100, 19,
		)
	}

	tests := []struct {
		give    string
		modify  func(p *CryptoPayment)
		wantErr bool
	}{
		{
			give:    "valid payment",
			modify:  func(p *CryptoPayment) {},
			wantErr: false,
		},
		{
			give:    "nil invoiceID → error",
			modify:  func(p *CryptoPayment) { p.InvoiceID = id.Nil() },
			wantErr: true,
		},
		{
			give:    "nil merchantID → error",
			modify:  func(p *CryptoPayment) { p.MerchantID = id.Nil() },
			wantErr: true,
		},
		{
			give:    "nil tokenID → error",
			modify:  func(p *CryptoPayment) { p.TokenID = id.Nil() },
			wantErr: true,
		},
		{
			give:    "nil walletID → error",
			modify:  func(p *CryptoPayment) { p.WalletID = id.Nil() },
			wantErr: true,
		},
		{
			give:    "empty txHash → error",
			modify:  func(p *CryptoPayment) { p.TxHash = "" },
			wantErr: true,
		},
		{
			give:    "zero amount → error",
			modify:  func(p *CryptoPayment) { p.Amount = types.ZeroCryptoAmount() },
			wantErr: true,
		},
		{
			give: "negative amount → error",
			modify: func(p *CryptoPayment) {
				p.Amount = types.NewCryptoAmount(big.NewInt(-1))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			p := validPayment()
			tt.modify(p)
			err := p.Validate(ctx)

			if tt.wantErr && err == nil {
				t.Error("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestCryptoPayment_IsFullyConfirmed(t *testing.T) {
	tests := []struct {
		give          string
		confirmations int
		requiredConfs int
		want          bool
	}{
		{"0/19 → false", 0, 19, false},
		{"5/19 → false", 5, 19, false},
		{"19/19 → true (exact)", 19, 19, true},
		{"25/19 → true (over)", 25, 19, true},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			p := &CryptoPayment{
				Confirmations: tt.confirmations,
				RequiredConfs: tt.requiredConfs,
			}
			if got := p.IsFullyConfirmed(); got != tt.want {
				t.Errorf("IsFullyConfirmed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCryptoPayment_GetRLSDimensions(t *testing.T) {
	merchantID := id.New()
	p := &CryptoPayment{MerchantID: merchantID}

	dims := p.GetRLSDimensions()
	if dims["merchant"] != merchantID.String() {
		t.Errorf("merchant dimension = %q, want %q", dims["merchant"], merchantID.String())
	}
}

func TestCryptoPayment_Lines_DefensiveCopy(t *testing.T) {
	p := NewCryptoPayment(
		id.New(), id.New(), id.New(), id.New(),
		"tx1", "sender",
		types.NewCryptoAmount(big.NewInt(100)),
		1, 19,
	)

	lines := []CryptoPaymentLine{
		{LineID: id.New(), LineNo: 1, Description: "Payment", Amount: types.NewCryptoAmount(big.NewInt(100))},
	}

	p.SetLines(lines)

	// Mutate original slice
	lines[0].Description = "MUTATED"

	// Internal copy should be unaffected (§2.13 defensive copy)
	got := p.GetLines()
	if got[0].Description == "MUTATED" {
		t.Error("SetLines did not perform defensive copy — internal state was mutated")
	}

	// Mutate returned slice
	got[0].Description = "ALSO_MUTATED"
	got2 := p.GetLines()
	if got2[0].Description == "ALSO_MUTATED" {
		t.Error("GetLines did not perform defensive copy — internal state was mutated via returned slice")
	}
}

func TestCryptoPayment_GenerateCryptoBalanceMovements(t *testing.T) {
	ctx := context.Background()

	t.Run("positive amount → single RECEIPT movement", func(t *testing.T) {
		p := NewCryptoPayment(
			id.New(), id.New(), id.New(), id.New(),
			"tx1", "sender",
			types.NewCryptoAmount(big.NewInt(5_000_000)),
			100, 19,
		)
		p.Date = time.Now().UTC()

		movements, err := p.GenerateCryptoBalanceMovements(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(movements) != 1 {
			t.Fatalf("expected 1 movement, got %d", len(movements))
		}
		if movements[0].Amount.Cmp(p.Amount) != 0 {
			t.Errorf("movement amount = %s, want %s", movements[0].Amount.String(), p.Amount.String())
		}
	})

	t.Run("zero amount → nil movements", func(t *testing.T) {
		p := NewCryptoPayment(
			id.New(), id.New(), id.New(), id.New(),
			"tx2", "sender",
			types.ZeroCryptoAmount(),
			100, 19,
		)

		movements, err := p.GenerateCryptoBalanceMovements(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if movements != nil {
			t.Errorf("expected nil movements for zero amount, got %d", len(movements))
		}
	})
}
