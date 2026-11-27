package content_test

import (
	"testing"

	"github.com/alex-bogatiuk/metapus/configurations/automotive/content"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/sdktest"
)

func TestAutomotiveKeepsTheConfigurationContract(t *testing.T) {
	t.Parallel()
	sdktest.Check(t, content.Automotive())
}
