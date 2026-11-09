// Package json is Metapus' encoding/json/v2 facade.
//
// All application JSON goes through this package so semantics, security
// defaults, and performance knobs live in one place.
//
// Defaults follow encoding/json/v2 (stricter and more interoperable than v1):
//   - case-sensitive struct field matching
//   - reject duplicate object names and invalid UTF-8
//   - nil slices/maps encode as [] / {}
//   - no HTML/JS escaping (this is a JSON API, not HTML)
//
// The only global compatibility exception is time.Duration, which remains a
// JSON number of nanoseconds for the existing API contract. Byte-level legacy
// protocols use the explicitly named helpers in compat_v1.go; new protocols
// must define and version their own canonical representation.
package json

import (
	"errors"
	"io"

	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
)

// RawMessage is an alias of encoding/json.RawMessage so pgx keeps its
// specialized JSON/JSONB encode plan (it type-switches on that named type).
type RawMessage = jsonv1.RawMessage

// Number is a JSON number literal. Use Decoder.UseNumber or
// UnmarshalPreserveNumbers to decode numbers into this type instead of float64.
type Number = jsonv1.Number

type (
	Marshaler       = jsonv2.Marshaler
	Unmarshaler     = jsonv2.Unmarshaler
	MarshalerTo     = jsonv2.MarshalerTo
	UnmarshalerFrom = jsonv2.UnmarshalerFrom
	Options         = jsonv2.Options
	SemanticError   = jsonv2.SemanticError
	SyntacticError  = jsontext.SyntacticError
)

var defaultOpts = jsonv2.JoinOptions(
	jsonv1.FormatDurationAsNano(true),
)

var deterministicOpts = jsonv2.JoinOptions(
	defaultOpts,
	jsonv2.Deterministic(true),
)

var numberUnmarshalers = jsonv2.UnmarshalFromFunc(func(dec *jsontext.Decoder, val *any) error {
	if dec.PeekKind() != '0' {
		return errors.ErrUnsupported
	}
	var n Number
	if err := jsonv2.UnmarshalDecode(dec, &n, defaultOpts); err != nil {
		return err
	}
	*val = n
	return nil
})

var preserveNumberOpts = jsonv2.JoinOptions(
	defaultOpts,
	jsonv2.WithUnmarshalers(numberUnmarshalers),
)

// Marshal serializes v using project defaults.
func Marshal(v any) ([]byte, error) {
	return jsonv2.Marshal(v, defaultOpts)
}

// MarshalDeterministic serializes maps in a stable order. Use it for signed or
// content-addressed protocols whose exact bytes must survive retries.
func MarshalDeterministic(v any) ([]byte, error) {
	return jsonv2.Marshal(v, deterministicOpts)
}

// Unmarshal decodes a single JSON value into v using project defaults.
func Unmarshal(data []byte, v any) error {
	return jsonv2.Unmarshal(data, v, defaultOpts)
}

// UnmarshalStrict is for closed request contracts. It rejects object members
// that do not map to the destination in addition to the default duplicate-name
// and invalid-UTF-8 checks.
func UnmarshalStrict(data []byte, v any) error {
	return jsonv2.Unmarshal(data, v, jsonv2.JoinOptions(
		defaultOpts,
		jsonv2.RejectUnknownMembers(true),
	))
}

// MarshalIndent is the v2 equivalent of encoding/json.MarshalIndent.
func MarshalIndent(v any, prefix, indent string) ([]byte, error) {
	return jsonv2.Marshal(v, jsonv2.JoinOptions(
		defaultOpts,
		jsontext.WithIndentPrefix(prefix),
		jsontext.WithIndent(indent),
	))
}

// MarshalWrite serializes v into w without a trailing newline.
func MarshalWrite(w io.Writer, v any) error {
	return jsonv2.MarshalWrite(w, v, defaultOpts)
}

// UnmarshalRead decodes one JSON value from r and consumes the rest of r
// (trailing non-whitespace is an error).
func UnmarshalRead(r io.Reader, v any) error {
	return jsonv2.UnmarshalRead(r, v, defaultOpts)
}

// UnmarshalPreserveNumbers is Unmarshal that decodes JSON numbers into Number
// when the destination is any. Use it for JSONB attributes, templates, and
// any other map[string]any that must not lose integer precision to float64.
func UnmarshalPreserveNumbers(data []byte, v any) error {
	return jsonv2.Unmarshal(data, v, preserveNumberOpts)
}

// Valid reports whether data is a single JSON value under v2 rules
// (unique object names, valid UTF-8).
func Valid(data []byte) bool {
	return jsontext.Value(data).IsValid()
}
