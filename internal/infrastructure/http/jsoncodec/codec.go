// Package jsoncodec installs Metapus' encoding/json/v2 facade as Gin's JSON codec.
// Blank-import this package from HTTP entry points and tests that call gin.JSON.
package jsoncodec

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"

	"github.com/gin-gonic/gin/binding"
	ginjson "github.com/gin-gonic/gin/codec/json"

	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/core/types"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/httpvalue"
)

func init() {
	ginjson.API = codec{}
	binding.EnableDecoderDisallowUnknownFields = true
}

type codec struct{}

var moneyMarshal = json.MarshalToOption(func(enc *jsontext.Encoder, value types.MinorUnits) error {
	return httpvalue.MinorUnits(value).MarshalJSONTo(enc)
})
var moneyUnmarshal = json.UnmarshalFromOption(func(dec *jsontext.Decoder, value *types.MinorUnits) error {
	var wire httpvalue.MinorUnits
	if err := wire.UnmarshalJSONFrom(dec); err != nil {
		return err
	}
	*value = wire.Domain()
	return nil
})

// Marshal serializes an HTTP response, including domain values in generic
// projections. The core JSON facade keeps its independent storage codec.
func Marshal(v any) ([]byte, error) { return json.MarshalWithOptions(v, moneyMarshal) }

func (codec) Marshal(v any) ([]byte, error) {
	return Marshal(v)
}

func (codec) Unmarshal(data []byte, v any) error {
	return json.UnmarshalStrictWithOptions(data, v, moneyUnmarshal)
}

func (codec) MarshalIndent(v any, prefix, indent string) ([]byte, error) {
	return json.MarshalIndentWithOptions(v, prefix, indent, moneyMarshal)
}

func (codec) NewEncoder(w io.Writer) ginjson.Encoder {
	return json.NewEncoderWithOptions(w, moneyMarshal)
}

func (codec) NewDecoder(r io.Reader) ginjson.Decoder {
	return &requestDecoder{decoder: json.NewDecoderWithOptions(r, moneyUnmarshal)}
}

// DecodeRequestPreserveNumbers is for dynamic numeric HTTP contracts such as
// report filters. It retains number literals without changing storage codecs,
// and keeps the same strict fields, monetary strings and single-value checks.
func DecodeRequestPreserveNumbers(r io.Reader, v any) error {
	d := codec{}.NewDecoder(r)
	d.UseNumber()
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	return binding.Validator.ValidateStruct(v)
}

type requestDecoder struct {
	decoder *json.Decoder
}

func (d *requestDecoder) UseNumber() {
	d.decoder.UseNumber()
}

func (d *requestDecoder) DisallowUnknownFields() {
	d.decoder.DisallowUnknownFields()
}

func (d *requestDecoder) Decode(v any) error {
	if err := d.decoder.Decode(v); err != nil {
		return err
	}
	var extra any
	err := d.decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("request body must contain exactly one JSON value")
	}
	return fmt.Errorf("invalid data after request JSON value: %w", err)
}
