package cryptohandlers

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/http/cryptohttp/cryptodto"
	"github.com/stretchr/testify/require"
)

func TestPortalInvoiceCSVAppendsAndEscapesOrderReference(t *testing.T) {
	var output bytes.Buffer
	orderID := "ORD,\"99881\"\nSAP-45001"
	externalID := "1C-DOC-42"
	require.NoError(t, writePortalInvoicesCSV(&output, []cryptodto.PortalInvoiceItem{{
		Number: "CI-1", Status: "completed", Symbol: "USDT", Network: "TRON",
		Amount: "100", ReceivedAmount: "100", ProcessingFee: "1", NetAmount: "99", PaymentCount: 1,
		OrderID: orderID, ExternalID: externalID,
	}, {Number: "CI-2"}}))
	require.True(t, strings.HasPrefix(output.String(), "\xEF\xBB\xBF"))
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(output.String(), "\xEF\xBB\xBF"))).ReadAll()
	require.NoError(t, err)
	require.Equal(t, []string{"Number", "Status", "Token", "Network", "Amount", "Received", "PaymentCount", "Fee", "Net", "Date", "Order ID", "External ID"}, rows[0])
	require.Equal(t, orderID, rows[1][10])
	require.Equal(t, externalID, rows[1][11])
	require.Equal(t, "100", rows[1][4])
	require.Empty(t, rows[2][10])
	require.Empty(t, rows[2][11])
}

func TestPortalInvoiceCSVNeutralizesFormulaReferencesWithoutChangingSource(t *testing.T) {
	for _, orderID := range []string{
		"=1+1", "+1+1", "-1+1", "@SUM(1,1)",
		"＝1+1", "＋1+1", "－1+1", "＠SUM(1,1)",
		" =1+1", "\t=1+1", "\r=1+1", "\n=1+1", "\u00a0=1+1", "\ufeff=1+1",
		"'=1+1", "''=1+1", " ' =1+1",
		"=1+2\";=1+2", "=1+2'\" ;,=1+2",
	} {
		t.Run(orderID, func(t *testing.T) {
			item := cryptodto.PortalInvoiceItem{Number: "CI-1", Amount: "100", OrderID: orderID, ExternalID: orderID}
			var output bytes.Buffer
			require.NoError(t, writePortalInvoicesCSV(&output, []cryptodto.PortalInvoiceItem{item}))
			require.Equal(t, orderID, item.OrderID)
			require.Equal(t, orderID, item.ExternalID)
			rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(output.String(), "\xEF\xBB\xBF"))).ReadAll()
			require.NoError(t, err)
			require.Len(t, rows, 2)
			require.Len(t, rows[1], 12)
			require.Equal(t, "\t"+orderID, rows[1][10])
			require.Equal(t, "\t"+orderID, rows[1][11])
			require.Equal(t, "100", rows[1][4])
			require.Contains(t, output.String(), ",\"\t")
		})
	}
	for _, orderID := range []string{"", "ORD-1", "00001", "ORD=1", "ORD,+1", "\";=1+1", "'ORD-1"} {
		require.Equal(t, orderID, portalCSVText(orderID))
	}
}
