package metadata

import (
	"testing"

	"github.com/alex-bogatiuk/metapus/internal/core/entity"
)

func TestBindCodePublishesTheCatalogCodeRule(t *testing.T) {
	catalog := entity.Definition{Kind: entity.CatalogKind, Key: "organization", Code: entity.CodeRule{Prefix: "ORG"}}
	def := EntityDef{Fields: []FieldDef{{Name: "code"}, {Name: "name"}}}
	BindCode(&def, catalog)
	if def.Code == nil || *def.Code != (CodeAssignment{Source: entity.CodeFromSeries, Prefix: "ORG"}) || def.Fields[0].Required {
		t.Fatalf("series rule %+v, fields %+v", def.Code, def.Fields)
	}

	catalog.Code = entity.CodeRule{Source: entity.CodeManual}
	def = EntityDef{Fields: []FieldDef{{Name: "code"}, {Name: "name"}}}
	BindCode(&def, catalog)
	if def.Code.Source != entity.CodeManual || def.Code.Prefix != "" || !def.Fields[0].Required || def.Fields[1].Required {
		t.Fatalf("a manual code is a required field: %+v %+v", def.Code, def.Fields)
	}

	document := EntityDef{}
	BindCode(&document, entity.Definition{Kind: entity.DocumentKind, Key: "waybill"})
	if document.Code != nil {
		t.Fatal("a document has no catalog code rule")
	}
}
