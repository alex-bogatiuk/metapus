package content_test

import (
	"testing"

	"metapus/internal/configuration/configurationtest"
	"metapus/internal/configuration/crypto/content"
)

func TestCryptoKeepsTheConfigurationContract(t *testing.T) {
	t.Parallel()
	configurationtest.Check(t, content.Crypto(content.Services{}))
}
