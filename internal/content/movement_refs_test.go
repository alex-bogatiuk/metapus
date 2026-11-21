package content

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"metapus/internal/configuration"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	v1 "metapus/internal/infrastructure/http/v1"
)

// Every "ref" movement column of a register names an entity of the read
// registry; otherwise readers would drop the reference.
func TestRegisterRefColumnsNameRegisteredEntities(t *testing.T) {
	factories := configuration.Compose(Crypto(Services{}))
	registry := v1.BuildReadRegistry(factories)
	known := map[string]bool{}
	for _, def := range registry.List() {
		known[def.Name] = true
	}

	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	root := filepath.Join(filepath.Dir(file), "..", "domain", "registers")
	columns := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return walkErr
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			fields := map[string]string{}
			for _, element := range literal.Elts {
				pair, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				name, nameOK := pair.Key.(*ast.Ident)
				value, valueOK := pair.Value.(*ast.BasicLit)
				if nameOK && valueOK && value.Kind == token.STRING {
					fields[name.Name], _ = strconv.Unquote(value.Value)
				}
			}
			if fields["Type"] != "ref" {
				return true
			}
			columns++
			rel, _ := filepath.Rel(root, path)
			require.NotEmptyf(t, fields["RefEntity"], "%s: ref column %q declares no RefEntity", rel, fields["Key"])
			require.Truef(t, known[fields["RefEntity"]], "%s: ref column %q names unknown entity %q", rel, fields["Key"], fields["RefEntity"])
			return true
		})
		return nil
	})
	require.NoError(t, err)
	require.NotZero(t, columns)
}
