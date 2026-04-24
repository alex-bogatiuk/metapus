# 28 — Reporting System (Query Engine)

> **Аналог:** Система компоновки данных (СКД) в 1С:Предприятие.
> Пользователь декларирует Dataset → платформа автоматически строит SQL,
> генерирует UI (фильтры, колонки, группировки), экспорт и варианты отчётов.

---

## 1. Архитектура

```
┌─────────────────────────────────────────────────────────────────────┐
│                        Frontend (Next.js)                           │
│                                                                     │
│  useReportPage(key)                                                 │
│    ├── GET /reports/{key}/metadata  →  ReportMeta (auto UI)         │
│    ├── POST /reports/{key}          →  QueryRequest → QueryResult   │
│    ├── POST /reports/{key}/export   →  QueryRequest → XLSX stream   │
│    ├── POST /reports/{key}/grouped  →  QueryRequest → DisplayRow[]  │
│    ├── GET  /reports/{key}/variants →  ReportVariant[]               │
│    └── POST /reports/variants       →  CRUD вариантов               │
│                                                                     │
│  ReportPage (report-page.tsx)                                       │
│    ├── FilterSidebar (advanced filters, reference pickers)          │
│    ├── ReportTable (group headers, subtotals, footer)               │
│    ├── Field Selector (auto-discovery tree)                         │
│    ├── Column Chooser / Resize                                      │
│    ├── Export (XLSX with Control Breaks)                             │
│    └── Variants (save/load/share named presets)                     │
└─────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│                     Backend (Go / Gin)                               │
│                                                                     │
│  DatasetReportHandler                                               │
│    ├── HandleMeta(key)    → DatasetToMeta() + BuildFieldTree()      │
│    ├── HandleExecute()    → Compiler.Execute(QueryRequest)          │
│    ├── HandleExport(key)  → Compiler.Execute() → export.XLSX()     │
│    └── HandleGrouped(key) → Compiler.Execute() → BuildDisplayRows()│
│                                                                     │
│  Compiler (Query Engine)                                            │
│    ├── Validate field paths (whitelist against metadata)            │
│    ├── Resolve reference paths → JoinSteps (auto-discovery)        │
│    ├── Build SQL (squirrel) or delegate to DatasetExecutor          │
│    ├── Append LEFT JOINs for dereferenced ref fields                │
│    ├── Apply AdvancedFilters (push-down to SQL WHERE)               │
│    ├── GROUP BY, ORDER BY, LIMIT/OFFSET                            │
│    └── Execute → scan pgx.Rows → []map[string]interface{}          │
│                                                                     │
│  ReportVariant Service                                              │
│    ├── personal / shared / system variants                          │
│    └── Stored in sys_report_variants table                          │
└─────────────────────────────────────────────────────────────────────┘
```

### Ключевые решения

| Аспект | Решение | Обоснование |
|--------|---------|-------------|
| SQL | squirrel builder, не ORM | Контроль над JOINs, CTEs, push-down фильтрация |
| Типы данных | `[]map[string]interface{}` | Generic: один код для всех отчётов |
| Группировка ≤5000 | Client-side (`report-grouping.ts`) | Мгновенный отклик при смене группировки |
| Группировка >5000 | Server-side (`BuildDisplayRows` в Go) | Защита от OOM в браузере |
| Экспорт | Streaming XLSX (excelize StreamWriter) | O(1) память даже для 100K+ строк |
| Дерево полей | Auto-Discovery из metadata.Registry | Новые справочники → автоматически доступны в отчётах |

---

## 2. Слой Schema — Декларация отчётов

### 2.1 Dataset (schema.Dataset)

Файл: `internal/domain/reports/schema/types.go`

```go
type Dataset struct {
    Key             string           // "stock-balance"
    Name            string           // "Остатки товаров"
    Description     string
    BaseTable       string           // SQL-таблица (игнорируется если есть Executor)
    Permission      string           // "report:stock:read"
    Fields          []Field          // Поля датасета
    Filters         []FilterDef      // Параметрические фильтры (as_of_date и т.д.)
    ScopeDimensions []string         // RLS-измерения ["warehouse"]
    DefaultGroupBy  []string         // Группировка по умолчанию
    DefaultSort     *SortDef
    ExportFormats   []string         // ["csv", "xlsx"]
    Executor        DatasetExecutor  // Кастомный SQL-билдер (CTE, UNION)
}
```

### 2.2 Field

```go
type Field struct {
    Name       string    // SQL-колонка: "warehouse_id"
    Label      string    // "Склад"
    Kind       FieldKind // dimension | measure | attribute
    Type       FieldType // string | date | quantity | money | ref | boolean | enum
    RefEntity  string    // "warehouse" — для Type==ref
    Agg        AggFunc   // sum | count | avg | min | max — для Kind==measure
    Hidden     bool      // Скрыт по умолчанию
    Sortable   bool
    Scale      int       // Количество знаков (quantity=4, money=2)
    Alias      string    // Переопределение имени в результате
    FilterOnly bool      // Параметр, не колонка (e.g. as_of_date)
}
```

**FieldKind:**
- `dimension` — группируемое/фильтруемое поле (склад, товар)
- `measure` — агрегируемое числовое поле (количество, сумма)
- `attribute` — информационное поле (описание, артикул)

### 2.3 DatasetExecutor (опционально)

```go
type DatasetExecutor interface {
    BuildQuery(ctx context.Context, params map[string]interface{}) (squirrel.SelectBuilder, error)
}
```

Используется для сложных датасетов:
- **CTE** — остатки товаров (SUM movements WHERE period ≤ date)
- **UNION ALL** — журнал документов (несколько таблиц)
- **Параметрические подзапросы** — период зависит от фильтров

Простые датасеты (SELECT из одной таблицы) не реализуют Executor — Compiler генерирует SQL автоматически из `BaseTable + Fields`.

### 2.4 Регистрация датасетов

Файл: `internal/content/report_datasets.go`

```go
var StockBalanceDataset = schema.Dataset{
    Key:         "stock-balance",
    Name:        "Остатки товаров",
    Permission:  "report:stock:read",
    Executor:    &executors.StockBalanceExecutor{},
    Fields: []schema.Field{
        {Name: "warehouse_id", Label: "Склад", Kind: schema.FieldDimension, Type: schema.TypeRef, RefEntity: "warehouse"},
        {Name: "product_id",   Label: "Товар", Kind: schema.FieldDimension, Type: schema.TypeRef, RefEntity: "nomenclature"},
        {Name: "quantity",     Label: "Остаток", Kind: schema.FieldMeasure, Type: schema.TypeQuantity, Agg: schema.AggSum},
    },
    Filters: []schema.FilterDef{
        {Key: "as_of_date", Label: "Дата отчёта", Type: schema.FilterDate},
    },
    // ...
}
```

---

## 3. Compiler — Query Engine

Файл: `internal/domain/reports/compiler/compiler.go`

### 3.1 QueryRequest (JSON от frontend)

```go
type QueryRequest struct {
    Dataset         string                    // "stock-balance"
    Select          []string                  // ["warehouse_id.name", "product_id.name", "quantity"]
    GroupBy         []string                  // ["warehouse_id.name"]
    OrderBy         string                    // "quantity"
    OrderDir        string                    // "desc"
    Filters         map[string]interface{}    // {"as_of_date": "2025-01-01"}
    AdvancedFilters []filter.Item             // Типизированные фильтры из FilterSidebar
    Limit           int
    Offset          int
    ExportColumns   []string                  // Порядок колонок для экспорта
    ExportGroupBy   []string                  // Группировка для Control Breaks
}
```

### 3.2 Пайплайн выполнения

```
Execute(QueryRequest)
  1. Определить selected fields (default: все non-hidden)
  2. Resolve field paths → SELECT expressions + JoinSteps
  3. Построить base query:
     ├── DatasetExecutor.BuildQuery()  — если есть Executor
     └── SELECT FROM BaseTable AS base — если нет
  4. Добавить SELECT columns
  5. Применить AdvancedFilters → WHERE clauses (push-down)
  6. Добавить LEFT JOINs из resolver (select + filters)
  7. GROUP BY
  8. ORDER BY (с учётом ExportGroupBy для Control Breaks)
  9. LIMIT / OFFSET
 10. Execute SQL → scan pgx.Rows → []map[string]interface{}
```

### 3.3 Auto-Discovery (Reference Resolution)

Файл: `internal/domain/reports/compiler/discovery.go`

При обнаружении поля `Type==ref` (e.g. `warehouse_id`), система рекурсивно обходит `metadata.Registry` и строит дерево доступных полей:

```
warehouse_id (Склад) [ref]
├── name (Наименование) [string]
├── code (Код) [string]
└── ...
product_id (Товар) [ref]
├── name (Наименование) [string]
├── article (Артикул) [string]
├── brand_id (Бренд) [ref]
│   ├── name (Наименование) [string]
│   └── country_id (Страна) [ref] ← depth=3, STOP
└── base_unit_id (Ед. изм.) [ref]
    └── name (Наименование) [string]
```

**MaxJoinDepth = 3** — ограничение глубины JOIN-цепочек.

Пользователь может выбрать `product_id.brand_id.name` → Compiler автоматически добавит:
```sql
LEFT JOIN cat_nomenclature AS j1 ON base.product_id = j1.id
LEFT JOIN cat_brands AS j2 ON j1.brand_id = j2.id
```

### 3.4 MetaBuilder (DatasetToMeta)

Файл: `internal/domain/reports/compiler/meta_builder.go`

Преобразует `schema.Dataset` → `platform.ReportMeta`:
- `Fields` → `Columns` (с авто-дереференсом ref → `.name`)
- `Filters` + dimension fields → `Filters`
- Dimension fields → `GroupBy`
- Measure fields с `Agg` → `Totals`
- Auto-Discovery → `AvailableFields` tree

---

## 4. Server-side Grouping Engine

Файл: `internal/domain/reports/compiler/grouping.go`

Порт `frontend/lib/report-grouping.ts` на Go. Используется эндпоинтом `/grouped` для датасетов >5000 строк.

```go
// BuildDisplayRows — рекурсивная группировка с subtotals и footer
func BuildDisplayRows(items []map[string]interface{}, groupByKeys []string, totalDefs []platform.ReportTotal) []DisplayRow

// SortItems — сортировка с числовым и строковым сравнением
func SortItems(items []map[string]interface{}, column string, direction string) []map[string]interface{}
```

**DisplayRow** — JSON-ответ, идентичный frontend-типу:

```go
type DisplayRow struct {
    Kind   string                 `json:"kind"`   // "group" | "data" | "subtotal" | "footer"
    Depth  int                    `json:"depth"`
    Label  string                 `json:"label"`
    Count  int                    `json:"count"`
    Item   map[string]interface{} `json:"item"`
    Totals map[string]float64     `json:"totals"`
}
```

---

## 5. Export Engine

Файл: `internal/domain/reports/export/export.go`

### XLSX Export (StreamWriter)

- **O(1) память** — использует `excelize.StreamWriter` для потоковой записи
- **Control Breaks** — подитоги по группам (через `ExportGroupBy`)
- **Порядок колонок** — определяется `ExportColumns` из frontend
- **Стилизация** — заголовки, числовой формат, группировка строк (OutlineLevel)

```
POST /reports/{key}/export
Body: QueryRequest (с ExportColumns + ExportGroupBy)
Response: attachment; filename="stock-balance.xlsx"
```

---

## 6. Report Variants

Файл: `internal/domain/reports/variants/`

### Модель

```go
type ReportVariant struct {
    ID          uuid.UUID      // PK
    DatasetKey  string         // "stock-balance"
    Name        string         // "Основной"
    AuthorID    *uuid.UUID     // NULL для system
    Visibility  Visibility     // personal | shared | system
    IsDefault   bool
    Config      VariantConfig  // JSON config
    Version     int
}

type VariantConfig struct {
    SelectedFields  []string                // Выбранные поля
    VisibleColumns  []string                // Видимые колонки
    GroupBy         []string                // Группировка
    SortColumn      *string
    SortDirection   string
    Filters         map[string]interface{}  // Параметры фильтров
    AdvancedFilters []filter.Item           // Типизированные фильтры
}
```

### Visibility

| Тип | Кто видит | Кто редактирует |
|-----|-----------|-----------------|
| `personal` | Только автор | Только автор |
| `shared` | Все пользователи | Автор |
| `system` | Все пользователи | Никто (через API) |

### API

| Метод | Путь | Описание |
|-------|------|----------|
| GET | `/reports/{key}/variants` | Список вариантов (personal + shared + system) |
| POST | `/reports/variants` | Создание варианта |
| PUT | `/reports/variants/{id}` | Обновление |
| DELETE | `/reports/variants/{id}` | Удаление |

---

## 7. HTTP Routes

Все отчёты доступны под `/api/v1/reports/{dataset-key}`:

| Метод | Путь | Описание |
|-------|------|----------|
| GET | `/reports/{key}/metadata` | Метаданные (ReportMeta + AvailableFields) |
| POST | `/reports/{key}` | Выполнение (QueryRequest → QueryResult) |
| POST | `/reports/{key}/export` | Экспорт XLSX (streaming) |
| POST | `/reports/{key}/grouped` | Server-side группировка (DisplayRow[]) |
| GET | `/reports/{key}/variants` | Список вариантов |

Каждый dataset защищён `middleware.RequirePermission(ds.Permission)`.

---

## 8. Frontend

### 8.1 useReportPage (Orchestration Hook)

Файл: `frontend/hooks/useReportPage.ts`

Инкапсулирует весь lifecycle отчёта:
- Загрузка метаданных (`GET /metadata`)
- Управление состоянием фильтров (URL-backed для shareability)
- Выполнение отчёта (`POST /reports/{key}`)
- Client-side/server-side группировка (adaptive)
- Управление колонками (visibility, resize, field selection)
- Export (XLSX with column order + grouping)
- Варианты (CRUD через API + local state)
- Drill-down (row selection → detail panel)
- View mode toggle (table / chart)
- URL sharing (auto-generate on open, copy link)

**Adaptive Grouping Strategy:**

| Строк | Стратегия | Где |
|-------|-----------|-----|
| ≤ 5000 | `buildDisplayRows()` | Браузер (~50ms) |
| 5001–50000 | `POST /reports/{key}/grouped` | Go сервер (~10ms) |
| > 50000 | `status = "export-only"` | Только экспорт |

**ReportStatus:** `"idle" | "loading" | "done" | "empty" | "error" | "export-only"`

### 8.2 ReportPage (UI Component)

Файл: `frontend/components/shared/report-page.tsx`

Полностью metadata-driven:
- **FilterSidebar** — advanced filters с reference pickers, conditions (equals, contains, between, etc.)
- **Field Selector** — дерево доступных полей (auto-discovery)
- **ReportTable** — group headers (collapsible), data rows, subtotals, grand total footer
- **Column Chooser** — toggle visibility, drag reorder
- **Column Resize** — persistent column widths
- **Export** — XLSX с текущими колонками и группировкой
- **Variants** — save/load/delete named presets (personal/shared)
- **Drill-Down** — click row → Sheet panel with all fields
- **Chart view** — toggle table ↔ chart
- **URL Sharing** — auto-generate from URL params, copy shareable link

### 8.3 Report Grouping (Client-side)

Файл: `frontend/lib/report-grouping.ts`

```ts
// Рекурсивная группировка + subtotals + footer
export function buildDisplayRows(
    items: Record<string, unknown>[],
    groupByKeys: string[],
    totalDefs: ReportTotalDef[],
): DisplayRow[]

// Сортировка по колонке
export function sortItems(
    items: Record<string, unknown>[],
    column: string,
    direction: "asc" | "desc",
): Record<string, unknown>[]
```

### 8.4 TypeScript Types

Файл: `frontend/types/report-meta.ts`

```ts
interface ReportMeta {
    key: string
    name: string
    filters: ReportFilterDef[]
    columns: ReportColumnDef[]
    groupBy: ReportGroupByDef[]
    totals: ReportTotalDef[]
    exportFormats: string[]
    scopeDimensions: string[]
    defaultSort?: ReportSortDef
    availableFields?: FieldTreeNode[]  // Auto-discovery tree
}

type DisplayRow =
    | { kind: "group";    depth: number; label: string; count: number; subtotals: Record<string, number> }
    | { kind: "data";     depth: number; item: Record<string, unknown> }
    | { kind: "subtotal"; depth: number; totals: Record<string, number> }
    | { kind: "footer";   totals: Record<string, number> }
```

### 8.5 Page Route

Файл: `frontend/app/(main)/reports/[key]/page.tsx`

Dynamic route: `/reports/stock-balance`, `/reports/stock-turnover`, `/reports/document-journal`

```tsx
const report = useReportPage(params.key)
return <ReportPage report={report} />
```

---

## 9. Существующие датасеты

Файл: `internal/content/report_datasets.go`

| Key | Название | Executor | Поля |
|-----|----------|----------|------|
| `stock-balance` | Остатки товаров | `StockBalanceExecutor` (CTE) | warehouse, product, quantity |
| `stock-turnover` | Оборотная ведомость | `StockTurnoverExecutor` (CTE) | warehouse, product, opening/receipt/expense/closing |
| `document-journal` | Журнал документов | `DocumentJournalExecutor` (UNION) | type, number, date, counterparty, warehouse, amount |

---

## 10. Как добавить новый отчёт

### Шаг 1: Описать Dataset

```go
// internal/content/report_datasets.go
var MyReportDataset = schema.Dataset{
    Key:        "my-report",
    Name:       "Мой отчёт",
    Permission: "report:my-report:read",
    Fields: []schema.Field{
        {Name: "date",   Label: "Дата",  Kind: schema.FieldDimension, Type: schema.TypeDate, Sortable: true},
        {Name: "amount", Label: "Сумма", Kind: schema.FieldMeasure,   Type: schema.TypeMoney, Agg: schema.AggSum},
    },
}
```

### Шаг 2: (Опционально) DatasetExecutor

Если нужен CTE/UNION:

```go
type MyReportExecutor struct{}
func (e *MyReportExecutor) BuildQuery(ctx context.Context, params map[string]interface{}) (squirrel.SelectBuilder, error) {
    // Build CTE, apply params, return squirrel builder
    // MUST alias as "base"
}
```

### Шаг 3: Зарегистрировать

Добавить в массив `datasets` в `registerReportRoutes()` в `router.go`.

### Шаг 4: Seed-миграция (permissions)

```sql
INSERT INTO auth_permissions (code, name, category)
VALUES ('report:my-report:read', 'Просмотр отчёта "Мой отчёт"', 'reports');
```

### Шаг 5: Навигация (sidebar)

Добавить элемент в конфигурацию `app-sidebar.tsx` с `url: "/reports/my-report"`.

**Результат:** UI (фильтры, таблица, группировка, экспорт, варианты) генерируется автоматически. **Ноль frontend-кода для нового отчёта.**

---

## 11. Пакеты и файловое дерево

```
internal/
  platform/
    report_contract.go          # ReportMeta, ReportColumn, ReportFilter, etc.
  domain/reports/
    schema/
      types.go                  # Dataset, Field, FieldKind, FieldType, FilterDef
      executor.go               # DatasetExecutor interface
    compiler/
      compiler.go               # Query Engine (Compiler.Execute)
      resolver.go               # Field path resolution + JoinSteps
      discovery.go              # BuildFieldTree (auto-discovery)
      meta_builder.go           # DatasetToMeta (Dataset → ReportMeta)
      grouping.go               # Server-side grouping (BuildDisplayRows)
    export/
      export.go                 # XLSX streaming export with Control Breaks
    variants/
      model.go                  # ReportVariant, VariantConfig, Visibility
      repository.go             # Repository interface
      service.go                # Business logic (Create/Update/Delete/GetList)
  content/
    report_datasets.go          # StockBalance/Turnover/Journal dataset definitions
  infrastructure/http/v1/
    handlers/
      dataset_handler.go        # DatasetReportHandler (Meta/Execute/Export/Grouped)
      report_variant_handler.go # ReportVariantHandler (CRUD)

frontend/
  types/
    report-meta.ts              # ReportMeta, DisplayRow, ReportStatus, FieldTreeNode
  hooks/
    useReportPage.ts            # Orchestration hook (909 lines)
  lib/
    report-grouping.ts          # Client-side grouping + sorting
    report-filter-adapter.ts    # ReportMeta → FilterFieldMeta adapter
  components/shared/
    report-page.tsx             # Full report UI (filters, table, variants, export)
  app/(main)/reports/
    [key]/page.tsx              # Dynamic route container
```

---

## 12. Security

### RLS (Row-Level Security)

Каждый Dataset объявляет `ScopeDimensions` (e.g. `["warehouse"]`).
Перед выполнением отчёта `DatasetReportHandler` проверяет `DataScope` пользователя:
- Admin → bypass
- Dimension не ограничена → pass
- Dimension ограничена до пустого набора → deny (403)
- Dimension ограничена → pass (фильтрация на уровне SQL)

### Permissions

Каждый Dataset имеет `Permission` (e.g. `report:stock:read`).
Проверяется через `middleware.RequirePermission()` до выполнения запроса.

### Variant Security

- **Personal** — только автор видит и редактирует
- **Shared** — все видят, только автор редактирует
- **System** — все видят, никто не редактирует через API

---

## 13. Performance

| Механизм | Описание |
|----------|----------|
| Push-down фильтрация | AdvancedFilters компилируются в SQL WHERE (не post-fetch) |
| Streaming export | `excelize.StreamWriter` — O(1) память |
| Adaptive grouping | Client <5K, Server 5-50K, Export-only >50K |
| pgx.Rows scan | Без промежуточных структур, прямой scan в `map[string]interface{}` |
| JOIN whitelist | Paths валидируются против metadata graph (нет произвольных JOINs) |
| MaxJoinDepth=3 | Ограничение глубины авто-JOIN цепочек |
