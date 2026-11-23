package cryptoprofiles

import (
	"net/http"
	"strings"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/integrations"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/integrations/profiles"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/integrations/providerpolicy"
)

func ankr() profiles.Profile {
	return profiles.Profile{
		Provider:      cryptointegrations.Ankr,
		Authorize:     pathSuffix(cryptointegrations.Ankr, integrations.CredentialURLSafe),
		Classify:      classifyAnkr,
		ResponseError: ankrResponseError,
	}
}

func classifyAnkr(status int, body []byte) integrations.Failure {
	switch status {
	case http.StatusTooManyRequests:
		return integrations.Throttled
	case http.StatusUnauthorized:
		return integrations.Unauthorized
	case http.StatusOK, http.StatusForbidden:
		code := readRPCError[rpcErrorCode](body).Code
		switch code {
		case -32049, -32050, -32051, -32060: // Missing, unknown, disabled or expired key.
			return integrations.Unauthorized
		case -32090, -32067, -32068, -32069, -32070, -32077: // Rate or connection limit.
			return integrations.Throttled
		}
		if status == http.StatusForbidden && code == 0 {
			// The gateway can reject credentials before JSON-RPC handling.
			message, _, _ := strings.Cut(string(body), ", trace_id:")
			switch strings.TrimSpace(message) {
			case "message: API key disabled", "message: API key expired":
				return integrations.Unauthorized
			}
		}
	}
	// A gateway's explicit monthly exhaustion can suspend the account.
	// A node error (including a contract revert) in a 200 response cannot.
	if (status == http.StatusPaymentRequired || status == http.StatusForbidden) && profiles.Classify(status, body) == integrations.Exhausted {
		return integrations.Exhausted
	}
	return ""
}

func ankrResponseError(status int, body []byte) error {
	switch status {
	case http.StatusRequestEntityTooLarge:
		return providerpolicy.ErrQueryLimit
	case http.StatusOK, http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound:
		switch readRPCError[rpcErrorCode](body).Code {
		case -32062:
			return providerpolicy.ErrQueryLimit
		case -32601, -32053, -32075: // Unavailable method, including Freemium restrictions.
			return providerpolicy.ErrUnsupported
		case -32700, -32600, -32602:
			if status == http.StatusBadRequest {
				return providerpolicy.ErrInvalidRequest
			}
		case -32052, -32054, -32079, -32080:
			return profiles.ErrAccessDenied
		}
		if status == http.StatusForbidden {
			return profiles.ErrAccessDenied
		}
	}
	return nil
}
