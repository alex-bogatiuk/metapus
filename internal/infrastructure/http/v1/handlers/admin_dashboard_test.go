package handlers

import (
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	appctx "metapus/internal/core/context"
	"metapus/internal/infrastructure/http/v1/middleware"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDashboardRoutesRejectNonSystemAdmins(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, roles := range [][]string{{"user"}, {"security_admin"}} {
		t.Run(roles[0], func(t *testing.T) {
			router := gin.New()
			router.Use(middleware.ErrorHandler(), func(c *gin.Context) {
				c.Request = c.Request.WithContext(appctx.WithUser(c.Request.Context(), &appctx.UserContext{Roles: roles}))
				c.Next()
			})
			NewAdminDashboardHandler(nil).RegisterRoutes(router.Group("/api/v1"))
			for _, path := range []string{"queues", "finance", "analytics"} {
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/dashboard/"+path, nil))
				require.Equal(t, http.StatusForbidden, response.Code)
			}
		})
	}
}
