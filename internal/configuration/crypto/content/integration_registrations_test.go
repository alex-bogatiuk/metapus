package content

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/configuration"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/integrations"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/metadata"
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
