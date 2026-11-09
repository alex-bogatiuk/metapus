package integrations

import (
	"crypto/sha256"
	"fmt"

	"metapus/internal/core/id"
	"metapus/internal/core/json"
)

// RPCTopology is the serializable configuration contract, independent of SQL,
// secret resolution and process-local HTTP clients. Field order and JSON tags
// retain the existing administrative/runtime revision format.
type RPCTopology struct {
	Roles map[string][]RPCConfiguredEndpoint `json:"roles"`
	TRON  *TRONSettings                      `json:"tron,omitempty"`
	EVM   *EVMSettings                       `json:"evm,omitempty"`
}

type RPCConfiguredEndpoint struct {
	EndpointID           id.ID  `json:"endpointId,omitempty"`
	ConfigVersion        int    `json:"configVersion,omitempty"`
	ProviderID           string `json:"providerId"`
	TrustGroup           string `json:"trustGroup,omitempty"`
	Priority             int    `json:"priority"`
	URL                  string `json:"url"`
	DisableKeepAlives    bool   `json:"disableKeepAlives,omitempty"`
	MinRequestIntervalMS int    `json:"minRequestIntervalMs,omitempty"`
	CredentialReference  string `json:"apiKeySecretFile,omitempty"`
}

func (n RPCTopology) Revision(family string, decoder int) string {
	data, err := json.MarshalDeterministic(n)
	if err != nil {
		panic("invalid internal RPC topology")
	}
	revision := fmt.Sprintf("%x", sha256.Sum256(data))
	if family == "tron" {
		revision = fmt.Sprintf("%s/decoder:%d", revision, decoder)
	}
	return revision
}

func RPCRequirements(family, environment string, endpoints []RPCConfigurationEndpoint) []RPCRoleRequirement {
	roles, required := []string{"rpc"}, 1
	if family == "evm" && environment == "production" {
		required = 2
	}
	if family == "tron" {
		roles, required = []string{"fullnode", "solidity"}, 2
	}
	result := make([]RPCRoleRequirement, 0, len(roles))
	for _, role := range roles {
		groups := make(map[string]bool)
		for _, e := range endpoints {
			if e.IsActive && e.Role == role {
				groups[e.TrustGroup] = true
			}
		}
		result = append(result, RPCRoleRequirement{Role: role, RequiredProviders: required, ConfiguredProviders: len(groups)})
	}
	return result
}
