// Package json is encoding/json/v2 with historical v1 semantics.
// Vault signing fingerprints and JWS payloads must stay byte-stable.
package json

import (
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"io"
)

type RawMessage = jsonv1.RawMessage

var opts = jsonv1.DefaultOptionsV1()

func Marshal(v any) ([]byte, error) {
	return jsonv2.Marshal(v, opts)
}

func Unmarshal(data []byte, v any) error {
	return jsonv2.Unmarshal(data, v, opts)
}

func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w, escapeHTML: true}
}

type Encoder struct {
	w          io.Writer
	escapeHTML bool
}

func (e *Encoder) SetEscapeHTML(on bool) { e.escapeHTML = on }

func (e *Encoder) Encode(v any) error {
	callOpts := opts
	if !e.escapeHTML {
		callOpts = jsonv2.JoinOptions(opts, jsontext.EscapeForHTML(false))
	}
	if err := jsonv2.MarshalWrite(e.w, v, callOpts); err != nil {
		return err
	}
	_, err := e.w.Write([]byte{'\n'})
	return err
}

func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{dec: jsontext.NewDecoder(r)}
}

type Decoder struct {
	dec       *jsontext.Decoder
	useNumber bool
}

func (d *Decoder) UseNumber() { d.useNumber = true }

func (d *Decoder) Decode(v any) error {
	callOpts := opts
	if d.useNumber {
		callOpts = jsonv2.JoinOptions(opts, jsonv2.WithUnmarshalers(numberUnmarshalers))
	}
	return jsonv2.UnmarshalDecode(d.dec, v, callOpts)
}

var numberUnmarshalers = jsonv2.UnmarshalFromFunc(func(dec *jsontext.Decoder, val *any) error {
	if dec.PeekKind() != '0' {
		return errors.ErrUnsupported
	}
	var n jsonv1.Number
	if err := jsonv2.UnmarshalDecode(dec, &n, opts); err != nil {
		return err
	}
	*val = n
	return nil
})
