package profiles

import (
	"errors"
	"net/url"

	model "metapus/internal/domain/integrations"
)

// ImportRPC normalizes an imported chains-file endpoint without exposing a secret in the
// result URL or in an error. Unknown endpoints may only be imported anonymously.
func (r *Registry) ImportRPC(raw, key, role, slot string) (model.Provider, string, string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", "", errors.New("invalid RPC endpoint")
	}
	provider, known := model.RPCProviderForHost(u.Hostname())
	if !known {
		if key != "" {
			return "", "", "", errors.New("unsupported credential endpoint")
		}
		return "", raw, "", nil
	}
	definition, _ := provider.Definition()
	if key == "" && !definition.ManagedRPCOnly {
		return "", raw, "", nil
	}
	profile, supported := r.Lookup(provider)
	if !supported {
		return "", "", "", errors.New("unsupported RPC profile")
	}
	if profile.ImportCredentials != nil {
		raw, key, err = profile.ImportCredentials(raw, key)
		if err != nil {
			return "", "", "", err
		}
	}
	contract, ok := provider.RPCProfileForURL(raw)
	if !ok || key == "" || !definition.Credentials.KeyFormat.Valid(key) || contract.SlotByRole && slot != role {
		return "", "", "", errors.New("RPC import requires a supported base, key and role slot")
	}
	return provider, raw, key, nil
}
