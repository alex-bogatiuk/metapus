package architecture_test

import (
	"go/parser"
	"go/token"
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
	internalRoot := filepath.Dir(filepath.Dir(currentFile))
	rules := []struct {
		directory string
		forbidden []string
	}{
		{directory: "core", forbidden: []string{"metapus/internal/application/", "metapus/internal/domain/", "metapus/internal/infrastructure/", "github.com/jackc/pgx", "github.com/georgysavva/scany/v2/pgxscan"}},
		{directory: "metadata", forbidden: []string{"metapus/internal/infrastructure/"}},
		{directory: "domain", forbidden: []string{"metapus/internal/application/", "metapus/internal/infrastructure/"}},
		{directory: "application", forbidden: []string{"metapus/internal/infrastructure/"}},
		// A configuration keeps the same layers inside its own root.
		{directory: "configuration/crypto/domain", forbidden: []string{"metapus/internal/application/", "metapus/internal/infrastructure/", "metapus/internal/configuration/crypto/application/", "metapus/internal/configuration/crypto/infrastructure/"}},
		{directory: "configuration/crypto/application", forbidden: []string{"metapus/internal/infrastructure/", "metapus/internal/configuration/crypto/infrastructure/"}},
		{directory: "configuration/autoparts/domain", forbidden: []string{"metapus/internal/application/", "metapus/internal/infrastructure/", "metapus/internal/configuration/autoparts/application/", "metapus/internal/configuration/autoparts/infrastructure/"}},
	}

	// A configuration reaches the platform through pkg/sdk: importing a
	// facade package imports what it fronts.
	fronts := sdkFronts(t, filepath.Dir(internalRoot))

	for _, rule := range rules {
		t.Run(rule.directory, func(t *testing.T) {
			err := filepath.WalkDir(filepath.Join(internalRoot, rule.directory), func(path string, entry fs.DirEntry, walkErr error) error {
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
		key := "metapus/" + filepath.ToSlash(rel)
		for _, imported := range file.Imports {
			value, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			if strings.HasPrefix(value, "metapus/") {
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
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
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
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		key := "metapus/internal/" + filepath.ToSlash(rel)
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
	for start := range graph {
		inner := false
		for _, layer := range []string{"core", "domain", "application", "configuration/crypto/domain", "configuration/crypto/application", "configuration/autoparts/domain"} {
			prefix := "metapus/internal/" + layer
			inner = inner || start == prefix || strings.HasPrefix(start, prefix+"/")
		}
		if !inner {
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
