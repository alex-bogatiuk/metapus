package main

import (
	"metapus/internal/domain/catalogs/counterparty"
	"metapus/internal/domain/catalogs/currency"
	"metapus/internal/domain/catalogs/nomenclature"
	"metapus/internal/domain/catalogs/unit"
	"metapus/internal/domain/catalogs/warehouse"
	"metapus/internal/domain/documents/goods_issue"
	"metapus/internal/domain/documents/goods_receipt"
	"metapus/internal/metadata"
)

// setupMetadataRegistry initializes and populates the metadata registry.
func setupMetadataRegistry() *metadata.Registry {
	reg := metadata.NewRegistry()

	// Helper to register entity with localized label
	register := func(entity interface{}, name string, typ metadata.EntityType, label string) {
		def := metadata.Inspect(entity, name, typ)
		def.Label = label

		// Here we could also augment fields with labels if we had a translation map.
		// For MVP we rely on Inspect's auto-guessing based on field names.

		reg.Register(def)
	}

	// --- Catalogs ---
	register(counterparty.Counterparty{}, "Counterparty", metadata.TypeCatalog, "Контрагенты")
	register(nomenclature.Nomenclature{}, "Nomenclature", metadata.TypeCatalog, "Номенклатура")
	register(warehouse.Warehouse{}, "Warehouse", metadata.TypeCatalog, "Склады")
	register(unit.Unit{}, "Unit", metadata.TypeCatalog, "Единицы измерения")
	register(currency.Currency{}, "Currency", metadata.TypeCatalog, "Валюты")

	// --- Documents ---
	register(goods_receipt.GoodsReceipt{}, "GoodsReceipt", metadata.TypeDocument, "Поступление товаров")
	register(goods_issue.GoodsIssue{}, "GoodsIssue", metadata.TypeDocument, "Реализация товаров")

	return reg
}
