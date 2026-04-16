# Generic CRUD Pipeline

> От generic handler с типовыми параметрами до generic repository. Система хуков (аналог подписок на события из 1С) и паттерн композиции сервисов.

---

## Общая схема

```
Router (wiring):
  repo := NewCounterpartyRepo()
  service := NewService(repo, numerator)       ← hooks registered here
  handler := NewHandler(base, service)         ← mappers injected here
  RegisterCatalogRoutes(group, handler)        ← permissions set here

┌── HANDLER (generic) ──────────────────────────────┐
│ CatalogHandler[*Counterparty, CreateDTO, UpdateDTO] │
│ ├── BindJSON(c, &req)           → CreateDTO        │
│ ├── mapCreateDTO(req)           → *Counterparty    │
│ ├── service.Create(ctx, entity) → PIPELINE         │
│ └── mapToDTO(entity)            → response JSON    │
└────────────────────────────────────────────────────┘
                    │
                    ▼
┌── SERVICE (generic + entity-specific) ────────────┐
│ CatalogService[*Counterparty]                      │
│ ┌─── PIPELINE ──────────────────────────────────┐  │
│ │ 1. entity.Validate(ctx)                        │  │
│ │ 2. hooks.RunBeforeCreate(ctx, entity)          │  │
│ │ 3. txm.RunInTransaction { repo.Create }        │  │
│ │ 4. hooks.RunAfterCreate(ctx, entity)           │  │
│ └────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────┘
                    │
                    ▼
┌── REPOSITORY (generic) ───────────────────────────┐
│ BaseCatalogRepo[*Counterparty]                     │
│ ├── tableName: "cat_counterparties"                │
│ ├── selectCols: ["id","code","name","inn",...]     │
│ └── StructToMap → squirrel → GetQuerier(ctx) → Exec│
└────────────────────────────────────────────────────┘
```

---

## Generic Handler — `CatalogHandler[T, CreateDTO, UpdateDTO]`

Один код обслуживает все справочники. Параметризуется типом сущности и DTO.

**Типовые параметры:**
- `T` = `*counterparty.Counterparty` (domain entity)
- `CreateDTO` = `dto.CreateCounterpartyRequest`
- `UpdateDTO` = `dto.UpdateCounterpartyRequest`

**Mapper functions** (инъекция через config):
- `mapCreateDTO: func(CreateDTO) T` — dto → domain entity
- `mapUpdateDTO: func(UpdateDTO, existing T) T` — dto + existing → updated
- `mapToDTO: func(T) any` — domain → response DTO

**Стандартные методы (7):**
- `List`, `Get`, `Create`, `Update`, `Delete`, `SetDeletionMark`, `GetTree`

**Document variant** (`BaseDocumentHandler`) дополнительно:
- `Post`, `Unpost`
- `Create` с `PostImmediately` → `PostAndSave` vs `Create`

---

## Generic Service — `CatalogService[T]` Pipeline

### Create Pipeline
```
1. VALIDATE       → entity.Validate(ctx)
                     └── ошибка → apperror.NewValidation (400)

2. BEFORE HOOKS   → hooks.RunBeforeCreate(ctx, entity)  [вне транзакции]
                     ├── hook1: prepareForCreate (код, уникальность)
                     ├── hook2: audit.EnrichCreatedBy
                     └── hookN: любая бизнес-логика
                     └── ошибка в любом hook → прерывание

3. TRANSACTION    → txm.RunInTransaction(ctx, func(ctx) {
                       repo.Create(ctx, entity)
                     })
                     └── ошибка → rollback

4. AFTER HOOKS    → hooks.RunAfterCreate(ctx, entity)  [вне транзакции]
                     └── ошибки логируются, НЕ прерывают
```

### Update Pipeline
`Validate → BeforeUpdate → tx{repo.Update} → AfterUpdate`

### Delete Pipeline
`GetByID → BeforeDelete → tx{repo.Delete} → AfterDelete`

---

## HookRegistry[T] — Event-based Lifecycle Hooks

Generic реестр хуков. Аналог подписок на события объектов в 1С.

### 6 событий

| Событие | Когда | Ошибка прерывает? |
|---------|-------|-------------------|
| `BeforeCreate` | До создания | Да |
| `AfterCreate` | После создания | Нет (логируется) |
| `BeforeUpdate` | До обновления | Да |
| `AfterUpdate` | После обновления | Нет (логируется) |
| `BeforeDelete` | До удаления | Да |
| `AfterDelete` | После удаления | Нет (логируется) |

### Регистрация и выполнение

```go
registry.On(BeforeCreate, hookFn)        // generic
registry.OnBeforeCreate(hookFn)          // convenience alias
```

- Несколько хуков на одно событие — выполняются **в порядке регистрации**
- **Fail-fast**: первый Before-hook с ошибкой прерывает всю цепочку

---

## Композиция сервисов — Entity-specific Logic

Entity-specific сервисы embed `CatalogService[T]` и добавляют свои хуки:

```go
type Service struct {
    *domain.CatalogService[*Counterparty]  // embedded CRUD
    repo      Repository
    numerator numerator.Generator
}

func NewService(repo Repository, numerator *numerator.Service) *Service {
    base := domain.NewCatalogService(config)
    svc := &Service{CatalogService: base, repo: repo}
    
    base.Hooks().OnBeforeCreate(svc.prepareForCreate)   // hook 1
    base.Hooks().OnBeforeUpdate(svc.prepareForUpdate)   // hook 2
    return svc
}
```

**Entity-specific hooks:**
- `prepareForCreate` — автонумерация, проверка уникальности INN
- `prepareForUpdate` — проверка уникальности (exclude self ID)

**External hooks** (из router.go):
```go
service.Hooks().OnBeforeCreate(audit.EnrichCreatedByDirect)
```

**Итоговая цепочка Create:**
1. `Validate(ctx)`
2. [hook] `prepareForCreate` (код, уникальность INN)
3. [hook] `audit.EnrichCreatedBy`
4. `tx { repo.Create(ctx, entity) }`
5. After hooks (если есть)

---

## Generic Repository — `BaseCatalogRepo[T]`

CRUD через reflection (`StructToMap`) и SQL builder (`squirrel`).

### Create
```
StructToMap(entity) → map[string]any (через db tags)
→ filter by selectCols
→ squirrel.Insert(tableName).SetMap(data)
→ querier.Exec(ctx, sql, args...)
```

### Update (с optimistic locking)
```
StructToMap → filter (exclude id, version)
→ .Set("version", Expr("version + 1"))
→ .Where(Eq{"id": id, "version": version})
→ .Suffix("RETURNING version")
→ ErrNoRows → ConcurrentModification (409)
```

### List (курсорная пагинация с бесконечным скроллом)

Списки используют **курсорную пагинацию** (keyset pagination) вместо OFFSET/LIMIT.
Это обеспечивает стабильную производительность на больших объёмах данных и корректное
поведение при параллельных вставках/удалениях.

#### Общая архитектура

```
Frontend (useEntityListPage)          Backend (BaseCatalogRepo / BaseDocumentRepo)
┌──────────────────────────┐          ┌──────────────────────────────────────────┐
│ 1. Initial load:         │          │                                          │
│    GET ?limit=100        │────────► │ SELECT ... WHERE ... ORDER BY           │
│                          │          │ + COUNT(*) → totalCount                  │
│                          │ ◄────────│ → items + nextCursor + totalCount       │
│                          │          │                                          │
│ 2. Scroll down:          │          │                                          │
│    GET ?after=<cursor>   │────────► │ SELECT ... WHERE (col,id)>(v1,v2)       │
│                          │ ◄────────│ → items + nextCursor  (NO COUNT)        │
│                          │          │                                          │
│ 3. Sort change:          │          │                                          │
│    GET ?orderBy=name     │          │                                          │
│    &skipCount=true       │────────► │ SELECT ... ORDER BY name               │
│                          │ ◄────────│ → items + nextCursor  (NO COUNT)        │
│                          │          │                                          │
│ 4. Filter change:        │          │                                          │
│    GET ?filter=[...]     │────────► │ SELECT ... WHERE <filters>              │
│                          │          │ + COUNT(*) → totalCount                  │
│                          │ ◄────────│ → items + nextCursor + totalCount       │
└──────────────────────────┘          └──────────────────────────────────────────┘
```

#### Курсоры

Курсор — base64-encoded JSON с последним значением сортировки и ID:
```json
{"v":["code","id"],"d":["NM-GEN-100","019d8ae1-dbc6-7e5b-..."]}
```

Навигация по курсорам:
- `?after=<cursor>` — загрузить следующую страницу (scroll down)
- `?before=<cursor>` — загрузить предыдущую страницу (scroll up)
- `?around=<id>` — телепортация к конкретному элементу (например, «показать в списке» из формы)

При cursor-запросах (`after`/`before`) COUNT(\*) **никогда не выполняется** — это основа
производительности бесконечного скролла.

#### Оптимизация подсчёта — `skipCount`

`COUNT(*)` — дорогая операция на больших таблицах. Система оптимизирует её через параметр
`?skipCount=true`, который позволяет пропустить подсчёт общего количества записей.

**Backend:**

```go
// domain/repository.go
type ListFilter struct {
    // ...
    SkipCount bool  // Если true — COUNT(*) не выполняется
}

type CursorListResult[T any] struct {
    Items      []T
    TotalCount *int64  // nil = подсчёт был пропущен
    // ...
}
```

```go
// catalog_repo/base.go — условный COUNT
if !f.SkipCount && f.After == "" && f.Before == "" {
    err := countQuery.RunWith(querier).QueryRowContext(ctx).Scan(&totalCount)
    result.TotalCount = &totalCount
}
// Если SkipCount=true или cursor-запрос → TotalCount остаётся nil
```

Параметр `?skipCount=true` парсится в `BaseHandler.ParseListFilter()`.

**Frontend — Filter Fingerprint:**

Фронтенд использует **filter fingerprint** — детерминированный хеш текущих фильтров —
чтобы определить, нужен ли пересчёт:

```typescript
// hooks/useEntityListPage.ts
const computeFingerprint = (filterValues, showDeleted) => {
  const entries = Object.entries(filterValues)
    .filter(([, v]) => v !== undefined && v !== null)
    .sort(([a], [b]) => a.localeCompare(b))
  return JSON.stringify([entries, showDeleted])
}
```

| Триггер | Fingerprint изм.? | skipCount | COUNT(\*) |
|---------|-------------------|-----------|-----------|
| Начальная загрузка | да (пустой → начальный) | `false` | ✅ выполняется |
| Смена фильтра | да | `false` | ✅ выполняется |
| Переключение «Показать удалённые» | да | `false` | ✅ выполняется |
| Смена сортировки | **нет** | `true` | ❌ пропускается |
| Scroll down/up (cursor) | — | — | ❌ пропускается |

**Nullable totalCount:**

Тип `TotalCount` — указатель `*int64` (Go) / `number | null` (TypeScript).
Когда COUNT пропущен, API возвращает `"totalCount": null`. Фронтенд сохраняет
предыдущее значение `totalCount` и не обнуляет его:

```typescript
// applyResult в useCursorList / useEntityListPage
if (res.totalCount != null) {
  setTotalCount(res.totalCount)
}
// Если null — totalCount остаётся прежним
```

#### API ответ — `CursorListResponse`

```json
{
  "items": [...],
  "nextCursor": "base64...",
  "prevCursor": "base64...",
  "hasMore": true,
  "hasPrev": false,
  "totalCount": 300,
  "targetIndex": null
}
```

| Поле | Тип | Описание |
|------|-----|----------|
| `items` | `T[]` | Элементы текущей страницы |
| `nextCursor` | `string?` | Курсор для следующей страницы |
| `prevCursor` | `string?` | Курсор для предыдущей страницы |
| `hasMore` | `bool` | Есть ли ещё записи вперёд |
| `hasPrev` | `bool` | Есть ли записи назад |
| `totalCount` | `int \| null` | Общее кол-во записей (null = COUNT пропущен) |
| `targetIndex` | `int?` | Индекс целевого элемента при `?around=` |

#### Frontend-хуки

| Хук | Назначение |
|-----|-----------|
| `useCursorList` | Low-level хук: items, loadMore, loadPrev, reset, loadAround |
| `useEntityListPage` | High-level хук: фильтры, сортировка, selection, fingerprint, skipCount |

#### SQL (пример)

```sql
-- Первая страница (no cursor, no skipCount)
SELECT id, code, name, ... FROM cat_nomenclature
WHERE deletion_mark = false
ORDER BY name ASC, id ASC
LIMIT 101;                         -- limit+1 для определения hasMore

SELECT COUNT(*) FROM cat_nomenclature WHERE deletion_mark = false;

-- Scroll down (after cursor)
SELECT id, code, name, ... FROM cat_nomenclature
WHERE deletion_mark = false
  AND (name, id) > ('Бумага...', '019d8ae1-...')   -- keyset condition
ORDER BY name ASC, id ASC
LIMIT 101;
-- COUNT НЕ выполняется

-- Sort change (skipCount=true)
SELECT id, code, name, ... FROM cat_nomenclature
WHERE deletion_mark = false
ORDER BY code ASC, id ASC
LIMIT 101;
-- COUNT НЕ выполняется (skipCount=true)
```

### Delete (soft)
```sql
UPDATE SET deletion_mark = true WHERE id = $1
```

---

## Wiring — связка через FactoryRegistry

Все сущности (справочники, документы, регистры, отчёты) регистрируются через **FactoryRegistry** (`factory_registry.go`). Ручная связка в `router.go` заменена итерацией по реестру.

### Composition Root (main.go / router)

```go
// Создание реестра и регистрация всех built-in сущностей
reg := v1.NewFactoryRegistry()
v1.RegisterDefaults(reg)                         // все встроенные сущности
reg.RegisterDocument(&custom.MyDocRegistration{}) // клиентское расширение

router := v1.NewRouter(v1.RouterConfig{
    Registry: reg,  // nil → автоматически RegisterDefaults()
    // ...
})
```

### Как работает регистрация справочников

Каждый справочник реализует `CatalogRegistration` (`catalog_factory.go`). `FactoryRegistry` итерирует все регистрации и для каждой:
1. Вызывает `factory.Build(deps)` → создаёт repo, service, handler
2. Вызывает `RegisterCatalogRoutes(group, handler, permission)` → монтирует маршруты
3. Регистрирует metadata (для `/meta` endpoint)

`RegisterCatalogRoutes` автоматически подключает permission middleware:
- `GET ""` + `RequirePermission("catalog:counterparty:read")` → List
- `POST ""` + `RequirePermission("catalog:counterparty:create")` → Create
- `GET "/:id"` + `RequirePermission("catalog:counterparty:read")` → Get
- `PUT "/:id"` + `RequirePermission("catalog:counterparty:update")` → Update
- `DELETE "/:id"` + `RequirePermission("catalog:counterparty:delete")` → Delete

Документы аналогично — `DocumentRegistration` + shared dependencies (`PostingEngine`, `CurrencyResolver`) через `DocumentDeps`.

`BaseDocumentService[T, L]` предоставляет единый pipeline для документов (аналог `CatalogService[T]`):

**Create pipeline:**
1. `hooks.RunBeforeCreate(ctx, doc)` — enrichment (CreatedBy, UpdatedBy)
2. `ResolveCurrency(ctx, doc)` — цепочка: Document → Contract → Organization → System
3. `doc.Validate(ctx)` — внутренняя валидация
4. `GenerateNumber(ctx, doc)` — автонумерация (если номер пуст)
5. `tx { repo.Create + repo.SaveLines }` — атомарная запись
6. `hooks.RunAfterCreate(ctx, doc)` — уведомления (ошибки логируются)

**Дополнительные методы:** `Post`, `Unpost`, `PostAndSave`, `UpdateAndRepost`, `SetDeletionMark`

### Abstract Factory — регистрация через FactoryRegistry

Каждый тип документа регистрируется через `DocumentRegistration` (Abstract Factory, `document_factory.go`):

```go
type DocumentRegistration interface {
    RoutePrefix() string                              // "goods-receipt"
    Permission() string                               // "document:goods_receipt"
    EntityName() string                               // "GoodsReceipt"
    EntityLabel() string                              // "Поступление товаров"
    EntityPresentation() metadata.Presentation        // rich UI names
    EntityStruct() interface{}                        // zero-value for metadata.Inspect()
    Build(deps DocumentDeps) DocumentRouteHandler     // repo → service → handler
}
```

`FactoryRegistry` (`factory_registry.go`) хранит все регистрации и итерирует их при старте:
```go
for _, factory := range factoryReg.Documents() {
    handler := factory.Build(deps)
    RegisterDocumentRoutes(docsGroup.Group("/"+factory.RoutePrefix()), handler, factory.Permission())
    // + авторегистрация metadata
}
```

Добавление нового документа: реализовать `DocumentRegistration` и зарегистрировать в `RegisterDefaults()` или через `reg.RegisterDocument(&MyDocRegistration{})`.

Аналогичные интерфейсы: `CatalogRegistration` для справочников, `RouteRegistration` для регистров и отчётов.

### Visitor + Recorder — мультирегистровые движения

Генерация движений по регистрам реализована через паттерн **Visitor** (`posting/visitor.go`).
Запись/реверс движений — через **RegisterRecorder** (`posting/recorder.go`).

**Участники:**
- `RegisterVisitor` — интерфейс посетителя (`Name()`, `CollectMovements(ctx, doc, set)`)
- `StockMovementSource` — интерфейс-источник, реализуемый документами (`GenerateStockMovements`)
- `StockVisitor`, `CostVisitor`, `SettlementVisitor` — конкретные посетители
- `RegisterRecorder` — интерфейс записи/реверса движений (`RecordFromSet`, `ReverseMovements`)
- `PostingValidator` — опциональный интерфейс предпроверки (stock availability)
- `Engine.visitors` — реестр посетителей; итерируется при сборе движений
- `Engine.recorders` — реестр рекордеров; итерируется при записи/реверсе

**Как работает:**

```
Engine.doPost(doc)
  → engine.collectMovements(doc)           # Visitor pattern
      → for each visitor:
           visitor.CollectMovements(ctx, doc, set)
             → type-assert doc.(StockMovementSource)
             → doc.GenerateStockMovements(ctx) → []StockMovement
  → for each recorder (PostingValidator):  # Validate phase
       recorder.ValidateBeforePost(ctx, set)
  → for each recorder:                     # Record phase
       recorder.RecordFromSet(ctx, set)
```

**Расширение (новый регистр):**
1. Определить `XxxMovementSource` interface в `posting/visitor.go`
2. Создать `XxxVisitor` реализующий `RegisterVisitor`
3. Создать `XxxRecorder` реализующий `RegisterRecorder` (и `PostingValidator` при необходимости)
4. Зарегистрировать: `engine.AddVisitor(&XxxVisitor{})` + `engine.AddRecorder(&XxxRecorder{})`
5. Документы реализуют `XxxMovementSource` для генерации движений

Документы НЕ знают обо всех регистрах — они реализуют только те source-интерфейсы, которые им нужны.

### Decorator — middleware-обёртки сервисов

Сервисы документов работают через каноничный интерфейс `domain.DocumentService[T]` (`document_middleware.go`).
Декораторы оборачивают интерфейс для cross-cutting concerns без изменения бизнес-логики.

**Встроенные декораторы:**
- `LoggingDocumentService[T]` — логирует method, duration, error для каждого вызова

**Wiring в document_factory.go:**
```go
// 1. Создать concrete service + hooks
service := goods_receipt.NewService(repo, engine, numerator, nil, currencyResolver)
service.Hooks().OnBeforeCreate(...)

// 2. Обернуть декоратором
decorated := domain.WithLogging[*goods_receipt.GoodsReceipt]("goods-receipt")(service)

// 3. Передать decorated в handler (через интерфейс)
return handlers.NewGoodsReceiptHandler(base, decorated)
```

**Композиция нескольких middleware:**
```go
decorated := domain.Chain[*GoodsReceipt](
    domain.WithLogging[*GoodsReceipt]("goods-receipt"),
    // domain.WithMetrics[*GoodsReceipt]("goods-receipt"),
)(service)
```

**Добавление нового middleware:**
1. Реализовать `domain.DocumentService[T]` с полем `next DocumentService[T]`
2. Каждый метод: вызов `next.Xxx(...)` + доп. логика (before/after/defer)
3. Создать `WithXxx[T]() ServiceMiddleware[T]` конструктор

### State — жизненный цикл документов

Документ хранит два boolean-флага (`Posted`, `DeletionMark`), из которых выводится состояние.
Паттерн **State** (`entity/document_state.go`) централизует все проверки допустимости операций:

```go
// entity.Document делегирует проверки текущему состоянию
func (d *Document) CanModify() error { return d.State().CanModify() }
func (d *Document) CanPost(ctx) error {
    if err := d.State().CanPost(); err != nil { return err }
    return d.Validate(ctx)
}

// BaseDocumentService использует State вместо if doc.IsPosted()
if err := doc.State().CanDelete(); err != nil { return err }
```

Три состояния: `StateDraft`, `StatePosted`, `StateMarkedForDeletion` — stateless singletons.
Подробности и таблица переходов → [05-domain-layer.md](05-domain-layer.md).

---

## Добавление нового справочника (6 файлов)

1. `model.go` — struct + `Validate()`
2. `repo.go` (infra) — `NewXxxRepo()` с tableName/selectCols
3. `service.go` — `NewService()` + entity-specific hooks
4. handler — `NewXxxHandler()` + mapper functions
5. `CatalogRegistration` — реализация `CatalogRegistration` interface (`catalog_factory.go`)
6. `factory_registry.go` — добавить в `RegisterDefaults()` (1 строка)

Подробнее: [14-howto-new-entity.md](14-howto-new-entity.md).

---

## Связанные документы

- [05-domain-layer.md](05-domain-layer.md) — модели и сервисы
- [06-infrastructure-layer.md](06-infrastructure-layer.md) — handlers и repos
- [14-howto-new-entity.md](14-howto-new-entity.md) — пошаговое руководство
- [12-numerator.md](12-numerator.md) — автонумерация в hooks
