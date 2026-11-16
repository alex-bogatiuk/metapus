package cryptohandlers

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"metapus/internal/application/readaccess"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/security"
	"metapus/internal/core/types"
	"metapus/internal/domain/documents/crypto_invoice"
	"metapus/internal/domain/printing"
	printadapter "metapus/internal/infrastructure/printing"
	"metapus/internal/metadata"
)

const (
	_printHidden     = "скрыто"
	_printNotSet     = "—"
	_printTimeLayout = "02.01.2006 15:04 UTC"
)

// cryptoInvoicePrintRefs holds the reference fields the form shows. Each value
// comes from an authorized projection; a missing value is never guessed.
type cryptoInvoicePrintRefs struct {
	merchant, tokenSymbol, network, walletAddress string
	merchantHidden, tokenHidden, networkHidden    bool
	walletHidden                                  bool
	tokenDecimals                                 int
	hasTokenDecimals                              bool
}

// buildCryptoInvoicePrintData renders the read projection of an invoice. doc
// already has restricted fields zeroed; policy distinguishes hidden from zero.
func buildCryptoInvoicePrintData(ctx context.Context, doc *crypto_invoice.CryptoInvoice, policy *security.FieldPolicy) (*printing.PrintData, error) {
	reads, ok := readaccess.FromContext(ctx)
	if !ok {
		return nil, apperror.NewForbidden("authorized reference reader is unavailable")
	}
	visible := func(column string) bool { return policy == nil || policy.IsFieldAllowed(column) }
	refs, err := readCryptoInvoicePrintRefs(ctx, reads, doc, visible)
	if err != nil {
		return nil, err
	}

	amount := func(column string, value types.RawAmount) string {
		if !visible(column) {
			return _printHidden
		}
		if refs.hasTokenDecimals {
			return printadapter.FormatRawUnits(value, refs.tokenDecimals) + suffix(refs.tokenSymbol)
		}
		// Without an authorized scale the exact raw units are shown as such.
		return value.String() + " мин. ед." + suffix(refs.tokenSymbol)
	}
	text := func(column, value string) string {
		switch {
		case !visible(column):
			return _printHidden
		case value == "":
			return _printNotSet
		default:
			return value
		}
	}

	subtitle := "№ " + text("number", doc.Number)
	if visible("date") && !doc.Date.IsZero() {
		subtitle += " от " + doc.Date.UTC().Format("02.01.2006")
	}

	status := string(doc.Status)
	if label, ok := metadata.EnumLabel(doc.Status); ok {
		status = label
	}

	header := [][]printing.PrintHeaderField{
		{{Label: "Мерчант", Value: refValue(refs.merchant, refs.merchantHidden)}, {Label: "Статус", Value: text("status", status)}},
		{{Label: "Сеть", Value: refValue(refs.network, refs.networkHidden)}, {Label: "Токен", Value: refValue(refs.tokenSymbol, refs.tokenHidden)}},
		{{Label: "Адрес для оплаты", Value: refValue(refs.walletAddress, refs.walletHidden)}},
		{{Label: "Действителен до", Value: timeValue(visible("expires_at"), &doc.ExpiresAt)}, {Label: "Требуемые подтверждения", Value: confirmations(visible("required_confirmations"), doc.RequiredConfirmations)}},
	}
	optional := []struct {
		label, column, value string
	}{
		{"Номер заказа", "order_id", doc.OrderID},
		{"Внешний ID", "external_id", doc.ExternalID},
		{"Описание", "description", doc.Description},
	}
	for _, field := range optional {
		// Absent optional references are omitted; hidden ones are marked so a
		// reader does not mistake a restricted field for an empty one.
		if !visible(field.column) || field.value != "" {
			header = append(header, []printing.PrintHeaderField{{Label: field.label, Value: text(field.column, field.value)}})
		}
	}
	for _, event := range []struct {
		label, column string
		at            *time.Time
	}{
		{"Оплачен", "paid_at", doc.PaidAt},
		{"Подтверждён", "confirmed_at", doc.ConfirmedAt},
		{"Завершён", "completed_at", doc.CompletedAt},
		{"Отозван", "reversed_at", doc.ReversedAt},
	} {
		if !visible(event.column) || (event.at != nil && !event.at.IsZero()) {
			header = append(header, []printing.PrintHeaderField{{Label: event.label, Value: timeValue(visible(event.column), event.at)}})
		}
	}

	rows := []printing.PrintTableRow{
		{Values: []string{"Получено", amount("received_amount", doc.ReceivedAmount)}},
		{Values: []string{"Подтверждено", amount("confirmed_amount", doc.ConfirmedAmount)}},
		{Values: []string{"Зачислено", amount("credited_amount", doc.CreditedAmount)}},
	}
	if !visible("overpaid_amount") || doc.OverpaidAmount.IsPositive() {
		rows = append(rows, printing.PrintTableRow{Values: []string{"Переплата", amount("overpaid_amount", doc.OverpaidAmount)}})
	}

	return &printing.PrintData{
		FormLabel: "Инвойс",
		Table: &printing.PrintTable{
			Title:      "Инвойс",
			Subtitle:   subtitle,
			HeaderRows: header,
			Columns:    []string{"Показатель", "Сумма"},
			Rows:       rows,
			Totals:     []printing.PrintTotalLine{{Label: "Сумма к оплате", Value: amount("expected_amount", doc.ExpectedAmount), Grand: true}},
		},
	}, nil
}

func readCryptoInvoicePrintRefs(ctx context.Context, reads *readaccess.Service, doc *crypto_invoice.CryptoInvoice, visible func(string) bool) (cryptoInvoicePrintRefs, error) {
	var refs cryptoInvoicePrintRefs

	merchant, ok, err := readPrintRef(ctx, reads, "cat_merchants", visible("merchant_id"), &doc.MerchantID)
	if err != nil {
		return refs, err
	}
	refs.merchant, refs.merchantHidden = stringField(merchant, ok, "name")

	token, ok, err := readPrintRef(ctx, reads, "cat_tokens", visible("token_id"), &doc.TokenID)
	if err != nil {
		return refs, err
	}
	refs.tokenSymbol, refs.tokenHidden = stringField(token, ok, "symbol")
	if ok {
		refs.tokenDecimals, refs.hasTokenDecimals = token.Fields["decimalPlaces"].(int)
	}

	var networkID *id.ID
	if ok {
		networkID = idField(token.Fields["networkId"])
	}
	network, ok, err := readPrintRef(ctx, reads, "cat_blockchain_networks", networkID != nil, networkID)
	if err != nil {
		return refs, err
	}
	refs.network, refs.networkHidden = stringField(network, ok, "name")

	// An invoice may legitimately have no leased wallet yet; that is "not set",
	// whereas a link or target the caller may not read is "hidden".
	if visible("wallet_id") && doc.WalletID == nil {
		return refs, nil
	}
	wallet, ok, err := readPrintRef(ctx, reads, "cat_wallets", visible("wallet_id"), doc.WalletID)
	if err != nil {
		return refs, err
	}
	refs.walletAddress, refs.walletHidden = stringField(wallet, ok, "address")
	return refs, nil
}

// readPrintRef reads one referenced row through the authorized reader. A
// reference the caller may not see (FLS on the link, RBAC or RLS on the target)
// reports ok=false without failing the whole form, as reference columns do.
func readPrintRef(ctx context.Context, reads *readaccess.Service, table string, allowed bool, ref *id.ID) (readaccess.Projection, bool, error) {
	if !allowed || ref == nil || id.IsNil(*ref) {
		return readaccess.Projection{}, false, nil
	}
	rows, err := reads.ReadIDs(ctx, table, []id.ID{*ref})
	if err != nil {
		if apperror.GetHTTPStatus(err) == http.StatusForbidden {
			return readaccess.Projection{}, false, nil
		}
		return readaccess.Projection{}, false, err
	}
	if len(rows) == 0 {
		return readaccess.Projection{}, false, nil
	}
	return rows[0], true, nil
}

// stringField returns the value and whether it must be shown as hidden.
func stringField(row readaccess.Projection, ok bool, name string) (string, bool) {
	if !ok {
		return "", true
	}
	value, present := row.Fields[name].(string)
	return value, !present
}

func idField(value any) *id.ID {
	switch v := value.(type) {
	case id.ID:
		if !id.IsNil(v) {
			return &v
		}
	case *id.ID:
		if v != nil && !id.IsNil(*v) {
			return v
		}
	}
	return nil
}

func refValue(value string, hidden bool) string {
	switch {
	case hidden:
		return _printHidden
	case value == "":
		return _printNotSet
	default:
		return value
	}
}

func timeValue(visible bool, at *time.Time) string {
	switch {
	case !visible:
		return _printHidden
	case at == nil || at.IsZero():
		return _printNotSet
	default:
		return at.UTC().Format(_printTimeLayout)
	}
}

func confirmations(visible bool, n int) string {
	switch {
	case !visible:
		return _printHidden
	case n <= 0:
		return _printNotSet
	default:
		return strconv.Itoa(n)
	}
}

func suffix(symbol string) string {
	if symbol == "" {
		return ""
	}
	return " " + symbol
}
