package architecture_test

import (
	"go/ast"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// configurationCommands are the cmd/ programs of the crypto-processing
// configuration. They may import configuration packages freely.
var configurationCommands = []string{
	"aml-pool-controller",
	"aml-testnet-bootstrap",
	"aml-testnet-config",
	"aml-wallet-provision",
	"crypto-ingestion-monitor",
	"crypto-preflight",
	"crypto-record-wallet-funding",
	"crypto-server",
	"crypto-worker",
	"integration-config",
	"nile-tcp-relay",
	"public-test-bootstrap",
}

// platformCommands are the cmd/ programs any configuration ships or uses.
// They must reach a configuration only through a platform extension point.
var platformCommands = []string{
	"healthcheck",
	"http-contracts",
	"integration-test",
	"npm-connect-proxy",
	"pdf-renderer",
	"pdf-renderer-smoke",
	"public-edge-proxy",
	"public-test-metrics-sink",
	"scaffold",
	"seed",
	"server",
	"tenant",
	"worker",
}

// knownCommandConfigurationLeaks lists tolerated platform command →
// configuration imports: commands that still pick the crypto configuration
// themselves. Each edge goes away once the command receives its
// configuration from a configuration's own main. Only remove entries: a new
// edge fails the test.
var knownCommandConfigurationLeaks = map[string][]string{
	"metapus/cmd/http-contracts": {
		"metapus/internal/configuration/crypto/content",
	},
	"metapus/cmd/integration-test": {
		"metapus/internal/configuration/crypto/content",
	},
	"metapus/cmd/tenant": {
		"metapus/internal/configuration/crypto/content",
	},
}

// knownCommandConfigurationTestLeaks lists platform commands whose tests
// import configuration packages. It is empty: test a configuration's
// behaviour in its own packages. Keep it empty.
var knownCommandConfigurationTestLeaks = map[string][]string{}

// knownCommandConfigurationTableLeaks lists platform command files whose
// string literals name configuration tables. Only remove entries.
var knownCommandConfigurationTableLeaks = map[string][]string{
	"cmd/seed/main.go": {
		"cat_blockchain_networks",
		"cat_merchants",
		"cat_tokens",
		"cat_wallets",
		"reg_fee_schedule",
	},
}

func TestEveryCommandIsClassified(t *testing.T) {
	t.Parallel()
	repoRoot := repositoryRoot(t)
	entries, err := os.ReadDir(filepath.Join(repoRoot, "cmd"))
	require.NoError(t, err)

	present := map[string]bool{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		present[name] = true
		platform := slices.Contains(platformCommands, name)
		configuration := slices.Contains(configurationCommands, name)
		switch {
		case platform && configuration:
			t.Errorf("command cmd/%s is listed as both platform and configuration", name)
		case !platform && !configuration:
			t.Errorf("command cmd/%s is not classified; add it to platformCommands or configurationCommands", name)
		}
	}
	for _, name := range append(slices.Clone(platformCommands), configurationCommands...) {
		if !present[name] {
			t.Errorf("command cmd/%s no longer exists; remove it from its list", name)
		}
	}
}

func TestPlatformCommandsDoNotDependOnConfiguration(t *testing.T) {
	t.Parallel()
	compareKnownLeaks(t, commandConfigurationImports(t, false), knownCommandConfigurationLeaks,
		"platform command %s imports configuration package %s; "+
			"pass the configuration in through a platform interface or move the command to configurationCommands",
		"leak %s -> %s is gone; remove it from knownCommandConfigurationLeaks")
}

func TestPlatformCommandTestsDoNotImportConfiguration(t *testing.T) {
	t.Parallel()
	compareKnownLeaks(t, commandConfigurationImports(t, true), knownCommandConfigurationTestLeaks,
		"tests of platform command %s import configuration package %s; move the test to the configuration side",
		"leak %s -> %s is gone; remove it from knownCommandConfigurationTestLeaks")
}

func TestPlatformCommandsDoNotQueryConfigurationTables(t *testing.T) {
	t.Parallel()
	repoRoot := repositoryRoot(t)
	tablePattern := configurationTablePattern(t, repoRoot)

	actual := map[string]map[string]bool{}
	walkGo(t, repoRoot, []string{"cmd"}, false, isConfigurationCommandPackage, func(rel string, file *ast.File) {
		collectConfigurationTables(actual, tablePattern, rel, file)
	})
	compareKnownLeaks(t, actual, knownCommandConfigurationTableLeaks,
		"platform command file %s names configuration table %s; move the query into a configuration package",
		"leak %s -> %s is gone; remove it from knownCommandConfigurationTableLeaks")
}

// commandConfigurationImports maps each platform command package to the
// configuration packages its production files (or its tests) import.
func commandConfigurationImports(t *testing.T, tests bool) map[string]map[string]bool {
	t.Helper()
	actual := map[string]map[string]bool{}
	walkGo(t, repositoryRoot(t), []string{"cmd"}, tests, isConfigurationCommandPackage, func(rel string, file *ast.File) {
		pkg := "metapus/" + filepath.ToSlash(filepath.Dir(rel))
		for _, imported := range file.Imports {
			value, err := strconv.Unquote(imported.Path.Value)
			require.NoError(t, err)
			if !isConfigurationPackage(value) {
				continue
			}
			if actual[pkg] == nil {
				actual[pkg] = map[string]bool{}
			}
			actual[pkg][value] = true
		}
	})
	return actual
}

func isConfigurationCommandPackage(importPath string) bool {
	for _, name := range configurationCommands {
		pkg := "metapus/cmd/" + name
		if importPath == pkg || strings.HasPrefix(importPath, pkg+"/") {
			return true
		}
	}
	return false
}
