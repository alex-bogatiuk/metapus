# Жизненный цикл HTTP-запроса

> Полный путь HTTP-запроса через все слои приложения: middleware chain → handler → service → repository → database — и обратно.

---

## Middleware Chain — глобальная обработка

Каждый запрос проходит middleware в **строго определённом порядке**:

| # | Middleware | Задача |
|---|-----------|--------|
| 1 | **Recovery** | `defer recover()` — ловит panic → `apperror.NewInternal()` → 500 |
| 2 | **Trace** | X-Request-ID / X-Trace-ID → context + response headers |
| 3 | **Logger** | `time.Since(start)` → method, path, status, latency_ms |
| 4 | **ErrorHandler** | Единая точка JSON-ответов из ошибок (после `c.Next()`) |
| 5 | **TenantDB** | X-Tenant-ID → Pool → TxManager → context |
| 6 | **Auth** | Bearer JWT → UserContext → context + tenant match check |
| 7 | **UserContext** | user_id → security context |
| 8 | **Idempotency** | X-Idempotency-Key → check/acquire (POST/PUT/PATCH) |
| 9 | **Permission** | Проверка конкретного разрешения из JWT claims |

- Middleware 1–4 — **глобальные** (все запросы)
- Middleware 5–9 — **protected группа** (`/api/v1/`)
- `/health` endpoints — **без авторизации** (для Kubernetes probes)

---

## Маршрутизация — от URL до Handler

Маршруты регистрируются через helper-функции:

```go
// Справочники: /api/v1/catalog/{entity}
RegisterCatalogRoutes(group, handler, "catalog:counterparty")
// → GET, POST, GET/:id, PUT/:id, DELETE/:id + RequirePermission

// Документы: /api/v1/document/{entity}
RegisterDocumentRoutes(group, handler, "document:goods_receipt")
// → стандартные CRUD + POST/:id/post, POST/:id/unpost, POST/:id/copy
```

---

## Пример: Create Counterparty (полный путь)

### 1. Handler

```
POST /api/v1/catalog/counterparties
│
├── ctx := c.Request.Context()
│   └── содержит: Tenant, TxManager, User, Trace
├── BindJSON(c, &CreateCounterpartyDTO)
│   └── ошибка → apperror.NewValidation("invalid request body")
├── entity := mapCreateDTO(req) → *Counterparty
├── service.Create(ctx, entity) → [Service Layer]
├── CompleteIdempotency(c, 201, ..., mapToDTO(entity))
└── c.JSON(201, responseDTO)
```

### 2. Service

```
CatalogService.Create(ctx, entity)
│
├── entity.Validate(ctx) — бизнес-инварианты
├── hooks.RunBeforeCreate(ctx, entity)
│   ├── prepareForCreate: numerator → cp.Code = "CP-2024-001"
│   └── checkINNExists → apperror.NewConflict(...)
├── txm := getTxManager(ctx) — из context (DB-per-Tenant)
├── txm.RunInTransaction(ctx, func(ctx) {
│       repo.Create(ctx, entity)
│   })
└── hooks.RunAfterCreate(ctx, entity) — ошибки логируются, не прерывают
```

### 3. Repository

```
BaseCatalogRepo.Create(ctx, entity)
│
├── data := StructToMap(entity) — reflect: db tags → map[string]any
├── filter by selectCols — только известные колонки
├── squirrel.Insert("cat_counterparties").SetMap(filteredData)
└── querier := getTxManager(ctx).GetQuerier(ctx)
    ├── внутри tx → pgx.Tx
    └── вне tx → pgxpool.Pool
        └── querier.Exec(ctx, sql, args...)
```

### 4. Response

```
← service.Create returns nil
← handler: CompleteIdempotency + c.JSON(201, responseDTO)
← ErrorHandler: len(c.Errors) == 0, skip
← Logger: log(method, path, 201, latency_ms)
← Response headers: X-Request-ID, X-Trace-ID

HTTP Response: 201 Created
{ "id": "...", "code": "CP-000001", "name": "ACME Corp", ... }
```

---

## Error Flow — от ошибки до JSON-ответа

Все ошибки проходят через **единую точку** — `middleware.ErrorHandler`:

```
Возникновение ошибки (любой слой)
├── Handler: apperror.NewValidation("invalid id")
├── Service: apperror.NewNotFound("counterparty", id)
├── Repo: apperror.NewConcurrentModification(...)
└── Panic: recover() → apperror.NewInternal(err)
         │
         ▼
handler.HandleError(c, err)
├── c.Error(err)  — добавляет в c.Errors
└── c.Abort()     — прекращает цепочку
         │
         ▼
ErrorHandler middleware (после c.Next())
├── apperror.AsAppError(err) → JSON{code, message, details}
└── else → JSON{500, "Internal server error"}
         │
         ▼
HTTP Response: { "code": "NOT_FOUND", "message": "...", "details": {...} }
```

**Handler никогда не формирует JSON для ошибок** — только `c.Error(err) + c.Abort()`.

---

## Idempotency — защита от дублей

Для мутирующих операций (POST/PUT/PATCH) через заголовок `X-Idempotency-Key`:

```
Request с X-Idempotency-Key
│
├── Middleware: Idempotency(store)
│   ├── requestHash := SHA256(body)
│   ├── store.AcquireKey(ctx, key, userID, operation, hash)
│   │   ├── Ключ новый → INSERT в sys_idempotency, продолжаем
│   │   ├── Ключ есть, status=Success → replay cached response
│   │   └── Ключ есть, hash другой → error (разные тела)
│   └── c.Set("idempotency_key", key)
│
├── Handler выполняет бизнес-логику
│
└── Handler: CompleteIdempotency(c, 201, "application/json", response)
    └── store.CompleteKey → UPDATE sys_idempotency SET status='Success'
```

При **повторном запросе** с тем же ключом — middleware возвращает кэшированный ответ **без вызова handler**.

---

## Document Create с PostImmediately

```
POST /api/v1/document/goods-receipt
Body: { ..., "postImmediately": true }
│
├── isPostImmediately(req) == true?
│   ├── ДА → service.PostAndSave(ctx, doc)
│   │   └── В одной транзакции:
│   │       ├── repo.Create(doc) — запись шапки
│   │       ├── repo.CreateLines(doc.Lines) — запись строк
│   │       ├── postingEngine.Post(doc) — проведение
│   │       └── repo.Update(doc) — обновление Posted=true
│   │
│   └── НЕТ → service.Create(ctx, doc)
│       └── Только запись документа (черновик)
```

---

## Связанные документы

- [06-infrastructure-layer.md](06-infrastructure-layer.md) — middleware, handlers, repos
- [09-crud-pipeline.md](09-crud-pipeline.md) — generic CRUD pipeline
- [07-multi-tenancy.md](07-multi-tenancy.md) — TenantDB middleware
- [08-auth-and-security.md](08-auth-and-security.md) — Auth middleware
- [10-posting-engine.md](10-posting-engine.md) — PostAndSave flow
