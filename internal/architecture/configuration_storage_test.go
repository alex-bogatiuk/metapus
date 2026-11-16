package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// configurationMigrations declare the crypto-processing configuration's schema.
var configurationMigrations = []string{
	"00006_crypto_core.sql",
	"00007_crypto_outbound.sql",
	"00008_crypto_aml.sql",
	"00009_crypto_custody.sql",
}

// platformTablesInConfigurationMigrations are platform tables (the exchange
// rate subsystem) still declared in a configuration migration. They belong in
// a platform migration; until they move there they are not configuration tables.
var platformTablesInConfigurationMigrations = []string{
	"cat_rate_sources",
	"reg_exchange_rates",
	"reg_rate_source_mappings",
	"sys_rate_feed_state",
}

// knownConfigurationTableLeaks lists platform files whose string literals name
// configuration tables. It is empty: query configuration tables from a
// configuration package, or invert the dependency (integrations.PoolConsumer),
// instead of listing a file here.
var knownConfigurationTableLeaks = map[string][]string{}

// knownConfigurationTestLeaks lists platform packages whose tests still import
// configuration packages. It only shrinks; test configuration behaviour from
// the configuration side instead of listing a new package here.
var knownConfigurationTestLeaks = map[string][]string{
	// Generated contract files are checked against the full configuration.
	"metapus/internal/infrastructure/http/contracts": {
		"metapus/internal/content",
		"metapus/internal/domain/crypto",
		"metapus/internal/infrastructure/http/cryptohttp/cryptodto",
		"metapus/internal/infrastructure/http/cryptohttp/cryptohandlers",
	},
	// Report security runs against the configuration's datasets.
	"metapus/internal/infrastructure/reporting/compiler": {"metapus/internal/content"},
	// Valuation snapshot consistency shares the exchange rate fixture.
	"metapus/internal/infrastructure/storage/postgres/register_repo": {
		"metapus/internal/application/cryptoapp",
		"metapus/internal/domain/crypto",
		"metapus/internal/infrastructure/storage/postgres/portal_repo",
	},
}

var createRelationPattern = regexp.MustCompile(`(?i)\bCREATE\s+(?:OR\s+REPLACE\s+)?(?:UNLOGGED\s+)?(?:TABLE|(?:MATERIALIZED\s+)?VIEW)\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-z_][a-z0-9_]*)`)

func TestPlatformCodeDoesNotQueryConfigurationTables(t *testing.T) {
	t.Parallel()
	repoRoot := repositoryRoot(t)

	tables := map[string]bool{}
	for _, name := range configurationMigrations {
		raw, err := os.ReadFile(filepath.Join(repoRoot, "db", "migrations", name))
		require.NoErrorf(t, err, "configuration migration %s no longer exists; update configurationMigrations", name)
		for _, match := range createRelationPattern.FindAllStringSubmatch(string(raw), -1) {
			tables[strings.ToLower(match[1])] = true
		}
	}
	for _, table := range platformTablesInConfigurationMigrations {
		require.Truef(t, tables[table], "%s left the configuration migrations; remove it from platformTablesInConfigurationMigrations", table)
		delete(tables, table)
	}
	names := make([]string, 0, len(tables))
	for table := range tables {
		names = append(names, regexp.QuoteMeta(table))
	}
	slices.SortFunc(names, func(a, b string) int { return len(b) - len(a) })
	tablePattern := regexp.MustCompile(`\b(` + strings.Join(names, "|") + `)\b`)

	actual := map[string]map[string]bool{}
	walkPlatformGo(t, repoRoot, false, func(rel string, file *ast.File) {
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err != nil {
				return true
			}
			for _, table := range tablePattern.FindAllString(value, -1) {
				if actual[rel] == nil {
					actual[rel] = map[string]bool{}
				}
				actual[rel][table] = true
			}
			return true
		})
	})
	compareKnownLeaks(t, actual, knownConfigurationTableLeaks,
		"platform file %s names configuration table %s; move the query into a configuration package",
		"leak %s -> %s is gone; remove it from knownConfigurationTableLeaks")
}

func TestPlatformTestsDoNotImportConfiguration(t *testing.T) {
	t.Parallel()
	repoRoot := repositoryRoot(t)
	actual := map[string]map[string]bool{}
	walkPlatformGo(t, repoRoot, true, func(rel string, file *ast.File) {
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
	compareKnownLeaks(t, actual, knownConfigurationTestLeaks,
		"tests of platform package %s import configuration package %s; move the test to the configuration side",
		"leak %s -> %s is gone; remove it from knownConfigurationTestLeaks")
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Dir(filepath.Dir(filepath.Dir(currentFile)))
}

// walkPlatformGo parses the platform's Go files under internal/ and pkg/:
// test files when tests is true, production files otherwise.
func walkPlatformGo(t *testing.T, repoRoot string, tests bool, visit func(rel string, file *ast.File)) {
	t.Helper()
	for _, top := range []string{"internal", "pkg"} {
		err := filepath.WalkDir(filepath.Join(repoRoot, top), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") != tests {
				return nil
			}
			rel, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return err
			}
			if isConfigurationPackage("metapus/" + filepath.ToSlash(filepath.Dir(rel))) {
				return nil
			}
			mode := parser.SkipObjectResolution
			if tests {
				mode = parser.ImportsOnly
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, mode)
			if err != nil {
				return err
			}
			visit(filepath.ToSlash(rel), file)
			return nil
		})
		require.NoError(t, err)
	}
}

func compareKnownLeaks(t *testing.T, actual map[string]map[string]bool, known map[string][]string, newLeak, goneLeak string) {
	t.Helper()
	for owner, targets := range actual {
		for target := range targets {
			if !slices.Contains(known[owner], target) {
				t.Errorf(newLeak, owner, target)
			}
		}
	}
	for owner, targets := range known {
		for _, target := range targets {
			if !actual[owner][target] {
				t.Errorf(goneLeak, owner, target)
			}
		}
	}
}
