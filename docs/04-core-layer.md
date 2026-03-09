# Core Layer — Ядро платформы

> Фундаментальные типы, политики и контракты, от которых зависят все вышележащие слои. Расположен в `internal/core/`.

---

## BaseEntity — фундамент для всех объектов

```go
// internal/core/entity/base.go
type BaseEntity struct {
    ID           id.ID      `db:"id" json:"id"`
    DeletionMark bool       `db:"deletion_mark" json:"deletionMark"`
    Version      int        `db:"version" json:"version"`       // Optimistic Locking
    Attributes   Attributes `db:"attributes" json:"attributes,omitempty"` // JSONB custom fields
}
```

- **ID** — UUIDv7 (сортируемый по времени)
- **DeletionMark** — soft delete (физическое удаление не используется)
- **Version** — optimistic locking (см. [11-transactions.md](11-transactions.md))
- **Attributes** — JSONB для кастомных полей

> **Database-per-Tenant:** tenant discriminator в бизнес-таблицах **НЕ нужен**. Tenant/tx/pool доступны через `context.Context`.

---

## Attributes — типобезопасный JSONB

```go
type Attributes map[string]any
```

Реализует `sql.Scanner` с `decoder.UseNumber()` — числа сохраняются как `json.Number`, а не `float64` (критично для точности).

**Типизированные геттеры:**
- `GetDecimal(key string) decimal.Decimal`
- `GetString(key string) string`

---

## Catalog — единый тип справочника с metadata-driven иерархией

```go
type Catalog struct {
    BaseCatalog
    Code     string `db:"code" json:"code"`
    Name     string `db:"name" json:"name"`
    ParentID *id.ID `db:"parent_id" json:"parentId,omitempty"`
    IsFolder bool   `db:"is_folder" json:"isFolder"`
}
```

**Иерархия управляется метаданными**, а не типом struct. Поля `ParentID`/`IsFolder` присутствуют в **всех** справочниках на уровне БД, но активируются только через `CatalogMeta`:

```go
type CatalogMeta struct {
    Hierarchical       bool          // Поддерживает ли иерархию
    HierarchyType      HierarchyType // groups_and_items | items_only
    MaxDepth           int           // Лимит вложенности (0 = без лимита)
    FolderAsParentOnly bool          // Parent может быть только папкой
}
```

**Реестр метаданных** (`catalog_meta.go`):
- Иерархические: `nomenclature`, `counterparty`, `warehouse`
- Плоские: `organization`, `currency`, `unit`, `vat_rate`, `contract`

Для плоских каталогов `GetTree`/`GetPath` возвращают `400 Bad Request`.
Расширение: `RegisterCatalogMeta("new_catalog", CatalogMeta{...})`.

**Валидация иерархии** (`HierarchyValidator`):
- Обнаружение циклов (обход вверх по parent chain)
- Контроль глубины вложенности (`MaxDepth`)
- Проверка «parent должен быть папкой» (для `GroupsAndItems`)


## BaseDocument — стандарт для документов

```go
type BaseDocument struct {
    BaseEntity
    CreatedAt time.Time `db:"created_at" json:"createdAt"`
    UpdatedAt time.Time `db:"updated_at" json:"updatedAt"`
    CreatedBy string    `db:"created_by" json:"createdBy,omitempty"`
    UpdatedBy string    `db:"updated_by" json:"updatedBy,omitempty"`
}

type Document struct {
    BaseDocument
    Number         string    `db:"number" json:"number"`
    Date           time.Time `db:"date" json:"date"`
    OrganizationID id.ID     `db:"organization_id" json:"organizationId"`
    Posted         bool      `db:"posted" json:"posted"`
    PostedVersion  int       `db:"posted_version" json:"postedVersion"`
}
```

**Документы включают audit-поля**, т.к. фиксируют факт хозяйственной операции.

## Трейты (Mixins)

Композиция для добавления стандартных измерений:

```go
type CurrencyAware struct {
    CurrencyID id.ID `db:"currency_id" json:"currencyId"`
}

// Пример сборки документа:
type GoodsReceipt struct {
    Document      // BaseDocument + Number, Date, Posted...
    CurrencyAware // Трейт валюты
    // ... специфичные поля
}
```

---

## UUIDv7 — генерация идентификаторов

```go
// internal/core/id/uuid.go
type ID = uuid.UUID

func New() ID        // Генерирует UUIDv7 (сортируемый по времени)
func Parse(s string) (ID, error)
func IsZero(id ID) bool
```

UUIDv7 обеспечивает:
- Монотонно возрастающие ID (лучше для B-tree индексов)
- Встроенный timestamp (можно извлечь время создания)

---

## AppError — структурированные ошибки (RFC 7807)

```go
// internal/core/apperror/error.go
type AppError struct {
    Code    string                 `json:"code"`
    Message string                 `json:"message"`
    Details map[string]interface{} `json:"details,omitempty"`
    Err     error                  `json:"-"` // Внутренняя ошибка (для логов)
}
```

### Коды ошибок

| Код | HTTP | Описание |
|-----|------|----------|
| `VALIDATION_ERROR` | 400 | Ошибка валидации |
| `NOT_FOUND` | 404 | Сущность не найдена |
| `CONFLICT` | 409 | Конфликт (дубликат) |
| `INSUFFICIENT_STOCK` | 409 | Недостаточно товара |
| `CONCURRENT_MODIFICATION` | 409 | Optimistic lock conflict |
| `CLOSED_PERIOD` | 403 | Закрытый период |
| `UNAUTHORIZED` | 401 | Не аутентифицирован |
| `FORBIDDEN` | 403 | Нет прав |
| `INTERNAL_ERROR` | 500 | Внутренняя ошибка |

### Конструкторы

```go
apperror.NewValidationError(message, details)
apperror.NewNotFoundError(entity, id)
apperror.NewInsufficientStockError(productID, requested, available)
apperror.NewConcurrentModificationError()
```

---

## Validatable — интерфейс валидации

```go
type Validatable interface {
    Validate(ctx context.Context) error
}
```

**Правила:**
- Проверяет **только внутреннюю согласованность** сущности
- **НЕ ходит** в БД/сеть
- Примеры: `EndDate >= StartDate`, `Amount > 0`, `INN format check`

---

## Transaction Manager Interface

```go
// internal/core/tx/tx.go
type Manager interface {
    RunInTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

type ReadOnlyManager interface {
    Manager
    ReadOnly(ctx context.Context, fn func(ctx context.Context) error) error
}
```

Domain зависит от **интерфейса**, реализация — в infrastructure. Подробнее: [11-transactions.md](11-transactions.md).

---

## Tenant Context

```go
// internal/core/tenant/context.go
func WithPool(ctx, pool)          // Инжекция pgxpool.Pool
func WithTxManager(ctx, txm)      // Инжекция tx.Manager
func WithTenant(ctx, tenant)      // Инжекция *Tenant struct

func MustGetTxManager(ctx) tx.Manager  // Извлечение (panic если нет)
func GetTenantID(ctx) uuid.UUID
```

Все эти значения инжектируются middleware `TenantDB` и доступны во всех слоях через context. Подробнее: [07-multi-tenancy.md](07-multi-tenancy.md).

---

## Связанные документы

- [02-architecture.md](02-architecture.md) — общая архитектура
- [05-domain-layer.md](05-domain-layer.md) — как domain использует core-типы
- [11-transactions.md](11-transactions.md) — TxManager в деталях
- [07-multi-tenancy.md](07-multi-tenancy.md) — tenant context chain
