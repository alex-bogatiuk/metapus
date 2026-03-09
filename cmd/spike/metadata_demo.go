package main

import (
	"encoding/json"
	"fmt"

	"metapus/internal/domain/catalogs/counterparty"
	"metapus/internal/domain/documents/goods_receipt"
	"metapus/internal/metadata"
)

func main() {
	reg := metadata.NewRegistry()

	// Register Counterparty
	cp := counterparty.Counterparty{}
	fmt.Println("Inspecting Counterparty...")
	defCP := metadata.Inspect(cp, "Counterparty", metadata.TypeCatalog)

	// Add table name manually or extract from conventions (spike simplification)
	defCP.TableName = "cat_counterparty"
	reg.Register(defCP)

	// Register GoodsReceipt
	gr := goods_receipt.GoodsReceipt{}
	fmt.Println("Inspecting GoodsReceipt...")
	defGR := metadata.Inspect(gr, "GoodsReceipt", metadata.TypeDocument)
	defGR.TableName = "doc_goods_receipt"

	// Manual enhancements (simulating what would come from tags or translation files)
	defGR.Label = "Поступление товаров"

	// Fix Labels
	for i, f := range defGR.Fields {
		switch f.Name {
		case "number":
			defGR.Fields[i].Label = "Номер"
		case "date":
			defGR.Fields[i].Label = "Дата"
		case "supplierId":
			defGR.Fields[i].Label = "Поставщик"
			defGR.Fields[i].ReferenceType = "counterparty"
		case "warehouseId":
			defGR.Fields[i].Label = "Склад"
			defGR.Fields[i].ReferenceType = "warehouse"
		}
	}

	// Fix TableParts
	if len(defGR.TableParts) > 0 {
		tp := &defGR.TableParts[0]
		tp.Label = "Товары"
		for i, c := range tp.Columns {
			switch c.Name {
			case "productId":
				tp.Columns[i].Label = "Номенклатура"
				tp.Columns[i].ReferenceType = "nomenclature"
			case "quantity":
				tp.Columns[i].Label = "Количество"
			case "unitPrice":
				tp.Columns[i].Label = "Цена за ед."
			}
		}
	}

	reg.Register(defGR)

	// List all
	defaults := reg.List()

	// Print JSON
	bytes, _ := json.MarshalIndent(defaults, "", "  ")
	fmt.Println(string(bytes))
}
