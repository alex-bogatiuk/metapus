package crypto_worker

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestGenericOrchestrationDoesNotImportFamilyAdapters(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read crypto_worker package: %v", err)
	}
	targets := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() && filepath.Ext(name) == ".go" && !strings.HasSuffix(name, "_test.go") {
			targets = append(targets, name)
		}
	}
	forbidden := []string{
		"metapus/internal/infrastructure/blockchain/tron",
		"metapus/internal/infrastructure/blockchain/evm",
		"metapus/internal/infrastructure/blockchain/ton",
		"metapus/internal/infrastructure/blockchain/solana",
		"metapus/internal/infrastructure/crypto/resources",
	}
	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			path := filepath.Clean(target)
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			for _, imported := range file.Imports {
				value, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					t.Fatalf("unquote import in %s: %v", path, err)
				}
				for _, prefix := range forbidden {
					if value == prefix || strings.HasPrefix(value, prefix+"/") {
						position := fileSetPosition(path, imported)
						t.Fatalf("generic orchestration imports family adapter %s at %s", value, position)
					}
				}
			}
		})
	}
}

func fileSetPosition(path string, node ast.Node) string {
	return path + ":" + strconv.Itoa(int(node.Pos()))
}
