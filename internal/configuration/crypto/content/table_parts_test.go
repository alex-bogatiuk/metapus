package content

import (
	"metapus/internal/configuration"
	"testing"

	v1 "metapus/internal/infrastructure/http/v1"
)

// Building the read registry binds every inspected collection to a declared
// SQL part or a derived collection, so it fails on any undeclared one.
func TestBuiltInTablePartsAreDeclared(t *testing.T) {
	factories := configuration.Compose(Crypto(Services{}))
	registry := v1.BuildReadRegistry(factories)
	sql := map[string]string{}
	derived := map[string]bool{}
	for _, def := range registry.List() {
		for _, tp := range def.TableParts {
			if tp.SQLTable != "" {
				sql[def.Name+"."+tp.Name] = tp.SQLTable
			} else {
				derived[def.Name+"."+tp.Name] = true
			}
		}
	}
	if len(sql) != 1 || sql["CryptoSweep.lines"] != "doc_crypto_sweeps__lines" {
		t.Fatalf("SQL table parts %v", sql)
	}
	if !derived["CryptoSweep.platformCostLines"] || !derived["TreasuryWithdrawal.feeLines"] {
		t.Fatalf("derived collections %v", derived)
	}
}
