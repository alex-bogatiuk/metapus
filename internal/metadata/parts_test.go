package metadata

import (
	"testing"

	"github.com/alex-bogatiuk/metapus/internal/core/entity"
)

type partsHeader struct {
	Lines []struct {
		WalletID string `db:"wallet_id" json:"walletId"`
	} `db:"-" json:"walletLines"`
	Costs []struct {
		Amount string `json:"amount"`
	} `db:"-" json:"costs"`
}

func TestBindPartsUsesDeclaredKeyAndRejectsUndeclaredCollections(t *testing.T) {
	identity := entity.Definition{Key: "parts_doc", Name: "PartsDoc", Route: "parts-doc", Table: "doc_parts_docs", Kind: entity.DocumentKind,
		Parts:        []entity.Part{{Key: "wallets", Field: "walletLines"}},
		DerivedParts: []string{"costs"},
	}
	def := Inspect(partsHeader{}, "PartsDoc", TypeDocument)
	if err := BindParts(&def, identity); err != nil {
		t.Fatal(err)
	}
	tables := map[string]string{}
	for _, tp := range def.TableParts {
		tables[tp.Name] = tp.SQLTable
	}
	if tables["walletLines"] != "doc_parts_docs__wallets" || tables["costs"] != "" {
		t.Fatalf("bound tables %v", tables)
	}

	undeclared := identity
	undeclared.DerivedParts = nil
	def = Inspect(partsHeader{}, "PartsDoc", TypeDocument)
	if err := BindParts(&def, undeclared); err == nil {
		t.Fatal("undeclared collection accepted")
	}
	unknown := identity
	unknown.Parts = []entity.Part{{Key: "wallets", Field: "lines"}}
	def = Inspect(partsHeader{}, "PartsDoc", TypeDocument)
	if err := BindParts(&def, unknown); err == nil {
		t.Fatal("part bound to a missing collection accepted")
	}
	unknownDerived := identity
	unknownDerived.DerivedParts = []string{"costs", "missing"}
	def = Inspect(partsHeader{}, "PartsDoc", TypeDocument)
	if err := BindParts(&def, unknownDerived); err == nil {
		t.Fatal("unknown derived collection accepted")
	}
}
