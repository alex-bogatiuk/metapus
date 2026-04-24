# Metapus Extensibility Architecture — 5-Phase Implementation Plan

Реализация 3-Layer Extension Model с Go compile-time safety: разделение platform/content, client-ext scaffold, Interface Segregation, Frontend UIRegistry + Metadata fallback, CI/CD pipeline.

---

## Решения (из обсуждения)

- **Scope:** все 5 фаз
- **Go modules:** гибридный — один модуль для platform+content, отдельный `client-ext/` со своим `go.mod`
- **Frontend:** UIRegistry + Metadata-driven fallback + monorepo workspace

---

## Phase 1: Формализация модульной структуры (Backend)

**Цель:** чёткое разделение Platform Core и Business Content внутри одного Go-модуля через package groups.

### 1.1 Создать `internal/platform/` — экспортируемые extension API

Выделить **стабильные интерфейсы** из текущих пакетов в новый `internal/platform/` package:

```
internal/platform/
├── extension.go          — ExtensionRegistrar interface (единая точка подключения расширений)
├── catalog_contract.go   — CatalogRegistration (переэкспорт из v1, но в стабильном месте)
├── document_contract.go  — DocumentRegistration
├── route_contract.go     — RouteRegistration
├── hook_contract.go      — HookRegistry re-export (read-only view для расширений)
├── posting_contract.go   — RegisterVisitor, RegisterRecorder, PostingValidator
└── version.go            — ExtensionAPIVersion const (semver string)
```

> **Примечание:** НЕ перемещаем реализации — только создаём facade-пакет с type aliases / re-exports для extension API. Это минимизирует ломающие изменения.

### 1.2 Создать `internal/content/` — бизнес-контент как package group

Переместить `RegisterDefaults()` и все concrete registrations:

```
internal/content/
├── register.go                — RegisterDefaults(reg) + RegisterAllContent(reg, cfg)
├── catalog_registrations.go   — ← из v1/catalog_registrations.go (8 struct'ов)
├── document_registrations.go  — ← из v1/document_registrations.go (2 struct'а)
└── register_registrations.go  — ← из v1/register_registrations.go (4 struct'а)
```

Файлы `v1/catalog_registrations.go`, `v1/document_registrations.go`, `v1/register_registrations.go` остаются как thin wrappers (вызывают content) для обратной совместимости на 1 версию, затем удаляются.

### 1.3 `RouterConfig` — явная инъекция PostingEngine

Сейчас `PostingEngine` создаётся внутри `registerDocumentRoutes()` если nil. Вынести создание в `cmd/server/main.go` чтобы client-ext мог добавить visitors/recorders ДО маршрутизации:

```go
// cmd/server/main.go
engine := posting.NewEngine(docLocker, posting.DefaultRecorders(...)...)
// Client extension adds custom visitors/recorders here
router := v1.NewRouter(v1.RouterConfig{
    PostingEngine: engine,
})
```

### 1.4 Документация stable API

Создать `docs/20-extension-api.md` — описание всех стабильных интерфейсов с semver-гарантиями.

---

## Phase 2: Client Extension Scaffold

**Цель:** готовый шаблон `client-ext/` с примером custom catalog + hook.

### 2.1 Структура `client-ext/`

```
client-ext/
├── go.mod                     — module client-ext; require metapus v0.0.0
├── go.sum
├── register.go                — Register(reg, cfg) — единая точка входа
├── config.go                  — Config struct (PostingEngine, Numerator, etc.)
├── catalogs/
│   └── vehicle/
│       ├── model.go           — Vehicle struct (embed entity.Catalog)
│       ├── service.go         — NewService() (embed CatalogService)
│       └── registration.go    — VehicleRegistration implements CatalogRegistration
├── hooks/
│   └── goods_receipt_hooks.go — пример hook на стандартную сущность
├── migrations/
│   └── 001_cat_vehicles.sql   — CREATE TABLE cat_vehicles
├── README.md                  — инструкция по использованию
└── Makefile                   — build, typecheck
```

### 2.2 Multi-migration support

Расширить `cmd/server/main.go` и migrator для поддержки нескольких migration paths:

```go
// Порядок: core migrations → client migrations
migrator.Run(
    "db/migrations",           // Platform + Content
    "client-ext/migrations",   // Client (опционально, если директория существует)
)
```

Реализация: обёртка над `goose` с sequential source dirs.

### 2.3 Модификация `cmd/server/main.go`

Добавить extension point pattern:

```go
// Composition root показывает как подключать расширения
reg := v1.NewFactoryRegistry()
v1.RegisterDefaults(reg)
// clientext.Register(reg, clientext.Config{...}) // uncomment to enable

router := v1.NewRouter(v1.RouterConfig{
    Registry: reg,
    // ...
})
```

### 2.4 Vehicle — полный E2E пример

- Migration: `cat_vehicles` таблица
- Model: `Vehicle` с `PlateNumber`, `Brand`, `Year`
- Repo: generic `BaseCatalogRepo[Vehicle]`
- Service: `CatalogService[Vehicle]`
- Handler: `CatalogHandler[Vehicle, CreateDTO, UpdateDTO]`
- Registration: `VehicleRegistration` implementing `CatalogRegistration`

---

## Phase 3: Interface Segregation

**Цель:** минимизировать breaking changes при расширении Registration interfaces.

### 3.1 Разбить `CatalogRegistration` на required + optional

**Required (core):**
```go
type CatalogRegistration interface {
    RoutePrefix() string
    Permission() string
    EntityName() string
    Build(deps CatalogDeps) CatalogRouteHandler
}
```

**Optional (через type assertion):**
```go
type Presentable interface {
    EntityPresentation() metadata.Presentation
}
type Inspectable interface {
    EntityStruct() interface{}
}
type Labeled interface {
    EntityLabel() string
}
type ReferenceProvider interface {
    ReferenceTypes() []string
}
```

### 3.2 Аналогично для `DocumentRegistration`

**Required:**
```go
type DocumentRegistration interface {
    RoutePrefix() string
    Permission() string
    EntityName() string
    Build(deps DocumentDeps) DocumentRouteHandler
}
```

**Optional:** `Presentable`, `Inspectable`, `Labeled` (те же interfaces).

### 3.3 Обновить `registerCatalogRoutes()` / `registerDocumentRoutes()`

Использовать type assertion для optional interfaces:

```go
// Metadata auto-registration with graceful fallback
if p, ok := factory.(Presentable); ok {
    def.Presentation = p.EntityPresentation()
}
if i, ok := factory.(Inspectable); ok {
    def = metadata.Inspect(i.EntityStruct(), factory.EntityName(), metadata.TypeCatalog)
}
```

### 3.4 Обратная совместимость

Все существующие registrations уже реализуют все методы → ничего не ломается. Новые клиентские registrations могут реализовать только Required interface.

---

## Phase 4: Frontend UIRegistry + Metadata Fallback

**Цель:** extensible frontend с runtime-регистрацией компонентов и автогенерацией форм из metadata.

### 4.1 `frontend/lib/entity-registry.ts` — UIRegistry

```typescript
interface EntityUIRegistration {
    entityType: "catalog" | "document";
    entityName: string;
    routePrefix: string;
    listColumns?: ColumnDef[];
    formComponent?: React.LazyExoticComponent<React.ComponentType<any>>;
    filtersMeta?: FilterFieldMeta[];
    // Если не указаны — генерируются из /meta endpoint
}

class UIRegistry {
    private catalogs = new Map<string, EntityUIRegistration>();
    private documents = new Map<string, EntityUIRegistration>();
    
    registerCatalog(reg: EntityUIRegistration): void;
    registerDocument(reg: EntityUIRegistration): void;
    getCatalog(name: string): EntityUIRegistration | undefined;
    getDocument(name: string): EntityUIRegistration | undefined;
    allCatalogs(): EntityUIRegistration[];
    allDocuments(): EntityUIRegistration[];
}
```

### 4.2 `frontend/lib/entity-registry-defaults.ts`

Регистрация всех существующих сущностей (Counterparty, Nomenclature, GoodsReceipt, etc.) с их текущими formComponent и listColumns.

### 4.3 Metadata-driven fallback компонент

`frontend/components/generic/auto-form.tsx` — генерирует форму из `/api/v1/meta/:name`:
- String → `<Input />`
- Boolean → `<Switch />`
- Reference → `<ReferenceSelect />` (существующий компонент)
- Date → `<DatePicker />`
- Money → `<MoneyInput />`
- Table parts → `<DataTable />` с inline editing

`frontend/components/generic/auto-list.tsx` — generic list из metadata:
- Колонки из `fields`
- Фильтры из `filtersMeta`
- Pagination, sorting — стандартные

### 4.4 Dynamic routing

`frontend/app/(main)/ext/[entityType]/[routePrefix]/page.tsx` — catch-all route:
1. Проверить UIRegistry → если есть custom `listComponent` — рендерить его
2. Fallback: загрузить metadata, рендерить `<AutoList />`

`frontend/app/(main)/ext/[entityType]/[routePrefix]/[id]/page.tsx`:
1. UIRegistry → custom `formComponent`?
2. Fallback: `<AutoForm />`

### 4.5 Widget Registry расширение

Уже есть `widget-registry.ts`. Добавить `registerWidget()` экспортную функцию для клиентских виджетов:

```typescript
export function registerWidget<T extends WidgetType>(def: WidgetDefinition<T>): void {
    WIDGET_DEFINITIONS.push(def);
    widgetRegistry.set(def.type, def as WidgetDefinition);
}
```

---

## Phase 5: CI/CD для обновлений

### 5.1 Makefile targets

```makefile
# Проверить совместимость всех extensions
check-extensions:
    cd client-ext && go build ./...
    cd client-ext && go vet ./...

# Полная проверка
check-all: check check-extensions

# Генерация changelog из git commits
changelog:
    @echo "Breaking changes in Registration interfaces..."
    git diff HEAD~1 -- internal/platform/ | grep "^[+-].*interface"
```

### 5.2 GitHub Actions workflow

`.github/workflows/extension-compat.yml`:
- На каждый PR в `internal/platform/`:
  - Сбилдить `client-ext/` с новой версией ядра
  - Если не компилируется → ❌ fail с описанием breaking change

### 5.3 Upgrade guide template

`docs/UPGRADE.md` — шаблон upgrade guide:
- Список breaking changes per version
- Migration instructions
- Code examples for adaptation

### 5.4 Extension version contract

`internal/platform/version.go`:
```go
const ExtensionAPIVersion = "1.0.0"
```

`client-ext/` проверяет совместимость при компиляции через build constraint или init().

---

## Порядок реализации (шаги)

| # | Шаг | Файлы | Статус |
|---|-----|-------|--------|
| 1 | ✅ Создать `internal/platform/` с extension API facades | 7 файлов | `go build ✓` |
| 2 | ✅ Создать `internal/content/` с RegisterDefaults | 4 файла | `go build ✓` |
| 3 | ✅ Обновить `v1/factory_registry.go` — thin wrappers на content | 3 файла | `go build ✓` |
| 4 | ✅ Обновить `cmd/server/main.go` — explicit PostingEngine + extension point | 1 файл | `go build ✓` |
| 5 | ✅ Interface Segregation — split Registration interfaces | 2 файла | `go build ✓` |
| 6 | ✅ Обновить `router.go` — type assertion для optional interfaces | 1 файл | `go build ✓` |
| 7 | ✅ Создать `extensions/vehicle/` scaffold с Vehicle example | 8 файлов | `go build ✓` |
| 8 | ⏳ Migration manager — multi-path support | — | deferred (manual per-extension migrations) |
| 9 | ✅ Frontend UIRegistry + defaults | `entity-registry.ts`, `entity-registry-defaults.ts` | `tsc ✓` |
| 10 | ✅ Auto-form / auto-list components | `auto-form.tsx`, `auto-list.tsx` | `tsc ✓` |
| 11 | ✅ Dynamic routing for extensions | `ext/[entityType]/[routePrefix]/page.tsx`, `…/[id]/page.tsx` | `tsc ✓` |
| 12 | ✅ Widget registry extensibility | `registerWidget()` в `widget-registry.ts` | `tsc ✓` |
| 13 | ✅ Extension API docs | `21-extension-api.md`, `UPGRADE.md`, `ROUTER.md` updated | docs ✓ |
| 14 | ✅ CI/CD — Makefile targets + GitHub Actions | `Makefile` + `ci.yml` | `make check-all` |

**Проверка после каждого шага:** `go build ./...` (backend) и `npx tsc --noEmit` (frontend) — оба ✅ pass.

---

## Риски и митигации

| Риск | Митигация |
|------|-----------|
| Circular imports при split на platform/content | Platform — только interfaces, content — implementations. Нет обратных зависимостей. |
| Breaking existing tests | Не двигаем существующий код — создаём facades. Старые imports работают. |
| client-ext не компилируется с текущим Go module | `replace` directive в `go.work` для development |
| Auto-form не покрывает сложные кейсы | Metadata fallback для простых, explicit registration для сложных. Документировать ограничения. |
