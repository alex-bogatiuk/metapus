package entity

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestEntityDefinitionRejectsInvalidIdentity(t *testing.T) {
	base := Definition{Kind: CatalogKind, Key: "vehicle", Name: "Vehicle", Route: "vehicles", Table: "cat_vehicles"}
	require.NoError(t, base.Validate())
	for _, change := range []func(*Definition){
		func(d *Definition) { d.Key = "vehicle;drop" }, func(d *Definition) { d.Name = "" }, func(d *Definition) { d.Route = "../vehicles" }, func(d *Definition) { d.Table = "cat_vehicles;drop" }, func(d *Definition) { d.Kind = "other" },
	} {
		copy := base
		change(&copy)
		require.Error(t, copy.Validate())
	}
}
