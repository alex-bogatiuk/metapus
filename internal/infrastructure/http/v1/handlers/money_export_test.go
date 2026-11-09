package handlers

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"metapus/internal/core/json"
	"metapus/internal/core/types"
	"metapus/internal/infrastructure/http/httpvalue"
	"metapus/internal/infrastructure/listexport"
	printadapter "metapus/internal/infrastructure/printing"
)

func TestListExportUsesDeclaredMoneyAndDoesNotRescaleQuantity(t *testing.T) {
	response := struct {
		TotalAmount httpvalue.MinorUnits `json:"totalAmount"`
		VATPercent  int                  `json:"vatPercent"`
		Quantity    types.Quantity       `json:"quantity"`
		Currency    map[string]any       `json:"currency"`
	}{9007199254740993, 20, 25000, map[string]any{"decimalPlaces": 9}}
	row, err := dtoToMap(response)
	require.NoError(t, err)
	require.NoError(t, resolveExportValues(row, []string{"totalAmount", "vatPercent", "quantity"}))
	require.Equal(t, listexport.MoneyCell{MinorUnits: 9007199254740993, DecimalPlaces: 9}, row["totalAmount"])
	require.Equal(t, json.Number("20"), row["vatPercent"])
	require.Equal(t, json.Number("2.5000"), row["quantity"])
	require.Equal(t, "-9\u00a0223\u00a0372\u00a0036,854775808", printadapter.FormatMoney(types.MinorUnits(mathMinInt64), 9))
}

const mathMinInt64 = -9223372036854775808

func TestTablePartExportRejectsNumericMoneyAndPreservesExactCells(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, raw := range []string{`"9007199254740993"`, `"123"`, `123`, `null`, `"9223372036854775808"`, `"1.2"`} {
		for _, scale := range []int{0, 2, 3, 9} {
			t.Run(fmt.Sprintf("%s/%d", raw, scale), func(t *testing.T) {
				rec := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(rec)
				body := fmt.Sprintf(`{"title":"Товары","documentTitle":"Документ","columns":[{"key":"amount","label":"Сумма","type":"money","decimalPlaces":%d}],"rows":[{"amount":%s}]}`, scale, raw)
				ctx.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
				ctx.Request.Header.Set("Content-Type", "application/json")
				ExportTablePart(ctx)
				if raw != `"9007199254740993"` && raw != `"123"` {
					require.NotEmpty(t, ctx.Errors)
					require.Empty(t, rec.Body.Bytes(), "no partial workbook on invalid money")
					return
				}
				require.Empty(t, ctx.Errors)
				workbook, err := excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
				require.NoError(t, err)
				defer func() { require.NoError(t, workbook.Close()) }()
				cellType, err := workbook.GetCellType("Sheet1", "A4")
				require.NoError(t, err)
				if raw == `"9007199254740993"` {
					value, err := workbook.GetCellValue("Sheet1", "A4")
					require.NoError(t, err)
					expected, err := types.FormatMinorUnits(9007199254740993, scale)
					require.NoError(t, err)
					require.Equal(t, expected, value)
					require.Contains(t, []excelize.CellType{excelize.CellTypeInlineString, excelize.CellTypeSharedString}, cellType)
				} else {
					require.Contains(t, []excelize.CellType{excelize.CellTypeNumber, excelize.CellTypeUnset}, cellType)
				}
			})
		}
	}
}
