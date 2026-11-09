package jsoncodec

import (
	"bytes"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"metapus/internal/core/json"
	"metapus/internal/core/types"
	"metapus/internal/infrastructure/http/httpvalue"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestHTTPMoneyDoesNotChangeStoredProtocolCodec(t *testing.T) {
	value := struct {
		Amount types.MinorUnits `json:"amount"`
		Lines  []map[string]any `json:"lines"`
	}{types.MinorUnits(math.MinInt64), []map[string]any{{"unitPrice": types.MinorUnits(9007199254740993)}}}
	stored, err := json.Marshal(value)
	require.NoError(t, err)
	require.JSONEq(t, `{"amount":-9223372036854775808,"lines":[{"unitPrice":9007199254740993}]}`, string(stored))
	wire, err := Marshal(value)
	require.NoError(t, err)
	require.JSONEq(t, `{"amount":"-9223372036854775808","lines":[{"unitPrice":"9007199254740993"}]}`, string(wire))
	var stream bytes.Buffer
	require.NoError(t, (codec{}).NewEncoder(&stream).Encode(value))
	require.JSONEq(t, string(wire), stream.String())
	indented, err := (codec{}).MarshalIndent(value, "", "  ")
	require.NoError(t, err)
	require.JSONEq(t, string(wire), string(indented))
	after, err := json.Marshal(value)
	require.NoError(t, err)
	require.Equal(t, stored, after)
}

func TestGinMoneyBindingUsesStringsForExplicitAndGenericDTOs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/", func(c *gin.Context) {
		var input struct {
			UnitPrice httpvalue.MinorUnits `json:"unitPrice"`
			Amount    types.MinorUnits     `json:"amount"`
		}
		if err := c.ShouldBindJSON(&input); err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		c.JSON(http.StatusOK, input)
	})
	for _, body := range []string{
		`{"unitPrice":"9007199254740993","amount":"9223372036854775807"}`,
		`{"unitPrice":"0","amount":"-9223372036854775808"}`,
		`{"unitPrice":100,"amount":"100"}`,
		`{"unitPrice":"100","amount":100}`,
		`{"unitPrice":"100","amount":null}`,
		`{"unitPrice":"100","amount":"9223372036854775808"}`,
	} {
		t.Run(body, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(rec, req)
			if strings.Contains(body, `"9007199254740993"`) || strings.Contains(body, `"-9223372036854775808"`) {
				require.Equal(t, http.StatusOK, rec.Code)
				require.JSONEq(t, body, rec.Body.String())
			} else {
				require.Equal(t, http.StatusBadRequest, rec.Code)
			}
		})
	}
	// UseNumber must compose with (not replace) the monetary type override.
	var input struct {
		Amount types.MinorUnits `json:"amount"`
		Other  any              `json:"other"`
	}
	decoder := (codec{}).NewDecoder(strings.NewReader(`{"amount":"9007199254740993","other":9007199254740993}`))
	decoder.UseNumber()
	require.NoError(t, decoder.Decode(&input))
	require.Equal(t, types.MinorUnits(9007199254740993), input.Amount)
	require.Equal(t, json.Number("9007199254740993"), input.Other)
	decoder = (codec{}).NewDecoder(strings.NewReader(`{"amount":1}`))
	decoder.UseNumber()
	require.Error(t, decoder.Decode(&input))
}
