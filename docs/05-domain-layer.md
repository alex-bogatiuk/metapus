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

Включает `CatalogService` + методы проведения:

```
service/
├── crud.go     # Create, Update, Delete (generic pipeline)
└── posting.go  # Post, Unpost (через posting.Engine)
```

Проведение делегируется централизованному `posting.Engine`. Подробнее: [10-posting-engine.md](10-posting-engine.md).

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
