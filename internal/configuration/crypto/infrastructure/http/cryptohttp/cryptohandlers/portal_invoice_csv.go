package cryptohandlers

import (
	"encoding/csv"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/http/cryptohttp/cryptodto"
)

func writePortalInvoicesCSV(w io.Writer, items []cryptodto.PortalInvoiceItem) error {
	// BOM preserves the existing Excel UTF-8 import behavior.
	if _, err := io.WriteString(w, "\xEF\xBB\xBF"); err != nil {
		return err
	}
	writer := csv.NewWriter(w)
	if err := writer.Write([]string{"Number", "Status", "Token", "Network", "Amount", "Received", "PaymentCount", "Fee", "Net", "Date", "Order ID", "External ID"}); err != nil {
		return err
	}
	for _, inv := range items {
		if err := writer.Write([]string{
			portalCSVText(inv.Number), portalCSVText(inv.Status), portalCSVText(inv.Symbol), portalCSVText(inv.Network), inv.Amount, inv.ReceivedAmount,
			strconv.Itoa(inv.PaymentCount), inv.ProcessingFee, inv.NetAmount, inv.CreatedAt, portalCSVText(inv.OrderID), portalCSVText(inv.ExternalID),
		}); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

// A leading tab inside a quoted CSV field makes Excel import formula-like
// identifiers as text. encoding/csv quotes leading whitespace automatically.
// This export-only prefix never changes the stored identifier or API response.
func portalCSVText(value string) string {
	if value == "" {
		return value
	}
	if strings.ContainsRune("\t\r\n", rune(value[0])) {
		return "\t" + value
	}
	// Excel can strip existing apostrophe text markers when a CSV is saved.
	candidate := strings.TrimLeftFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || r == '\uFEFF' || r == '\''
	})
	first, _ := utf8.DecodeRuneInString(candidate)
	if strings.ContainsRune("=+-@＝＋－＠", first) {
		return "\t" + value
	}
	return value
}
