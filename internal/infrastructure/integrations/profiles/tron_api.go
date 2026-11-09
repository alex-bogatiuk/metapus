package profiles

import model "metapus/internal/domain/integrations"

func tronAPI() Profile {
	return Profile{Provider: model.TronAPI, PinsPagination: true, Authorize: headerKey("TRON-PRO-API-KEY", func(host string) bool {
		provider, ok := model.RPCProviderForHost(host)
		return ok && provider == model.TronAPI
	})}
}
