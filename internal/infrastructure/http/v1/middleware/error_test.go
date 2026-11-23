package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
)

func TestErrorHandlerSerializesWrappedAppErrorWithRetryAfter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(ErrorHandler())
	router.GET("/fail", func(c *gin.Context) {
		capacity := &apperror.AppError{
			Code:       "CAPACITY_EXHAUSTED",
			Message:    "Ресурс временно недоступен.",
			HTTPStatus: http.StatusServiceUnavailable,
			Details:    map[string]any{"resource": "pool", "retryAfterSeconds": 45},
		}
		_ = c.Error(fmt.Errorf("reserve capacity: %w", capacity))
		c.Abort()
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/fail", nil))

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
	if body.Code != "CAPACITY_EXHAUSTED" || body.Message != "Ресурс временно недоступен." {
		t.Fatalf("body = %+v", body)
	}
	if body.Details["resource"] != "pool" {
		t.Fatalf("details.resource = %v, want pool", body.Details["resource"])
	}
}

func TestErrorHandlerOmitsRetryAfterWithoutHint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(ErrorHandler())
	router.GET("/fail", func(c *gin.Context) {
		_ = c.Error(apperror.NewConflict("busy"))
		c.Abort()
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/fail", nil))

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusConflict)
	}
	if got := w.Header().Get("Retry-After"); got != "" {
		t.Fatalf("Retry-After = %q, want empty", got)
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
