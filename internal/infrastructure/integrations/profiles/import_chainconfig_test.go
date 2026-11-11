package profiles

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	model "metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/blockchain/chainconfig"
)

// A loaded chains.json trims the trailing slash of an Alchemy base whose key
// lives in a secret file; the import must still bind that key to the base.
func TestImportAcceptsAlchemyBasesLoadedWithSeparateKeyFiles(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "alchemy_key")
	require.NoError(t, os.WriteFile(keyFile, []byte("file-key\n"), 0o600))
	chains := filepath.Join(dir, "chains.json")
	require.NoError(t, os.WriteFile(chains, []byte(`{"networks":{
		"TRON-NILE":{"roles":{"fullnode":[{"providerId":"alchemy-nile","priority":20,"url":"https://tron-testnet.g.alchemy.com/v2/","apiKeySecretFile":"`+keyFile+`"}]}},
		"ETHEREUM-SEPOLIA":{"roles":{"rpc":[{"providerId":"alchemy-sepolia","priority":5,"url":"https://eth-sepolia.g.alchemy.com/v2/","apiKeySecretFile":"`+keyFile+`"}]}}}}`), 0o600))
	cfg, err := chainconfig.Load(chains)
	require.NoError(t, err)

	for code, want := range map[string]string{"TRON-NILE": "https://tron-testnet.g.alchemy.com/v2/", "ETHEREUM-SEPOLIA": "https://eth-sepolia.g.alchemy.com/v2/"} {
		for role, endpoints := range cfg.Networks[code].Roles {
			endpoint := endpoints[0]
			provider, base, key, err := (*Registry)(nil).ImportRPC(endpoint.URL, endpoint.APIKey, string(role), "default")
			require.NoError(t, err, code)
			require.Equal(t, model.Alchemy, provider)
			require.Equal(t, want, base)
			require.Equal(t, "file-key", key)
		}
	}
}
