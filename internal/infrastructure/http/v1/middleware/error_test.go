package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/json"
)

func TestErrorHandlerSerializesWrappedWalletPoolExhausted(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const networkID = "network-123"
	router := gin.New()
	router.Use(ErrorHandler())
	router.GET("/fail", func(c *gin.Context) {
		err := fmt.Errorf("lease wallet for invoice: %w", apperror.NewWalletPoolExhausted(networkID, 45))
		_ = c.Error(err)
		c.Abort()
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/fail", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusServiceUnavailable, w.Body.String())
	}
	if got := w.Header().Get("Retry-After"); got != "45" {
		t.Fatalf("Retry-After = %q, want 45", got)
	}

	var body struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if body.Code != apperror.CodeWalletPoolExhausted {
		t.Fatalf("code = %q, want %q", body.Code, apperror.CodeWalletPoolExhausted)
	}
	if body.Message != "Нет свободного кошелька для приема платежа. Повторите позже или обратитесь в поддержку." {
		t.Fatalf("message = %q", body.Message)
	}
	if body.Details["resource"] != "wallet_pool" {
		t.Fatalf("details.resource = %v, want wallet_pool", body.Details["resource"])
	}
	if body.Details["networkId"] != networkID {
		t.Fatalf("details.networkId = %v, want %s", body.Details["networkId"], networkID)
	}
}

func TestErrorHandlerAddsRetryAfterForMerchantLeaseLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(ErrorHandler())
	router.GET("/fail", func(c *gin.Context) {
		_ = c.Error(apperror.NewMerchantWalletLeaseLimitExceeded("network-123", 17))
		c.Abort()
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/fail", nil))

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}
	if got := w.Header().Get("Retry-After"); got != "17" {
		t.Fatalf("Retry-After = %q, want 17", got)
	}
}

func TestErrorHandlerNeverSerializesInvalidRequestCause(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(ErrorHandler())
	router.POST("/fail", func(c *gin.Context) {
		_ = c.Error(apperror.NewInvalidRequest(fmt.Errorf("json: cannot unmarshal secret_internal_type")))
		c.Abort()
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/fail", nil))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if body := w.Body.String(); body == "" || strings.Contains(body, "secret_internal_type") {
		t.Fatalf("internal cause leaked in response: %s", body)
	}
}
