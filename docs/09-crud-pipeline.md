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

### List (с пагинацией и фильтрацией)
- WHERE: search, parentId, isFolder, deletionMark, advancedFilters
- ORDER BY, LIMIT, OFFSET
- COUNT(*) для totalCount (отдельный запрос)

### Delete (soft)
```sql
UPDATE SET deletion_mark = true WHERE id = $1
```

---

## Wiring — связка в Router

Для каждого справочника в `router.go`:

```go
{
    repo := catalog_repo.NewCounterpartyRepo()
    service := counterparty.NewService(repo, cfg.Numerator)
    handler := handlers.NewCounterpartyHandler(baseHandler, service)
    RegisterCatalogRoutes(catalogs.Group("/counterparties"), handler, "catalog:counterparty")
}
```

`RegisterCatalogRoutes` автоматически подключает permission middleware:
- `GET ""` + `RequirePermission("catalog:counterparty:read")` → List
- `POST ""` + `RequirePermission("catalog:counterparty:create")` → Create
- `GET "/:id"` + `RequirePermission("catalog:counterparty:read")` → Get
- `PUT "/:id"` + `RequirePermission("catalog:counterparty:update")` → Update
- `DELETE "/:id"` + `RequirePermission("catalog:counterparty:delete")` → Delete

Документы аналогично + shared dependencies (stockRepo, stockService, postingEngine):

```go
{
    repo := document_repo.NewGoodsReceiptRepo()
    service := goods_receipt.NewService(repo, postingEngine, cfg.Numerator, nil, currencyResolver)
    // Audit hooks
    service.Hooks().OnBeforeCreate(func(ctx context.Context, doc *goods_receipt.GoodsReceipt) error {
        audit.EnrichCreatedByDirect(ctx, &doc.CreatedBy, &doc.UpdatedBy)
        return nil
    })
    handler := handlers.NewGoodsReceiptHandler(baseHandler, service)
    RegisterDocumentRoutes(docsGroup.Group("/goods-receipt"), handler, "document:goods_receipt")
}
```

`BaseDocumentService[T, L]` предоставляет единый pipeline для документов (аналог `CatalogService[T]`):

**Create pipeline:**
1. `hooks.RunBeforeCreate(ctx, doc)` — enrichment (CreatedBy, UpdatedBy)
2. `ResolveCurrency(ctx, doc)` — цепочка: Document → Contract → Organization → System
3. `doc.Validate(ctx)` — внутренняя валидация
4. `GenerateNumber(ctx, doc)` — автонумерация (если номер пуст)
5. `tx { repo.Create + repo.SaveLines }` — атомарная запись
6. `hooks.RunAfterCreate(ctx, doc)` — уведомления (ошибки логируются)

**Дополнительные методы:** `Post`, `Unpost`, `PostAndSave`, `UpdateAndRepost`, `SetDeletionMark`

### Abstract Factory — регистрация документов

Каждый тип документа регистрируется через `DocumentRegistration` (Abstract Factory, `document_factory.go`):

```go
type DocumentRegistration interface {
    RoutePrefix() string                              // "goods-receipt"
    Permission() string                               // "document:goods_receipt"
    Build(deps DocumentDeps) DocumentRouteHandler     // repo → service → handler
}
```

Реестр (`documentFactories`) итерируется в `registerDocumentRoutes`:
```go
for _, factory := range documentFactories {
    handler := factory.Build(deps)
    RegisterDocumentRoutes(docsGroup.Group("/"+factory.RoutePrefix()), handler, factory.Permission())
}
```

Добавление нового документа: реализовать `DocumentRegistration` и добавить в `documentFactories`.

### Visitor — мультирегистровые движения

Генерация движений по регистрам реализована через паттерн **Visitor** (`posting/visitor.go`).

**Участники:**
- `RegisterVisitor` — интерфейс посетителя (`Name()`, `CollectMovements(ctx, doc, set)`)
- `StockMovementSource` — интерфейс-источник, реализуемый документами (`GenerateStockMovements`)
- `StockVisitor` — конкретный посетитель для регистра товарных остатков
- `Engine.visitors` — реестр посетителей; итерируется при проведении

**Как работает:**

```
Engine.doPost(doc)
  → engine.collectMovements(doc)
      → for each visitor:
           visitor.CollectMovements(ctx, doc, set)
             → type-assert doc.(StockMovementSource)
             → doc.GenerateStockMovements(ctx) → []StockMovement
  → engine.validateStockAvailability(set)
  → engine.recordMovements(set)
```

**Расширение (новый регистр):**
1. Определить `XxxMovementSource` interface в `posting/visitor.go`
2. Создать `XxxVisitor` реализующий `RegisterVisitor`
3. Зарегистрировать: `engine.AddVisitor(&XxxVisitor{})`
4. Документы реализуют `XxxMovementSource` для генерации движений

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

## Добавление нового справочника (5 файлов)

1. `model.go` — struct + `Validate()`
2. `repo.go` (infra) — `NewXxxRepo()` с tableName/selectCols
3. `service.go` — `NewService()` + entity-specific hooks
4. handler — `NewXxxHandler()` + mapper functions
5. `router.go` — 4 строки wiring

Подробнее: [14-howto-new-entity.md](14-howto-new-entity.md).

---

## Связанные документы

- [05-domain-layer.md](05-domain-layer.md) — модели и сервисы
- [06-infrastructure-layer.md](06-infrastructure-layer.md) — handlers и repos
- [14-howto-new-entity.md](14-howto-new-entity.md) — пошаговое руководство
- [12-numerator.md](12-numerator.md) — автонумерация в hooks
