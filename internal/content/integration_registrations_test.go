package content

import (
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration"
	"metapus/internal/domain/crypto/cryptointegrations"
	"metapus/internal/domain/integrations"
	"metapus/internal/metadata"
)

// Composing the application lists its catalog's providers, the crypto ones
// included, as the values of integration accounts' and pools' provider.
func TestIntegrationProviderMetadataIncludesAllRegisteredProfiles(t *testing.T) {
	configuration.Compose(Crypto(Services{}))
	for _, model := range []any{integrations.Account{}, integrations.Pool{}} {
		definition := metadata.Inspect(model, "", metadata.TypeCatalog)
		found := false
		for _, field := range definition.Fields {
			if field.Name != "provider" {
				continue
			}
			found = true
			require.Len(t, field.EnumValues, len(cryptointegrations.Catalog().Providers()))
			require.Contains(t, field.EnumValues, metadata.EnumValue{Value: "infura", Label: "Infura"})
		}
		require.True(t, found)
	}
}
