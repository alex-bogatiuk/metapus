package rpcconfig

import (
	"fmt"
	"net/url"
	"slices"
	"strings"

	"metapus/internal/core/apperror"
	"metapus/internal/domain/crypto/cryptointegrations"
	"metapus/internal/domain/integrations"
)

// rpcProfiles lists the networks, roles and secret-free bases each RPC
// operator supports. Storage and HTTP adapters enforce the same policy.
var rpcProfiles = map[integrations.Provider][]RPCProfile{
	cryptointegrations.DRPC: {{Family: "evm", NetworkIdentifier: "11155111", Roles: []string{"rpc"}, Host: "lb.drpc.live", BasePath: "/sepolia", TrustGroup: "drpc", KeyName: "sepolia"}},
	cryptointegrations.Alchemy: {
		{Family: "evm", Roles: []string{"rpc"}, HostSuffix: ".g.alchemy.com", BasePath: "/v2", TrustGroup: "alchemy"},
		{Family: "tron", NetworkIdentifier: "mainnet", Roles: []string{"fullnode", "solidity"}, Host: "tron-mainnet.g.alchemy.com", BasePath: "/v2", TrustGroup: "alchemy"},
		{Family: "tron", NetworkIdentifier: "nile", Roles: []string{"fullnode", "solidity"}, Host: "tron-testnet.g.alchemy.com", BasePath: "/v2", TrustGroup: "alchemy"},
	},
	cryptointegrations.Infura:          {{Family: "evm", NetworkIdentifier: "11155111", Roles: []string{"rpc"}, Host: "sepolia.infura.io", BasePath: "/v3", TrustGroup: "infura", KeyName: "sepolia"}},
	cryptointegrations.Ankr:            {{Family: "evm", NetworkIdentifier: "11155111", Roles: []string{"rpc"}, Host: "rpc.ankr.com", BasePath: "/eth_sepolia", TrustGroup: "ankr", KeyName: "sepolia"}},
	cryptointegrations.ValidationCloud: {{Family: "evm", NetworkIdentifier: "11155111", Roles: []string{"rpc"}, Host: "sepolia.ethereum.validationcloud.io", BasePath: "/v1", TrustGroup: "validation_cloud", KeyName: "sepolia"}},
	cryptointegrations.ZAN: {
		{Family: "evm", NetworkIdentifier: "11155111", Roles: []string{"rpc"}, Host: "api.zan.top", BasePath: "/node/v1/eth/sepolia", TrustGroup: "zan", KeyName: "default"},
		{Family: "tron", NetworkIdentifier: "nile", Roles: []string{"fullnode", "solidity"}, Host: "api.zan.top", BasePath: "/node/v1/tron/nile", TrustGroup: "zan", KeyName: "default"},
	},
	cryptointegrations.TronAPI: {
		{Family: "tron", NetworkIdentifier: "nile", Roles: []string{"fullnode", "solidity", "indexer"}, Host: "nile.trongrid.io", TrustGroup: "trongrid"},
		{Family: "tron", NetworkIdentifier: "mainnet", Roles: []string{"fullnode", "solidity", "indexer"}, Host: "api.trongrid.io", TrustGroup: "trongrid"},
	},
	cryptointegrations.Chainstack: {
		{Family: "tron", NetworkIdentifier: "nile", Roles: []string{"fullnode", "solidity"}, Host: "tron-nile.core.chainstack.com", TrustGroup: "chainstack", KeyName: "nile"},
		{Family: "evm", NetworkIdentifier: "11155111", Roles: []string{"rpc"}, Host: "ethereum-sepolia.core.chainstack.com", TrustGroup: "chainstack", KeyName: "sepolia"},
	},
	cryptointegrations.GetBlock: {{Family: "tron", NetworkIdentifier: "nile", Roles: []string{"fullnode", "solidity"}, Host: "shared.eu-central-1.getblock.io", TrustGroup: "getblock", SlotByRole: true}},
}

// rpcProviderOrder fixes host lookup order across operators.
var rpcProviderOrder = []integrations.Provider{cryptointegrations.DRPC, cryptointegrations.Alchemy, cryptointegrations.Ankr, cryptointegrations.ValidationCloud, cryptointegrations.ZAN, cryptointegrations.Infura, cryptointegrations.TronAPI, cryptointegrations.Chainstack, cryptointegrations.GetBlock}

// managedRPCOnly marks operators whose endpoints must read keys from an
// integration account pool.
var managedRPCOnly = map[integrations.Provider]bool{
	cryptointegrations.DRPC: true, cryptointegrations.Alchemy: true, cryptointegrations.Infura: true,
	cryptointegrations.Chainstack: true, cryptointegrations.GetBlock: true, cryptointegrations.Ankr: true,
	cryptointegrations.ValidationCloud: true, cryptointegrations.ZAN: true,
}

// RPCProfiles returns copies of the provider's RPC profiles.
func RPCProfiles(p integrations.Provider) []RPCProfile {
	profiles := slices.Clone(rpcProfiles[p])
	for i := range profiles {
		profiles[i].Roles = slices.Clone(profiles[i].Roles)
	}
	return profiles
}

// ManagedRPCOnly reports whether the provider's endpoints require a pool.
func ManagedRPCOnly(p integrations.Provider) bool { return managedRPCOnly[p] }

// RPCProfile declares the supported external contract. It contains no clients,
// secrets or I/O; storage and HTTP adapters enforce the same binding policy.
type RPCProfile struct {
	Family            string
	NetworkIdentifier string // Empty means any network of this family.
	Roles             []string
	Host              string
	HostSuffix        string
	BasePath          string
	TrustGroup        string
	// KeyName is the account key this contract always reads. Empty lets the
	// operator choose among names the pool accounts have (for example Alchemy).
	KeyName    string
	SlotByRole bool
}

type RPCBinding struct {
	Family, NetworkIdentifier, Role, URL, TrustGroup, Slot string
}

func (p RPCProfile) OwnsHost(host string) bool {
	host = strings.ToLower(host)
	return p.Host != "" && host == p.Host || p.HostSuffix != "" && strings.HasSuffix(host, p.HostSuffix)
}

func (p RPCProfile) ValidBase(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && !strings.ContainsAny(raw, "?# \r\n\t") && u.Scheme == "https" &&
		p.OwnsHost(u.Hostname()) && (u.Port() == "" || u.Port() == "443") &&
		(u.Path == p.BasePath || u.Path == p.BasePath+"/") && u.RawPath == "" && u.User == nil &&
		u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.RawFragment == ""
}

func (p RPCProfile) Supports(b RPCBinding) bool {
	if b.Family != p.Family || p.NetworkIdentifier != "" && b.NetworkIdentifier != p.NetworkIdentifier {
		return false
	}
	for _, role := range p.Roles {
		if b.Role == role {
			return true
		}
	}
	return false
}

// RPCProfileForURL returns the provider profile that owns a secret-free base.
func RPCProfileForURL(p integrations.Provider, raw string) (RPCProfile, bool) {
	profiles, ok := rpcProfiles[p]
	u, err := url.Parse(raw)
	if !ok || err != nil {
		return RPCProfile{}, false
	}
	// A specific host owns its contract even when a broader suffix also matches.
	// Do not fall back to another family when its base path is invalid.
	var best RPCProfile
	exactHost := false
	for _, profile := range profiles {
		if !profile.OwnsHost(u.Hostname()) {
			continue
		}
		if profile.Host != "" {
			exactHost = true
			if profile.ValidBase(raw) {
				return profile, true
			}
			continue
		}
		if len(profile.HostSuffix) > len(best.HostSuffix) {
			best = profile
		}
	}
	return best, !exactHost && best.HostSuffix != "" && best.ValidBase(raw)
}

// RPCKeyName returns the account key name that the provider profile requires
// for a binding. An empty result means the profile leaves the choice open.
func RPCKeyName(p integrations.Provider, b RPCBinding) string {
	definition, _ := cryptointegrations.Catalog().Definition(p)
	if definition.Credentials.RequireFields && definition.Credentials.SingleKey && len(definition.Credentials.Fields) == 1 {
		return definition.Credentials.Fields[0].Name
	}
	profile, ok := RPCProfileForURL(p, b.URL)
	if !ok || !profile.Supports(b) {
		return ""
	}
	if profile.SlotByRole {
		return b.Role
	}
	return profile.KeyName
}

func ValidateRPCBinding(p integrations.Provider, b RPCBinding) error {
	profile, ok := RPCProfileForURL(p, b.URL)
	if !ok || !profile.Supports(b) || b.TrustGroup != profile.TrustGroup {
		return apperror.NewValidation("Параметры RPC не соответствуют поддерживаемому профилю провайдера").WithDetail("reasonCode", "rpc_binding_invalid")
	}
	if name := RPCKeyName(p, b); name != "" && b.Slot != name {
		return apperror.NewValidation(fmt.Sprintf("Для этого подключения используется ключ «%s»", name)).WithDetail("reasonCode", "rpc_binding_invalid").WithDetail("field", "credentialSlot")
	}
	return nil
}

// RPCProviderForHost identifies the operator even for a credential URL. Callers
// still have to validate the complete secret-free base before using credentials.
func RPCProviderForHost(host string) (integrations.Provider, bool) {
	for _, provider := range rpcProviderOrder {
		for _, profile := range rpcProfiles[provider] {
			if profile.OwnsHost(host) {
				return provider, true
			}
		}
	}
	return "", false
}

func RPCTrustIdentity(raw, configured, endpointName string) string {
	u, _ := url.Parse(raw)
	if u != nil {
		if provider, ok := RPCProviderForHost(u.Hostname()); ok {
			for _, profile := range rpcProfiles[provider] {
				if profile.OwnsHost(u.Hostname()) {
					return profile.TrustGroup
				}
			}
		}
		// Public/file-only services have no managed account profile.
		host := strings.ToLower(u.Hostname())
		for _, operator := range []struct{ suffix, group string }{
			{".g.alchemy.com", "alchemy"}, {".infura.io", "infura"}, {".tenderly.co", "tenderly"}, {".publicnode.com", "publicnode"},
			{".uniblock.dev", "uniblock"}, {".onfinality.io", "onfinality"}, {".trongrid.io", "trongrid"}, {".nileex.io", "nileex"}, {".core.chainstack.com", "chainstack"},
		} {
			if host == strings.TrimPrefix(operator.suffix, ".") || strings.HasSuffix(host, operator.suffix) {
				return operator.group
			}
		}
	}
	if configured != "" {
		return configured
	}
	return endpointName
}

// RPCVotingGroup is empty for a gateway whose upstream may be another configured
// operator. Its endpoints can service ordinary calls but cannot attest to chain state.
func RPCVotingGroup(raw, configured, endpointName string) string {
	group := RPCTrustIdentity(raw, configured, endpointName)
	if group == "uniblock" {
		return ""
	}
	return group
}
