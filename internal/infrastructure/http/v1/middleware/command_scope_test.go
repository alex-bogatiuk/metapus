package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
)

func TestTrustedCommandScopePrefersInstalledResolver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/", nil)
	ctx := appctx.WithUser(c.Request.Context(), &appctx.UserContext{UserID: "user-1"})
	c.Request = c.Request.WithContext(ctx)

	kind, owner, err := trustedCommandScope(c)
	require.NoError(t, err)
	require.Equal(t, []string{"user", "user-1"}, []string{kind, owner})

	c.Request = c.Request.WithContext(WithCommandScope(ctx, func(*gin.Context) (string, string, error) {
		return "partner", "p-7", nil
	}))
	kind, owner, err = trustedCommandScope(c)
	require.NoError(t, err)
	require.Equal(t, []string{"partner", "p-7"}, []string{kind, owner})
}
