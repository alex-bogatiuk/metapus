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

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/content"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/configuration"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/contracts"
	"github.com/stretchr/testify/require"
)

// The generated frontend files describe the assembled application: the
// platform's files (with client extensions) and the configuration's files on
// top of them. Generating the platform layer alone also proves that no
// platform route depends on a configuration contract.
func TestHTTPArtifactsStayCurrent(t *testing.T) {
	artifacts, _, err := configuration.HTTPArtifacts(content.Crypto(content.Services{}), configuration.Extensions)
	require.NoError(t, err)
	require.Len(t, artifacts, 6)
	for _, artifact := range artifacts {
		committed, err := os.ReadFile(filepath.Join("../../../..", artifact.Path))
		require.NoError(t, err, artifact.Path)
		require.Equal(t, string(artifact.Data), string(bytes.ReplaceAll(committed, []byte("\r\n"), []byte("\n"))), "run go run ./cmd/http-contracts: %s", artifact.Path)
	}
}

func TestRegistryCoversOwnedJSONDTOs(t *testing.T) {
	bundles := configuration.HTTPContractBundles(content.Crypto(content.Services{}), configuration.Extensions)
	registered := map[string]bool{}
	for _, contract := range contracts.Registry(bundles...) {
		registered[contract.Name] = true
	}
	// These have no HTTP JSON body: two form/query binders and a mapper's internal bag.
	excluded := map[string]bool{"PaginationRequest": true, "BaseFilter": true, "DocRefsBag": true}
	files, err := filepath.Glob("../../../infrastructure/http/v1/dto/*.go")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	configurationDTOs, err := filepath.Glob("../infrastructure/http/cryptohttp/cryptodto/*.go")
	require.NoError(t, err)
	require.NotEmpty(t, configurationDTOs)
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
}
