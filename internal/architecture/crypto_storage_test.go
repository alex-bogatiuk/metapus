package architecture_test

import (
	"go/ast"
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

func TestCryptoWorkerRuntimeDoesNotExecuteSQL(t *testing.T) {
	t.Parallel()
	_, current, _, ok := runtime.Caller(0)
	require.True(t, ok)
	root := filepath.Join(filepath.Dir(filepath.Dir(current)), "infrastructure", "crypto_worker")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, imp := range file.Imports {
			value, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			if value == "database/sql" || strings.HasPrefix(value, "github.com/jackc/pgx/") {
				t.Errorf("%s imports a SQL driver in worker runtime: %s", path, value)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch selector.Sel.Name {
			case "Query", "QueryRow", "Exec", "GetQuerier":
				t.Errorf("%s: SQL execution belongs to a PostgreSQL adapter", fset.Position(call.Pos()))
			}
			return true
		})
		return nil
	})
	require.NoError(t, err)
}

func TestPostgresAdaptersDoNotDependOnCryptoWorker(t *testing.T) {
	t.Parallel()
	_, current, _, ok := runtime.Caller(0)
	require.True(t, ok)
	root := filepath.Join(filepath.Dir(filepath.Dir(current)), "infrastructure", "storage", "postgres")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range file.Imports {
			value, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			if value == "metapus/internal/infrastructure/crypto_worker" || strings.HasPrefix(value, "metapus/internal/infrastructure/crypto_worker/") {
				t.Errorf("%s depends on crypto worker runtime", path)
			}
			if strings.Contains(filepath.ToSlash(path), "/outbound_repo/") &&
				(strings.HasPrefix(value, "go.opentelemetry.io/") || strings.HasPrefix(value, "metapus/internal/infrastructure/blockchain/") || strings.HasPrefix(value, "metapus/internal/infrastructure/crypto/")) {
				t.Errorf("%s imports a runtime adapter %s in persistence", path, value)
			}
		}
		return nil
	})
	require.NoError(t, err)
}
