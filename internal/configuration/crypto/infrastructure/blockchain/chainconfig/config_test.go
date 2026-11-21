package chainconfig

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"metapus/internal/core/json"

	"github.com/stretchr/testify/require"
)

func TestLoadRequiresEveryRoleAndResolvesSecretFiles(t *testing.T) {
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "api-key")
	require.NoError(t, os.WriteFile(secretPath, []byte("secret-value\n"), 0o600))
	configPath := filepath.Join(dir, "chains.json")
	config := `{"networks":{"TRON-NILE":{"roles":{` +
		`"indexer":[{"providerId":"secondary","priority":20,"url":"https://indexer-2.example"},{"providerId":"primary","priority":10,"url":"https://indexer.example","disableKeepAlives":true,"apiKeySecretFile":` + strconv.Quote(secretPath) + `}],` +
		`"fullnode":[{"providerId":"primary","priority":10,"url":"https://full.example"}],` +
		`"solidity":[{"providerId":"primary","priority":10,"url":"https://solidity.example"}]}}}}`
	require.NoError(t, os.WriteFile(configPath, []byte(config), 0o600))

	cfg, err := Load(configPath)
	require.NoError(t, err)
	network := cfg.Networks["TRON-NILE"]
	require.Equal(t, "primary", network.Endpoints(RoleIndexer)[0].ProviderID)
	require.True(t, network.Endpoints(RoleIndexer)[0].DisableKeepAlives)
	require.Equal(t, "secret-value", network.Endpoints(RoleIndexer)[0].APIKey)

	encoded, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret-value")
}

func TestLoadFailsClosedWhenRoleMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chains.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"networks":{"TRON-NILE":{"roles":{"indexer":[]}}}}`), 0o600))
	_, err := Load(path)
	require.ErrorContains(t, err, "has no indexer endpoint")
}

func TestLoadAllowsEmptyOptionalAPIKeyFileForPublicEndpoint(t *testing.T) {
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "empty-api-key")
	require.NoError(t, os.WriteFile(secretPath, nil, 0o600))
	configPath := filepath.Join(dir, "chains.json")
	endpoint := `{"providerId":"public","priority":0,"url":"https://rpc.example","apiKeySecretFile":` + strconv.Quote(secretPath) + `}`
	config := `{"networks":{"TRON-NILE":{"roles":{"indexer":[` + endpoint + `],"fullnode":[` + endpoint + `],"solidity":[` + endpoint + `]}}}}`
	require.NoError(t, os.WriteFile(configPath, []byte(config), 0o600))

	cfg, err := Load(configPath)
	require.NoError(t, err)
	require.Empty(t, cfg.Networks["TRON-NILE"].Endpoints(RoleIndexer)[0].APIKey)
}

func TestLoadAcceptsUTF8BOMFromWindowsTools(t *testing.T) {
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "api-key")
	require.NoError(t, os.WriteFile(secretPath, append(append([]byte{}, utf8BOM...), []byte("secret-value")...), 0o600))
	configPath := filepath.Join(dir, "chains.json")
	endpoint := `{"providerId":"primary","priority":0,"url":"https://rpc.example","apiKeySecretFile":` + strconv.Quote(secretPath) + `}`
	config := `{"networks":{"TRON-NILE":{"roles":{"indexer":[` + endpoint + `],"fullnode":[` + endpoint + `],"solidity":[` + endpoint + `]}}}}`
	require.NoError(t, os.WriteFile(configPath, append(append([]byte{}, utf8BOM...), []byte(config)...), 0o600))

	cfg, err := Load(configPath)
	require.NoError(t, err)
	require.Equal(t, "secret-value", cfg.Networks["TRON-NILE"].Endpoints(RoleIndexer)[0].APIKey)
}

func TestLoadAcceptsFamilyDefinedEVMRPCRole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chains.json")
	raw := `{"networks":{"ETHEREUM-SEPOLIA":{"roles":{"rpc":[{"providerId":"primary","priority":0,"url":"https://sepolia.example"}]}}}}`
	require.NoError(t, os.WriteFile(path, []byte(raw), 0o600))
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Len(t, cfg.Networks["ETHEREUM-SEPOLIA"].Endpoints(RoleRPC), 1)
}

func TestLoadLimitsIntervalsToEVMRPCAndBoundedValues(t *testing.T) {
	for _, interval := range []int{-1, 1, 30001, 7000} {
		path := filepath.Join(t.TempDir(), "chains.json")
		raw := `{"networks":{"ETHEREUM-SEPOLIA":{"roles":{"rpc":[{"providerId":"limited","priority":1,"url":"https://rpc.example","minRequestIntervalMs":` + strconv.Itoa(interval) + `}]}}}}`
		require.NoError(t, os.WriteFile(path, []byte(raw), 0o600))
		cfg, err := Load(path)
		if interval == 7000 {
			require.NoError(t, err)
			require.Equal(t, interval, cfg.Networks["ETHEREUM-SEPOLIA"].Endpoints(RoleRPC)[0].MinRequestIntervalMS)
		} else {
			require.ErrorContains(t, err, "minimum request interval")
		}
	}
}

func TestEVMSettingsDefaultsAndBoundedWindow(t *testing.T) {
	defaults := (EVMSettings{}).Defaults()
	require.NoError(t, defaults.Validate())
	require.Equal(t, 4, defaults.NativeConcurrency)
	require.Equal(t, 32, defaults.Window())
	smaller := EVMSettings{MaxResponseBytes: 8 << 20, MaxBufferedBytes: 32 << 20}
	require.NoError(t, smaller.Validate())
	require.Equal(t, 4, smaller.Window())
	for _, invalid := range []EVMSettings{
		{AttemptTimeoutMS: -1}, {ReadBudgetMS: 1000}, {BackfillConcurrency: 8}, {NativeConcurrency: 17}, {PrefetchBlocks: 2}, {MaxResponseBytes: 128 << 20}, {MaxBufferedBytes: 1},
	} {
		require.Error(t, invalid.Validate())
	}
}

func TestTRONBlockModeDefaultsAndRetiredIndexerConfig(t *testing.T) {
	require.Equal(t, "hybrid", (TRONSettings{}).Defaults().IngestionMode)
	require.Error(t, (TRONSettings{IngestionMode: "indexer"}).Validate())
	for _, mode := range []string{"shadow", "solidified", "hybrid", "paused"} {
		require.NoError(t, (TRONSettings{IngestionMode: mode}).Validate())
	}
	path := filepath.Join(t.TempDir(), "chains.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"networks":{"TRON-NILE":{"roles":{"fullnode":[{"providerId":"a","url":"https://a.example"}],"solidity":[{"providerId":"b","url":"https://b.example"}]},"tron":{"ingestionMode":"hybrid","legacyIndexer":true}}}}`), 0600))
	_, err := Load(path)
	require.ErrorContains(t, err, "legacyIndexer")
}
