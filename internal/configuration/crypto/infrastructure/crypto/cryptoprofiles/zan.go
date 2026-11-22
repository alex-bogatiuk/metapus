package cryptoprofiles

import (
	"net/http"

	"metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	"metapus/pkg/sdk/integrations"
	"metapus/pkg/sdk/integrations/profiles"
	"metapus/pkg/sdk/integrations/providerpolicy"
	"metapus/pkg/sdk/json"
)

func zan() profiles.Profile {
	return profiles.Profile{
		Provider: cryptointegrations.ZAN, Authorize: chainPathKey(cryptointegrations.ZAN, integrations.CredentialURLSafe),
		Classify: func(status int, body []byte) integrations.Failure {
			switch status {
			case http.StatusUnauthorized:
				return integrations.Unauthorized
			case http.StatusTooManyRequests:
				return integrations.Throttled
			case http.StatusForbidden:
				switch zanErrorCode(body) {
				case -32004:
					return integrations.Throttled
				case -32005:
					return integrations.Exhausted
				}
			}
			return ""
		},
		ResponseError: func(status int, body []byte) error {
			switch status {
			case http.StatusForbidden:
				if zanErrorCode(body) == -32007 {
					return providerpolicy.ErrUnsupported
				}
				return profiles.ErrAccessDenied
			case http.StatusRequestEntityTooLarge:
				return providerpolicy.ErrQueryLimit
			case http.StatusBadRequest:
				if zanErrorCode(body) == -32601 {
					return providerpolicy.ErrUnsupported
				}
			}
			return nil
		},
	}
}

// The gateway emits both JSON-RPC errors and a flat error_code for native HTTP requests.
func zanErrorCode(body []byte) int {
	var envelope struct {
		Error rpcErrorCode `json:"error"`
		Code  int          `json:"error_code"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return 0
	}
	if envelope.Code != 0 {
		return envelope.Code
	}
	return envelope.Error.Code
}
