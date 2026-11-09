package openapi_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestMerchantV1OpenAPIIsParseableAndAllLocalReferencesResolve(t *testing.T) {
	raw, err := os.ReadFile("merchant-v1.yaml")
	require.NoError(t, err)

	var document map[string]any
	require.NoError(t, yaml.Unmarshal(raw, &document))
	require.Equal(t, "3.1.0", document["openapi"])

	paths := document["paths"].(map[string]any)
	require.Contains(t, paths, "/invoices")
	require.Contains(t, paths, "/webhook-endpoints")
	require.Contains(t, paths, "/events/{id}/redeliver")

	var visit func(any)
	visit = func(value any) {
		switch node := value.(type) {
		case map[string]any:
			if ref, ok := node["$ref"].(string); ok {
				require.True(t, strings.HasPrefix(ref, "#/"), "only local refs are allowed: %s", ref)
				current := any(document)
				for _, segment := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
					mapping, ok := current.(map[string]any)
					require.True(t, ok, "invalid ref path %s", ref)
					current, ok = mapping[segment]
					require.True(t, ok, "unresolved ref %s", ref)
				}
			}
			for _, child := range node {
				visit(child)
			}
		case []any:
			for _, child := range node {
				visit(child)
			}
		}
	}
	visit(document)
}

func TestMerchantV1PublicJSONPropertiesAreCamelCase(t *testing.T) {
	raw, err := os.ReadFile("merchant-v1.yaml")
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, yaml.Unmarshal(raw, &document))

	schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
	for schemaName, rawSchema := range schemas {
		schema, ok := rawSchema.(map[string]any)
		if !ok {
			continue
		}
		properties, _ := schema["properties"].(map[string]any)
		for property := range properties {
			require.NotContains(t, property, "_", "%s.%s must use camelCase", schemaName, property)
		}
	}
}
