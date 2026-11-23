package content_test

import (
	"testing"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/content"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/sdktest"
)

func TestCryptoKeepsTheConfigurationContract(t *testing.T) {
	t.Parallel()
	sdktest.Check(t, content.Crypto(content.Services{}))
}
