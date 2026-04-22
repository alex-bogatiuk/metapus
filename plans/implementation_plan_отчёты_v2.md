# Система отчётов Metapus — Идеальная реализация

## Анализ черновика

Черновик содержит **сильный концептуальный фундамент**: типизированный `ReportRegistration[F, R]`, metadata-driven UI, streaming export, RLS-интеграция. Ниже — усиленный план, устраняющий слабые места и добавляющий возможности, которых нет у конкурентов.

```
💡 ERP Insight: Архитектура отчётной подсистемы
│ 1С:      СКД — лучшая концепция (декларативная схема → платформа строит SQL/группировки/итоги).
│          Но: runtime-только типизация, нет compile-time проверок, XML-based макеты.
│ SAP:     CDS View + @Analytics аннотации → Fiori ALP автоматически строит UI.
│          Но: тяжёлая инфраструктура, ABAP, огромный порог вхождения.
│ ERPNext: Script Report / Query Report — максимальная гибкость, но нулевая типизация.
│          Report Builder — UI-конструктор, но ограниченный.
│ Odoo:    read_group() + Pivot/Graph views — элегантно, но N+1 в сложных отчётах.
│ ────────────────────
│ Metapus: Go generics дают compile-time контракт Filter→Result.
│          CODE IS METADATA — ReportMeta из Go struct, не из БД.
│          Streaming export через pgx.Rows → нет лимитов на объём.
│          RLS применяется платформой ДО Execute() — нет дыр как в ERPNext/Odoo.
│ Превосходство: типобезопасность + производительность + декларативность.
```

---

## Критические замечания к черновику

🔍 **Нелогичность**: Дублирование DI в каждом ReportRegistration
└─ **Файл**: [register_registrations.go](file:///c:/Users/user/go/src/metapus/internal/content/register_registrations.go)
└─ **Проблема**: Каждый отчёт создаёт `NewReportRepo()` + `NewService()` + `NewHandler()` — копипаста, нарушение DRY
└─ **Решение**: Generic `ReportHandler[F, R]` на платформенном уровне, DI автоматически

⚡ **Оптимизация**: `float64` для количеств в отчётах
└─ **Текущее**: `Quantity float64` в `StockBalanceReportItem` — потеря точности
└─ **Решение**: Использовать `types.Quantity` (int64 × 10000) как в остальной системе
└─ **Выигрыш**: Консистентность с регистрами, нет ошибок округления

🐛 **Критический риск**: Отсутствие RLS в текущих отчётах
└─ **Сценарий**: Пользователь с ограниченным доступом к складам видит ВСЕ остатки
└─ **Решение**: `ScopeDimensions` в ReportMeta → платформа инжектит WHERE-условия

🔗 **Цепочка ERP**: Отсутствие Fast Path для текущей даты
└─ **Проблема**: Всегда CTE по movements — O(N) вместо O(1) из reg_stock_balances
└─ **ERP-опыт**: 1С автоматически выбирает: итоги (быстро) vs пересчёт (точно)
└─ **Решение**: `isCurrentDate(filter) → fromBalancesTable()` в executor

---

## Принятые архитектурные решения

### 1. Кэширование результатов → **Не нужно**

Redis в проекте отсутствует. Единственный in-memory кэш — LRU для CEL-программ в `automation.Engine`. В архитектуре Database-per-Tenant инвалидация кэша при каждом проведении документа создаёт сложность, не оправданную выигрышем. **Fast Path** (`reg_stock_balances` для текущей даты, O(1)) полностью решает проблему производительности.

### 2. Асинхронные отчёты → **WebSocket есть, механизм — после снятия cap**

WebSocket Hub (`hub.BroadcastToUser`) уже реализован и работает. При текущем cap в 1000 строк (`service.go` L28-33) отчёт физически не может выполняться >30s. Async-режим актуален только после реализации streaming export (фазы 9-10), когда cap снимается.

> [!TIP]
> При снятии cap: горутина + `hub.BroadcastToUser(tenantID, userID, "report:done", payload)`. Отдельный SSE не нужен.

### 3. Граница frontend группировки → **5000 строк**

При cap 1000 строк проблема не возникает. После снятия cap — пороговая схема:

| Строк | Стратегия |
|-------|----------|
| < 5000 | Всё на фронте (`buildDisplayRows()`, <50ms) |
| 5000–50000 | Сервер возвращает pre-grouped JSON (`/reports/{key}/grouped`) |
| > 50000 | Только streaming export, без интерактивной таблицы |

> [!IMPORTANT]
> Серверный fallback `/grouped` реализуется одновременно со снятием cap на 1000 строк, не раньше.

---

## Фаза 1: Backend — Report Contract (platform layer)

> Новый типизированный контракт `ReportRegistration[F, R]` на уровне `internal/platform/`.

### [NEW] [report_contract.go](file:///c:/Users/user/go/src/metapus/internal/platform/report_contract.go)

Ключевые типы:

```go
// ReportRegistration[F, R] — типизированный контракт отчёта.
// F — тип фильтра (парсится из query params), R — тип результата.
type ReportRegistration[F any, R any] interface {
    RoutePrefix() string          // URL segment: "stock-balance"
    Permission() string           // "report:stock:read"
    Meta() ReportMeta             // декларативное описание
    Execute(ctx context.Context, filter F) (R, error)
}

type ReportMeta struct {
    Name            string
    Description     string
    Filters         []ReportFilterDef
    Columns         []ReportColumnDef
    GroupBy         []ReportGroupByDef
    Totals          []ReportTotalDef
    ExportFormats   []string          // ["csv", "xlsx"]
    ScopeDimensions []string          // RLS: ["warehouse", "organization"]
    DefaultSort     *ReportSortDef    // дефолтная сортировка
}

type ReportFilterDef struct {
    Key      string `json:"key"`
    Type     string `json:"type"`      // "date", "reference", "boolean", "period", "enum"
    Label    string `json:"label"`
    Required bool   `json:"required,omitempty"`
    Ref      string `json:"ref,omitempty"`      // entity name for reference picker
    Multi    bool   `json:"multi,omitempty"`
    Default  any    `json:"default,omitempty"`   // default value
}

type ReportColumnDef struct {
    Key           string `json:"key"`
    Label         string `json:"label"`
    Type          string `json:"type"`      // "string", "quantity", "money", "date", "reference"
    Align         string `json:"align,omitempty"`
    Sortable      bool   `json:"sortable,omitempty"`
    DefaultHidden bool   `json:"defaultHidden,omitempty"`
    Format        string `json:"format,omitempty"`   // "number", "currency", "percent"
}

type ReportGroupByDef struct {
    Key           string `json:"key"`
    Label         string `json:"label"`
    DefaultActive bool   `json:"defaultActive,omitempty"`
}

type ReportTotalDef struct {
    Column string `json:"column"`
    Func   string `json:"func"`    // "sum", "count", "avg", "min", "max"
    Label  string `json:"label,omitempty"`
}

type ReportSortDef struct {
    Column    string `json:"column"`
    Direction string `json:"direction"` // "asc", "desc"
}
```

**Важно**: `ReportRegistration` — это Go interface с type parameters. Но `FactoryRegistry` хранит `[]RouteRegistration` (без generics). Решение: **wrapper-функция** `RegisterTypedReport[F, R]()` которая создаёт type-erased обёртку.

### [MODIFY] [factory_registry.go](file:///c:/Users/user/go/src/metapus/internal/infrastructure/http/v1/factory_registry.go)

Добавить:
```go
// RegisterTypedReport wraps a typed ReportRegistration into a RouteRegistration
// and registers it. The platform automatically:
//   - creates GET /{prefix} endpoint → Execute()
//   - creates GET /{prefix}/export endpoint → streaming CSV/XLSX
//   - creates GET /metadata/reports/{prefix} → Meta()
//   - applies RequirePermission(Permission())
//   - applies DataScope (RLS) from ScopeDimensions
func RegisterTypedReport[F any, R any](
    reg *FactoryRegistry,
    report platform.ReportRegistration[F, R],
) { ... }
```

---

## Фаза 2: Backend — Generic Report Handler (infrastructure)

### [NEW] `internal/infrastructure/http/v1/handlers/report_handler.go`

Generic handler, который:
1. Парсит query params → `F` через `reflect` + struct tags
2. Применяет RLS из `ScopeDimensions` + `security.DataScope` из context
3. Вызывает `Execute(ctx, filter)`
4. Сериализует результат `R` как JSON

```go
type GenericReportHandler[F any, R any] struct {
    report platform.ReportRegistration[F, R]
}

func (h *GenericReportHandler[F, R]) HandleExecute(c *gin.Context) {
    filter, err := parseReportFilter[F](c)  // query params → struct
    // ... apply RLS, call Execute, respond JSON
}

func (h *GenericReportHandler[F, R]) HandleExport(c *gin.Context) {
    // streaming: pgx.Rows → csv.Writer → http.ResponseWriter
    // Content-Disposition: attachment
}

func (h *GenericReportHandler[F, R]) HandleMeta(c *gin.Context) {
    c.JSON(http.StatusOK, h.report.Meta())
}
```

### [NEW] `internal/infrastructure/http/v1/handlers/report_export.go`

Streaming export engine:
- **CSV**: `encoding/csv` → `http.ResponseWriter` напрямую, row-by-row
- **XLSX**: `excelize` library, streaming API, flush каждые N строк
- Заголовки из `ReportMeta.Columns`
- Форматирование: `quantity` → `fmtQuantity()`, `money` → `fmtMoney()`

---

## Фаза 3: Backend — Конкретные отчёты (domain)

### [MODIFY] `internal/domain/reports/stock_balance.go` (рефакторинг из types.go)

```go
type StockBalanceReport struct{}

func (r *StockBalanceReport) RoutePrefix() string  { return "stock-balance" }
func (r *StockBalanceReport) Permission() string   { return "report:stock:read" }

func (r *StockBalanceReport) Meta() platform.ReportMeta {
    return platform.ReportMeta{
        Name: "Остатки товаров",
        Filters: []platform.ReportFilterDef{
            {Key: "asOfDate", Type: "date", Label: "На дату"},
            {Key: "warehouseIds", Type: "reference", Label: "Склад", Ref: "warehouse", Multi: true},
            {Key: "productIds", Type: "reference", Label: "Товар", Ref: "nomenclature", Multi: true},
            {Key: "excludeZero", Type: "boolean", Label: "Скрыть нулевые"},
        },
        Columns: []platform.ReportColumnDef{
            {Key: "warehouseName", Label: "Склад", Type: "string", Sortable: true},
            {Key: "productName", Label: "Товар", Type: "string", Sortable: true},
            {Key: "productSku", Label: "Артикул", Type: "string", DefaultHidden: true},
            {Key: "unitName", Label: "Ед.", Type: "string"},
            {Key: "quantity", Label: "Остаток", Type: "quantity", Align: "right"},
        },
        GroupBy: []platform.ReportGroupByDef{
            {Key: "warehouseName", Label: "По складу", DefaultActive: true},
        },
        Totals: []platform.ReportTotalDef{
            {Column: "quantity", Func: "sum"},
        },
        ExportFormats:   []string{"csv", "xlsx"},
        ScopeDimensions: []string{"warehouse"},
    }
}

func (r *StockBalanceReport) Execute(ctx context.Context, filter StockBalanceFilter) (*StockBalanceResult, error) {
    // Fast path: текущая дата → reg_stock_balances
    // Historical path: CTE по reg_stock_movements
}
```

### [MODIFY] `internal/content/register.go`

Регистрация становится 1 строка:
```go
v1.RegisterTypedReport(reg, &StockBalanceReport{})
v1.RegisterTypedReport(reg, &StockTurnoverReport{})
v1.RegisterTypedReport(reg, &DocumentJournalReport{})
```

---

## Фаза 4: Frontend — API & Types

### [NEW] `frontend/lib/report-api.ts`

```typescript
// Generic report API factory — Pattern #1
export function createReportApi<F, R>(reportKey: string) {
    return {
        execute: (filter: F) => {
            const qs = buildReportQS(filter)
            return apiFetch<R>(`/reports/${reportKey}${qs}`)
        },
        meta: () =>
            apiFetch<ReportMeta>(`/metadata/reports/${reportKey}`),
        exportUrl: (filter: F, format: "csv" | "xlsx") => {
            const qs = buildReportQS({ ...filter, format })
            return `${API_BASE}/reports/${reportKey}/export${qs}`
        },
    }
}
```

### [NEW] `frontend/types/report-meta.ts`

```typescript
export interface ReportMeta {
    name: string
    description: string
    filters: ReportFilterDef[]
    columns: ReportColumnDef[]
    groupBy: ReportGroupByDef[]
    totals: ReportTotalDef[]
    exportFormats: string[]
    scopeDimensions: string[]
    defaultSort?: { column: string; direction: "asc" | "desc" }
}

// Зеркало backend типов — строгая типизация
export interface ReportFilterDef { ... }
export interface ReportColumnDef { ... }
export interface ReportGroupByDef { ... }
export interface ReportTotalDef { ... }
```

---

## Фаза 5: Frontend — Generic Hooks

### [NEW] `frontend/hooks/useReportPage.ts`

```typescript
export function useReportPage<F extends Record<string, unknown>, R>(reportKey: string) {
    // 1. Загрузка метаданных: GET /metadata/reports/{key}
    const { data: meta } = useSWR(`report-meta-${reportKey}`, () => api.meta.getReportMeta(reportKey))
    
    // 2. Фильтры в URL state (как useUrlSort, но для фильтров)
    const [filter, setFilter] = useReportFilter<F>(reportKey)
    
    // 3. Состояние выполнения: idle → loading → done → error → empty
    const [status, setStatus] = useState<ReportStatus>("idle")
    const [result, setResult] = useState<R | null>(null)
    const [error, setError] = useState<string | null>(null)
    
    // 4. Группировка (frontend-only, без нового запроса)
    const [activeGroupBy, setActiveGroupBy] = useState<string[]>([])
    
    // 5. Видимые колонки (из preferences)
    const { visibleKeys, onToggle, onReorder, onReset } = useColumnVisibility(reportKey, meta?.columns)
    
    // 6. Генерация отчёта
    const generate = useCallback(async () => {
        setStatus("loading")
        try {
            const data = await reportApi.execute(filter)
            setResult(data)
            setStatus(hasItems(data) ? "done" : "empty")
        } catch (e) {
            setError(getErrorMessage(e))
            setStatus("error")
        }
    }, [filter])
    
    // 7. Экспорт
    const exportReport = useCallback((format: "csv" | "xlsx") => {
        window.open(reportApi.exportUrl(filter, format), "_blank")
    }, [filter])
    
    return { meta, filter, setFilter, result, status, error, generate, exportReport,
             activeGroupBy, setActiveGroupBy, visibleKeys, onToggle, onReorder, onReset }
}
```

### [NEW] `frontend/hooks/useReportVariants.ts`

Сохранение именованных наборов настроек через `api.preferences`:
```typescript
export function useReportVariants(reportKey: string) {
    // CRUD для вариантов: { name, filter, columns, groupBy }
    // Хранение: api.preferences.saveReportVariant(reportKey, variant)
    // Загрузка: api.preferences.getReportVariants(reportKey)
}
```

---

## Фаза 6: Frontend — Generic Components

### [NEW] `frontend/components/shared/report-page.tsx`

Полностью metadata-driven страница:
```
ReportPage
├── FormToolbar (существующий)
│   ├── primaryAction="Сформировать"
│   ├── extraMenuItems=[Export CSV, Export XLSX]
│   └── toolbarIcons=[Настройки]
├── ReportFilterSidebar (новый, extends FilterSidebar)
│   ├── filters из meta.filters (metadata-driven)
│   ├── periodField (если есть)
│   └── detailsContent (drill-down)
└── ReportResultArea (новый)
    ├── ReportDataToolbar (Column chooser, GroupBy selector)
    ├── ReportTable (group-header + data rows + subtotals)
    └── ReportTotalsFooter (произвольные итоги из meta.totals)
```

### [NEW] `frontend/components/shared/report-table.tsx`

Отдельный от `DataTable` компонент (не ломаем `T extends { id: string }`):
```typescript
interface ReportTableProps {
    rows: DisplayRow[]          // group | data | subtotal | footer
    columns: ReportColumnDef[]
    visibleKeys: string[]
    sortColumn?: string
    sortDirection?: "asc" | "desc"
    onSort?: (key: string) => void
    onRowClick?: (item: Record<string, unknown>) => void
}

type DisplayRow =
    | { kind: "group";    depth: number; label: string; subtotals: Record<string, number> }
    | { kind: "data";     depth: number; item: Record<string, unknown> }
    | { kind: "subtotal"; depth: number; totals: Record<string, number> }
    | { kind: "footer";   totals: Record<string, number> }
```

### [NEW] `frontend/lib/report-grouping.ts`

Чистая функция трансформации:
```typescript
export function buildDisplayRows(
    items: Record<string, unknown>[],
    activeGroupBy: string[],
    totals: ReportTotalDef[],
): DisplayRow[]
```

### [NEW] `frontend/components/shared/report-totals-footer.tsx`

Расширение `DocumentTotalsFooter` для произвольного набора итогов:
```typescript
interface ReportTotalsFooterProps {
    totals: { label: string; value: number; format?: string }[]
    itemCount: number
    groupCount?: number
}
```

---

## Фаза 7: Advanced Features (после MVP)

### 7.1 Drill-Down Panel
- Клик по строке → `FilterSidebar.detailsContent` показывает движения
- `GET /reports/{key}/drill-down?productId=...&warehouseId=...`
- Навигация: «Открыть карточку» / «Показать все движения»

### 7.2 Report Variants (Варианты настроек)
- Сохранение: фильтры + колонки + группировка + сортировка
- `api.preferences.saveReportVariant(key, variant)`
- Быстрое переключение через dropdown в toolbar

### 7.3 Chart View (визуализация)
- Toggle: Table ↔ Chart (как Odoo Pivot ↔ Graph)
- Библиотека: recharts (уже в проекте для dashboard)
- Типы: bar, line, pie — автоматически из `meta.columns` + `meta.groupBy`

### 7.4 Scheduled Reports (интеграция с Automation Engine)
- `EventType: "schedule"` → `ActionType: "generate_report"`
- Параметры: `reportKey`, `filter`, `format`, `channelId`
- Результат: файл экспорта отправляется в Telegram/Email по расписанию

### 7.5 URL Sharing
- Все фильтры в URL query params
- «Копировать ссылку» → полный URL с фильтрами
- Открытие URL → автоматическая генерация отчёта

---

## Фаза 8: Post-Cap Features (после снятия лимита 1000 строк)

> [!NOTE]
> Эти задачи становятся актуальны только после реализации streaming export (фазы 9-10 из MVP), когда cap на 1000 строк снимается.

### 8.1 Async Report Execution
- Порог: если `Execute()` >5s → переключение в async-режим
- Горутина выполняет отчёт → `hub.BroadcastToUser(tenantID, userID, "report:done", {reportKey, resultId})`
- Frontend: `useReportPage` слушает WebSocket → автоматически показывает результат
- Нет нового SSE — используем существующий WebSocket Hub

### 8.2 Server-Side Grouping Fallback
- Порог: >5000 строк в результате
- `GET /reports/{key}/grouped?groupBy=warehouseName` → сервер возвращает pre-grouped JSON
- Frontend определяет стратегию автоматически по `result.totalItems`
- >50000 строк → интерактивная таблица скрывается, предлагается только export

---

## Сравнение: Текущее → Идеальное

| Аспект | Сейчас | Идеально |
|---|---|---|
| Новый отчёт backend | 4 файла, ~200 строк boilerplate | 1 struct: `Meta()` + `Execute()` |
| Новый отчёт frontend | Ручная страница | `<ReportPage reportKey="..." />` |
| RLS | Не применяется | Автоматически через ScopeDimensions |
| Экспорт | Нет | Streaming CSV/XLSX |
| Метаданные для UI | Нет | `GET /metadata/reports/{key}` |
| Группировка | Нет | Frontend трансформация без запроса |
| Fast path (текущая дата) | Нет | `reg_stock_balances` → O(1) |
| Варианты настроек | Нет | `api.preferences` + именованные |
| Drill-down | Нет | `FilterSidebar.detailsContent` |
| Визуализация | Нет | Table ↔ Chart toggle |
| Типизация (backend) | `float64` для количеств | `types.Quantity` (int64) |
| Типизация (frontend) | Ручные типы | Зеркало `ReportMeta` с generics |
| Extension API | Невозможно | `v1.RegisterTypedReport(reg, &MyReport{})` |

---

## Порядок реализации (приоритет)

### Wave 1: MVP (backend contract + frontend page)

| # | Задача | Зависимости | Усилия |
|---|--------|-------------|--------|
| 1 | `ReportMeta` + `ReportRegistration[F,R]` types | — | S |
| 2 | `GenericReportHandler[F,R]` + filter parsing | #1 | M |
| 3 | `RegisterTypedReport()` в FactoryRegistry | #1, #2 | S |
| 4 | Миграция StockBalance на новый контракт | #3 | S |
| 5 | Frontend: `report-meta.ts` types | #1 | S |
| 6 | Frontend: `useReportPage` hook | #5 | M |
| 7 | Frontend: `ReportPage` + `ReportTable` | #6 | L |
| 8 | Frontend: `report-grouping.ts` | #7 | M |

**Оценка**: ~5-7 дней

### Wave 2: Export + оставшиеся отчёты

| # | Задача | Зависимости | Усилия |
|---|--------|-------------|--------|
| 9 | Streaming CSV export | #2 | M |
| 10 | XLSX export (excelize) | #9 | M |
| 11 | Миграция StockTurnover + DocumentJournal | #4 | S |
| 12 | RLS injection в GenericReportHandler | #2 | M |

**Оценка**: ~3-4 дня

### Wave 3: Advanced UX

| # | Задача | Зависимости | Усилия |
|---|--------|-------------|--------|
| 13 | Drill-down panel | #7 | M |
| 14 | Report Variants (preferences) | #6 | M |
| 15 | URL Sharing (фильтры в URL) | #6 | S |
| 16 | Chart view toggle | #7 | L |

**Оценка**: ~4-5 дней

---

## Детальный план реализации Wave 4 (Post-Cap)

Снятие лимита в 1000 строк (который был установлен в базовых сервисах) требует трёх ключевых изменений для сохранения производительности и UX:

### 1. Server-side grouping fallback (`/grouped`)
Когда строк больше 5000 (но меньше 50000), клиентский `buildDisplayRows()` начнёт тормозить UI-поток браузера.
- **Backend:** В `GenericReportHandler` добавится новый endpoint `GET /reports/{key}/grouped`. Он вызывает `Execute()`, получает плоский слайс (например, 25 000 строк) и применяет логику группировки на стороне Go (используя пакет `report-grouping` портированный на Go, либо агрегацию in-memory). Возвращает массив уже готовых `DisplayRow` (группы, сабтоталы).
- **Frontend:** В `useReportPage.ts` добавится проверка: если `items.length > 5000`, то при изменении группировки/сортировки делается запрос к `/grouped`, вместо локального пересчёта.

### 2. Async execution через WebSocket
Для тяжелых исторических отчётов (например, оборотная ведомость за год по всем складам).
- **Backend:** Если `Execute()` понимает, что объем данных огромный, он возвращает HTTP 202 Accepted с `job_id`. Запускается горутина, которая собирает данные. По готовности результат отправляется через существующий `hub.BroadcastToUser("report:done", payload)`.
- **Frontend:** В `useReportPage.ts` статус переходит в `processing`. Хук слушает WS-событие `report:done` и обновляет стейт, как только данные приходят.

### 3. Снятие LIMIT в SQL
- В `internal/domain/reports/executors.go` (StockBalance, StockTurnover, DocumentJournal) убираются искусственные `LIMIT 1000`.
- Для защиты от OOM устанавливается жесткий лимит для JSON-выдачи в 50000 строк. Для бóльших объёмов выдаётся ошибка с предложением использовать Export (где ограничений нет благодаря `pgx.Rows` streaming).

> [!IMPORTANT]
> **Требуется подтверждение**: Начинаем реализацию Wave 4 с пункта 1 (Server-side grouping) и 3 (Снятие LIMIT)?

---

### Roadmap (Оценки)


| # | Задача | Зависимости | Усилия |
|---|--------|-------------|--------|
| 17 | Async execution через WebSocket | #9 | M |
| 18 | Server-side grouping fallback (`/grouped`) | #8 | M |
| 19 | Scheduled Reports (Automation Engine) | #9 | M |

**Оценка**: ~3-4 дня

**Итого**: MVP ~5-7 дней, полная система ~15-20 дней.

---

## Verification Plan

### Automated Tests
```bash
# Backend
go test ./internal/platform/... -run TestReportMeta
go test ./internal/infrastructure/http/v1/handlers/... -run TestReportHandler
go test ./internal/domain/reports/... -run TestStockBalance

# Frontend
npx tsc --noEmit
```

### Manual Verification
1. Открыть `/reports/stock-balance` → фильтры строятся из metadata
2. Нажать «Сформировать» → таблица с данными
3. Изменить группировку → таблица перестраивается без запроса
4. Экспорт CSV → файл скачивается, данные корректны
5. RLS: залогиниться пользователем с ограниченным доступом → видны только разрешённые склады
6. Drill-down: клик по строке → панель деталей с движениями
