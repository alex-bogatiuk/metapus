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

// knownConfigurationTableLeaks lists platform files whose string literals name
// configuration tables. It is empty: query configuration tables from a
// configuration package, or invert the dependency (integrations.PoolConsumer),
// instead of listing a file here.
var knownConfigurationTableLeaks = map[string][]string{}

// knownConfigurationTestLeaks lists platform packages whose tests import
// configuration packages. It is empty: a test of the assembled application
// (all contracts, the configuration's datasets) lives in internal/content, and
// a test of configuration behaviour in the configuration's own package.
var knownConfigurationTestLeaks = map[string][]string{}

var createRelationPattern = regexp.MustCompile(`(?i)\bCREATE\s+(?:OR\s+REPLACE\s+)?(?:UNLOGGED\s+)?(?:TABLE|(?:MATERIALIZED\s+)?VIEW)\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-z_][a-z0-9_]*)`)

func TestPlatformCodeDoesNotQueryConfigurationTables(t *testing.T) {
	t.Parallel()
	repoRoot := repositoryRoot(t)
	tablePattern := configurationTablePattern(t, repoRoot)

	actual := map[string]map[string]bool{}
	walkPlatformGo(t, repoRoot, false, func(rel string, file *ast.File) {
		collectConfigurationTables(actual, tablePattern, rel, file)
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

// configurationTablePattern matches the name of any table or view that a
// configuration migration (db/configuration/<name>/) creates.
func configurationTablePattern(t *testing.T, repoRoot string) *regexp.Regexp {
	t.Helper()
	migrations, err := filepath.Glob(filepath.Join(repoRoot, "db", "configuration", "*", "*.sql"))
	require.NoError(t, err)
	require.NotEmpty(t, migrations)
	tables := map[string]bool{}
	for _, path := range migrations {
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		for _, match := range createRelationPattern.FindAllStringSubmatch(string(raw), -1) {
			tables[strings.ToLower(match[1])] = true
		}
	}
	names := make([]string, 0, len(tables))
	for table := range tables {
		names = append(names, regexp.QuoteMeta(table))
	}
	slices.SortFunc(names, func(a, b string) int { return len(b) - len(a) })
	return regexp.MustCompile(`\b(` + strings.Join(names, "|") + `)\b`)
}

// collectConfigurationTables records, under the file's path rel, every
// configuration table that a string literal of file names.
func collectConfigurationTables(actual map[string]map[string]bool, tablePattern *regexp.Regexp, rel string, file *ast.File) {
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
}

// walkPlatformGo parses the platform's Go files under internal/ and pkg/:
// test files when tests is true, production files otherwise.
func walkPlatformGo(t *testing.T, repoRoot string, tests bool, visit func(rel string, file *ast.File)) {
	t.Helper()
	walkGo(t, repoRoot, []string{"internal", "pkg"}, tests, isConfigurationPackage, visit)
}

// walkGo parses the Go files under the given top-level directories, skipping
// the packages for which skip reports true: test files when tests is true,
// production files otherwise.
func walkGo(t *testing.T, repoRoot string, tops []string, tests bool, skip func(pkg string) bool, visit func(rel string, file *ast.File)) {
	t.Helper()
	for _, top := range tops {
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
			if skip("metapus/" + filepath.ToSlash(filepath.Dir(rel))) {
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
