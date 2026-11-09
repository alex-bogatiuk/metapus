package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
)

func TestRequirePortalRoleUsesSelectedMerchantRole(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ownerMerchant := id.New()
	managerMerchant := id.New()
	user := &appctx.UserContext{
		UserID:      id.New().String(),
		MerchantIDs: []string{ownerMerchant.String(), managerMerchant.String()},
		MerchantRoles: map[string]int{
			ownerMerchant.String():   int(appctx.PortalRoleOwner),
			managerMerchant.String(): int(appctx.PortalRoleManager),
		},
	}
	router := portalRoleTestRouter(user, appctx.PortalRoleOwner)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/protected?merchant_id="+managerMerchant.String(), nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/protected?merchant_id="+ownerMerchant.String(), nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestRequirePortalRoleRequiresExplicitMerchantID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	merchantID := id.New()
	user := &appctx.UserContext{
		UserID:      id.New().String(),
		MerchantIDs: []string{merchantID.String()},
		MerchantRoles: map[string]int{
			merchantID.String(): int(appctx.PortalRoleOwner),
		},
	}
	router := portalRoleTestRouter(user, appctx.PortalRoleOwner)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/protected", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRequirePortalRoleRejectsMerchantOutsideScope(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ownerMerchant := id.New()
	otherMerchant := id.New()
	user := &appctx.UserContext{
		UserID:      id.New().String(),
		MerchantIDs: []string{ownerMerchant.String()},
		MerchantRoles: map[string]int{
			ownerMerchant.String(): int(appctx.PortalRoleOwner),
		},
	}
	router := portalRoleTestRouter(user, appctx.PortalRoleOwner)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/protected?merchant_id="+otherMerchant.String(), nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestMerchantPortalRejectsLegacyGlobalRoleWithoutPerMerchantRoles(t *testing.T) {
	gin.SetMode(gin.TestMode)

	merchantID := id.New()
	user := &appctx.UserContext{
		UserID:      id.New().String(),
		MerchantIDs: []string{merchantID.String()},
	}
	router := portalRoleTestRouter(user, appctx.PortalRoleOwner)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/protected?merchant_id="+merchantID.String(), nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func portalRoleTestRouter(user *appctx.UserContext, minRole appctx.MerchantPortalRole) *gin.Engine {
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Next()
		if len(c.Errors) == 0 || c.Writer.Written() {
			return
		}
		if appErr, ok := apperror.AsAppError(c.Errors.Last().Err); ok {
			c.Status(appErr.HTTPStatus)
			return
		}
		c.Status(http.StatusInternalServerError)
	})
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(appctx.WithUser(c.Request.Context(), user))
		c.Next()
	})
	router.Use(MerchantPortal())
	router.POST("/protected", RequirePortalRole(minRole), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	return router
}
