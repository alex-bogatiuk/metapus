package cryptoprofiles

import (
	"metapus/internal/domain/crypto/cryptointegrations"
	"metapus/internal/domain/crypto/rpcconfig"
	"metapus/internal/infrastructure/integrations/profiles"
)

func tronAPI() profiles.Profile {
	return profiles.Profile{Provider: cryptointegrations.TronAPI, PinsPagination: true, Authorize: profiles.HeaderKey("TRON-PRO-API-KEY", func(host string) bool {
		provider, ok := rpcconfig.RPCProviderForHost(host)
		return ok && provider == cryptointegrations.TronAPI
	}, tronHeader)}
}
