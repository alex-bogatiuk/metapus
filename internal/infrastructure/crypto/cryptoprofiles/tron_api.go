package cryptoprofiles

import (
	"metapus/internal/domain/crypto/cryptointegrations"
	"metapus/internal/domain/crypto/rpcconfig"
	"metapus/internal/infrastructure/integrations/profiles"
)

func tronAPI() profiles.Profile {
	return profiles.Profile{Provider: cryptointegrations.TronAPI, CursorPin: TronGridCursor(), Classify: classify, Authorize: profiles.HeaderKey("TRON-PRO-API-KEY", func(host string) bool {
		provider, ok := rpcconfig.RPCProviderForHost(host)
		return ok && provider == cryptointegrations.TronAPI
	}, tronHeader)}
}

// TronGridCursor is TronGrid's v1 pagination cursor. Anonymous TronGrid
// endpoints get it through the RPC transport.
func TronGridCursor() *profiles.CursorPin {
	return &profiles.CursorPin{PathContains: "/v1/", Param: "fingerprint"}
}
