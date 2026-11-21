package none_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration"
	"metapus/internal/configuration/configurationtest"
	"metapus/internal/configuration/none"
	"metapus/internal/domain/auth"
)

func TestKeepsTheConfigurationContract(t *testing.T) {
	t.Parallel()
	configurationtest.Check(t, none.New())
}

// The empty configuration adds nothing: the assembled application has only
// what the platform registers itself.
func TestAddsNothingToThePlatform(t *testing.T) {
	t.Parallel()
	reg := configuration.Compose(none.New())
	require.Empty(t, reg.Catalogs())
	require.Empty(t, reg.Documents())
	require.Empty(t, reg.Registers())
	require.Empty(t, reg.Datasets())
	require.Equal(t, auth.SystemPermissions(), configuration.Permissions(none.New()))
}
