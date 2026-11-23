package handlers

import (
	"context"
	"github.com/alex-bogatiuk/metapus/internal/application/readaccess"
	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/domain/reports/schema"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/reporting/compiler"
	"github.com/alex-bogatiuk/metapus/internal/metadata"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDatasetRouteBindsEveryExecutionMode(t *testing.T) {
	// A denied dataset allows testing omitted/body mismatch without any DB.
	registry := metadata.NewRegistry()
	comp := compiler.NewCompiler(registry, []*schema.Dataset{{Key: "actual", Permission: "report:actual:read"}}, readaccess.New(nil, nil, nil))
	handler := NewDatasetReportHandler(NewBaseHandler(), comp, registry)
	for _, mode := range []struct {
		name    string
		handler gin.HandlerFunc
	}{
		{"execute", handler.HandleExecute("actual")}, {"export", handler.HandleExport("actual")}, {"grouped", handler.HandleGrouped("actual")},
	} {
		t.Run(mode.name, func(t *testing.T) {
			for _, tc := range []struct {
				body, code string
				status     int
			}{
				{`{"dataset":"other"}`, "DATASET_MISMATCH", http.StatusBadRequest},
				{`{}`, "", http.StatusForbidden}, {`{"dataset":"actual"}`, "", http.StatusForbidden},
			} {
				router := gin.New()
				router.Use(func(c *gin.Context) {
					c.Next()
					if len(c.Errors) > 0 {
						err := c.Errors.Last().Err
						c.JSON(apperror.GetHTTPStatus(err), gin.H{"code": apperror.Public(err).Code})
					}
				})
				router.POST("/actual", mode.handler)
				ctx := appctx.WithUser(context.Background(), &appctx.UserContext{UserID: id.New().String()})
				req := httptest.NewRequest(http.MethodPost, "/actual", strings.NewReader(tc.body)).WithContext(ctx)
				req.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				router.ServeHTTP(response, req)
				require.Equal(t, tc.status, response.Code, response.Body.String())
				if tc.code != "" {
					require.Contains(t, response.Body.String(), tc.code)
				}
			}
		})
	}
}
