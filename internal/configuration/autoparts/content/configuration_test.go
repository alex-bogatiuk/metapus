package content_test

import (
	"testing"

	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/content"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/sdktest"
)

func TestAutopartsKeepsTheConfigurationContract(t *testing.T) {
	t.Parallel()
	sdktest.Check(t, content.Autoparts())
}
