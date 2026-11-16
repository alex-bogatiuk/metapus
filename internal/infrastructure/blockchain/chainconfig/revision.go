package chainconfig

import (
	"metapus/internal/domain/crypto/rpcconfig"
)

// Configuration removes clients, resolved secrets and process-local admission
// scopes before sharing a configuration with policy or computing its revision.
func (n Network) Configuration() rpcconfig.RPCTopology {
	out := rpcconfig.RPCTopology{TRON: n.TRON, EVM: n.EVM}
	if n.Roles == nil {
		return out
	}
	out.Roles = make(map[string][]rpcconfig.RPCConfiguredEndpoint, len(n.Roles))
	for role, endpoints := range n.Roles {
		var configured []rpcconfig.RPCConfiguredEndpoint
		if endpoints != nil {
			configured = make([]rpcconfig.RPCConfiguredEndpoint, 0, len(endpoints))
		}
		for _, e := range endpoints {
			configured = append(configured, rpcconfig.RPCConfiguredEndpoint{
				EndpointID: e.EndpointID, ConfigVersion: e.ConfigVersion, ProviderID: e.ProviderID, TrustGroup: e.TrustGroup,
				Priority: e.Priority, URL: e.URL, DisableKeepAlives: e.DisableKeepAlives, MinRequestIntervalMS: e.MinRequestIntervalMS,
				CredentialReference: e.APIKeySecretFile,
			})
		}
		out.Roles[string(role)] = configured
	}
	return out
}

func (n Network) Revision(family string, decoder int) string {
	return n.Configuration().Revision(family, decoder)
}
