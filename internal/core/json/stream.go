package json

import (
	"io"

	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
)

// Encoder writes JSON values to an output stream.
// Encode appends a newline after each value, matching encoding/json.Encoder.
// HTML and JavaScript escaping are off by default, matching this package's
// JSON API semantics.
type Encoder struct {
	options    []Options
	w          io.Writer
	escapeHTML bool
	prefix     string
	indent     string
}

// NewEncoder returns an Encoder that writes to w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}

func NewEncoderWithOptions(w io.Writer, options ...Options) *Encoder {
	return &Encoder{w: w, options: options}
}

// SetEscapeHTML matches encoding/json.Encoder.SetEscapeHTML.
func (e *Encoder) SetEscapeHTML(on bool) {
	e.escapeHTML = on
}

// SetIndent matches encoding/json.Encoder.SetIndent.
func (e *Encoder) SetIndent(prefix, indent string) {
	e.prefix = prefix
	e.indent = indent
}

// Encode writes the JSON encoding of v followed by a newline.
func (e *Encoder) Encode(v any) error {
	opts := []jsonv2.Options{defaultOpts}
	opts = append(opts, e.options...)
	if e.escapeHTML {
		opts = append(opts, jsontext.EscapeForHTML(true), jsontext.EscapeForJS(true))
	}
	if e.prefix != "" || e.indent != "" {
		opts = append(opts, jsontext.WithIndentPrefix(e.prefix), jsontext.WithIndent(e.indent))
	}
	if err := jsonv2.MarshalWrite(e.w, v, jsonv2.JoinOptions(opts...)); err != nil {
		return err
	}
	_, err := e.w.Write([]byte{'\n'})
	return err
}

// Decoder reads and decodes JSON values from an input stream.
type Decoder struct {
	options       []Options
	dec           *jsontext.Decoder
	useNumber     bool
	rejectUnknown bool
}

// NewDecoder returns a Decoder that reads from r.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{dec: jsontext.NewDecoder(r)}
}

func NewDecoderWithOptions(r io.Reader, options ...Options) *Decoder {
	return &Decoder{dec: jsontext.NewDecoder(r), options: options}
}

// UseNumber causes Decode to unmarshal a JSON number into any as Number
// instead of float64.
func (d *Decoder) UseNumber() {
	d.useNumber = true
}

// DisallowUnknownFields causes Decode to reject unknown struct fields.
func (d *Decoder) DisallowUnknownFields() {
	d.rejectUnknown = true
}

// Decode reads the next JSON value from the stream into v.
func (d *Decoder) Decode(v any) error {
	opts := []jsonv2.Options{defaultOpts}
	opts = append(opts, d.options...)
	if d.useNumber {
		existing, _ := jsonv2.GetOption(jsonv2.JoinOptions(opts...), jsonv2.WithUnmarshalers)
		opts = append(opts, jsonv2.WithUnmarshalers(jsonv2.JoinUnmarshalers(existing, numberUnmarshalers)))
	}
	if d.rejectUnknown {
		opts = append(opts, jsonv2.RejectUnknownMembers(true))
	}
	return jsonv2.UnmarshalDecode(d.dec, v, jsonv2.JoinOptions(opts...))
}
