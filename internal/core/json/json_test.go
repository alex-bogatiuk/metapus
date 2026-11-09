package json

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMarshalNilSliceAndMapAreEmpty(t *testing.T) {
	t.Parallel()
	type payload struct {
		Items []string
		Meta  map[string]string
	}
	got, err := Marshal(payload{})
	require.NoError(t, err)
	require.JSONEq(t, `{"Items":[],"Meta":{}}`, string(got))
}

func TestOmitZeroBoolOmitsFalse(t *testing.T) {
	t.Parallel()
	type payload struct {
		Flag bool `json:"flag,omitzero"`
	}
	got, err := Marshal(payload{})
	require.NoError(t, err)
	require.Equal(t, `{}`, string(got))
}

func TestLegacyOmitEmptySemanticsAreNotGlobal(t *testing.T) {
	t.Parallel()
	type payload struct {
		Flag bool `json:"flag,omitempty"`
	}
	got, err := Marshal(payload{})
	require.NoError(t, err)
	require.Equal(t, `{"flag":false}`, string(got))
}

func TestMarshalDoesNotEscapeHTMLOrJavaScript(t *testing.T) {
	t.Parallel()
	got, err := Marshal(map[string]string{"text": "<>&\u2028\u2029"})
	require.NoError(t, err)
	require.Equal(t, "{\"text\":\"<>&\u2028\u2029\"}", string(got))
}

func TestMarshalDeterministicSortsMapKeys(t *testing.T) {
	t.Parallel()
	value := map[string]any{"z": 1, "nested": map[string]any{"b": 2, "a": 1}, "a": 3}
	for range 100 {
		got, err := MarshalDeterministic(value)
		require.NoError(t, err)
		require.Equal(t, `{"a":3,"nested":{"a":1,"b":2},"z":1}`, string(got))
	}
}

func TestDurationEncodesAsNanoseconds(t *testing.T) {
	t.Parallel()
	type payload struct {
		Wait time.Duration `json:"wait"`
	}
	got, err := Marshal(payload{Wait: 2 * time.Second})
	require.NoError(t, err)
	require.JSONEq(t, `{"wait":2000000000}`, string(got))

	var decoded payload
	require.NoError(t, Unmarshal(got, &decoded))
	require.Equal(t, 2*time.Second, decoded.Wait)
}

func TestUnmarshalIsCaseSensitive(t *testing.T) {
	t.Parallel()
	var got struct {
		Name string `json:"name"`
	}
	require.NoError(t, Unmarshal([]byte(`{"Name":"x","name":"y"}`), &got))
	require.Equal(t, "y", got.Name)
}

func TestUnmarshalRejectsDuplicateNames(t *testing.T) {
	t.Parallel()
	var got map[string]int
	err := Unmarshal([]byte(`{"a":1,"a":2}`), &got)
	require.Error(t, err)
}

func TestUnmarshalPreserveNumbers(t *testing.T) {
	t.Parallel()
	var got any
	require.NoError(t, UnmarshalPreserveNumbers([]byte(`{"n":9007199254740993,"ok":true}`), &got))
	obj := got.(map[string]any)
	n, ok := obj["n"].(Number)
	require.True(t, ok)
	require.Equal(t, "9007199254740993", n.String())
	require.Equal(t, true, obj["ok"])
}

func TestLegacyV1CanonicalizationIgnoresFormattingAndKeyOrder(t *testing.T) {
	t.Parallel()
	a, err := CanonicalizeLegacyV1([]byte(`{"amount":1,"to":"T"}`))
	require.NoError(t, err)
	b, err := CanonicalizeLegacyV1([]byte("{\n  \"to\": \"T\", \"amount\": 1\n}"))
	require.NoError(t, err)
	require.Equal(t, a, b)
	require.Equal(t, `{"amount":1,"to":"T"}`, string(a))
}

func TestLegacyV1CanonicalizationPreservesIntegerPrecisionAndEscaping(t *testing.T) {
	t.Parallel()
	got, err := CanonicalizeLegacyV1([]byte(`{"text":"<>&\u2028\u2029","n":9007199254740993}`))
	require.NoError(t, err)
	require.Equal(t, `{"n":9007199254740993,"text":"\u003c\u003e\u0026\u2028\u2029"}`, string(got))
}

func TestLegacyV1CanonicalizationRejectsTrailingValue(t *testing.T) {
	t.Parallel()
	_, err := CanonicalizeLegacyV1([]byte(`{"a":1}{"b":2}`))
	require.Error(t, err)
}

func TestValid(t *testing.T) {
	t.Parallel()
	require.True(t, Valid([]byte(`{"a":1}`)))
	require.False(t, Valid([]byte(`{"a":1,"a":2}`)))
	require.False(t, Valid([]byte(`{`)))
}

func TestEncoderDecodeRoundTrip(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	require.NoError(t, enc.Encode(map[string]string{"a": "<b>&\u2028"}))
	require.True(t, strings.HasSuffix(buf.String(), "\n"))
	require.Contains(t, buf.String(), `"<b>&`)
	require.Contains(t, buf.String(), "\u2028")

	dec := NewDecoder(strings.NewReader(buf.String()))
	var got map[string]string
	require.NoError(t, dec.Decode(&got))
	require.Equal(t, "<b>&\u2028", got["a"])
	require.ErrorIs(t, dec.Decode(&got), io.EOF)
}

func TestDecoderUseNumberAndDisallowUnknown(t *testing.T) {
	t.Parallel()
	dec := NewDecoder(strings.NewReader(`{"n":1}`))
	dec.UseNumber()
	var got map[string]any
	require.NoError(t, dec.Decode(&got))
	_, ok := got["n"].(Number)
	require.True(t, ok)

	type row struct {
		N int `json:"n"`
	}
	strict := NewDecoder(strings.NewReader(`{"n":1,"x":2}`))
	strict.DisallowUnknownFields()
	var decoded row
	require.Error(t, strict.Decode(&decoded))
}

func TestMarshalLegacyV1NoHTMLEscapeMatchesHistoricalITRXBody(t *testing.T) {
	t.Parallel()
	type body struct {
		Energy int    `json:"energy_amount"`
		Active int    `json:"active"`
		Period string `json:"period"`
		Note   string `json:"note"`
		Items  []int  `json:"items"`
	}
	got, err := MarshalLegacyV1NoHTMLEscape(body{Energy: 65000, Period: "1H", Note: "<>&\u2028"})
	require.NoError(t, err)
	require.Equal(t, `{"active":0,"energy_amount":65000,"items":null,"note":"<>&\u2028","period":"1H"}`, string(got))
}
