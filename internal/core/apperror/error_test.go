package apperror

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/core/types"
)

func TestAppErrorPreservesFixedPointQuantities(t *testing.T) {
	requested := types.NewQuantityFromInt64Scaled(9_007_199_254_740_993)
	available := types.NewQuantityFromInt64Scaled(9_007_199_254_740_991)

	appErr := NewBusinessRule("INSUFFICIENT_QUANTITY", "Недостаточное количество").
		WithDetail("requested", requested).
		WithDetail("available", available)

	encoded, err := json.Marshal(appErr)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"requested":900719925474.0993`)
	require.Contains(t, string(encoded), `"available":900719925474.0991`)
	require.JSONEq(t, `{
		"code":"INSUFFICIENT_QUANTITY",
		"message":"Недостаточное количество",
		"details":{
			"requested":900719925474.0993,
			"available":900719925474.0991
		}
	}`, string(encoded))
}
