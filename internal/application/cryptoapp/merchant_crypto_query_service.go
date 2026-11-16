package cryptoapp

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
	"metapus/internal/domain/catalogs/blockchain_network"
	"metapus/internal/domain/documents/crypto_invoice"
	"metapus/internal/domain/documents/crypto_payment"
)

type MerchantTokenContext struct {
	TokenID               id.ID
	NetworkID             id.ID
	NetworkCode           string
	DecimalPlaces         int
	RequiredConfirmations int
	FinalityMode          string
	Enabled               bool
	Held                  bool
}

type MerchantCurrency struct {
	Code                  string
	Symbol                string
	Name                  string
	Network               string
	NetworkName           string
	Standard              string
	DecimalPlaces         int
	ContractAddress       string
	RequiredConfirmations int
	FinalityMode          string
	AcceptsPayments       bool
}

type MerchantInvoiceDisplay struct {
	WalletAddress string
	TokenCode     string
	NetworkCode   string
	DecimalPlaces int
}

type MerchantInvoicePayment struct {
	ID                    id.ID
	InvoiceID             id.ID
	TxHash                string
	EventIndex            int64
	EventKind             string
	Amount                string
	FeeAmount             string
	NetAmount             string
	Status                string
	Canonicality          string
	FinalityStatus        string
	Posted                bool
	Confirmations         int
	RequiredConfirmations int
	SourceNonce           *int64
	SourceAddress         string
	ReplacementTxHash     string
	DetectedAt            time.Time
	ConfirmedAt           *time.Time
	UpdatedAt             time.Time
	RawAmount             types.CryptoAmount
	RawFeeAmount          types.CryptoAmount
	RawNetAmount          types.CryptoAmount
}

type MerchantInvoicePaymentsSummary struct {
	FeeAmount  string
	NetAmount  string
	ObservedAt *string
	Payments   []MerchantInvoicePayment
}

type MerchantInvoiceRecord struct {
	Invoice       *crypto_invoice.CryptoInvoice
	WalletAddress string
	TokenCode     string
	NetworkCode   string
	DecimalPlaces int
	Payments      MerchantInvoicePaymentsSummary
}

type MerchantInvoiceListFilter struct {
	MerchantID      id.ID
	Statuses        []string
	Currency        string
	OrderID         string
	ExternalID      string
	CustomerID      string
	CreatedFrom     *time.Time
	CreatedTo       *time.Time
	CompletedFrom   *time.Time
	CompletedTo     *time.Time
	Limit           int
	Cursor          string
	CursorTime      *time.Time
	CursorID        *id.ID
	SortByCompleted bool
}

type MerchantInvoiceListPage struct {
	Items      []MerchantInvoiceRecord
	HasMore    bool
	NextCursor *string
}

type merchantInvoiceCursorPayload struct {
	Mode string `json:"m"`
	Time string `json:"t"`
	ID   string `json:"id"`
}

type MerchantCryptoQueryRepository interface {
	ResolveTokenContext(context.Context, string) (MerchantTokenContext, bool, error)
	ListMerchantCurrencies(context.Context) ([]MerchantCurrency, error)
	GetMerchantInvoiceDisplay(context.Context, id.ID) (MerchantInvoiceDisplay, error)
	GetMerchantInvoiceObservedAt(context.Context, id.ID) (*time.Time, error)
	ListMerchantInvoicePayments(context.Context, id.ID) ([]MerchantInvoicePayment, error)
	ListMerchantInvoicePaymentsByInvoices(context.Context, []id.ID) (map[id.ID][]MerchantInvoicePayment, error)
	ListMerchantInvoices(context.Context, MerchantInvoiceListFilter) ([]MerchantInvoiceRecord, error)
}

type MerchantCryptoQueryService struct {
	repo MerchantCryptoQueryRepository
}

func NewMerchantCryptoQueryService(repo MerchantCryptoQueryRepository) *MerchantCryptoQueryService {
	if repo == nil {
		panic("cryptoapp.NewMerchantCryptoQueryService: repository is required")
	}
	return &MerchantCryptoQueryService{repo: repo}
}

func (s *MerchantCryptoQueryService) ResolveToken(ctx context.Context, code string) (MerchantTokenContext, error) {
	value, found, err := s.repo.ResolveTokenContext(ctx, code)
	if err != nil {
		return value, err
	}
	if !found {
		return value, apperror.NewValidation(fmt.Sprintf("unknown currency: %s", code)).WithDetail("field", "currency")
	}
	if !value.Enabled || value.Held {
		return value, blockchain_network.NewNetworkUnavailable(value.NetworkID)
	}
	return value, nil
}

func (s *MerchantCryptoQueryService) ListCurrencies(ctx context.Context) ([]MerchantCurrency, error) {
	return s.repo.ListMerchantCurrencies(ctx)
}

func (s *MerchantCryptoQueryService) InvoiceDisplay(ctx context.Context, invoiceID id.ID) (MerchantInvoiceDisplay, error) {
	return s.repo.GetMerchantInvoiceDisplay(ctx, invoiceID)
}

func (s *MerchantCryptoQueryService) InvoiceObservedAt(ctx context.Context, invoiceID id.ID) *string {
	observedAt, err := s.repo.GetMerchantInvoiceObservedAt(ctx, invoiceID)
	if err != nil || observedAt == nil {
		return nil
	}
	formatted := observedAt.UTC().Format(time.RFC3339Nano)
	return &formatted
}

const (
	_defaultInvoicePageLimit = 50
	_maxInvoicePageLimit     = 100
	_cursorModeCreated       = "created"
	_cursorModeCompleted     = "completed"
)

func (s *MerchantCryptoQueryService) InvoicePaymentsSummary(ctx context.Context, invoiceID id.ID) (MerchantInvoicePaymentsSummary, error) {
	items, err := s.repo.ListMerchantInvoicePayments(ctx, invoiceID)
	if err != nil {
		return MerchantInvoicePaymentsSummary{}, err
	}
	return summarizeMerchantInvoicePayments(items), nil
}

func (s *MerchantCryptoQueryService) ListInvoices(ctx context.Context, filter MerchantInvoiceListFilter) (MerchantInvoiceListPage, error) {
	if filter.Limit <= 0 || filter.Limit > _maxInvoicePageLimit {
		filter.Limit = _defaultInvoicePageLimit
	}
	filter.SortByCompleted = filter.CompletedFrom != nil || filter.CompletedTo != nil
	expectedMode := _cursorModeCreated
	if filter.SortByCompleted {
		expectedMode = _cursorModeCompleted
	}

	if filter.Cursor != "" {
		cursorTime, cursorID, mode, err := decodeMerchantInvoiceCursor(filter.Cursor)
		if err != nil || mode != expectedMode {
			return MerchantInvoiceListPage{}, apperror.NewValidation("invalid cursor").WithDetail("field", "cursor")
		}
		filter.CursorTime = &cursorTime
		filter.CursorID = &cursorID
	}

	records, err := s.repo.ListMerchantInvoices(ctx, filter)
	if err != nil {
		return MerchantInvoiceListPage{}, apperror.NewInternal(fmt.Errorf("list merchant invoices: %w", err))
	}

	var hasMore bool
	var nextCursor *string
	if len(records) > filter.Limit {
		hasMore = true
		records = records[:filter.Limit]
		last := records[len(records)-1].Invoice
		cursorTS := last.CreatedAt.UTC()
		if filter.SortByCompleted && last.CompletedAt != nil {
			cursorTS = last.CompletedAt.UTC()
		}
		encoded := encodeMerchantInvoiceCursor(expectedMode, cursorTS, last.ID)
		nextCursor = &encoded
	}

	if len(records) == 0 {
		return MerchantInvoiceListPage{Items: []MerchantInvoiceRecord{}}, nil
	}

	invoiceIDs := make([]id.ID, 0, len(records))
	for _, r := range records {
		invoiceIDs = append(invoiceIDs, r.Invoice.ID)
	}

	paymentsByInvoice, err := s.repo.ListMerchantInvoicePaymentsByInvoices(ctx, invoiceIDs)
	if err != nil {
		return MerchantInvoiceListPage{}, apperror.NewInternal(fmt.Errorf("list merchant invoice payments: %w", err))
	}

	for i := range records {
		invID := records[i].Invoice.ID
		records[i].Payments = summarizeMerchantInvoicePayments(paymentsByInvoice[invID])
	}

	return MerchantInvoiceListPage{
		Items:      records,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}, nil
}

func summarizeMerchantInvoicePayments(items []MerchantInvoicePayment) MerchantInvoicePaymentsSummary {
	totalFee := types.ZeroCryptoAmount()
	totalNet := types.ZeroCryptoAmount()
	var latest time.Time

	payments := make([]MerchantInvoicePayment, len(items))
	copy(payments, items)

	for i := range payments {
		item := &payments[i]
		if item.FeeAmount == "" && item.NetAmount == "" {
			item.FeeAmount = item.RawFeeAmount.String()
			item.NetAmount = item.RawNetAmount.String()
		}
		if item.UpdatedAt.After(latest) {
			latest = item.UpdatedAt
		}
		if item.Canonicality == "canonical" && item.Posted &&
			(item.Status == string(crypto_payment.PaymentStatusConfirmed) || item.Status == string(crypto_payment.PaymentStatusSettled)) {
			totalFee = totalFee.Add(item.RawFeeAmount)
			totalNet = totalNet.Add(item.RawNetAmount)
		}
	}

	var observedAt *string
	if !latest.IsZero() {
		formatted := latest.UTC().Format(time.RFC3339Nano)
		observedAt = &formatted
	}

	return MerchantInvoicePaymentsSummary{
		FeeAmount:  totalFee.String(),
		NetAmount:  totalNet.String(),
		ObservedAt: observedAt,
		Payments:   payments,
	}
}

func encodeMerchantInvoiceCursor(mode string, ts time.Time, invoiceID id.ID) string {
	payload := merchantInvoiceCursorPayload{
		Mode: mode,
		Time: ts.UTC().Format(time.RFC3339Nano),
		ID:   invoiceID.String(),
	}
	raw, _ := json.Marshal(payload)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeMerchantInvoiceCursor(cursor string) (time.Time, id.ID, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, id.ID{}, "", err
	}
	var payload merchantInvoiceCursorPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return time.Time{}, id.ID{}, "", err
	}
	ts, err := time.Parse(time.RFC3339Nano, payload.Time)
	if err != nil {
		return time.Time{}, id.ID{}, "", err
	}
	parsedID, err := id.Parse(payload.ID)
	if err != nil {
		return time.Time{}, id.ID{}, "", err
	}
	return ts.UTC(), parsedID, payload.Mode, nil
}
