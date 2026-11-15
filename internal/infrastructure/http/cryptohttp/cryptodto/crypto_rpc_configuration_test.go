package cryptodto

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
)

func TestRPCConfigurationDTORequiresExplicitIntentAndRejectsOtherFields(t *testing.T) {
	base := fmt.Sprintf(`{"expectedRevision":%q,"endpoints":[{"id":%q,"version":1,"isActive":false,"priority":0}]}`, strings.Repeat("a", 64), id.New().String())
	var req UpdateCryptoRPCConfigurationRequest
	require.NoError(t, json.UnmarshalStrict([]byte(base), &req))
	update, err := req.ToDomain()
	require.NoError(t, err)
	require.False(t, update.Endpoints[0].IsActive)
	for _, body := range []string{strings.Replace(base, `"isActive":false,`, "", 1), strings.Replace(base, `,"priority":0`, "", 1)} {
		var missing UpdateCryptoRPCConfigurationRequest
		require.NoError(t, json.UnmarshalStrict([]byte(body), &missing))
		_, err := missing.ToDomain()
		require.Error(t, err)
	}
	unknown := strings.Replace(base, `"priority":0`, `"priority":0,"url":"https://untrusted.example"`, 1)
	require.Error(t, json.UnmarshalStrict([]byte(unknown), &req))
}

func TestRPCSettingsDTORejectsUnknownNestedFieldsAndWrongNumericTypes(t *testing.T) {
	base := `{"expectedRevision":"` + strings.Repeat("a", 64) + `","settings":{"evm":{"attemptTimeoutMs":10000,"readBudgetMs":25000},"alerts":{"maxLagSeconds":120}}}`
	var req UpdateCryptoRPCConfigurationRequest
	require.NoError(t, json.UnmarshalStrict([]byte(base), &req))
	update, err := req.ToDomain()
	require.NoError(t, err)
	require.NoError(t, update.Settings.Validate("evm"))
	for _, body := range []string{
		strings.Replace(base, `"maxLagSeconds":120`, `"maxLagSeconds":120,"disabled":true`, 1),
		strings.Replace(base, `"attemptTimeoutMs":10000`, `"attemptTimeoutMs":10000,"unsafeQuorum":1`, 1),
		strings.Replace(base, `"maxLagSeconds":120`, `"maxLagSeconds":1.5`, 1),
	} {
		require.Error(t, json.UnmarshalStrict([]byte(body), &req))
	}
}
