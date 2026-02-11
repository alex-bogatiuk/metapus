# Infrastructure Layer — Реализация

> Адаптеры и драйверы: HTTP (Gin), Postgres (pgx), кэш, фоновые задачи. Расположен в `internal/infrastructure/`.

---

## HTTP слой (`internal/infrastructure/http/v1/`)

### Router — Composition Root для маршрутов

`router.go` — связка всех компонентов: repo → service → hooks → handler → routes.

Для каждой сущности:
1. Создание repo: `catalog_repo.NewCounterpartyRepo()`
2. Создание service: `counterparty.NewService(repo, numerator)`
3. Создание handler: `handlers.NewCounterpartyHandler(base, service)`
4. Регистрация маршрутов: `RegisterCatalogRoutes(group, handler, "catalog:counterparty")`

Документы дополнительно получают shared `postingEngine` и audit hooks.

### Handlers — тонкие адаптеры

Handler = адаптер между HTTP и domain:
- Парсит/валидирует вход (DTO binding)
- Маппит DTO ↔ domain через инжектированные mapper-функции
- Вызывает доменный сервис
- **Не формирует JSON для ошибок** — это задача ErrorHandler middleware

Используются **generic handlers**: `CatalogHandler[T, CreateDTO, UpdateDTO]` — один код для всех справочников.

### DTOs — Request/Response

```
dto/
├── catalog.go     # CreateXxxRequest, UpdateXxxRequest, XxxResponse
└── document.go    # CreateDocRequest, UpdateDocRequest, DocResponse
```

Каждый DTO содержит:
- `ToEntity()` — конвертация request → domain entity
- `ApplyTo(existing)` — применение update к существующей сущности
- `FromEntity()` — конвертация domain → response DTO

### Middleware chain

Порядок middleware **критичен**:

| # | Middleware | Задача |
|---|-----------|--------|
| 1 | `Recovery` | `defer recover()` — ловит panic |
| 2 | `Trace` | X-Request-ID, X-Trace-ID → context |
| 3 | `Logger` | Замер latency, структурированный лог |
| 4 | `ErrorHandler` | Единая точка формирования JSON из ошибок |
| 5 | `TenantDB` | X-Tenant-ID → Pool → TxManager → context |
| 6 | `Auth` | Bearer JWT → UserContext → context + tenant match |
| 7 | `UserContext` | user_id → security context |
| 8 | `Idempotency` | X-Idempotency-Key → check/acquire |
| 9 | `Permission` | Проверка конкретного разрешения |

Middleware 5–9 применяются только к protected-группе `/api/v1/`. Health endpoints (`/health`) доступны без авторизации.

Подробнее: [13-request-lifecycle.md](13-request-lifecycle.md).

---

## Postgres слой (`internal/infrastructure/storage/postgres/`)

### TxManager — управление транзакциями

- Реализует `tx.Manager` интерфейс из core
- Создаётся **для каждого запроса** в middleware `TenantDB`
- Поддерживает вложенные транзакции (reuse или savepoint)
- `GetQuerier(ctx)` — возвращает `pgx.Tx` внутри транзакции или `pgxpool.Pool` вне её

Подробнее: [11-transactions.md](11-transactions.md).

### Репозитории — generic + entity-specific

`BaseCatalogRepo[T]` — generic реализация CRUD через reflection + squirrel:
- `StructToMap(entity)` — через `db` теги
- Фильтрация по `selectCols` — только известные колонки в INSERT
- **Optimistic locking** через `WHERE version = $N` + `RETURNING version`
- **Soft delete** — `deletion_mark = true`

Конкретный репозиторий:
```go
type CounterpartyRepo struct {
    *BaseCatalogRepo[*counterparty.Counterparty]
}

func NewCounterpartyRepo() *CounterpartyRepo {
    return &CounterpartyRepo{
        BaseCatalogRepo: NewBaseCatalogRepo[*counterparty.Counterparty](
            "cat_counterparties",
            postgres.ExtractDBColumns[counterparty.Counterparty](),
        ),
    }
}
```

**Ключевое правило:** TxManager **НЕ хранится** в struct репозитория. Получается из context:

```go
func (r *Repo) getTxManager(ctx context.Context) *postgres.TxManager {
    return tenant.MustGetTxManager(ctx)
}
```

### SQL-паттерны

- Используй `pgx/pgxpool` и **явные** транзакции
- Предпочитай простые запросы
- Следи за индексами под фильтры/сортировки
- Избегай N+1 на горячих путях
- Для bulk операций: батчи/CopyFrom
- **Никаких tenant discriminator колонок в SQL**

### Outbox Publisher

Transactional Outbox для гарантированной доставки событий:

```go
func (p *OutboxPublisher) Publish(ctx context.Context, event DomainEvent) error {
    conn := tenant.MustGetTxManager(ctx).GetQuerier(ctx)
    _, err = conn.Exec(ctx, `INSERT INTO sys_outbox ...`, ...)
    return err
}
```

Событие записывается **в той же транзакции**, что и бизнес-данные. Worker (`outbox_relay.go`) обрабатывает потом.

### Idempotency Store

`sys_idempotency` — хранение обработанных idempotency keys:
- `AcquireKey` — INSERT или replay cached response
- `CompleteKey` — UPDATE status=Success + response_body
- SHA-256 хеш тела запроса для обнаружения разных тел с одним ключом

---

## Composition Root (`cmd/*`)

В `cmd/*/main.go`:
1. Собрать зависимости (DI руками)
2. Настроить логирование/конфиг
3. Поднять HTTP/router/worker
4. Сделать **graceful shutdown** (context cancellation, закрытие пулов)

```go
// cmd/server/main.go (упрощённо)
func main() {
    cfg := loadConfig()
    metaPool := connectMetaDB(cfg)
    tenantManager := tenant.NewManager(metaPool, cfg)
    
    router := http.NewRouter(tenantManager, cfg)
    srv := &http.Server{Handler: router}
    
    // Graceful shutdown
    go srv.ListenAndServe()
    waitForSignal()
    srv.Shutdown(ctx)
    tenantManager.Close()
}
```

---

## Worker — фоновые задачи

`BaseWorker` — итерирует по всем активным тенантам:

```
worker/
├── base.go               # BaseWorker (итерация по тенантам)
├── outbox_relay.go       # Outbox → Kafka/NATS
├── audit_cleaner.go      # Удаление старых партиций
└── handlers/
    └── month_closing.go  # Закрытие периода
```

Каждый worker получает pool тенанта через `Manager.GetPool()` и выполняет задачу изолированно.

---

## Observability

- **Логи** — структурированные (ключ-значение): tenant, request_id, trace_id, user_id
- **Метрики/healthchecks** — добавляются по потребности
- **Tracing** — OpenTelemetry spans для транзакций и ключевых операций

---

## Связанные документы

- [13-request-lifecycle.md](13-request-lifecycle.md) — полный путь HTTP-запроса
- [11-transactions.md](11-transactions.md) — TxManager в деталях
- [09-crud-pipeline.md](09-crud-pipeline.md) — generic handler/service/repo pipeline
- [07-multi-tenancy.md](07-multi-tenancy.md) — как TenantDB middleware работает
