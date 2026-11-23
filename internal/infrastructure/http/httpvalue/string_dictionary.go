package httpvalue

import (
	"encoding/json/jsontext"
	"fmt"

	"github.com/alex-bogatiuk/metapus/internal/core/json"
)

// StringDictionary distinguishes omission from {} and rejects null,
// including null values inside the dictionary, at the strict HTTP boundary.
type StringDictionary map[string]string

func (m *StringDictionary) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if dec.PeekKind() != '{' {
		return fmt.Errorf("metadata must be an object of string values")
	}
	raw, err := dec.ReadValue()
	if err != nil {
		return err
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return err
	}
	metadata := make(StringDictionary, len(values))
	for key, value := range values {
		if len(value) == 0 || value[0] != '"' {
			return fmt.Errorf("metadata values must be strings")
		}
		var text string
		if err := json.Unmarshal(value, &text); err != nil {
			return err
		}
		metadata[key] = text
	}
	*m = metadata
	return nil
}
