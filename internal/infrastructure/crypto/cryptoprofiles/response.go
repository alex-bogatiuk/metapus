package cryptoprofiles

import (
	"metapus/internal/core/json"
	model "metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/integrations/profiles"
)

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
