// Package jsoncodec is the JSON codec of the HTTP API.
package jsoncodec

import (
	core "metapus/internal/infrastructure/http/jsoncodec"
)

// Marshal serializes an HTTP response, including domain values in generic
// projections. The core JSON facade keeps its independent storage codec.
func Marshal(v any) ([]byte, error) { return core.Marshal(v) }
