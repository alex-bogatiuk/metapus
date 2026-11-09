// Package httpvalue defines exact wire values for HTTP contracts. These codecs
// must not be used implicitly for stored, signed or fingerprinted domain values.
package httpvalue

import (
	"encoding/json/jsontext"
	"fmt"
	"metapus/internal/core/types"
	"strconv"
)

// MinorUnits is an int64 amount serialized strictly as a canonical integer
// string. It preserves cents/wei above JavaScript's exact-number limit.
type MinorUnits int64

func (m MinorUnits) Domain() types.MinorUnits { return types.MinorUnits(m) }
func (m MinorUnits) MarshalJSON() ([]byte, error) {
	return []byte(`"` + strconv.FormatInt(int64(m), 10) + `"`), nil
}
func (m MinorUnits) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteToken(jsontext.String(strconv.FormatInt(int64(m), 10)))
}

func ParseMinorUnits(text string) (MinorUnits, error) {
	value, err := types.ParseMinorUnits(text)
	return MinorUnits(value), err
}

func (m *MinorUnits) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if dec.PeekKind() != '"' {
		return fmt.Errorf("money must be a JSON string of minor units")
	}
	token, err := dec.ReadToken()
	if err != nil {
		return err
	}
	value, err := ParseMinorUnits(token.String())
	if err != nil {
		return err
	}
	*m = value
	return nil
}

func (m *MinorUnits) UnmarshalJSON(data []byte) error {
	if len(data) < 2 || data[0] != '"' || data[len(data)-1] != '"' {
		return fmt.Errorf("money must be a JSON string of minor units")
	}
	text, err := strconv.Unquote(string(data))
	if err != nil {
		return err
	}
	value, err := ParseMinorUnits(text)
	if err != nil {
		return err
	}
	*m = value
	return nil
}

func (m MinorUnits) MinorUnitsValue() int64 { return int64(m) }
