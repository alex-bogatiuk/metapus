package command

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"metapus/internal/core/json"
)

// Fingerprint returns a deterministic SHA-256 over a validated business DTO
// envelope. encoding/json sorts map keys, while structs make the intended
// business fields explicit at each use-case boundary.
func Fingerprint(value any) (string, error) {
	canonical, err := json.MarshalDeterministic(value)
	if err != nil {
		return "", fmt.Errorf("canonicalize command request: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// MustPayload serializes a result DTO. It is intended for application
// services immediately after they have constructed a known-valid response.
func MustPayload(value any) json.RawMessage {
	payload, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("serialize command result: %v", err))
	}
	return payload
}
