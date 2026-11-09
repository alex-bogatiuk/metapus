package merchant

import (
	"context"
	"net/netip"
	"time"

	"metapus/internal/core/id"
)

type NetworkSurface string
type NetworkPolicyMode string
type NetworkAccessDecision string

const (
	NetworkSurfacePortal NetworkSurface = "portal"
	NetworkSurfaceAPI    NetworkSurface = "api"

	NetworkPolicyOff     NetworkPolicyMode = "off"
	NetworkPolicyWarn    NetworkPolicyMode = "warn"
	NetworkPolicyEnforce NetworkPolicyMode = "enforce"

	NetworkAccessAllow NetworkAccessDecision = "allow"
	NetworkAccessWarn  NetworkAccessDecision = "warn"
	NetworkAccessDeny  NetworkAccessDecision = "deny"
)

func (s NetworkSurface) Valid() bool { return s == NetworkSurfacePortal || s == NetworkSurfaceAPI }
func (m NetworkPolicyMode) Valid() bool {
	return m == NetworkPolicyOff || m == NetworkPolicyWarn || m == NetworkPolicyEnforce
}

type AllowedNetwork struct {
	ID        id.ID     `json:"id"`
	Network   string    `json:"network"`
	Label     string    `json:"label"`
	IsActive  bool      `json:"isActive"`
	CreatedAt time.Time `json:"createdAt"`
}

type NetworkPolicy struct {
	ID         id.ID             `json:"id"`
	MerchantID id.ID             `json:"merchantId"`
	Surface    NetworkSurface    `json:"surface"`
	APIKeyID   *id.ID            `json:"apiKeyId,omitempty"`
	Mode       NetworkPolicyMode `json:"mode"`
	Version    int               `json:"version"`
	Networks   []AllowedNetwork  `json:"allowedNetworks"`
	UpdatedAt  time.Time         `json:"updatedAt"`
}

func EvaluateNetworkPolicies(policies []NetworkPolicy, clientIP netip.Addr) NetworkAccessDecision {
	clientIP = clientIP.Unmap()
	decision := NetworkAccessAllow
	for _, policy := range policies {
		if policy.Mode == NetworkPolicyOff {
			continue
		}
		allowed := false
		for _, item := range policy.Networks {
			prefix, err := netip.ParsePrefix(item.Network)
			if err == nil && item.IsActive && prefix.Contains(clientIP) {
				allowed = true
				break
			}
		}
		if allowed {
			continue
		}
		if policy.Mode == NetworkPolicyEnforce {
			return NetworkAccessDeny
		}
		decision = NetworkAccessWarn
	}
	return decision
}

type NetworkPolicyRepository interface {
	GetNetworkPolicy(context.Context, id.ID, NetworkSurface, *id.ID) (*NetworkPolicy, error)
	GetEffectiveNetworkPolicies(context.Context, id.ID, NetworkSurface, *id.ID) ([]NetworkPolicy, error)
	SetNetworkPolicyMode(context.Context, id.ID, NetworkSurface, *id.ID, NetworkPolicyMode, id.ID, string, bool) (*NetworkPolicy, error)
	AddAllowedNetwork(context.Context, id.ID, NetworkSurface, *id.ID, netip.Prefix, string, id.ID, string, bool) (*NetworkPolicy, error)
	RevokeAllowedNetwork(context.Context, id.ID, NetworkSurface, *id.ID, id.ID, id.ID, string, bool) (*NetworkPolicy, error)
	RecordNetworkDecision(context.Context, id.ID, *id.ID, string, string, NetworkAccessDecision, netip.Addr) error
}
