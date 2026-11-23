// Package json is the platform's JSON codec: deterministic marshalling,
// strict decoding and the legacy wire forms.
package json

import (
	"io"

	core "github.com/alex-bogatiuk/metapus/internal/core/json"
)

type (
	// RawMessage is an alias of encoding/json.RawMessage so pgx keeps its
	// specialized JSON/JSONB encode plan (it type-switches on that named type).
	RawMessage = core.RawMessage
)

// CanonicalizeLegacyV1 reproduces encoding/json v1's historical canonical
// representation used by schema-version-1 unsigned transaction fingerprints.
// It intentionally accepts duplicate names and invalid UTF-8 exactly as v1
// did. Do not use it to define a new protocol.
func CanonicalizeLegacyV1(in []byte) ([]byte, error) { return core.CanonicalizeLegacyV1(in) }

// Marshal serializes v using project defaults.
func Marshal(v any) ([]byte, error) { return core.Marshal(v) }

// MarshalDeterministic serializes maps in a stable order. Use it for signed or
// content-addressed protocols whose exact bytes must survive retries.
func MarshalDeterministic(v any) ([]byte, error) { return core.MarshalDeterministic(v) }

// MarshalLegacyV1NoHTMLEscape reproduces the historical iTRX HMAC body:
// encoding/json v1 semantics, sorted object names, preserved number literals,
// nil maps/slices as null, and Encoder.SetEscapeHTML(false). U+2028 and U+2029
// remain escaped, as required by the v1 encoder contract.
func MarshalLegacyV1NoHTMLEscape(v any) ([]byte, error) { return core.MarshalLegacyV1NoHTMLEscape(v) }

// NewDecoder returns a Decoder that reads from r.
func NewDecoder(r io.Reader) *core.Decoder { return core.NewDecoder(r) }

// NewEncoder returns an Encoder that writes to w.
func NewEncoder(w io.Writer) *core.Encoder { return core.NewEncoder(w) }

// Unmarshal decodes a single JSON value into v using project defaults.
func Unmarshal(data []byte, v any) error { return core.Unmarshal(data, v) }

// UnmarshalStrict is for closed request contracts. It rejects object members
// that do not map to the destination in addition to the default duplicate-name
// and invalid-UTF-8 checks.
func UnmarshalStrict(data []byte, v any) error { return core.UnmarshalStrict(data, v) }

// Valid reports whether data is a single JSON value under v2 rules
// (unique object names, valid UTF-8).
func Valid(data []byte) bool { return core.Valid(data) }
