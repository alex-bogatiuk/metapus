package content_test

import (
	"testing"

	"github.com/alex-bogatiuk/metapus/configurations/trade/content"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/sdktest"
)

func TestTradeKeepsTheConfigurationContract(t *testing.T) {
	t.Parallel()
	sdktest.Check(t, content.Trade())
}
