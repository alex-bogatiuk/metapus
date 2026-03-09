package filter

// ComparisonType определяет виды сравнения.
type ComparisonType string

const (
	Equal          ComparisonType = "eq"        // Равно
	NotEqual       ComparisonType = "neq"       // Не равно
	LessOrEqual    ComparisonType = "lte"       // Меньше или равно
	GreaterOrEqual ComparisonType = "gte"       // Больше или равно
	Less           ComparisonType = "lt"        // Меньше
	Greater        ComparisonType = "gt"        // Больше
	InList         ComparisonType = "in"        // В списке
	NotInList      ComparisonType = "nin"       // Не в списке
	Contains       ComparisonType = "contains"  // Содержит (ILIKE %val%)
	NotContains    ComparisonType = "ncontains" // Не содержит (NOT ILIKE %val%)

	// Иерархические фильтры
	InHierarchy    ComparisonType = "in_hierarchy"  // В иерархии (в группе или подгруппах)
	NotInHierarchy ComparisonType = "nin_hierarchy" // Не в иерархии

	// Дополнительно
	IsNull    ComparisonType = "null"     // Не заполнено
	IsNotNull ComparisonType = "not_null" // Заполнено
)

// Item представляет одну строку отбора.
type Item struct {
	Field     string         `json:"field"`               // Имя поля (snake_case)
	FieldType string         `json:"fieldType,omitempty"` // Тип поля (например, date)
	Operator  ComparisonType `json:"operator"`            // Вид сравнения
	Value     any            `json:"value"`               // Значение (строка, число, массив ID)
	Scale     int            `json:"scale,omitempty"`     // Storage multiplier (e.g. 10000 for Quantity, 100 for Money)
}

// TablePartInfo describes a child table (table part / tabular section)
// for generating EXISTS subqueries when filtering by table part columns.
type TablePartInfo struct {
	TableName  string              // SQL table name, e.g. "doc_goods_receipt_lines"
	ForeignKey string              // FK column linking to parent, e.g. "document_id"
	ValidCols  map[string]struct{} // allowed filter columns in this table part
}

// ReferenceFieldInfo describes a linked reference entity (catalog)
// for generating EXISTS subqueries when filtering by nested reference fields.
type ReferenceFieldInfo struct {
	TableName  string              // SQL table name of the reference entity, e.g. "cat_counterparties"
	ForeignKey string              // FK column in the parent entity linking to the reference, e.g. "supplier_id"
	ValidCols  map[string]struct{} // allowed filter columns in the reference entity
}
