package cryptoprofiles

import (
	"metapus/internal/core/json"
	model "metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/integrations/profiles"
)

type rpcErrorCode struct {
	Code int `json:"code"`
}

type rpcErrorDetails struct {
	rpcErrorCode
	Message string `json:"message"`
}

// readRPCError decodes only the fields declared by T. If decoding fails
// (for example, on a truncated response), it discards any partial result.
func readRPCError[T any](body []byte) T {
	var envelope struct {
		Error T `json:"error"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		var zero T
		return zero
	}
	return envelope.Error
}

// classify is the default classification plus TRON's HTTP API error
// envelope, {"Error": ...}, which blockchain operators return for TRON calls.
func classify(status int, body []byte) model.Failure {
	return profiles.ClassifyResponse(status, body, hasTRONError(body))
}

func hasTRONError(body []byte) bool {
	var envelope struct {
		Error json.RawMessage `json:"Error"`
	}
	return json.Unmarshal(body, &envelope) == nil && len(envelope.Error) > 0
}
