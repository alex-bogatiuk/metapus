# Жизненный цикл HTTP-запроса в Metapus: От входящего запроса до ответа клиенту

Карта показывает полный путь HTTP-запроса через все слои приложения: middleware chain, handler, service, repository, database — и обратно.

**Ключевые точки:**
- Цепочка middleware (1a–1g)
- Маршрутизация и Permission (2a–2c)
- Handler: DTO binding и маппинг (3a–3e)
- Service: валидация, хуки, транзакция (4a–4f)
- Repository: SQL-запрос через TxManager (5a–5d)
- Error flow: от handler до JSON-ответа (6a–6d)
- Idempotency: защита от дублей (7a–7d)

---

## Trace 1: Middleware Chain — Глобальная обработка каждого запроса

Каждый HTTP-запрос проходит через цепочку middleware в строго определённом порядке. Порядок критичен: Recovery оборачивает всё, Trace добавляет ID, Logger замеряет время, ErrorHandler формирует JSON-ответ из ошибок.

```
HTTP Request входит в Gin Engine
│
├── 1. Recovery() <-- 1a
│   └── defer recover() — ловит panic
│       └── apperror.NewInternal() → c.Error() → c.Abort()
│
├── 2. Trace() <-- 1b
│   ├── X-Request-ID из заголовка (или uuid.New())
│   ├── X-Trace-ID из заголовка (или uuid.New())
│   ├── appctx.WithTrace(ctx, trace) — в context
│   ├── c.Set("request_id", ...) — в gin context
│   └── c.Header() — в response headers
│
├── 3. Logger(log) <-- 1c
│   ├── start := time.Now()
│   ├── c.Next() — ожидание завершения
│   └── log.Infow("http request", method, path, status, latency_ms, ...)
│
├── 4. ErrorHandler() <-- 1d
│   ├── c.Next() — ожидание завершения
│   └── if len(c.Errors) > 0
│       ├── apperror.AsAppError(err) → JSON{code, message, details} <-- 1e
│       └── else → JSON{500, "Internal server error"} <-- 1f
│
├── [Группа /health — без авторизации] <-- 1g
│
└── [Группа /api/v1/protected] — дополнительные middleware:
    ├── 5. TenantDB(manager) <-- [Trace 2 в MultiTenantВMetapus.MD]
    ├── 6. Auth(jwtValidator) <-- [Trace 3 в AuthВMetapus.MD]
    └── 7. UserContext() <-- 1h
        ├── c.Get("user_id") — из Auth middleware
        └── security.WithUserID(ctx, uid) — в request context
```

| ID | Описание | Файл |
|----|----------|------|
| **1a** | Recovery middleware. Оборачивает весь запрос в `defer recover()`. При panic логирует stack trace и возвращает 500 через `apperror.NewInternal` | `middleware/recovery.go:16` |
| **1b** | Trace middleware. Генерирует или извлекает `X-Request-ID` и `X-Trace-ID`. Добавляет в context (`appctx.WithTrace`), в gin context (`c.Set`) и в response headers | `middleware/trace.go:17` |
| **1c** | Logger middleware. Замеряет `time.Since(start)` и логирует method, path, status, latency_ms, client_ip после завершения запроса | `middleware/logger.go:12` |
| **1d** | ErrorHandler middleware. Единственная точка формирования JSON-ответов из ошибок. Работает ПОСЛЕ `c.Next()` — обрабатывает все ошибки, накопленные в `c.Errors` | `middleware/error.go:13` |
| **1e** | Обработка AppError. Извлекает `code`, `message`, `details` из структурированной ошибки. Логирует внутреннюю причину, но не отдаёт клиенту | `middleware/error.go:31` |
| **1f** | Обработка неизвестных ошибок. Логирует полную ошибку, клиенту возвращает generic "Internal server error" с `request_id` | `middleware/error.go:60` |
| **1g** | Health endpoints. Группа `/health` не проходит через TenantDB/Auth — доступна без авторизации для Kubernetes probes | `router.go:80` |
| **1h** | UserContext middleware. Берёт `user_id` из gin context (установленный Auth middleware) и добавляет в request context через `security.WithUserID` для доступа из domain layer | `middleware/user_context.go:19` |

---

## Trace 2: Маршрутизация — От URL до конкретного Handler

Маршруты регистрируются через helper-функции `RegisterCatalogRoutes` / `RegisterDocumentRoutes`, которые автоматически подключают permission middleware к каждому endpoint.

```
Router: Регистрация маршрутов
│
├── registerCatalogRoutes(protected, cfg) <-- router.go:108
│   └── catalogs := rg.Group("/catalog") <-- 2a
│       └── для каждого справочника:
│           ├── repo := catalog_repo.NewXxxRepo() <-- router.go:159
│           ├── service := xxx.NewService(repo, numerator) <-- router.go:160
│           ├── handler := handlers.NewXxxHandler(base, service) <-- router.go:161
│           └── RegisterCatalogRoutes(group, handler, "catalog:xxx") <-- 2b
│               ├── GET  ""      + RequirePermission("catalog:xxx:read")   → List
│               ├── POST ""      + RequirePermission("catalog:xxx:create") → Create
│               ├── GET  "/:id"  + RequirePermission("catalog:xxx:read")   → Get
│               ├── PUT  "/:id"  + RequirePermission("catalog:xxx:update") → Update
│               └── DELETE "/:id"+ RequirePermission("catalog:xxx:delete") → Delete
│
├── registerDocumentRoutes(protected, cfg) <-- router.go:109
│   └── docsGroup := rg.Group("/document") <-- 2c
│       └── для каждого документа:
│           ├── repo, service, postingEngine, handler ...
│           ├── service.Hooks().OnBeforeCreate(audit.EnrichCreatedBy) <-- router.go:244
│           └── RegisterDocumentRoutes(group, handler, "document:xxx")
│               ├── ... стандартные CRUD маршруты ...
│               ├── POST "/:id/post"   + RequirePermission("...:post")   → Post
│               ├── POST "/:id/unpost" + RequirePermission("...:unpost") → Unpost
│               └── POST "/:id/copy"   (если handler реализует DocumentCopyHandler)
│
└── RequirePermission(permission) <-- 2d
    ├── user := appctx.GetUser(ctx)
    ├── if user.IsAdmin → пропускаем (admin имеет все права)
    └── проверяем permission через O(1) set lookup (permissions_set)
        ├── fallback: линейный поиск по []string из JWT claims
        └── если нет → apperror.NewForbidden("insufficient permissions")
```

| ID | Описание | Файл |
|----|----------|------|
| **2a** | Группировка маршрутов справочников. Все справочники регистрируются под `/api/v1/catalog/{entity}` | `router.go:152` |
| **2b** | Helper для регистрации стандартных CRUD-маршрутов справочника. Автоматически подключает `RequirePermission` к каждому endpoint с правильным суффиксом (`:read`, `:create`, `:update`, `:delete`) | `route_helpers.go:48` |
| **2c** | Группировка маршрутов документов. Все документы под `/api/v1/document/{entity}`. Дополнительно создаются shared-зависимости: `stockRepo`, `stockService`, `postingEngine` | `router.go:223` |
| **2d** | Permission middleware. Проверяет наличие конкретного разрешения в JWT claims пользователя. Админы пропускаются автоматически | `middleware/permission.go:13` |

---

## Trace 3: Catalog Handler — Create (типичный CRUD-запрос)

Полный путь создания элемента справочника: от JSON-тела запроса до записи в PostgreSQL и ответа клиенту.

```
POST /api/v1/catalog/counterparties
│
├── CatalogHandler.Create(c *gin.Context) <-- catalog.go:132
│   │
│   ├── ctx := c.Request.Context() <-- 3a
│   │   └── содержит: Tenant, TxManager, User, Trace
│   │
│   ├── BindJSON(c, &req) <-- 3b
│   │   └── c.ShouldBindJSON(&CreateCounterpartyDTO)
│   │       └── ошибка → apperror.NewValidation("invalid request body")
│   │
│   ├── entity := mapCreateDTO(req) <-- 3c
│   │   └── преобразование DTO → domain entity
│   │       └── NewCounterparty(name, inn, ...)
│   │
│   ├── service.Create(ctx, entity) <-- 3d
│   │   └── [переход в Trace 4: Service Layer]
│   │
│   ├── CompleteIdempotency(c, 201, ..., mapToDTO(entity)) <-- 3e
│   │   └── сохраняет ответ в sys_idempotency для повторных запросов
│   │
│   └── c.JSON(201, mapToDTO(entity)) <-- 3f
│       └── преобразование domain entity → response DTO
│           └── JSON-ответ клиенту
```

| ID | Описание | Файл |
|----|----------|------|
| **3a** | Извлечение context. Context уже обогащён middleware: содержит `Tenant`, `TxManager`, `UserContext`, `TraceContext` | `catalog.go:133` |
| **3b** | Binding JSON в DTO. Использует `c.ShouldBindJSON` (Gin). При ошибке парсинга возвращает 400 через `apperror.NewValidation` | `base.go:24` |
| **3c** | Маппинг DTO → domain entity. Каждый handler предоставляет свою функцию `mapCreateDTO`, которая создаёт domain-объект из DTO | `catalog.go:142` |
| **3d** | Вызов сервиса. Handler делегирует бизнес-логику сервису, передавая обогащённый context | `catalog.go:144` |
| **3e** | Завершение идемпотентности. Если запрос содержал `X-Idempotency-Key`, ответ кэшируется для повторного воспроизведения | `base.go:82` |
| **3f** | Формирование JSON-ответа. Domain entity преобразуется в response DTO через `mapToDTO` и отправляется клиенту | `catalog.go:152` |

---

## Trace 4: Service Layer — Валидация, хуки, транзакция

Сервис выполняет бизнес-логику: валидация инвариантов, выполнение хуков (автонумерация, проверка уникальности), запись в транзакции.

```
CatalogService[T].Create(ctx, entity) <-- service.go:88
│
├── 1. Validate entity invariants <-- 4a
│   └── entity.Validate(ctx)
│       └── ошибка → normalizeValidationErr() → apperror.NewValidation(...)
│
├── 2. Run before-create hooks <-- 4b
│   └── hooks.RunBeforeCreate(ctx, entity)
│       ├── prepareForCreate: numerator.GetNextNumber() → cp.Code <-- 4c
│       └── checkINNExists: repo.GetByINN() → apperror.NewConflict(...)
│
├── 3. Get TxManager <-- 4d
│   └── getTxManager(ctx)
│       └── tenant.GetTxManager(ctx) — из context (Database-per-Tenant)
│
├── 4. RunInTransaction <-- 4e
│   └── txm.RunInTransaction(ctx, func(ctx) {
│       └── repo.Create(ctx, entity) <-- [переход в Trace 5]
│   })
│
└── 5. Run after-create hooks (вне транзакции) <-- 4f
    └── hooks.RunAfterCreate(ctx, entity)
        └── ошибки логируются, но не прерывают операцию
```

| ID | Описание | Файл |
|----|----------|------|
| **4a** | Валидация инвариантов. Каждая сущность реализует `Validate(ctx)` — проверка обязательных полей, диапазонов, бизнес-правил | `service.go:90` |
| **4b** | Before-create хуки. Выполняются до транзакции. Типичные задачи: автонумерация, проверка уникальности, обогащение полей | `service.go:95` |
| **4c** | Автонумерация. Hook вызывает `numerator.GetNextNumber()` для генерации уникального кода (например `CP-000001`) | `counterparty/service.go:50` |
| **4d** | Получение TxManager. В Database-per-Tenant режиме TxManager извлекается из context (инжектирован middleware.TenantDB) | `service.go:100` |
| **4e** | Выполнение в транзакции. `RunInTransaction` создаёт PostgreSQL транзакцию, выполняет `repo.Create`, и коммитит при успехе | `service.go:104` |
| **4f** | After-create хуки. Выполняются ВНЕ транзакции. Ошибки не прерывают операцию — сущность уже создана. Используются для отправки событий, уведомлений | `service.go:115` |

---

## Trace 5: Repository Layer — SQL-запрос через TxManager

Репозиторий формирует SQL-запрос, получает querier (транзакция или пул) из TxManager, и выполняет запрос.

```
BaseCatalogRepo[T].Create(ctx, entity) <-- catalog_repo/base.go:80
│
├── data := postgres.StructToMap(entity) <-- 5a
│   └── reflect: field.Tag.Get("db") → map[string]any
│
├── Фильтрация по selectCols <-- 5b
│   └── только колонки из selectCols попадают в INSERT
│
├── squirrel.Insert(tableName).SetMap(filteredData) <-- 5c
│   └── sql, args := q.ToSql()
│       └── INSERT INTO cat_counterparties (id, name, code, ...) VALUES ($1, $2, ...)
│
└── querier := getTxManager(ctx).GetQuerier(ctx) <-- 5d
    ├── если есть активная транзакция → использует pgx.Tx
    └── если нет → использует pgxpool.Pool
        └── querier.Exec(ctx, sql, args...)
```

| ID | Описание | Файл |
|----|----------|------|
| **5a** | Маппинг struct → map. Через рефлексию читает `db` теги и формирует `map[string]any` для SQL-билдера | `catalog_repo/base.go:81` |
| **5b** | Фильтрация колонок. Только колонки из `selectCols` попадают в INSERT — защита от случайных полей (например `db:"-"`) | `catalog_repo/base.go:87` |
| **5c** | Построение SQL. Используется `squirrel` query builder с `$N` placeholder-ами для PostgreSQL | `catalog_repo/base.go:94` |
| **5d** | Получение querier. `GetQuerier(ctx)` возвращает `pgx.Tx` если внутри транзакции, или `pgxpool.Pool` если нет. Единый интерфейс для обоих случаев | `catalog_repo/base.go:103` |

---

## Trace 6: Error Flow — От ошибки до JSON-ответа

Все ошибки в Metapus проходят через единую точку — `middleware.ErrorHandler`. Handler никогда не формирует JSON-ответ для ошибок напрямую.

```
Поток ошибок в Metapus
│
├── Возникновение ошибки (любой слой)
│   ├── Handler: apperror.NewValidation("invalid id") <-- 6a
│   ├── Service: apperror.NewNotFound("counterparty", id) <-- service.go:78
│   ├── Repo: apperror.NewConcurrentModification(...) <-- catalog_repo/base.go:162
│   └── Panic: recover() → apperror.NewInternal(err) <-- recovery.go:27
│
├── Регистрация ошибки в Gin
│   └── handler.HandleError(c, err) <-- 6b
│       ├── c.Error(err) — добавляет в c.Errors
│       └── c.Abort() — прекращает цепочку middleware
│
├── ErrorHandler middleware обрабатывает (после c.Next()) <-- 6c
│   ├── err := c.Errors.Last().Err
│   ├── apperror.AsAppError(err) — извлечение AppError
│   │   ├── appErr.HTTPStatus → HTTP статус код
│   │   ├── appErr.Code → бизнес-код ошибки
│   │   ├── appErr.Message → сообщение для клиента
│   │   └── appErr.Details → доп. информация
│   └── c.JSON(status, {code, message, details})
│
└── JSON-ответ клиенту <-- 6d
    {
      "code": "NOT_FOUND",
      "message": "counterparty not found",
      "details": {"id": "550e8400-..."}
    }
```

| ID | Описание | Файл |
|----|----------|------|
| **6a** | Создание структурированной ошибки. Все слои используют `apperror.NewXxx()` для создания типизированных ошибок с HTTP-статусом, кодом и деталями | `apperror/` |
| **6b** | Регистрация ошибки. Handler вызывает `c.Error(err)` + `c.Abort()`. JSON-ответ **не формируется** в handler — это задача ErrorHandler | `base.go:48` |
| **6c** | Единая обработка ошибок. ErrorHandler middleware — единственная точка формирования JSON-ответов для ошибок. Внутренние ошибки логируются, но не отдаются клиенту | `middleware/error.go:13` |
| **6d** | Консистентный формат ответа. Все ошибки возвращаются в формате `{code, message, details}` независимо от слоя, где они возникли | `middleware/error.go:40` |

---

## Trace 7: Idempotency — Защита от дублирующих запросов

Для мутирующих операций (POST/PUT/PATCH) поддерживается идемпотентность через заголовок `X-Idempotency-Key`.

```
Idempotency Flow
│
├── Middleware: Idempotency(store) <-- 7a
│   ├── Проверка метода: только POST/PUT/PATCH
│   ├── key := c.GetHeader("X-Idempotency-Key")
│   │   └── если пуст → пропускаем (обычный запрос)
│   ├── Хеширование тела запроса (SHA-256) <-- 7b
│   │   └── requestHash := hex.EncodeToString(sha256(body))
│   ├── store.AcquireKey(ctx, key, userID, operation, hash)
│   │   ├── Ключ новый → INSERT в sys_idempotency, продолжаем
│   │   ├── Ключ существует, status=Success → replay cached response <-- 7c
│   │   └── Ключ существует, hash не совпадает → error (разные тела)
│   └── c.Set("idempotency_key", key) — для handler
│
├── Handler выполняет бизнес-логику
│   └── ...
│
├── Handler завершает идемпотентность <-- 7d
│   └── CompleteIdempotency(c, 201, "application/json", response)
│       └── store.CompleteKey(ctx, key, status, contentType, body)
│           └── UPDATE sys_idempotency SET status='Success', response_body=...
│
└── При повторном запросе с тем же ключом:
    └── Middleware возвращает кэшированный ответ без вызова handler
```

| ID | Описание | Файл |
|----|----------|------|
| **7a** | Idempotency middleware. Активируется только для мутирующих HTTP-методов при наличии заголовка `X-Idempotency-Key` | `middleware/idempotency.go:22` |
| **7b** | Хеширование тела запроса. SHA-256 хеш тела запроса используется для обнаружения попыток отправить разное тело с одним и тем же ключом | `middleware/idempotency.go:46` |
| **7c** | Replay кэшированного ответа. Если ключ уже обработан (`status=Success`), middleware возвращает сохранённый ответ без вызова handler | `middleware/idempotency.go:81` |
| **7d** | Завершение идемпотентности. Handler вызывает `CompleteIdempotency` после успешной операции — ответ сохраняется в `sys_idempotency` | `base.go:82` |

---

## Trace 8: Document Handler — Create с PostImmediately

Создание документа с флагом `PostImmediately` — записывает документ и сразу проводит его в одной операции.

```
POST /api/v1/document/goods-receipt
Body: { ..., "postImmediately": true }
│
├── BaseDocumentHandler.Create(c) <-- document.go:95
│   ├── BindJSON(c, &req) — парсинг CreateGoodsReceiptDTO
│   ├── doc := mapCreateDTO(req) — создание domain entity
│   │
│   ├── isPostImmediately(req) == true? <-- 8a
│   │   ├── ДА → service.PostAndSave(ctx, doc) <-- 8b
│   │   │   └── В одной транзакции:
│   │   │       ├── repo.Create(doc) — запись шапки
│   │   │       ├── repo.CreateLines(doc.Lines) — запись строк
│   │   │       ├── postingEngine.Post(doc) — проведение <-- [PostingВMetapus.MD]
│   │   │       └── repo.Update(doc) — обновление Posted=true
│   │   │
│   │   └── НЕТ → service.Create(ctx, doc) <-- 8c
│   │       └── Только запись документа (черновик)
│   │
│   ├── response := mapToDTO(doc)
│   ├── CompleteIdempotency(c, 201, ..., response)
│   └── c.JSON(201, response)
```

| ID | Описание | Файл |
|----|----------|------|
| **8a** | Проверка флага PostImmediately. Функция `isPostImmediately(dto)` проверяет, нужно ли сразу провести документ после создания | `document.go:127` |
| **8b** | PostAndSave — создание + проведение. Документ записывается и проводится в одной атомарной операции. При ошибке проведения документ не создаётся | `document.go:128` |
| **8c** | Создание черновика. Документ только записывается без проведения. Проведение можно выполнить позже через `POST /:id/post` | `document.go:133` |

---

## Trace 9: Полный путь запроса — Сводная диаграмма

```
┌─────────────────────────────────────────────────────────────────────────┐
│                    ПОЛНЫЙ ПУТЬ HTTP-ЗАПРОСА В METAPUS                     │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  HTTP Request                                                            │
│  POST /api/v1/catalog/counterparties                                    │
│  Headers: X-Tenant-ID, Authorization: Bearer ..., X-Idempotency-Key    │
│       │                                                                  │
│       ▼                                                                  │
│  ┌──────────────────── MIDDLEWARE CHAIN ────────────────────────┐       │
│  │ 1. Recovery   → defer recover()                               │       │
│  │ 2. Trace      → request_id, trace_id → context               │       │
│  │ 3. Logger     → start timer                                   │       │
│  │ 4. ErrorHandler → c.Next() (ждёт завершения)                 │       │
│  │ 5. TenantDB   → X-Tenant-ID → Pool → TxManager → context    │       │
│  │ 6. Auth       → Bearer token → UserContext → context          │       │
│  │ 7. UserContext → user_id → security context                   │       │
│  │ 8. Idempotency → X-Idempotency-Key → check/acquire           │       │
│  │ 9. Permission  → "catalog:counterparty:create" check          │       │
│  └───────────────────────────┬──────────────────────────────────┘       │
│                              │                                           │
│                              ▼                                           │
│  ┌──────────── HANDLER ──────────────────────────────────────┐         │
│  │ CatalogHandler.Create(c)                                    │         │
│  │ ├── BindJSON → CreateCounterpartyDTO                        │         │
│  │ ├── mapCreateDTO → Counterparty (domain entity)             │         │
│  │ └── service.Create(ctx, entity)                             │         │
│  └───────────────────────────┬──────────────────────────────────┘       │
│                              │                                           │
│                              ▼                                           │
│  ┌──────────── SERVICE ──────────────────────────────────────┐         │
│  │ CatalogService.Create(ctx, entity)                          │         │
│  │ ├── entity.Validate(ctx)                                    │         │
│  │ ├── hooks.RunBeforeCreate (код, уникальность)               │         │
│  │ ├── txm.RunInTransaction {                                  │         │
│  │ │   └── repo.Create(ctx, entity)                            │         │
│  │ │       └── StructToMap → squirrel.Insert → querier.Exec    │         │
│  │ │           └── INSERT INTO cat_counterparties VALUES (...)  │         │
│  │ │   }                                                        │         │
│  │ └── hooks.RunAfterCreate (события, уведомления)             │         │
│  └───────────────────────────┬──────────────────────────────────┘       │
│                              │                                           │
│                              ▼                                           │
│  ┌──────────── RESPONSE ─────────────────────────────────────┐         │
│  │ ← service.Create returns nil                                │         │
│  │ ← handler: CompleteIdempotency + c.JSON(201, responseDTO)   │         │
│  │ ← ErrorHandler: len(c.Errors) == 0, skip                   │         │
│  │ ← Logger: log(method, path, 201, latency_ms)               │         │
│  │ ← Response headers: X-Request-ID, X-Trace-ID               │         │
│  └──────────────────────────────────────────────────────────────┘       │
│                                                                          │
│  HTTP Response: 201 Created                                              │
│  { "id": "...", "code": "CP-000001", "name": "ACME Corp", ... }        │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```
