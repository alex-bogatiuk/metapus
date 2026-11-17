package metadata

import "testing"

func TestInspectCarriesTheFormGroupOfAField(t *testing.T) {
	type probe struct {
		Amount int    `db:"amount" json:"amount" meta:"label:Сумма,group: Оплата "`
		Note   string `db:"note" json:"note"`
	}
	def := Inspect(probe{}, "Probe", TypeCatalog)
	groups := map[string]string{}
	for _, field := range def.Fields {
		groups[field.Name] = field.Group
	}
	if groups["amount"] != "Оплата" || groups["note"] != "" {
		t.Fatalf("groups %v", groups)
	}
}
