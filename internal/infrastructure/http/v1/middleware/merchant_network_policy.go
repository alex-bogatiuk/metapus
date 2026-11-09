package middleware

import (
	"net/netip"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/catalogs/merchant"
)

const merchantNetworkPolicyMaxStale = 5 * time.Second

type cachedMerchantPolicies struct {
	policies  []merchant.NetworkPolicy
	expiresAt time.Time
}

var merchantNetworkPolicyCache sync.Map

// InvalidateMerchantNetworkPolicyCache invalidates every surface/key variant
// for a merchant after a committed policy mutation.
func InvalidateMerchantNetworkPolicyCache(merchantID id.ID) {
	prefix := merchantID.String() + "|"
	merchantNetworkPolicyCache.Range(func(key, _ any) bool {
		if value, ok := key.(string); ok && len(value) >= len(prefix) && value[:len(prefix)] == prefix {
			merchantNetworkPolicyCache.Delete(key)
		}
		return true
	})
}

func MerchantNetworkAccess(repo merchant.NetworkPolicyRepository, surface merchant.NetworkSurface) gin.HandlerFunc {
	if repo == nil {
		panic("merchant network access repository is required")
	}
	return func(c *gin.Context) {
		merchantID, apiKeyID, actorType, actorID, applies, err := merchantNetworkIdentity(c, surface)
		if err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}
		if !applies {
			c.Next()
			return
		}
		clientIP, err := netip.ParseAddr(c.ClientIP())
		if err != nil {
			_ = c.Error(apperror.NewMerchantNetworkDenied("invalid client IP"))
			c.Abort()
			return
		}
		clientIP = clientIP.Unmap()
		policies, err := loadMerchantPolicies(c, repo, merchantID, surface, apiKeyID)
		if err != nil {
			_ = c.Error(apperror.NewMerchantNetworkDenied("network policy is unavailable").WithCause(err))
			c.Abort()
			return
		}
		decision := merchant.EvaluateNetworkPolicies(policies, clientIP)
		var policyID *id.ID
		for index := range policies {
			if !id.IsNil(policies[index].ID) {
				policyID = &policies[index].ID
			}
		}
		if err := repo.RecordNetworkDecision(c.Request.Context(), merchantID, policyID, actorType, actorID, decision, clientIP); err != nil {
			_ = c.Error(apperror.NewMerchantNetworkDenied("network policy audit is unavailable").WithCause(err))
			c.Abort()
			return
		}
		switch decision {
		case merchant.NetworkAccessDeny:
			_ = c.Error(apperror.NewMerchantNetworkDenied("client IP is outside the merchant allowlist"))
			c.Abort()
			return
		case merchant.NetworkAccessWarn:
			c.Header("X-Network-Policy-Warn", "true")
		}
		c.Set("merchant_network_access", string(decision))
		c.Next()
	}
}

func merchantNetworkIdentity(c *gin.Context, surface merchant.NetworkSurface) (id.ID, *id.ID, string, string, bool, error) {
	if surface == merchant.NetworkSurfaceAPI {
		mc := GetMerchant(c.Request.Context())
		if mc == nil {
			return id.Nil(), nil, "", "", false, apperror.NewUnauthorized("merchant context missing")
		}
		keyID := mc.KeyID
		return mc.MerchantID, &keyID, "api_key", keyID.String(), true, nil
	}
	scope, ok := appctx.GetMerchantScope(c.Request.Context())
	if !ok {
		return id.Nil(), nil, "", "", false, apperror.NewUnauthorized("merchant scope missing")
	}
	raw := c.Query("merchant_id")
	if raw == "" && len(scope.MerchantIDs) == 1 {
		raw = scope.MerchantIDs[0].String()
	}
	if raw == "" && c.FullPath() == "/portal/v1/merchants" {
		return id.Nil(), nil, "", "", false, nil
	}
	merchantID, err := id.Parse(raw)
	if err != nil || !scope.AllowsFor(merchantID, appctx.PortalRoleViewer) {
		return id.Nil(), nil, "", "", false, apperror.NewForbidden("merchant network scope is not allowed")
	}
	user := appctx.GetUser(c.Request.Context())
	actorID := ""
	if user != nil {
		actorID = user.UserID
	}
	return merchantID, nil, "user", actorID, true, nil
}

func loadMerchantPolicies(c *gin.Context, repo merchant.NetworkPolicyRepository, merchantID id.ID, surface merchant.NetworkSurface, apiKeyID *id.ID) ([]merchant.NetworkPolicy, error) {
	keyID := "baseline"
	if apiKeyID != nil {
		keyID = apiKeyID.String()
	}
	key := merchantID.String() + "|" + tenant.GetTenantID(c.Request.Context()) + "|" + string(surface) + "|" + keyID
	if cached, ok := merchantNetworkPolicyCache.Load(key); ok {
		entry := cached.(cachedMerchantPolicies)
		if time.Now().Before(entry.expiresAt) {
			return entry.policies, nil
		}
		merchantNetworkPolicyCache.Delete(key)
	}
	policies, err := repo.GetEffectiveNetworkPolicies(c.Request.Context(), merchantID, surface, apiKeyID)
	if err != nil {
		return nil, err
	}
	merchantNetworkPolicyCache.Store(key, cachedMerchantPolicies{policies: policies, expiresAt: time.Now().Add(merchantNetworkPolicyMaxStale)})
	return policies, nil
}
