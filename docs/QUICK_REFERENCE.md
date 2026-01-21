# Быстрая справка: Добавление сущностей

## Новый Справочник (Catalog)

### 1. Модель
```go
// internal/domain/catalogs/your_entity/model.go
type YourEntity struct {
    entity.Catalog
    
    CustomField string `db:"custom_field" json:"customField"`
}

func (e *YourEntity) Validate(ctx context.Context) error {
    // Валидация
    return e.Catalog.Validate(ctx)
}
```

### 2. Репозиторий
```go
// internal/infrastructure/storage/postgres/catalog_repo/your_entity.go
func NewYourEntityRepo() *YourEntityRepo {
    return &YourEntityRepo{
        BaseCatalogRepo: NewBaseCatalogRepo[*your_entity.YourEntity](
            "cat_your_entities",
            postgres.ExtractDBColumns[your_entity.YourEntity](), // ✨
        ),
    }
}
```

### 3. Сервис
```go
// internal/domain/catalogs/your_entity/service.go
func NewService(repo Repository, numerator *numerator.Service) *Service {
    base := domain.NewCatalogService(domain.CatalogServiceConfig[*YourEntity]{
        Repo:       repo,
        TxManager:  nil, // TxManager берётся из context (Database-per-Tenant)
        Numerator:  numerator,
        EntityName: "your_entity",
    })
    
    return &Service{CatalogService: base, repo: repo}
}
```

### 4. Handler
```go
// internal/infrastructure/http/v1/handlers/your_entity.go
type YourEntityHandler = CatalogHandler[
    *your_entity.YourEntity,
    dto.CreateYourEntityRequest,
    dto.UpdateYourEntityRequest,
]

func NewYourEntityHandler(base *BaseHandler, service *your_entity.Service) *YourEntityHandler {
    return NewCatalogHandler(base, CatalogHandlerConfig[...]{
        Service:      service.CatalogService,
        EntityName:   "your_entity",
        MapCreateDTO: func(req dto.CreateYourEntityRequest) *your_entity.YourEntity { ... },
        MapUpdateDTO: func(req dto.UpdateYourEntityRequest, existing *your_entity.YourEntity) *your_entity.YourEntity { ... },
        MapToDTO:     func(entity *your_entity.YourEntity) any { return dto.FromYourEntity(entity) },
    })
}
```

### 5. Регистрация роутов
```go
// internal/infrastructure/http/v1/router.go
{
    repo := catalog_repo.NewYourEntityRepo()
    service := your_entity.NewService(repo, cfg.Numerator)
    handler := handlers.NewYourEntityHandler(baseHandler, service)
    RegisterCatalogRoutes(catalogs.Group("/your-entities"), handler, "catalog:your_entity") // ✨
}
```

---

## Новый Документ (Document)

### 1. Модель
```go
// internal/domain/documents/your_doc/model.go
type YourDocument struct {
    entity.Document
    
    CustomField string         `db:"custom_field" json:"customField"`
    Lines       []YourDocLine  `db:"-" json:"lines"` // Табличная часть
}

type YourDocLine struct {
    LineID   id.ID   `db:"line_id" json:"lineId"`
    LineNo   int     `db:"line_no" json:"lineNo"`
    ProductID id.ID  `db:"product_id" json:"productId"`
}
```

### 2. Репозиторий
```go
// internal/infrastructure/storage/postgres/document_repo/your_doc.go
func NewYourDocRepo() *YourDocRepo {
    return &YourDocRepo{
        BaseDocumentRepo: NewBaseDocumentRepo[*your_doc.YourDocument](
            "doc_your_documents",
            postgres.ExtractDBColumns[your_doc.YourDocument](), // ✨
        ),
    }
}

// Методы для работы с табличной частью
func (r *YourDocRepo) GetLines(ctx context.Context, docID id.ID) ([]your_doc.YourDocLine, error) { ... }
func (r *YourDocRepo) SaveLines(ctx context.Context, docID id.ID, lines []your_doc.YourDocLine) error { ... }
```

### 3. Сервис
```go
// internal/domain/documents/your_doc/service.go
func NewService(repo Repository, postingEngine *posting.Engine, numerator *numerator.Service) *Service {
    base := domain.NewDocumentService(domain.DocumentServiceConfig[*YourDocument]{
        Repo:          repo,
        TxManager:     nil, // TxManager берётся из context (Database-per-Tenant)
        PostingEngine: postingEngine,
        Numerator:     numerator,
        EntityName:    "your_document",
    })
    
    return &Service{DocumentService: base, repo: repo}
}
```

### 4. Handler
```go
// internal/infrastructure/http/v1/handlers/your_doc.go
type YourDocHandler struct {
    *BaseHandler
    service *your_doc.Service
}

// Реализовать методы: List, Create, Get, Update, Delete, Post, Unpost
// Опционально: Copy (если поддерживается)
```

### 5. Регистрация роутов
```go
// internal/infrastructure/http/v1/router.go
{
    repo := document_repo.NewYourDocRepo()
    service := your_doc.NewService(repo, postingEngine, cfg.Numerator)
    handler := handlers.NewYourDocHandler(baseHandler, service)
    RegisterDocumentRoutes(documents.Group("/your-docs"), handler, "document:your_doc") // ✨
    
    // Дополнительные специфичные роуты (если нужны)
    // yourDocGroup.POST("/:id/custom-action", handler.CustomAction)
}
```

---

## Multi-tenancy quick notes

- **Tenant header**: используем `X-Tenant-ID` (**tenantID UUID**, как в meta-database `tenants.id`).
- **slug**: хранится в meta-database как “label/информация”, но не используется как идентификатор в runtime.
- **DB-per-tenant**: pool/txManager кладутся в `context.Context` в middleware `TenantDB`.
  - Репозитории/сервисы получают транзакции/querier из контекста (изоляция обеспечивается выбором базы).

## Idempotency quick notes

- Для mutating операций используем `X-Idempotency-Key`.
- Один key **нельзя** переиспользовать под другой payload/operation/user — вернём **409** (Idempotency key mismatch).

---

## Типичные задачи

### Добавить custom метод в репозиторий
```go
// Используйте baseSelect() и FindOne() из BaseCatalogRepo
func (r *YourRepo) FindByCustomField(ctx context.Context, value string) (*YourEntity, error) {
    q := r.baseSelect(ctx).
        Where(squirrel.Eq{"custom_field": value}).
        Limit(1)
    return r.FindOne(ctx, q)
}
```

### Добавить custom валидацию через hooks
```go
func NewService(...) *Service {
    base := domain.NewCatalogService(...)
    svc := &Service{CatalogService: base}
    
    // Регистрируем хуки
    base.Hooks().OnBeforeCreate(svc.checkUniqueness)
    base.Hooks().OnAfterUpdate(svc.sendNotification)
    
    return svc
}
```

### Добавить специфичный роут
```go
// После RegisterCatalogRoutes можно добавить свои роуты
RegisterCatalogRoutes(group, handler, "catalog:entity")
group.GET("/:id/custom", middleware.RequirePermission("..."), handler.CustomMethod)
```

---

## Checklist при добавлении новой сущности

- [ ] Создать миграцию БД (`db/migrations/`)
- [ ] Создать модель (`internal/domain/catalogs/` или `documents/`)
- [ ] Создать репозиторий (`internal/infrastructure/storage/postgres/catalog_repo/` или `document_repo/`)
  - [ ] Использовать `ExtractDBColumns[T]()`
- [ ] Создать сервис (`internal/domain/catalogs/` или `documents/`)
- [ ] Создать DTO (`internal/infrastructure/http/v1/dto/`)
  - [ ] CreateXRequest
  - [ ] UpdateXRequest
  - [ ] XResponse
- [ ] Создать handler (`internal/infrastructure/http/v1/handlers/`)
- [ ] Зарегистрировать роуты в `router.go`
  - [ ] Использовать `RegisterCatalogRoutes()` или `RegisterDocumentRoutes()`
- [ ] Добавить permissions в seed миграцию
- [ ] Проверить компиляцию: `go build ./cmd/server`
- [ ] Протестировать API

---

## Полезные команды

```bash
# Компиляция
go build ./cmd/server

# Запуск тестов
go test ./...

# Запуск с миграциями
migrate -path db/migrations -database "postgres://..." up

# Проверка линтером
golangci-lint run

# Форматирование
go fmt ./...
```

---

## Дополнительная информация

- Полный пример: `docs/ADD_NEW_ENTITY_EXAMPLE.md`
- Архитектура проекта: `Manifest.md`
