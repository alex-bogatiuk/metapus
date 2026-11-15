package entity

import (
	"strings"
	"testing"
)

func TestDefinitionTablePartNamesAreReservedAndExplicit(t *testing.T) {
	base := Definition{Key: "crypto_sweep", Name: "CryptoSweep", Route: "crypto-sweep", Table: "doc_crypto_sweeps", Kind: DocumentKind}
	valid := base
	valid.Parts = []Part{{Key: "lines", Field: "walletLines"}}
	valid.DerivedParts = []string{"platformCostLines"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	if table := valid.MustPartTable("lines"); table != "doc_crypto_sweeps__lines" {
		t.Fatalf("part table %q", table)
	}
	if _, ok := valid.PartTable("walletLines"); ok {
		t.Fatal("part table must be addressed by its SQL key, not the field")
	}
	for name, mutate := range map[string]func(*Definition){
		"separator in header":     func(d *Definition) { d.Table = "doc_crypto__sweeps" },
		"trailing underscore":     func(d *Definition) { d.Table = "doc_crypto_sweeps_" },
		"separator in key":        func(d *Definition) { d.Parts = []Part{{Key: "wallet__lines", Field: "lines"}} },
		"key with underscore end": func(d *Definition) { d.Parts = []Part{{Key: "lines_", Field: "lines"}} },
		"camel key":               func(d *Definition) { d.Parts = []Part{{Key: "walletLines", Field: "lines"}} },
		"missing field":           func(d *Definition) { d.Parts = []Part{{Key: "lines"}} },
		"duplicate key":           func(d *Definition) { d.Parts = []Part{{Key: "lines", Field: "a"}, {Key: "lines", Field: "b"}} },
		"duplicate field":         func(d *Definition) { d.Parts = []Part{{Key: "a", Field: "lines"}, {Key: "b", Field: "lines"}} },
		"field also derived": func(d *Definition) {
			d.Parts = []Part{{Key: "lines", Field: "lines"}}
			d.DerivedParts = []string{"lines"}
		},
		"long part name": func(d *Definition) { d.Parts = []Part{{Key: strings.Repeat("x", 63-len(d.Table)-1), Field: "lines"}} },
		"long header":    func(d *Definition) { d.Table = "doc_" + strings.Repeat("x", 60) },
	} {
		t.Run(name, func(t *testing.T) {
			d := base
			mutate(&d)
			if err := d.Validate(); err == nil {
				t.Fatalf("accepted %+v", d)
			}
		})
	}
	edge := base
	edge.Parts = []Part{{Key: strings.Repeat("x", 63-len(base.Table)-2), Field: "lines"}}
	if err := edge.Validate(); err != nil {
		t.Fatalf("63-byte part name rejected: %v", err)
	}
}
