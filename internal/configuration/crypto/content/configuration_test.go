package content_test

import (
	"testing"

	"metapus/internal/configuration/crypto/content"
	"metapus/pkg/sdk/sdktest"
)

func TestCryptoKeepsTheConfigurationContract(t *testing.T) {
	t.Parallel()
	sdktest.Check(t, content.Crypto(content.Services{}))
}
