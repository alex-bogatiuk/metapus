# Domain Layer — Бизнес-логика

> Слой бизнес-логики и use cases. Расположен в `internal/domain/`. Зависит только от `internal/core/`, не знает о HTTP/Postgres.

---

## Структура bounded context

Каждый bounded context — подкаталог в `internal/domain/*`:

```
internal/domain/catalogs/counterparty/
├── model.go      # Struct + Validate(ctx)
├── repo.go       # Интерфейс Repository
├── service.go    # Бизнес-логика, оркестрация
└── hooks.go      # BeforeSave, AfterSave hooks (опционально)
```

---

## Справочники (Catalogs)

### Модель

Embed `entity.Catalog` (или `entity.HierarchicalCatalog` для иерархических) и добавить специфичные поля:

```go
type Counterparty struct {
    entity.Catalog
    INN           string `db:"inn" json:"inn"`
    LegalStatus   int    `db:"legal_status" json:"legalStatus"`
    ContactPerson string `db:"contact_person" json:"contactPerson,omitempty"`
    Phone         string `db:"phone" json:"phone,omitempty"`
    Email         string `db:"email" json:"email,omitempty"`
}
```

### Валидация

Реализация `entity.Validatable` — проверяет **только внутреннюю согласованность**, без обращений к БД:

```go
func (c *Counterparty) Validate(ctx context.Context) error {
    if c.Name == "" {
        return errors.New("наименование обязательно")
    }
    if c.LegalStatus == 2 && c.INN == "" {
        return errors.New("ИНН обязателен для юридических лиц")
    }
    return nil
}
```

### Интерфейс Repository

Объявляется **в domain** рядом с моделью. Репозиторий не протаскивает HTTP/DTO-типы:

```go
type Repository interface {
    GetByID(ctx context.Context, id id.ID) (*Counterparty, error)
    GetByINN(ctx context.Context, inn string) (*Counterparty, error)
    List(ctx context.Context, filter ListFilter) ([]Counterparty, int, error)
    Create(ctx context.Context, c *Counterparty) error
    Update(ctx context.Context, c *Counterparty) error
    Delete(ctx context.Context, id id.ID) error
    Lock(ctx context.Context, id id.ID) error // FOR UPDATE
}
```

### Сервис

Оркестрирует use case: Validate → Before hooks → Transaction(repo) → After hooks.

```go
type Service struct {
    *domain.CatalogService[*Counterparty]  // embedded generic CRUD
    repo      Repository
    numerator numerator.Generator
}
```

- **TxManager** берётся из `context` (Database-per-Tenant), не хранится в struct
- **Хуки** регистрируются при создании сервиса
- Подробнее о pipeline: [09-crud-pipeline.md](09-crud-pipeline.md)

---

## Документы (Documents)

### Модель

Embed `entity.Document` + трейты + табличные части:

```go
type Invoice struct {
    entity.Document
    CounterpartyID string           `db:"counterparty_id" json:"counterpartyId"`
    WarehouseID    string           `db:"warehouse_id" json:"warehouseId"`
    CurrencyID     string           `db:"currency_id" json:"currencyId"`
    TotalAmount    types.MinorUnits `db:"total_amount" json:"totalAmount"`
    Items          []InvoiceItem    `db:"-" json:"items"` // Табличная часть
}

type InvoiceItem struct {
    LineID     string          `db:"line_id" json:"lineId"`
    LineNumber int             `db:"line_number" json:"lineNumber"`
    ProductID  string          `db:"product_id" json:"productId"`
    Quantity   types.Quantity  `db:"quantity" json:"quantity"`
    Price      types.MinorUnits `db:"price" json:"price"`
    Amount     types.MinorUnits `db:"amount" json:"amount"`
}
```

### GenerateMovements

Документ сам генерирует движения для регистров — детерминированная функция:

```go
func (i *Invoice) GenerateMovements(version int) []StockMovement {
    movements := make([]StockMovement, len(i.Items))
    for idx, item := range i.Items {
        movements[idx] = StockMovement{
            Period:      i.Date,
            WarehouseID: i.WarehouseID,
            ProductID:   item.ProductID,
            RecorderID:  i.ID,
            RecorderVersion: version,
            RecordType:  RecordTypeExpense,
            Quantity:    item.Quantity.Neg(),
        }
    }
    return movements
}
```

### Calculate

Пересчёт сумм — тоже детерминированная функция, легко тестируемая:

```go
func (i *Invoice) Calculate() {
    var total types.MinorUnits
    for idx := range i.Items {
        i.Items[idx].Amount = /* integer arithmetic */
        total += i.Items[idx].Amount
    }
    i.TotalAmount = total
}
```

### Сервис документа

Документы используют `domain.BaseDocumentService[T, L]` — generic сервис по аналогии с `CatalogService[T]`:

```go
type Service struct {
    *domain.BaseDocumentService[*GoodsReceipt, GoodsReceiptLine]
}

func NewService(repo Repository, engine *posting.Engine, num numerator.Generator, txm tx.Manager, currencyStrategy domain.CurrencyResolveStrategy) *Service {
    base := domain.NewBaseDocumentService(domain.BaseDocumentServiceConfig[*GoodsReceipt, GoodsReceiptLine]{
        Repo:              repo,
        PostingEngine:     engine,
        Numerator:         num,
        TxManager:         txm,
        CurrencyResolver:  currencyStrategy,
        NumeratorPrefix:   "GR",
        NumeratorStrategy: NumeratorStrategy,
        EntityName:        "goods receipt",
    })
    return &Service{BaseDocumentService: base}
}

func (s *Service) Hooks() *domain.HookRegistry[*GoodsReceipt] {
    return s.BaseDocumentService.GetHooks()
}
```

`BaseDocumentService` предоставляет:
- **CRUD**: `Create`, `GetByID`, `Update`, `Delete`, `List`
- **Проведение**: `Post`, `Unpost`, `PostAndSave`, `UpdateAndRepost`
- **Пометка удаления**: `SetDeletionMark` (1С-стиль: снятие проведения + пометка атомарно)
- **Валюта**: автоматическое разрешение через `CurrencyResolveStrategy` (Strategy pattern)
- **Нумерация**: автогенерация номера через `numerator.Generator`
- **Хуки**: `BeforeCreate`, `AfterCreate`, `BeforeUpdate`, `AfterUpdate`

Модель документа должна реализовать `domain.DocumentEntity[L]`:
- `entity.Validatable` — валидация (рекомендуется `domain.ValidateDocumentLines` для строк)
- `posting.Postable` — проведение
- `domain.LinesAccessor[L]` — доступ к табличной части (`GetLines`, `SetLines`)
- `domain.CurrencyAwareDoc` — валюта (`GetCurrencyID`, `SetCurrencyID`, `GetContractID`, `GetOrganizationID`)
- `domain.ValidatableDocLine` — строки реализуют для общей валидации (`GetProductID`, `GetUnitID`, `GetCoefficient`, `GetQuantity`, `GetVATRateID`)

### Стратегия (Strategy) — CurrencyResolveStrategy

Разрешение валюты документа вынесено в интерфейс-стратегию:

```go
type CurrencyResolveStrategy interface {
    ResolveForDocument(ctx, explicitCurrencyID, contractID, organizationID) (id.ID, error)
}
```

Встроенная реализация — `documents.CurrencyResolver` (1С-стиль: Document → Contract → Organization → System).
Можно подключить альтернативную стратегию (e.g. `FixedCurrencyResolver` для внутренних перемещений).

### Стратегия (Strategy) — ValidateDocumentLines

Общая валидация строк табличных частей вынесена в `domain.ValidateDocumentLines[L]`:

```go
// Строка реализует ValidatableDocLine:
func (l GoodsReceiptLine) GetProductID() id.ID  { return l.ProductID }
func (l GoodsReceiptLine) GetUnitID() id.ID     { return l.UnitID }
// ... и т.д.

// В Validate() модели:
return domain.ValidateDocumentLines(g.Lines)
```

Правила: непустой список строк, обязательность product/unit/vatRate, coefficient > 0, quantity > 0.
Новые типы строк получают валидацию бесплатно, реализовав `ValidatableDocLine`.

### Строитель (Builder) — для создания документов

Каждый тип документа предоставляет `Builder` с fluent API для тестов, seed'ов и программного создания:

```go
doc := goods_receipt.NewBuilder(orgID, supplierID, warehouseID).
    WithCurrency(rubID).
    WithContract(&contractID).
    WithDescription("Поступление канцтоваров").
    AddLine(productID, unitID, 10, 15000, vatRateID, 20). // qty, price, vatRateID, %
    AddLine(productID2, unitID, 5, 8000, vatRateID, 20).
    Build()
```

Ключевые методы:
- `AddLine(product, unit, qty, price, vatRate, vatPercent)` — упрощённый (coefficient=1, discount=0)
- `AddLineDetailed(...)` — полный контроль над всеми полями
- `Build()` — возвращает документ, `MustBuild()` — с паникой при пустых обязательных полях
- `WithID`, `WithDate`, `WithNumber`, `WithCreatedBy` — удобно для детерминированных тестов
- `NewTestLine(productID, unitID, vatRateID)` — хелпер для минимальной строки в unit-тестах

Проведение делегируется централизованному `posting.Engine`. Подробнее: [10-posting-engine.md](10-posting-engine.md).

### Посетитель (Visitor) — мультирегистровые движения

Генерация движений при проведении документов использует паттерн **Visitor** (`posting/visitor.go`):

```go
// Интерфейс посетителя — один на каждый тип регистра
type RegisterVisitor interface {
    Name() string
    CollectMovements(ctx context.Context, doc Postable, set *MovementSet) error
}

// Интерфейс-источник — реализуется документами
type StockMovementSource interface {
    GenerateStockMovements(ctx context.Context) ([]entity.StockMovement, error)
}
```

Документы реализуют **source-интерфейсы** для нужных регистров (opt-in):
- `StockMovementSource` → GoodsReceipt (receipt), GoodsIssue (expense)
- Future: `CostMovementSource`, `SettlementMovementSource`

Engine итерирует зарегистрированных посетителей; каждый проверяет документ через type-assertion.
Расширение: `engine.AddVisitor(&XxxVisitor{})` — без изменения существующих документов.

### Декоратор (Decorator) — middleware-обёртки сервисов

Сервисы документов работают через каноничный интерфейс `DocumentService[T]` (`document_middleware.go`):

```go
type DocumentService[T any] interface {
    Create(ctx, entity T) error
    GetByID(ctx, id) (T, error)
    Update(ctx, entity T) error
    Delete(ctx, id) error
    Post(ctx, id) error
    Unpost(ctx, id) error
    PostAndSave(ctx, entity T) error
    UpdateAndRepost(ctx, entity T) error
    SetDeletionMark(ctx, id, marked) error
    List(ctx, filter) (ListResult[T], error)
}
```

**Декораторы** оборачивают `DocumentService[T]` для добавления cross-cutting concerns:

```go
// Логирование — каждый вызов логируется с method, duration, error
decorated := domain.WithLogging[*GoodsReceipt]("goods-receipt")(service)

// Композиция нескольких middleware
decorated := domain.Chain[*GoodsReceipt](
    domain.WithLogging[*GoodsReceipt]("goods-receipt"),
    // future: domain.WithMetrics, domain.WithTracing, ...
)(service)
```

**Wiring (document_factory.go):**
1. Создать concrete service + зарегистрировать hooks
2. Обернуть декоратором: `decorated := domain.WithLogging[T](name)(service)`
3. Передать `decorated` в handler constructor

Handlers принимают `domain.DocumentService[T]` — не конкретные типы сервисов.
Добавление нового middleware: реализовать `DocumentService[T]`, делегировать `next`.

### Состояние (State) — жизненный цикл документов

Каждый документ имеет lifecycle-состояние, определяемое флагами `Posted` и `DeletionMark`.
Вместо разбросанных `if d.Posted { ... }` проверок используется паттерн **State** (`document_state.go`):

```
┌───────────┐  Post   ┌──────────┐  Unpost  ┌───────────┐
│   Draft   │ ──────→ │  Posted  │ ──────→  │   Draft   │
│ !P && !DM │         │ P && !DM │          │ !P && !DM │
└───────────┘         └──────────┘          └───────────┘
      │                     │
      │ SetDeletionMark     │ SetDeletionMark (auto-unpost)
      ▼                     ▼
┌─────────────────────┐
│  MarkedForDeletion  │
│   !P && DM          │
└─────────────────────┘
```

**Интерфейс `DocumentState`:**
```go
type DocumentState interface {
    Name() DocumentStateName   // "draft" | "posted" | "marked_for_deletion"
    CanModify() error          // Можно ли редактировать?
    CanPost() error            // Можно ли провести?
    CanUnpost() error          // Можно ли отменить проведение?
    CanDelete() error          // Можно ли удалить?
}
```

| Операция   | Draft | Posted | MarkedForDeletion |
|------------|-------|--------|-------------------|
| CanModify  | ✅    | ✗      | ✅                |
| CanPost    | ✅    | ✅ (repost) | ✗            |
| CanUnpost  | ✗     | ✅     | ✗                 |
| CanDelete  | ✅    | ✗      | ✅                |

**Использование:**
```go
// В entity — делегация к текущему состоянию
func (d *Document) State() DocumentState {
    return ResolveDocumentState(d.Posted, d.DeletionMark)
}
func (d *Document) CanModify() error { return d.State().CanModify() }

// В сервисе — вместо doc.IsPosted()
if err := doc.State().CanDelete(); err != nil { return err }
```

Состояния — stateless singletons; `ResolveDocumentState()` возвращает нужное по флагам.

---

## Регистры

### Регистры накопления (Accumulation)

Два типа таблиц:
- **Movements** (`reg_stock_movements`) — immutable ledger
- **Balances** (`reg_stock_balances`) — горячий кэш остатков

```go
type StockMovement struct {
    Period          time.Time
    WarehouseID     string
    ProductID       string
    RecorderID      string
    RecorderVersion int
    LineID          string
    RecordType      string  // "receipt" | "expense"
    Quantity        types.Quantity
}

type StockBalance struct {
    WarehouseID string
    ProductID   string
    Quantity    types.Quantity
    Reserved    types.Quantity
}
```

### Регистры сведений (Information)

- **Периодические** — история изменения (курсы валют). Получение среза последних через `DISTINCT ON`.
- **Непериодические** — текущее состояние без истории (штрихкоды).

---

## Хуки (Hooks)

`HookRegistry[T]` — generic реестр lifecycle hooks (аналог подписок на события в 1С):

**6 событий:** `BeforeCreate`, `AfterCreate`, `BeforeUpdate`, `AfterUpdate`, `BeforeDelete`, `AfterDelete`

- **Before hooks** выполняются ДО транзакции. Ошибка прерывает операцию.
- **After hooks** выполняются ПОСЛЕ коммита. Ошибки логируются, не прерывают.
- Хуки выполняются **в порядке регистрации**.

Типичные задачи hooks:
- Автонумерация (`numerator.GetNextNumber`)
- Проверка уникальности (INN, code)
- Обогащение полей (audit: `CreatedBy`, `UpdatedBy`)
- Отправка уведомлений (after hooks)

Подробнее: [09-crud-pipeline.md](09-crud-pipeline.md).

---

## Правила Domain слоя

- **Bounded context = подкаталог** (`catalogs/*`, `documents/*`, `registers/*`, `auth/*`)
- Доменные типы **самодостаточны**: инварианты в `Validate(ctx)`, бизнес-методы на структурах
- Сервис **не знает** о конкретных провайдерах/транспорте
- `Validate(ctx)` **не ходит** в БД/сеть
- Вычисления (`Calculate`, `GenerateMovements`) должны быть **детерминированы**
- `TxManager` и tenant берутся из `context.Context`

---

## Связанные документы

- [04-core-layer.md](04-core-layer.md) — базовые типы (BaseEntity, Catalog, Document)
- [09-crud-pipeline.md](09-crud-pipeline.md) — полный CRUD pipeline с хуками
- [10-posting-engine.md](10-posting-engine.md) — проведение документов
- [14-howto-new-entity.md](14-howto-new-entity.md) — пошаговое добавление сущности
