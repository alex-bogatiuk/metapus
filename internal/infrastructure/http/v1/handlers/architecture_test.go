package handlers

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var sqlStatementPattern = regexp.MustCompile(`(?is)(\bSELECT\b.+\bFROM\b|\bINSERT\s+INTO\b|\bUPDATE\b.+\bSET\b|\bDELETE\s+FROM\b)`)

// Financial and crypto transport adapters must never grow their own database
// transaction boundary. SQL belongs to persistence adapters; orchestration and
// state transitions belong to application services.
func TestHTTPHandlersContainNoSQL(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read handlers directory: %v", err)
	}
	files := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(files, entry.Name(), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", entry.Name(), err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err == nil && sqlStatementPattern.MatchString(value) {
				position := files.Position(literal.Pos())
				t.Errorf("%s:%d contains SQL in an HTTP handler", entry.Name(), position.Line)
			}
			return true
		})
	}
}
