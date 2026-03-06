# How-To: Добавление новой сущности

> Пошаговое руководство по добавлению нового справочника или документа. Включает полный пример и checklist.

---

## Новый справочник (Catalog) — 8 шагов, ~22 минуты

### Шаг 1: Миграция БД (5 мин)

**Файл:** `db/migrations/NNNNN_cat_your_entity.sql`

```sql
-- +goose Up
SELECT pg_advisory_lock(hashtext('metapus_migrations'));

CREATE TABLE IF NOT EXISTS cat_your_entities (
    -- Base fields (from entity.BaseEntity)
    id UUID PRIMARY KEY DEFAULT gen_random_uuid_v7(),
    deletion_mark BOOLEAN NOT NULL DEFAULT FALSE,
    version INT NOT NULL DEFAULT 1,
    attributes JSONB DEFAULT '{}',
    
    -- CDC-ready columns
    _deleted_at TIMESTAMPTZ,
    _txid BIGINT DEFAULT txid_current(),
    
    -- Catalog fields (from entity.Catalog)
    code VARCHAR(50) NOT NULL,
    name VARCHAR(255) NOT NULL,
    parent_id UUID REFERENCES cat_your_entities(id),
    is_folder BOOLEAN NOT NULL DEFAULT FALSE,
    
    -- Entity-specific fields
    custom_field VARCHAR(255) NOT NULL
);

-- Indexes
CREATE UNIQUE INDEX idx_cat_your_entities_code
    ON cat_your_entities (code) WHERE deletion_mark = FALSE;
CREATE INDEX idx_cat_your_entities_name
    ON cat_your_entities USING gin (name gin_trgm_ops);

-- CDC triggers
CREATE INDEX IF NOT EXISTS idx_cat_your_entities_txid
    ON cat_your_entities (_txid) WHERE _deleted_at IS NULL;
CREATE TRIGGER trg_cat_your_entities_txid
    BEFORE UPDATE ON cat_your_entities
    FOR EACH ROW EXECUTE FUNCTION update_txid_column();
CREATE TRIGGER trg_cat_your_entities_soft_delete
    BEFORE UPDATE OF deletion_mark ON cat_your_entities
    FOR EACH ROW EXECUTE FUNCTION soft_delete_with_timestamp();

SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
SELECT pg_advisory_lock(hashtext('metapus_migrations'));
DROP TRIGGER IF EXISTS trg_cat_your_entities_soft_delete ON cat_your_entities;
DROP TRIGGER IF EXISTS trg_cat_your_entities_txid ON cat_your_entities;
DROP TABLE IF EXISTS cat_your_entities;
SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
```

### Шаг 2: Модель (5 мин)

**Файл:** `internal/domain/catalogs/your_entity/model.go`

```go
package your_entity

import (
    "context"
    "metapus/internal/core/apperror"
    "metapus/internal/core/entity"
)

type YourEntity struct {
    entity.Catalog
    CustomField string `db:"custom_field" json:"customField"`
}

func NewYourEntity(code, name string) *YourEntity {
    return &YourEntity{Catalog: entity.NewCatalog(code, name)}
}

func (e *YourEntity) Validate(ctx context.Context) error {
    if err := e.Catalog.Validate(ctx); err != nil {
        return err
    }
    if e.CustomField == "" {
        return apperror.NewValidation("custom field is required").
            WithDetail("field", "customField")
    }
    return nil
}
```

### Шаг 3: Репозиторий (3 мин)

**Файл:** `internal/infrastructure/storage/postgres/catalog_repo/your_entity.go`

```go
package catalog_repo

import (
    "metapus/internal/domain/catalogs/your_entity"
    "metapus/internal/infrastructure/storage/postgres"
)

type YourEntityRepo struct {
    *BaseCatalogRepo[*your_entity.YourEntity]
}

func NewYourEntityRepo() *YourEntityRepo {
    return &YourEntityRepo{
        BaseCatalogRepo: NewBaseCatalogRepo[*your_entity.YourEntity](
            "cat_your_entities",
            postgres.ExtractDBColumns[your_entity.YourEntity](),
        ),
    }
}
```

### Шаг 4: Сервис (5 мин)

**Файл:** `internal/domain/catalogs/your_entity/service.go`

```go
package your_entity

import (
    "metapus/internal/domain"
    "metapus/pkg/numerator"
)

type Repository interface {
    domain.CatalogRepository[*YourEntity]
}

type Service struct {
    *domain.CatalogService[*YourEntity]
    repo Repository
}

func NewService(repo Repository, numerator *numerator.Service) *Service {
    base := domain.NewCatalogService(domain.CatalogServiceConfig[*YourEntity]{
        Repo:       repo,
        TxManager:  nil, // из context (Database-per-Tenant)
        Numerator:  numerator,
        EntityName: "your_entity",
    })
    return &Service{CatalogService: base, repo: repo}
}
```

### Шаг 5: DTO (8 мин)

**Файл:** `internal/infrastructure/http/v1/dto/your_entity.go`

```go
package dto

type CreateYourEntityRequest struct {
    Code        string            `json:"code"`
    Name        string            `json:"name" binding:"required"`
    CustomField string            `json:"customField" binding:"required"`
    ParentID    *string           `json:"parentId"`
    IsFolder    bool              `json:"isFolder"`
    Attributes  entity.Attributes `json:"attributes"`
}

type UpdateYourEntityRequest struct {
    // аналогично Create + Version int `json:"version" binding:"required"`
}

type YourEntityResponse struct {
    ID          string `json:"id"`
    Code        string `json:"code"`
    Name        string `json:"name"`
    CustomField string `json:"customField"`
    // ...
}

func (r *CreateYourEntityRequest) ToEntity() *your_entity.YourEntity { ... }
func (r *UpdateYourEntityRequest) ApplyTo(e *your_entity.YourEntity) error { ... }
func FromYourEntity(e *your_entity.YourEntity) *YourEntityResponse { ... }
```

### Шаг 6: Handler (3 мин)

**Файл:** `internal/infrastructure/http/v1/handlers/your_entity.go`

```go
package handlers

type YourEntityHandler = CatalogHandler[
    *your_entity.YourEntity,
    dto.CreateYourEntityRequest,
    dto.UpdateYourEntityRequest,
]

func NewYourEntityHandler(base *BaseHandler, service *your_entity.Service) *YourEntityHandler {
    return NewCatalogHandler(base, CatalogHandlerConfig[...]{
        Service:      service.CatalogService,
        EntityName:   "your_entity",
        MapCreateDTO: func(req) *YourEntity { return req.ToEntity() },
        MapUpdateDTO: func(req, existing) *YourEntity { req.ApplyTo(existing); return existing },
        MapToDTO:     func(entity) any { return dto.FromYourEntity(entity) },
    })
}
```

### Шаг 7: Регистрация роутов (1 мин)

**Файл:** `internal/infrastructure/http/v1/router.go`

```go
{
    repo := catalog_repo.NewYourEntityRepo()
    service := your_entity.NewService(repo, cfg.Numerator)
    handler := handlers.NewYourEntityHandler(baseHandler, service)
    RegisterCatalogRoutes(catalogs.Group("/your-entities"), handler, "catalog:your_entity")
}
```

### Шаг 8: Permissions (2 мин)

```sql
INSERT INTO auth_permissions (id, name, resource_type, resource_name, action) VALUES
    (gen_random_uuid_v7(), 'catalog:your_entity:read', 'catalog', 'your_entity', 'read'),
    (gen_random_uuid_v7(), 'catalog:your_entity:create', 'catalog', 'your_entity', 'create'),
    (gen_random_uuid_v7(), 'catalog:your_entity:update', 'catalog', 'your_entity', 'update'),
    (gen_random_uuid_v7(), 'catalog:your_entity:delete', 'catalog', 'your_entity', 'delete');
```

### Результат

API эндпоинты (автоматически):
```
GET    /api/v1/catalog/your-entities          — List
POST   /api/v1/catalog/your-entities          — Create
GET    /api/v1/catalog/your-entities/:id      — Get
PUT    /api/v1/catalog/your-entities/:id      — Update
DELETE /api/v1/catalog/your-entities/:id      — Delete
POST   /api/v1/catalog/your-entities/:id/deletion-mark — Soft delete
GET    /api/v1/catalog/your-entities/tree     — Tree view
```

---

## Новый документ (Document) — дополнительно

### Модель

Embed `entity.Document` + `entity.CurrencyAware` + табличная часть.
Реализовать `domain.DocumentEntity[L]`: `GetLines`, `SetLines`, `GetContractID`, и `posting.Postable`.

```go
type YourDocument struct {
    entity.Document
    entity.CurrencyAware
    CounterpartyID id.ID          `db:"counterparty_id" json:"counterpartyId"`
    ContractID     *id.ID         `db:"contract_id" json:"contractId,omitempty"`
    WarehouseID    id.ID          `db:"warehouse_id" json:"warehouseId"`
    Lines          []YourDocLine  `db:"-" json:"lines"`
}

type YourDocLine struct {
    LineID      id.ID            `db:"line_id" json:"lineId"`
    LineNo      int              `db:"line_no" json:"lineNo"`
    ProductID   id.ID            `db:"product_id" json:"productId"`
    UnitID      id.ID            `db:"unit_id" json:"unitId"`
    Coefficient decimal.Decimal  `db:"coefficient" json:"coefficient"`
    Quantity    types.Quantity   `db:"quantity" json:"quantity"`
    VATRateID   id.ID            `db:"vat_rate_id" json:"vatRateId"`
}

// --- LinesAccessor ---
func (d *YourDocument) GetLines() []YourDocLine    { return d.Lines }
func (d *YourDocument) SetLines(l []YourDocLine)   { d.Lines = l }

// --- CurrencyAwareDoc ---
func (d *YourDocument) GetContractID() *id.ID      { return d.ContractID }

// --- ValidatableDocLine (Strategy) ---
func (l YourDocLine) GetProductID() id.ID              { return l.ProductID }
func (l YourDocLine) GetUnitID() id.ID                 { return l.UnitID }
func (l YourDocLine) GetCoefficient() decimal.Decimal   { return l.Coefficient }
func (l YourDocLine) GetQuantity() types.Quantity       { return l.Quantity }
func (l YourDocLine) GetVATRateID() id.ID               { return l.VATRateID }

// В Validate():
return domain.ValidateDocumentLines(d.Lines)  // общая валидация строк бесплатно
```

### Репозиторий

Интерфейс должен соответствовать `domain.DocumentRepository[T, L]`:

```go
type Repository interface {
    Create(ctx context.Context, doc *YourDocument) error
    GetByID(ctx context.Context, docID id.ID) (*YourDocument, error)
    GetByNumber(ctx context.Context, number string) (*YourDocument, error)
    Update(ctx context.Context, doc *YourDocument) error
    Delete(ctx context.Context, docID id.ID) error
    GetLines(ctx context.Context, docID id.ID) ([]YourDocLine, error)
    SaveLines(ctx context.Context, docID id.ID, lines []YourDocLine) error
    List(ctx context.Context, filter domain.ListFilter) (domain.ListResult[*YourDocument], error)
    GetForUpdate(ctx context.Context, docID id.ID) (*YourDocument, error)
}
```

### Сервис

Embed `domain.BaseDocumentService[T, L]` — все CRUD + проведение + нумерация автоматически:

```go
type Service struct {
    *domain.BaseDocumentService[*YourDocument, YourDocLine]
}

func NewService(repo Repository, engine *posting.Engine, num numerator.Generator, txm tx.Manager, currencyStrategy domain.CurrencyResolveStrategy) *Service {
    base := domain.NewBaseDocumentService(domain.BaseDocumentServiceConfig[*YourDocument, YourDocLine]{
        Repo:              repo,
        PostingEngine:     engine,
        Numerator:         num,
        TxManager:         txm,
        CurrencyResolver:  currencyStrategy,
        NumeratorPrefix:   "YD",
        NumeratorStrategy: NumeratorStrategy,
        EntityName:        "your document",
    })
    return &Service{BaseDocumentService: base}
}

func (s *Service) Hooks() *domain.HookRegistry[*YourDocument] {
    return s.BaseDocumentService.GetHooks()
}
```

Методы, доступные из коробки (через embedding):
`Create`, `GetByID`, `Update`, `Delete`, `List`, `Post`, `Unpost`, `PostAndSave`, `UpdateAndRepost`, `SetDeletionMark`

### Регистрация (Abstract Factory)

Документы регистрируются через паттерн **Abstract Factory** (`document_factory.go`).
Каждый тип документа реализует интерфейс `DocumentRegistration`:

```go
// internal/infrastructure/http/v1/document_factory.go

type YourDocRegistration struct{}

func (r *YourDocRegistration) RoutePrefix() string { return "your-docs" }
func (r *YourDocRegistration) Permission() string  { return "document:your_doc" }

func (r *YourDocRegistration) Build(deps DocumentDeps) DocumentRouteHandler {
    repo := document_repo.NewYourDocRepo()
    service := your_doc.NewService(repo, deps.PostingEngine, deps.Numerator, nil, deps.CurrencyResolver)

    // Audit hooks
    service.Hooks().OnBeforeCreate(func(ctx context.Context, doc *your_doc.YourDocument) error {
        audit.EnrichCreatedByDirect(ctx, &doc.CreatedBy, &doc.UpdatedBy)
        return nil
    })
    service.Hooks().OnBeforeUpdate(func(ctx context.Context, doc *your_doc.YourDocument) error {
        audit.EnrichUpdatedByDirect(ctx, &doc.UpdatedBy)
        return nil
    })

    return handlers.NewYourDocHandler(deps.BaseHandler, service)
}
```

Затем добавить фабрику в реестр:

```go
var documentFactories = []DocumentRegistration{
    &GoodsReceiptRegistration{},
    &GoodsIssueRegistration{},
    &YourDocRegistration{},        // ← добавить сюда
}
```

Роуты, permissions и shared dependencies подключатся автоматически через `RegisterDocumentRoutes`.

### Движения по регистрам (Visitor)

Документ реализует **source-интерфейсы** для нужных регистров (`posting/visitor.go`):

```go
// Для товарных остатков:
func (d *YourDocument) GenerateStockMovements(ctx context.Context) ([]entity.StockMovement, error) {
    newVersion := d.PostedVersion + 1
    var movements []entity.StockMovement
    for _, line := range d.Lines {
        movements = append(movements, entity.NewStockMovement(
            d.ID, d.GetDocumentType(), newVersion, d.Date,
            entity.RecordTypeReceipt, // или RecordTypeExpense
            d.WarehouseID, line.ProductID, baseQty,
        ))
    }
    return movements, nil
}

// Compile-time check:
var _ posting.StockMovementSource = (*YourDocument)(nil)
```

Engine автоматически обнаружит source-интерфейс через `StockVisitor` (type-assertion).
Для будущих регистров (cost, settlement) — реализовать соответствующий `XxxMovementSource`.

### Builder (для тестов и seed'ов)

Создать файл `builder.go` с fluent API:

```go
type Builder struct {
    doc *YourDocument
}

func NewBuilder(organizationID, counterpartyID, warehouseID id.ID) *Builder {
    return &Builder{doc: NewYourDocument(organizationID, counterpartyID, warehouseID)}
}

func (b *Builder) WithCurrency(id id.ID) *Builder   { b.doc.CurrencyID = id; return b }
func (b *Builder) WithDescription(d string) *Builder { b.doc.Description = d; return b }

// AddLine — упрощённый (coefficient=1, discount=0)
func (b *Builder) AddLine(productID, unitID id.ID, qty int, price types.MinorUnits, vatRateID id.ID, vatPct int) *Builder {
    b.doc.AddLine(productID, unitID, decimal.NewFromInt(1), types.NewQuantityFromFloat64(float64(qty)), price, vatRateID, vatPct, decimal.Zero)
    return b
}

func (b *Builder) Build() *YourDocument      { return b.doc }
func (b *Builder) MustBuild() *YourDocument   { /* panic on missing required */ return b.doc }
```

Использование:
```go
doc := your_doc.NewBuilder(orgID, cpID, whID).
    WithCurrency(rubID).
    AddLine(productID, unitID, 10, 15000, vatRateID, 20).
    MustBuild()
```

---

## Типичные задачи

### Добавить custom метод в репозиторий
```go
func (r *YourRepo) FindByCustomField(ctx context.Context, value string) (*YourEntity, error) {
    q := r.baseSelect(ctx).Where(squirrel.Eq{"custom_field": value}).Limit(1)
    return r.FindOne(ctx, q)
}
```

### Добавить custom валидацию через hooks
```go
base.Hooks().OnBeforeCreate(svc.checkUniqueness)
base.Hooks().OnAfterUpdate(svc.sendNotification)
```

### Добавить специфичный роут
```go
RegisterCatalogRoutes(group, handler, "catalog:entity")
group.GET("/:id/custom", middleware.RequirePermission("..."), handler.CustomMethod)
```

---

## Checklist

- [ ] Миграция БД (`db/migrations/`)
- [ ] Модель (`internal/domain/catalogs/` или `documents/`)
- [ ] Репозиторий (`internal/infrastructure/storage/postgres/catalog_repo/` или `document_repo/`)
  - [ ] Использовать `ExtractDBColumns[T]()`
- [ ] Сервис (`internal/domain/catalogs/` или `documents/`)
- [ ] DTO (`internal/infrastructure/http/v1/dto/`)
  - [ ] CreateXRequest, UpdateXRequest, XResponse
- [ ] Handler (`internal/infrastructure/http/v1/handlers/`)
- [ ] Builder (`builder.go` в пакете документа) — fluent API для тестов и seed'ов
- [ ] Регистрация: `DocumentRegistration` в `document_factory.go` + добавить в `documentFactories`
- [ ] Декоратор: обернуть сервис `domain.WithLogging[T](name)(service)` в `Build()` фабрики
- [ ] Permissions в seed миграцию
- [ ] Проверить компиляцию: `go build ./cmd/server`
- [ ] Протестировать API

---

## Связанные документы

- [09-crud-pipeline.md](09-crud-pipeline.md) — как работает generic CRUD
- [15-naming-conventions.md](15-naming-conventions.md) — правила именования таблиц
- [17-development-rules.md](17-development-rules.md) — правила миграций
- [12-numerator.md](12-numerator.md) — автонумерация
