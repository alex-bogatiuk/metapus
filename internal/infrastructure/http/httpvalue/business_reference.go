package httpvalue

import (
	"encoding/json/jsontext"
	"fmt"
)

// BusinessReference permits omission, while rejecting explicit null, empty
// strings and non-string values at the HTTP boundary.
type BusinessReference string

func (r *BusinessReference) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if dec.PeekKind() != '"' {
		return fmt.Errorf("business reference must be a non-empty string")
	}
	token, err := dec.ReadToken()
	if err != nil {
		return err
	}
	if token.String() == "" {
		return fmt.Errorf("business reference must be a non-empty string")
	}
	*r = BusinessReference(token.String())
	return nil
}
