package content_test

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/extensions"
	"metapus/internal/content"
	"metapus/internal/infrastructure/http/contracts"
)

// The generated frontend files describe the assembled application: the
// platform with every configuration and extension.

func TestRouteContractsOfTheApplicationStayCurrent(t *testing.T) {
	bundles := content.HTTPContractBundles(extensions.Register)
	generated, err := contracts.GenerateRoutes(contracts.Registry(bundles...), contracts.Routes(bundles...))
	require.NoError(t, err)
	committed, err := os.ReadFile("../../frontend/lib/generated/http-contract-routes.ts")
	require.NoError(t, err)
	require.Equal(t, string(generated), string(bytes.ReplaceAll(committed, []byte("\r\n"), []byte("\n"))), "run go run ./cmd/http-contracts")
}

func TestRegistryCoversOwnedJSONDTOsAndGeneratedFileIsCurrent(t *testing.T) {
	bundles := content.HTTPContractBundles(extensions.Register)
	registered := map[string]bool{}
	for _, contract := range contracts.Registry(bundles...) {
		registered[contract.Name] = true
	}
	// These have no HTTP JSON body: two form/query binders and a mapper's internal bag.
	excluded := map[string]bool{"PaginationRequest": true, "BaseFilter": true, "DocRefsBag": true}
	files, err := filepath.Glob("../infrastructure/http/v1/dto/*.go")
	require.NoError(t, err)
	configurationDTOs, err := filepath.Glob("../infrastructure/http/cryptohttp/cryptodto/*.go")
	require.NoError(t, err)
	files = append(files, configurationDTOs...)
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		source, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		require.NoError(t, err)
		for _, declaration := range source.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, spec := range general.Specs {
				typeSpec := spec.(*ast.TypeSpec)
				if !typeSpec.Name.IsExported() || typeSpec.TypeParams != nil || excluded[typeSpec.Name.Name] {
					continue
				}
				require.True(t, registered[typeSpec.Name.Name], "%s must be registered as an HTTP contract", typeSpec.Name)
			}
		}
	}
	generated, err := contracts.Generate(contracts.Registry(bundles...))
	require.NoError(t, err)
	committed, err := os.ReadFile("../../frontend/types/generated/http-contracts.ts")
	require.NoError(t, err)
	require.Equal(t, string(generated), string(bytes.ReplaceAll(committed, []byte("\r\n"), []byte("\n"))), "run go run ./cmd/http-contracts")
	schemas, err := contracts.GenerateSchemas(contracts.Registry(bundles...))
	require.NoError(t, err)
	committed, err = os.ReadFile("../../frontend/lib/generated/http-contract-schemas.ts")
	require.NoError(t, err)
	require.Equal(t, string(schemas), string(bytes.ReplaceAll(committed, []byte("\r\n"), []byte("\n"))), "run go run ./cmd/http-contracts")
}
