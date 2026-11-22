package content_test

import (
	"testing"

	"metapus/internal/configuration/autoparts/content"
	"metapus/pkg/sdk/sdktest"
)

func TestAutopartsKeepsTheConfigurationContract(t *testing.T) {
	t.Parallel()
	sdktest.Check(t, content.Autoparts())
}
