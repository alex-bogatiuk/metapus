package integrations

import (
	"fmt"
	"net/url"
	"strings"

	"metapus/internal/core/apperror"
)

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

func (p Provider) RPCProfileForURL(raw string) (RPCProfile, bool) {
	d, ok := p.Definition()
	u, err := url.Parse(raw)
	if !ok || err != nil {
		return RPCProfile{}, false
	}
	// A specific host owns its contract even when a broader suffix also matches.
	// Do not fall back to another family when its base path is invalid.
	var best RPCProfile
	for _, profile := range d.RPCProfiles {
		if !profile.OwnsHost(u.Hostname()) {
			continue
		}
		if profile.Host != "" {
			return profile, profile.ValidBase(raw)
		}
		if len(profile.HostSuffix) > len(best.HostSuffix) {
			best = profile
		}
	}
	return best, best.HostSuffix != "" && best.ValidBase(raw)
}

// RPCKeyName returns the account key name that the provider profile requires
// for a binding. An empty result means the profile leaves the choice open.
func (p Provider) RPCKeyName(b RPCBinding) string {
	definition, _ := p.Definition()
	if definition.Credentials.RequireFields && definition.Credentials.SingleKey && len(definition.Credentials.Fields) == 1 {
		return definition.Credentials.Fields[0].Name
	}
	profile, ok := p.RPCProfileForURL(b.URL)
	if !ok || !profile.Supports(b) {
		return ""
	}
	if profile.SlotByRole {
		return b.Role
	}
	return profile.KeyName
}

func (p Provider) ValidateRPCBinding(b RPCBinding) error {
	profile, ok := p.RPCProfileForURL(b.URL)
	if !ok || !profile.Supports(b) || b.TrustGroup != profile.TrustGroup {
		return apperror.NewValidation("Параметры RPC не соответствуют поддерживаемому профилю провайдера").WithDetail("reasonCode", "rpc_binding_invalid")
	}
	if name := p.RPCKeyName(b); name != "" && b.Slot != name {
		return apperror.NewValidation(fmt.Sprintf("Для этого подключения используется ключ «%s»", name)).WithDetail("reasonCode", "rpc_binding_invalid").WithDetail("field", "credentialSlot")
	}
	return nil
}

// RPCProviderForHost identifies the operator even for a credential URL. Callers
// still have to validate the complete secret-free base before using credentials.
func RPCProviderForHost(host string) (Provider, bool) {
	for _, d := range providerDefinitions {
		for _, profile := range d.RPCProfiles {
			if profile.OwnsHost(host) {
				return d.ID, true
			}
		}
	}
	return "", false
}

func RPCTrustIdentity(raw, configured, endpointName string) string {
	u, _ := url.Parse(raw)
	if u != nil {
		if provider, ok := RPCProviderForHost(u.Hostname()); ok {
			d, _ := provider.Definition()
			for _, profile := range d.RPCProfiles {
				if profile.OwnsHost(u.Hostname()) {
					return profile.TrustGroup
				}
			}
		}
		// Public/file-only services have no managed account profile.
		host := strings.ToLower(u.Hostname())
		for _, operator := range []struct{ suffix, group string }{
			{".g.alchemy.com", "alchemy"}, {".infura.io", "infura"}, {".tenderly.co", "tenderly"}, {".publicnode.com", "publicnode"},
			{".onfinality.io", "onfinality"}, {".trongrid.io", "trongrid"}, {".nileex.io", "nileex"}, {".core.chainstack.com", "chainstack"},
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
