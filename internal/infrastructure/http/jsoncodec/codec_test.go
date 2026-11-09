package jsoncodec

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGinJSONBindingUsesStrictV2RequestContract(t *testing.T) {
	gin.SetMode(gin.TestMode)

	type request struct {
		Name string `json:"name" binding:"required"`
	}
	router := gin.New()
	router.POST("/", func(c *gin.Context) {
		var input request
		if err := c.ShouldBindJSON(&input); err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		c.Status(http.StatusNoContent)
	})

	tests := []struct {
		name string
		body string
		want int
	}{
		{name: "exact field", body: `{"name":"metapus"}`, want: http.StatusNoContent},
		{name: "wrong case", body: `{"Name":"metapus"}`, want: http.StatusBadRequest},
		{name: "unknown field", body: `{"name":"metapus","extra":true}`, want: http.StatusBadRequest},
		{name: "duplicate field", body: `{"name":"a","name":"b"}`, want: http.StatusBadRequest},
		{name: "trailing value", body: `{"name":"a"}{"name":"b"}`, want: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, req)
			require.Equal(t, tt.want, recorder.Code)
		})
	}
}
