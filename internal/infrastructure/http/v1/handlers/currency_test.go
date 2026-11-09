package handlers

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
	"metapus/internal/domain"
	"metapus/internal/domain/catalogs/currency"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/http/v1/middleware"
)

func TestCreateCurrencyInvalidNumericCodeReturnsBadRequest(t *testing.T) {
	// No repository or transaction manager: invalid input must be rejected
	// before the persistence layer is reached.
	service := domain.NewCatalogService(domain.CatalogServiceConfig[*currency.Currency]{Definition: &currency.Definition})
	handler := NewCatalogHandler(NewBaseHandler(), CatalogHandlerConfig[*currency.Currency, dto.CreateCurrencyRequest, dto.UpdateCurrencyRequest]{
		Service:      service,
		MapCreateDTO: func(req dto.CreateCurrencyRequest) *currency.Currency { return req.ToEntity() },
	})
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
