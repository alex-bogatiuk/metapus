package content_test

import (
	"testing"

	"metapus/internal/configuration/autoparts/content"
	"metapus/internal/configuration/configurationtest"
)

func TestAutopartsKeepsTheConfigurationContract(t *testing.T) {
	t.Parallel()
	configurationtest.Check(t, content.Autoparts())
}
