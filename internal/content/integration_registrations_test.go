package content

import (
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/domain/integrations"
	"metapus/internal/metadata"
)

func TestIntegrationProviderMetadataIncludesAllRegisteredProfiles(t *testing.T) {
	for _, model := range []any{integrations.Account{}, integrations.Pool{}} {
		definition := metadata.Inspect(model, "", metadata.TypeCatalog)
		found := false
		for _, field := range definition.Fields {
			if field.Name != "provider" {
				continue
			}
			found = true
			require.Len(t, field.EnumValues, len(integrations.ProviderDefinitions()))
			require.Contains(t, field.EnumValues, metadata.EnumValue{Value: "infura", Label: "Infura"})
		}
		require.True(t, found)
	}
}
