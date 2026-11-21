package none_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration"
	"metapus/internal/configuration/configurationtest"
	"metapus/internal/configuration/none"
	"metapus/internal/domain/auth"
	"metapus/internal/domain/integrations"
	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/platformcontent"
)

func TestKeepsTheConfigurationContract(t *testing.T) {
	t.Parallel()
	configurationtest.Check(t, none.New())
}

// The empty configuration adds nothing: the assembled application has the
// platform's shared entities and system permissions only.
func TestAddsNothingToThePlatform(t *testing.T) {
	t.Parallel()
	platform := v1.NewFactoryRegistry()
	platformcontent.Register(platform, platformcontent.Integrations{})
	require.NotEmpty(t, platform.Catalogs())

	reg := configuration.Compose(none.New())
	require.Equal(t, entityNames(platform), entityNames(reg))
	require.Empty(t, reg.Documents())
	require.Empty(t, reg.Datasets())
	require.Equal(t, append(platform.Permissions(), auth.SystemPermissions()...), configuration.Permissions(none.New()))
	require.Equal(t, integrations.PlatformCatalog().Providers(), configuration.IntegrationCatalog(none.New()).Providers())

	artifacts, _, err := configuration.HTTPArtifacts(none.New())
	require.NoError(t, err)
	require.Len(t, artifacts, 3, "only the platform's frontend files")
}

func entityNames(reg *v1.FactoryRegistry) []string {
	var names []string
	for _, catalog := range reg.Catalogs() {
		names = append(names, catalog.EntityName())
	}
	for _, register := range reg.Registers() {
		names = append(names, register.RoutePrefix())
	}
	return names
}
