package cryptoapp

import (
	"context"
	"testing"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/documents/crypto_invoice"
	"metapus/internal/domain/documents/crypto_payment"
)

type stubMerchantCryptoQueryRepo struct {
	invoices           []MerchantInvoiceRecord
	paymentsByInvoices map[id.ID][]MerchantInvoicePayment
	capturedFilter     MerchantInvoiceListFilter
}

func (s *stubMerchantCryptoQueryRepo) ResolveTokenContext(context.Context, string) (MerchantTokenContext, bool, error) {
	return MerchantTokenContext{}, false, nil
}

func (s *stubMerchantCryptoQueryRepo) ListMerchantCurrencies(context.Context) ([]MerchantCurrency, error) {
	return nil, nil
}

func (s *stubMerchantCryptoQueryRepo) GetMerchantInvoiceDisplay(context.Context, id.ID) (MerchantInvoiceDisplay, error) {
	return MerchantInvoiceDisplay{}, nil
}

func (s *stubMerchantCryptoQueryRepo) GetMerchantInvoiceObservedAt(context.Context, id.ID) (*time.Time, error) {
	return nil, nil
}

func (s *stubMerchantCryptoQueryRepo) ListMerchantInvoicePayments(_ context.Context, invoiceID id.ID) ([]MerchantInvoicePayment, error) {
	return s.paymentsByInvoices[invoiceID], nil
}

func (s *stubMerchantCryptoQueryRepo) ListMerchantInvoicePaymentsByInvoices(_ context.Context, invoiceIDs []id.ID) (map[id.ID][]MerchantInvoicePayment, error) {
	out := make(map[id.ID][]MerchantInvoicePayment, len(invoiceIDs))
	for _, invID := range invoiceIDs {
		out[invID] = s.paymentsByInvoices[invID]
	}
	return out, nil
}

func (s *stubMerchantCryptoQueryRepo) ListMerchantInvoices(_ context.Context, filter MerchantInvoiceListFilter) ([]MerchantInvoiceRecord, error) {
	s.capturedFilter = filter
	return s.invoices, nil
}

func TestSummarizeMerchantInvoicePayments(t *testing.T) {
	mustAmount := func(v string) types.CryptoAmount {
		a, err := types.NewCryptoAmountFromString(v)
		if err != nil {
			t.Fatalf("unexpected amount error: %v", err)
		}
		return a
	}

	tests := []struct {
		name    string
		give    []MerchantInvoicePayment
		wantFee string
		wantNet string
	}{
		{
			name:    "empty payments returns zero fee and net",
			give:    nil,
			wantFee: "0",
			wantNet: "0",
		},
		{
			name: "sums only canonical posted confirmed or settled payments",
			give: []MerchantInvoicePayment{
				{
					ID:           id.New(),
					Amount:       "10000000",
					RawAmount:    mustAmount("10000000"),
					RawFeeAmount: mustAmount("100000"),
					RawNetAmount: mustAmount("9900000"),
					Status:       string(crypto_payment.PaymentStatusSettled),
					Canonicality: "canonical",
					Posted:       true,
					UpdatedAt:    time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
				},
				{
					ID:           id.New(),
					Amount:       "5000000",
					RawAmount:    mustAmount("5000000"),
					RawFeeAmount: mustAmount("50000"),
					RawNetAmount: mustAmount("4950000"),
					Status:       string(crypto_payment.PaymentStatusDetected),
					Canonicality: "canonical",
					Posted:       false,
					UpdatedAt:    time.Date(2026, 5, 1, 10, 1, 0, 0, time.UTC),
				},
				{
					ID:           id.New(),
					Amount:       "5000000",
					RawAmount:    mustAmount("5000000"),
					RawFeeAmount: mustAmount("50000"),
					RawNetAmount: mustAmount("4950000"),
					Status:       string(crypto_payment.PaymentStatusReorged),
					Canonicality: "orphaned",
					Posted:       false,
					UpdatedAt:    time.Date(2026, 5, 1, 10, 2, 0, 0, time.UTC),
				},
			},
			wantFee: "100000",
			wantNet: "9900000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarizeMerchantInvoicePayments(tt.give)
			if got.FeeAmount != tt.wantFee {
				t.Fatalf("FeeAmount = %s, want %s", got.FeeAmount, tt.wantFee)
			}
			if got.NetAmount != tt.wantNet {
				t.Fatalf("NetAmount = %s, want %s", got.NetAmount, tt.wantNet)
			}
		})
	}
}

func TestMerchantCryptoQueryService_ListInvoices_CursorPagination(t *testing.T) {
	inv1ID := id.New()
	inv2ID := id.New()
	t1 := time.Date(2026, 5, 12, 10, 5, 0, 0, time.UTC)
	t2 := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)

	inv1 := &crypto_invoice.CryptoInvoice{}
	inv1.ID = inv1ID
	inv1.CreatedAt = t1
	inv1.CompletedAt = &t1

	inv2 := &crypto_invoice.CryptoInvoice{}
	inv2.ID = inv2ID
	inv2.CreatedAt = t2
	inv2.CompletedAt = &t2

	completedFrom := time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC)
	validCreatedCursor := encodeMerchantInvoiceCursor("created", t1, inv1ID)

	tests := []struct {
		name        string
		giveRecords []MerchantInvoiceRecord
		giveFilter  MerchantInvoiceListFilter
		wantCount   int
		wantHasMore bool
		wantErr     bool
	}{
		{
			name: "returns page and nextCursor when records exceed limit",
			giveRecords: []MerchantInvoiceRecord{
				{Invoice: inv1, TokenCode: "USDT-TRC20", DecimalPlaces: 6},
				{Invoice: inv2, TokenCode: "USDT-TRC20", DecimalPlaces: 6},
			},
			giveFilter: MerchantInvoiceListFilter{
				MerchantID: id.New(),
				Limit:      1,
			},
			wantCount:   1,
			wantHasMore: true,
		},
		{
			name: "rejects created-mode cursor when querying completedFrom",
			giveFilter: MerchantInvoiceListFilter{
				MerchantID:    id.New(),
				CompletedFrom: &completedFrom,
				Cursor:        validCreatedCursor,
				Limit:         10,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &stubMerchantCryptoQueryRepo{
				invoices:           tt.giveRecords,
				paymentsByInvoices: map[id.ID][]MerchantInvoicePayment{},
			}
			svc := NewMerchantCryptoQueryService(repo)
			got, err := svc.ListInvoices(context.Background(), tt.giveFilter)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ListInvoices() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(got.Items) != tt.wantCount {
				t.Fatalf("len(Items) = %d, want %d", len(got.Items), tt.wantCount)
			}
			if got.HasMore != tt.wantHasMore {
				t.Fatalf("HasMore = %v, want %v", got.HasMore, tt.wantHasMore)
			}
			if tt.wantHasMore && (got.NextCursor == nil || *got.NextCursor == "") {
				t.Fatalf("expected non-empty NextCursor when HasMore is true")
			}
		})
	}
}
