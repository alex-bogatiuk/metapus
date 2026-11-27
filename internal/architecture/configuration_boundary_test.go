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

// configurationPackages are the roots of the repository's configurations
// (repositoryConfigurations). Everything else under internal/ and pkg/ is the
// platform, including the contract package itself, the empty configuration
// (none) and internal/platformcontent with the shared applied catalogs.
var configurationPackages = func() []string {
	roots := make([]string, len(repositoryConfigurations))
	for i, c := range repositoryConfigurations {
		roots[i] = c.pkg()
	}
	return roots
}()

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

// TestConfigurationsDoNotDependOnEachOther keeps each base configuration
// buildable on the platform alone and each add-on on its base: a
// configuration's code and tests never import another configuration, except
// an add-on its own base. The base never imports its add-on.
func TestConfigurationsDoNotDependOnEachOther(t *testing.T) {
	t.Parallel()
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(currentFile)))
	for _, c := range repositoryConfigurations {
		err := filepath.WalkDir(filepath.Join(repoRoot, filepath.FromSlash(c.dir)), func(path string, entry fs.DirEntry, walkErr error) error {
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
				other, ok := configurationOwning(value)
				if !ok || other.name == c.name || other.name == c.base {
					continue
				}
				rel, _ := filepath.Rel(repoRoot, path)
				t.Errorf("%s imports %s of configuration %s; only an add-on imports its own base", filepath.ToSlash(rel), value, other.name)
			}
			return nil
		})
		require.NoError(t, err)
	}
}

// TestRepositoryConfigurationsAreBasesAndAddOns checks the declared stack:
// an add-on names a base of the repository that is no add-on itself.
func TestRepositoryConfigurationsAreBasesAndAddOns(t *testing.T) {
	t.Parallel()
	for _, c := range repositoryConfigurations {
		if c.base == "" {
			continue
		}
		base := configurationNamed(c.base)
		require.Emptyf(t, base.base, "add-on %s is built on %s, an add-on itself", c.name, base.name)
	}
}
