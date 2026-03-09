# Правила разработки

> Кодовый стиль, правила слоёв, миграции, CDC, тестируемость, concurrency, goroutine leak prevention.

---

## Общие принципы

1. **Расширяемость** — поведение задаётся конфигурацией/метаданными и точками расширения (хуки, интерфейсы)
2. **Читаемость** — Clean Architecture + уместный DDD
3. **Производительность** — нативные драйверы, пулы, целочисленная арифметика

---

## Core Layer (`internal/core`)

- **Нет зависимостей** от domain/infrastructure
- Базовые типы: entity, apperror, id, tenant, tx, security, types
- `Validate(ctx)` — только внутренняя согласованность, **НЕ ходит** в БД/сеть
- Все ошибки — через `apperror.AppError` (код, сообщение, детали)
- UUIDv7 для всех идентификаторов

---

## Domain Layer (`internal/domain`)

- **Зависит только от core**, не знает о HTTP/Postgres
- Bounded context = подкаталог: `catalogs/*`, `documents/*`, `registers/*`
- Доменные типы самодостаточны: инварианты в `Validate(ctx)`, бизнес-методы на структурах
- Сервис оркестрирует use case: Validate → hooks → tx{repo} → after hooks
- `TxManager` берётся из `context.Context` (Database-per-Tenant)
- Вычисления (`Calculate`, `GenerateMovements`) должны быть **детерминированы**

---

## Infrastructure Layer (`internal/infrastructure`)

- Handlers — тонкие адаптеры: bind → map → service.Call → respond
- **Handler НЕ формирует** JSON для ошибок — это делает ErrorHandler middleware
- Репозитории используют `pgx/pgxpool` с **явными** транзакциями
- `TxManager` **НЕ хранится** в struct репозитория — получается из context
- SQL без tenant discriminator — изоляция через Database-per-Tenant
- Никаких ORM — только `pgx` + `squirrel`

---

## Composition Root (`cmd/*`)

- DI руками (без фреймворков)
- Конфигурация через переменные окружения
- Graceful shutdown: context cancellation → закрытие пулов
- Логирование: structured (ключ-значение), tracing: OpenTelemetry

---

## Миграции БД

### Стратегия раннего этапа
На этапе разработки БД пересоздаётся с нуля. Поэтому:
- **НЕ создавай** новые миграции для изменения существующих объектов
- **Редактируй** оригинальный файл миграции, создавший таблицу
- Новые миграции — только для **новых объектов**

### Формат файла
```sql
-- +goose Up
SELECT pg_advisory_lock(hashtext('metapus_migrations'));
-- ... DDL ...
SELECT pg_advisory_unlock(hashtext('metapus_migrations'));

-- +goose Down
SELECT pg_advisory_lock(hashtext('metapus_migrations'));
-- ... DROP ...
SELECT pg_advisory_unlock(hashtext('metapus_migrations'));
```

### Обязательные элементы для `cat_*` и `doc_*`

- CDC-колонки: `_txid BIGINT DEFAULT txid_current()`, `_deleted_at TIMESTAMPTZ`
- CDC-индекс: `CREATE INDEX idx_{table}_txid ON {table} (_txid) WHERE _deleted_at IS NULL`
- Триггер `_txid`: `BEFORE UPDATE → update_txid_column()`
- Триггер soft delete: `BEFORE UPDATE OF deletion_mark → soft_delete_with_timestamp()`
- Timestamps: всегда `TIMESTAMPTZ`, не `TIMESTAMP`

### Запреты
- **НЕ** используй `UPDATE` на регистрах (Immutable Ledger)
- **НЕ** создавай cross-tenant запросы
- **НЕ** добавляй `tenant_id` колонки в бизнес-таблицы

---

## Structured Errors

- Все ошибки — через `apperror.AppError`
- ErrorHandler middleware: единая точка маппинга `AppError → JSON`
- Handler: `c.Error(err) + c.Abort()`, **НЕ** `c.JSON(status, errorBody)`
- Context propagation: traceID, requestID, tenantID, userID
- Structured logging: все значимые поля как ключи лога

---

## Go Code Style

- **Конвенции**: `gofmt`, `go vet`, `golangci-lint`
- **Receiver name**: одна буква (`func (s *Service) Create(...)`)
- **Error handling**: `if err != nil { return ..., fmt.Errorf("action: %w", err) }`
- **Interfaces**: определяй в **потребителе** (domain), не в поставщике (infrastructure)
- **Generics**: для CRUD pipeline (`CatalogService[T]`, `BaseCatalogRepo[T]`)
- **Context**: первый параметр, всегда `ctx context.Context`

---

## Тестируемость

- Domain-логика покрыта **unit-тестами** (мокаем Repository через interface)
- Integration-тесты: `testcontainers-go` + реальный PostgreSQL
- `TxManager` в тестах — mock/fake
- Вычисления (`Calculate`, `GenerateMovements`) — чистые функции, тестируй без моков
- `Validate(ctx)` — не зависит от БД, тестируй напрямую

---

## Concurrency и Блокировки

### Optimistic Locking (по умолчанию)
- `Version` field + `WHERE version = $N` в UPDATE
- `AffectedRows == 0` → `CONCURRENT_MODIFICATION` (409)
- Подходит для справочников и документов

### Pessimistic Locking (критические операции)
- `SELECT ... FOR UPDATE` для проверки остатков
- Всегда блокируй в **фиксированном порядке** (resource ordering)
- Предотвращение deadlock: сортировка ключей (warehouseID + productID)

### Правила
- **NO** indefinite locks — транзакции должны быть короткими
- **Всегда** указывай таймауты (`statement_timeout`)
- **Prefer** retries с backoff для transient конфликтов

---

## Goroutine Leak Prevention

### Checklist для code review

1. **Inventory goroutines** — для каждой `go func()` зафиксируй owner и stop-signal
2. **Context lifecycle** — все `cancel()` вызываются (defer или явно)
3. **Channel contracts** — один owner для `close(ch)`, send/recv не блокируются навсегда
4. **Timers/Tickers** — `defer ticker.Stop()` обязателен
5. **I/O errors** — ошибка write/read → cleanup (cancel/unsubscribe/close)
6. **Cleanup order** — `cancel()` → `close(ch)` → удаление из registry
7. **Concurrency limits** — semaphore/worker pool для fan-out
8. **Observability** — `runtime.NumGoroutine()` метрика, `go.uber.org/goleak` в тестах

### Red flags
- `go func()` без stop-signal (fire-and-forget)
- `select` без `case <-ctx.Done()` в долгоживущей горутине
- `NewTicker` без `Stop()`
- Send в канал без таймаута/ctx — risk of partial deadlock
- Подписка переживает соединение

---

## UI Metadata Provisioning

- Metadata Registry: регистрация сущностей при старте (`cmd/server/main.go`)
- Каждое поле описывается: Name, Label, Type, Widget, Validators
- Dynamic Enums: endpoint для заполнения dropdown'ов
- **NO** хардкод списков полей на фронтенде
- Single Source of Truth: metadata из Go struct → frontend

---

## Analytical Reporting

- **Запрашивай регистры**, не документы — для аналитических отчётов
- **Slice-Last Pattern** — `DISTINCT ON` для периодических регистров сведений
- **No N+1** — батчи и оптимизированные JOIN'ы
- Reporting DTOs отдельно от CRUD DTOs
- White-list проверка полей в динамических запросах (защита от SQL injection)

---

## Связанные документы

- [15-naming-conventions.md](15-naming-conventions.md) — правила именования
- [11-transactions.md](11-transactions.md) — транзакции и блокировки
- [14-howto-new-entity.md](14-howto-new-entity.md) — применение правил на практике
- [04-core-layer.md](04-core-layer.md) — базовые типы и ошибки
