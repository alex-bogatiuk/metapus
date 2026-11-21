package cryptoapp

import (
	"context"
	"net/netip"
	"testing"

	"metapus/internal/configuration/crypto/domain/catalogs/merchant"
	"metapus/internal/core/id"
)

type networkPolicyTxManager struct{}

func (networkPolicyTxManager) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type networkPolicyRepoStub struct{ policy merchant.NetworkPolicy }

func (r *networkPolicyRepoStub) GetNetworkPolicy(context.Context, id.ID, merchant.NetworkSurface, *id.ID) (*merchant.NetworkPolicy, error) {
	copy := r.policy
	return &copy, nil
}
func (r *networkPolicyRepoStub) GetEffectiveNetworkPolicies(context.Context, id.ID, merchant.NetworkSurface, *id.ID) ([]merchant.NetworkPolicy, error) {
	return []merchant.NetworkPolicy{r.policy}, nil
}
func (r *networkPolicyRepoStub) SetNetworkPolicyMode(_ context.Context, _ id.ID, _ merchant.NetworkSurface, _ *id.ID, mode merchant.NetworkPolicyMode, _ id.ID, _ string, _ bool) (*merchant.NetworkPolicy, error) {
	r.policy.Mode = mode
	copy := r.policy
	return &copy, nil
}
func (r *networkPolicyRepoStub) AddAllowedNetwork(_ context.Context, _ id.ID, _ merchant.NetworkSurface, _ *id.ID, prefix netip.Prefix, label string, _ id.ID, _ string, _ bool) (*merchant.NetworkPolicy, error) {
	r.policy.Networks = append(r.policy.Networks, merchant.AllowedNetwork{ID: id.New(), Network: prefix.String(), Label: label, IsActive: true})
	copy := r.policy
	return &copy, nil
}
func (r *networkPolicyRepoStub) RevokeAllowedNetwork(context.Context, id.ID, merchant.NetworkSurface, *id.ID, id.ID, id.ID, string, bool) (*merchant.NetworkPolicy, error) {
	copy := r.policy
	return &copy, nil
}
func (*networkPolicyRepoStub) RecordNetworkDecision(context.Context, id.ID, *id.ID, string, string, merchant.NetworkAccessDecision, netip.Addr) error {
	return nil
}

func TestMerchantNetworkPolicyRequiresStagedEnforcement(t *testing.T) {
	repo := &networkPolicyRepoStub{policy: merchant.NetworkPolicy{Mode: merchant.NetworkPolicyOff}}
	svc := NewMerchantNetworkPolicyService(networkPolicyTxManager{}, repo)
	_, err := svc.SetMode(context.Background(), id.New(), merchant.NetworkSurfacePortal, nil, merchant.NetworkPolicyEnforce, id.New(), netip.MustParseAddr("203.0.113.1"), "lock down", false)
	if err == nil {
		t.Fatal("off -> enforce must be rejected")
	}
}

func TestMerchantNetworkPolicyNormalizesMappedCIDR(t *testing.T) {
	repo := &networkPolicyRepoStub{policy: merchant.NetworkPolicy{Mode: merchant.NetworkPolicyWarn}}
	svc := NewMerchantNetworkPolicyService(networkPolicyTxManager{}, repo)
	policy, err := svc.AddNetwork(context.Background(), id.New(), merchant.NetworkSurfaceAPI, nil, "::ffff:203.0.113.10/128", "office", id.New(), "allow office", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := policy.Networks[0].Network; got != "203.0.113.10/32" {
		t.Fatalf("normalized CIDR = %q", got)
	}
}

func TestMerchantNetworkPolicyPreventsCurrentIPLockout(t *testing.T) {
	repo := &networkPolicyRepoStub{policy: merchant.NetworkPolicy{Mode: merchant.NetworkPolicyWarn, Networks: []merchant.AllowedNetwork{{Network: "198.51.100.0/24", IsActive: true}}}}
	svc := NewMerchantNetworkPolicyService(networkPolicyTxManager{}, repo)
	_, err := svc.SetMode(context.Background(), id.New(), merchant.NetworkSurfacePortal, nil, merchant.NetworkPolicyEnforce, id.New(), netip.MustParseAddr("203.0.113.1"), "lock down", false)
	if err == nil {
		t.Fatal("enforce must reject current-IP lockout without recovery CIDR")
	}
}
