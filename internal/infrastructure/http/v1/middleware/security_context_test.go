package middleware

import (
	"context"
	"errors"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/domain/security_profile"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

type profileFailureStub struct {
	security_profile.ProfileProvider
	err error
}

func (p profileFailureStub) GetUserProfile(context.Context, id.ID) (*security_profile.SecurityProfile, error) {
	return nil, p.err
}

type dimensionFailureStub struct {
	security_profile.DimensionResolver
}

func (dimensionFailureStub) DimensionName() string { return "merchant" }
func (dimensionFailureStub) Resolve(context.Context, id.ID) ([]string, error) {
	return nil, errors.New("resolver unavailable")
}

func TestSecurityContextDistinguishesUnassignedProfileFromLoadFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name     string
		err      error
		resolver bool
		allowed  bool
	}{
		{"unassigned", nil, false, true}, {"profile load failed", errors.New("database unavailable"), false, false}, {"dimension failed", nil, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var resolvers []security_profile.DimensionResolver
			if test.resolver {
				resolvers = append(resolvers, dimensionFailureStub{})
			}
			router := gin.New()
			var called bool
			router.Use(func(c *gin.Context) {
				c.Request = c.Request.WithContext(appctx.WithUser(c.Request.Context(), &appctx.UserContext{UserID: id.New().String()}))
				c.Next()
			})
			router.Use(SecurityContext(profileFailureStub{err: test.err}, resolvers...))
			router.GET("/entity-without-dimensions", func(c *gin.Context) { called = true; c.Status(204) })
			router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/entity-without-dimensions", nil))
			require.Equal(t, test.allowed, called)
		})
	}
}
