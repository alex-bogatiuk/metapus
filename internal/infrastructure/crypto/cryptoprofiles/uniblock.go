package cryptoprofiles

import (
	"errors"
	"net/http"

	"metapus/internal/core/json"
	"metapus/internal/domain/crypto/cryptointegrations"
	"metapus/internal/domain/crypto/rpcconfig"
	"metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/integrations/profiles"
)

func uniblock() profiles.Profile {
	return profiles.Profile{Provider: cryptointegrations.Uniblock, Authorize: authorizeUniblock, Classify: uniblockFailure}
}

func authorizeUniblock(r *http.Request, key string) error {
	profile, ok := rpcconfig.RPCProfileForURL(cryptointegrations.Uniblock, r.URL.String())
	if !ok || !integrations.CredentialURLSafe.Valid(key) {
		return errors.New("invalid Uniblock HTTP binding or key")
	}
	profiles.ClearAuthorization(r, tronHeader)
	r.Header.Set("X-Api-Key", key)
	r.URL.Path = profile.BasePath
	r.URL.RawQuery = "chainId=" + profile.NetworkIdentifier
	return nil
}

func uniblockFailure(status int, body []byte) integrations.Failure {
	if status == http.StatusOK || status == http.StatusTooManyRequests {
		err := readRPCError[struct {
			Code         json.RawMessage `json:"code"`
			UniblockCode string          `json:"uniblockCode"`
		}](body)
		// REST uses a string code, JSON-RPC sends a number and a separate uniblockCode.
		switch {
		case err.UniblockCode == "UR-429-03" || string(err.Code) == `"UR-429-03"`:
			return integrations.Exhausted
		case string(err.Code) == "-32429":
			if profiles.Classify(http.StatusTooManyRequests, body) == integrations.Exhausted {
				return integrations.Exhausted
			}
			return integrations.Throttled
		}
	}
	return gatewayHTTPFailure(status, body)
}
