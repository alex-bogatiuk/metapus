// Package cursor provides opaque cursor encoding/decoding for keyset pagination.
//
// Cursor format: base64url(JSON({ "v": ["field1","field2"], "d": [val1, val2] }))
// "v" = sort field names (for validation that sort hasn't changed between requests)
// "d" = sort field values at the boundary row
package cursor

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// Payload is the internal structure of a cursor token.
type Payload struct {
	Fields []string `json:"v"` // sort column names (e.g. ["-date","id"])
	Values []any    `json:"d"` // column values at the boundary row
}

// Encode creates an opaque cursor string from sort fields and their values.
func Encode(fields []string, values []any) (string, error) {
	if len(fields) != len(values) {
		return "", fmt.Errorf("cursor: fields/values length mismatch (%d vs %d)", len(fields), len(values))
	}
	p := Payload{Fields: fields, Values: values}
	data, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("cursor: marshal: %w", err)
	}
	return base64.URLEncoding.EncodeToString(data), nil
}

// Decode parses an opaque cursor string back into a Payload.
func Decode(token string) (*Payload, error) {
	data, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return nil, fmt.Errorf("cursor: invalid base64: %w", err)
	}
	var p Payload
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("cursor: invalid json: %w", err)
	}
	if len(p.Fields) == 0 || len(p.Fields) != len(p.Values) {
		return nil, fmt.Errorf("cursor: malformed payload (fields=%d, values=%d)", len(p.Fields), len(p.Values))
	}
	return &p, nil
}

// Direction of cursor-based pagination.
type Direction string

const (
	DirAfter  Direction = "after"  // forward: load items after cursor (scroll down)
	DirBefore Direction = "before" // backward: load items before cursor (scroll up)
	DirAround Direction = "around" // teleportation: load items around target ID
)

// Request contains cursor pagination parameters parsed from query string.
type Request struct {
	Direction Direction
	Token     string // opaque cursor token (for after/before)
	TargetID  string // UUID string (for around)
}
