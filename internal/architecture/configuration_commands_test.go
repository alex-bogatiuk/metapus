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

// configurationCommands are the cmd/ programs of the repository's
// configurations, and http-contracts, which generates the frontend files of
// all of them. They may import configuration packages freely.
var configurationCommands = []string{
	"autoparts-seed",
	"autoparts-server",
	"autoparts-tenant",
	"autoparts-worker",
	"trade-seed",
	"trade-server",
	"trade-tenant",
	"trade-worker",
	"aml-pool-controller",
	"aml-testnet-bootstrap",
	"aml-testnet-config",
	"aml-wallet-provision",
	"crypto-ingestion-monitor",
	"crypto-preflight",
	"crypto-record-wallet-funding",
	"crypto-seed",
	"crypto-server",
	"crypto-tenant",
	"crypto-worker",
	"http-contracts",
	"integration-config",
	"nile-tcp-relay",
	"public-test-bootstrap",
}

// platformCommands are the cmd/ programs any configuration ships or uses.
// They must reach a configuration only through a platform extension point.
var platformCommands = []string{
	"healthcheck",
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
	"github.com/alex-bogatiuk/metapus/cmd/integration-test": {
		"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/content",
	},
}

// knownCommandConfigurationTestLeaks lists platform commands whose tests
// import configuration packages. It is empty: test a configuration's
// behaviour in its own packages. Keep it empty.
var knownCommandConfigurationTestLeaks = map[string][]string{}

// knownCommandConfigurationTableLeaks lists platform command files whose
// string literals name configuration tables. It is empty: keep it empty.
var knownCommandConfigurationTableLeaks = map[string][]string{}

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
		pkg := "github.com/alex-bogatiuk/metapus/" + filepath.ToSlash(filepath.Dir(rel))
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
		pkg := "github.com/alex-bogatiuk/metapus/cmd/" + name
		if importPath == pkg || strings.HasPrefix(importPath, pkg+"/") {
			return true
		}
	}
	return false
}

// knownConfigurationCommandPlatformImports lists the platform packages
// outside pkg/sdk that configuration commands still import: utilities that
// open pools, bind tenants and read secrets themselves, and the contracts
// generator that walks every configuration of the repository. Each entry
// goes away once the platform offers it through pkg/sdk (a tenant task
// runner, a public contracts generator). Only remove entries: a new import
// fails the test.
var knownConfigurationCommandPlatformImports = map[string][]string{
	"github.com/alex-bogatiuk/metapus/cmd/aml-pool-controller": {
		"github.com/alex-bogatiuk/metapus/internal/core/id", "github.com/alex-bogatiuk/metapus/internal/core/json",
		"github.com/alex-bogatiuk/metapus/internal/core/tenant", "github.com/alex-bogatiuk/metapus/internal/core/types",
		"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres", "github.com/alex-bogatiuk/metapus/internal/platform/secretfile",
	},
	"github.com/alex-bogatiuk/metapus/cmd/aml-testnet-bootstrap": {
		"github.com/alex-bogatiuk/metapus/internal/core/id", "github.com/alex-bogatiuk/metapus/internal/core/json",
		"github.com/alex-bogatiuk/metapus/internal/core/tenant", "github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres",
		"github.com/alex-bogatiuk/metapus/internal/platform/secretfile",
	},
	"github.com/alex-bogatiuk/metapus/cmd/aml-testnet-config": {
		"github.com/alex-bogatiuk/metapus/internal/core/id", "github.com/alex-bogatiuk/metapus/internal/core/json",
		"github.com/alex-bogatiuk/metapus/internal/core/tenant", "github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres",
		"github.com/alex-bogatiuk/metapus/internal/platform/secretfile",
	},
	"github.com/alex-bogatiuk/metapus/cmd/aml-wallet-provision": {
		"github.com/alex-bogatiuk/metapus/internal/core/id", "github.com/alex-bogatiuk/metapus/internal/core/tenant",
		"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres",
	},
	"github.com/alex-bogatiuk/metapus/cmd/crypto-ingestion-monitor": {
		"github.com/alex-bogatiuk/metapus/internal/core/id", "github.com/alex-bogatiuk/metapus/internal/core/tenant",
		"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres", "github.com/alex-bogatiuk/metapus/internal/infrastructure/tenantdb",
		"github.com/alex-bogatiuk/metapus/internal/platform/secretfile", "github.com/alex-bogatiuk/metapus/internal/platform/telemetry",
		"github.com/alex-bogatiuk/metapus/pkg/logger",
	},
	"github.com/alex-bogatiuk/metapus/cmd/crypto-preflight": {
		"github.com/alex-bogatiuk/metapus/internal/core/json", "github.com/alex-bogatiuk/metapus/internal/core/tenant",
		"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres", "github.com/alex-bogatiuk/metapus/internal/infrastructure/tenantdb",
		"github.com/alex-bogatiuk/metapus/internal/platform/secretfile", "github.com/alex-bogatiuk/metapus/pkg/logger",
	},
	"github.com/alex-bogatiuk/metapus/cmd/crypto-record-wallet-funding": {
		"github.com/alex-bogatiuk/metapus/internal/core/json", "github.com/alex-bogatiuk/metapus/internal/core/tenant",
		"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres", "github.com/alex-bogatiuk/metapus/internal/infrastructure/tenantdb",
		"github.com/alex-bogatiuk/metapus/internal/platform/secretfile",
	},
	"github.com/alex-bogatiuk/metapus/cmd/http-contracts": {
		"github.com/alex-bogatiuk/metapus/internal/configuration", "github.com/alex-bogatiuk/metapus/internal/configuration/none",
	},
	"github.com/alex-bogatiuk/metapus/cmd/integration-config": {
		"github.com/alex-bogatiuk/metapus/internal/core/json", "github.com/alex-bogatiuk/metapus/internal/core/tenant",
		"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres", "github.com/alex-bogatiuk/metapus/internal/platform/secretfile",
	},
	"github.com/alex-bogatiuk/metapus/cmd/public-test-bootstrap": {
		"github.com/alex-bogatiuk/metapus/internal/core/id", "github.com/alex-bogatiuk/metapus/internal/core/json",
		"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres",
	},
}

// TestConfigurationCommandsUseThePublicLauncher keeps a configuration's
// commands buildable from its own module: besides its configuration's
// packages and the client extensions they take the platform only from
// pkg/sdk, so cmd/<name>-server is launch.Server(…), not internal/bootstrap.
func TestConfigurationCommandsUseThePublicLauncher(t *testing.T) {
	t.Parallel()
	allowed := func(value string) bool {
		if !strings.HasPrefix(value, module) || value == module+"extensions" ||
			value == module+"pkg/sdk" || strings.HasPrefix(value, module+"pkg/sdk/") {
			return true
		}
		for _, name := range sdkConfigurations {
			if configurationNamed(name).owns(value) {
				return true
			}
		}
		return false
	}
	actual := map[string]map[string]bool{}
	notConfigurationCommand := func(pkg string) bool { return !isConfigurationCommandPackage(pkg) }
	walkGo(t, repositoryRoot(t), []string{"cmd"}, false, notConfigurationCommand, func(rel string, file *ast.File) {
		pkg := module + filepath.ToSlash(filepath.Dir(rel))
		for _, imported := range file.Imports {
			value, err := strconv.Unquote(imported.Path.Value)
			require.NoError(t, err)
			if allowed(value) {
				continue
			}
			if actual[pkg] == nil {
				actual[pkg] = map[string]bool{}
			}
			actual[pkg][value] = true
		}
	})
	compareKnownLeaks(t, actual, knownConfigurationCommandPlatformImports,
		"configuration command %s imports platform package %s; take it from pkg/sdk (launch.Server, launch.Worker, launch.Tenant, launch.Seed for the mains)",
		"import %s -> %s is gone; remove it from knownConfigurationCommandPlatformImports")
}
