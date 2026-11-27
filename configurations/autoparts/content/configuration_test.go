package content_test

import (
	"testing"

	"github.com/alex-bogatiuk/metapus/configurations/autoparts/content"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/sdktest"
)

func TestAutoPartsKeepsTheConfigurationContract(t *testing.T) {
	t.Parallel()
	sdktest.Check(t, content.AutoParts())
}
