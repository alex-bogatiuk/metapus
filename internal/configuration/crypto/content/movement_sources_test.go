package content

import (
	"metapus/pkg/sdk/configuration"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/pkg/sdk/http/v1"
	"metapus/pkg/sdk/metadata"
)

// Every declared movement source reaches the read registry and names fields
// the document really has; a typo would silently hide the column.
func TestMovementSourcesNameRealDocumentFields(t *testing.T) {
	factories := configuration.Compose(Crypto(Services{}))
	registry := v1.BuildReadRegistry(factories)

	declared := map[string]bool{}
	for _, def := range registry.List() {
		if def.MovementSources == nil {
			continue
		}
		declared[def.Key] = true
		for column, paths := range def.MovementSources {
			for _, path := range paths {
				if !hasSourceField(def, path) {
					t.Errorf("%s.%s names unknown source field %q", def.Key, column, path)
				}
			}
		}
	}
	for key := range movementSources {
		require.Truef(t, declared[key], "movement sources of %s do not reach its read definition", key)
	}
	require.Len(t, declared, len(movementSources))
}

func hasSourceField(def metadata.EntityDef, path string) bool {
	// Field policies name a column by its db tag, else by its JSON name.
	policyName := func(field metadata.FieldDef) string {
		if field.DBColumn != "" {
			return field.DBColumn
		}
		return field.Name
	}
	byPolicyName := func(fields []metadata.FieldDef, name string) bool {
		return slices.ContainsFunc(fields, func(field metadata.FieldDef) bool { return policyName(field) == name })
	}
	part, field, nested := strings.Cut(path, ".")
	if !nested {
		return byPolicyName(def.Fields, path)
	}
	for _, tablePart := range def.TableParts {
		if tablePart.Name == part {
			return byPolicyName(tablePart.Columns, field)
		}
	}
	return false
}
