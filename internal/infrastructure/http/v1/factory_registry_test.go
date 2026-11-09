package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/entity"
	"metapus/internal/domain"
	"metapus/internal/infrastructure/http/contracts/spec"
)

type definitionCatalog struct{ entity.Definition }

func (*definitionCatalog) Build(CatalogDeps) CatalogRouteHandler { return nil }

type definitionDocument struct{ entity.Definition }

func (*definitionDocument) Build(DocumentDeps) DocumentRouteHandler { return nil }

func TestFactoryRejectsDuplicateAndInconsistentEntityRegistrations(t *testing.T) {
	base := entity.Definition{Kind: entity.CatalogKind, Key: "definition_probe", Name: "DefinitionProbe", Route: "definition-probes", Table: "cat_definition_probes"}
	for _, change := range []func(*entity.Definition){
		func(d *entity.Definition) { d.Key = "other"; d.Route = "other" },
		func(d *entity.Definition) { d.Name = "Other"; d.Key = "other" },
		func(d *entity.Definition) { d.Name = "Other"; d.Route = "other" },
		func(d *entity.Definition) { d.Kind = entity.DocumentKind },
	} {
		reg := NewFactoryRegistry()
		reg.RegisterCatalog(&definitionCatalog{base})
		other := base
		change(&other)
		require.Panics(t, func() { reg.RegisterCatalog(&definitionCatalog{other}) })
	}
	reg := NewFactoryRegistry()
	reg.RegisterCatalog(&definitionCatalog{base})
	other := base
	other.Kind = entity.DocumentKind
	require.Panics(t, func() { reg.RegisterDocument(&definitionDocument{other}) }, "metadata names must be unique across kinds")
}

type definitionRow struct{ entity.Catalog }

func TestCatalogDefinitionControlsServiceHierarchyWithoutGlobalRegistration(t *testing.T) {
	def := entity.Definition{Kind: entity.CatalogKind, Key: "unregistered_definition_tree", Name: "UnregisteredDefinitionTree", Route: "definition-trees", Table: "cat_definition_trees", Hierarchy: entity.CatalogMeta{Hierarchical: true, HierarchyType: entity.HierarchyItemsOnly, MaxDepth: 3}}
	svc := domain.NewCatalogService(domain.CatalogServiceConfig[*definitionRow]{Definition: &def})
	require.Equal(t, def.Hierarchy, svc.Meta())
}

func (*definitionCatalog) HTTPContracts() spec.Bundle  { return spec.Bundle{} }
func (*definitionDocument) HTTPContracts() spec.Bundle { return spec.Bundle{} }
