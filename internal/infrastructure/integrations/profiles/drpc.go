package profiles

import (
	"net/http"
	"strings"
	"time"

	"metapus/internal/core/json"
	model "metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/blockchain/rpcpolicy"
)

func drpcError(body []byte) (int, string) {
	var envelope struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return 0, ""
	}
	return envelope.Error.Code, strings.ToLower(envelope.Error.Message)
}

func drpcRangeLimit(status int, body []byte) bool {
	if status != http.StatusBadRequest && status != http.StatusOK {
		return false
	}
	code, text := drpcError(body)
	return code == 35 && strings.Contains(text, "range") && strings.Contains(text, "blocks") && strings.Contains(text, "not supported") && strings.Contains(text, "plan")
}

func drpc() Profile {
	return Profile{Provider: model.DRPC, Authorize: pathSuffix(model.DRPC, model.CredentialURLSafe),
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
			code, _ := drpcError(body)
			// dRPC documents separate billing, key authentication and feature errors.
			// An unknown 403 must not invalidate an otherwise usable account.
			switch code {
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
			code, _ := drpcError(body)
			if (status == http.StatusForbidden || status == http.StatusOK) && code == 24 {
				return now.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
			}
			return RetryAt(headers.Get("Retry-After"), now)
		},
		ResponseError: func(status int, body []byte) error {
			if drpcRangeLimit(status, body) {
				return rpcpolicy.ErrQueryLimit
			}
			if status == http.StatusForbidden || status == http.StatusBadRequest || status == http.StatusOK {
				code, message := drpcError(body)
				if code == 35 && strings.Contains(message, "chain is not available") && strings.Contains(message, "plan") {
					return ErrAccessDenied
				}
				switch code {
				case 29, 48, 51:
					return rpcpolicy.ErrUnsupported
				case 47:
					return rpcpolicy.ErrInvalidRequest
				case 50:
					return ErrAccessDenied
				}
			}
			if status == http.StatusForbidden {
				return ErrAccessDenied
			}
			return nil
		},
	}
}
