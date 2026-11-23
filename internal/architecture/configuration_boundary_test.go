package architecture_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// configurationPackages lists the import paths (and their subpackages) that
// belong to an industry configuration rather than to the reusable platform:
// each configuration lives under its own root in internal/configuration/.
// Everything else under internal/ and pkg/ is the platform, including the
// contract package itself, the empty configuration (none) and
// internal/platformcontent with the shared applied catalogs.
var configurationPackages = []string{
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts",
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto",
}

// knownConfigurationLeaks lists tolerated platform → configuration imports.
// It started with 105 edges and is now empty: the platform depends on no
// configuration package. Keep it empty; invert a new dependency through a
// platform extension point instead of listing it here.
var knownConfigurationLeaks = map[string][]string{}

func TestPlatformDoesNotDependOnConfiguration(t *testing.T) {
	t.Parallel()
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(currentFile)))

	for _, pkg := range configurationPackages {
		dir := filepath.Join(repoRoot, filepath.FromSlash(strings.TrimPrefix(pkg, "github.com/alex-bogatiuk/metapus/")))
		info, err := os.Stat(dir)
		require.NoErrorf(t, err, "configuration package %s no longer exists; update configurationPackages", pkg)
		require.Truef(t, info.IsDir(), "configuration package %s is not a directory", pkg)
	}

	actual := map[string]map[string]bool{}
	for _, top := range []string{"internal", "pkg"} {
		err := filepath.WalkDir(filepath.Join(repoRoot, top), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, err := filepath.Rel(repoRoot, filepath.Dir(path))
			if err != nil {
				return err
			}
			pkg := "github.com/alex-bogatiuk/metapus/" + filepath.ToSlash(rel)
			if isConfigurationPackage(pkg) {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, imported := range file.Imports {
				value, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					return err
				}
				if !isConfigurationPackage(value) {
					continue
				}
				if actual[pkg] == nil {
					actual[pkg] = map[string]bool{}
				}
				actual[pkg][value] = true
			}
			return nil
		})
		require.NoError(t, err)
	}

	for pkg, imports := range actual {
		for imported := range imports {
			if !slices.Contains(knownConfigurationLeaks[pkg], imported) {
				t.Errorf("platform package %s imports configuration package %s; "+
					"move the dependency into the configuration or invert it through a platform interface", pkg, imported)
			}
		}
	}
	for pkg, allowed := range knownConfigurationLeaks {
		for _, imported := range allowed {
			if !actual[pkg][imported] {
				t.Errorf("leak %s -> %s is gone; remove it from knownConfigurationLeaks", pkg, imported)
			}
		}
	}
}

func isConfigurationPackage(importPath string) bool {
	for _, pkg := range configurationPackages {
		if importPath == pkg || strings.HasPrefix(importPath, pkg+"/") {
			return true
		}
	}
	return false
}

// TestConfigurationsDoNotDependOnEachOther keeps each configuration buildable
// on the platform alone: one configuration's code and tests never import
// another's.
func TestConfigurationsDoNotDependOnEachOther(t *testing.T) {
	t.Parallel()
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(currentFile)))
	owner := func(pkg string) string {
		for _, root := range configurationPackages {
			if pkg == root || strings.HasPrefix(pkg, root+"/") {
				return root
			}
		}
		return ""
	}
	for _, root := range configurationPackages {
		dir := filepath.Join(repoRoot, filepath.FromSlash(strings.TrimPrefix(root, "github.com/alex-bogatiuk/metapus/")))
		err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() || filepath.Ext(path) != ".go" {
				return walkErr
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, imported := range file.Imports {
				value, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					return err
				}
				if other := owner(value); other != "" && other != root {
					rel, _ := filepath.Rel(repoRoot, path)
					t.Errorf("%s imports %s of another configuration", filepath.ToSlash(rel), value)
				}
			}
			return nil
		})
		require.NoError(t, err)
	}
}
