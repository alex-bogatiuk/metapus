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

// refEndpoints maps referenceType (derived from Go struct field names by the inspector)
// to the corresponding catalog API endpoint path.
// E.g. SupplierID → referenceType="supplier" → "/catalog/counterparties".
var refEndpoints = map[string]string{
	"organization": "/catalog/organizations",
	"supplier":     "/catalog/counterparties",
	"customer":     "/catalog/counterparties",
	"contract":     "/catalog/contracts",
	"warehouse":    "/catalog/warehouses",
	"currency":     "/catalog/currencies",
	"product":      "/catalog/nomenclature",
	"unit":         "/catalog/units",
	"vatrate":      "/catalog/vat-rates",
	"parent":       "", // parent is self-referencing, skip
}

// setupMetadataRegistry initializes and populates the metadata registry.
func setupMetadataRegistry() *metadata.Registry {
	reg := metadata.NewRegistry()

	// Helper to register entity with localized label and field labels.
	register := func(entity interface{}, name string, typ metadata.EntityType, label string, fieldLabels map[string]string) {
		def := metadata.Inspect(entity, name, typ)
		def.Label = label
		def.SetRefEndpoints(refEndpoints)
		if fieldLabels != nil {
			def.SetFieldLabels(fieldLabels)
		}
		reg.Register(def)
	}

	// --- Catalogs ---
	register(counterparty.Counterparty{}, "Counterparty", metadata.TypeCatalog, "Контрагенты", map[string]string{
		"code": "Код", "name": "Наименование",
		"inn": "ИНН", "kpp": "КПП", "isSupplier": "Поставщик", "isCustomer": "Покупатель",
		"deletionMark": "Пометка удаления", "isFolder": "Группа", "parentId": "Родитель",
	})
	register(nomenclature.Nomenclature{}, "Nomenclature", metadata.TypeCatalog, "Номенклатура", map[string]string{
		"code": "Код", "name": "Наименование",
		"deletionMark": "Пометка удаления", "isFolder": "Группа", "parentId": "Родитель",
	})
	register(warehouse.Warehouse{}, "Warehouse", metadata.TypeCatalog, "Склады", map[string]string{
		"code": "Код", "name": "Наименование",
		"deletionMark": "Пометка удаления", "isFolder": "Группа", "parentId": "Родитель",
	})
	register(unit.Unit{}, "Unit", metadata.TypeCatalog, "Единицы измерения", map[string]string{
		"code": "Код", "name": "Наименование",
		"deletionMark": "Пометка удаления",
	})
	register(currency.Currency{}, "Currency", metadata.TypeCatalog, "Валюты", map[string]string{
		"code": "Код", "name": "Наименование",
		"deletionMark": "Пометка удаления",
	})

	// --- Documents ---
	register(goods_receipt.GoodsReceipt{}, "GoodsReceipt", metadata.TypeDocument, "Поступление товаров", map[string]string{
		"number": "Номер", "date": "Дата", "posted": "Проведен",
		"organizationId": "Организация", "description": "Комментарий",
		"supplierId": "Поставщик", "contractId": "Договор",
		"warehouseId":       "Склад",
		"supplierDocNumber": "№ документа поставщика", "supplierDocDate": "Дата документа поставщика",
		"incomingNumber": "№ вх. документа",
		"currencyId":     "Валюта", "amountIncludesVat": "Сумма включает НДС",
		"totalQuantity": "Количество итого", "totalAmount": "Сумма итого", "totalVat": "НДС итого",
		"deletionMark": "Пометка удаления",
		// Table part "lines"
		"lines":                 "Товары",
		"lines.lineNo":          "№ строки",
		"lines.productId":       "Номенклатура",
		"lines.unitId":          "Единица",
		"lines.coefficient":     "Коэффициент",
		"lines.quantity":        "Количество",
		"lines.unitPrice":       "Цена",
		"lines.discountPercent": "Скидка %",
		"lines.discountAmount":  "Скидка сумма",
		"lines.vatRateId":       "Ставка НДС",
		"lines.vatAmount":       "Сумма НДС",
		"lines.amount":          "Сумма",
	})
	register(goods_issue.GoodsIssue{}, "GoodsIssue", metadata.TypeDocument, "Реализация товаров", map[string]string{
		"number": "Номер", "date": "Дата", "posted": "Проведен",
		"organizationId": "Организация", "description": "Комментарий",
		"customerId": "Покупатель", "contractId": "Договор",
		"warehouseId": "Склад",
		"currencyId":  "Валюта", "amountIncludesVat": "Сумма включает НДС",
		"totalQuantity": "Количество итого", "totalAmount": "Сумма итого", "totalVat": "НДС итого",
		"deletionMark": "Пометка удаления",
		// Table part "lines"
		"lines":                 "Товары",
		"lines.lineNo":          "№ строки",
		"lines.productId":       "Номенклатура",
		"lines.unitId":          "Единица",
		"lines.coefficient":     "Коэффициент",
		"lines.quantity":        "Количество",
		"lines.unitPrice":       "Цена",
		"lines.discountPercent": "Скидка %",
		"lines.discountAmount":  "Скидка сумма",
		"lines.vatRateId":       "Ставка НДС",
		"lines.vatAmount":       "Сумма НДС",
		"lines.amount":          "Сумма",
	})

	return reg
}
