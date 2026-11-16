package json

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
)

// Type-specific options keep boundary codecs local instead of changing the
// default representation used by storage or signed protocols.
func MarshalToOption[T any](fn func(*jsontext.Encoder, T) error) Options {
	return jsonv2.WithMarshalers(jsonv2.MarshalToFunc(fn))
}
func UnmarshalFromOption[T any](fn func(*jsontext.Decoder, T) error) Options {
	return jsonv2.WithUnmarshalers(jsonv2.UnmarshalFromFunc(fn))
}
func MarshalWithOptions(v any, options ...Options) ([]byte, error) {
	return jsonv2.Marshal(v, jsonv2.JoinOptions(append([]Options{defaultOpts}, options...)...))
}
func MarshalIndentWithOptions(v any, prefix, indent string, options ...Options) ([]byte, error) {
	return MarshalWithOptions(v, append(options, jsontext.WithIndentPrefix(prefix), jsontext.WithIndent(indent))...)
}
func UnmarshalStrictWithOptions(data []byte, v any, options ...Options) error {
	opts := append([]Options{defaultOpts, jsonv2.RejectUnknownMembers(true)}, options...)
	return jsonv2.Unmarshal(data, v, jsonv2.JoinOptions(opts...))
}

// UnmarshalDecode reads the next value from dec into v with the options of
// the enclosing call, e.g. inside an UnmarshalerFrom that decodes into a
// type chosen at run time.
func UnmarshalDecode(dec *jsontext.Decoder, v any) error {
	return jsonv2.UnmarshalDecode(dec, v)
}
