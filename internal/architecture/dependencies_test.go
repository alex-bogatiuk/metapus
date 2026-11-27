package architecture_test

import (
	"go/parser"
	"go/token"
	"os"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInternalLayerDependenciesPointInward(t *testing.T) {
	t.Parallel()
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(currentFile)))
	// directory is from the repository root.
	rules := []struct {
		directory string
		forbidden []string
	}{
		{directory: "internal/core", forbidden: []string{"github.com/alex-bogatiuk/metapus/internal/application/", "github.com/alex-bogatiuk/metapus/internal/domain/", "github.com/alex-bogatiuk/metapus/internal/infrastructure/", "github.com/jackc/pgx", "github.com/georgysavva/scany/v2/pgxscan"}},
		{directory: "internal/metadata", forbidden: []string{"github.com/alex-bogatiuk/metapus/internal/infrastructure/"}},
		{directory: "internal/domain", forbidden: []string{"github.com/alex-bogatiuk/metapus/internal/application/", "github.com/alex-bogatiuk/metapus/internal/infrastructure/"}},
		{directory: "internal/application", forbidden: []string{"github.com/alex-bogatiuk/metapus/internal/infrastructure/"}},
	}
	// A configuration keeps the same layers inside its own root; an add-on's
	// domain does not reach its base's application or infrastructure either.
	for _, c := range repositoryConfigurations {
		roots := []string{c.pkg()}
		if c.base != "" {
			roots = append(roots, configurationNamed(c.base).pkg())
		}
		var domain, application []string
		for _, root := range roots {
			domain = append(domain, root+"/application/", root+"/infrastructure/")
			application = append(application, root+"/infrastructure/")
		}
		rules = append(rules,
			struct {
				directory string
				forbidden []string
			}{c.dir + "/domain", append([]string{"github.com/alex-bogatiuk/metapus/internal/application/", "github.com/alex-bogatiuk/metapus/internal/infrastructure/"}, domain...)},
			struct {
				directory string
				forbidden []string
			}{c.dir + "/application", append([]string{"github.com/alex-bogatiuk/metapus/internal/infrastructure/"}, application...)})
	}

	// A configuration reaches the platform through pkg/sdk: importing a
	// facade package imports what it fronts.
	fronts := sdkFronts(t, repoRoot)

	for _, rule := range rules {
		t.Run(rule.directory, func(t *testing.T) {
			dir := filepath.Join(repoRoot, filepath.FromSlash(rule.directory))
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				return // a configuration without the layer
			}
			err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.IsDir() || filepath.Ext(path) != ".go" {
					return nil
				}
				file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
				if err != nil {
					return err
				}
				for _, imported := range file.Imports {
					pathValue, err := strconv.Unquote(imported.Path.Value)
					if err != nil {
						return err
					}
					for _, reached := range append([]string{pathValue}, fronts[pathValue]...) {
						for _, prefix := range rule.forbidden {
							if !strings.HasPrefix(reached, prefix) {
								continue
							}
							if reached == pathValue {
								t.Errorf("%s imports outward layer %s", path, pathValue)
							} else {
								t.Errorf("%s imports outward layer %s through %s", path, reached, pathValue)
							}
						}
					}
				}
				return nil
			})
			require.NoError(t, err)
		})
	}
}

// sdkFronts maps each pkg/sdk package to the platform packages it fronts:
// the internal packages it imports, directly or through other facade
// packages.
func sdkFronts(t *testing.T, repoRoot string) map[string][]string {
	t.Helper()
	direct := map[string][]string{}
	err := filepath.WalkDir(filepath.Join(repoRoot, "pkg", "sdk"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return walkErr
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(repoRoot, filepath.Dir(path))
		if err != nil {
			return err
		}
		key := "github.com/alex-bogatiuk/metapus/" + filepath.ToSlash(rel)
		for _, imported := range file.Imports {
			value, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			if strings.HasPrefix(value, "github.com/alex-bogatiuk/metapus/") {
				direct[key] = append(direct[key], value)
			}
		}
		return nil
	})
	require.NoError(t, err)
	fronts := map[string][]string{}
	for pkg := range direct {
		seen := map[string]bool{}
		var visit func(string)
		visit = func(current string) {
			for _, dep := range direct[current] {
				if seen[dep] {
					continue
				}
				seen[dep] = true
				if _, facade := direct[dep]; facade {
					visit(dep)
				} else {
					fronts[pkg] = append(fronts[pkg], dep)
				}
			}
		}
		visit(pkg)
	}
	return fronts
}

// Shared contracts must not smuggle framework dependencies into inner packages.
// Inspect the complete local import closure, not just the first import edge.
func TestInnerPackagesDoNotTransitivelyDependOnAdapters(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	root := filepath.Dir(filepath.Dir(file))
	graph := map[string][]string{}
	// The graph covers pkg/sdk too: a configuration reaches the platform's
	// packages through its facade.
	for start, deps := range sdkFronts(t, filepath.Dir(root)) {
		graph[start] = deps
	}
	repoRoot := filepath.Dir(root)
	for _, top := range []string{"internal", "configurations"} {
		err := filepath.WalkDir(filepath.Join(repoRoot, top), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(repoRoot, filepath.Dir(path))
			if err != nil {
				return err
			}
			key := module + filepath.ToSlash(rel)
			for _, imp := range parsed.Imports {
				value, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					return err
				}
				graph[key] = append(graph[key], value)
			}
			return nil
		})
		require.NoError(t, err)
	}
	inner := []string{module + "internal/core", module + "internal/domain", module + "internal/application"}
	for _, c := range repositoryConfigurations {
		inner = append(inner, c.pkg()+"/domain", c.pkg()+"/application")
	}
	for start := range graph {
		isInner := false
		for _, prefix := range inner {
			isInner = isInner || start == prefix || strings.HasPrefix(start, prefix+"/")
		}
		if !isInner {
			continue
		}
		seen := map[string]bool{}
		var visit func(string, []string)
		visit = func(current string, route []string) {
			if seen[current] {
				return
			}
			seen[current] = true
			route = append(append([]string(nil), route...), current)
			if strings.HasPrefix(current, "github.com/golang-jwt/jwt/") || current == "github.com/skip2/go-qrcode" || current == "github.com/gin-gonic/gin" || current == "github.com/Masterminds/squirrel" || strings.HasPrefix(current, "github.com/jackc/pgx") || strings.HasPrefix(current, "github.com/xuri/excelize") || strings.HasPrefix(current, "github.com/gomutex/godocx") {
				t.Errorf("outward dependency: %s", strings.Join(route, " -> "))
				return
			}
			for _, dep := range graph[current] {
				visit(dep, route)
			}
		}
		visit(start, nil)
	}
}

func TestEncodingJSONV1IsIsolatedToCompatibilityFacades(t *testing.T) {
	t.Parallel()
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(currentFile)))
	allowedDirectories := []string{
		filepath.Join(repoRoot, "internal", "core", "json"),
		filepath.Join(repoRoot, "vault-sign-plugins", "evm", "internal", "json"),
		filepath.Join(repoRoot, "vault-sign-plugins", "tron", "internal", "json"),
		// The merchant SDK is an independent public Go module and cannot import
		// Metapus internal compatibility packages.
		filepath.Join(repoRoot, "sdk", "go"),
	}

	err := filepath.WalkDir(repoRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			// Local database snapshots can contain complete historical source trees.
			// Check only the working sources, regardless of retained backups.
			if path == filepath.Join(repoRoot, "db", "backup") {
				return filepath.SkipDir
			}
			switch entry.Name() {
			// .claude holds local agent worktrees: whole checkouts of other branches.
			case ".git", "node_modules", "vendor", ".claude":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range file.Imports {
			pathValue, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			if pathValue != "encoding/json" {
				continue
			}
			for _, allowed := range allowedDirectories {
				if path == allowed || strings.HasPrefix(path, allowed+string(filepath.Separator)) {
					return nil
				}
			}
			t.Errorf("%s imports encoding/json outside a compatibility facade", path)
		}
		return nil
	})
	require.NoError(t, err)
}
