package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// sdkConfigurations are the configurations built on the public platform API
// (metapus/pkg/sdk) alone, as a configuration in its own repository has to
// be: their code and tests import no other platform package.
var sdkConfigurations = []string{"autoparts", "crypto"}

func TestSDKConfigurationsUseOnlyThePublicAPI(t *testing.T) {
	t.Parallel()
	repoRoot := repositoryRoot(t)
	for _, name := range sdkConfigurations {
		own := "metapus/internal/configuration/" + name
		err := filepath.WalkDir(filepath.Join(repoRoot, "internal", "configuration", name), func(path string, entry fs.DirEntry, walkErr error) error {
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
				if !strings.HasPrefix(value, "metapus/") || value == own || strings.HasPrefix(value, own+"/") ||
					value == "metapus/pkg/sdk" || strings.HasPrefix(value, "metapus/pkg/sdk/") {
					continue
				}
				rel, _ := filepath.Rel(repoRoot, path)
				t.Errorf("%s imports %s; a configuration on the public API uses metapus/pkg/sdk only (add what it needs there)", filepath.ToSlash(rel), value)
			}
			return nil
		})
		require.NoError(t, err)
	}
}

// sdkOwnTypes are the types pkg/sdk declares itself: test helpers that wrap
// the platform's test support.
var sdkOwnTypes = map[string]bool{"pkg/sdk/sdktest.Tenant": true}

// TestSDKIsAFacade keeps pkg/sdk a thin facade: it declares aliases and
// wrappers over the platform, never behaviour of its own that the platform
// does not run. Its packages declare no types of their own beyond generic
// constraints and sdkOwnTypes.
func TestSDKIsAFacade(t *testing.T) {
	t.Parallel()
	repoRoot := repositoryRoot(t)
	err := filepath.WalkDir(filepath.Join(repoRoot, "pkg", "sdk"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return walkErr
		}
		raw, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			for _, spec := range declaredTypes(decl) {
				if !spec.alias && !spec.constraint && !sdkOwnTypes[filepath.ToSlash(filepath.Dir(raw))+"."+spec.name] {
					t.Errorf("%s declares type %s; pkg/sdk exposes the platform's types as aliases", filepath.ToSlash(raw), spec.name)
				}
			}
		}
		return nil
	})
	require.NoError(t, err)
}

type declaredType struct {
	name              string
	alias, constraint bool
}

// declaredTypes lists the types decl declares; a constraint is an interface
// with a type term (*T), which an alias cannot replace for a generic type.
func declaredTypes(decl ast.Decl) []declaredType {
	gen, ok := decl.(*ast.GenDecl)
	if !ok || gen.Tok != token.TYPE {
		return nil
	}
	var result []declaredType
	for _, s := range gen.Specs {
		spec := s.(*ast.TypeSpec)
		typ := declaredType{name: spec.Name.Name, alias: spec.Assign.IsValid()}
		if iface, ok := spec.Type.(*ast.InterfaceType); ok {
			for _, method := range iface.Methods.List {
				if _, star := method.Type.(*ast.StarExpr); star {
					typ.constraint = true
				}
			}
		}
		result = append(result, typ)
	}
	return result
}

// TestSDKTestStaysInTests keeps pkg/sdk/sdktest out of a configuration's run
// time: its fixtures build transaction managers over raw pools and bind
// tenants by hand, which the platform does for configuration code. Tests and
// test-support packages (named *test) may use it.
func TestSDKTestStaysInTests(t *testing.T) {
	t.Parallel()
	repoRoot := repositoryRoot(t)
	for _, name := range sdkConfigurations {
		err := filepath.WalkDir(filepath.Join(repoRoot, "internal", "configuration", name), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return walkErr
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			if strings.HasSuffix(file.Name.Name, "test") {
				return nil
			}
			for _, imported := range file.Imports {
				if value, _ := strconv.Unquote(imported.Path.Value); value == "metapus/pkg/sdk/sdktest" {
					rel, _ := filepath.Rel(repoRoot, path)
					t.Errorf("%s imports %s outside tests", filepath.ToSlash(rel), value)
				}
			}
			return nil
		})
		require.NoError(t, err)
	}
}

// TestConfigurationsDoNotHoldTheEncryptionKey keeps the platform's
// credential key in the platform: a configuration seals and opens its
// credentials through pkg/sdk/secretbox and reads integration accounts
// through pkg/sdk/postgres/catalog_repo, so no production file of it names
// the key's secret. Tests may set it for their fixtures.
func TestConfigurationsDoNotHoldTheEncryptionKey(t *testing.T) {
	t.Parallel()
	repoRoot := repositoryRoot(t)
	for _, name := range sdkConfigurations {
		err := filepath.WalkDir(filepath.Join(repoRoot, "internal", "configuration", name), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return walkErr
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(node ast.Node) bool {
				literal, ok := node.(*ast.BasicLit)
				if ok && literal.Kind == token.STRING && strings.Contains(literal.Value, "AUTOMATION_ENCRYPTION_KEY") {
					rel, _ := filepath.Rel(repoRoot, path)
					t.Errorf("%s names AUTOMATION_ENCRYPTION_KEY; seal credentials with pkg/sdk/secretbox", filepath.ToSlash(rel))
				}
				return true
			})
			return nil
		})
		require.NoError(t, err)
	}
}

// TestConfigurationsDoNotQueryIntegrationTables keeps the platform's
// integration storage behind pkg/sdk/postgres/catalog_repo: a configuration
// asks it for providers, pool members and usable accounts and joins its own
// tables with the IDs it gets, so the platform may change how it stores
// accounts and pools. Migrations still reference the tables (foreign keys,
// the configuration's CHECKs) and tests still seed them.
func TestConfigurationsDoNotQueryIntegrationTables(t *testing.T) {
	t.Parallel()
	repoRoot := repositoryRoot(t)
	migrations, err := filepath.Glob(filepath.Join(repoRoot, "db", "migrations", "*.sql"))
	require.NoError(t, err)
	created := regexp.MustCompile(`CREATE TABLE ((?:cat|sys)_integration_[a-z_]+)`)
	var tables []string
	for _, path := range migrations {
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		for _, match := range created.FindAllStringSubmatch(string(raw), -1) {
			tables = append(tables, match[1])
		}
	}
	require.NotEmpty(t, tables, "platform migrations create no integration tables")
	named := regexp.MustCompile(`\b(` + strings.Join(tables, "|") + `)\b`)
	for _, name := range sdkConfigurations {
		err := filepath.WalkDir(filepath.Join(repoRoot, "internal", "configuration", name), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return walkErr
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(node ast.Node) bool {
				literal, ok := node.(*ast.BasicLit)
				if ok && literal.Kind == token.STRING && named.MatchString(literal.Value) {
					rel, _ := filepath.Rel(repoRoot, path)
					t.Errorf("%s names platform table %s; read integration accounts and pools through pkg/sdk/postgres/catalog_repo",
						filepath.ToSlash(rel), named.FindString(literal.Value))
				}
				return true
			})
			return nil
		})
		require.NoError(t, err)
	}
}
