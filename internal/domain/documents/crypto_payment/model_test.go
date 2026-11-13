// internal/domain/documents/crypto_payment/model_test.go
package crypto_payment

import (
	"context"
	"testing"
	"time"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

func TestNewCryptoPayment(t *testing.T) {
	invoiceID := id.New()
	merchantID := id.New()
	tokenID := id.New()
	walletID := id.New()
	amount := types.NewCryptoAmountFromInt64(5_000_000)

	p := NewCryptoPayment(invoiceID, merchantID, tokenID, walletID, id.New(), "txhash123", 0, "contract_log", "block123", "TSender", amount, 12345, 19)

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
	if p.Amount.String() != "5000000" {
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
			id.New(), id.New(), id.New(), id.New(), id.New(),
			"0xabc123", 0, "contract_log", "block123", "TSender",
			types.NewCryptoAmountFromInt64(1_000_000),
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

func TestCryptoPayment_GenerateCryptoReceiptSuspenseMovements(t *testing.T) {
	ctx := context.Background()
	newPayment := func(amount types.CryptoAmount) *CryptoPayment {
		p := NewCryptoPayment(
			id.New(), id.New(), id.New(), id.New(), id.New(),
			"tx1", 0, "contract_log", "block1", "sender",
			amount, 100, 19,
		)
		p.Date = time.Now().UTC()
		return p
	}

	t.Run("attributed receipt → single suspense expense of the gross", func(t *testing.T) {
		p := newPayment(types.NewCryptoAmountFromInt64(5_000_000))
		receipt := id.New()
		p.ReceiptID = &receipt
		movements, err := p.GenerateCryptoReceiptSuspenseMovements(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(movements) != 1 || movements[0].ReceiptID != receipt || movements[0].RecordType != entity.RecordTypeExpense ||
			movements[0].Amount.Cmp(p.Amount) != 0 || movements[0].RecorderVersion != 1 {
			t.Fatalf("unexpected suspense movements: %+v", movements)
		}
	})

	t.Run("no receipt → posting refused", func(t *testing.T) {
		if _, err := newPayment(types.NewCryptoAmountFromInt64(5_000_000)).GenerateCryptoReceiptSuspenseMovements(ctx); err == nil {
			t.Fatal("payment without a receipt must not consume suspense")
		}
	})

	t.Run("fee above gross → posting refused", func(t *testing.T) {
		p := newPayment(types.NewCryptoAmountFromInt64(3))
		receipt := id.New()
		p.ReceiptID = &receipt
		p.SetFeeConfig(types.NewCryptoAmountFromInt64(5), 0, types.ZeroCryptoAmount(), types.ZeroCryptoAmount())
		if _, err := p.GenerateCryptoReceiptSuspenseMovements(ctx); err == nil {
			t.Fatal("fee above the gross must not be posted")
		}
	})
}

func TestFeeAboveGrossDoesNotUnderflow(t *testing.T) {
	p := &CryptoPayment{Amount: types.MustCryptoAmount("3"), FeeFixed: types.MustCryptoAmount("5"), FeeMin: types.ZeroCryptoAmount(), FeeMax: types.ZeroCryptoAmount()}
	if p.FeeAmount().String() != "5" || !p.NetAmount().IsZero() {
		t.Fatalf("fee=%s net=%s", p.FeeAmount(), p.NetAmount())
	}
}
