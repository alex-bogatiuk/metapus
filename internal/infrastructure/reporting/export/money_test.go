package export

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
	reportcontract "metapus/internal/domain/reports/contract"
	"metapus/internal/domain/reports/tabular"
)

func TestCryptoReportExportsBeyondInt64RemainExactText(t *testing.T) {
	sources := &reportcontract.MoneySources{AmountType: reportcontract.AmountTypeRaw, CurrencyID: "id", CurrencyCode: "code", DecimalPlaces: "scale"}
	meta := reportcontract.ReportMeta{Name: "Токены", Columns: []reportcontract.ReportColumn{{Key: "amount", Label: "Сумма", Type: "money", Money: sources}}}
	items := []map[string]any{{"amount": strings.Repeat("9", 78), "id": "eth", "code": "ETH", "scale": 18}}
	want := strings.Repeat("9", 60) + "." + strings.Repeat("9", 18)
	var data bytes.Buffer
	require.NoError(t, CSV(&data, meta, items, nil))
	require.Contains(t, data.String(), want)
	data.Reset()
	require.NoError(t, XLSX(&data, meta, items, nil, nil))
	workbook, err := excelize.OpenReader(bytes.NewReader(data.Bytes()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, workbook.Close()) })
	value, err := workbook.GetCellValue("Sheet1", "A4")
	require.NoError(t, err)
	require.Equal(t, want, value)
	kind, err := workbook.GetCellType("Sheet1", "A4")
	require.NoError(t, err)
	require.Contains(t, []excelize.CellType{excelize.CellTypeSharedString, excelize.CellTypeInlineString}, kind)
}

func TestReportMoneyUnitsAreExactInCSVXLSXAndTotals(t *testing.T) {
	sources := &reportcontract.MoneySources{CurrencyID: "currencyId", CurrencyCode: "currencyCode", DecimalPlaces: "decimalPlaces"}
	meta := reportcontract.ReportMeta{Name: "Точность сумм", Columns: []reportcontract.ReportColumn{
		{Key: "name", Label: "Документ", Type: "string"},
		{Key: "amount", Label: "Сумма", Type: "money", Money: sources},
		{Key: "currencyCode", Label: "Валюта", Type: "string"},
	}, Totals: []reportcontract.ReportTotal{{Column: "amount", Func: "sum", Money: sources}}}
	items := []map[string]any{
		{"name": "JPY", "amount": "123", "currencyId": "jpy", "currencyCode": "JPY", "decimalPlaces": 0},
		{"name": "USD", "amount": "123", "currencyId": "usd", "currencyCode": "USD", "decimalPlaces": 2},
		{"name": "KWD", "amount": "123", "currencyId": "kwd", "currencyCode": "KWD", "decimalPlaces": 3},
		{"name": "TON", "amount": "123", "currencyId": "ton", "currencyCode": "TON", "decimalPlaces": 9},
		{"name": "Large", "amount": "9007199254740993", "currencyId": "usd", "currencyCode": "USD", "decimalPlaces": 2},
		{"name": "Minimum", "amount": "-9223372036854775808", "currencyId": "ton", "currencyCode": "TON", "decimalPlaces": 9},
	}
	expected := []string{"123", "1.23", "0.123", "0.000000123", "90071992547409.93", "-9223372036.854775808"}
	var data bytes.Buffer
	require.NoError(t, CSV(&data, meta, items, nil))
	csvRows, err := csv.NewReader(bytes.NewReader(data.Bytes())).ReadAll()
	require.NoError(t, err)
	for i, want := range expected {
		require.Equal(t, want, csvRows[i+1][1])
	}
	data.Reset()
	require.NoError(t, XLSX(&data, meta, items, nil, nil))
	workbook, err := excelize.OpenReader(bytes.NewReader(data.Bytes()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, workbook.Close()) })
	for i, want := range expected {
		cell, err := excelize.CoordinatesToCellName(2, i+4)
		require.NoError(t, err)
		value, err := workbook.GetCellValue("Sheet1", cell)
		require.NoError(t, err)
		require.Equal(t, want, value)
		kind, err := workbook.GetCellType("Sheet1", cell)
		require.NoError(t, err)
		if i >= 4 {
			require.Contains(t, []excelize.CellType{excelize.CellTypeSharedString, excelize.CellTypeInlineString}, kind)
		} else {
			require.Contains(t, []excelize.CellType{excelize.CellTypeNumber, excelize.CellTypeUnset}, kind)
		}
	}
	totals, err := tabular.Aggregate(items, meta.Totals)
	require.NoError(t, err)
	amounts := totals["amount"].([]tabular.MoneyTotal)
	require.Len(t, amounts, 4)
	require.Equal(t, "9007199254741116", amounts[1].Amount)
	require.Equal(t, "-9223372036854775685", amounts[3].Amount)
	total, err := workbook.GetCellValue("Sheet1", "B10")
	require.NoError(t, err)
	require.Contains(t, total, "90071992547411.16 USD")
	require.Contains(t, total, "0.123 KWD")
}

func TestReportRejectsInvalidMonetaryProjection(t *testing.T) {
	sources := &reportcontract.MoneySources{CurrencyID: "id", CurrencyCode: "code", DecimalPlaces: "scale"}
	for _, value := range []any{float64(123), "1.5", "9223372036854775808", "01"} {
		_, err := tabular.Aggregate([]map[string]any{{"amount": value, "id": "x", "code": "X", "scale": 2}}, []reportcontract.ReportTotal{{Column: "amount", Func: "sum", Money: sources}})
		require.Error(t, err)
	}
	_, err := tabular.Aggregate([]map[string]any{
		{"amount": "1", "id": "x", "code": "X", "scale": 2},
		{"amount": "1", "id": "x", "code": "X", "scale": 3},
	}, []reportcontract.ReportTotal{{Column: "amount", Func: "sum", Money: sources}})
	require.Error(t, err)
}
