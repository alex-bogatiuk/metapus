package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/merchant"
)

type merchantNetworkPolicyRepoStub struct {
	policies []merchant.NetworkPolicy
	loadErr  error
	auditErr error
	audited  netip.Addr
	decision merchant.NetworkAccessDecision
}

func (r *merchantNetworkPolicyRepoStub) GetNetworkPolicy(context.Context, id.ID, merchant.NetworkSurface, *id.ID) (*merchant.NetworkPolicy, error) {
	if len(r.policies) == 0 {
		return &merchant.NetworkPolicy{Mode: merchant.NetworkPolicyOff}, r.loadErr
	}
	copy := r.policies[0]
	return &copy, r.loadErr
}

func (r *merchantNetworkPolicyRepoStub) GetEffectiveNetworkPolicies(context.Context, id.ID, merchant.NetworkSurface, *id.ID) ([]merchant.NetworkPolicy, error) {
	return r.policies, r.loadErr
}

func (r *merchantNetworkPolicyRepoStub) SetNetworkPolicyMode(context.Context, id.ID, merchant.NetworkSurface, *id.ID, merchant.NetworkPolicyMode, id.ID, string, bool) (*merchant.NetworkPolicy, error) {
	panic("not used")
}

func (r *merchantNetworkPolicyRepoStub) AddAllowedNetwork(context.Context, id.ID, merchant.NetworkSurface, *id.ID, netip.Prefix, string, id.ID, string, bool) (*merchant.NetworkPolicy, error) {
	panic("not used")
}

func (r *merchantNetworkPolicyRepoStub) RevokeAllowedNetwork(context.Context, id.ID, merchant.NetworkSurface, *id.ID, id.ID, id.ID, string, bool) (*merchant.NetworkPolicy, error) {
	panic("not used")
}

func (r *merchantNetworkPolicyRepoStub) RecordNetworkDecision(_ context.Context, _ id.ID, _ *id.ID, _, _ string, decision merchant.NetworkAccessDecision, clientIP netip.Addr) error {
	r.audited = clientIP
	r.decision = decision
	return r.auditErr
}

func TestMerchantNetworkAccessIgnoresSpoofedForwardingHeaders(t *testing.T) {
	merchantID := id.New()
	repo := &merchantNetworkPolicyRepoStub{policies: []merchant.NetworkPolicy{{
		ID: id.New(), Mode: merchant.NetworkPolicyEnforce,
		Networks: []merchant.AllowedNetwork{{Network: "203.0.113.7/32", IsActive: true}},
	}}}
	router := merchantNetworkPolicyTestRouter(t, merchantID, repo, nil)

	request := httptest.NewRequest(http.MethodGet, "/protected?merchant_id="+merchantID.String(), nil)
	request.RemoteAddr = "198.51.100.9:4321"
	request.Header.Set("X-Forwarded-For", "203.0.113.7")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusForbidden, response.Code)
	require.Equal(t, netip.MustParseAddr("198.51.100.9"), repo.audited)
	require.Equal(t, merchant.NetworkAccessDeny, repo.decision)
}

func TestMerchantNetworkAccessUsesForwardedIPOnlyFromTrustedProxy(t *testing.T) {
	merchantID := id.New()
	repo := &merchantNetworkPolicyRepoStub{policies: []merchant.NetworkPolicy{{
		ID: id.New(), Mode: merchant.NetworkPolicyEnforce,
		Networks: []merchant.AllowedNetwork{{Network: "203.0.113.7/32", IsActive: true}},
	}}}
	router := merchantNetworkPolicyTestRouter(t, merchantID, repo, []string{"198.51.100.0/24"})

	request := httptest.NewRequest(http.MethodGet, "/protected?merchant_id="+merchantID.String(), nil)
	request.RemoteAddr = "198.51.100.9:4321"
	request.Header.Set("X-Forwarded-For", "203.0.113.7")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusNoContent, response.Code)
	require.Equal(t, netip.MustParseAddr("203.0.113.7"), repo.audited)
	require.Equal(t, merchant.NetworkAccessAllow, repo.decision)
}

func TestMerchantNetworkAccessFailsClosedOnPolicyOrAuditFailure(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		loadErr  error
		auditErr error
	}{
		{name: "policy read", loadErr: errors.New("policy database unavailable")},
		{name: "security audit", auditErr: errors.New("audit database unavailable")},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			merchantID := id.New()
			repo := &merchantNetworkPolicyRepoStub{
				policies: []merchant.NetworkPolicy{{Mode: merchant.NetworkPolicyOff}},
				loadErr:  testCase.loadErr, auditErr: testCase.auditErr,
			}
			router := merchantNetworkPolicyTestRouter(t, merchantID, repo, nil)
			request := httptest.NewRequest(http.MethodGet, "/protected?merchant_id="+merchantID.String(), nil)
			request.RemoteAddr = "203.0.113.7:4321"
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, http.StatusForbidden, response.Code)
		})
	}
}

func merchantNetworkPolicyTestRouter(t *testing.T, merchantID id.ID, repo merchant.NetworkPolicyRepository, trustedProxies []string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	InvalidateMerchantNetworkPolicyCache(merchantID)
	router := gin.New()
	require.NoError(t, router.SetTrustedProxies(trustedProxies))
	router.Use(ErrorHandler())
	router.Use(func(c *gin.Context) {
		scope := merchant.PortalScope{
			MerchantIDs: []id.ID{merchantID},
			Roles:       map[id.ID]merchant.PortalRole{merchantID: merchant.PortalRoleOwner},
		}
		ctx := merchant.WithPortalScope(c.Request.Context(), scope)
		ctx = appctx.WithUser(ctx, &appctx.UserContext{UserID: id.New().String()})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	router.Use(MerchantNetworkAccess(repo, merchant.NetworkSurfacePortal))
	router.GET("/protected", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	return router
}
