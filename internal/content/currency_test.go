package content

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/apperror"
	"metapus/internal/core/json"
	"metapus/internal/core/security"
	_ "metapus/internal/infrastructure/http/jsoncodec"
	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/http/v1/middleware"
)

func TestCreateCurrencyInvalidNumericCodeReturnsBadRequest(t *testing.T) {
	// The declared currency catalog validates the model before its service
	// reaches the repository or a transaction.
	gin.SetMode(gin.TestMode)
	reg := v1.NewFactoryRegistry()
	reg.RegisterCatalog(currencyRegistration())
	handler := reg.Catalogs()[0].Build(v1.CatalogDeps{BaseHandler: handlers.NewBaseHandler()})
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	router.POST("/api/v1/catalog/currencies", handler.Create)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/catalog/currencies", strings.NewReader(
		`{"code":"PYUSD","name":"PYUSD","isoCode":"PYUSD","isoNumericCode":"PYUSD","symbol":"PYUSD","decimalPlaces":6}`,
	))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(security.WithDataScope(request.Context(), &security.DataScope{IsAdmin: true}))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details struct {
			Field string `json:"field"`
		} `json:"details"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.Equal(t, apperror.CodeValidation, body.Code)
	require.Equal(t, "isoNumericCode", body.Details.Field)
	require.Contains(t, body.Message, "3 цифры")
}
