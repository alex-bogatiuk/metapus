package metadata

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCustomFieldSnapshotIsIndependentAndReplacesRemovedFields(t *testing.T) {
	def := EntityDef{Name: "Counterparty", Key: "counterparty", Fields: []FieldDef{{Name: "name", Type: TypeString}}}
	fields := []FieldDef{{Name: "attributes.region", Label: "Регион", Type: TypeEnum, Options: []string{"A", "B"}}}
	def.MergeCustomFields(fields)
	fields[0].Options[0] = "changed"
	fields[0].Label = "changed"
	require.Equal(t, "Регион", def.CustomFields[0].Label)
	require.Equal(t, "A", def.CustomFields[0].Options[0])
	filters := def.ToFilterMeta(nil)
	require.Len(t, filters, 2)
	require.Equal(t, "attributes.region", filters[1].Key)
	require.Equal(t, "Additional attributes", filters[1].Group)
	def.MergeCustomFields(nil)
	require.Empty(t, def.CustomFields)
	require.Len(t, def.Fields, 1)
}
