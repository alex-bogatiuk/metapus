package contracts

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/infrastructure/http/httpvalue"
	"metapus/internal/infrastructure/http/jsoncodec"
)

type presenceFixture struct {
	Optional *id.ID               `json:"optional,omitempty"`
	Nullable *id.ID               `json:"nullable"`
	False    bool                 `json:"false,omitempty"`
	Zero     int                  `json:"zero,omitempty"`
	OmitZero int                  `json:"omitZero,omitzero"`
	Money    httpvalue.MinorUnits `json:"money,omitempty"`
	Time     time.Time            `json:"time,omitzero"`
	Quantity types.Quantity       `json:"quantity"`
	Secret   string               `json:"-"`
}

func TestGenerationUsesHTTPFacadePresenceAndSpecialTypes(t *testing.T) {
	output, err := Generate([]Contract{Response[presenceFixture]("Presence")})
	require.NoError(t, err)
	for _, line := range []string{
		`"optional"?: string`, `"nullable": string | null`, `"false": boolean`, `"zero": number`,
		`"omitZero"?: number`, `"money": MinorUnits`, `"time"?: string`, `"quantity": number`,
	} {
		require.Contains(t, string(output), line)
	}
	require.NotContains(t, string(output), `"optional"?: string | null`)
	require.NotContains(t, string(output), "Secret")
	type input struct {
		Required *httpvalue.MinorUnits `json:"amount" binding:"required"`
		Optional *httpvalue.MinorUnits `json:"optional,omitempty"`
		Quantity types.Quantity        `json:"quantity" binding:"required"`
	}
	output, err = Generate([]Contract{Request[input]("Input")})
	require.NoError(t, err)
	require.Contains(t, string(output), `"amount": MinorUnits`)
	require.NotContains(t, string(output), `"amount": MinorUnits | null`)
	require.Contains(t, string(output), `"optional"?: MinorUnits | null`)
	require.Contains(t, string(output), `"quantity": number | string`)
	schemas, err := GenerateSchemas([]Contract{Response[presenceFixture]("Presence"), Request[input]("Input")})
	require.NoError(t, err)
	for _, line := range []string{
		`"optional": z.string().optional()`, `"nullable": z.string().nullable()`,
		`"false": z.boolean()`, `"zero": z.number().int()`, `"omitZero": z.number().int().optional()`,
		`"amount": minorUnitsSchema`, `"optional": minorUnitsSchema.nullable().optional()`,
	} {
		require.Contains(t, string(schemas), line)
	}
}

func TestByteCollectionsMatchHTTPCodec(t *testing.T) {
	type bytesFixture struct {
		Slice []byte  `json:"slice"`
		Array [2]byte `json:"array"`
	}
	encoded, err := jsoncodec.Marshal(bytesFixture{Slice: []byte{1, 2}, Array: [2]byte{1, 2}})
	require.NoError(t, err)
	require.JSONEq(t, `{"slice":"AQI=","array":"AQI="}`, string(encoded))
	output, err := GenerateSchemas([]Contract{Response[bytesFixture]("Bytes")})
	require.NoError(t, err)
	require.Contains(t, string(output), `"slice": z.string()`)
	require.Contains(t, string(output), `"array": z.string()`)
}

type customWire struct{ Internal string }

func (customWire) MarshalJSON() ([]byte, error) { return []byte(`"custom"`), nil }

func TestGenerationRejectsUnknownCustomCodecs(t *testing.T) {
	_, err := Generate([]Contract{Response[customWire]("Custom")})
	require.ErrorContains(t, err, "requires an explicit HTTP mapping")
	type stringNumber struct {
		Count int64 `json:"count,string"`
	}
	_, err = Generate([]Contract{Response[stringNumber]("StringNumber")})
	require.ErrorContains(t, err, "requires an explicit HTTP projection")
}
