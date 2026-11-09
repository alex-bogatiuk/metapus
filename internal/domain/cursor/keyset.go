package cursor

import (
	"encoding/base64"
	"fmt"
	"metapus/internal/core/json"
)

// KeysetPayload binds a boundary to its complete ordering contract. Values are
// strings so int64/numeric boundaries never round through JSON float64.
type KeysetPayload struct {
	Version   int     `json:"version"`
	Field     string  `json:"field"`
	Direction string  `json:"direction"`
	FieldType string  `json:"type"`
	Nullable  bool    `json:"nullable"`
	Nulls     string  `json:"nulls"`
	NullRank  int     `json:"nullRank"`
	Value     *string `json:"value"`
	ID        string  `json:"id"`
}

func EncodeKeyset(payload KeysetPayload) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func DecodeKeyset(token string) (KeysetPayload, error) {
	var payload KeysetPayload
	if len(token) > 16384 {
		return payload, fmt.Errorf("cursor is too long")
	}
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return payload, err
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return payload, err
	}
	if payload.Version != 2 {
		return payload, fmt.Errorf("unsupported cursor version")
	}
	return payload, nil
}
