package cryptoprofiles

import (
	"metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	"metapus/internal/infrastructure/integrations/profiles"
	"net/http"
	"strings"
	"time"

	model "metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/providerpolicy"
)

func drpc() profiles.Profile {
	return profiles.Profile{Provider: cryptointegrations.DRPC, Authorize: pathSuffix(cryptointegrations.DRPC, model.CredentialURLSafe),
		Classify: func(status int, body []byte) model.Failure {
			if status == http.StatusUnauthorized {
				return model.Unauthorized
			}
			if status == http.StatusTooManyRequests {
				return model.Throttled
			}
			if status != http.StatusForbidden && status != http.StatusOK {
				return ""
			}
			// dRPC documents separate billing, key authentication and feature errors.
			// An unknown 403 must not invalidate an otherwise usable account.
			switch readRPCError[rpcErrorDetails](body).Code {
			case 10, 52, 53:
				return model.Exhausted
			case 4, 9, 32:
				return model.Unauthorized
			case 24, 31:
				return model.Throttled
			}
			return ""
		},
		RetryAt: func(status int, headers http.Header, body []byte, now time.Time) time.Time {
			if (status == http.StatusForbidden || status == http.StatusOK) && readRPCError[rpcErrorDetails](body).Code == 24 {
				return now.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
			}
			return profiles.RetryAt(headers.Get("Retry-After"), now)
		},
		ResponseError: func(status int, body []byte) error {
			if status != http.StatusForbidden && status != http.StatusBadRequest && status != http.StatusOK {
				return nil
			}
			detail := readRPCError[rpcErrorDetails](body)
			switch detail.Code {
			case 35:
				message := strings.ToLower(detail.Message)
				if status != http.StatusForbidden && strings.Contains(message, "range") && strings.Contains(message, "blocks") && strings.Contains(message, "not supported") && strings.Contains(message, "plan") {
					return providerpolicy.ErrQueryLimit
				}
				if strings.Contains(message, "chain is not available") && strings.Contains(message, "plan") {
					return profiles.ErrAccessDenied
				}
			case 29, 48, 51:
				return providerpolicy.ErrUnsupported
			case 47:
				return providerpolicy.ErrInvalidRequest
			case 50:
				return profiles.ErrAccessDenied
			}
			if status == http.StatusForbidden {
				return profiles.ErrAccessDenied
			}
			return nil
		},
	}
}
