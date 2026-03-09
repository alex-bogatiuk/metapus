# Система фильтрации

> Metadata-driven фильтрация списков: от Go-структуры до SQL WHERE-условия. Поддержка шапки, табличных частей, масштабируемых типов (Quantity, Money) и иерархий.

---

## Общая схема

```
┌── FRONTEND ──────────────────────────────────────────────────────────────────┐
│                                                                              │
│  useEntityFiltersMeta("GoodsReceipt")                                        │
│       │  GET /api/v1/meta/GoodsReceipt/filters                               │
│       ▼                                                                      │
│  FilterFieldMeta[]  ──►  FilterConfigDialog  ──►  FilterSidebar              │
│       │                    (выбор полей)           (ввод значений)            │
│       │                                                │                     │
│       ▼                                                ▼                     │
│  buildFilterItems(values, fieldsMeta)                                         │
│       │                                                                      │
│       ▼                                                                      │
│  AdvancedFilterItem[] ──►  ?filter=JSON  ──► GET /api/v1/document/goods-receipt│
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
       │
       ▼
┌── BACKEND ───────────────────────────────────────────────────────────────────┐
│                                                                              │
│  BaseHandler.ParseListFilter(c)                                              │
│       │  json.Unmarshal → []filter.Item                                      │
│       │  filter.ValidateItems()                                              │
│       ▼                                                                      │
│  domain.ListFilter { AdvancedFilters: []filter.Item }                        │
│       │                                                                      │
│       ▼                                                                      │
│  BaseDocumentRepo.buildWhereConditions(f)                                    │
│       ├── header fields → filter.BuildConditions()                           │
│       └── dot-notation → filter.BuildTablePartCondition()  (EXISTS subquery) │
│                │                                                             │
│                ▼                                                             │
│  []squirrel.Sqlizer  →  WHERE clause  →  PostgreSQL                          │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
```

---

## Слои системы фильтрации

### 1. Metadata — автоматическое обнаружение полей

Система фильтрации — **metadata-driven**. Backend автоматически анализирует Go-структуры через reflection и генерирует список фильтруемых полей для frontend.

#### Цепочка

```
Go struct (model.go)
    │  reflection
    ▼
metadata.Inspect()          →  EntityDef { Fields, TableParts }
    │                              │
    ▼                              ▼
metadata.inspector.go       metadata.registry.go
  mapFieldType()              ToFilterMeta()
  (Go type → FieldType)      (EntityDef → []FilterFieldMeta)
```

#### `inspector.go` — определение типов полей

Функция `mapFieldType()` анализирует Go-тип каждого поля структуры:

| Go-тип | `FieldType` | `ValueScale` | Комментарий |
|--------|-------------|-------------|-------------|
| `id.ID` / `*id.ID` | `TypeReference` | — | Ссылка на справочник |
| `time.Time` / `*time.Time` | `TypeDate` | — | Дата/время |
| `types.Quantity` | `TypeNumber` | `10000` | Фиксированная точка ×10000 |
| `types.MinorUnits` | `TypeMoney` | — | Динамический масштаб (валюта) |
| `string` | `TypeString` | — | |
| `int*` | `TypeInteger` | — | Если имя содержит `Amount`/`Price` → `TypeMoney` |
| `float*` | `TypeNumber` | — | |
| `bool` | `TypeBoolean` | — | |

Табличные части (слайсы структур, например `Lines []GoodsReceiptLine`) автоматически обнаруживаются и инспектируются рекурсивно.

#### `registry.go` — формирование FilterFieldMeta

`ToFilterMeta()` конвертирует `EntityDef` в плоский список `FilterFieldMeta`:

```go
type FilterFieldMeta struct {
    Key         string `json:"key"`                   // "totalAmount" или "lines.unitPrice"
    Label       string `json:"label"`                 // "Сумма итого"
    FieldType   string `json:"fieldType"`             // "string" | "number" | "money" | "date" | ...
    Group       string `json:"group,omitempty"`        // Группа (имя табличной части)
    RefEndpoint string `json:"refEndpoint,omitempty"`  // Эндпоинт для справочника
    ValueScale  int    `json:"valueScale,omitempty"`   // Множитель хранения (10000 для Quantity)
}
```

**Маппинг FieldType → фронтенд-тип:**

| Внутренний `FieldType` | Фронтенд `fieldType` |
|------------------------|---------------------|
| `TypeString` | `"string"` |
| `TypeInteger`, `TypeNumber` | `"number"` |
| `TypeMoney` | `"money"` |
| `TypeDate` | `"date"` |
| `TypeBoolean` | `"boolean"` |
| `TypeReference` | `"reference"` |
| `TypeEnum` | `"enum"` |

**Ключи полей табличных частей** используют dot-нотацию: `"lines.productId"`.

**Системные поля** (`id`, `version`, `attributes`, `createdAt`, `updatedAt`, `createdBy`, `updatedBy`, `txid`, `deletedAt`, `postedVersion`) автоматически исключаются из списка фильтров через `skipFilterFields`.

#### HTTP-эндпоинт метаданных

```
GET /api/v1/meta/:entityName/filters
```

Обработчик в `handlers/metadata.go`:

```go
func (h *MetadataHandler) GetEntityFilters(c *gin.Context) {
    name := c.Param("name")
    def, ok := h.registry.Get(name)
    if !ok {
        c.Status(http.StatusNotFound)
        return
    }
    c.JSON(http.StatusOK, def.ToFilterMeta())
}
```

---

### 2. Frontend — UI фильтрации

#### Компоненты

| Компонент | Файл | Назначение |
|-----------|------|-----------|
| `useEntityFiltersMeta` | `hooks/useEntityFiltersMeta.ts` | Загрузка `FilterFieldMeta[]` из backend |
| `FilterConfigDialog` | `components/shared/filter-config-dialog.tsx` | Диалог выбора полей для отбора |
| `FilterSidebar` | `components/shared/filter-sidebar.tsx` | Боковая панель с полями ввода фильтров |
| `buildFilterItems` | `lib/filter-utils.ts` | Сборка `AdvancedFilterItem[]` для API |

#### Типы фильтруемых полей

```typescript
type FieldType = "string" | "number" | "money" | "date" | "boolean" | "reference" | "enum"
```

Каждый тип определяет:
- **Набор операторов** — через `getOperatorsForType(fieldType)`
- **Виджет ввода** — текст, число, дата, справочник, boolean-переключатель
- **Оператор по умолчанию** — через `getDefaultOperator(fieldType)`

#### Доступные операторы по типу поля

| Тип поля | Операторы |
|----------|----------|
| `string` | `contains`, `ncontains`, `eq`, `neq`, `null`, `not_null` |
| `number`, `money` | `eq`, `neq`, `gt`, `gte`, `lt`, `lte`, `null`, `not_null` |
| `date` | `eq`, `neq`, `gt`, `gte`, `lt`, `lte`, `null`, `not_null` |
| `boolean` | `eq` |
| `reference` | `eq`, `neq`, `in`, `nin`, `null`, `not_null` |
| `enum` | `eq`, `neq`, `in`, `nin`, `null`, `not_null` |

#### Формирование запроса — `buildFilterItems()`

Функция `buildFilterItems()` преобразует состояние UI в массив `AdvancedFilterItem[]`:

1. **camelCase → snake_case**: `"supplierId"` → `"supplier_id"`
2. **Тип поля** (`fieldType`) и **множитель** (`valueScale`) берутся из метаданных
3. **Период** (встроенный `__period__`) → два элемента `gte`/`lte` по полю `date`
4. **Ссылки** (`{ id, name }`) → извлекается только `id`
5. **Списки** (`in`/`nin`) → массив значений
6. **Nullary** (`null`/`not_null`) → элемент без `value`

Результат сериализуется в JSON и передаётся в query-параметр `?filter=`.

**Пример запроса:**
```
GET /api/v1/document/goods-receipt?limit=50&filter=[
  {"field":"total_amount","fieldType":"money","operator":"gte","value":1000},
  {"field":"supplier_id","fieldType":"reference","operator":"eq","value":"019c..."}
]
```

---

### 3. Backend — парсинг и валидация

#### `ParseListFilter()` — точка входа

`BaseHandler.ParseListFilter()` в `handlers/base.go` — единая точка парсинга query-параметров для всех List-эндпоинтов:

```go
func (h *BaseHandler) ParseListFilter(c *gin.Context, defaultOrderBy string) (domain.ListFilter, error) {
    filter := domain.DefaultListFilter()
    filter.Search = c.Query("search")
    filter.Limit = h.ParseIntQuery(c, "limit", 50)
    filter.Offset = h.ParseIntQuery(c, "offset", 0)
    filter.OrderBy = c.DefaultQuery("orderBy", defaultOrderBy)
    filter.IncludeDeleted = c.Query("includeDeleted") == "true"

    filterJSON := c.Query("filter")
    if filterJSON != "" {
        var advFilters []filter.Item
        json.Unmarshal([]byte(filterJSON), &advFilters)
        filter.ValidateItems(advFilters)
        filter.AdvancedFilters = advFilters
    }
    return filter, nil
}
```

#### `domain.ListFilter` — транспортная структура

```go
type ListFilter struct {
    Search          string        // Полнотекстовый поиск (ILIKE по number/name)
    IDs             []id.ID       // Фильтр по конкретным ID
    IncludeDeleted  bool          // Включать помеченные на удаление
    ParentID        *id.ID        // Для иерархических справочников
    IsFolder        *bool         // Только группы / только элементы
    AdvancedFilters []filter.Item // Расширенные отборы
    OrderBy         string        // Сортировка ("-date", "name")
    Limit           int           // Пагинация
    Offset          int
}
```

#### `filter.Item` — единица отбора

```go
type Item struct {
    Field     string         `json:"field"`               // "total_amount" или "lines.product_id"
    FieldType string         `json:"fieldType,omitempty"` // "date", "money", "number", ...
    Operator  ComparisonType `json:"operator"`            // "eq", "gte", "in", ...
    Value     any            `json:"value"`               // Значение
    Scale     int            `json:"scale,omitempty"`     // Множитель хранения (10000 для Quantity)
}
```

---

### 4. Backend — построение SQL

#### Операторы сравнения (`ComparisonType`)

| Оператор | Значение | SQL | Описание |
|----------|----------|-----|----------|
| `eq` | `"eq"` | `= ?` | Равно |
| `neq` | `"neq"` | `(<> ? OR IS NULL)` | Не равно (NULL-safe) |
| `lt` | `"lt"` | `< ?` | Меньше |
| `lte` | `"lte"` | `<= ?` | Меньше или равно |
| `gt` | `"gt"` | `> ?` | Больше |
| `gte` | `"gte"` | `>= ?` | Больше или равно |
| `in` | `"in"` | `IN (?, ?, ...)` | В списке |
| `nin` | `"nin"` | `(NOT IN (...) OR IS NULL)` | Не в списке (NULL-safe) |
| `contains` | `"contains"` | `ILIKE '%val%'` | Содержит (регистронезависимо) |
| `ncontains` | `"ncontains"` | `(NOT ILIKE OR IS NULL)` | Не содержит (NULL-safe) |
| `in_hierarchy` | `"in_hierarchy"` | Рекурсивный CTE | В иерархии (группа + подгруппы) |
| `nin_hierarchy` | `"nin_hierarchy"` | Рекурсивный CTE (NOT IN) | Не в иерархии |
| `null` | `"null"` | `IS NULL` | Не заполнено |
| `not_null` | `"not_null"` | `IS NOT NULL` | Заполнено |

> **NULL-safe семантика:** Негативные операторы (`neq`, `nin`, `ncontains`) включают строки с `NULL` — это соответствует ожиданию пользователя "покажи все, кроме X".

#### `BuildConditions()` — фильтрация полей шапки

Основная функция в `filter/builder.go`. Принимает `[]Item`, whitelist допустимых колонок и имя таблицы:

```go
func BuildConditions(items []Item, validCols map[string]struct{}, tableName string) ([]squirrel.Sqlizer, error)
```

**Алгоритм:**
1. `ValidateItems()` — проверка структуры каждого элемента
2. Проверка `item.Field` по whitelist (`validCols`) — **защита от SQL injection**
3. Для `fieldType == "money"` → делегация в `buildMoneyCondition()` (динамическое масштабирование)
4. Для остальных — `scaleFilterValue(item.Value, item.Scale)` (статическое масштабирование)
5. Для `fieldType == "date"` — оборачивание в `DATE(field)`
6. Построение `squirrel.Sqlizer` по оператору

#### `BuildTablePartCondition()` — фильтрация полей табличных частей

Генерирует `EXISTS` / `NOT EXISTS` подзапрос к дочерней таблице:

```go
func BuildTablePartCondition(item Item, parentTable string, tp TablePartInfo, column string) (squirrel.Sqlizer, error)
```

**Результирующий SQL:**

```sql
-- Положительные операторы (eq, gt, gte, lt, lte, in, contains):
EXISTS (SELECT 1 FROM doc_goods_receipt_lines
        WHERE doc_goods_receipt_lines.document_id = doc_goods_receipts.id
          AND product_id = $1)

-- Отрицательные операторы (neq, nin, ncontains):
NOT EXISTS (SELECT 1 FROM doc_goods_receipt_lines
            WHERE doc_goods_receipt_lines.document_id = doc_goods_receipts.id
              AND product_id = $1)

-- null:
EXISTS (... AND column IS NULL)

-- not_null:
NOT EXISTS (... AND column IS NULL)
```

**Семантика:**
- `eq` = «документ имеет хотя бы одну строку с col = value»
- `neq` = «документ НЕ имеет ни одной строки с col = value»

#### Разделение шапки и табличных частей

`BaseDocumentRepo.buildWhereConditions()` автоматически разделяет фильтры по dot-нотации:

```go
for _, item := range f.AdvancedFilters {
    if strings.Contains(item.Field, ".") {
        // "lines.product_id" → EXISTS subquery
        cond, err := r.buildTablePartCondition(item)
    } else {
        // "total_amount" → header filter
        headerFilters = append(headerFilters, item)
    }
}
```

---

### 5. Масштабирование числовых типов

Metapus хранит числа в фиксированной точке для избежания потерь точности с плавающей запятой. Система фильтрации должна преобразовывать «человекочитаемые» значения в формат хранения.

#### Quantity — статическое масштабирование

`Quantity` хранится как `int64 × 10000`. Масштаб **универсальный** для всех единиц измерения.

```
Пользователь вводит: 10
В БД хранится:       100000 (= 10 × 10000)
```

**Цепочка:**
1. `inspector.go`: `types.Quantity` → `ValueScale = 10000`
2. Metadata API: `{ "fieldType": "number", "valueScale": 10000 }`
3. Frontend: `buildFilterItems()` копирует `valueScale` → `scale: 10000` в `AdvancedFilterItem`
4. Backend: `scaleFilterValue(10, 10000)` → `100000`
5. SQL: `WHERE quantity = 100000`

```go
// scaleFilterValue умножает значение на масштаб хранения.
// Поддерживает float64, int, int64, string, []interface{}.
func scaleFilterValue(value any, scale int) any {
    if scale <= 1 || value == nil {
        return value
    }
    // ...
    case float64:
        return int64(math.Round(v * s))
}
```

#### Money (MinorUnits) — динамическое SQL-масштабирование

`MinorUnits` хранится как `int64` в минимальных единицах валюты. Масштаб **зависит от валюты документа**:

| Валюта | `decimal_places` | `minor_multiplier` | Пример |
|--------|----------------:|-------------------:|--------|
| RUB | 2 | 100 | 1000 руб = 100000 |
| USD | 2 | 100 | 10.50$ = 1050 |
| JPY | 0 | 1 | 500¥ = 500 |
| ETH | 9 | 1000000000 | 0.5 ETH = 500000000 |
| BTC | 8 | 100000000 | 0.001 BTC = 100000 |

> **Критично:** Статический `valueScale = 100` **неправилен** для мульти-валютных систем. Масштаб определяется динамически через `currency_id` документа.

**Решение:** Масштабирование выполняется **на уровне SQL** через подзапрос к `cat_currencies`:

```sql
-- Шапка документа:
WHERE total_amount = ROUND(CAST($1 AS NUMERIC) * (
    SELECT minor_multiplier FROM cat_currencies
    WHERE id = doc_goods_receipts.currency_id
))

-- Табличная часть:
EXISTS (SELECT 1 FROM doc_goods_receipt_lines
    WHERE doc_goods_receipt_lines.document_id = doc_goods_receipts.id
      AND amount >= ROUND(CAST($1 AS NUMERIC) * (
          SELECT minor_multiplier FROM cat_currencies
          WHERE id = doc_goods_receipts.currency_id
      ))
)
```

**Цепочка:**
1. `inspector.go`: `types.MinorUnits` → `TypeMoney`, **без** `ValueScale`
2. Metadata API: `{ "fieldType": "money" }` (нет `valueScale`)
3. Frontend: не передаёт `scale` для money-полей
4. Backend: `buildMoneyCondition()` генерирует SQL с подзапросом к `cat_currencies`

```go
func buildMoneyCondition(fieldExpr string, op ComparisonType, value any, tableName string) (squirrel.Sqlizer, error) {
    mul := fmt.Sprintf(
        "ROUND(CAST(? AS NUMERIC) * (SELECT minor_multiplier FROM cat_currencies WHERE id = %s.currency_id))",
        tableName,
    )
    // ...
    case Equal:
        return squirrel.Expr(fmt.Sprintf("%s = %s", fieldExpr, mul), value), nil
}
```

> **Ограничение:** Операторы `in` / `nin` **не поддерживаются** для `money`-полей с динамическим масштабированием (возвращается ошибка).

---

### 6. Иерархическая фильтрация

Для справочников с иерархией (поле `parent_id`) доступны операторы `in_hierarchy` и `nin_hierarchy`. Они генерируют рекурсивный CTE:

```sql
-- in_hierarchy: все элементы в группе и подгруппах
id IN (
    WITH RECURSIVE hierarchy AS (
        SELECT id FROM cat_nomenclature WHERE id = $1
        UNION ALL
        SELECT t.id FROM cat_nomenclature t
        JOIN hierarchy h ON t.parent_id = h.id
    )
    SELECT id FROM hierarchy
)

-- nin_hierarchy: все элементы вне иерархии (NULL-safe)
(id NOT IN (
    WITH RECURSIVE hierarchy AS (...)
    SELECT id FROM hierarchy
) OR id IS NULL)
```

---

### 7. Whitelist и защита от SQL injection

Каждый репозиторий строит whitelist допустимых колонок при инициализации:

```go
// document_repo/base.go
validCols := filter.BuildValidCols(selectCols, "id", "number", "date", "posted", "deletion_mark")
```

Для табличных частей — отдельный whitelist:

```go
// document_repo/goods_receipt.go
repo.RegisterTablePart("lines", goodsReceiptLinesTable, "document_id", []string{
    "product_id", "unit_id", "quantity", "unit_price",
    "discount_percent", "discount_amount",
    "vat_rate_id", "vat_percent", "vat_amount", "amount",
})
```

Любое поле, отсутствующее в whitelist, приводит к ошибке `"invalid filter column: %s"`.

---

### 8. Сортировка

Сортировка контролируется query-параметром `orderBy`:
- `"name"` → `ORDER BY name ASC`
- `"-date"` → `ORDER BY date DESC`
- `"+code"` → `ORDER BY code ASC`

Допустимые колонки сортировки проверяются по отдельному whitelist `orderCols`, который включает `selectCols` + стандартные (`id`, `code`, `name`, `created_at`, `updated_at`, `version`).

---

## Файловая структура

```
internal/domain/filter/
├── types.go          # Item, ComparisonType, TablePartInfo
└── builder.go        # BuildConditions, BuildTablePartCondition,
                      # buildMoneyCondition, scaleFilterValue,
                      # ValidateItems, BuildValidCols, expandSlice

internal/domain/
└── repository.go     # ListFilter, ListResult[T]

internal/metadata/
├── inspector.go      # Inspect(), mapFieldType() — Go struct → EntityDef
└── registry.go       # EntityDef, FieldDef, FilterFieldMeta, ToFilterMeta(),
                      # Registry, filterFieldType(), skipFilterFields

internal/infrastructure/http/v1/handlers/
├── base.go           # ParseListFilter() — парсинг ?filter= JSON
└── metadata.go       # GetEntityFilters() — /meta/:name/filters

internal/infrastructure/storage/postgres/
├── catalog_repo/base.go   # BaseCatalogRepo.buildWhereConditions()
└── document_repo/
    ├── base.go             # BaseDocumentRepo.buildWhereConditions(),
    │                       # buildTablePartCondition(), RegisterTablePart()
    └── goods_receipt.go    # RegisterTablePart("lines", ...) — пример

frontend/
├── hooks/useEntityFiltersMeta.ts       # Загрузка FilterFieldMeta[] из API
├── components/shared/
│   ├── filter-config-dialog.tsx        # Диалог выбора полей + FieldType, FilterFieldMeta
│   └── filter-sidebar.tsx              # Боковая панель фильтров
├── lib/filter-utils.ts                 # buildFilterItems(), getOperatorsForType(),
│                                       # camelToSnake(), FilterEntry, FilterValues
└── types/common.ts                     # AdvancedFilterItem, ComparisonOperator
```

---

## Подключение фильтрации к новой сущности

### Справочник (Catalog)

Фильтрация работает **автоматически** через `BaseCatalogRepo`. Достаточно:

1. **Model** — определить поля Go-структуры с корректными типами
2. **Metadata** — зарегистрировать в `setupMetadataRegistry()` с `SetFieldLabels()`
3. **Handler** — использовать `ParseListFilter()` (уже есть в generic `CatalogHandler`)

### Документ (Document) — шапка

Аналогично справочнику — через `BaseDocumentRepo`.

### Документ (Document) — табличные части

Дополнительно к шапке, зарегистрировать табличную часть в репозитории:

```go
func NewGoodsReceiptRepo() *GoodsReceiptRepo {
    repo := &GoodsReceiptRepo{
        BaseDocumentRepo: NewBaseDocumentRepo[*goods_receipt.GoodsReceipt](...),
    }

    // Регистрация табличной части для фильтрации
    repo.RegisterTablePart("lines", "doc_goods_receipt_lines", "document_id", []string{
        "product_id", "unit_id", "quantity", "unit_price",
        "discount_percent", "discount_amount",
        "vat_rate_id", "vat_percent", "vat_amount", "amount",
    })

    return repo
}
```

- `"lines"` — имя табличной части (совпадает с `json`-тегом поля `Lines` в модели)
- `"doc_goods_receipt_lines"` — SQL-таблица дочерних записей
- `"document_id"` — FK-колонка, связывающая с шапкой
- `[]string{...}` — whitelist колонок для фильтрации

> После регистрации фильтры вида `"lines.product_id"` автоматически преобразуются в EXISTS-подзапросы.

---

## Сравнение с аналогами

| Аспект | Metapus | 1С:Предприятие | ERPNext (Frappe) |
|--------|---------|---------------|-----------------|
| Источник метаданных | Go struct + reflection | Конфигуратор (XML-метаданные) | DocType (JSON) |
| Хранение конфигурации фильтров | Frontend (localStorage + API) | Настройки отбора в форме списка | URL query + saved filters |
| Фильтры табличных частей | EXISTS подзапрос | Соединение таблиц | Не поддерживается |
| Масштабирование чисел | Автоматическое (Quantity ×10000, Money динамическое) | Не требуется (decimal) | Не требуется (decimal) |
| Иерархия | Рекурсивный CTE | Встроенная поддержка иерархии | Нет прямого аналога |
| NULL-safe отрицание | `(field <> value OR field IS NULL)` | Аналогичное поведение | Стандартный SQL (без NULL-safe) |
