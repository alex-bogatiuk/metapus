package cryptoapp

import (
	"context"
	"net/netip"
	"strings"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/tx"
	"metapus/internal/domain/catalogs/merchant"
)

type MerchantNetworkPolicyService struct {
	txm  tx.Manager
	repo merchant.NetworkPolicyRepository
}

func NewMerchantNetworkPolicyService(txm tx.Manager, repo merchant.NetworkPolicyRepository) *MerchantNetworkPolicyService {
	if txm == nil || repo == nil {
		panic("merchant network policy service dependencies are required")
	}
	return &MerchantNetworkPolicyService{txm: txm, repo: repo}
}

func (s *MerchantNetworkPolicyService) Get(ctx context.Context, merchantID id.ID, surface merchant.NetworkSurface, apiKeyID *id.ID) (*merchant.NetworkPolicy, error) {
	if id.IsNil(merchantID) || !surface.Valid() || (surface == merchant.NetworkSurfacePortal && apiKeyID != nil) {
		return nil, apperror.NewValidation("invalid merchant network policy scope")
	}
	return s.repo.GetNetworkPolicy(ctx, merchantID, surface, apiKeyID)
}

func (s *MerchantNetworkPolicyService) SetMode(ctx context.Context, merchantID id.ID, surface merchant.NetworkSurface, apiKeyID *id.ID, mode merchant.NetworkPolicyMode, actorID id.ID, clientIP netip.Addr, reason string, recovery bool) (*merchant.NetworkPolicy, error) {
	if id.IsNil(merchantID) || id.IsNil(actorID) || !surface.Valid() ||
		(surface == merchant.NetworkSurfacePortal && apiKeyID != nil) || !clientIP.IsValid() ||
		!mode.Valid() || strings.TrimSpace(reason) == "" {
		return nil, apperror.NewValidation("valid policy scope, mode, actor, client IP and reason are required")
	}
	clientIP = clientIP.Unmap()
	var result *merchant.NetworkPolicy
	err := s.txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		current, err := s.repo.GetNetworkPolicy(txCtx, merchantID, surface, apiKeyID)
		if err != nil {
			return err
		}
		if !recovery && current.Mode == merchant.NetworkPolicyOff && mode == merchant.NetworkPolicyEnforce {
			return apperror.NewValidation("network policy must pass through warn before enforce")
		}
		if !recovery && mode == merchant.NetworkPolicyEnforce {
			enforced := *current
			enforced.Mode = merchant.NetworkPolicyEnforce
			if merchant.EvaluateNetworkPolicies([]merchant.NetworkPolicy{enforced}, clientIP) == merchant.NetworkAccessDeny && !hasRecoveryNetwork(current) {
				return apperror.NewValidation("enforce would lock out the current IP and no recovery CIDR is active")
			}
		}
		result, err = s.repo.SetNetworkPolicyMode(txCtx, merchantID, surface, apiKeyID, mode, actorID, reason, recovery)
		return err
	})
	return result, err
}

func (s *MerchantNetworkPolicyService) AddNetwork(ctx context.Context, merchantID id.ID, surface merchant.NetworkSurface, apiKeyID *id.ID, rawCIDR, label string, actorID id.ID, reason string, recovery bool) (*merchant.NetworkPolicy, error) {
	if id.IsNil(merchantID) || id.IsNil(actorID) || !surface.Valid() ||
		(surface == merchant.NetworkSurfacePortal && apiKeyID != nil) || strings.TrimSpace(reason) == "" {
		return nil, apperror.NewValidation("valid policy scope, actor and reason are required")
	}
	prefix, err := netip.ParsePrefix(strings.TrimSpace(rawCIDR))
	if err != nil {
		return nil, apperror.NewValidation("invalid CIDR network")
	}
	bits := prefix.Bits()
	if prefix.Addr().Is4In6() {
		bits -= 96
	}
	prefix = netip.PrefixFrom(prefix.Addr().Unmap(), bits)
	var result *merchant.NetworkPolicy
	err = s.txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		var err error
		result, err = s.repo.AddAllowedNetwork(txCtx, merchantID, surface, apiKeyID, prefix.Masked(), label, actorID, reason, recovery)
		return err
	})
	return result, err
}

func (s *MerchantNetworkPolicyService) RevokeNetwork(ctx context.Context, merchantID id.ID, surface merchant.NetworkSurface, apiKeyID *id.ID, networkID, actorID id.ID, reason string, recovery bool) (*merchant.NetworkPolicy, error) {
	if id.IsNil(merchantID) || id.IsNil(networkID) || id.IsNil(actorID) || !surface.Valid() ||
		(surface == merchant.NetworkSurfacePortal && apiKeyID != nil) || strings.TrimSpace(reason) == "" {
		return nil, apperror.NewValidation("valid policy scope, network, actor and reason are required")
	}
	var result *merchant.NetworkPolicy
	err := s.txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		var err error
		result, err = s.repo.RevokeAllowedNetwork(txCtx, merchantID, surface, apiKeyID, networkID, actorID, reason, recovery)
		return err
	})
	return result, err
}

func hasRecoveryNetwork(policy *merchant.NetworkPolicy) bool {
	for _, network := range policy.Networks {
		if network.IsActive && strings.EqualFold(strings.TrimSpace(network.Label), "recovery") {
			return true
		}
	}
	return false
}
