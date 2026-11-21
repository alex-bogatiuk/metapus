package cryptomiddleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/catalogs/merchant"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
)

func TestPortalCommandScopeSelectsMerchantInScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	first, second := id.New(), id.New()
	resolve := func(scope merchant.PortalScope, target string) (string, string, error) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, target, nil)
		return portalCommandScope(scope)(c)
	}
	single := merchant.PortalScope{MerchantIDs: []id.ID{first}}
	both := merchant.PortalScope{MerchantIDs: []id.ID{first, second}}

	kind, owner, err := resolve(single, "/")
	require.NoError(t, err)
	require.Equal(t, []string{"merchant", first.String()}, []string{kind, owner})

	_, owner, err = resolve(both, "/?merchant_id="+second.String())
	require.NoError(t, err)
	require.Equal(t, second.String(), owner)

	_, _, err = resolve(both, "/")
	appErr, ok := apperror.AsAppError(err)
	require.True(t, ok)
	require.Equal(t, http.StatusForbidden, appErr.HTTPStatus)

	_, _, err = resolve(single, "/?merchant_id="+id.New().String())
	require.Error(t, err)
}
