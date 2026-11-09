package merchant

import (
	"net/netip"
	"testing"
)

func TestEvaluateNetworkPoliciesUsesEnforcedIntersection(t *testing.T) {
	ip := netip.MustParseAddr("203.0.113.10")
	baseline := NetworkPolicy{Mode: NetworkPolicyEnforce, Networks: []AllowedNetwork{{Network: "203.0.113.0/24", IsActive: true}}}
	key := NetworkPolicy{Mode: NetworkPolicyEnforce, Networks: []AllowedNetwork{{Network: "198.51.100.0/24", IsActive: true}}}
	if got := EvaluateNetworkPolicies([]NetworkPolicy{baseline, key}, ip); got != NetworkAccessDeny {
		t.Fatalf("intersection decision = %q, want deny", got)
	}
	key.Networks = []AllowedNetwork{{Network: "203.0.113.10/32", IsActive: true}}
	if got := EvaluateNetworkPolicies([]NetworkPolicy{baseline, key}, ip); got != NetworkAccessAllow {
		t.Fatalf("intersection decision = %q, want allow", got)
	}
}

func TestEvaluateNetworkPoliciesWarnAndIPv4MappedIPv6(t *testing.T) {
	mapped := netip.MustParseAddr("::ffff:203.0.113.10")
	policy := NetworkPolicy{Mode: NetworkPolicyWarn, Networks: []AllowedNetwork{{Network: "198.51.100.0/24", IsActive: true}}}
	if got := EvaluateNetworkPolicies([]NetworkPolicy{policy}, mapped); got != NetworkAccessWarn {
		t.Fatalf("warn decision = %q", got)
	}
	policy.Mode = NetworkPolicyEnforce
	policy.Networks = []AllowedNetwork{{Network: "203.0.113.0/24", IsActive: true}}
	if got := EvaluateNetworkPolicies([]NetworkPolicy{policy}, mapped); got != NetworkAccessAllow {
		t.Fatalf("mapped IPv4 decision = %q", got)
	}
}
