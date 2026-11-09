package json

import (
	"bytes"
	jsonv1 "encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFacadeMatchesEncodingJSONV1(t *testing.T) {
	t.Parallel()
	type payload struct {
		Name    string         `json:"name"`
		Enabled bool           `json:"enabled,omitempty"`
		Items   []string       `json:"items"`
		Meta    map[string]any `json:"meta"`
	}
	input := payload{Name: "<>&\u2028\u2029"}

	want, err := jsonv1.Marshal(input)
	require.NoError(t, err)
	got, err := Marshal(input)
	require.NoError(t, err)
	require.Equal(t, want, got)

	var wantDecoded, gotDecoded payload
	require.NoError(t, jsonv1.Unmarshal([]byte(`{"NAME":"legacy","enabled":false}`), &wantDecoded))
	require.NoError(t, Unmarshal([]byte(`{"NAME":"legacy","enabled":false}`), &gotDecoded))
	require.Equal(t, wantDecoded, gotDecoded)
}

func TestEncoderSetEscapeHTMLMatchesEncodingJSONV1(t *testing.T) {
	t.Parallel()
	value := map[string]string{"text": "<>&\u2028\u2029"}

	var want bytes.Buffer
	wantEncoder := jsonv1.NewEncoder(&want)
	wantEncoder.SetEscapeHTML(false)
	require.NoError(t, wantEncoder.Encode(value))

	var got bytes.Buffer
	gotEncoder := NewEncoder(&got)
	gotEncoder.SetEscapeHTML(false)
	require.NoError(t, gotEncoder.Encode(value))
	require.Equal(t, want.Bytes(), got.Bytes())

	wantDecoder := jsonv1.NewDecoder(strings.NewReader(`{"n":9007199254740993}`))
	wantDecoder.UseNumber()
	gotDecoder := NewDecoder(strings.NewReader(`{"n":9007199254740993}`))
	gotDecoder.UseNumber()
	var wantValue, gotValue any
	require.NoError(t, wantDecoder.Decode(&wantValue))
	require.NoError(t, gotDecoder.Decode(&gotValue))
	require.Equal(t, wantValue, gotValue)
}
