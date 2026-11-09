package postgres

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	corejson "metapus/internal/core/json"
)

func TestRegisterJSONCodecsUsesProductV2Semantics(t *testing.T) {
	t.Parallel()

	type payload struct {
		Items []string      `json:"items"`
		Wait  time.Duration `json:"wait"`
	}

	typeMap := pgtype.NewMap()
	RegisterJSONCodecs(typeMap)
	jsonbType, ok := typeMap.TypeForOID(pgtype.JSONBOID)
	require.True(t, ok)

	value := payload{Wait: 2 * time.Second}
	encodePlan := jsonbType.Codec.PlanEncode(typeMap, pgtype.JSONBOID, pgtype.TextFormatCode, value)
	require.NotNil(t, encodePlan)
	encoded, err := encodePlan.Encode(value, nil)
	require.NoError(t, err)
	require.JSONEq(t, `{"items":[],"wait":2000000000}`, string(encoded))

	var decoded payload
	scanPlan := jsonbType.Codec.PlanScan(typeMap, pgtype.JSONBOID, pgtype.TextFormatCode, &decoded)
	require.NotNil(t, scanPlan)
	require.NoError(t, scanPlan.Scan(encoded, &decoded))
	require.Empty(t, decoded.Items)
	require.Equal(t, 2*time.Second, decoded.Wait)

	raw := corejson.RawMessage(`{"n":9007199254740993}`)
	rawPlan := jsonbType.Codec.PlanEncode(typeMap, pgtype.JSONBOID, pgtype.TextFormatCode, raw)
	require.NotNil(t, rawPlan)
	rawEncoded, err := rawPlan.Encode(raw, nil)
	require.NoError(t, err)
	require.Equal(t, []byte(raw), rawEncoded, "RawMessage must not be marshaled as a base64 string or nested JSON string")
}
