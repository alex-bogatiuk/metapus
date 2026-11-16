package cryptohandlers

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/application/readaccess"
	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/security"
	"metapus/internal/core/types"
	"metapus/internal/domain/documents/crypto_invoice"
	"metapus/internal/domain/printing"
	printadapter "metapus/internal/infrastructure/printing"
)

type printRefRegistry struct{}

var _printRefFields = map[string][]string{
	"cat_merchants":           {"name"},
	"cat_tokens":              {"symbol", "decimalPlaces", "networkId"},
	"cat_blockchain_networks": {"name"},
	"cat_wallets":             {"address"},
}

func (printRefRegistry) Definition(key string) (readaccess.Definition, bool) {
	names, ok := _printRefFields[key]
	if !ok {
		return readaccess.Definition{}, false
	}
	fields := make([]readaccess.Field, 0, len(names))
	for _, name := range names {
		fields = append(fields, readaccess.Field{Name: name, Column: name})
	}
	return readaccess.Definition{Key: key, Permission: key + ":read", Table: key, Fields: fields}, true
}

type printRefRepo struct {
	rows  map[string]map[string]any
	reads map[string]int
}

func (r *printRefRepo) Read(_ context.Context, d readaccess.Definition, q readaccess.Query) ([]readaccess.Record, error) {
	r.reads[d.Key]++
	fields, ok := r.rows[d.Key]
	if !ok || len(q.IDs) != 1 {
		return nil, nil
	}
	return []readaccess.Record{{ID: q.IDs[0], Entity: fields, Fields: fields}}, nil
}

type printFixture struct {
	doc  *crypto_invoice.CryptoInvoice
	repo *printRefRepo
}

func newPrintFixture() printFixture {
	walletID := id.New()
	paidAt := time.Date(2026, 11, 10, 9, 30, 0, 0, time.UTC)
	doc := &crypto_invoice.CryptoInvoice{
		Document:              entity.Document{Number: "INV-42", Date: time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)},
		MerchantID:            id.New(),
		TokenID:               id.New(),
		WalletID:              &walletID,
		ExpectedAmount:        types.NewRawAmountFromInt64(100500000),
		ReceivedAmount:        types.NewRawAmountFromInt64(100500000),
		ConfirmedAmount:       types.NewRawAmountFromInt64(100500000),
		Status:                crypto_invoice.InvoiceStatus("paid"),
		ExpiresAt:             time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC),
		PaidAt:                &paidAt,
		RequiredConfirmations: 20,
		OrderID:               "ORD-7",
	}
	return printFixture{doc: doc, repo: &printRefRepo{reads: map[string]int{}, rows: map[string]map[string]any{
		"cat_merchants":           {"name": "<b>Acme</b>"},
		"cat_tokens":              {"symbol": "USDT", "decimalPlaces": 6, "networkId": id.New()},
		"cat_blockchain_networks": {"name": "TRON Nile"},
		"cat_wallets":             {"address": "TXYZaddress"},
	}}}
}

func printContext(t *testing.T, repo *printRefRepo, permissions []string, policy *security.FieldPolicy) context.Context {
	t.Helper()
	ctx := appctx.WithUser(context.Background(), &appctx.UserContext{UserID: id.New().String(), Permissions: permissions})
	ctx = readaccess.WithService(ctx, readaccess.New(printRefRegistry{}, repo, nil))
	if policy != nil {
		ctx = security.WithFieldPolicies(ctx, map[string]*security.FieldPolicy{"crypto_invoice:read": policy})
	}
	return ctx
}

func headerValue(data *printing.PrintData, label string) (string, bool) {
	for _, row := range data.Table.HeaderRows {
		for _, field := range row {
			if field.Label == label {
				return field.Value, true
			}
		}
	}
	return "", false
}

func rowValue(data *printing.PrintData, label string) (string, bool) {
	for _, row := range data.Table.Rows {
		if row.Values[0] == label {
			return row.Values[1], true
		}
	}
	return "", false
}

var _allPrintRefs = []string{"cat_merchants:read", "cat_tokens:read", "cat_blockchain_networks:read", "cat_wallets:read"}

func TestCryptoInvoicePrintDataShowsAuthorizedValuesExactly(t *testing.T) {
	f := newPrintFixture()
	ctx := printContext(t, f.repo, _allPrintRefs, nil)

	data, err := buildCryptoInvoicePrintData(ctx, f.doc, nil)
	require.NoError(t, err)

	require.Equal(t, "№ INV-42 от 10.11.2026", data.Table.Subtitle)
	tests := []struct {
		give string
		want string
	}{
		{give: "Мерчант", want: "<b>Acme</b>"},
		{give: "Сеть", want: "TRON Nile"},
		{give: "Токен", want: "USDT"},
		{give: "Адрес для оплаты", want: "TXYZaddress"},
		{give: "Действителен до", want: "10.11.2026 10:00 UTC"},
		{give: "Требуемые подтверждения", want: "20"},
		{give: "Номер заказа", want: "ORD-7"},
		{give: "Оплачен", want: "10.11.2026 09:30 UTC"},
	}
	for _, tt := range tests {
		got, ok := headerValue(data, tt.give)
		require.True(t, ok, tt.give)
		require.Equal(t, tt.want, got, tt.give)
	}
	for _, absent := range []string{"Внешний ID", "Описание", "Подтверждён", "Завершён", "Отозван"} {
		_, ok := headerValue(data, absent)
		require.False(t, ok, "unset optional field %q must be omitted", absent)
	}
	got, _ := rowValue(data, "Получено")
	require.Equal(t, "100,50 USDT", got)
	_, ok := rowValue(data, "Переплата")
	require.False(t, ok, "zero overpayment is omitted")
	require.Equal(t, "100,50 USDT", data.Table.Totals[0].Value)
}

func TestCryptoInvoicePrintDataMarksRestrictedFieldsHidden(t *testing.T) {
	f := newPrintFixture()
	policy := &security.FieldPolicy{AllowedFields: []string{"*", "-expected_amount", "-overpaid_amount", "-wallet_id", "-order_id"}}
	ctx := printContext(t, f.repo, _allPrintRefs, policy)

	// The handler passes the read projection; masked fields arrive zeroed.
	data, err := buildCryptoInvoicePrintData(ctx, security.ProjectForRead(f.doc, policy), policy)
	require.NoError(t, err)

	require.Equal(t, _printHidden, data.Table.Totals[0].Value, "a hidden amount must not print as zero")
	overpaid, ok := rowValue(data, "Переплата")
	require.True(t, ok, "a hidden overpayment is shown as hidden, not omitted")
	require.Equal(t, _printHidden, overpaid)
	address, _ := headerValue(data, "Адрес для оплаты")
	require.Equal(t, _printHidden, address)
	order, _ := headerValue(data, "Номер заказа")
	require.Equal(t, _printHidden, order)
	require.Zero(t, f.repo.reads["cat_wallets"], "a hidden link must not be followed")
	received, _ := rowValue(data, "Получено")
	require.Equal(t, "100,50 USDT", received)
}

func TestCryptoInvoicePrintDataNeverInventsTokenScale(t *testing.T) {
	f := newPrintFixture()
	permissions := []string{"cat_merchants:read", "cat_wallets:read"}
	ctx := printContext(t, f.repo, permissions, nil)

	data, err := buildCryptoInvoicePrintData(ctx, f.doc, nil)
	require.NoError(t, err)

	token, _ := headerValue(data, "Токен")
	require.Equal(t, _printHidden, token)
	network, _ := headerValue(data, "Сеть")
	require.Equal(t, _printHidden, network)
	require.Equal(t, "100500000 мин. ед.", data.Table.Totals[0].Value)
	require.Zero(t, f.repo.reads["cat_tokens"], "a forbidden entity must not be queried")
}

func TestCryptoInvoicePrintDataRequiresAuthorizedReader(t *testing.T) {
	f := newPrintFixture()
	_, err := buildCryptoInvoicePrintData(context.Background(), f.doc, nil)
	require.Error(t, err)
	require.Equal(t, http.StatusForbidden, apperror.GetHTTPStatus(err))
}

func TestCryptoInvoicePrintFormEscapesReferenceValues(t *testing.T) {
	f := newPrintFixture()
	data, err := buildCryptoInvoicePrintData(printContext(t, f.repo, _allPrintRefs, nil), f.doc, nil)
	require.NoError(t, err)
	renderer, err := printadapter.NewRenderer()
	require.NoError(t, err)
	form := CryptoInvoicePrintForms()[0]

	var html bytes.Buffer
	require.NoError(t, renderer.Render(&html, form.Template, data))
	require.Contains(t, html.String(), "&lt;b&gt;Acme&lt;/b&gt;")
	require.NotContains(t, html.String(), "<b>Acme</b>")
	require.Contains(t, html.String(), "TXYZaddress")
	require.Contains(t, html.String(), "100,50 USDT")
	require.False(t, strings.Contains(html.String(), "onclick"), "print forms carry no active content")

	var docx bytes.Buffer
	require.NoError(t, printadapter.RenderDOCX(&docx, data))
	require.Equal(t, "PK", docx.String()[:2])
}
