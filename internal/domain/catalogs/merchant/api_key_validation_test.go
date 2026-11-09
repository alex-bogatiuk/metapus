package merchant

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestAPIKeyIssuanceUsesExplicitTimeWithoutMutatingModel(t *testing.T) {
	expiry := time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)
	key := MerchantAPIKey{Name: "test", Scopes: DefaultScopes(), ExpiresAt: &expiry}
	before := key
	require.NoError(t, key.Validate(t.Context()), "an expired stored key is structurally valid")
	require.NoError(t, key.ValidateAt(t.Context(), expiry.Add(-time.Nanosecond)))
	require.Error(t, key.ValidateAt(t.Context(), expiry))
	require.Equal(t, before, key)
}
