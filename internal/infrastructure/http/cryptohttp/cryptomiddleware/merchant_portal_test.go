package cryptomiddleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/merchant"
)

func TestRequirePortalRoleUsesSelectedMerchantRole(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ownerMerchant := id.New()
	managerMerchant := id.New()
	user := &appctx.UserContext{
		UserID: id.New().String(),
		AccessScopes: appctx.AccessScopes{merchant.AccessScopeKind: {
			ownerMerchant.String():   int(merchant.PortalRoleOwner),
			managerMerchant.String(): int(merchant.PortalRoleManager),
		}},
	}
	router := portalRoleTestRouter(user, merchant.PortalRoleOwner)

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
		UserID: id.New().String(),
		AccessScopes: appctx.AccessScopes{merchant.AccessScopeKind: {
			merchantID.String(): int(merchant.PortalRoleOwner),
		}},
	}
	router := portalRoleTestRouter(user, merchant.PortalRoleOwner)

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
		UserID: id.New().String(),
		AccessScopes: appctx.AccessScopes{merchant.AccessScopeKind: {
			ownerMerchant.String(): int(merchant.PortalRoleOwner),
		}},
	}
	router := portalRoleTestRouter(user, merchant.PortalRoleOwner)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/protected?merchant_id="+otherMerchant.String(), nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestMerchantPortalRejectsUserWithoutMerchantGrants(t *testing.T) {
	gin.SetMode(gin.TestMode)

	merchantID := id.New()
	user := &appctx.UserContext{
		UserID:       id.New().String(),
		AccessScopes: appctx.AccessScopes{"other_kind": {merchantID.String(): 1}},
	}
	router := portalRoleTestRouter(user, merchant.PortalRoleOwner)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/protected?merchant_id="+merchantID.String(), nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestMerchantPortalSkipsInvalidGrants(t *testing.T) {
	gin.SetMode(gin.TestMode)

	merchantID := id.New()
	user := &appctx.UserContext{
		UserID: id.New().String(),
		AccessScopes: appctx.AccessScopes{merchant.AccessScopeKind: {
			"not-a-uuid":        int(merchant.PortalRoleOwner),
			merchantID.String(): 0,
		}},
	}
	router := portalRoleTestRouter(user, merchant.PortalRoleViewer)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/protected?merchant_id="+merchantID.String(), nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func portalRoleTestRouter(user *appctx.UserContext, minRole merchant.PortalRole) *gin.Engine {
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
