package filter

// ComparisonType определяет виды сравнения.
type ComparisonType string

const (
	Equal          ComparisonType = "eq"        // Равно
	NotEqual       ComparisonType = "neq"       // Не равно
	LessOrEqual    ComparisonType = "lte"       // Меньше или равно
	GreaterOrEqual ComparisonType = "gte"       // Больше или равно
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
	Field    string         `json:"field"`    // Имя поля (snake_case)
	Operator ComparisonType `json:"operator"` // Вид сравнения
	Value    any            `json:"value"`    // Значение (строка, число, массив ID)
}
