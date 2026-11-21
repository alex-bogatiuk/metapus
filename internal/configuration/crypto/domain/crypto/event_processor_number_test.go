package crypto

import (
	"context"
	"errors"
	"testing"
	"time"

	"metapus/internal/configuration/crypto/domain/catalogs/token"
	"metapus/internal/configuration/crypto/domain/catalogs/wallet"
	"metapus/internal/configuration/crypto/domain/documents/crypto_payment"
	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/core/tenant"
	"metapus/internal/core/types"
)

// seriesNumbers numbers documents in their series, one after another.
func seriesNumbers() *numerator.MockGenerator {
	var n int64
	return &numerator.MockGenerator{GetNextNumberFunc: func(_ context.Context, cfg numerator.Config, period time.Time) (string, error) {
		n++
		return cfg.Format(period, n), nil
	}}
}

func failingNumbers() *numerator.MockGenerator {
	return &numerator.MockGenerator{GetNextNumberFunc: func(context.Context, numerator.Config, time.Time) (string, error) {
		return "", errors.New("sequence unavailable")
	}}
}

// A detected payment is numbered in the CP series; when the numerator fails
// the event fails instead of storing a number outside the series, so its
// transaction rolls back and the event is retried.
func TestDetectedPaymentIsNumberedInItsSeriesOrNotAtAll(t *testing.T) {
	ctx := tenant.WithTxManager(context.Background(), &noopTxManager{})

	f := setupTestFixture(t, 10_000_000)
	event := f.makeTransferEvent(5_000_000)
	event.EventKind = EventKindContractLog
	if err := f.processor.ProcessEvent(ctx, event); err != nil {
		t.Fatalf("detect transfer: %v", err)
	}
	payment, err := f.paymentRepo.FindByTxHash(ctx, event.TxHash)
	if err != nil {
		t.Fatal(err)
	}
	if want := numerator.DocumentSeries(crypto_payment.Definition).Format(event.Timestamp, 1); payment.Number != want {
		t.Fatalf("payment number = %q, want %q", payment.Number, want)
	}

	for name, numbers := range map[string]numerator.Generator{"failing numerator": failingNumbers(), "no numerator": nil} {
		f := setupTestFixture(t, 10_000_000)
		f.processor.numerator = numbers
		event := f.makeTransferEvent(5_000_000)
		event.EventKind = EventKindContractLog
		if err := f.processor.ProcessEvent(ctx, event); err == nil {
			t.Fatalf("%s: the event succeeded without a number", name)
		}
		if len(f.paymentRepo.payments) != 0 {
			t.Fatalf("%s: a payment was stored without a series number", name)
		}
	}
}

func TestTopUpInvoiceIsNotCreatedWithoutASeriesNumber(t *testing.T) {
	merchantID := id.New()
	w := &wallet.Wallet{MerchantID: &merchantID, AllocationMode: wallet.AllocationModePersistent}
	w.ID = id.New()
	tok := &token.Token{}
	tok.ID = id.New()
	invoices := newMemInvoiceRepo()
	p := &EventProcessor{invoiceSvc: invoices, numerator: failingNumbers(), nowFunc: time.Now}
	if _, err := p.createTopUpInvoice(context.Background(), w, BlockchainEvent{Amount: types.NewRawAmountFromInt64(100)}, tok); err == nil {
		t.Fatal("a top-up invoice was created without a series number")
	}
	if len(invoices.invoices) != 0 {
		t.Fatal("a top-up invoice was stored without a series number")
	}
}
