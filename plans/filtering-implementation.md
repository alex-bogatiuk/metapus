# План реализации: Система фильтрации Metapus

> **Дата:** 2026-02-22
> **Статус:** Черновик
> **Автор:** Системный архитектор

---

## 1. Анализ текущего состояния

### 1.1. Backend (Go)

#### Что уже есть:

| Компонент | Файл | Статус |
|-----------|------|--------|
| `filter.Item` — единица отбора (field + operator + value) | `internal/domain/filter/types.go` | ✅ Готово |
| `filter.ComparisonType` — 14 операторов сравнения (eq, neq, lt, gt, contains, in_hierarchy и др.) | `internal/domain/filter/types.go` | ✅ Готово |
| `domain.ListFilter.AdvancedFilters []filter.Item` | `internal/domain/repository.go` | ✅ Готово |
| `BaseCatalogRepo.buildAdvancedFilterConditions()` — трансляция `filter.Item` → SQL (squirrel) | `internal/infrastructure/.../catalog_repo/base.go:343` | ✅ Для справочников |
| Парсинг `?filter=` JSON из query string для каталогов | `internal/infrastructure/http/v1/handlers/catalog.go:82-91` | ✅ Для справочников |
| `GoodsReceipt.ListFilter` — typed-фильтры (SupplierID, WarehouseID, DateFrom/DateTo) | `internal/domain/documents/goods_receipt/repository.go` | ✅ Hardcoded-поля |
| `GoodsReceiptHandler.List()` — парсинг typed query params | `internal/infrastructure/http/v1/handlers/goods_receipt.go:107-166` | ✅ Hardcoded-парсинг |
| `validCols` whitelist в `BaseCatalogRepo` для защиты от SQL injection | `catalog_repo/base.go:47-56` | ✅ Для справочников |

#### Что отсутствует:

| Компонент | Описание | Приоритет |
|-----------|----------|-----------|
| `AdvancedFilters` в document handler | Document handler (`GoodsReceiptHandler.List`) не парсит `?filter=` JSON — только typed поля | 🔴 Высокий |
| `buildAdvancedFilterConditions()` в `BaseDocumentRepo` | `BaseDocumentRepo` не имеет метода для advanced фильтров (только `BaseCatalogRepo`) | 🔴 Высокий |
| `validCols` whitelist в `BaseDocumentRepo` | Нет защиты от произвольных column names | 🔴 Высокий |
| Entity Metadata API | Нет endpoint'а для получения структуры полей объекта (метаданные фильтрации) | 🟡 Средний |
| Фильтрация по табличным частям (JOIN) | `filter.Item` не поддерживает поля из связанных таблиц (lines) | 🟠 Средний-Высокий |
| Сохранение пользовательских настроек фильтрации | Нет хранения выбранных фильтров пользователя | 🟢 Низкий (Phase 2) |

### 1.2. Frontend (Next.js / React)

#### Что уже есть:

| Компонент | Файл | Статус |
|-----------|------|--------|
| `FilterSidebar` — боковая панель с фильтрами | `components/shared/filter-sidebar.tsx` | ✅ UI-каркас |
| `FilterConfigDialog` — диалог настройки фильтров (1С-стиль) | `components/shared/filter-config-dialog.tsx` | ✅ UI готов |
| `FilterFieldMeta` — описание поля для диалога настройки | `filter-config-dialog.tsx` | ✅ TypeScript тип |
| `goodsReceiptFieldsMeta[]` — hardcoded метаданные полей | `app/purchases/goods-receipts/page.tsx` | ✅ Hardcoded |
| `docFilters[]` — статические фильтры sidebar | `app/purchases/goods-receipts/page.tsx` | ✅ Hardcoded |
| `useUrlSort` — URL-driven сортировка | `hooks/useUrlSort.ts` | ✅ |
| `useListSelection` — выделение строк | `hooks/useListSelection.ts` | ✅ |
| `api.goodsReceipts.list()` — API-клиент | `lib/api.ts` | ✅ Без фильтров |

#### Что отсутствует:

| Компонент | Описание | Приоритет |
|-----------|----------|-----------|
| `useUrlFilters` — URL-driven состояние фильтров | Синхронизация фильтров с URL search params | 🔴 Высокий |
| Интеграция `FilterSidebar` с API | Sidebar не отправляет запросы при изменении фильтров | 🔴 Высокий |
| Динамическая генерация `FilterFieldMeta` из API | Метаданные полей хардкожены, не получаются с сервера | 🟡 Средний |
| Интеграция `FilterConfigDialog` → `FilterSidebar` → Data Fetch | Замкнуть цепочку: настройка фильтров → появление в sidebar → изменение значения → API запрос | 🔴 Высокий |
| Рендеринг разных типов фильтров по `FieldType` | Для reference-полей нужен lookup (комбобокс), для date — period picker и т.д. | 🟡 Средний |
| Debounce для text-фильтров | Не слать запрос на каждый keystroke | 🟡 Средний |
| `localStorage` persistence для выбранных фильтров | Запоминать набор фильтров между сессиями | 🟢 Низкий |

---

## 2. Архитектура решения

### 2.1. Общая схема data flow

```
┌──────────────────────────────────────────────────────────────────┐
│  FRONTEND                                                         │
│                                                                   │
│  FilterConfigDialog ──(onApply)──▶ FilterSidebar                 │
│       │ выбор полей                    │ значения фильтров       │
│       ▼                                ▼                         │
│  selectedFilterKeys[]          useUrlFilters()                   │
│                                    │  ←── URL search params      │
│                                    ▼                             │
│                            buildApiQuery()                       │
│                                    │                             │
│                                    ▼                             │
│                              API Call                            │
│                    GET /api/documents/goods-receipts              │
│                    ?filter=[{field,operator,value}]               │
│                    &search=...&orderBy=...                        │
│                    &limit=50&offset=0                             │
└──────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌──────────────────────────────────────────────────────────────────┐
│  BACKEND                                                          │
│                                                                   │
│  Handler.List()                                                  │
│    ├── ParseQueryParams (search, limit, offset, orderBy)         │
│    ├── Parse ?filter= JSON → []filter.Item                       │
│    └── service.List(ctx, filter)                                 │
│            │                                                     │
│            ▼                                                     │
│        repo.List(ctx, filter)                                    │
│            ├── buildWhereConditions(filter)                       │
│            │   ├── Standard filters (deletion_mark, search)       │
│            │   ├── Typed filters (supplierId, warehouseId)        │
│            │   └── buildAdvancedFilterConditions(advFilters)       │
│            │       └── for each filter.Item → squirrel WHERE      │
│            ├── COUNT(*) → totalCount                              │
│            ├── ORDER BY → sorted                                  │
│            └── LIMIT/OFFSET → paginated                           │
└──────────────────────────────────────────────────────────────────┘
```

### 2.2. Принципы проектирования

1. **URL-driven State** (Frontend Guidelines §3.2) — все активные фильтры хранятся в URL.
   - Формат: `?f.counterparty=eq:UUID&f.date=gte:2026-01-01&f.posted=eq:true`
   - Альтернатива (JSON): `?filter=[{"field":"counterparty","operator":"eq","value":"UUID"}]`
   - **Выбор:** JSON-формат (`?filter=...`), так как он уже реализован в `catalog.go`.

2. **Metadata-driven** (Backend Rules §1) — структура полей определяется метаданными.
   - Phase 1: hardcoded `FilterFieldMeta[]` на фронте.
   - Phase 2: API endpoint `/api/metadata/{entity}/fields`.

3. **Generic reuse** — `buildAdvancedFilterConditions()` вынести в shared utility, используемый и `BaseCatalogRepo`, и `BaseDocumentRepo`.

4. **Security** — whitelist валидных column names (protection against SQL injection через `validCols`).

---

## 3. План реализации

### Phase 1: Core Filtering Pipeline (MVP)

> Цель: замкнуть цепочку Frontend → API → SQL для произвольных фильтров.

#### 3.1.1. Backend: Advanced Filters в Document Repository

**Задача:** Добавить поддержку `AdvancedFilters` в `BaseDocumentRepo` и `GoodsReceiptRepo.List()`.

**Файлы:**

| Файл | Изменения |
|------|-----------|
| `internal/infrastructure/storage/postgres/document_repo/base.go` | Добавить `validCols`, `buildAdvancedFilterConditions()` — вынести из `catalog_repo/base.go` в shared |
| `internal/infrastructure/storage/postgres/shared_filter.go` (NEW) | Общая функция `BuildAdvancedFilterConditions(filters []filter.Item, validCols map[string]struct{}) ([]squirrel.Sqlizer, error)` |
| `internal/infrastructure/storage/postgres/catalog_repo/base.go` | Рефакторинг: делегировать вызов `shared_filter.BuildAdvancedFilterConditions()` |
| `internal/infrastructure/storage/postgres/document_repo/goods_receipt.go` | В `List()`: добавить вызов `sharedFilter.BuildAdvancedFilterConditions(filter.AdvancedFilters, r.validCols)` |

**Детали реализации:**

```go
// internal/infrastructure/storage/postgres/shared_filter.go
package postgres

import (
    "fmt"
    "metapus/internal/domain/filter"
    "github.com/Masterminds/squirrel"
)

// BuildAdvancedFilterConditions translates []filter.Item into SQL conditions.
// validCols is a whitelist of allowed column names (SQL injection protection).
func BuildAdvancedFilterConditions(
    filters []filter.Item,
    validCols map[string]struct{},
) ([]squirrel.Sqlizer, error) {
    var conditions []squirrel.Sqlizer
    for _, item := range filters {
        if _, ok := validCols[item.Field]; !ok {
            return nil, fmt.Errorf("invalid filter column: %s", item.Field)
        }
        // ... same switch as in catalog_repo/base.go:buildAdvancedFilterConditions
    }
    return conditions, nil
}
```

#### 3.1.2. Backend: Парсинг `?filter=` в Document Handlers

**Задача:** Добавить парсинг `?filter=` параметра в `GoodsReceiptHandler.List()`.

**Файл:** `internal/infrastructure/http/v1/handlers/goods_receipt.go`

```go
// После парсинга typed фильтров, добавить:
filterJson := c.Query("filter")
if filterJson != "" {
    var advFilters []domainFilter.Item
    if err := json.Unmarshal([]byte(filterJson), &advFilters); err != nil {
        h.Error(c, apperror.NewValidation("invalid filter format"))
        return
    }
    filter.AdvancedFilters = advFilters
}
```

**Аналогично для:** `GoodsIssueHandler.List()`

#### 3.1.3. Frontend: Hook `useUrlFilters`

**Задача:** Создать хук для синхронизации фильтров с URL search params.

**Файл:** `frontend/hooks/useUrlFilters.ts` (NEW)

**Интерфейс:**

```typescript
interface FilterValue {
  field: string       // e.g. "counterparty", "date"
  operator: string    // e.g. "eq", "gte", "contains"
  value: unknown      // e.g. "UUID", "2026-01-01", true
}

interface UseUrlFiltersReturn {
  /** Текущие активные фильтры (из URL) */
  filters: FilterValue[]

  /** Установить значение конкретного фильтра */
  setFilter: (field: string, operator: string, value: unknown) => void

  /** Удалить фильтр по полю */
  removeFilter: (field: string) => void

  /** Очистить все фильтры */
  clearFilters: () => void

  /** Сериализовать фильтры в query string формат для API */
  toApiQuery: () => string
}

export function useUrlFilters(): UseUrlFiltersReturn
```

**Формат URL:** `?filter=[{"field":"name","operator":"contains","value":"тест"}]`

- Совместим с существующим парсингом в `catalog.go:82-91`
- URL-encoded JSON массив

#### 3.1.4. Frontend: Интеграция FilterSidebar → API

**Задача:** Связать изменение значений в `FilterSidebar` с URL-фильтрами и fetch данных.

**Изменения в `FilterSidebar`:**

```typescript
interface FilterSidebarProps {
  filters?: FilterConfig[]        // статические конфигурации фильтров
  fieldsMeta?: FilterFieldMeta[]  // метаданные для диалога настройки
  showGroups?: boolean
  showDetails?: boolean

  // NEW:
  /** Callback при изменении значения фильтра */
  onFilterChange?: (filters: FilterValue[]) => void
  /** Текущие активные значения фильтров */
  activeFilters?: FilterValue[]
}
```

**Изменения на странице списка (`page.tsx`):**

```typescript
export default function GoodsReceiptsListPage() {
  const { filters, setFilter, removeFilter, toApiQuery } = useUrlFilters()

  // Fetch data with filters
  useEffect(() => {
    const query = toApiQuery()
    api.goodsReceipts.list(query).then(setDocuments)
  }, [toApiQuery])

  return (
    <FilterSidebar
      filters={docFilters}
      fieldsMeta={goodsReceiptFieldsMeta}
      activeFilters={filters}
      onFilterChange={...}
    />
  )
}
```

---

### Phase 2: Расширенные возможности фильтрации

#### 3.2.1. Backend: Entity Metadata API

**Задача:** Endpoint для получения структуры полей объекта (для `FilterConfigDialog`).

**Endpoint:** `GET /api/metadata/{entityType}/fields`

**Пример ответа:**

```json
{
  "entityType": "goods-receipt",
  "fields": [
    {
      "key": "number",
      "label": "Номер",
      "fieldType": "string",
      "dbColumn": "number",
      "filterable": true,
      "sortable": true
    },
    {
      "key": "date",
      "label": "Дата",
      "fieldType": "date",
      "dbColumn": "date",
      "filterable": true,
      "sortable": true
    },
    {
      "key": "supplier",
      "label": "Поставщик",
      "fieldType": "reference",
      "dbColumn": "supplier_id",
      "refEntity": "counterparty",
      "filterable": true,
      "sortable": false
    },
    {
      "key": "lines.nomenclature",
      "label": "Номенклатура",
      "fieldType": "reference",
      "dbColumn": "product_id",
      "refEntity": "nomenclature",
      "group": "Товары",
      "filterable": true,
      "joinTable": "doc_goods_receipt_lines",
      "joinCondition": "document_id = id"
    }
  ]
}
```

**Архитектура:**

```
internal/
├── core/
│   └── metadata/
│       ├── field.go          # FieldMeta struct
│       └── registry.go       # EntityFieldRegistry (in-memory registry)
├── domain/
│   └── documents/
│       └── goods_receipt/
│           └── metadata.go   # RegisterGoodsReceiptFields()
└── infrastructure/
    └── http/v1/
        └── handlers/
            └── metadata.go   # MetadataHandler.GetFields()
```

**Обоснование (SAP analogy):** В SAP CDS Views и аннотации `@Consumption.filter` определяют, какие поля доступны для фильтрации. Мы создаём аналогичный реестр метаданных — `EntityFieldRegistry`.

#### 3.2.2. Frontend: Динамическая загрузка метаданных полей

**Задача:** Заменить hardcoded `goodsReceiptFieldsMeta[]` на загрузку с сервера.

```typescript
// hooks/useEntityFields.ts
export function useEntityFields(entityType: string): FilterFieldMeta[] {
  const [fields, setFields] = useState<FilterFieldMeta[]>([])

  useEffect(() => {
    api.metadata.getFields(entityType).then(setFields)
  }, [entityType])

  return fields
}
```

#### 3.2.3. Фильтрация по табличным частям (JOIN-based)

**Задача:** Поддержать фильтры типа "документы, где в товарах есть номенклатура X".

**SQL-подход:**

```sql
SELECT DISTINCT d.*
FROM doc_goods_receipts d
JOIN doc_goods_receipt_lines l ON l.document_id = d.id
WHERE l.product_id = $1
ORDER BY d.date DESC
```

**Backend-реализация:**

```go
// Расширение filter.Item
type Item struct {
    Field    string         `json:"field"`
    Operator ComparisonType `json:"operator"`
    Value    any            `json:"value"`
    // NEW: Для полей из табличных частей
    JoinTable     string `json:"joinTable,omitempty"`
    JoinCondition string `json:"joinCondition,omitempty"`
}
```

**Защита:** Join-таблицы также через whitelist (`validJoinTables`).

#### 3.2.4. Frontend: Reference Lookup в фильтрах

**Задача:** Для `reference`-полей (Поставщик, Склад, Номенклатура) рендерить комбобокс с поиском по справочнику.

**Компонент:** `FilterReferenceInput` (использует `Command` из shadcn/ui)

```typescript
interface FilterReferenceInputProps {
  entityType: string       // "counterparty", "warehouse"
  value: string | null     // selected ID
  onChange: (id: string | null) => void
  placeholder: string
}
```

**Зависимости:** shadcn `Command` (combobox pattern) + API `GET /api/catalogs/{type}?search=...`

---

### Phase 3: UX-полировка

#### 3.3.1. Сохранение настроек фильтрации

| Механизм | Хранение | Описание |
|----------|----------|----------|
| `localStorage` | Browser | Запоминать выбранные фильтры (selectedKeys) per entity type |
| `sys_user_settings` | DB | Серверное хранение настроек пользователя (Phase 3+) |

**localStorage ключ:** `metapus-filters:{entityType}` → `string[]` (selectedKeys)

#### 3.3.2. Быстрые фильтры (Chips)

**Задача:** Отображать активные фильтры как "чипсы" над таблицей (аналог 1С).

```
┌──────────────────────────────────────────────────────────┐
│ ✕ Поставщик: Тест  │ ✕ Период: 01.01 – 31.01  │ 🗑 Сбросить │
└──────────────────────────────────────────────────────────┘
```

Компонент: `ActiveFilterChips` — рендерится в `DataToolbar` или под ним.

#### 3.3.3. Debounce для текстовых фильтров

```typescript
// В useUrlFilters — debounce 300ms для text-полей
const debouncedSetFilter = useMemo(
  () => debounce((field, op, value) => {
    // update URL
  }, 300),
  []
)
```

#### 3.3.4. Оператор сравнения в sidebar

**UX-паттерн (из 1С):** Рядом с каждым фильтром — выпадающий список оператора:
- `=` (Равно) — по умолчанию для reference-полей
- `≠` (Не равно)
- `⊃` (Содержит) — по умолчанию для text-полей
- `≥` / `≤` — для date/number
- `∈` (В списке) — множественный выбор

---

## 4. Mapping: поля → типы фильтров → UI

| `FieldType` | Оператор по умолч. | UI-компонент sidebar | Пример |
|-------------|--------------------|--------------------|--------|
| `string` | `contains` | `Input` (текст) | Комментарий: "основной" |
| `number` | `eq` | `Input` (числовой) + range (от/до) | Количество: от 10 до 100 |
| `date` | `gte` / `lte` | `AccountingPeriodPicker` (range) | Период: 01.01 – 31.01 |
| `boolean` | `eq` | `Switch` / 3-state: Да/Нет/Любой | Проведен: ✓ |
| `reference` | `eq` | `Combobox` (lookup) | Поставщик: [Тест ▼] |
| `enum` | `eq` | `Select` (dropdown) | Операция: [Все ▼] |

---

## 5. Маппинг backend ↔ frontend field keys

| Frontend field key | Backend `filter.Item.Field` (DB column) | Тип |
|--------------------|------------------------------------------|-----|
| `number` | `number` | string |
| `date` | `date` | date |
| `counterparty` | `supplier_id` | reference |
| `contract` | `contract_id` | reference |
| `warehouse` | `warehouse_id` | reference |
| `operation` | — (не реализовано на бэке) | enum |
| `responsible` | — (не реализовано на бэке) | reference |
| `comment` | `description` | string |
| `author` | `created_by` | reference |
| `organization` | `organization_id` | reference |
| `currency` | `currency_id` | reference |
| `includeVat` | `amount_includes_vat` | boolean |
| `posted` | `posted` | boolean |
| `deletionMark` | `deletion_mark` | boolean |
| `lines.nomenclature` | lines JOIN → `product_id` | reference (JOIN) |
| `lines.quantity` | lines JOIN → `quantity` | number (JOIN) |

> **Важно:** Frontend использует camelCase / человекочитаемые ключи, backend — snake_case DB columns. Маппинг выполняется на уровне:
> - Phase 1: Frontend `lib/filterMapping.ts` (hardcoded dictionary)
> - Phase 2: Через Entity Metadata API (сервер возвращает `dbColumn` рядом с `key`)

---

## 6. Порядок реализации (рекомендуемый)

### Phase 1 — MVP (1-2 дня)

```
1. [Backend]  Вынести buildAdvancedFilterConditions в shared_filter.go
2. [Backend]  Добавить validCols в BaseDocumentRepo
3. [Backend]  Добавить ?filter= парсинг в GoodsReceiptHandler.List()
4. [Backend]  Добавить вызов advanced filters в GoodsReceiptRepo.List()
5. [Frontend] Создать useUrlFilters hook
6. [Frontend] Создать filterMapping.ts (frontend key → backend field)
7. [Frontend] Интегрировать FilterSidebar с useUrlFilters  
8. [Frontend] Подключить API-вызов с фильтрами в page.tsx
9. [E2E]      Smoke-test: фильтрация по поставщику, дате, статусу
```

### Phase 2 — Metadata & Reference Lookup (2-3 дня)

```
10. [Backend]  Создать EntityFieldRegistry + metadata handler
11. [Frontend] Создать useEntityFields hook
12. [Frontend] Создать FilterReferenceInput (combobox lookup)
13. [Frontend] Заменить hardcoded fieldsMeta на загрузку с API
14. [Backend]  Реализовать JOIN-фильтрацию для табличных частей
```

### Phase 3 — Полировка (1-2 дня)

```
15. [Frontend] ActiveFilterChips (визуализация активных фильтров)
16. [Frontend] Debounce для текстовых фильтров
17. [Frontend] localStorage persistence для selectedFilterKeys
18. [Frontend] Выбор оператора сравнения в sidebar
19. [E2E]      Полные тесты фильтрации
```

---

## 7. Риски и решения

| Риск | Вероятность | Решение |
|------|-------------|---------|
| SQL Injection через `filter.Item.Field` | Высокая (если забыть whitelist) | `validCols` whitelist **обязателен**. Unit-тест на отклонение невалидных column names |
| N+1 при JOIN-фильтрации (табличные части) | Средняя | `EXISTS (SELECT 1 FROM lines WHERE ...)` вместо `JOIN` + `DISTINCT` |
| Сложность URL при множественных фильтрах | Средняя | JSON-формат `?filter=[...]` — компактнее, чем `?f.field=op:val` |
| Несинхронизация field naming frontend ↔ backend | Высокая | Phase 2: единый metadata API решает проблему. Phase 1: строгий `filterMapping.ts` |
| Большие справочники в reference lookup | Средняя | Lazy search (debounce 300ms, limit 20), а не загрузка всего справочника |

---

## 8. Файловая структура после реализации

```
# Backend (новые файлы выделены *)
internal/
├── core/
│   └── metadata/
│       ├── field.go *         # FieldMeta struct
│       └── registry.go *      # EntityFieldRegistry
├── domain/
│   ├── filter/
│   │   └── types.go           # ComparisonType, Item (без изменений Phase 1)
│   └── documents/
│       └── goods_receipt/
│           └── metadata.go *  # RegisterGoodsReceiptFields()
└── infrastructure/
    ├── storage/postgres/
    │   ├── shared_filter.go * # BuildAdvancedFilterConditions()
    │   ├── catalog_repo/
    │   │   └── base.go        # Рефакторинг: делегирует в shared_filter
    │   └── document_repo/
    │       ├── base.go        # + validCols, + вызов shared_filter
    │       └── goods_receipt.go # + AdvancedFilters
    └── http/v1/
        └── handlers/
            ├── goods_receipt.go  # + парсинг ?filter=
            └── metadata.go *     # MetadataHandler

# Frontend (новые файлы выделены *)
frontend/
├── hooks/
│   ├── useUrlFilters.ts *     # URL-driven filter state
│   └── useEntityFields.ts *   # Load metadata from API (Phase 2)
├── lib/
│   ├── api.ts                 # + metadata endpoint
│   └── filterMapping.ts *     # Frontend key → backend column mapping
├── components/shared/
│   ├── filter-sidebar.tsx     # + onFilterChange, activeFilters
│   ├── filter-config-dialog.tsx # Без изменений
│   ├── active-filter-chips.tsx * # Чипсы активных фильтров (Phase 3)
│   └── filter-reference-input.tsx * # Combobox lookup (Phase 2)
└── app/purchases/goods-receipts/
    └── page.tsx               # + useUrlFilters, + API integration
```

---

## 9. Аналоги в ERP-системах

| Функционал | 1С:Предприятие | SAP Fiori | ERPNext | Metapus |
|------------|----------------|-----------|---------|---------|
| Настройка фильтров | «Настройка отбора» — два столбца | FilterBar + Adapt Filters | Filter Area | `FilterConfigDialog` |
| Хранение настроек | Variant (серверное) | LREP / Personalization | User Settings | localStorage → sys_user_settings |
| Операторы сравнения | 12+ операторов | Standard Operators | contains/= | 14 (`filter.ComparisonType`) |
| Фильтрация по ТЧ | ОтборПоТабличнойЧасти | Associations | Child таблицы | JOIN + EXISTS (Phase 2) |
| URL-driven | Нет (1С) | ✅ (HashParams) | ✅ (?filters) | ✅ (`?filter=`) |
| Метаданные полей | Конфигуратор / DDL | CDS Annotations | DocType | EntityFieldRegistry (Phase 2) |
