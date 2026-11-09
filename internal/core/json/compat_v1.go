package json

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	jsonv1 "encoding/json"
)

// CanonicalizeLegacyV1 reproduces encoding/json v1's historical canonical
// representation used by schema-version-1 unsigned transaction fingerprints.
// It intentionally accepts duplicate names and invalid UTF-8 exactly as v1
// did. Do not use it to define a new protocol.
func CanonicalizeLegacyV1(in []byte) ([]byte, error) {
	dec := jsonv1.NewDecoder(bytes.NewReader(in))
	dec.UseNumber()

	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode legacy JSON: %w", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("legacy JSON must contain exactly one value")
	}

	out, err := jsonv1.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal legacy JSON: %w", err)
	}
	return out, nil
}

// MarshalLegacyV1NoHTMLEscape reproduces the historical iTRX HMAC body:
// encoding/json v1 semantics, sorted object names, preserved number literals,
// nil maps/slices as null, and Encoder.SetEscapeHTML(false). U+2028 and U+2029
// remain escaped, as required by the v1 encoder contract.
func MarshalLegacyV1NoHTMLEscape(v any) ([]byte, error) {
	raw, err := jsonv1.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal legacy JSON input: %w", err)
	}

	dec := jsonv1.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var normalized any
	if err := dec.Decode(&normalized); err != nil {
		return nil, fmt.Errorf("normalize legacy JSON: %w", err)
	}

	var buf bytes.Buffer
	enc := jsonv1.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(normalized); err != nil {
		return nil, fmt.Errorf("encode legacy JSON: %w", err)
	}
	return bytes.TrimSpace(buf.Bytes()), nil
}
